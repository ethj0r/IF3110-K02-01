-include .env
export

.PHONY: help setup db-up db-down db-test-up dev-api dev-web fmt check-fmt vet test test-integration test-web test-e2e lint typecheck check build bootstrap migrate-up migrate-status seed-admin clean

help:
	@echo "setup           Copy local env if absent and install dependencies"
	@echo "db-up/db-down   Start/stop local PostgreSQL services (preserve volumes)"
	@echo "db-test-up      Start disposable integration-test PostgreSQL"
	@echo "dev-api/dev-web Run Go API / Vite (separate terminals)"
	@echo "check           Check formatting, vet, unit tests, lint, and TypeScript"
	@echo "test-integration Run real PostgreSQL tests (requires TEST_DATABASE_URL)"
	@echo "test-e2e        Test running local app in Chromium with temporary fixtures"
	@echo "build           Build frontend and single Go binary into bin/server"
	@echo "bootstrap       Apply platform migrations and create the initial admin"
	@echo "migrate-up/migrate-status Run embedded platform migrations"
	@echo "seed-admin      Bootstrap the initial admin from environment credentials"

setup:
	@umask 077; test -f .env || cp .env.example .env
	@chmod 600 .env
	cd backend && go mod download
	@umask 077; if [ -z "$$SESSION_KEY" ]; then \
		SessionKeyFile=$$(mktemp); \
		trap 'rm -f "$$SessionKeyFile"' EXIT; \
		(cd backend && go run ./cmd/session-key) > "$$SessionKeyFile" || exit 1; \
		printf '\nSESSION_KEY=' >> .env; \
		cat "$$SessionKeyFile" >> .env; \
	fi
	@if ! grep -q '^BOOTSTRAP_ADMIN_USERNAME=' .env; then printf '\nBOOTSTRAP_ADMIN_USERNAME=\n' >> .env; fi
	@if ! grep -q '^BOOTSTRAP_ADMIN_PASSWORD=' .env; then printf 'BOOTSTRAP_ADMIN_PASSWORD=\n' >> .env; fi
	npm --prefix frontend ci --no-audit --no-fund

db-up:
	docker compose up -d --wait platform-db target-db

db-down:
	docker compose --profile test down

db-test-up:
	docker compose --profile test up -d --wait test-db

dev-api:
	cd backend && go run ./cmd/server

dev-web:
	npm --prefix frontend run dev

fmt:
	cd backend && go fmt ./...

check-fmt:
	@test -z "$$(find backend -name '*.go' -not -path '*/vendor/*' -exec gofmt -l {} +)" || (echo "Run make fmt"; exit 1)

vet:
	cd backend && go vet ./...

test:
	cd backend && go test -race ./...

test-integration:
	@test -n "$$TEST_DATABASE_URL" || (echo "Set TEST_DATABASE_URL to a dedicated test database"; exit 1)
	cd backend && go test -race -tags=integration ./...

lint:
	npm --prefix frontend run lint

typecheck:
	npm --prefix frontend run typecheck

test-web:
	npm --prefix frontend test

test-e2e:
	npm --prefix frontend run test:e2e

check: check-fmt vet test test-web lint typecheck

build:
	npm --prefix frontend run build
	mkdir -p bin
	cd backend && go build -trimpath -o ../bin/server ./cmd/server

bootstrap: migrate-up seed-admin

migrate-up:
	cd backend && go run ./cmd/migrate up

migrate-status:
	cd backend && go run ./cmd/migrate status

seed-admin:
	cd backend && go run ./cmd/seed-admin

clean:
	rm -rf bin
