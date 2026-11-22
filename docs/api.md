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
| `POST` | `/databases/{dbId}/migrations` | View + Edit | `{"title":"...","sql_text":"...","down_sql":"...","irreversible":false,"irreversible_reason":""}` | `201` `{...}` | Creates a draft with the next sequential version |
| `GET` | `/databases/{dbId}/migrations/{id}` | View Only, View + Edit | None | `200` `{...}` | Retrieves a definition |
| `PUT` | `/databases/{dbId}/migrations/{id}` | View + Edit | Same as create | `200` `{...}` | Updates a draft and increments its revision |
| `DELETE` | `/databases/{dbId}/migrations/{id}` | View + Edit | None | `204 No Content` | Deletes an unexecuted draft |
| `GET` | `/databases/{dbId}/migrations/history?hide_rollback=false` | View Only, View + Edit | None | `200` `[...]` | Lists executions and attempts; optionally hides successful rollback pairs |
| `POST` | `/databases/{dbId}/migrations/{id}/execute` | View + Edit | None | `200` `{...}` | Applies a draft, or retries its latest failed execution |
| `POST` | `/databases/{dbId}/migrations/{id}/apply` | View + Edit | None | `200` `{...}` | Applies an unexecuted draft |
| `POST` | `/databases/{dbId}/migrations/{id}/rollback` | View + Edit | None | `200` `{...}` | Runs the original down SQL for the last active migration |
| `POST` | `/databases/{dbId}/migrations/{id}/forward` | View + Edit | None | `200` `{...}` | Re-applies up SQL for a rolled-back migration in linear order |
| `POST` | `/databases/{dbId}/migrations/{id}/retry` | View + Edit | None | `200` `{...}` | Retries the latest failed execution using its SQL snapshot |
| `POST` | `/databases/{dbId}/migrations/analyze` | View Only, View + Edit | `{"sql_text":"..."}` | `200` `{...}` | Runs EXPLAIN (ANALYZE) in a target rollback transaction |

Successful actions return an execution object containing the execution ID,
migration/version/revision, `kind` (`apply`, `rollback`, `forward`, `retry`),
`direction` (`up`, `down`), `status`, the exact SQL snapshot, optional
`rollback_of_execution_id` / `retry_of_execution_id`, actor fields, timestamps,
and an `attempts` array. Definitions return derived `status`, `revision`,
`down_sql`, `irreversible`, `locked_at`, and `can_apply` / `can_rollback` /
`can_forward` / `can_retry` hints.

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

### Execution Model

1. **Down SQL and irreversibility:** A reversible definition must provide non-empty `down_sql`. An irreversible definition must omit `down_sql` and provide a non-empty `irreversible_reason`.
2. **Immutable definitions:** The first execution attempt sets `locked_at`. `PUT` and `DELETE` return `409 migration_locked` after that point, including after a failed attempt. SQL snapshots are stored on every execution and attempt.
3. **Linear ordering:** A draft may be applied when every earlier version is applied or failed and no later version is locked. A failed attempt does not advance the active position. A pending/running execution blocks later actions.
4. **Rollback:** Only the highest applied version may be rolled back, and only when its latest successful execution is an up execution and the definition is reversible. Rollback runs the original down SQL snapshot as a new execution and records `rollback_of_execution_id`.
5. **Forward:** Forward is allowed only for a rolled-back migration that is the next version after the highest applied version. It runs the original up SQL as a new execution; it never automatically runs the next migration.
6. **Retry:** Retry is allowed only for the latest failed execution on the database and repeats that execution's SQL snapshot and direction.
7. **History integrity:** Executions and attempts are never overwritten or deleted. Failures remain visible. `hide_rollback=true` hides successful rollback executions together with the execution they cancelled, paired by execution ID. A failed rollback changes neither the hidden set nor the active position.
8. **Atomicity:** SQL runs in an atomic PL/pgSQL `DO` block within the target transaction. Explicit transaction control, including nested `COMMIT`, is rejected by PostgreSQL.
9. **Safe errors:** Confirmed SQL failures record a generic error in the platform. Raw PostgreSQL errors and credentials are not persisted or returned by the API.
10. **Uncertain outcomes:** A lost commit response, rollback failure, or failed result write leaves the execution running/pending. Retry and later actions are blocked until the target outcome is verified and reconciled.
11. **Logging:** Every attempt logs structured JSON with actor, database, migration, execution, version/revision, kind/direction, attempt number, SQL, status, and error category. Application logs remain separate from migration history.
12. **Timeout:** Execution and lock acquisition share a 30-second deadline; rollback and result recording use a separate five-second cleanup window. Queue-based asynchronous execution replaces this synchronous path in the async milestone.

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
| GET | `/admin/groups/{id}/members` | List group members |
| PUT, DELETE | `/admin/groups/{id}/members/{userId}` | Add / remove member |
| GET | `/admin/groups/{id}/access` | List group database grants |
| PUT, DELETE | `/admin/groups/{id}/access/{databaseId}` | Set `{level:"view_only"\|"view_edit"}` / remove grant |
| GET | `/admin/access` | Group x database access matrix |

Users inherit the highest grant across their groups. Membership/grant increases
trigger session rotation; removal or reduction revokes affected sessions.

