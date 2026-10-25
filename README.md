# IF3110-K02-01

A PostgreSQL database management and migration platform built with Go and React.

## Status

The repository includes a Go server, configuration loading, PostgreSQL pooling, JSON response helpers, health checks, a goose migration runner, isolated integration tests, a React starter, an embedded frontend build, Docker Compose, Make targets, and CI.

Authentication, session management, CSRF protection, administrator bootstrap, group-based database access, administration, encrypted target connection management, schema/data inspection, transactional target migrations, and schema comparison with editable migration generation are available.

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
```

`make setup` copies `.env.example` to `.env` if the file does not exist, generates a local session encryption key when absent, adds placeholder bootstrap administrator variables when missing, restricts `.env` permissions, and installs dependencies using the lockfiles. `make bootstrap` applies platform migrations and creates the initial administrator. Make loads `.env`; direct binary or `go run` commands require exported environment variables. Use simple Make-compatible assignments as shown in the example file.

### Run the Application

Start the API and frontend in separate terminals:

```sh
make dev-api
```

```sh
make dev-web
```

Open `http://127.0.0.1:5173`. Vite proxies `/api` to `HTTP_ADDR`, which defaults to `127.0.0.1:8080`, keeping browser requests on the same origin.

Client environment variables use the `VITE_` prefix. Never use that prefix for secrets.

### Health Checks

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/health` | Process liveness |
| GET | `/api/ready` | Platform database readiness |

API errors use `{"error":{"code":"...","message":"..."}}`. Error messages must not contain raw database errors or credentials.

## Local Databases

| Service | Host Port | Database | Purpose |
| --- | --- | --- | --- |
| `platform-db` | 55432 | `platform` | Platform metadata |
| `target-db` | 55433 | `target` | Target database inspection and migrations |
| `test-db` | 55434 | `platform_test` | Disposable integration tests |

Database ports bind to loopback only. Credentials in Compose and `.env.example` are for local development. Do not use them in production or commit `.env`.

```sh
make db-test-up
make test-integration
make db-down
```

`db-down` preserves development database volumes. The test database uses temporary storage. Integration tests require `TEST_DATABASE_URL` pointing to a dedicated test database; they never fall back to `DATABASE_URL`. Each test receives an isolated schema that is removed during cleanup. Schema comparison tests also create two temporary databases on this dedicated test instance and drop them afterwards; its test role needs `CREATEDB`.

The test database user must also be able to create and drop temporary roles for SELECT-only target inspection tests. The Compose test user has these privileges.

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

The browser suite uses `APP_ORIGIN`, `TARGET_DATABASE_URL`, `TEST_DATABASE_URL`, and the bootstrap administrator credentials loaded by Make. It requires the existing administrator's actual password, a local development application, and two distinct local PostgreSQL databases. Run `make db-test-up` before the schema comparison test. It creates temporary users, groups, connections, and uniquely named schemas, then removes them. Existing target tables are not used. If a migration outcome remains uncertain, the fixture is preserved for reconciliation instead of being retried or deleted.

Chromium exercises administrator user/group/membership/grant forms, real login, group-based access, the SQL editor, commit, rollback, history, inspection of quoted names, session revocation, logout, and schema comparison through editable generated SQL and execution. Traces and video are disabled; screenshots of the viewer and schema comparison are saved under the ignored `frontend/test-results/` directory. These tests run separately from `make check` and do not replace backend integration tests.

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

On server startup, a non-empty `TARGET_DATABASE_URL` seeds `local-target` only when the connection table is empty. Seeding grants no access automatically. Restarting with an existing connection does not overwrite it; manage it through the administration page.

Rows default to 25 per page, with a maximum of 100. Cells use PostgreSQL text representations to preserve numeric precision; SQL NULL remains distinct from empty text. Schema/table names are literal UTF-8 PostgreSQL identifiers, limited to 63 bytes without NUL. Spaces, Unicode, punctuation, embedded quotes, slash, and percent characters are supported. Values are bound as query parameters or quoted with `pgx.Identifier`; SQL-like text in a name remains a literal name. The frontend URL-encodes names and the API preserves their exact spelling.

Rows are ordered by primary key, with full-row text as a tie-breaker or fallback for tables/views without a key. Count and data share one read-only repeatable-read snapshot per request; a new page request sees a fresh snapshot. Inspection queries have a ten-second request budget and connection setup a five-second timeout. Column types include length/precision, and constraints include their PostgreSQL definitions.

Target connections currently use `sslmode=disable`; URL TLS options are not retained by the connection store. Use the local Compose target or an appropriately protected database network. Targets requiring TLS need additional connection configuration support.

Deleting a connection also deletes its platform grants and migration history; it does not drop the target database. Verification commands are `make check`, `make db-test-up`, `make test-integration`, and `make build`.

## Target Database Migrations

Target database migrations represent ordered, linear schema and data modifications executed on managed target PostgreSQL databases.

### Endpoints

| Method | Path | Required Access |
| --- | --- | --- |
| `GET` | `/api/databases/{dbId}/migrations` | View Only / View + Edit |
| `POST` | `/api/databases/{dbId}/migrations` | View + Edit |
| `GET` | `/api/databases/{dbId}/migrations/{id}` | View Only / View + Edit |
| `PUT` | `/api/databases/{dbId}/migrations/{id}` | View + Edit |
| `DELETE` | `/api/databases/{dbId}/migrations/{id}` | View + Edit |
| `POST` | `/api/databases/{dbId}/migrations/{id}/execute` | View + Edit |

Migration mutations and execution are serialized per target database using session advisory locks. SQL runs in an atomic block inside a target transaction; confirmed rollback records a generic error. The platform persists `pending` before execution. If commit or result recording is uncertain, it keeps that status and blocks retry until the target outcome is verified and the record reconciled. See the execution model in `docs/api.md` for version ordering and recovery rules.

See [`docs/api.md`](docs/api.md) for detailed request, response, and error specifications.

## Schema Comparison and Migration Generation

1. Register the current PostgreSQL database and a reference database containing the desired schema. The namespace must have the same name in both databases, such as `public`; it may be absent from the current database.
2. Grant the user **View + Edit** on the current database and at least **View Only** on the reference database. Platform administrators also need these group grants.
3. Open the current database and choose **Compare schemas**. Select the reference database and schema, then compare.
4. Review the ordered SQL statements and their reported hazards. Choose **Use SQL in migration editor** to review or change the generated SQL and title.
5. Save the draft and execute it explicitly using the existing migration workflow. Run comparison again after execution to check the result.

The comparison direction is **current → reference**. Generation reads catalog snapshots in read-only transactions, does not compare/copy row data, and does not save or execute a migration. It uses [pg-schema-diff v1.0.9](https://github.com/stripe/pg-schema-diff/tree/v1.0.9) for dependency ordering and PostgreSQL DDL generation. Index operations use ordinary transactional SQL to match the migration executor. No shadow database is created and the candidate is not rehearsed against live data before review.

Object renames are treated as removal and creation; review destructive statements carefully. Unsupported generator changes return an error so the migration can be written manually. Role creation and general non-enum type migrations are outside the generator's supported scope. Cross-schema dependencies must already be available on the current database. Comparison has a 20-second deadline and a generated SQL limit of 48,000 bytes, with a separate JSON size limit to fit migration drafts. Changes made to either database after comparison require a fresh comparison.

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
