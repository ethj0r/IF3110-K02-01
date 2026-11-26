import http from 'k6/http';
import { check, sleep } from 'k6';
import exec from 'k6/execution';
import { Counter, Rate, Trend } from 'k6/metrics';

// Configuration via environment variables
const BASE_URL = (__ENV.BASE_URL || 'http://127.0.0.1:8080').replace(/\/+$/, '');
const ORIGIN = (__ENV.APP_ORIGIN || BASE_URL).replace(/\/+$/, '');
const USERNAME = __ENV.K6_USERNAME || __ENV.BOOTSTRAP_ADMIN_USERNAME || 'admin';
const PASSWORD = __ENV.K6_PASSWORD || __ENV.BOOTSTRAP_ADMIN_PASSWORD || '';
const POLL_INTERVAL_SEC = Number(__ENV.POLL_INTERVAL_SEC || '0.15');
const POLL_TIMEOUT_SEC = Number(__ENV.POLL_TIMEOUT_SEC || '30');

// Timeline boundaries (seconds) for per-stage breakdown:
//   0s  - 45s  : normal   (API: 0->10->10 VUs, Jobs: 2 VUs)
//   45s - 90s  : stress   (API: 10->40->40 VUs, Jobs: 6 VUs)
//   90s - 135s : peak     (API: 40->80->80 VUs, Jobs: 12 VUs)
//   135s - 190s: recovery (API: 80->10->10->0 VUs, Jobs: 2->0 VUs)
const STAGES = ['normal', 'stress', 'peak', 'recovery'];

function currentStage() {
  const elapsedSec = (Date.now() - exec.scenario.startTime) / 1000;
  if (elapsedSec < 45) return 'normal';
  if (elapsedSec < 90) return 'stress';
  if (elapsedSec < 135) return 'peak';
  return 'recovery';
}

// Overall custom metrics
const apiDuration = new Trend('api_response_time_ms', true);
const apiErrorRate = new Rate('api_error_rate');
const apiRequests = new Counter('api_requests_total');

const jobQueueWait = new Trend('migration_queue_wait_ms', true);
const jobCompletionDuration = new Trend('migration_job_duration_ms', true);
const jobExecDuration = new Trend('migration_job_exec_ms', true);
const jobsSubmitted = new Counter('migration_jobs_submitted');
const jobsSucceeded = new Counter('migration_jobs_succeeded');
const jobsFailed = new Counter('migration_jobs_failed');
const jobConflicts = new Counter('migration_same_db_conflicts');
const jobSuccessRate = new Rate('migration_job_success_rate');

// Per-stage custom metrics so handleSummary produces exact per-load-level stats
const stageApiDuration = {};
const stageApiErrors = {};
const stageApiReqs = {};
const stageQueueWait = {};
const stageJobDuration = {};
const stageJobsDone = {};
const stageJobConflicts = {};

for (const s of STAGES) {
  stageApiDuration[s] = new Trend(`stage_${s}_api_ms`, true);
  stageApiErrors[s] = new Rate(`stage_${s}_api_err_rate`);
  stageApiReqs[s] = new Counter(`stage_${s}_api_reqs`);
  stageQueueWait[s] = new Trend(`stage_${s}_queue_wait_ms`, true);
  stageJobDuration[s] = new Trend(`stage_${s}_job_duration_ms`, true);
  stageJobsDone[s] = new Counter(`stage_${s}_jobs_succeeded`);
  stageJobConflicts[s] = new Counter(`stage_${s}_job_conflicts`);
}

export const options = {
  scenarios: {
    api_stress: {
      executor: 'ramping-vus',
      exec: 'apiWorkload',
      startVUs: 0,
      stages: [
        { duration: '15s', target: 10 }, // ramp to normal
        { duration: '30s', target: 10 }, // hold normal
        { duration: '15s', target: 40 }, // ramp to stress
        { duration: '30s', target: 40 }, // hold stress
        { duration: '15s', target: 80 }, // ramp to peak
        { duration: '30s', target: 80 }, // hold peak
        { duration: '15s', target: 10 }, // ramp down to recovery
        { duration: '30s', target: 10 }, // hold recovery
        { duration: '10s', target: 0 },  // cool down
      ],
      gracefulRampDown: '10s',
    },
    migration_jobs: {
      executor: 'ramping-vus',
      exec: 'migrationWorkload',
      startVUs: 0,
      stages: [
        { duration: '15s', target: 2 },  // normal: 1 VU per DB (parallel across DBs)
        { duration: '30s', target: 2 },
        { duration: '15s', target: 6 },  // stress: concurrent same-DB + multi-DB
        { duration: '30s', target: 6 },
        { duration: '15s', target: 12 }, // peak: high contention same-DB + multi-DB
        { duration: '30s', target: 12 },
        { duration: '15s', target: 2 },  // recovery
        { duration: '30s', target: 2 },
        { duration: '10s', target: 0 },
      ],
      gracefulRampDown: '15s',
    },
  },
  thresholds: {
    api_response_time_ms: ['p(95)<500'],
    api_error_rate: ['rate<0.05'],
    migration_job_success_rate: ['rate>0.95'],
  },
};

function authHeaders(csrfToken, withJson = false) {
  const headers = { Origin: ORIGIN };
  if (csrfToken) headers['X-CSRF-Token'] = csrfToken;
  if (withJson) headers['Content-Type'] = 'application/json';
  return headers;
}

function login() {
  if (!PASSWORD) {
    throw new Error('Set K6_PASSWORD or BOOTSTRAP_ADMIN_PASSWORD before running k6');
  }
  const res = http.post(
    `${BASE_URL}/api/auth/login`,
    JSON.stringify({ username: USERNAME, password: PASSWORD }),
    { headers: authHeaders('', true), tags: { endpoint: 'login' } }
  );
  if (res.status !== 200) {
    throw new Error(`Login failed (${res.status}): ${res.body}`);
  }
  const body = res.json();
  const cookies = {};
  for (const [name, vals] of Object.entries(res.cookies || {})) {
    if (Array.isArray(vals) && vals.length > 0) {
      cookies[name] = { value: vals[0].value, replace: true };
    }
  }
  if (Object.keys(cookies).length === 0) {
    const rawSetCookie = res.headers['Set-Cookie'] || res.headers['set-cookie'] || '';
    const list = Array.isArray(rawSetCookie) ? rawSetCookie : [rawSetCookie];
    for (const item of list) {
      const pair = String(item).split(';')[0].trim();
      const eq = pair.indexOf('=');
      if (eq > 0) {
        cookies[pair.slice(0, eq)] = { value: pair.slice(eq + 1), replace: true };
      }
    }
  }
  if (Object.keys(cookies).length === 0) {
    throw new Error(`Login succeeded but no session cookie found in response headers: ${JSON.stringify(res.headers)}`);
  }
  return {
    user: body.user,
    csrf: body.csrf_token,
    cookies,
  };
}

function authedParams(session, extra = {}) {
  return {
    headers: authHeaders(session.csrf, Boolean(extra.json)),
    cookies: session.cookies,
    tags: extra.tags || {},
  };
}

// Refresh CSRF token if rotation/rejection happens during long runs
function refreshCsrf(session) {
  const res = http.get(`${BASE_URL}/api/auth/csrf`, authedParams(session, { tags: { endpoint: 'csrf' } }));
  if (res.status === 200) {
    session.csrf = res.json().csrf_token;
  }
}

export function setup() {
  const session = login();

  // Ensure the bootstrap admin is assigned as leader of all groups so reviews can be approved
  const groupsRes = http.get(`${BASE_URL}/api/admin/groups`, authedParams(session));
  if (groupsRes.status !== 200) {
    throw new Error(`Failed to list groups (${groupsRes.status}): ${groupsRes.body}`);
  }
  const groups = groupsRes.json() || [];
  const groupIds = [];
  for (const g of groups) {
    groupIds.push(g.id);
    if (!g.leader_user_id) {
      const assignRes = http.put(
        `${BASE_URL}/api/admin/groups/${g.id}/leader`,
        JSON.stringify({ user_id: session.user.id }),
        authedParams(session, { json: true })
      );
      if (assignRes.status !== 204 && assignRes.status !== 200) {
        console.warn(`Could not assign leader for group ${g.name}: ${assignRes.status} ${assignRes.body}`);
      }
    }
  }

  // Discover accessible databases with view_edit access
  const dbRes = http.get(`${BASE_URL}/api/databases`, authedParams(session));
  if (dbRes.status !== 200) {
    throw new Error(`Failed to list databases (${dbRes.status}): ${dbRes.body}`);
  }
  const allDbs = dbRes.json() || [];
  const editableDbs = allDbs.filter((d) => d.level === 'view_edit');
  if (editableDbs.length < 2) {
    throw new Error(
      `Need at least 2 view_edit target databases for multi-DB stress testing; found ${editableDbs.length}. Run 'make db-up && make bootstrap && make seed-dev'.`
    );
  }

  // Pick up to 2 lightweight target databases (prefer local-target and local-target_b)
  const preferred = editableDbs.filter((d) => !d.name.includes('large'));
  const targetDbs = (preferred.length >= 2 ? preferred : editableDbs).slice(0, 2);

  // Remove any leftover unexecuted drafts from earlier dev sessions so linear ordering starts clean
  for (const db of targetDbs) {
    const migRes = http.get(`${BASE_URL}/api/databases/${db.id}/migrations`, authedParams(session));
    if (migRes.status === 200) {
      const existing = migRes.json() || [];
      for (const m of existing) {
        if (m.status === 'draft' && !m.locked_at) {
          http.del(`${BASE_URL}/api/databases/${db.id}/migrations/${m.id}`, null, authedParams(session));
        }
      }
    }
  }

  console.log(
    `k6 setup complete: user=${session.user.username}, groups=${groupIds.length}, targetDbs=${targetDbs.map((d) => d.name).join(', ')}`
  );

  return {
    session,
    targetDbs: targetDbs.map((d) => ({ id: d.id, name: d.name })),
    groupIds,
  };
}

// Reuse the authenticated session from setup() to avoid Argon2id login storms
// distorting API/migration stress metrics; refresh only on 401/CSRF expiry.
let vuSession = null;

function ensureSession(data) {
  if (!vuSession) {
    vuSession = {
      user: data.session.user,
      csrf: data.session.csrf,
      cookies: data.session.cookies,
    };
  }
  return vuSession;
}

function recordApi(res, stage) {
  const ok = res.status >= 200 && res.status < 400;
  apiDuration.add(res.timings.duration);
  apiErrorRate.add(!ok);
  apiRequests.add(1);

  stageApiDuration[stage].add(res.timings.duration);
  stageApiErrors[stage].add(!ok);
  stageApiReqs[stage].add(1);
  return ok;
}

export function apiWorkload(data) {
  const session = ensureSession(data);
  const stage = currentStage();
  const db = data.targetDbs[(__VU + __ITER) % data.targetDbs.length];

  const endpoints = [
    { name: 'list_databases', url: `${BASE_URL}/api/databases` },
    { name: 'list_migrations', url: `${BASE_URL}/api/databases/${db.id}/migrations` },
    { name: 'migration_history', url: `${BASE_URL}/api/databases/${db.id}/migrations/history?hide_rollback=false` },
    { name: 'list_schemas', url: `${BASE_URL}/api/databases/${db.id}/schemas` },
  ];

  const target = endpoints[__ITER % endpoints.length];
  const res = http.get(target.url, authedParams(session, { tags: { kind: 'api', endpoint: target.name, stage } }));

  if (res.status === 401) {
    vuSession = login();
    return;
  }

  recordApi(res, stage);
  check(res, { 'API status 200': (r) => r.status === 200 });
  sleep(0.1);
}

export function migrationWorkload(data) {
  const session = ensureSession(data);
  const stage = currentStage();

  // Distribute VUs across target DBs:
  // At 2 VUs: VU 1 -> DB[1], VU 2 -> DB[0] (parallel different DBs)
  // At 6/12 VUs: multiple VUs contend on the SAME DB while other VUs run on a DIFFERENT DB
  const db = data.targetDbs[__VU % data.targetDbs.length];
  const title = `k6-${stage}-vu${__VU}-it${__ITER}`;

  // 1. Create migration draft (lightweight, idempotent, reversible SQL)
  const createPayload = JSON.stringify({
    title,
    sql_text: 'SELECT 1;',
    down_sql: 'SELECT 1;',
    batch_mode: false,
    batch_column: '',
    batch_size: 0,
    irreversible: false,
    irreversible_reason: '',
  });

  let createRes = http.post(
    `${BASE_URL}/api/databases/${db.id}/migrations`,
    createPayload,
    authedParams(session, { json: true, tags: { kind: 'migration_setup', endpoint: 'create_migration', stage } })
  );
  if (createRes.status === 403 && createRes.body && createRes.body.includes('csrf_rejected')) {
    refreshCsrf(session);
    createRes = http.post(
      `${BASE_URL}/api/databases/${db.id}/migrations`,
      createPayload,
      authedParams(session, { json: true, tags: { kind: 'migration_setup', endpoint: 'create_migration', stage } })
    );
  }
  if (createRes.status !== 201) {
    recordApi(createRes, stage);
    sleep(0.3);
    return;
  }
  recordApi(createRes, stage);
  const mig = createRes.json();

  // 2. Submit review for apply
  const reviewRes = http.post(
    `${BASE_URL}/api/databases/${db.id}/migrations/${mig.id}/reviews/apply`,
    null,
    authedParams(session, { tags: { kind: 'migration_setup', endpoint: 'submit_review', stage } })
  );
  if (reviewRes.status !== 200) {
    recordApi(reviewRes, stage);
    sleep(0.2);
    return;
  }
  const review = reviewRes.json();

  // 3. Approve review for all required groups
  for (const g of review.groups || []) {
    if (!g.approved && g.can_approve) {
      const appRes = http.put(
        `${BASE_URL}/api/databases/${db.id}/migrations/${mig.id}/reviews/apply/${review.id}/approvals/${g.id}`,
        null,
        authedParams(session, { tags: { kind: 'migration_setup', endpoint: 'approve_review', stage } })
      );
      if (appRes.status !== 204 && appRes.status !== 200) {
        recordApi(appRes, stage);
      }
    }
  }

  // 4. Submit migration job (POST /apply) — retry briefly on 409 (same-DB contention / linear order)
  let job = null;
  const maxSubmitRetries = 12;
  for (let attempt = 0; attempt < maxSubmitRetries; attempt++) {
    const applyRes = http.post(
      `${BASE_URL}/api/databases/${db.id}/migrations/${mig.id}/apply`,
      null,
      authedParams(session, { tags: { kind: 'migration_submit', endpoint: 'apply_migration', stage } })
    );

    if (applyRes.status === 202) {
      job = applyRes.json();
      jobsSubmitted.add(1);
      apiDuration.add(applyRes.timings.duration);
      apiErrorRate.add(false);
      apiRequests.add(1);
      stageApiDuration[stage].add(applyRes.timings.duration);
      stageApiErrors[stage].add(false);
      stageApiReqs[stage].add(1);
      break;
    }

    if (applyRes.status === 409) {
      // Expected contention when multiple VUs submit to the same DB simultaneously
      jobConflicts.add(1);
      stageJobConflicts[stage].add(1);
      sleep(0.25);
      continue;
    }

    // Unexpected error
    recordApi(applyRes, stage);
    break;
  }

  if (!job) {
    // Clean up unexecuted draft so it does not block subsequent linear versions
    http.del(
      `${BASE_URL}/api/databases/${db.id}/migrations/${mig.id}`,
      null,
      authedParams(session, { tags: { kind: 'migration_cleanup', endpoint: 'delete_draft', stage } })
    );
    sleep(0.2);
    return;
  }

  // 5. Poll job status until terminal state (202 Accepted does NOT count as migration success!)
  const pollStart = Date.now();
  let finalJob = job;
  while ((Date.now() - pollStart) / 1000 < POLL_TIMEOUT_SEC) {
    sleep(POLL_INTERVAL_SEC);
    const pollRes = http.get(
      `${BASE_URL}/api/databases/${db.id}/jobs/${job.id}`,
      authedParams(session, { tags: { kind: 'job_poll', endpoint: 'poll_job', stage } })
    );
    if (pollRes.status !== 200) {
      recordApi(pollRes, stage);
      continue;
    }
    finalJob = pollRes.json();
    if (finalJob.status === 'succeeded' || finalJob.status === 'failed' || finalJob.status === 'cancelled') {
      break;
    }
  }

  const succeeded = finalJob.status === 'succeeded';
  jobSuccessRate.add(succeeded);
  check(finalJob, { 'migration job completed with succeeded': (j) => j.status === 'succeeded' });

  if (succeeded) {
    jobsSucceeded.add(1);
    stageJobsDone[stage].add(1);
  } else {
    jobsFailed.add(1);
  }

  // Compute server-side queue wait & completion durations from job timestamps
  const createdMs = Date.parse(finalJob.created_at);
  const lockedMs = finalJob.locked_at ? Date.parse(finalJob.locked_at) : NaN;
  const finishedMs = finalJob.finished_at ? Date.parse(finalJob.finished_at) : Date.now();

  if (!Number.isNaN(createdMs) && !Number.isNaN(lockedMs)) {
    const qWait = Math.max(0, lockedMs - createdMs);
    jobQueueWait.add(qWait);
    stageQueueWait[stage].add(qWait);
  }
  if (!Number.isNaN(createdMs) && !Number.isNaN(finishedMs)) {
    const totalDur = Math.max(0, finishedMs - createdMs);
    jobCompletionDuration.add(totalDur);
    stageJobDuration[stage].add(totalDur);
  }
  if (!Number.isNaN(lockedMs) && !Number.isNaN(finishedMs)) {
    jobExecDuration.add(Math.max(0, finishedMs - lockedMs));
  }
}

function fmt(val, digits = 2) {
  if (val === undefined || val === null || Number.isNaN(val)) return '-';
  return Number(val).toFixed(digits);
}

export function handleSummary(data) {
  const m = data.metrics;
  const totalDurSec = (data.state.testRunDurationMs || 190000) / 1000;

  const lines = [];
  lines.push('\n==================== k6 STRESS TEST SUMMARY ====================');
  lines.push(`Total Run Duration       : ${fmt(totalDurSec, 1)} s`);
  lines.push(`Total HTTP Requests      : ${m.http_reqs ? m.http_reqs.values.count : 0} (${fmt(m.http_reqs ? m.http_reqs.values.rate : 0)} req/s)`);
  lines.push(`API Response Time p50    : ${fmt(m.api_response_time_ms?.values?.med)} ms`);
  lines.push(`API Response Time p95    : ${fmt(m.api_response_time_ms?.values?.['p(95)'])} ms`);
  lines.push(`API Error Rate           : ${fmt((m.api_error_rate?.values?.rate || 0) * 100, 3)} %`);
  lines.push(`Migration Jobs Submitted : ${m.migration_jobs_submitted?.values?.count || 0}`);
  lines.push(`Migration Jobs Succeeded : ${m.migration_jobs_succeeded?.values?.count || 0}`);
  lines.push(`Migration Jobs Failed    : ${m.migration_jobs_failed?.values?.count || 0}`);
  lines.push(`Same-DB 409 Contention   : ${m.migration_same_db_conflicts?.values?.count || 0}`);
  lines.push(`Job Throughput           : ${fmt((m.migration_jobs_succeeded?.values?.count || 0) / totalDurSec, 2)} jobs/s`);
  lines.push(`Queue Wait p50 / p95     : ${fmt(m.migration_queue_wait_ms?.values?.med)} ms / ${fmt(m.migration_queue_wait_ms?.values?.['p(95)'])} ms`);
  lines.push(`Job Duration p50 / p95   : ${fmt(m.migration_job_duration_ms?.values?.med)} ms / ${fmt(m.migration_job_duration_ms?.values?.['p(95)'])} ms`);
  lines.push('');
  lines.push('--- Per-Stage Breakdown (Markdown Table) ---');
  lines.push('| Stage | Window | API Reqs (req/s) | API p50 (ms) | API p95 (ms) | API Error (%) | Queue Wait p50/p95 (ms) | Job Dur p50/p95 (ms) | Jobs Done (jobs/s) | 409 Conflicts |');
  lines.push('| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |');

  const stageDurations = { normal: 45, stress: 45, peak: 45, recovery: 55 };
  for (const s of STAGES) {
    const dur = stageDurations[s];
    const reqs = m[`stage_${s}_api_reqs`]?.values?.count || 0;
    const apiP50 = m[`stage_${s}_api_ms`]?.values?.med;
    const apiP95 = m[`stage_${s}_api_ms`]?.values?.['p(95)'];
    const errPct = (m[`stage_${s}_api_err_rate`]?.values?.rate || 0) * 100;
    const qP50 = m[`stage_${s}_queue_wait_ms`]?.values?.med;
    const qP95 = m[`stage_${s}_queue_wait_ms`]?.values?.['p(95)'];
    const jP50 = m[`stage_${s}_job_duration_ms`]?.values?.med;
    const jP95 = m[`stage_${s}_job_duration_ms`]?.values?.['p(95)'];
    const done = m[`stage_${s}_jobs_succeeded`]?.values?.count || 0;
    const conf = m[`stage_${s}_job_conflicts`]?.values?.count || 0;

    lines.push(
      `| ${s} | ${dur}s | ${reqs} (${fmt(reqs / dur, 1)}) | ${fmt(apiP50)} | ${fmt(apiP95)} | ${fmt(errPct, 2)}% | ${fmt(qP50)} / ${fmt(qP95)} | ${fmt(jP50)} / ${fmt(jP95)} | ${done} (${fmt(done / dur, 2)}) | ${conf} |`
    );
  }
  lines.push('================================================================\n');

  return {
    stdout: lines.join('\n'),
    'k6/summary.json': JSON.stringify(data, null, 2),
  };
}
