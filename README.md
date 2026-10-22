# IF3110-K02-01

A PostgreSQL database management and migration platform built with Go and React.

## Status

The repository includes a Go server, configuration loading, PostgreSQL pooling, JSON response helpers, health checks, a goose migration runner, isolated integration tests, a React starter, an embedded frontend build, Docker Compose, Make targets, and CI.

Authentication, authorization, administration, database inspection, and target database migrations are not implemented yet. The frontend currently displays a placeholder, and the application schema has not been created.

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
make migrate-up
```

`make setup` copies `.env.example` to `.env` if the file does not exist and installs dependencies using the lockfiles. Make loads `.env`; direct binary or `go run` commands require exported environment variables. Use simple Make-compatible assignments as shown in the example file.

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

`db-down` preserves development database volumes. The test database uses temporary storage. Integration tests require `TEST_DATABASE_URL` pointing to a dedicated test database; they never fall back to `DATABASE_URL`. Each test receives an isolated schema that is removed during cleanup.

## Validation

```sh
make check
make test-integration
```

`check` runs formatting checks, `go vet`, unit tests with the race detector, ESLint, and TypeScript. Integration tests use the `integration` build tag and a real PostgreSQL database. CI runs these checks and builds the application.

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

The runner reports when no migration files exist. The server does not apply migrations automatically; run `migrate-up` explicitly before using features that require their schema.
