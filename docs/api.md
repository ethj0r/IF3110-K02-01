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
| `draft` / `pending` | No | Migration created or edited, awaiting execution |
| `committed` / `applied` | Yes | Migration executed and committed successfully on target |
| `rolled_back` / `failed` | Yes | SQL failed on target; transaction rolled back and error message recorded |

### Execution Model

1. **Isolation & Atomicity:** The migration SQL executes in a dedicated PostgreSQL transaction on the target database (`BEGIN` -> `Exec` -> `COMMIT`).
2. **Rollback & Persistence:** If execution encounters any SQL error, target transaction rolls back (`ROLLBACK`) leaving target data untouched. Failure status and error details are stored in the platform database.
3. **Linear Locking:** Sequential versioning is enforced using transaction-level advisory locks (`pg_advisory_xact_lock`) serialized per target database ID to prevent race conditions during concurrent creation.

