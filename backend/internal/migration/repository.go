package Migration

import (
	Context "context"
	Errors "errors"
	Time "time"

	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
	Pgx "github.com/jackc/pgx/v5"
	Pgconn "github.com/jackc/pgx/v5/pgconn"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	Pool *Pgxpool.Pool
	Conn *Pgxpool.Conn
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

func (Repo *Repository) WithDatabaseLock(Ctx Context.Context, DatabaseID string, Run func(MigrationRepository) (Migration, error)) (Migration, error) {
	Conn, Err := Repo.Pool.Acquire(Ctx)
	if Err != nil {
		return Migration{}, ErrUnavailable
	}
	defer Conn.Release()
	defer func() {
		Cleanup, Cancel := Context.WithTimeout(Context.Background(), 5*Time.Second)
		defer Cancel()
		if _, Err := Conn.Exec(Cleanup, "SELECT pg_advisory_unlock(hashtext('migration_' || $1::uuid::text))", DatabaseID); Err != nil {
			_ = Conn.Hijack().Close(Cleanup)
		}
	}()
	if _, Err := Conn.Exec(Ctx, "SELECT pg_advisory_lock(hashtext('migration_' || $1::uuid::text))", DatabaseID); Err != nil {
		return Migration{}, ErrUnavailable
	}
	return Run(&Repository{Pool: Repo.Pool, Conn: Conn})
}

const MigrationColumns = "M.id::text, M.database_id::text, M.version, M.title, M.sql_text, M.status, M.error_message, M.created_by::text, M.executed_by::text, M.created_at, M.updated_at, M.executed_at"

const MigrationReturning = "id::text, database_id::text, version, title, sql_text, status, error_message, created_by::text, executed_by::text, created_at, updated_at, executed_at"

func isConflict(Err error) bool {
	var PgErr *Pgconn.PgError
	return Errors.As(Err, &PgErr) && PgErr.Code == "23505"
}

func scanMigration(Row Pgx.Row) (Migration, error) {
	var Item Migration
	var StatusStr string
	Err := Row.Scan(
		&Item.ID,
		&Item.DatabaseID,
		&Item.Version,
		&Item.Title,
		&Item.SQLText,
		&StatusStr,
		&Item.ErrorMessage,
		&Item.CreatedBy,
		&Item.ExecutedBy,
		&Item.CreatedAt,
		&Item.UpdatedAt,
		&Item.ExecutedAt,
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
	Item.Status = Status(StatusStr)
	if Item.ErrorMessage != nil {
		Safe := Driver.ErrExecution.Error()
		Item.ErrorMessage = &Safe
	}
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

	Row := Repo.queryRow(Ctx, `INSERT INTO migrations (database_id, version, title, sql_text, status, created_by)
VALUES ($1, $2, $3, $4, 'draft', $5) RETURNING `+MigrationReturning, DatabaseID, NextVersion, Input.Title, Input.SQLText, CreatedBy)

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
	if !Current.Status.IsDraft() {
		return Migration{}, ErrAlreadyExecuted
	}

	Row := Repo.queryRow(Ctx, `UPDATE migrations SET title = $3, sql_text = $4, updated_at = CURRENT_TIMESTAMP
WHERE database_id = $1 AND id = $2 AND status = 'draft' RETURNING `+MigrationReturning, DatabaseID, ID, Input.Title, Input.SQLText)

	return scanMigration(Row)
}

func (Repo *Repository) StartExecution(Ctx Context.Context, DatabaseID, ID string, ExecutedBy *string) (Migration, error) {
	return scanMigration(Repo.queryRow(Ctx, `UPDATE migrations
SET status = 'pending', error_message = NULL, executed_by = $3,
executed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE database_id = $1 AND id = $2 AND status IN ('draft', 'rolled_back', 'failed')
RETURNING `+MigrationReturning, DatabaseID, ID, ExecutedBy))
}

func (Repo *Repository) RecordExecution(Ctx Context.Context, DatabaseID string, ID string, ExecutedBy *string, Succeeded bool, ExecutionErr *string) (Migration, error) {
	if Repo.Conn == nil {
		return Repo.WithDatabaseLock(Ctx, DatabaseID, func(Locked MigrationRepository) (Migration, error) {
			return Locked.RecordExecution(Ctx, DatabaseID, ID, ExecutedBy, Succeeded, ExecutionErr)
		})
	}
	Current, Err := Repo.Get(Ctx, DatabaseID, ID)
	if Err != nil {
		return Migration{}, Err
	}
	if Current.Status.IsSuccess() {
		return Migration{}, ErrAlreadyExecuted
	}

	NewStatus := StatusCommitted
	if !Succeeded {
		NewStatus = StatusRolledBack
		Safe := Driver.ErrExecution.Error()
		ExecutionErr = &Safe
	} else {
		ExecutionErr = nil
	}

	Row := Repo.queryRow(Ctx, `UPDATE migrations SET status = $3, error_message = $4, executed_by = $5, executed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE database_id = $1 AND id = $2 RETURNING `+MigrationReturning, DatabaseID, ID, string(NewStatus), ExecutionErr, ExecutedBy)

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
	if !Current.Status.IsDraft() {
		return ErrAlreadyExecuted
	}

	CommandTag, Err := Repo.Conn.Exec(Ctx, "DELETE FROM migrations WHERE database_id = $1 AND id = $2 AND status = 'draft'", DatabaseID, ID)
	if Err != nil {
		return ErrUnavailable
	}
	if CommandTag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
