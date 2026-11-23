//go:build integration

package Observability

import (
	Context "context"
	Http "net/http"
	Httptest "net/http/httptest"
	Strconv "strconv"
	Strings "strings"
	Testing "testing"
	Time "time"

	Migration "github.com/ethj0r/IF3110-K02-01/backend/internal/migration"
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
	Metrics := MetricsHandler{Pool: Pool}
	Empty := Httptest.NewRecorder()
	Metrics.ServeHTTP(Empty, Httptest.NewRequest(Http.MethodGet, "/api/metrics", nil))
	if Empty.Code != Http.StatusOK || Strings.Contains(Empty.Body.String(), "wbd_migration_queue_wait_mean_seconds") {
		T.Fatalf("unexpected scrape before a job has recorded a wait: %d %s", Empty.Code, Empty.Body.String())
	}
	if _, Err := Pool.Exec(Ctx, `WITH Targets AS (
    INSERT INTO db_connections (name, host, port, dbname, username, password_ciphertext)
    VALUES ('private_target', '127.0.0.1', 5432, 'private_database', 'private_user', '\x01'),
           ('running_target', '127.0.0.1', 5432, 'private_database', 'private_user', '\x01')
    RETURNING id, name
), Definitions AS (
    INSERT INTO migrations (database_id, version, title, sql_text, down_sql, status)
    SELECT id, 1, 'Test observability', 'SELECT 123456789', 'SELECT 0', 'draft' FROM Targets
    RETURNING id, database_id, sql_text
), Executions AS (
    INSERT INTO migration_executions
        (migration_id, database_id, kind, direction, revision, status, sql_text, up_sql, started_at, finished_at)
    SELECT D.id, D.database_id, E.kind, 'up', 1, E.status, D.sql_text, D.sql_text,
           CURRENT_TIMESTAMP - E.duration, CURRENT_TIMESTAMP
    FROM Definitions D JOIN Targets T ON T.id = D.database_id
    CROSS JOIN (VALUES ('apply', 'succeeded', interval '2 seconds'), ('retry', 'failed', interval '4 seconds'))
        AS E(kind, status, duration)
    WHERE T.name = 'running_target'
    RETURNING id, database_id, status
), Batches AS (
    INSERT INTO migration_batches (execution_id, database_id, batch_no, cursor_from, cursor_to, row_count, status)
    SELECT E.id, E.database_id, B.n, B.first, B.last, B.rows, B.status
    FROM Executions E
    CROSS JOIN (VALUES (1, 0, 10, 10, 'committed'), (2, 10, 15, 5, 'reverted')) AS B(n, first, last, rows, status)
    WHERE E.status = 'succeeded'
)
INSERT INTO migration_jobs (database_id, migration_id, action, status, attempts, created_at, available_at, locked_at)
SELECT D.database_id, D.id, J.action, J.status, J.attempts, CURRENT_TIMESTAMP - interval '1 hour',
       CURRENT_TIMESTAMP - interval '5 seconds', J.locked_at
FROM Definitions D JOIN Targets T ON T.id = D.database_id
JOIN (VALUES
    ('private_target', 'apply', 'queued', 0, NULL),
    ('running_target', 'apply', 'running', 1, CURRENT_TIMESTAMP),
    ('running_target', 'apply', 'succeeded', 1, CURRENT_TIMESTAMP),
    ('running_target', 'retry', 'failed', 3, CURRENT_TIMESTAMP)
) J(target, action, status, attempts, locked_at) ON J.target = T.name`); Err != nil {
		T.Fatal(Err)
	}
	Conn, Err := Pool.Acquire(Ctx)
	if Err != nil {
		T.Fatal(Err)
	}
	defer Conn.Release()

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
		`wbd_migration_jobs{status="cancelled"} 0`,
		"wbd_migration_retry_jobs 1",
		"wbd_migration_retry_attempts 2",
		"wbd_migration_execution_mean_seconds 3",
		"wbd_migration_queue_wait_mean_seconds 5",
		`wbd_migration_batches_processed{direction="up"} 2`,
		`wbd_migration_batches_processed{direction="down"} 1`,
		`wbd_migration_rows_processed{direction="up"} 15`,
		`wbd_migration_rows_processed{direction="down"} 5`,
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

	// Hold a real advisory lock and let the worker's lock query wait in PostgreSQL.
	var DatabaseID string
	if Err := Pool.QueryRow(Ctx, "SELECT id::text FROM db_connections WHERE name = 'running_target'").Scan(&DatabaseID); Err != nil {
		T.Fatal(Err)
	}
	Tx, Err := Conn.Begin(Ctx)
	if Err != nil {
		T.Fatal(Err)
	}
	defer Tx.Rollback(Ctx)
	if _, Err := Tx.Exec(Ctx, Strings.Replace(Migration.AdvisoryLockSQL, "pg_advisory_lock", "pg_advisory_xact_lock", 1), DatabaseID); Err != nil {
		T.Fatal(Err)
	}
	LockCtx, Cancel := Context.WithTimeout(Ctx, 3*Time.Second)
	Done := make(chan struct{})
	go func() {
		defer close(Done)
		Repo := &Migration.Repository{Pool: Pool}
		_, _ = Repo.WithExecutionLock(LockCtx, DatabaseID, func(Migration.MigrationRepository) (Migration.Execution, error) {
			return Migration.Execution{}, nil
		})
	}()
	defer func() {
		Cancel()
		<-Done
	}()
	for LockCtx.Err() == nil {
		Scrape := Httptest.NewRecorder()
		Metrics.ServeHTTP(Scrape, Httptest.NewRequest(Http.MethodGet, "/api/metrics", nil).WithContext(LockCtx))
		for _, Line := range Strings.Split(Scrape.Body.String(), "\n") {
			if Value, OK := Strings.CutPrefix(Line, "wbd_migration_lock_wait_max_seconds "); OK {
				if Wait, Err := Strconv.ParseFloat(Value, 64); Err == nil && Wait > 0 {
					return
				}
			}
		}
		Time.Sleep(20 * Time.Millisecond)
	}
	T.Fatal("scrape did not observe the advisory lock wait")
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
