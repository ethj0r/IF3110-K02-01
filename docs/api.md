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

`GET /databases/{id}/tables/{table}/rows?schema=public&page=1&page_size=25`
requires group-based view access. The response contains `columns`, `rows`, `page`,
`page_size`, and `total`; page size is limited to 100. Every non-null cell is a
PostgreSQL text string, preserving bigint/numeric precision, UUIDs, arrays, JSON,
and special floating-point values. SQL NULL is JSON `null`; empty text is `""`.

## Database Migrations

Base Route: `/databases/{dbId}/migrations` (alias: `/connections/{dbId}/migrations`)

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

