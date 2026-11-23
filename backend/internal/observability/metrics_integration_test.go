//go:build integration

package Observability

import (
	Context "context"
	Http "net/http"
	Httptest "net/http/httptest"
	Strings "strings"
	Testing "testing"

	Testutil "github.com/ethj0r/IF3110-K02-01/backend/internal/testutil"
	Migrations "github.com/ethj0r/IF3110-K02-01/backend/migrations"
	Stdlib "github.com/jackc/pgx/v5/stdlib"
	Goose "github.com/pressly/goose/v3"
)

func TestMetricsExposeRetainedJobsAndLivePool(T *Testing.T) {
	Ctx := Context.Background()
	Pool := Testutil.Database(T)
	DB := Stdlib.OpenDB(*Pool.Config().ConnConfig)
	defer DB.Close()
	Provider, Err := Goose.NewProvider(Goose.DialectPostgres, DB, Migrations.FS())
	if Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Provider.Up(Ctx); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Pool.Exec(Ctx, `WITH Target AS (
    INSERT INTO db_connections (name, host, port, dbname, username, password_ciphertext)
    VALUES ('private_target', '127.0.0.1', 5432, 'private_database', 'private_user', '\x01')
    RETURNING id
), Definition AS (
    INSERT INTO migrations (database_id, version, title, sql_text, down_sql, status)
    SELECT id, 1, 'Test observability', 'SELECT 123456789', 'SELECT 0', 'draft' FROM Target
    RETURNING id, database_id, sql_text
)
INSERT INTO migration_executions
    (migration_id, database_id, kind, direction, revision, status, sql_text, up_sql, started_at, finished_at)
SELECT D.id, D.database_id, J.kind, 'up', 1, J.status, D.sql_text, D.sql_text,
    CURRENT_TIMESTAMP - J.duration, J.finished_at
FROM Definition D CROSS JOIN (VALUES
    ('apply', 'queued', NULL::interval, NULL),
    ('apply', 'running', interval '1 second', NULL),
    ('apply', 'succeeded', interval '2 seconds', CURRENT_TIMESTAMP),
    ('retry', 'failed', interval '4 seconds', CURRENT_TIMESTAMP)
) J(kind, status, duration, finished_at)`); Err != nil {
		T.Fatal(Err)
	}
	Conn, Err := Pool.Acquire(Ctx)
	if Err != nil {
		T.Fatal(Err)
	}
	defer Conn.Release()

	Metrics := MetricsHandler{Pool: Pool}
	Result := Httptest.NewRecorder()
	Metrics.ServeHTTP(Result, Httptest.NewRequest(Http.MethodGet, "/api/metrics", nil))
	if Result.Code != Http.StatusOK || Result.Header().Get("Content-Type") != "text/plain; version=0.0.4; charset=utf-8" {
		T.Fatalf("unexpected scrape response: %d %v", Result.Code, Result.Header())
	}
	Body := Result.Body.String()
	for _, Sample := range []string{
		`wbd_migration_jobs{status="queued"} 1`,
		`wbd_migration_jobs{status="running"} 1`,
		`wbd_migration_jobs{status="succeeded"} 1`,
		`wbd_migration_jobs{status="failed"} 1`,
		`wbd_migration_jobs{status="partial"} 0`,
		`wbd_migration_jobs{status="partially_reverted"} 0`,
		"wbd_migration_retry_jobs 1",
		"wbd_migration_execution_mean_seconds 3",
		`wbd_platform_pool_connections{state="active"} 1`,
		`wbd_platform_pool_connections{state="idle"} 1`,
	} {
		if !Strings.Contains(Body, "\n"+Sample+"\n") {
			T.Errorf("missing sample %s", Sample)
		}
	}
	for _, Private := range []string{"private_target", "private_database", "private_user", "SELECT 123456789"} {
		if Strings.Contains(Body, Private) {
			T.Errorf("scrape exposed private fixture data: %s", Private)
		}
	}
}

func TestMetricsReturnsSafeErrorWhenJobsAreUnavailable(T *Testing.T) {
	Metrics := MetricsHandler{Pool: Testutil.Database(T)}
	Result := Httptest.NewRecorder()
	Metrics.ServeHTTP(Result, Httptest.NewRequest(Http.MethodGet, "/api/metrics", nil))
	if Result.Code != Http.StatusServiceUnavailable || !Strings.Contains(Result.Body.String(), `"code":"metrics_unavailable"`) {
		T.Fatalf("unexpected error response: %d %s", Result.Code, Result.Body.String())
	}
	if Strings.Contains(Result.Body.String(), "migration_executions") {
		T.Error("scrape error exposed database details")
	}
}
