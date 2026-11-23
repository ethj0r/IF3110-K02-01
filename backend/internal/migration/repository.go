package Migration

import (
	Context "context"
	Errors "errors"
	Slog "log/slog"
	Time "time"

	Tracing "github.com/ethj0r/IF3110-K02-01/backend/internal/tracing"
	Attribute "go.opentelemetry.io/otel/attribute"

	Pgx "github.com/jackc/pgx/v5"
	Pgconn "github.com/jackc/pgx/v5/pgconn"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	Pool          *Pgxpool.Pool
	Conn          *Pgxpool.Conn
	requireReview bool
}

func NewReviewedRepository(Pool *Pgxpool.Pool) *Repository {
	return &Repository{Pool: Pool, requireReview: true}
}

type ExecutionRequest struct {
	Kind        ExecutionKind
	Direction   Direction
	SQLText     string
	RollbackOf  *string
	RetryOf     *string
	RequestedBy *string
}

func (Repo *Repository) queryRow(Ctx Context.Context, SQL string, Args ...any) Pgx.Row {
	if Repo.Conn != nil {
		return Repo.Conn.QueryRow(Ctx, SQL, Args...)
	}
	return Repo.Pool.QueryRow(Ctx, SQL, Args...)
}

func (Repo *Repository) query(Ctx Context.Context, SQL string, Args ...any) (Pgx.Rows, error) {
	if Repo.Conn != nil {
		return Repo.Conn.Query(Ctx, SQL, Args...)
	}
	return Repo.Pool.Query(Ctx, SQL, Args...)
}

func (Repo *Repository) exec(Ctx Context.Context, SQL string, Args ...any) (Pgconn.CommandTag, error) {
	if Repo.Conn != nil {
		return Repo.Conn.Exec(Ctx, SQL, Args...)
	}
	return Repo.Pool.Exec(Ctx, SQL, Args...)
}

// acquireLock takes the per-database advisory lock and records the wait as a
// span and a migration.lock.acquired (or .failed) log event.
func (Repo *Repository) acquireLock(Ctx Context.Context, DatabaseID string) (*Pgxpool.Conn, Time.Time, error) {
	Started := Time.Now()
	Ctx, Span := Tracing.Start(Ctx, "migration.lock.acquire", Attribute.String("database_id", DatabaseID))
	defer Span.End()
	Conn, Err := Repo.Pool.Acquire(Ctx)
	if Err == nil {
		if _, Err = Conn.Exec(Ctx, "SELECT pg_advisory_lock(hashtext('migration_' || $1::uuid::text))", DatabaseID); Err != nil {
			Conn.Release()
		}
	}
	Wait := float64(Time.Since(Started).Microseconds()) / 1000
	Span.SetAttributes(Attribute.Float64("lock.wait_ms", Wait))
	if Err != nil {
		Tracing.Fail(Span, Err, "migration lock unavailable")
		Slog.ErrorContext(Ctx, "migration lock unavailable", "event", "migration.lock.failed", "database_id", DatabaseID, "wait_ms", Wait)
		return nil, Time.Time{}, ErrUnavailable
	}
	Slog.InfoContext(Ctx, "migration lock acquired", "event", "migration.lock.acquired", "database_id", DatabaseID, "wait_ms", Wait)
	return Conn, Time.Now(), nil
}

func (Repo *Repository) releaseLock(Ctx Context.Context, Conn *Pgxpool.Conn, DatabaseID string, Acquired Time.Time) {
	Cleanup, Cancel := Context.WithTimeout(Context.WithoutCancel(Ctx), 5*Time.Second)
	defer Cancel()
	Held := float64(Time.Since(Acquired).Microseconds()) / 1000
	if _, Err := Conn.Exec(Cleanup, "SELECT pg_advisory_unlock(hashtext('migration_' || $1::uuid::text))", DatabaseID); Err != nil {
		_ = Conn.Hijack().Close(Cleanup)
		Slog.WarnContext(Cleanup, "migration lock released by closing the connection", "event", "migration.lock.released", "database_id", DatabaseID, "held_ms", Held, "status", "connection_closed")
		return
	}
	Conn.Release()
	Slog.InfoContext(Cleanup, "migration lock released", "event", "migration.lock.released", "database_id", DatabaseID, "held_ms", Held, "status", "ok")
}

func (Repo *Repository) WithDatabaseLock(Ctx Context.Context, DatabaseID string, Run func(MigrationRepository) (Migration, error)) (Migration, error) {
	Conn, Acquired, Err := Repo.acquireLock(Ctx, DatabaseID)
	if Err != nil {
		return Migration{}, Err
	}
	defer Repo.releaseLock(Ctx, Conn, DatabaseID, Acquired)
	Locked := *Repo
	Locked.Conn = Conn
	return Run(&Locked)
}

func (Repo *Repository) WithExecutionLock(Ctx Context.Context, DatabaseID string, Run func(MigrationRepository) (Execution, error)) (Execution, error) {
	Conn, Acquired, Err := Repo.acquireLock(Ctx, DatabaseID)
	if Err != nil {
		return Execution{}, Err
	}
	defer Repo.releaseLock(Ctx, Conn, DatabaseID, Acquired)
	Locked := *Repo
	Locked.Conn = Conn
	return Run(&Locked)
}

const MigrationColumns = `M.id::text, M.database_id::text, M.version, M.revision, M.title, M.sql_text, M.down_sql,
M.batch_mode, M.batch_column, M.batch_size,
M.irreversible, M.irreversible_reason, M.status, M.error_message, M.created_by::text, M.executed_by::text,
M.created_at, M.updated_at, M.executed_at, (SELECT U.username FROM users U WHERE U.id = M.executed_by), M.locked_at`

const MigrationReturning = `id::text, database_id::text, version, revision, title, sql_text, down_sql,
batch_mode, batch_column, batch_size,
irreversible, irreversible_reason, status, error_message, created_by::text, executed_by::text,
created_at, updated_at, executed_at, (SELECT U.username FROM users U WHERE U.id = migrations.executed_by), locked_at`

const ExecutionColumns = `E.id::text, E.migration_id::text, E.database_id::text, M.version, E.revision, M.title,
E.kind, E.direction, E.status, E.sql_text, E.up_sql, E.down_sql,
E.mode, E.batch_column, E.batch_size, E.committed_batches, E.committed_rows, E.reverted_batches, E.reverted_rows, E.checkpoint_cursor,
E.rollback_of_execution_id::text, E.retry_of_execution_id::text,
E.requested_by::text, U.username, E.created_at, E.started_at, E.finished_at, E.error_message`

func isConflict(Err error) bool {
	var PgErr *Pgconn.PgError
	return Errors.As(Err, &PgErr) && PgErr.Code == "23505"
}

type rowScanner interface {
	Scan(Dest ...any) error
}

func scanMigration(Row rowScanner) (Migration, error) {
	var Item Migration
	var StatusStr string
	Err := Row.Scan(
		&Item.ID,
		&Item.DatabaseID,
		&Item.Version,
		&Item.Revision,
		&Item.Title,
		&Item.SQLText,
		&Item.DownSQL,
		&Item.BatchMode,
		&Item.BatchColumn,
		&Item.BatchSize,
		&Item.Irreversible,
		&Item.IrreversibleReason,
		&StatusStr,
		&Item.ErrorMessage,
		&Item.CreatedBy,
		&Item.ExecutedBy,
		&Item.CreatedAt,
		&Item.UpdatedAt,
		&Item.ExecutedAt,
		&Item.ExecutedByUsername,
		&Item.LockedAt,
	)
	if Errors.Is(Err, Pgx.ErrNoRows) {
		return Migration{}, ErrNotFound
	}
	if Err != nil {
		if isConflict(Err) {
			return Migration{}, ErrConflict
		}
		return Migration{}, ErrUnavailable
	}
	if StatusStr == string(StatusCommitted) {
		StatusStr = string(StatusApplied)
	}
	Item.Status = Status(StatusStr)
	return Item, nil
}

func scanExecution(Row rowScanner) (Execution, error) {
	var Item Execution
	var KindStr, DirectionStr, StatusStr string
	Err := Row.Scan(
		&Item.ID,
		&Item.MigrationID,
		&Item.DatabaseID,
		&Item.Version,
		&Item.Revision,
		&Item.Title,
		&KindStr,
		&DirectionStr,
		&StatusStr,
		&Item.SQLText,
		&Item.UpSQL,
		&Item.DownSQL,
		&Item.Mode,
		&Item.BatchColumn,
		&Item.BatchSize,
		&Item.CommittedBatches,
		&Item.CommittedRows,
		&Item.RevertedBatches,
		&Item.RevertedRows,
		&Item.CheckpointCursor,
		&Item.RollbackOfExecutionID,
		&Item.RetryOfExecutionID,
		&Item.RequestedBy,
		&Item.RequestedByUsername,
		&Item.CreatedAt,
		&Item.StartedAt,
		&Item.FinishedAt,
		&Item.ErrorMessage,
	)
	if Errors.Is(Err, Pgx.ErrNoRows) {
		return Execution{}, ErrNotFound
	}
	if Err != nil {
		return Execution{}, ErrUnavailable
	}
	Item.Kind = ExecutionKind(KindStr)
	Item.Direction = Direction(DirectionStr)
	Item.Status = ExecutionStatus(StatusStr)
	Item.Attempts = []Attempt{}
	return Item, nil
}

func scanAttempt(Row rowScanner) (Attempt, error) {
	var Item Attempt
	var StatusStr string
	Err := Row.Scan(
		&Item.ID,
		&Item.ExecutionID,
		&Item.AttemptNo,
		&Item.Version,
		&Item.Revision,
		&Item.ActorID,
		&Item.ActorUsername,
		&Item.SQLText,
		&StatusStr,
		&Item.ErrorMessage,
		&Item.StartedAt,
		&Item.FinishedAt,
		&Item.DurationMillis,
	)
	if Errors.Is(Err, Pgx.ErrNoRows) {
		return Attempt{}, ErrExecutionMissing
	}
	if Err != nil {
		return Attempt{}, ErrUnavailable
	}
	Item.Status = AttemptStatus(StatusStr)
	return Item, nil
}

func (Repo *Repository) Create(Ctx Context.Context, DatabaseID string, Input MigrationInput, CreatedBy *string) (Migration, error) {
	if Repo.Conn == nil {
		return Repo.WithDatabaseLock(Ctx, DatabaseID, func(Locked MigrationRepository) (Migration, error) {
			return Locked.Create(Ctx, DatabaseID, Input, CreatedBy)
		})
	}
	if Err := Input.Validate(); Err != nil {
		return Migration{}, Err
	}
	Input = Input.Normalized()

	var NextVersion int64
	if Err := Repo.queryRow(Ctx, "SELECT COALESCE(MAX(version), 0) + 1 FROM migrations WHERE database_id = $1", DatabaseID).Scan(&NextVersion); Err != nil {
		return Migration{}, ErrUnavailable
	}

	var DownSQL *string
	var Reason *string
	if !Input.Irreversible {
		DownSQL = &Input.DownSQL
	} else {
		Reason = &Input.IrreversibleReason
	}
	var BatchColumn *string
	var BatchSize *int
	if Input.BatchMode {
		BatchColumn = &Input.BatchColumn
		BatchSize = &Input.BatchSize
	}

	Row := Repo.queryRow(Ctx, `INSERT INTO migrations
(database_id, version, revision, title, sql_text, down_sql, batch_mode, batch_column, batch_size, irreversible, irreversible_reason, status, created_by)
VALUES ($1, $2, 1, $3, $4, $5, $6, $7, $8, $9, $10, 'draft', $11) RETURNING `+MigrationReturning,
		DatabaseID, NextVersion, Input.Title, Input.SQLText, DownSQL, Input.BatchMode, BatchColumn, BatchSize, Input.Irreversible, Reason, CreatedBy)

	return scanMigration(Row)
}

func (Repo *Repository) Get(Ctx Context.Context, DatabaseID string, ID string) (Migration, error) {
	Row := Repo.queryRow(Ctx, "SELECT "+MigrationColumns+" FROM migrations M WHERE M.database_id = $1 AND M.id = $2", DatabaseID, ID)
	return scanMigration(Row)
}

func (Repo *Repository) GetByVersion(Ctx Context.Context, DatabaseID string, Version int64) (Migration, error) {
	Row := Repo.queryRow(Ctx, "SELECT "+MigrationColumns+" FROM migrations M WHERE M.database_id = $1 AND M.version = $2", DatabaseID, Version)
	return scanMigration(Row)
}

func (Repo *Repository) List(Ctx Context.Context, DatabaseID string) ([]Migration, error) {
	Rows, Err := Repo.query(Ctx, "SELECT "+MigrationColumns+" FROM migrations M WHERE M.database_id = $1 ORDER BY M.version ASC", DatabaseID)
	if Err != nil {
		return nil, ErrUnavailable
	}
	defer Rows.Close()

	Result := []Migration{}
	for Rows.Next() {
		Item, Err := scanMigration(Rows)
		if Err != nil {
			return nil, Err
		}
		Result = append(Result, Item)
	}
	if Err := Rows.Err(); Err != nil {
		return nil, ErrUnavailable
	}
	return Result, nil
}

func (Repo *Repository) UpdateDraft(Ctx Context.Context, DatabaseID string, ID string, Input MigrationInput) (Migration, error) {
	if Repo.Conn == nil {
		return Repo.WithDatabaseLock(Ctx, DatabaseID, func(Locked MigrationRepository) (Migration, error) {
			return Locked.UpdateDraft(Ctx, DatabaseID, ID, Input)
		})
	}
	if Err := Input.Validate(); Err != nil {
		return Migration{}, Err
	}
	Input = Input.Normalized()

	Current, Err := Repo.Get(Ctx, DatabaseID, ID)
	if Err != nil {
		return Migration{}, Err
	}
	if Current.LockedAt != nil || !Current.Status.IsDraft() {
		return Migration{}, ErrLocked
	}

	var DownSQL *string
	var Reason *string
	if !Input.Irreversible {
		DownSQL = &Input.DownSQL
	} else {
		Reason = &Input.IrreversibleReason
	}
	var BatchColumn *string
	var BatchSize *int
	if Input.BatchMode {
		BatchColumn = &Input.BatchColumn
		BatchSize = &Input.BatchSize
	}

	Row := Repo.queryRow(Ctx, `UPDATE migrations
SET title = $3, sql_text = $4, down_sql = $5, batch_mode = $6, batch_column = $7, batch_size = $8,
    irreversible = $9, irreversible_reason = $10, revision = revision + 1, updated_at = CURRENT_TIMESTAMP
WHERE database_id = $1 AND id = $2 AND status = 'draft' AND locked_at IS NULL
RETURNING `+MigrationReturning, DatabaseID, ID, Input.Title, Input.SQLText, DownSQL, Input.BatchMode, BatchColumn, BatchSize, Input.Irreversible, Reason)

	return scanMigration(Row)
}

func (Repo *Repository) DeleteDraft(Ctx Context.Context, DatabaseID string, ID string) error {
	if Repo.Conn == nil {
		_, Err := Repo.WithDatabaseLock(Ctx, DatabaseID, func(Locked MigrationRepository) (Migration, error) {
			return Migration{}, Locked.DeleteDraft(Ctx, DatabaseID, ID)
		})
		return Err
	}
	Current, Err := Repo.Get(Ctx, DatabaseID, ID)
	if Err != nil {
		return Err
	}
	if Current.LockedAt != nil || !Current.Status.IsDraft() {
		return ErrLocked
	}

	CommandTag, Err := Repo.Conn.Exec(Ctx, "DELETE FROM migrations WHERE database_id = $1 AND id = $2 AND status = 'draft' AND locked_at IS NULL", DatabaseID, ID)
	if Err != nil {
		return ErrUnavailable
	}
	if CommandTag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (Repo *Repository) CreateExecution(Ctx Context.Context, DatabaseID string, MigrationID string, Request ExecutionRequest) (Execution, error) {
	if Repo.Conn == nil {
		return Repo.WithExecutionLock(Ctx, DatabaseID, func(Locked MigrationRepository) (Execution, error) {
			return Locked.CreateExecution(Ctx, DatabaseID, MigrationID, Request)
		})
	}
	var ExecutionID string
	Err := Repo.queryRow(Ctx, reviewGroups+`, Locked AS (
UPDATE migrations
SET locked_at = COALESCE(locked_at, CURRENT_TIMESTAMP), updated_at = CURRENT_TIMESTAMP
WHERE database_id = $1::text::uuid AND id = $2::text::uuid
  AND (NOT $9::boolean OR EXISTS (
      SELECT 1 FROM current_review R JOIN review_groups G ON G.review_id = R.id
      WHERE R.sql_text = $5
      GROUP BY R.id HAVING bool_and(G.approved)
  ))
RETURNING id, database_id, revision, sql_text, down_sql, batch_mode, batch_column, batch_size
)
INSERT INTO migration_executions
(migration_id, database_id, kind, direction, revision, status, sql_text, up_sql, down_sql, mode, batch_column, batch_size,
 rollback_of_execution_id, retry_of_execution_id, requested_by, created_at, started_at)
SELECT L.id, L.database_id, $3, $4, L.revision, 'running', $5, L.sql_text, L.down_sql,
       CASE WHEN L.batch_mode THEN 'batch' ELSE 'normal' END, L.batch_column, L.batch_size,
       $6, $7, $8, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM Locked L
RETURNING id::text`, DatabaseID, MigrationID, string(Request.Kind), string(Request.Direction), Request.SQLText, Request.RollbackOf, Request.RetryOf, Request.RequestedBy, Repo.requireReview).Scan(&ExecutionID)
	if Errors.Is(Err, Pgx.ErrNoRows) {
		if Repo.requireReview {
			return Execution{}, ErrApprovalRequired
		}
		return Execution{}, ErrNotFound
	}
	if Err != nil {
		return Execution{}, ErrUnavailable
	}
	return Repo.GetExecution(Ctx, DatabaseID, ExecutionID)
}

func (Repo *Repository) GetExecution(Ctx Context.Context, DatabaseID string, ExecutionID string) (Execution, error) {
	Row := Repo.queryRow(Ctx, "SELECT "+ExecutionColumns+` FROM migration_executions E
JOIN migrations M ON M.id = E.migration_id
LEFT JOIN users U ON U.id = E.requested_by
WHERE E.database_id = $1 AND E.id = $2`, DatabaseID, ExecutionID)
	Item, Err := scanExecution(Row)
	if Err != nil {
		return Execution{}, Err
	}
	Attempts, Err := Repo.listAttempts(Ctx, []string{Item.ID})
	if Err != nil {
		return Execution{}, Err
	}
	Item.Attempts = Attempts[Item.ID]
	if Item.Attempts == nil {
		Item.Attempts = []Attempt{}
	}
	return Item, nil
}

func (Repo *Repository) LatestExecution(Ctx Context.Context, DatabaseID string, MigrationID string) (Execution, error) {
	Row := Repo.queryRow(Ctx, "SELECT "+ExecutionColumns+` FROM migration_executions E
JOIN migrations M ON M.id = E.migration_id
LEFT JOIN users U ON U.id = E.requested_by
WHERE E.database_id = $1 AND E.migration_id = $2
ORDER BY E.created_at DESC, E.id DESC LIMIT 1`, DatabaseID, MigrationID)
	Item, Err := scanExecution(Row)
	if Errors.Is(Err, ErrNotFound) {
		return Execution{}, ErrNotFound
	}
	if Err != nil {
		return Execution{}, Err
	}
	Attempts, Err := Repo.listAttempts(Ctx, []string{Item.ID})
	if Err != nil {
		return Execution{}, Err
	}
	Item.Attempts = Attempts[Item.ID]
	if Item.Attempts == nil {
		Item.Attempts = []Attempt{}
	}
	return Item, nil
}

func (Repo *Repository) LatestSuccessfulExecution(Ctx Context.Context, DatabaseID string, MigrationID string) (Execution, error) {
	Row := Repo.queryRow(Ctx, "SELECT "+ExecutionColumns+` FROM migration_executions E
JOIN migrations M ON M.id = E.migration_id
LEFT JOIN users U ON U.id = E.requested_by
WHERE E.database_id = $1 AND E.migration_id = $2 AND E.status = 'succeeded'
ORDER BY E.finished_at DESC NULLS LAST, E.created_at DESC, E.id DESC LIMIT 1`, DatabaseID, MigrationID)
	Item, Err := scanExecution(Row)
	if Errors.Is(Err, ErrNotFound) {
		return Execution{}, ErrNotFound
	}
	if Err != nil {
		return Execution{}, Err
	}
	Attempts, Err := Repo.listAttempts(Ctx, []string{Item.ID})
	if Err != nil {
		return Execution{}, Err
	}
	Item.Attempts = Attempts[Item.ID]
	if Item.Attempts == nil {
		Item.Attempts = []Attempt{}
	}
	return Item, nil
}

func (Repo *Repository) listAttempts(Ctx Context.Context, ExecutionIDs []string) (map[string][]Attempt, error) {
	Result := map[string][]Attempt{}
	if len(ExecutionIDs) == 0 {
		return Result, nil
	}
	Rows, Err := Repo.query(Ctx, `SELECT A.id::text, A.execution_id::text, A.attempt_no, A.version, A.revision,
A.actor_id::text, (SELECT U.username FROM users U WHERE U.id = A.actor_id), A.sql_text, A.status,
A.error_message, A.started_at, A.finished_at, A.duration_ms
FROM migration_attempts A
WHERE A.execution_id = ANY($1::uuid[])
ORDER BY A.execution_id, A.attempt_no`, ExecutionIDs)
	if Err != nil {
		return nil, ErrUnavailable
	}
	defer Rows.Close()
	for Rows.Next() {
		Item, Err := scanAttempt(Rows)
		if Err != nil {
			return nil, Err
		}
		Result[Item.ExecutionID] = append(Result[Item.ExecutionID], Item)
	}
	if Err := Rows.Err(); Err != nil {
		return nil, ErrUnavailable
	}
	return Result, nil
}

func (Repo *Repository) CreateAttempt(Ctx Context.Context, ExecutionID string, ActorID *string, SQLText string) (Attempt, error) {
	Row := Repo.queryRow(Ctx, `INSERT INTO migration_attempts
(execution_id, migration_id, database_id, attempt_no, version, revision, actor_id, sql_text, status, started_at)
SELECT E.id, E.migration_id, E.database_id,
       COALESCE((SELECT MAX(A.attempt_no) FROM migration_attempts A WHERE A.execution_id = E.id), 0) + 1,
       M.version, E.revision, $2, $3, 'running', CURRENT_TIMESTAMP
FROM migration_executions E JOIN migrations M ON M.id = E.migration_id
WHERE E.id = $1
RETURNING id::text, execution_id::text, attempt_no, version, revision, actor_id::text,
          (SELECT U.username FROM users U WHERE U.id = migration_attempts.actor_id),
          sql_text, status, error_message, started_at, finished_at, duration_ms`, ExecutionID, ActorID, SQLText)
	return scanAttempt(Row)
}

func (Repo *Repository) FinishAttempt(Ctx Context.Context, AttemptID string, Status AttemptStatus, ExecutionErr *string) error {
	var ID string
	Err := Repo.queryRow(Ctx, `UPDATE migration_attempts
SET status = $2, error_message = $3, finished_at = CURRENT_TIMESTAMP,
    duration_ms = GREATEST(0, (EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - started_at)) * 1000)::bigint)
WHERE id = $1 RETURNING id::text`, AttemptID, string(Status), ExecutionErr).Scan(&ID)
	if Errors.Is(Err, Pgx.ErrNoRows) {
		return ErrExecutionMissing
	}
	if Err != nil {
		return ErrUnavailable
	}
	return nil
}

func (Repo *Repository) FinishExecution(Ctx Context.Context, ExecutionID string, Status ExecutionStatus, ExecutionErr *string) error {
	var ID string
	Err := Repo.queryRow(Ctx, `UPDATE migration_executions
SET status = $2, error_message = $3, finished_at = CURRENT_TIMESTAMP
WHERE id = $1 RETURNING id::text`, ExecutionID, string(Status), ExecutionErr).Scan(&ID)
	if Errors.Is(Err, Pgx.ErrNoRows) {
		return ErrExecutionMissing
	}
	if Err != nil {
		return ErrUnavailable
	}
	return nil
}

func (Repo *Repository) RefreshStatus(Ctx Context.Context, DatabaseID string, MigrationID string) (Migration, error) {
	SQL := `WITH Latest AS (
SELECT status, direction, error_message, requested_by
FROM migration_executions
WHERE database_id = $1 AND migration_id = $2
ORDER BY created_at DESC, id DESC LIMIT 1
), Success AS (
SELECT direction, finished_at, requested_by
FROM migration_executions
WHERE database_id = $1 AND migration_id = $2 AND status = 'succeeded'
ORDER BY finished_at DESC NULLS LAST, created_at DESC, id DESC LIMIT 1
), Applied AS (
SELECT status, direction
FROM migration_executions
WHERE database_id = $1 AND migration_id = $2 AND direction = 'up' AND status IN ('succeeded', 'partial')
ORDER BY created_at DESC, id DESC LIMIT 1
), Derived AS (
SELECT CASE
    WHEN NOT EXISTS (SELECT 1 FROM Latest) THEN 'draft'
    WHEN (SELECT status FROM Latest) IN ('queued', 'running') THEN 'pending'
    WHEN (SELECT status FROM Latest) = 'partial' THEN 'partial'
    WHEN (SELECT status FROM Latest) = 'partially_reverted' THEN 'partially_reverted'
    WHEN (SELECT status FROM Latest) = 'succeeded' AND (SELECT direction FROM Latest) = 'up' THEN 'applied'
    WHEN (SELECT status FROM Latest) = 'succeeded' AND (SELECT direction FROM Latest) = 'down' THEN 'rolled_back'
    WHEN (SELECT status FROM Applied) = 'partial' THEN 'partial'
    WHEN (SELECT status FROM Applied) = 'succeeded' THEN 'applied'
    WHEN (SELECT direction FROM Success) = 'down' THEN 'rolled_back'
    ELSE 'failed'
END AS status,
(SELECT error_message FROM Latest) AS error_message,
(SELECT finished_at FROM Success) AS executed_at,
(SELECT requested_by FROM Success) AS executed_by
)
UPDATE migrations AS M
SET status = D.status,
    error_message = CASE WHEN D.status IN ('failed', 'partial', 'partially_reverted') THEN D.error_message ELSE NULL END,
    executed_at = D.executed_at,
    executed_by = D.executed_by,
    updated_at = CURRENT_TIMESTAMP
FROM Derived D
WHERE M.database_id = $1 AND M.id = $2`
	if Repo.Conn != nil {
		if _, Err := Repo.Conn.Exec(Ctx, SQL, DatabaseID, MigrationID); Err != nil {
			return Migration{}, ErrUnavailable
		}
	} else if _, Err := Repo.Pool.Exec(Ctx, SQL, DatabaseID, MigrationID); Err != nil {
		return Migration{}, ErrUnavailable
	}
	return Repo.Get(Ctx, DatabaseID, MigrationID)
}

func (Repo *Repository) ListHistory(Ctx Context.Context, DatabaseID string, HideRollback bool) ([]Execution, error) {
	Rows, Err := Repo.query(Ctx, "SELECT "+ExecutionColumns+` FROM migration_executions E
JOIN migrations M ON M.id = E.migration_id
LEFT JOIN users U ON U.id = E.requested_by
WHERE E.database_id = $1
AND ($2 = FALSE OR NOT EXISTS (
    SELECT 1 FROM migration_executions R
    WHERE R.database_id = E.database_id AND R.kind = 'rollback' AND R.status = 'succeeded'
      AND (R.id = E.id OR R.rollback_of_execution_id = E.id)
))
ORDER BY E.created_at ASC, E.id ASC`, DatabaseID, HideRollback)
	if Err != nil {
		return nil, ErrUnavailable
	}
	defer Rows.Close()
	Result := []Execution{}
	IDs := []string{}
	for Rows.Next() {
		Item, Err := scanExecution(Rows)
		if Err != nil {
			return nil, Err
		}
		Result = append(Result, Item)
		IDs = append(IDs, Item.ID)
	}
	if Err := Rows.Err(); Err != nil {
		return nil, ErrUnavailable
	}
	Attempts, Err := Repo.listAttempts(Ctx, IDs)
	if Err != nil {
		return nil, Err
	}
	for Index := range Result {
		Result[Index].Attempts = Attempts[Result[Index].ID]
		if Result[Index].Attempts == nil {
			Result[Index].Attempts = []Attempt{}
		}
	}
	return Result, nil
}

const JobColumns = `J.id::text, J.database_id::text, J.migration_id::text, M.version, M.title,
J.action, J.status, J.attempts, J.max_attempts, J.available_at, J.locked_by, J.locked_at,
J.lease_expires_at, J.last_error, J.execution_id::text, J.requested_by::text,
(SELECT U.username FROM users U WHERE U.id = J.requested_by), J.traceparent,
E.mode, COALESCE(E.committed_batches, 0), COALESCE(E.committed_rows, 0),
COALESCE(E.reverted_batches, 0), COALESCE(E.reverted_rows, 0), COALESCE(E.checkpoint_cursor, 0),
J.created_at, J.updated_at, J.finished_at`

func scanJob(Row rowScanner) (Job, error) {
	var Item Job
	var ActionStr, StatusStr string
	Err := Row.Scan(
		&Item.ID,
		&Item.DatabaseID,
		&Item.MigrationID,
		&Item.Version,
		&Item.Title,
		&ActionStr,
		&StatusStr,
		&Item.Attempts,
		&Item.MaxAttempts,
		&Item.AvailableAt,
		&Item.LockedBy,
		&Item.LockedAt,
		&Item.LeaseExpiresAt,
		&Item.LastError,
		&Item.ExecutionID,
		&Item.RequestedBy,
		&Item.RequestedByUsername,
		&Item.TraceParent,
		&Item.Mode,
		&Item.CommittedBatches,
		&Item.CommittedRows,
		&Item.RevertedBatches,
		&Item.RevertedRows,
		&Item.CheckpointCursor,
		&Item.CreatedAt,
		&Item.UpdatedAt,
		&Item.FinishedAt,
	)
	if Errors.Is(Err, Pgx.ErrNoRows) {
		return Job{}, ErrJobNotFound
	}
	if Err != nil {
		return Job{}, ErrJobUnavailable
	}
	Item.Action = JobAction(ActionStr)
	Item.Status = JobStatus(StatusStr)
	return Item, nil
}

func (Repo *Repository) EnqueueJob(Ctx Context.Context, DatabaseID string, MigrationID string, Action JobAction, RequestedBy *string, TraceParent *string) (Job, error) {
	var JobID string
	Err := Repo.queryRow(Ctx, `INSERT INTO migration_jobs (database_id, migration_id, action, requested_by, traceparent)
SELECT M.database_id, M.id, $3, $4, $5
FROM migrations M
WHERE M.database_id::text = $1 AND M.id::text = $2
RETURNING id::text`, DatabaseID, MigrationID, string(Action), RequestedBy, TraceParent).Scan(&JobID)
	if Errors.Is(Err, Pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if isConflict(Err) {
		return Job{}, ErrJobConflict
	}
	if Err != nil {
		return Job{}, ErrJobUnavailable
	}
	return Repo.GetJob(Ctx, DatabaseID, JobID)
}

func (Repo *Repository) GetJob(Ctx Context.Context, DatabaseID string, JobID string) (Job, error) {
	Row := Repo.queryRow(Ctx, "SELECT "+JobColumns+` FROM migration_jobs J
JOIN migrations M ON M.id = J.migration_id
LEFT JOIN migration_executions E ON E.id = J.execution_id
WHERE J.database_id::text = $1 AND J.id::text = $2`, DatabaseID, JobID)
	return scanJob(Row)
}

func (Repo *Repository) ClaimJob(Ctx Context.Context, WorkerID string, Lease Time.Duration) (Job, error) {
	var JobID, DatabaseID string
	Err := Repo.queryRow(Ctx, `WITH Candidate AS (
SELECT J.id FROM migration_jobs J
WHERE J.status = 'queued' AND J.available_at <= CURRENT_TIMESTAMP
ORDER BY J.created_at ASC
FOR UPDATE SKIP LOCKED
LIMIT 1
)
UPDATE migration_jobs J
SET status = 'running', locked_by = $1, locked_at = CURRENT_TIMESTAMP,
    lease_expires_at = CURRENT_TIMESTAMP + $2::interval,
    attempts = J.attempts + 1, updated_at = CURRENT_TIMESTAMP
FROM Candidate C
WHERE J.id = C.id
RETURNING J.id::text, J.database_id::text`, WorkerID, Lease.String()).Scan(&JobID, &DatabaseID)
	if Errors.Is(Err, Pgx.ErrNoRows) {
		return Job{}, ErrJobNotFound
	}
	if Err != nil {
		return Job{}, ErrJobUnavailable
	}
	return Repo.GetJob(Ctx, DatabaseID, JobID)
}

func (Repo *Repository) FinishJob(Ctx Context.Context, JobID string, Status JobStatus, ExecutionID *string, LastError *string) error {
	var ID string
	Err := Repo.queryRow(Ctx, `UPDATE migration_jobs
SET status = $2, execution_id = COALESCE($3, execution_id), last_error = $4,
    finished_at = CURRENT_TIMESTAMP, locked_by = NULL, lease_expires_at = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE id::text = $1
RETURNING id::text`, JobID, string(Status), ExecutionID, LastError).Scan(&ID)
	if Errors.Is(Err, Pgx.ErrNoRows) {
		return ErrJobNotFound
	}
	if Err != nil {
		return ErrJobUnavailable
	}
	return nil
}

func (Repo *Repository) RequeueJob(Ctx Context.Context, JobID string, AvailableAt Time.Time, LastError string) error {
	var ID string
	Err := Repo.queryRow(Ctx, `UPDATE migration_jobs
SET status = 'queued', available_at = $2, last_error = $3,
    locked_by = NULL, locked_at = NULL, lease_expires_at = NULL, updated_at = CURRENT_TIMESTAMP
WHERE id::text = $1 AND status = 'running'
RETURNING id::text`, JobID, AvailableAt, LastError).Scan(&ID)
	if Errors.Is(Err, Pgx.ErrNoRows) {
		return ErrJobNotFound
	}
	if Err != nil {
		return ErrJobUnavailable
	}
	return nil
}

func (Repo *Repository) ExpireStaleJobs(Ctx Context.Context, Limit int) (int, error) {
	CommandTag, Err := Repo.exec(Ctx, `WITH Expired AS (
SELECT id FROM migration_jobs
WHERE status = 'running' AND lease_expires_at IS NOT NULL AND lease_expires_at < CURRENT_TIMESTAMP
ORDER BY lease_expires_at ASC
LIMIT $1
FOR UPDATE SKIP LOCKED
)
UPDATE migration_jobs J
SET status = 'failed',
    last_error = 'worker lease expired; verify the execution outcome before retrying',
    finished_at = CURRENT_TIMESTAMP, locked_by = NULL, lease_expires_at = NULL,
    updated_at = CURRENT_TIMESTAMP
FROM Expired E
WHERE J.id = E.id`, Limit)
	if Err != nil {
		return 0, ErrJobUnavailable
	}
	return int(CommandTag.RowsAffected()), nil
}

func (Repo *Repository) begin(Ctx Context.Context) (Pgx.Tx, error) {
	if Repo.Conn != nil {
		return Repo.Conn.Begin(Ctx)
	}
	return Repo.Pool.Begin(Ctx)
}

func (Repo *Repository) SetExecutionRunning(Ctx Context.Context, DatabaseID string, ExecutionID string) (Execution, error) {
	var ID string
	Err := Repo.queryRow(Ctx, `UPDATE migration_executions
SET status = 'running', error_message = NULL, finished_at = NULL,
    started_at = COALESCE(started_at, CURRENT_TIMESTAMP)
WHERE database_id::text = $1 AND id::text = $2
RETURNING id::text`, DatabaseID, ExecutionID).Scan(&ID)
	if Errors.Is(Err, Pgx.ErrNoRows) {
		return Execution{}, ErrNotFound
	}
	if Err != nil {
		return Execution{}, ErrUnavailable
	}
	return Repo.GetExecution(Ctx, DatabaseID, ExecutionID)
}

func (Repo *Repository) RecordBatchUp(Ctx Context.Context, ExecutionID string, DatabaseID string, BatchNo int64, CursorFrom int64, CursorTo int64, RowCount int64) (Execution, error) {
	Tx, Err := Repo.begin(Ctx)
	if Err != nil {
		return Execution{}, ErrUnavailable
	}
	defer Tx.Rollback(Ctx)
	if _, Err := Tx.Exec(Ctx, `INSERT INTO migration_batches
(execution_id, database_id, batch_no, cursor_from, cursor_to, row_count, status)
VALUES ($1, $2, $3, $4, $5, $6, 'committed')
ON CONFLICT (execution_id, batch_no) DO NOTHING`, ExecutionID, DatabaseID, BatchNo, CursorFrom, CursorTo, RowCount); Err != nil {
		return Execution{}, ErrUnavailable
	}
	if _, Err := Tx.Exec(Ctx, `UPDATE migration_executions E SET
committed_batches = (SELECT count(*) FROM migration_batches B WHERE B.execution_id = E.id AND B.status = 'committed'),
committed_rows = (SELECT COALESCE(sum(B.row_count), 0) FROM migration_batches B WHERE B.execution_id = E.id AND B.status = 'committed'),
checkpoint_cursor = (SELECT COALESCE(max(B.cursor_to), 0) FROM migration_batches B WHERE B.execution_id = E.id AND B.status = 'committed')
WHERE E.id::text = $1`, ExecutionID); Err != nil {
		return Execution{}, ErrUnavailable
	}
	if Err := Tx.Commit(Ctx); Err != nil {
		return Execution{}, ErrUnavailable
	}
	return Repo.GetExecution(Ctx, DatabaseID, ExecutionID)
}

func (Repo *Repository) RecordBatchReverted(Ctx Context.Context, RollbackExecutionID string, UpExecutionID string, DatabaseID string, BatchNo int64) (Execution, error) {
	Tx, Err := Repo.begin(Ctx)
	if Err != nil {
		return Execution{}, ErrUnavailable
	}
	defer Tx.Rollback(Ctx)
	if _, Err := Tx.Exec(Ctx, `UPDATE migration_batches
SET status = 'reverted', reverted_at = CURRENT_TIMESTAMP, reverted_by_execution_id = $1
WHERE execution_id::text = $2 AND batch_no = $3 AND status = 'committed'`, RollbackExecutionID, UpExecutionID, BatchNo); Err != nil {
		return Execution{}, ErrUnavailable
	}
	if _, Err := Tx.Exec(Ctx, `UPDATE migration_executions E SET
reverted_batches = (SELECT count(*) FROM migration_batches B WHERE B.reverted_by_execution_id = E.id AND B.status = 'reverted'),
reverted_rows = (SELECT COALESCE(sum(B.row_count), 0) FROM migration_batches B WHERE B.reverted_by_execution_id = E.id AND B.status = 'reverted')
WHERE E.id::text = $1`, RollbackExecutionID); Err != nil {
		return Execution{}, ErrUnavailable
	}
	if Err := Tx.Commit(Ctx); Err != nil {
		return Execution{}, ErrUnavailable
	}
	return Repo.GetExecution(Ctx, DatabaseID, RollbackExecutionID)
}

func (Repo *Repository) ListCommittedBatches(Ctx Context.Context, ExecutionID string) ([]Batch, error) {
	Rows, Err := Repo.query(Ctx, `SELECT id::text, execution_id::text, database_id::text, batch_no, cursor_from, cursor_to,
row_count, status, committed_at, reverted_at, reverted_by_execution_id::text
FROM migration_batches
WHERE execution_id::text = $1 AND status = 'committed'
ORDER BY batch_no DESC`, ExecutionID)
	if Err != nil {
		return nil, ErrUnavailable
	}
	defer Rows.Close()
	Result := []Batch{}
	for Rows.Next() {
		var Item Batch
		if Err := Rows.Scan(&Item.ID, &Item.ExecutionID, &Item.DatabaseID, &Item.BatchNo, &Item.CursorFrom, &Item.CursorTo,
			&Item.RowCount, &Item.Status, &Item.CommittedAt, &Item.RevertedAt, &Item.RevertedByExecutionID); Err != nil {
			return nil, ErrUnavailable
		}
		Result = append(Result, Item)
	}
	if Err := Rows.Err(); Err != nil {
		return nil, ErrUnavailable
	}
	return Result, nil
}
