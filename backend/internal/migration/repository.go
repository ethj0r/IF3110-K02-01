package Migration

import (
	Context "context"
	Errors "errors"
	Time "time"

	Pgx "github.com/jackc/pgx/v5"
	Pgconn "github.com/jackc/pgx/v5/pgconn"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	Pool *Pgxpool.Pool
	Conn *Pgxpool.Conn
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

func (Repo *Repository) acquireLock(Ctx Context.Context, DatabaseID string) (*Pgxpool.Conn, error) {
	Conn, Err := Repo.Pool.Acquire(Ctx)
	if Err != nil {
		return nil, ErrUnavailable
	}
	if _, Err := Conn.Exec(Ctx, "SELECT pg_advisory_lock(hashtext('migration_' || $1::uuid::text))", DatabaseID); Err != nil {
		Conn.Release()
		return nil, ErrUnavailable
	}
	return Conn, nil
}

func (Repo *Repository) WithDatabaseLock(Ctx Context.Context, DatabaseID string, Run func(MigrationRepository) (Migration, error)) (Migration, error) {
	Conn, Err := Repo.acquireLock(Ctx, DatabaseID)
	if Err != nil {
		return Migration{}, Err
	}
	defer func() {
		Cleanup, Cancel := Context.WithTimeout(Context.Background(), 5*Time.Second)
		defer Cancel()
		if _, Err := Conn.Exec(Cleanup, "SELECT pg_advisory_unlock(hashtext('migration_' || $1::uuid::text))", DatabaseID); Err != nil {
			_ = Conn.Hijack().Close(Cleanup)
		}
		Conn.Release()
	}()
	return Run(&Repository{Pool: Repo.Pool, Conn: Conn})
}

func (Repo *Repository) WithExecutionLock(Ctx Context.Context, DatabaseID string, Run func(MigrationRepository) (Execution, error)) (Execution, error) {
	Conn, Err := Repo.acquireLock(Ctx, DatabaseID)
	if Err != nil {
		return Execution{}, Err
	}
	defer func() {
		Cleanup, Cancel := Context.WithTimeout(Context.Background(), 5*Time.Second)
		defer Cancel()
		if _, Err := Conn.Exec(Cleanup, "SELECT pg_advisory_unlock(hashtext('migration_' || $1::uuid::text))", DatabaseID); Err != nil {
			_ = Conn.Hijack().Close(Cleanup)
		}
		Conn.Release()
	}()
	return Run(&Repository{Pool: Repo.Pool, Conn: Conn})
}

const MigrationColumns = `M.id::text, M.database_id::text, M.version, M.revision, M.title, M.sql_text, M.down_sql,
M.irreversible, M.irreversible_reason, M.status, M.error_message, M.created_by::text, M.executed_by::text,
M.created_at, M.updated_at, M.executed_at, (SELECT U.username FROM users U WHERE U.id = M.executed_by), M.locked_at`

const MigrationReturning = `id::text, database_id::text, version, revision, title, sql_text, down_sql,
irreversible, irreversible_reason, status, error_message, created_by::text, executed_by::text,
created_at, updated_at, executed_at, (SELECT U.username FROM users U WHERE U.id = migrations.executed_by), locked_at`

const ExecutionColumns = `E.id::text, E.migration_id::text, E.database_id::text, M.version, E.revision, M.title,
E.kind, E.direction, E.status, E.sql_text, E.up_sql, E.down_sql,
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

	Row := Repo.queryRow(Ctx, `INSERT INTO migrations
(database_id, version, revision, title, sql_text, down_sql, irreversible, irreversible_reason, status, created_by)
VALUES ($1, $2, 1, $3, $4, $5, $6, $7, 'draft', $8) RETURNING `+MigrationReturning,
		DatabaseID, NextVersion, Input.Title, Input.SQLText, DownSQL, Input.Irreversible, Reason, CreatedBy)

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

	Row := Repo.queryRow(Ctx, `UPDATE migrations
SET title = $3, sql_text = $4, down_sql = $5, irreversible = $6, irreversible_reason = $7,
    revision = revision + 1, updated_at = CURRENT_TIMESTAMP
WHERE database_id = $1 AND id = $2 AND status = 'draft' AND locked_at IS NULL
RETURNING `+MigrationReturning, DatabaseID, ID, Input.Title, Input.SQLText, DownSQL, Input.Irreversible, Reason)

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
	Err := Repo.queryRow(Ctx, `WITH Locked AS (
UPDATE migrations
SET locked_at = COALESCE(locked_at, CURRENT_TIMESTAMP), updated_at = CURRENT_TIMESTAMP
WHERE database_id = $1 AND id = $2
RETURNING id, database_id, revision, sql_text, down_sql
)
INSERT INTO migration_executions
(migration_id, database_id, kind, direction, revision, status, sql_text, up_sql, down_sql,
 rollback_of_execution_id, retry_of_execution_id, requested_by, created_at, started_at)
SELECT L.id, L.database_id, $3, $4, L.revision, 'running', $5, L.sql_text, L.down_sql, $6, $7, $8,
       CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM Locked L
RETURNING id::text`, DatabaseID, MigrationID, string(Request.Kind), string(Request.Direction), Request.SQLText, Request.RollbackOf, Request.RetryOf, Request.RequestedBy).Scan(&ExecutionID)
	if Errors.Is(Err, Pgx.ErrNoRows) {
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
), Derived AS (
SELECT CASE
    WHEN NOT EXISTS (SELECT 1 FROM Latest) THEN 'draft'
    WHEN (SELECT status FROM Latest) IN ('queued', 'running') THEN 'pending'
    WHEN (SELECT status FROM Latest) = 'succeeded' AND (SELECT direction FROM Latest) = 'up' THEN 'applied'
    WHEN (SELECT status FROM Latest) = 'succeeded' AND (SELECT direction FROM Latest) = 'down' THEN 'rolled_back'
    WHEN (SELECT direction FROM Success) = 'up' THEN 'applied'
    WHEN (SELECT direction FROM Success) = 'down' THEN 'rolled_back'
    ELSE 'failed'
END AS status,
(SELECT error_message FROM Latest) AS error_message,
(SELECT finished_at FROM Success) AS executed_at,
(SELECT requested_by FROM Success) AS executed_by
)
UPDATE migrations AS M
SET status = D.status,
    error_message = CASE WHEN D.status = 'failed' THEN D.error_message ELSE NULL END,
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
(SELECT U.username FROM users U WHERE U.id = J.requested_by), J.traceparent, J.created_at, J.updated_at, J.finished_at`

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
