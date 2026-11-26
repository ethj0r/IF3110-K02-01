# IF3110-K02-01

A PostgreSQL database management and migration platform built with Go and React.

## Status

The repository includes a Go server, configuration loading, PostgreSQL pooling, JSON response helpers, health checks, a goose migration runner, isolated integration tests, a React starter, an embedded frontend build, Docker Compose, Make targets, and CI.

Authentication, session management, CSRF protection, administrator bootstrap, group-based database access, administration, encrypted target connection management, schema/data inspection, transactional target migrations, schema comparison with editable migration generation, and query execution plan analysis are available.

Milestone 2 adds immutable up/down definitions, rollback/forward and complete execution history, per-group leader approvals, a durable asynchronous migration job queue with a worker, batch mode with checkpoints and partial/reverted progress, target-side execution evidence and administrator reconciliation, structured JSON logging, OpenTelemetry tracing, and observability metrics/dashboards.

## Requirements

- Go matching `backend/go.mod`.
- Node.js 22.14 or newer within the 22.x release line, and npm. Run `nvm use` if available.
- Docker with Compose and a running daemon.
- Make.

## Development

### Setup

Run from the repository root:

```sh
make setup
make db-up
# Set BOOTSTRAP_ADMIN_USERNAME and BOOTSTRAP_ADMIN_PASSWORD in .env first.
make bootstrap
make seed-dev
make seed-data
```

`make setup` copies `.env.example` to `.env` if the file does not exist, generates a local session encryption key when absent, adds placeholder bootstrap administrator variables and local target URLs when missing, restricts `.env` permissions, and installs dependencies using the lockfiles. `make bootstrap` applies platform migrations and creates the initial administrator. `make seed-dev` registers the local target connections, creates the `developers` group, adds the bootstrap administrator, and grants View + Edit on `local-*` connections. `make seed-data` runs `seed-sample` and `seed-large`: the former adds a curated movie list to `target-db` and a book list to `target-db-b`, while the latter fills `batch_demo.movies` on `target-db-large` with enough rows to time an unbatched `UPDATE` for batch testing. Make loads `.env`; direct binary or `go run` commands require exported environment variables. Use simple Make-compatible assignments as shown in the example file.

### Run the Application

Start the API, frontend, and migration worker in separate terminals:

```sh
make dev-api
```

```sh
make dev-web
```

```sh
make dev-worker
```

Every apply, rollback, forward, and retry is published to a durable PostgreSQL queue and processed by the worker. The API returns a job ID immediately; the SPA polls `/api/databases/{dbId}/jobs/{jobId}` for queued/running/final status. Without a running worker, jobs remain queued. One database has at most one active migration job, while different databases can run in parallel.

Open `http://127.0.0.1:5173`. Vite proxies `/api` to `HTTP_ADDR`, which defaults to `127.0.0.1:8080`, keeping browser requests on the same origin.

Client environment variables use the `VITE_` prefix. Never use that prefix for secrets.

### Health Checks

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/health` | Process liveness |
| GET | `/api/ready` | Platform database readiness |

API errors use `{"error":{"code":"...","message":"..."}}`. Error messages must not contain raw database errors or credentials.

### Logging and Tracing

The API and the worker write one JSON object per line to stdout through `log/slog` (`backend/internal/logging`). The CLI commands (`migrate`, `seed-*`) write the same format to stderr, so their normal output stays readable. Set `LOG_LEVEL` to `debug`, `info` (default), `warn`, or `error`. Logs are never stored in the platform database.

| Event | Level | Written when |
| --- | --- | --- |
| `http.request` | info, warn (4xx), error (5xx) | An `/api` request finishes: method, route pattern, status, bytes, duration |
| `sql.start` | info | A statement is sent: SQL text and bound parameters |
| `sql.finish` | info, error | A statement returns: duration, rows, command, and SQLSTATE and message on failure |
| `sql.begin` / `sql.commit` / `sql.rollback` | info, error | Transaction control, each logged as its own event |
| `migration.lock.acquired` / `.released` / `.failed` | info, warn, error | The per-database advisory lock, with `wait_ms` and `held_ms` |
| `migration.execution.*` | info, error | Execution started, committed, succeeded, rolled back, failed, or uncertain |
| `migration.job.claimed` / `.succeeded` / `.failed` / `.retry_scheduled` / `.idempotent` | info, warn, error | Worker job lifecycle; `claimed` includes `queue_wait_ms` |
| `migration.batch.committed` / `.reverted` / `.reconciled` / `.uncertain` | info, error | Each batch, with `batch`, cursor range, rows, and duration |
| `auth.login`, `auth.logout`, `admin.*`, `migration.draft.*`, `migration.review.*`, `migration.action.*`, `migration.plan.analyze`, `schema.compare` | info, warn | A state-changing action finished, with `outcome` (`succeeded`, `failed`, or `denied`), `status`, and the route IDs under `target` |
| `auth.session.rejected`, `auth.csrf.rejected`, `access.denied` | info, warn | A missing or invalid session, a failed CSRF or origin check, or a missing database or administrator permission |

Correlation attributes are added automatically:

- `request_id` on every record of one request.
- `trace_id` and `span_id` of the active span.
- `actor_id` and `actor` once the session is authenticated.
- `database_id` from the access check, or from the route for connection and grant administration.
- `migration_id`, `execution_id`, `version`, and `attempt` during migration execution.
- `job_id`, `worker_id`, and `batch` in the worker.

`db` is `platform` or `target`, and `query_id` pairs each `sql.start` with its result. Platform pools, target pools (viewer, EXPLAIN, migrations, connection tests), schema comparison snapshots, and goose platform migrations are traced through a `pgx` query tracer.

Redaction:

- Attributes whose key mentions a password, secret, token, cookie, CSRF, session identifier, DSN, or ciphertext become `[REDACTED]`.
- Connection URL credentials, `password=` pairs, and SQL `PASSWORD '...'` literals are masked in every value and in span query text.
- All parameters are masked when a statement touches credentials or sessions. Binary values are logged only by length.
- PostgreSQL detail lines, which can contain row values, are never logged.
- SQL is truncated at 4 KiB and each parameter at 256 bytes.

#### Tracing

OpenTelemetry tracing (`backend/internal/tracing`) is always on in the API and the worker. One queued migration produces one trace across both processes:

1. An `HTTP <METHOD> <route>` server span in the API, continuing an incoming W3C `traceparent` header.
2. The enqueue handler stores that span's `traceparent` with the job (`Tracing.Inject`).
3. The worker continues it (`Tracing.Extract`) with a consumer span `migration.job <action>`. It records `job_id`, `worker_id`, `attempt`, and `queue.wait_ms`.
4. `migration.lock`, with `migration.lock.acquire` measuring the lock wait.
5. `migration.execute`, recording migration and execution IDs, kind, direction, attempt, and the final status.
6. One `migration.batch` span per batch, with batch number and cursor range.
7. A client span per SQL statement (`sql SELECT`, `sql BEGIN`, `sql UPDATE`, `sql COMMIT`, …) with `db.query.text`, `db.response.rows`, and `db.response.status_code` (SQLSTATE).

Failures set the span status to error with a safe message.

| Variable | Default | Effect |
| --- | --- | --- |
| `OTEL_TRACES_EXPORTER` | `console` | `console` writes JSON spans to stderr, `otlp` sends OTLP/HTTP, `none` keeps trace IDs in logs without exporting spans (quieter local runs) |
| `OTEL_LOGS_EXPORTER` | `none` | `otlp` also ships every log record (same redaction and level) to the Collector with its trace and span IDs |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4318` | Collector endpoint for `otlp` traces and logs |
| `OTEL_SERVICE_NAME` | `wbd-api` (API), `wbd-worker` (worker) | `service.name` resource attribute; leave unset so the two processes stay distinguishable |

Sample log records captured from a local run, in time order: a forward migration (lock, execution, target SQL, commit, activity), then a failing EXPLAIN request. UUIDs are shortened and the request ID host is replaced.

```json
{"time":"2026-11-23T18:15:47.415395+07:00","level":"INFO","msg":"migration lock acquired","event":"migration.lock.acquired","database_id":"7288ccc1-…","wait_ms":0.323,"trace_id":"89c2c8edd9f1a3acb46f4a0907180a4c","span_id":"5f8abd93b9a2edb5","request_id":"host/etZhqJPnIk-000021","actor_id":"1706297c-…","actor":"admin","migration_id":"193909ec-…"}
{"time":"2026-11-23T18:15:47.427082+07:00","level":"INFO","msg":"migration execution started","event":"migration.execution.started","actor_id":"1706297c-…","actor":"admin","database_id":"7288ccc1-…","migration_id":"193909ec-…","execution_id":"b0e19c7c-…","version":1,"revision":1,"kind":"forward","direction":"up","attempt":1,"sql":"CREATE TABLE demo.logging_demo (id integer PRIMARY KEY)","trace_id":"89c2c8edd9f1a3acb46f4a0907180a4c","span_id":"85b0c50f3d04e8d4","request_id":"host/etZhqJPnIk-000021"}
{"time":"2026-11-23T18:15:47.442016+07:00","level":"INFO","msg":"sql statement finished","event":"sql.finish","db":"target","query_id":148,"duration_ms":5.249,"sql":"DO LANGUAGE plpgsql 'BEGIN EXECUTE ''CREATE TABLE demo.logging_demo (id integer PRIMARY KEY)''; END'","status":"ok","rows":0,"command":"DO","trace_id":"89c2c8edd9f1a3acb46f4a0907180a4c","span_id":"518ad095620387d2","request_id":"host/etZhqJPnIk-000021","actor_id":"1706297c-…","actor":"admin","database_id":"7288ccc1-…","migration_id":"193909ec-…","execution_id":"b0e19c7c-…","version":1,"attempt":1}
{"time":"2026-11-23T18:15:47.442565+07:00","level":"INFO","msg":"sql transaction committed","event":"sql.commit","db":"target","query_id":149,"duration_ms":0.483,"status":"ok","rows":0,"command":"COMMIT","trace_id":"89c2c8edd9f1a3acb46f4a0907180a4c","span_id":"4dd7f52af5e037f9","request_id":"host/etZhqJPnIk-000021","actor_id":"1706297c-…","actor":"admin","database_id":"7288ccc1-…","migration_id":"193909ec-…","execution_id":"b0e19c7c-…","version":1,"attempt":1}
{"time":"2026-11-23T18:15:47.448188+07:00","level":"INFO","msg":"migration.action.forward","event":"migration.action.forward","outcome":"succeeded","status":200,"target":{"dbID":"7288ccc1-…","migrationID":"193909ec-…"},"trace_id":"89c2c8edd9f1a3acb46f4a0907180a4c","span_id":"e9f2af90ad0cf833","request_id":"host/etZhqJPnIk-000021","actor_id":"1706297c-…","actor":"admin","database_id":"7288ccc1-…"}
{"time":"2026-11-23T18:17:41.921575+07:00","level":"ERROR","msg":"sql statement failed","event":"sql.finish","db":"target","query_id":51,"duration_ms":0.678,"sql":"EXPLAIN (ANALYZE, COSTS, VERBOSE, BUFFERS, FORMAT JSON) SELECT * FROM demo.missing_table","status":"error","sqlstate":"42P01","error":"relation \"demo.missing_table\" does not exist","trace_id":"dfe14e609b85b4bc3bb33f9fa853d4aa","span_id":"157079a1fa300720","request_id":"host/MiE29545E9-000012","actor_id":"1706297c-…","actor":"admin","database_id":"7288ccc1-…"}
{"time":"2026-11-23T18:17:41.922058+07:00","level":"WARN","msg":"http request completed","event":"http.request","method":"POST","route":"/api/databases/{dbID}/migrations/analyze","status":422,"bytes":93,"duration_ms":12.436,"trace_id":"dfe14e609b85b4bc3bb33f9fa853d4aa","span_id":"3c5a960783f5f892","request_id":"host/MiE29545E9-000012","actor_id":"1706297c-…","actor":"admin","database_id":"7288ccc1-…"}
```

Sample spans from the console exporter, trimmed to the main fields: a viewer request and its row query. `SpanKind` 2 is server and 3 is client. The SQL span's `Parent.SpanID` is the HTTP span.

```json
[
  {
    "Name": "HTTP GET /api/databases/{id}/tables/{table}/rows",
    "SpanContext": {
      "TraceID": "9fae9c7bd6a878dbf886cabcc2b24518",
      "SpanID": "04f640a8bc964308"
    },
    "Parent": {
      "SpanID": "0000000000000000"
    },
    "SpanKind": 2,
    "StartTime": "2026-11-23T18:17:10.814532+07:00",
    "EndTime": "2026-11-23T18:17:10.842979791+07:00",
    "Attributes": [
      {
        "Key": "http.request.method",
        "Value": "GET"
      },
      {
        "Key": "url.path",
        "Value": "/api/databases/7288ccc1-1ca1-419e-882a-8d1c885f031c/tables/movies/rows"
      },
      {
        "Key": "http.route",
        "Value": "/api/databases/{id}/tables/{table}/rows"
      },
      {
        "Key": "http.response.status_code",
        "Value": 200
      }
    ],
    "Status": {
      "Code": "Unset",
      "Description": ""
    },
    "Resource": [
      {
        "Key": "service.name",
        "Value": {
          "Type": "STRING",
          "Value": "wbd-api"
        }
      }
    ]
  },
  {
    "Name": "sql SELECT",
    "SpanContext": {
      "TraceID": "9fae9c7bd6a878dbf886cabcc2b24518",
      "SpanID": "2325ebab4d6c9cf9"
    },
    "Parent": {
      "SpanID": "04f640a8bc964308"
    },
    "SpanKind": 3,
    "StartTime": "2026-11-23T18:17:10.841588+07:00",
    "EndTime": "2026-11-23T18:17:10.842579958+07:00",
    "Attributes": [
      {
        "Key": "db.system.name",
        "Value": "postgresql"
      },
      {
        "Key": "db.namespace",
        "Value": "target"
      },
      {
        "Key": "db.operation.name",
        "Value": "SELECT"
      },
      {
        "Key": "db.query.text",
        "Value": "SELECT browse_row.* FROM \"demo\".\"movies\" AS browse_row ORDER BY \"browse_row\".\"id\", ROW(browse_row.*)::text COLLATE \"C\" LIMIT $1 OFFSET $2"
      },
      {
        "Key": "query_id",
        "Value": 28
      },
      {
        "Key": "db.response.rows",
        "Value": 2
      }
    ],
    "Status": {
      "Code": "Unset",
      "Description": ""
    },
    "Resource": [
      {
        "Key": "service.name",
        "Value": {
          "Type": "STRING",
          "Value": "wbd-api"
        }
      }
    ]
  }
]
```

#### Central collection (Collector, Tempo, Loki)

Follow [Grafana and OpenTelemetry Collector](#grafana-and-opentelemetry-collector) to run the API and worker with OTLP export. Traces go to Tempo and logs go to Loki. Each record keeps the same redaction as stdout and arrives with its trace and span IDs. The Collector removes SQL attributes before storage.

To correlate, copy a `trace_id` from any log line. Open it in Grafana Explore → Tempo, or run this query in Explore → Loki:

```logql
{service_name=~"wbd-api|wbd-worker"} | trace_id = "<trace_id>"
```

Filter by service or event to find the enqueue or completion record when the trace has many log entries.

Local check (batch migration, 12 rows, batch size 5, forwarded through the queue):

- Tempo returned one trace of 85 spans across `wbd-api` (14) and `wbd-worker` (71). Its chain is `HTTP POST …/forward` → `migration.job forward` → `migration.lock` → `migration.execute` → `migration.batch` → `sql UPDATE`.
- Loki returned 163 log records with the same `trace_id` (28 from the API, 135 from the worker): `http.request`, `migration.action.forward`, `migration.job.claimed` / `.succeeded`, `migration.lock.acquired` / `.released`, `migration.execution.started` / `.succeeded`, three `migration.batch.committed`, and the SQL events.
- No `sql` or `password` attribute was stored.

### Prometheus Metrics

Run `make dev-api METRICS_ENABLED=true` and scrape `GET /api/metrics` on the API listener.
With the default address, verify the response using `curl -fsS http://127.0.0.1:8080/api/metrics`.
The metrics endpoint is unauthenticated; bind the API to a trusted internal interface or
place it behind an authenticated reverse proxy when exposing it beyond localhost.
The endpoint is disabled by default. When enabled, restrict access to the monitoring network.

| Metric | Meaning |
| --- | --- |
| `wbd_migration_jobs{status}` | Retained queue records by status, including jobs without an execution yet |
| `wbd_migration_retry_jobs` | Retained jobs explicitly requested as retries |
| `wbd_migration_retry_attempts` | Additional worker attempts across retained jobs |
| `wbd_migration_execution_mean_seconds` | Mean duration of retained executions with start and finish timestamps |
| `wbd_migration_queue_wait_mean_seconds` | Mean time from eligibility to the latest recorded worker claim |
| `wbd_migration_lock_wait_max_seconds` | Longest current wait for a migration advisory lock |
| `wbd_migration_batches_processed{direction}` | Retained batches applied (`up`) or reverted (`down`) |
| `wbd_migration_rows_processed{direction}` | Rows applied (`up`) or reverted (`down`) in retained batches |
| `wbd_platform_pool_connections{state}` | Active and idle PostgreSQL connections in the platform pool |
| `wbd_platform_pool_wait_seconds_total` | Cumulative wait in seconds for successful acquisitions from the empty platform pool |
| `wbd_platform_pool_waited_acquires_total` | Successful acquisitions that waited for the pool |

Job and batch counts are snapshots of the migration store and reflect record removal. Queue wait excludes retry backoff and appears when a worker has a recorded claim. Lock wait measures ongoing contention at scrape time. Pool wait counters reset on API restart. Divide their rates to graph mean wait per delayed acquisition.

Sample `GET /api/metrics` output (after an approved batch migration of 12 rows with batch size 5):

```prometheus
# HELP wbd_migration_jobs Retained worker jobs by status.
# TYPE wbd_migration_jobs gauge
wbd_migration_jobs{status="queued"} 0
wbd_migration_jobs{status="running"} 0
wbd_migration_jobs{status="succeeded"} 1
wbd_migration_jobs{status="failed"} 0
wbd_migration_jobs{status="cancelled"} 0
# HELP wbd_migration_retry_jobs Retained jobs explicitly requested as retries.
# TYPE wbd_migration_retry_jobs gauge
wbd_migration_retry_jobs 0
# HELP wbd_migration_retry_attempts Automatic retry attempts across retained jobs.
# TYPE wbd_migration_retry_attempts gauge
wbd_migration_retry_attempts 0
# HELP wbd_migration_execution_mean_seconds Mean execution duration across retained jobs with start and finish timestamps.
# TYPE wbd_migration_execution_mean_seconds gauge
wbd_migration_execution_mean_seconds 0.042
# HELP wbd_migration_lock_wait_max_seconds Longest current wait for a migration advisory lock.
# TYPE wbd_migration_lock_wait_max_seconds gauge
wbd_migration_lock_wait_max_seconds 0
# HELP wbd_migration_batches_processed Batches applied or reverted across retained executions.
# TYPE wbd_migration_batches_processed gauge
wbd_migration_batches_processed{direction="up"} 3
wbd_migration_batches_processed{direction="down"} 0
# HELP wbd_migration_rows_processed Rows applied or reverted across retained batches.
# TYPE wbd_migration_rows_processed gauge
wbd_migration_rows_processed{direction="up"} 12
wbd_migration_rows_processed{direction="down"} 0
# HELP wbd_platform_pool_connections Connections currently checked out or idle in the platform pool.
# TYPE wbd_platform_pool_connections gauge
wbd_platform_pool_connections{state="active"} 1
wbd_platform_pool_connections{state="idle"} 3
# HELP wbd_platform_pool_wait_seconds_total Time spent waiting for successful acquisitions from an empty platform pool.
# TYPE wbd_platform_pool_wait_seconds_total counter
wbd_platform_pool_wait_seconds_total 0
# HELP wbd_platform_pool_waited_acquires_total Successful acquisitions that waited for an empty platform pool.
# TYPE wbd_platform_pool_waited_acquires_total counter
wbd_platform_pool_waited_acquires_total 0
# HELP wbd_migration_queue_wait_mean_seconds Mean time from eligibility to latest recorded worker claim across retained jobs.
# TYPE wbd_migration_queue_wait_mean_seconds gauge
wbd_migration_queue_wait_mean_seconds 0.018
```

### Grafana and OpenTelemetry Collector

With the local PostgreSQL databases running, start the optional monitoring stack from the repository root.

```sh
mkdir -p observability/logs
touch observability/logs/api.log observability/logs/worker.log
docker compose -f docker-compose.observability.yml up -d --wait
```

Start the API and worker in separate terminals with OTLP export for traces and logs.

```sh
make dev-api METRICS_ENABLED=true OTEL_TRACES_EXPORTER=otlp OTEL_LOGS_EXPORTER=otlp OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318
```

```sh
make dev-worker OTEL_TRACES_EXPORTER=otlp OTEL_LOGS_EXPORTER=otlp OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318
```

For file-based log collection, set `OTEL_LOGS_EXPORTER=none` on those commands and pipe stdout to `tee -a observability/logs/api.log` and `tee -a observability/logs/worker.log` respectively. Select one log export path per process to keep one copy of each record.

On native Linux Docker, append `HTTP_ADDR=0.0.0.0:8080` to the API command on a trusted development network so Prometheus can reach the host listener through the Docker bridge. If the API listens on another port, update the scrape target in `observability/prometheus.yml`.

1. Check the `wbd-api` scrape target at [Prometheus Targets](http://127.0.0.1:9090/targets).
2. Open [WBD Migration Observability](http://127.0.0.1:3000/d/wbd-migrations/wbd-migration-observability) with the local Grafana credentials `admin` / `admin`. Run an approved batch migration and check the job, queue wait, batch, and row panels.
3. Open Grafana Explore with Loki and run `{service_name="wbd-worker"} | event = "migration.job.succeeded"` to see the corresponding worker completion event.

The Collector accepts OTLP/HTTP on port 4318 and OTLP/gRPC on port 4317. It sends traces to Tempo and logs to Loki. To verify correlation independently of application instrumentation, run the demo below and paste its Loki query into Explore. Open the log's trace link to view the matching `observability.demo` span in Tempo. The demo emits synthetic telemetry and leaves application metrics unchanged.

```sh
sh observability/demo.sh
```

Stop the monitoring stack with `docker compose -f docker-compose.observability.yml down`.

## Local Databases

| Service | Host Port | Database | Purpose |
| --- | --- | --- | --- |
| `platform-db` | 55432 | `platform` | Platform metadata |
| `target-db` | 55433 | `target` | Target database inspection and migrations |
| `target-db-b` | 55435 | `target_b` | Second target for parallel-database jobs and comparisons |
| `target-db-large` | 55436 | `target_large` | Large dataset for batch migration testing |
| `test-db` | 55434 | `platform_test` | Disposable integration tests |

Database ports bind to loopback only. Credentials in Compose and `.env.example` are for local development. Do not use them in production or commit `.env`.

`make db-up` starts the platform and all three target databases. On startup in a non-production environment, each configured target URL is registered once as `local-<dbname>` (`TARGET_DATABASE_URL`, `TARGET_DATABASE_URL_2`, `TARGET_DATABASE_URL_3`). Seeding is idempotent by connection name and never overwrites an existing connection. Set `SEED_TARGET_DATABASES=true` to enable it in production; set it to any other value with `APP_ENV=production` to disable startup seeding. Connections are added through `POST /api/connections` or the administration page when you need a target outside these local services.

```sh
make db-test-up
make test-integration
make db-down
```

`db-down` preserves development database volumes. The test database uses temporary storage. Integration tests require `TEST_DATABASE_URL` pointing to a dedicated test database; they never fall back to `DATABASE_URL`. Each test receives an isolated schema that is removed during cleanup. Schema comparison tests also create two temporary databases on this dedicated test instance and drop them afterwards; its test role needs `CREATEDB`.

The test database user must also be able to create and drop temporary roles for SELECT-only target inspection tests. The Compose test user has these privileges.

### Adding a Target Database

A target database must already exist and be reachable from the API; the platform stores only connection metadata and an encrypted password. For a local stack, run `make db-up` and `make seed-dev`. For an external or additional database:

1. Start PostgreSQL somewhere the API can reach (for example `docker run -p 55437:5432 -e POSTGRES_USER=app -e POSTGRES_PASSWORD=secret -e POSTGRES_DB=app postgres:17-alpine`).
2. Sign in as a platform administrator and open `/admin/connections`, choose **New connection**, fill in the host, port, database, username, and password, then use **Test**.
3. Create or select a group in `/admin/groups`, add the user as a member, and grant View Only or View + Edit for the connection in `/admin/access`.
4. The connection appears in the workspace only after the user's group receives a grant and the user signs in again if their session was rotated.

Target connections currently force `sslmode=disable`; a target that only accepts TLS needs connection configuration support before it can be registered.

### Sample Data

`make seed-sample` creates small, readable datasets for browsing and migration demos:

- `demo.movies` in `target-db` (`TARGET_DATABASE_URL`): 12 films with title, release year, genre, rating, and views.
- `demo.books` in `target-db-b` (`TARGET_DATABASE_URL_2`): 12 classics with title, author, publication year, genre, and rating.

`make seed-large` creates `batch_demo.movies` in `target-db-large` (`TARGET_DATABASE_URL_3`): a generated movie catalog with `SEED_LARGE_ROWS` rows (default 2,000,000), then times one unbatched `UPDATE batch_demo.movies SET views = views + 1`. Re-running skips the insert unless `SEED_LARGE_FORCE=true`, which truncates and reseeds. All seeds are idempotent and use `ON CONFLICT (id) DO NOTHING` for the curated lists.

### Batch Dataset Measurement

The required large-dataset check was run on the local Compose target with
`SEED_LARGE_ROWS=2000000` and a batch size of 10,000:

| Operation | Rows | Duration |
| --- | --- | --- |
| Unbatched `UPDATE batch_demo.movies SET views = views + 1` | 2,000,000 | 11.808s |
| Batched apply (200 batches, each in its own transaction) | 2,000,000 | 23.522s |
| Batched rollback in reverse batch order | 2,000,000 | 15.105s |

This satisfies the requirement that one unbatched full-table update takes longer
than one second in the test environment. The batch run uses the same table and
dataset and reports 200 committed batches / 2,000,000 rows, then 200 reverted
batches / 2,000,000 rows. Reproduce with `make seed-large` (and optionally
`SEED_LARGE_ROWS`/`SEED_LARGE_FORCE`) plus a batch migration using the SQL form
described above.

## Validation

```sh
make check
make test-integration
```

`check` runs formatting checks, `go vet`, backend unit tests with the race detector, API client and DOM tests, ESLint, and TypeScript. Integration tests use the `integration` build tag and a real PostgreSQL database. CI runs these checks and builds the application.

### Browser Workflow Tests

Install Chromium once, start the API and frontend in separate terminals, and run:

```sh
(cd frontend && npx playwright install --no-shell chromium)
make test-e2e
```

The browser suite uses `APP_ORIGIN`, `TARGET_DATABASE_URL`, `TEST_DATABASE_URL`, and the bootstrap administrator credentials loaded by Make. Run `make dev-worker` alongside the API and frontend so queued migration actions finish. It requires the existing administrator's actual password, a local development application, and two distinct local PostgreSQL databases. Run `make db-test-up` before the schema comparison test. It creates temporary users, groups, connections, and uniquely named schemas, then removes them. Existing target tables are not used. If a migration outcome remains uncertain, the fixture is preserved for reconciliation instead of being retried or deleted.

Chromium exercises administrator user/group/membership/grant forms, real login, group-based access, the SQL editor, commit, rollback, history, inspection of quoted names, session revocation, logout, and schema comparison through editable generated SQL and execution. Traces and video are disabled; screenshots of the viewer and schema comparison are saved under the ignored `frontend/test-results/` directory. These tests run separately from `make check` and do not replace backend integration tests.

`e2e/responsive.spec.ts` signs in as the administrator at 360 px, 820 px, and 1440 px widths. It checks that no page scrolls horizontally, that the navigation drawer opens, closes with Escape, and navigates below 1024 px, and that the workspace, viewer, migration, and administration pages render without page errors. It only reads data.

## Frontend

The SPA is React with TanStack Query and React Router. All pages, including sign-in and administration, share one shell: a fixed sidebar on wide screens that becomes a slide-in drawer at 1024 px and below. Wide tables, SQL editors, and plans scroll inside their own containers, so the page itself never scrolls horizontally.

| Path | Purpose |
| --- | --- |
| `frontend/src/styles/tokens.css` | Color, radius, shadow, and spacing tokens |
| `frontend/src/styles/components.css` | Buttons, form controls, cards, tables, badges, notices, tabs, dialogs |
| `frontend/src/styles/layout.css` | App shell, drawer, page header, sign-in, database cards |
| `frontend/src/ui/` | Shared components: icons, cards, feedback, confirmation dialog |
| `frontend/src/layout/` | App shell and database section tabs |

Fonts (Inter and Geist Mono, OFL) are self-hosted through `@fontsource-variable` packages and embedded in the binary; the app makes no external font requests. Migration actions and destructive admin actions ask for confirmation in an in-app dialog.

## Production Build

```sh
make build
```

The build compiles React, embeds its assets in Go, and creates `bin/server`. The binary runs without a frontend directory at runtime. Set `DATABASE_URL` and `HTTP_ADDR` in the process environment before starting it.

Unknown API routes return JSON 404 responses. Frontend routes use an SPA fallback. Go can build and run from a clean clone without generated assets; frontend requests return 503 until the frontend is built. Use Vite during development.

## Platform Migrations

```sh
make migrate-status
make migrate-up
```

The goose runner manages platform metadata separately from target database migrations. Add SQL files to `backend/migrations/sql/` using unique sequential versions, such as `00001_create_users.sql`, with `-- +goose Up` and `-- +goose Down` directives.

The server does not apply migrations automatically. Run `migrate-up` before starting it; startup and readiness verify that the authentication schema is available.

## Database Viewer

1. Run setup/bootstrap and sign in as the platform administrator.
2. Open `/admin/connections`, add the target PostgreSQL connection, and use **Test** to verify reachability. Editing requires re-entering its password.
3. Create a group, add members (including the administrator if they need database access), and assign its database grant in `/admin/access`.
4. Open the workspace, select the database, then choose a schema and table to inspect columns, constraints, indexes, and paginated data.
5. Use **Migrations** in the viewer to open that database's history. The global migration page also provides a dropdown of assigned databases. View Only can inspect history; View + Edit enables draft and execution controls.

Connections use `SESSION_KEY` to encrypt target passwords. Keep it stable: replacing it invalidates sessions and makes saved target passwords unreadable until re-entered. Passwords and ciphertext are never returned by connection metadata APIs.

On server startup, configured target URLs seed connections named `local-<dbname>` in non-production environments. Seeding is idempotent by connection name, does not overwrite an existing connection, and grants no access automatically. Use `make seed-dev` to create the `developers` group and grant access to the `local-*` connections. Manage additional connections through the administration page.

Rows default to 25 per page, with a maximum of 100. Cells use PostgreSQL text representations to preserve numeric precision; SQL NULL remains distinct from empty text. Schema/table names are literal UTF-8 PostgreSQL identifiers, limited to 63 bytes without NUL. Spaces, Unicode, punctuation, embedded quotes, slash, and percent characters are supported. Values are bound as query parameters or quoted with `pgx.Identifier`; SQL-like text in a name remains a literal name. The frontend URL-encodes names and the API preserves their exact spelling.

Rows are ordered by primary key, with full-row text as a tie-breaker or fallback for tables/views without a key. Count and data share one read-only repeatable-read snapshot per request; a new page request sees a fresh snapshot. Inspection queries have a ten-second request budget and connection setup a five-second timeout. Column types include length/precision, and constraints include their PostgreSQL definitions.

Target connections currently use `sslmode=disable`; URL TLS options are not retained by the connection store. Use the local Compose target or an appropriately protected database network. Targets requiring TLS need additional connection configuration support.

Deleting a connection also deletes its platform grants and migration history; it does not drop the target database. Verification commands are `make check`, `make db-test-up`, `make test-integration`, and `make build`.

## Target Database Migrations

Target database migrations are ordered, linear changes with immutable up/down SQL.
A reversible migration requires down SQL; an irreversible migration requires a
reason and cannot be rolled back. Definitions can be edited only before their first
execution attempt; after that, corrections require a new migration.

Every apply, rollback, forward, retry, success, and failure is stored as an
execution with one or more attempts. The exact SQL snapshot is recorded on both the
execution and the attempt, along with the version, revision, actor, timestamps,
status, and error category. Application logs are separate and go to stdout/stderr.

### Endpoints

| Method | Path | Required Access |
| --- | --- | --- |
| `GET` | `/api/databases/{dbId}/migrations` | View Only / View + Edit |
| `POST` | `/api/databases/{dbId}/migrations` | View + Edit |
| `GET` | `/api/databases/{dbId}/migrations/{id}` | View Only / View + Edit |
| `PUT` | `/api/databases/{dbId}/migrations/{id}` | View + Edit |
| `DELETE` | `/api/databases/{dbId}/migrations/{id}` | View + Edit |
| `GET` | `/api/databases/{dbId}/migrations/history?hide_rollback=false` | View Only / View + Edit |
| `GET` | `/api/databases/{dbId}/jobs?limit=10` | View Only / View + Edit |
| `GET` | `/api/databases/{dbId}/jobs/{jobId}` | View Only / View + Edit |
| `POST` | `/api/databases/{dbId}/migrations/{id}/execute` | View + Edit |
| `POST` | `/api/databases/{dbId}/migrations/{id}/apply` | View + Edit |
| `POST` | `/api/databases/{dbId}/migrations/{id}/rollback` | View + Edit |
| `POST` | `/api/databases/{dbId}/migrations/{id}/forward` | View + Edit |
| `POST` | `/api/databases/{dbId}/migrations/{id}/retry` | View + Edit |

Action endpoints return `202 Accepted` with a migration job. The worker claims one
job at a time per database, revalidates the action under a cross-worker advisory
lock, executes it, and records the execution ID and final job status. Only the last
active reversible migration can be rolled back. Forward is limited to the next
version after the highest applied version and never runs subsequent migrations
automatically. Retry repeats the latest failed execution snapshot. Hide rollback
removes a successful rollback execution and the execution it cancelled, paired by
execution ID; failed attempts remain visible.

Batch mode executes a cursor-based statement once per batch, each in its own target
transaction. The up script receives `$1` as the cursor and `$2` as the batch size and
must return the cursor column; `down_sql` receives `$1` as `cursor_to` and `$2` as
`cursor_from`. Committed batches and rows are recorded, a later failure leaves the
execution `partial`, retry resumes from the checkpoint, and rollback compensates
committed batches in reverse order. The worker writes a target-side evidence row in
each batch transaction so a lost platform record can be reconciled. See the sample
large dataset from `make seed-large`.

SQL runs atomically inside a target transaction. Confirmed failure records a generic
error and preserves history. Jobs and executions survive restarts. A worker that
loses its lease before recording a result leaves the job failed as uncertain; the
execution remains running/pending and blocks further actions until the target
outcome is verified. Duplicate delivery cannot commit twice because the execution
gate rejects a second run once an execution exists. See [`docs/api.md`](docs/api.md)
for the full execution model and error specifications.

### Approvals, Timeouts, and Recovery

Every apply, rollback, forward, and retry action requires a review approved by the
leader of every group with any access to the database. Review identity includes the
migration revision, action, and previous execution; editing the definition or the
previous execution invalidates the review. Approvals are checked when the action is
enqueued, when the execution is created, and again when the worker retries.

Worker executions use `WORKER_EXECUTION_TIMEOUT` (default `30m`), independent of the
API timeout. While a job runs, the worker renews its lease every third of
`WORKER_LEASE`, so another worker does not expire a legitimately long migration. A
statement cancelled by its deadline is rolled back with a fresh cleanup context and
recorded as a confirmed `execution_timeout` failure; later migrations are not
blocked.

If a commit result is lost or a worker dies, the job becomes failed/uncertain and
the execution stays `running`, blocking later actions. A platform administrator
verifies the target and uses the execution reconcile action (or
`POST /api/databases/{dbId}/migrations/{id}/executions/{executionId}/reconcile`)
with `{"outcome":"succeeded"|"failed","note":"..."}`. Target evidence is available
at `GET .../executions/{executionId}/evidence`. Reconciliation closes open attempts,
updates the execution and job, refreshes the migration status, and preserves the
SQL history. The per-database advisory lock is released when the worker's platform
connection closes; the pending execution is what blocks later actions until
reconciliation.

Worker configuration: `WORKER_ID` overrides the generated worker identity,
`WORKER_EXECUTION_TIMEOUT` controls execution time (default `30m`), `WORKER_LEASE`
controls the claim lease (default `2m`), and `WORKER_POLL` controls idle polling
(default `2s`). The production build creates both `bin/server` and `bin/worker`.

## Query Execution Plan Analysis

Use **Analyze Plan (EXPLAIN)** in the migration editor or migration details to inspect a data query before execution. The report shows scan types, indexes, join strategies, estimated cost, execution/planning times, row counts, and the raw JSON plan. This action requires database view access, a session, and CSRF protection.

The backend runs `EXPLAIN ANALYZE` inside a target transaction that is rolled back afterwards. PostgreSQL DDL such as `CREATE TABLE` or generated `ALTER TABLE` statements has no query execution plan; use this feature with supported data queries such as `SELECT`, `INSERT`, `UPDATE`, or `DELETE`. Schema comparison still imports candidates into the same editable migration editor and requires View + Edit on the current database.

## Schema Comparison and Migration Generation

1. Register the current PostgreSQL database and a reference database containing the desired schema. The namespace must have the same name in both databases, such as `public`; it may be absent from the current database.
2. Grant the user **View + Edit** on the current database and at least **View Only** on the reference database. Platform administrators also need these group grants.
3. Open the current database and choose **Compare schemas**. Select the reference database and schema, then compare.
4. Review the ordered SQL statements and their reported hazards. Choose **Use SQL in migration editor** to review or change the generated SQL and title.
5. Save the draft and execute it explicitly using the existing migration workflow. Run comparison again after execution to check the result.

The comparison direction is **current → reference**. Generation reads catalog snapshots in read-only transactions, does not compare/copy row data, and does not save or execute a migration. It uses [pg-schema-diff v1.0.9](https://github.com/stripe/pg-schema-diff/tree/v1.0.9) for dependency ordering and PostgreSQL DDL generation. Index operations use ordinary transactional SQL to match the migration executor. No shadow database is created and the candidate is not rehearsed against live data before review.

When an existing enum gains values alongside other changes, the generated candidate contains only the enum additions. The UI marks this preparation step: commit it, compare again, then review and execute the remaining migration. This follows [PostgreSQL's enum transaction rules](https://www.postgresql.org/docs/17/sql-altertype.html#SQL-ALTERTYPE-NOTES): new values must be committed before they are used in defaults or expressions. The full statement list still shows the remaining differences.

Object renames are treated as removal and creation; review destructive statements carefully. Unsupported generator changes return an error so the migration can be written manually. Schema snapshots containing user-defined domains, standalone composite types, range/multirange types, or base types outside an extension return `422` instead of reporting a misleading match. Removing or reordering existing enum labels also returns `422`; these changes need an explicitly authored migration. Adding labels before, between, or after existing values is supported. Role creation is outside the generator's scope. Cross-schema dependencies must already be available on the current database. Comparison has a 20-second deadline and a generated SQL limit of 48,000 bytes, with a separate JSON size limit to fit migration drafts. Changes made to either database after comparison require a fresh comparison.

### Demonstration Walkthrough

Start the databases with `make db-up` and `make db-test-up`, then run the API and frontend. Register separate connections for the local target (`TARGET_DATABASE_URL`) and the local test database (`TEST_DATABASE_URL`). Keep the test database running during the demonstration; its storage is temporary. Use a group with View + Edit on both connections while preparing the two schemas. After preparation, the reference connection only needs View Only; reducing its grant requires signing in again.

Use an unused namespace reserved for this demonstration, such as `schema_diff_demo`. Run each preparation script as a normal migration against its own connection.

Current database:

```sql
CREATE SCHEMA schema_diff_demo;
CREATE TABLE schema_diff_demo.items (id integer PRIMARY KEY, title text);
INSERT INTO schema_diff_demo.items VALUES (1, 'Existing data');
```

Reference database:

```sql
CREATE SCHEMA schema_diff_demo;
CREATE TABLE schema_diff_demo.items (
    id integer PRIMARY KEY,
    title text,
    enabled boolean NOT NULL DEFAULT true
);
CREATE INDEX items_title ON schema_diff_demo.items (title);
```

Open the current database, choose **Compare schemas**, select the reference connection and `schema_diff_demo`, and compare. Show the proposed column and index changes with the reported index lock hazard. Open the current viewer to confirm generation has not changed it, then compare again and import the SQL into the migration editor. Append the following statement to demonstrate review and editing:

```sql
INSERT INTO schema_diff_demo.items (id, title) VALUES (2, 'Reviewed SQL');
```

Save Draft, execute, and show the committed history entry and both rows in the viewer. Compare again: no schema differences should remain. Row contents are not copied from the reference. To repeat the demonstration, run `DROP SCHEMA schema_diff_demo CASCADE` as a new migration on each connection, then run the preparation scripts again. Only use this reset for the namespace reserved for this demonstration.

### Tested Scenarios

Backend tests exercise table dependencies, column types/defaults/nullability, constraints, index replacement, enum creation and staged additions, serial sequences, views/materialized views, functions/triggers, RLS policies, literal Unicode/quoted identifiers, data-only and empty comparisons, unsupported types, target failures, read-only credentials/snapshots, cancellation, and concurrent comparisons. Generated SQL is applied through the same atomic script executor used by migrations, followed by a fresh comparison. A data-conflict test verifies complete rollback and a reviewed backfill before successful execution.

Browser tests cover both ordinary and staged enum migrations, editable imports without automatic save/execution, connection failure/recovery, removal of stale results, and reference grant revocation, alongside the existing authentication, administration, viewer, and migration workflow. Candidate generation does not rehearse live row data: incompatible existing data can still reject execution and roll back. Review hazards and prepare any required data backfill before a real migration.

## Authentication

### Initial Administrator

Provide `BOOTSTRAP_ADMIN_USERNAME` and `BOOTSTRAP_ADMIN_PASSWORD` through the process environment, then run:

```sh
make seed-admin
```

`make bootstrap` combines `migrate-up` and `seed-admin`. There is no default administrator password. Usernames contain 3–64 ASCII letters, digits, dots, underscores, or hyphens and are matched case-insensitively. Passwords contain 12–1024 bytes and are stored using Argon2id with independent salts. Repeating bootstrap for the same active administrator preserves its password; bootstrap cannot promote an existing ordinary account or create an additional administrator.

### Endpoints

| Method | Path | Protection |
| --- | --- | --- |
| POST | `/api/auth/login` | Exact trusted Origin and JSON body |
| GET | `/api/auth/me` | Session cookie |
| GET | `/api/auth/csrf` | Session cookie |
| POST | `/api/auth/logout` | Session cookie, trusted Origin, and `X-CSRF-Token` |

Login accepts `{"username":"...","password":"..."}`. Login and `me` return `user`, `csrf_token`, and `expires_at`. The opaque session identifier is sent exclusively in an HttpOnly cookie and never in a response body or URL. Protected unsafe requests require the CSRF header and exact configured Origin. Cookies use SameSite=Lax, Path=/, and Secure on HTTPS. Auth responses are not cached.

The frontend keeps CSRF data in memory, restores it through the authenticated endpoint after reload, and sends cookies through same-origin requests. Login, logout, protected routes, and service errors work without additional styling. Passwords and session identifiers are not stored in browser storage.

### Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `APP_ENV` | `development` | Production requires HTTPS |
| `APP_ORIGIN` | `http://127.0.0.1:5173` | Trusted browser origin, without a path |
| `SESSION_KEY` | Required | Base64-encoded 32-byte AES-GCM key |
| `SESSION_IDLE_TIMEOUT` | `30m` | Maximum inactivity |
| `SESSION_ABSOLUTE_TIMEOUT` | `12h` | Maximum authenticated session lifetime |
| `SESSION_ROTATION_INTERVAL` | `15m` | Periodic identifier rotation |
| `SESSION_GRACE_PERIOD` | `30s` | Previous identifier validity after rotation |
| `BOOTSTRAP_ADMIN_USERNAME` | Required by `seed-admin` | Initial administrator username |
| `BOOTSTRAP_ADMIN_PASSWORD` | Required by `seed-admin` | Initial administrator password, 12–1024 bytes |

Set `APP_ORIGIN` to the actual application origin when serving the embedded frontend directly, such as `http://127.0.0.1:8080` locally. Production must use an HTTPS origin. Keep `SESSION_KEY` stable and secret; replacing it requires users to log in again.

Rotation preserves absolute expiration. Re-authenticating an active session rotates its identifier without extending its lifetime. A fully expired session requires a fresh login. During grace, a previous identifier can recover the current cookie if a rotation response was lost. Logout revokes the session and every associated identifier.

### Backend Integration

`Auth.Handler.RequireSession` authenticates protected routes and enforces CSRF on unsafe methods. `Auth.PrincipalFromContext` returns the current user identity. Database permission checks remain separate from authentication; Platform Admin is not a database access level.

Within an existing database transaction, call `Auth.MarkPrivilegeChange` when privileges increase and `Auth.InvalidateUserSessions` when access is revoked. Both accept the affected user ID. The schema automatically invalidates sessions after deactivation, password changes, or administrator demotion, and marks administrator promotion for rotation on the next request.

## Access Control and Administration

Database access is granted to groups, and users inherit the highest grant across their groups. `view_only` permits viewing, while `view_edit` permits viewing and editing. Platform administrators do not bypass database grants; an administrator without a group grant cannot access that database. Missing or unavailable policy evaluators deny access.

The administration UI is available at `/admin` for platform administrators:

| Route | Purpose |
| --- | --- |
| `/admin/users` | Create and deactivate users, reset passwords, and delete accounts |
| `/admin/groups` | Manage groups and memberships |
| `/admin/access` | Assign or remove group database grants |
| `/admin/connections` | Add, edit, test, and remove target connections |

The admin API is under `/api/admin` and every route requires an authenticated platform administrator. Creating or promoting administrators is not supported through the API. Deactivating or deleting the last active administrator is rejected with `last_admin`.

## Stress Testing with k6

Stress testing is implemented with [Grafana k6](https://k6.io/) in [`k6/stress.js`](k6/stress.js) to evaluate API latency, throughput, queue wait time, asynchronous migration completion, same-database contention, multi-database parallelism, and post-peak system recovery.

### Script Overview

[`k6/stress.js`](k6/stress.js) runs two concurrent `ramping-vus` scenarios across four load stages (**Normal → Stress → Peak → Recovery**, total `3m10s` plus graceful ramp-down):

1. **`api_stress` (0 → 10 → 40 → 80 → 10 → 0 VUs):**
   Continuously exercises authenticated read endpoints (`GET /api/databases`, `GET /api/databases/{dbId}/migrations`, `GET /api/databases/{dbId}/migrations/history`, and `GET /api/databases/{dbId}/schemas`) across both target databases.
2. **`migration_jobs` (0 → 2 → 6 → 12 → 2 → 0 VUs):**
   In `setup()`, the script authenticates as the bootstrap administrator, ensures the group leader is assigned, selects two target databases (`local-target` and `local-target_b`), and clears any unexecuted leftover drafts. During the test, VUs distribute across both databases (`__VU % 2`): at 2 VUs both databases run in parallel without contention; at 6 and 12 VUs multiple VUs contend on the **same database** while other VUs execute concurrently on a **different database**. Each iteration:
   - Creates a reversible migration draft (`SELECT 1;`), submits an `apply` review, and approves it as the group leader.
   - Submits the approved migration via `POST /api/databases/{dbId}/migrations/{id}/apply` (retrying briefly on expected `409` same-database active-job / linear-order contention, tracked separately as `migration_same_db_conflicts`).
   - Polls `GET /api/databases/{dbId}/jobs/{jobId}` every `150ms` until the job reaches a terminal state (`succeeded`, `failed`, or `cancelled`). A `202 Accepted` enqueue response is **not** counted as migration success; only jobs reaching `status === "succeeded"` are counted.
   - Computes server-side **queue wait** (`locked_at - created_at`) and **job completion duration** (`finished_at - created_at`) directly from job timestamps.

### Thresholds & How to Run

Configured k6 thresholds:

- `api_response_time_ms`: `p(95) < 500` ms
- `api_error_rate`: `rate < 0.05` (`< 5%`)
- `migration_job_success_rate`: `rate > 0.95` (`> 95%`)

Run locally against the API server and two asynchronous workers:

```sh
make db-up && make bootstrap && make seed-dev
make build

# Terminal 1 (API)
set -a && . ./.env && set +a && LOG_LEVEL=warn ./bin/server

# Terminal 2 & 3 (2 parallel workers)
set -a && . ./.env && set +a && WORKER_ID=worker-1 WORKER_POLL=100ms LOG_LEVEL=warn ./bin/worker
set -a && . ./.env && set +a && WORKER_ID=worker-2 WORKER_POLL=100ms LOG_LEVEL=warn ./bin/worker

# Terminal 4 (k6 stress test)
set -a && . ./.env && set +a && k6 run k6/stress.js
```

### Results (2 Target Databases, 2 Workers, `WORKER_POLL=100ms`)

Overall run summary (`190.3s` total):

- **Total HTTP Requests:** `24,521` (`128.84 req/s`)
- **API Response Time:** `p50 = 137.01 ms`, `p95 = 601.48 ms` *(crossed `500 ms` threshold at Peak load — see analysis)*
- **API Error Rate:** `0.000%` *(passed `< 5%` threshold)*
- **Migration Jobs:** `713` submitted, `713` succeeded (`100.0%`, passed `> 95%` threshold), `0` failed (`3.75 jobs/s` overall)
- **Same-DB `409` Contention:** `501` expected conflicts
- **Queue Wait:** `p50 = 52.00 ms`, `p95 = 121.40 ms`
- **Job Completion Duration:** `p50 = 158.00 ms`, `p95 = 379.80 ms`

| Stage | VUs (API + Jobs) | Window | API Reqs (req/s) | API p50 (ms) | API p95 (ms) | API Error (%) | Queue Wait p50 / p95 (ms) | Job Dur p50 / p95 (ms) | Jobs Done (jobs/s) | 409 Conflicts |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| **Normal** | `10 + 2` | 45s | `3,682` (`81.8`) | `15.70` | `35.59` | `0.00%` | `37.00` / `85.00` | `93.00` / `151.00` | `283` (`6.29`) | `0` |
| **Stress** | `40 + 6` | 45s | `6,983` (`155.2`) | `122.80` | `293.25` | `0.00%` | `66.00` / `128.70` | `246.00` / `394.10` | `167` (`3.71`) | `145` |
| **Peak** | `80 + 12` | 45s | `6,460` (`143.6`) | `443.24` | `718.23` | `0.00%` | `83.00` / `140.90` | `295.00` / `421.00` | `83` (`1.84`) | `289` |
| **Recovery** | `10 + 2` → `0` | 55s | `4,235` (`77.0`) | `63.36` | `564.21` | `0.00%` | `52.50` / `129.15` | `195.00` / `398.40` | `180` (`3.27`) | `67` |

### Analysis per Load Level

1. **Normal (`10` API VUs + `2` Job VUs):**
   The platform easily stays within SLA (`p50 = 15.70 ms`, `p95 = 35.59 ms`, `0.00%` errors). Because each job VU targets a different database and two workers run in parallel, there are `0` same-DB conflicts and job throughput peaks at `6.29 jobs/s` (`283` completed jobs, `p50` queue wait `37 ms` within the `100 ms` poll interval).
2. **Stress (`40` API VUs + `6` Job VUs):**
   API throughput nearly doubles to `155.2 req/s` with `p95 = 293.25 ms` (still well under the `500 ms` threshold) and `0.00%` errors. With 3 job VUs contending per database, same-DB serialization triggers `145` expected `409` conflicts, increasing median queue wait to `66 ms` and job completion duration to `246 ms` (`3.71 jobs/s`).
3. **Peak (`80` API VUs + `12` Job VUs):**
   The system reaches its saturation knee: API throughput plateaus/dips slightly to `143.6 req/s` while `p50` rises to `443.24 ms` and `p95` reaches `718.23 ms`, crossing the `500 ms` `p95` threshold. The primary bottlenecks are platform PostgreSQL connection pool contention (`pgxpool` default `MaxConns`) shared across 92 concurrent VUs plus heavier `GET /migrations` and `/migrations/history` reads as the history table grows past 600+ executions, along with `289` same-DB `409` conflicts from 6 VUs per database competing for linear execution order. Crucially, the system degrades gracefully: **`0.00%` API errors**, **`0` failed jobs**, and no stuck locks or double commits.
4. **Recovery (`80 → 10` API VUs, `12 → 2` Job VUs):**
   As load drops back to normal, the system recovers automatically without intervention: median API latency drops **7×** (`443.24 ms → 63.36 ms`; `p95` remains `564.21 ms` only because of the 15-second ramp-down tail from 80 to 10 VUs), queue wait drops (`83 ms → 52.5 ms`), job duration drops (`295 ms → 195 ms`), and job throughput rebounds to `3.27 jobs/s` (`180` jobs completed, `0` failures).

## Security: SAST with CodeQL

Static Application Security Testing (SAST) is automated via GitHub Actions in [`.github/workflows/codeql.yml`](.github/workflows/codeql.yml) using GitHub CodeQL (`+security-extended` query suite).

### Analysis Scope

| Language | Build Mode | Scope Analyzed | Ignored Paths |
| --- | --- | --- | --- |
| `go` | `manual` (`go build ./...` in `backend/`) | All Go backend and worker packages (`backend/cmd/...`, `backend/internal/...`, `backend/migrations/...`) | `backend/internal/webui/dist` |
| `javascript-typescript` | `none` | React SPA frontend (`frontend/src/...`, `frontend/tests/...`, `frontend/e2e/...`) | `frontend/dist`, `frontend/node_modules` |

The workflow runs on `push` to `main`, `dev`, and `feat/codeql`, on `pull_request` to `main` and `dev`, and via manual `workflow_dispatch`. Results are uploaded to GitHub **Security → Code scanning** and also exported as SARIF artifacts (`codeql-sarif-go`, `codeql-sarif-javascript-typescript`) with a summary printed in the workflow logs.

### Initial Scan Results & Findings Triage (Commit `98a96c9`)

- **`javascript-typescript`:** `0` findings.
- **`go`:** `6` findings (all `go/log-injection`, CWE-117, severity **Medium**, score `6.1`).

| # | Rule (CWE) | Severity | Location | Potential Impact | Decision & Rationale | Fix Commit |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | `go/log-injection` (CWE-117) | Medium (6.1) | `backend/internal/migration/repository.go:75` | User-controlled `database_id` from HTTP route params logged on `migration.lock.failed`; unescaped CR/LF could forge log entries in line-oriented log consumers. | **Valid — Fixed.** Although `slog.JSONHandler` escapes newlines in JSON output, sanitizing CR/LF before logging provides defense-in-depth across all log handlers/OTLP exporters. | `0677e19` |
| 2 | `go/log-injection` (CWE-117) | Medium (6.1) | `backend/internal/migration/repository.go:78` | Same as #1, on `migration.lock.acquired`. | **Valid — Fixed.** Sanitized `DatabaseID` via `Logging.SanitizeLogValue` before logging. | `0677e19` |
| 3 | `go/log-injection` (CWE-117) | Medium (6.1) | `backend/internal/migration/repository.go:88` | Same as #1, on `migration.lock.released` (`connection_closed`). | **Valid — Fixed.** Sanitized `DatabaseID` via `Logging.SanitizeLogValue` before logging. | `0677e19` |
| 4 | `go/log-injection` (CWE-117) | Medium (6.1) | `backend/internal/migration/repository.go:92` | Same as #1, on `migration.lock.released` (`ok`). | **Valid — Fixed.** Sanitized `DatabaseID` via `Logging.SanitizeLogValue` before logging. | `0677e19` |
| 5 | `go/log-injection` (CWE-117) | Medium (6.1) | `backend/internal/logging/http.go:51` | Raw `R.URL.Path` logged as `route` on unmatched routes; attacker-controlled path could inject CR/LF into log entries. | **Valid — Fixed.** Sanitized `Route` with `SanitizeLogValue(Route)` before `Logger.LogAttrs`. | `0677e19` |
| 6 | `go/log-injection` (CWE-117) | Medium (6.1) | `backend/internal/logging/http.go:107` | Variadic `Args ...any` in `Logging.Record` forwarded user-controlled fields (e.g., `database_id`, `actor`) directly to `slog.Default().Log`. | **Valid — Fixed.** Normalized and sanitized `Event` and every attribute through `SanitizeLogValue` and logged via `slog.Default().LogAttrs`. | `0677e19`, `8fdff64` |

### Remediation & Re-Scan Verification

1. **Commit `0677e19`**: Added `Logging.SanitizeLogValue` in `backend/internal/logging/redact.go` to strip `\r` and `\n`, applied it in `backend/internal/migration/repository.go` and `backend/internal/logging/http.go`, and added unit test coverage in `backend/internal/logging/activity_test.go`. Re-scan reduced findings from **6 → 1** (alerts #1–#5 closed).
2. **Commit `8fdff64`**: Updated `Logging.Record` in `backend/internal/logging/http.go` to convert all arguments into sanitized `slog.Attr` values via `LogAttrs`.
3. **Final Re-Scan Result:**
   - **`go`:** `0` findings (`go.sarif`: 0 findings; all 6 previous alerts closed in GitHub Code Scanning).
   - **`javascript-typescript`:** `0` findings.
   - All backend checks (`make check-fmt`, `make vet`, `go test -race ./...`) pass.

> **Note on SAST Limitations:** A clean CodeQL scan (`0` findings) does not guarantee that the application is completely free of security vulnerabilities. SAST analyzes source code statically against known patterns and data-flow rules, so it cannot replace runtime/dynamic testing (DAST), dependency auditing, authentication/authorization review, or proper deployment hardening.
