package Migration

import (
	Context "context"
	Errors "errors"

	Pgx "github.com/jackc/pgx/v5"
	Pgconn "github.com/jackc/pgx/v5/pgconn"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	Pool *Pgxpool.Pool
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
	return Item, nil
}

func (Repo *Repository) Create(Ctx Context.Context, DatabaseID string, Input MigrationInput, CreatedBy *string) (Migration, error) {
	if Err := Input.Validate(); Err != nil {
		return Migration{}, Err
	}
	Input = Input.Normalized()

	Tx, Err := Repo.Pool.Begin(Ctx)
	if Err != nil {
		return Migration{}, ErrUnavailable
	}
	defer func() { _ = Tx.Rollback(Ctx) }()

	if _, Err := Tx.Exec(Ctx, "SELECT pg_advisory_xact_lock(hashtext('migration_' || $1))", DatabaseID); Err != nil {
		return Migration{}, ErrUnavailable
	}

	var NextVersion int64
	if Err := Tx.QueryRow(Ctx, "SELECT COALESCE(MAX(version), 0) + 1 FROM migrations WHERE database_id = $1", DatabaseID).Scan(&NextVersion); Err != nil {
		return Migration{}, ErrUnavailable
	}

	Row := Tx.QueryRow(Ctx, `INSERT INTO migrations (database_id, version, title, sql_text, status, created_by)
VALUES ($1, $2, $3, $4, 'draft', $5) RETURNING `+MigrationReturning, DatabaseID, NextVersion, Input.Title, Input.SQLText, CreatedBy)

	Result, Err := scanMigration(Row)
	if Err != nil {
		return Migration{}, Err
	}

	if Err := Tx.Commit(Ctx); Err != nil {
		return Migration{}, ErrUnavailable
	}

	return Result, nil
}

func (Repo *Repository) Get(Ctx Context.Context, DatabaseID string, ID string) (Migration, error) {
	Row := Repo.Pool.QueryRow(Ctx, "SELECT "+MigrationColumns+" FROM migrations M WHERE M.database_id = $1 AND M.id = $2", DatabaseID, ID)
	return scanMigration(Row)
}

func (Repo *Repository) GetByVersion(Ctx Context.Context, DatabaseID string, Version int64) (Migration, error) {
	Row := Repo.Pool.QueryRow(Ctx, "SELECT "+MigrationColumns+" FROM migrations M WHERE M.database_id = $1 AND M.version = $2", DatabaseID, Version)
	return scanMigration(Row)
}

func (Repo *Repository) List(Ctx Context.Context, DatabaseID string) ([]Migration, error) {
	Rows, Err := Repo.Pool.Query(Ctx, "SELECT "+MigrationColumns+" FROM migrations M WHERE M.database_id = $1 ORDER BY M.version ASC", DatabaseID)
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

	Row := Repo.Pool.QueryRow(Ctx, `UPDATE migrations SET title = $3, sql_text = $4, updated_at = CURRENT_TIMESTAMP
WHERE database_id = $1 AND id = $2 AND status IN ('draft', 'pending') RETURNING `+MigrationReturning, DatabaseID, ID, Input.Title, Input.SQLText)

	return scanMigration(Row)
}

func (Repo *Repository) RecordExecution(Ctx Context.Context, DatabaseID string, ID string, ExecutedBy *string, Succeeded bool, ExecutionErr *string) (Migration, error) {
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
	}

	Row := Repo.Pool.QueryRow(Ctx, `UPDATE migrations SET status = $3, error_message = $4, executed_by = $5, executed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE database_id = $1 AND id = $2 RETURNING `+MigrationReturning, DatabaseID, ID, string(NewStatus), ExecutionErr, ExecutedBy)

	return scanMigration(Row)
}

func (Repo *Repository) DeleteDraft(Ctx Context.Context, DatabaseID string, ID string) error {
	Current, Err := Repo.Get(Ctx, DatabaseID, ID)
	if Err != nil {
		return Err
	}
	if !Current.Status.IsDraft() {
		return ErrAlreadyExecuted
	}

	CommandTag, Err := Repo.Pool.Exec(Ctx, "DELETE FROM migrations WHERE database_id = $1 AND id = $2 AND status IN ('draft', 'pending')", DatabaseID, ID)
	if Err != nil {
		return ErrUnavailable
	}
	if CommandTag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

