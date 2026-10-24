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

Responses include `executed_by` and `executed_by_username` when the executor
account still exists, plus `executed_at`, SQL, version, and status. The UI uses
the effective database grant to show mutation controls; backend authorization
remains mandatory. Successful execution invalidates cached viewer metadata/data.

| Method | Path | Access | Request Body | Success Response | Description |
| --- | --- | --- | --- | --- | --- |
| `GET` | `/databases/{dbId}/migrations` | View Only, View + Edit | None | `200` `[...]` | Lists migrations in linear version order |
| `POST` | `/databases/{dbId}/migrations` | View + Edit | `{"title":"...","sql_text":"..."}` | `201` `{...}` | Creates a draft migration with next sequential version |
| `GET` | `/databases/{dbId}/migrations/{id}` | View Only, View + Edit | None | `200` `{...}` | Retrieves migration details and script |
| `PUT` | `/databases/{dbId}/migrations/{id}` | View + Edit | `{"title":"...","sql_text":"..."}` | `200` `{...}` | Updates title or SQL of a draft migration |
| `DELETE` | `/databases/{dbId}/migrations/{id}` | View + Edit | None | `204 No Content` | Deletes a draft migration |
| `POST` | `/databases/{dbId}/migrations/{id}/execute` | View + Edit | None | `200` `{...}` | Executes migration inside a target transaction |

### Migration Statuses

| Status | Terminal | Description |
| --- | --- | --- |
| `draft` | No | Migration created or edited, awaiting execution |
| `pending` | No | Execution started; SQL is immutable and automatic retry is blocked |
| `committed` / `applied` | Yes | Migration executed and committed successfully on target |
| `rolled_back` / `failed` | Yes | SQL failed on target; transaction rolled back and error message recorded |

### Execution Model

1. **Isolation & Atomicity:** SQL runs in an atomic PL/pgSQL `DO` block within the target transaction. Explicit transaction control, including nested `COMMIT`, is rejected by PostgreSQL.
2. **Rollback & Persistence:** Confirmed rollback records a generic error. Raw PostgreSQL errors are neither persisted nor returned, including legacy history responses.
3. **Linear Locking:** A session advisory lock covers creation, draft mutations, execution, and result recording per database. Earlier versions must have a terminal outcome before the next executes; only the latest attempted version may be retried after failure.
4. **Uncertain Outcomes:** `pending` is persisted before SQL runs. A lost commit response, process interruption, or failed result write leaves it pending, blocks retry, and blocks later versions. An operator must verify target effects and reconcile the platform record to `committed` or `rolled_back` before proceeding; never blindly reset it to draft. This also applies to legacy pending records. Target and platform commits are separate transactions.
5. **Timeout:** Execution and lock acquisition share a 30-second deadline; rollback and result recording have a separate five-second cleanup window.

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

