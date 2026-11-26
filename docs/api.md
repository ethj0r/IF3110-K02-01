# API Specification

Base Path: `/api`  
Error Format: `{"error":{"code":"...","message":"..."}}`  
Authentication: Session cookie (HttpOnly)  
CSRF Protection: Unsafe methods require `X-CSRF-Token` header and trusted `Origin`.

## Authentication

| Method | Path | Access | Request Body | Success Response | Description |
| --- | --- | --- | --- | --- | --- |
| `POST` | `/auth/login` | Public | `{"username":"...","password":"..."}` | `200` `{"user":{...},"csrf_token":"...","expires_at":"..."}` | Authenticates user credentials |
| `GET` | `/auth/me` | Authenticated | None | `200` `{"user":{...},"csrf_token":"...","expires_at":"..."}` | Returns current user session state |
| `GET` | `/auth/csrf` | Authenticated | None | `200` `{"csrf_token":"..."}` | Returns fresh CSRF token |
| `POST` | `/auth/logout` | Authenticated | None | `204 No Content` | Revokes current session and cookies |

## Database Connections

| Method | Path | Access | Request Body | Success Response | Description |
| --- | --- | --- | --- | --- | --- |
| `GET` | `/connections` | Platform Admin | None | `200` `[...]` | Lists all configured target connections |
| `POST` | `/connections` | Platform Admin | `{"name":"...","host":"...","port":5432,"dbname":"...","username":"...","password":"..."}` | `201` `{...}` | Creates a target database connection |
| `GET` | `/connections/{id}` | Platform Admin | None | `200` `{...}` | Retrieves connection metadata |
| `PUT` | `/connections/{id}` | Platform Admin | `{"name":"...","host":"...","port":5432,"dbname":"...","username":"...","password":"..."}` | `200` `{...}` | Updates target database connection |
| `DELETE` | `/connections/{id}` | Platform Admin | None | `204 No Content` | Removes a database connection |
| `POST` | `/connections/{id}/test` | Platform Admin | None | `200` `{"status":"ok"}` | Tests connection to the target database |

## Database Inspection

| Method | Path | Response | Access |
| --- | --- | --- | --- |
| GET | `/databases` | `[{id,name,host,dbname,level}]` | Databases granted through the user's groups |
| GET | `/databases/{id}` | `{id,name,host,dbname,level}` | Group-based view access |
| GET | `/databases/{id}/schemas` | `{schemas:[string]}` | Group-based view access |
| GET | `/databases/{id}/tables?schema=public` | `{tables:[{name,type}]}` | Group-based view access |
| GET | `/databases/{id}/tables/{table}/columns?schema=public` | `{columns:[{name,data_type,nullable,default}]}` | Group-based view access |
| GET | `/databases/{id}/tables/{table}/constraints?schema=public` | `{constraints:[{name,type,columns,definition}]}` | Group-based view access |
| GET | `/databases/{id}/tables/{table}/indexes?schema=public` | `{indexes:[{name,definition}]}` | Group-based view access |
| GET | `/databases/{id}/tables/{table}/rows` | `{columns,rows,page,page_size,total}` | Group-based view access |

Platform administrators also need a group grant to browse a database. Missing
grants produce 403 on inspection routes; inaccessible database detail produces
404, and inaccessible databases are omitted from the listing. Target failures
produce sanitized 502 `connection_unavailable`. Invalid identifiers or pagination
produce 400. Malformed connection IDs produce 404 on connection endpoints.

`GET /databases/{id}/tables/{table}/rows?schema=public&page=1&page_size=25`
requires group-based view access. The response contains `columns`, `rows`, `page`,
`page_size`, and `total`; page size is limited to 100. Every non-null cell is a
PostgreSQL text string, preserving bigint/numeric precision, UUIDs, arrays, JSON,
and special floating-point values. SQL NULL is JSON `null`; empty text is `""`.

Object names are literal UTF-8 identifiers without NUL, at most 63 bytes. The
client URL-encodes names; the API binds catalog filters and quotes SQL identifiers.
The same inspection endpoints also accept `/databases/{id}/table/{endpoint}` with
`table` and `schema` query parameters. The client uses this form for literal `.`
and `..` table names, which browsers otherwise normalize as URL path segments.
For example: `/databases/{id}/table/rows?schema=public&table=..&page=1&page_size=25`.

Count and rows use one read-only repeatable-read transaction per request. Ordering
uses primary-key columns plus full-row text, or full-row text alone without a key.
Each page request has a fresh snapshot; concurrent changes between requests can
still move rows between pages. Overflowing offsets are rejected with 400.
Inspection requests apply a ten-second query budget; timeout errors use the same
sanitized 502 response as other target failures. Column `data_type` includes type
modifiers such as `character varying(12)` and `numeric(12,2)`; constraint
`definition` includes CHECK expressions and foreign-key targets.

## Database Migrations

Base Route: `/databases/{dbId}/migrations` (alias: `/connections/{dbId}/migrations`)

Each migration definition contains immutable up SQL and either immutable down SQL
or an `irreversible` flag with a reason. Definitions are editable only while they
are drafts and have never been submitted for execution. The first execution locks
the definition (`locked_at`); later edits must be new migrations.

| Method | Path | Access | Request Body | Success Response | Description |
| --- | --- | --- | --- | --- | --- |
| `GET` | `/databases/{dbId}/migrations` | View Only, View + Edit | None | `200` `[...]` | Lists definitions with derived status and action capabilities |
| `POST` | `/databases/{dbId}/migrations` | View + Edit | `{"title":"...","sql_text":"...","down_sql":"...","batch_mode":false,"batch_column":"","batch_size":0,"irreversible":false,"irreversible_reason":""}` | `201` `{...}` | Creates a draft with the next sequential version |
| `GET` | `/databases/{dbId}/migrations/{id}` | View Only, View + Edit | None | `200` `{...}` | Retrieves a definition |
| `PUT` | `/databases/{dbId}/migrations/{id}` | View + Edit | Same as create | `200` `{...}` | Updates a draft and increments its revision |
| `DELETE` | `/databases/{dbId}/migrations/{id}` | View + Edit | None | `204 No Content` | Deletes an unexecuted draft |
| `GET` | `/databases/{dbId}/migrations/history?hide_rollback=false` | View Only, View + Edit | None | `200` `[...]` | Lists executions and attempts; optionally hides successful rollback pairs |
| `GET` | `/databases/{dbId}/jobs?limit=10` | View Only, View + Edit | None | `200` `[{...}]` | Latest jobs of the database, newest first; `limit` 1–50 (default 10), otherwise `400` |
| `GET` | `/databases/{dbId}/jobs/{jobId}` | View Only, View + Edit | None | `200` `{...}` | Polls a queued/running/finished migration job |
| `POST` | `/databases/{dbId}/migrations/{id}/execute` | View + Edit | None | `202` job | Enqueues apply or retry |
| `POST` | `/databases/{dbId}/migrations/{id}/apply` | View + Edit | None | `202` job | Enqueues an apply |
| `POST` | `/databases/{dbId}/migrations/{id}/rollback` | View + Edit | None | `202` job | Enqueues a rollback |
| `POST` | `/databases/{dbId}/migrations/{id}/forward` | View + Edit | None | `202` job | Enqueues a forward |
| `POST` | `/databases/{dbId}/migrations/{id}/retry` | View + Edit | None | `202` job | Enqueues a retry |
| `GET` | `/databases/{dbId}/migrations/{id}/executions/{executionId}/evidence` | View Only, View + Edit | None | `200` `[...]` | Lists target-side execution evidence |
| `POST` | `/databases/{dbId}/migrations/{id}/executions/{executionId}/reconcile` | Platform Admin + View + Edit | `{"outcome":"succeeded"\|"failed","note":"..."}` | `200` `{...}` | Reconciles a queued/running execution after target verification |
| `POST` | `/databases/{dbId}/migrations/analyze` | View Only, View + Edit | `{"sql_text":"..."}` | `200` `{...}` | Runs EXPLAIN (ANALYZE) in a target rollback transaction |

Action endpoints enqueue a migration job and return it immediately. A job contains
`id`, `action`, `status` (`queued`, `running`, `succeeded`, `failed`, `cancelled`),
attempt counters, lease metadata, optional `execution_id`, `last_error`, actor
fields, and timestamps. The worker revalidates the action under a per-database lock,
runs it, and fills `execution_id`. History executions contain the execution ID,
migration/version/revision, `kind` (`apply`, `rollback`, `forward`, `retry`),
`direction` (`up`, `down`), `status`, the exact SQL snapshot, optional
`rollback_of_execution_id` / `retry_of_execution_id`, actor fields, timestamps,
and an `attempts` array. Batch executions also expose `mode`, `batch_column`,
`batch_size`, `committed_batches`, `committed_rows`, `reverted_batches`,
`reverted_rows`, and `checkpoint_cursor`. Definitions return derived `status`,
`revision`, `down_sql`, `batch_mode`, `batch_column`, `batch_size`,
`irreversible`, `locked_at`, and `can_apply` / `can_rollback` / `can_forward` /
`can_retry` hints.

### Migration Statuses

| Status | Terminal | Description |
| --- | --- | --- |
| `draft` | No | Definition created or edited and never executed |
| `pending` | No | Latest execution is queued/running or its outcome requires verification |
| `applied` | Yes | Latest successful execution used up SQL |
| `rolled_back` | Yes | Latest successful execution used down SQL |
| `failed` | Yes | No successful execution exists and the latest attempt failed |

Execution statuses are `queued`, `running`, `succeeded`, `failed`, `partial`, and
`partially_reverted`. Attempt statuses are `running`, `succeeded`, and `failed`.
Migration definitions surface `partial` and `partially_reverted` while a batch
execution is incomplete.

### Batch Mode

A batch definition sets `batch_mode: true`, a cursor `batch_column`, and a positive
`batch_size`. `sql_text` must be one statement that uses `$1` as the last cursor and
`$2` as the batch size, and must return the cursor column. `down_sql` must be one
statement that uses `$1` as `cursor_to` and `$2` as `cursor_from` to compensate one
committed batch.

The worker executes one target transaction per batch, commits it, and records the
batch scope (`cursor_from`, `cursor_to`, `row_count`) plus the execution checkpoint.
Previous batches stay committed when a later batch fails; the execution becomes
`partial`. Retry resumes the same execution from the checkpoint without repeating
committed batches. Rollback compensates committed batches in reverse order; a
partial failure leaves `partially_reverted` and can be resumed. The target database
receives a `platform_migration.batches` evidence row in the same transaction as each
batch, allowing the worker to reconcile a commit whose platform record was lost.

### Execution Model

1. **Down SQL and irreversibility:** A reversible definition must provide non-empty `down_sql`. An irreversible definition must omit `down_sql` and provide a non-empty `irreversible_reason`.
2. **Immutable definitions:** The first execution attempt sets `locked_at`. `PUT` and `DELETE` return `409 migration_locked` after that point, including after a failed attempt. SQL snapshots are stored on every execution and attempt.
3. **Linear ordering:** A draft may be applied when every earlier version is applied or failed and no later version is locked. A failed attempt does not advance the active position. A pending/running execution blocks later actions.
4. **Rollback:** Only the highest applied version may be rolled back, and only when its latest successful execution is an up execution and the definition is reversible. Rollback runs the original down SQL snapshot as a new execution and records `rollback_of_execution_id`.
5. **Forward:** Forward is allowed only for a rolled-back migration that is the next version after the highest applied version. It runs the original up SQL as a new execution; it never automatically runs the next migration.
6. **Retry:** Retry is allowed only for the latest failed execution on the database and repeats that execution's SQL snapshot and direction.
7. **History integrity:** Executions and attempts are never overwritten or deleted. Failures remain visible. `hide_rollback=true` hides successful rollback executions together with the execution they cancelled, paired by execution ID. A failed rollback changes neither the hidden set nor the active position.
8. **Job progress:** A job response carries `execution_id`, `execution_status`, and the execution's committed/reverted batch and row counters plus `checkpoint_cursor`. While a job is running those fields come from the migration's in-flight execution, so batch progress is visible before the job finishes; `execution_status` reports `partial` or `partially_reverted` after an incomplete batch run. The SPA reads the job list to restore the active job after navigation or reload.
9. **Queue and worker:** Actions are stored in `migration_jobs` and claimed with `FOR UPDATE SKIP LOCKED`. A partial unique index allows one queued/running job per database, while different databases run in parallel. A session advisory lock protects execution across workers and remains held between batch commits. Jobs, executions, and batch checkpoints survive restart.
10. **Leases, retries, and duplicate delivery:** A claimed job has a lease. Transient store/target failures are requeued with exponential backoff while attempts remain below `max_attempts`. Expired running jobs are marked failed as uncertain instead of being blindly retried. Duplicate delivery is safe because a second run sees the existing execution and does not commit again. The per-database advisory lock is released when the worker's platform connection closes; the pending execution continues to block later actions until an operator reconciles it.
11. **Atomicity:** SQL runs in an atomic PL/pgSQL `DO` block within the target transaction. Explicit transaction control, including nested `COMMIT`, is rejected by PostgreSQL.
12. **Safe errors:** Confirmed SQL failures record a generic error in the platform. Raw PostgreSQL errors and credentials are not persisted or returned by the API.
13. **Uncertain outcomes:** A lost commit response, rollback failure, or failed result write leaves the execution running/pending. Retry and later actions are blocked until the target outcome is verified and reconciled.
14. **Logging:** Every attempt logs structured JSON with actor, database, migration, execution, version/revision, kind/direction, attempt number, SQL, status, and error category. Application logs remain separate from migration history.
15. **Timeout:** Worker executions use `WORKER_EXECUTION_TIMEOUT` (default `30m`), independent of HTTP timeouts. The worker renews its job lease while executing so long jobs are not expired by another worker. A statement cancelled by its own deadline is rolled back and recorded as a confirmed timeout failure; rollback and result recording use separate five-second cleanup windows.

### Reconciliation

Normal and batch executions write a target-side evidence row inside the same target
transaction as the change (`platform_migration.batches`, `batch_no=0` for normal
mode). If a commit result is lost, a worker dies, or the lease expires, the
execution remains `running` and blocks later actions. Expiring a stale job links it
to that execution.

`POST .../executions/{executionId}/reconcile` requires a platform administrator who
also has View + Edit on the database. Body: `{"outcome":"succeeded"|"failed","note":"..."}`
(note up to 500 characters, unknown fields rejected).

| Response | When |
| --- | --- |
| `200` execution | Settled. Normal mode becomes `succeeded`/`failed`; batch mode stops at its checkpoint (`partial`, `partially_reverted`, or `failed` when nothing committed). |
| `400 invalid_request` | Unknown outcome, note too long, or malformed body. |
| `403 forbidden` | Not a platform administrator, or no View + Edit grant on the database. |
| `404 execution_not_found` | The execution does not belong to this database and migration. |
| `409 reconcile_not_allowed` | The execution is not `queued` or `running`. |
| `409 evidence_mismatch` | Normal mode: the outcome contradicts the target evidence (row present means committed). |
| `409 batch_reconcile` | Batch mode with `outcome=succeeded`; resume with a retry instead. |
| `409 database_busy` | A worker still holds the database lock or a job lease is still live. |

Reconciliation takes the advisory lock with `pg_try_advisory_xact_lock`, so it never
waits behind a running migration. It closes open attempts, settles the execution,
stamps the latest job of the migration (`reconciled_at`, `reconciled_by`) without
rewriting the job status, refreshes the migration status, and records the actor and
note. It never runs SQL on the target and never rewrites SQL snapshots.

`GET .../executions/{executionId}/evidence` (View Only or View + Edit) lists the
evidence rows of an execution; a target without the evidence table returns `[]`.

### Migration Review

For `/databases/{dbId}/migrations/{id}/reviews/{action}` (also under `/connections`),
`action` is `apply`, `rollback`, `forward`, or `retry`.

| Method and suffix | Access | Response |
| --- | --- | --- |
| `GET` | View Only or View + Edit | `200` with current review, or `404` before submission |
| `POST` | View + Edit | `200` with the submitted review or the existing one for the same revision and action |
| `PUT /{reviewId}/approvals/{groupId}` | The active leader of that group | `204`, or `404` if the review is stale |

A review includes the proposed `sql_text`, `revision`, `action`, `status`, and `groups`.
Each group includes `approved` and `can_approve` for the signed-in user. Status is
`approved` when every group with current database access has approved through its
current active leader. Groups without an active leader keep the review pending.
Editing a draft or recording a new execution makes previous reviews stale.
Review requests and approvals do not execute SQL. Starting an action through
`/execute`, `/apply`, `/rollback`, `/forward`, or `/retry` requires a current,
fully approved review for that action. An otherwise eligible action with missing
approvals returns `409 approval_required`.

## Schema Comparison

`POST /databases/{dbId}/schema-diff` compares the current database with a registered reference database. Requires a session, trusted Origin, CSRF token, View + Edit on `{dbId}`, and at least View Only on `reference_database_id`. Both permissions are checked before resolving either connection. Platform administrators do not bypass grants.

Request (`application/json`, maximum 2 KiB, unknown fields rejected):

```json
{"reference_database_id":"reference-connection-uuid","schema":"public"}
```

`schema` defaults to `public` when omitted or empty. Its literal UTF-8 name is limited to 63 bytes without NUL; `pg_*` and `information_schema` are rejected. The reference schema must exist. The same namespace is compared in both databases; an absent current schema generates creation statements.

Success (`200`, `Cache-Control: no-store`):

```json
{
  "current_database_id": "current-connection-uuid",
  "reference_database_id": "reference-connection-uuid",
  "schema": "public",
  "current_schema_hash": "catalog-hash",
  "requires_recomparison": false,
  "statements": [
    {
      "sql": "ALTER TABLE \"public\".\"items\" ADD COLUMN \"enabled\" boolean;",
      "hazards": [{"type":"ACQUIRES_ACCESS_EXCLUSIVE_LOCK","message":"Reported generator hazard"}]
    }
  ],
  "sql_text": "ALTER TABLE \"public\".\"items\" ADD COLUMN \"enabled\" boolean;"
}
```

`statements` contains ordered, complete DDL statements and generator hazards. `sql_text` is the editable migration candidate. Normally it joins all statements. If existing enum values must be added alongside other changes, `requires_recomparison` is `true`: the full `statements` list still describes the differences, but `sql_text` contains only enum additions. Commit this candidate, compare again, then generate the remaining migration. No transaction control is inserted into the script. Equal schemas return `statements: []` and `sql_text: ""`. This endpoint never creates a draft, executes DDL, or copies data. Both catalog snapshots use read-only repeatable-read transactions; no shadow database or plan rehearsal is used. Index operations are generated without `CONCURRENTLY` for the existing transactional executor. The returned hash identifies the captured current schema; execution does not enforce it, so compare again after intervening changes.

Errors use the standard sanitized JSON envelope: `400 invalid_request` (including missing reference schema), `401 unauthenticated`, `403 forbidden` or CSRF rejection, `404 not_found`, `413 schema_diff_too_large`, `415 unsupported_media_type`, `422 schema_diff_unsupported`, or `502 schema_diff_unavailable`. Generated SQL must fit 48,000 raw bytes and 60,000 bytes when JSON encoded, leaving space in the 64 KiB draft request. Comparison shares a 20-second deadline, with a 15-second database statement timeout. Unsupported library changes must be authored manually; object renames may produce destructive drop/create candidates. Snapshots containing non-extension user-defined domains, standalone composites, ranges/multiranges, or base types return `422` rather than a false empty diff. Removing or reordering existing enum labels also returns `422` instead of an invalid or false empty candidate.

## Administration

Every route below requires a platform administrator. Unsafe methods also require
the trusted Origin and CSRF header. Errors follow the common JSON envelope.

| Methods | Path | Purpose / request |
| --- | --- | --- |
| GET, POST | `/admin/users` | List users / create ordinary user `{username,password}` |
| GET, PATCH, DELETE | `/admin/users/{id}` | Read / set `{is_active}` / delete user |
| PUT | `/admin/users/{id}/password` | Reset `{password}` and revoke sessions |
| GET, POST | `/admin/groups` | List / create `{name,description}` |
| GET, PATCH, DELETE | `/admin/groups/{id}` | Read / edit `{name,description}` / delete group |
| PUT | `/admin/groups/{id}/leader` | Assign an active group member with `{user_id}` (204) |
| GET | `/admin/groups/{id}/members` | List group members |
| PUT, DELETE | `/admin/groups/{id}/members/{userId}` | Add / remove member |
| GET | `/admin/groups/{id}/access` | List group database grants |
| PUT, DELETE | `/admin/groups/{id}/access/{databaseId}` | Set `{level:"view_only"\|"view_edit"}` / remove grant |
| GET | `/admin/access` | Group x database access matrix |

Users inherit the highest grant across their groups. Membership/grant increases
trigger session rotation; removal or reduction revokes affected sessions.

Group responses include `leader_user_id` when a leader is assigned. Assigning
another leader replaces the current assignment. Removing the leader's membership
clears the assignment, so the administrator can select another active member.

