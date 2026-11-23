package Migration

import (
	Context "context"
	Errors "errors"
	Time "time"

	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
)

type batchEvidence struct {
	CursorFrom int64
	CursorTo   int64
	RowCount   int64
}

func (Server *Service) ensureBatchEvidence(Ctx Context.Context, Target Driver.Conn) error {
	if Err := Target.Exec(Ctx, "CREATE SCHEMA IF NOT EXISTS platform_migration"); Err != nil {
		return ErrUnavailable
	}
	if Err := Target.Exec(Ctx, `CREATE TABLE IF NOT EXISTS platform_migration.batches (
execution_id UUID NOT NULL,
batch_no BIGINT NOT NULL,
direction TEXT NOT NULL,
cursor_from BIGINT NOT NULL,
cursor_to BIGINT NOT NULL,
row_count BIGINT NOT NULL,
attempt_no INTEGER NOT NULL,
created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
PRIMARY KEY (execution_id, batch_no)
)`); Err != nil {
		return ErrUnavailable
	}
	return nil
}

func readBatchEvidence(Ctx Context.Context, Target Driver.Conn, ExecutionID string, BatchNo int64) (batchEvidence, bool, error) {
	Rows, Err := Target.Query(Ctx, `SELECT cursor_from, cursor_to, row_count
FROM platform_migration.batches WHERE execution_id = $1::uuid AND batch_no = $2`, ExecutionID, BatchNo)
	if Err != nil {
		return batchEvidence{}, false, ErrUnavailable
	}
	defer Rows.Close()
	if !Rows.Next() {
		if Err := Rows.Err(); Err != nil {
			return batchEvidence{}, false, ErrUnavailable
		}
		return batchEvidence{}, false, nil
	}
	var Result batchEvidence
	if Err := Rows.Scan(&Result.CursorFrom, &Result.CursorTo, &Result.RowCount); Err != nil {
		return batchEvidence{}, false, ErrUnavailable
	}
	return Result, true, nil
}

func writeBatchEvidence(Ctx Context.Context, Target Driver.Tx, ExecutionID string, BatchNo int64, Direction string, CursorFrom int64, CursorTo int64, RowCount int64, AttemptNo int) error {
	return Target.Exec(Ctx, `INSERT INTO platform_migration.batches
(execution_id, batch_no, direction, cursor_from, cursor_to, row_count, attempt_no)
VALUES ($1::uuid, $2, $3, $4, $5, $6, $7)
ON CONFLICT (execution_id, batch_no) DO NOTHING`, ExecutionID, BatchNo, Direction, CursorFrom, CursorTo, RowCount, AttemptNo)
}

func (Server *Service) runBatchUp(Ctx Context.Context, Repo MigrationRepository, User Auth.Principal, Current Migration, Exec Execution, Attempt Attempt) (Execution, error) {
	if Exec.BatchSize == nil || *Exec.BatchSize < 1 {
		return Server.finishBatchFailure(Ctx, Repo, Exec, Attempt, ErrInvalidInput)
	}
	DSN, Err := Server.Resolver.DialTargetDSN(Ctx, Current.DatabaseID)
	if Err != nil {
		return Server.finishBatchFailure(Ctx, Repo, Exec, Attempt, Err)
	}
	Target, Err := Server.getDriver().Open(DSN)
	if Err != nil {
		return Server.finishBatchFailure(Ctx, Repo, Exec, Attempt, Err)
	}
	defer Target.Close()
	if Err := Server.ensureBatchEvidence(Ctx, Target); Err != nil {
		return Server.finishBatchFailure(Ctx, Repo, Exec, Attempt, Err)
	}
	Cursor := Exec.CheckpointCursor
	BatchNo := Exec.CommittedBatches + 1
	for {
		Started := Time.Now()
		Evidence, Found, Err := readBatchEvidence(Ctx, Target, Exec.ID, BatchNo)
		if Err != nil {
			return Server.finishBatchFailure(Ctx, Repo, Exec, Attempt, Err)
		}
		if Found {
			Exec, Err = Repo.RecordBatchUp(Ctx, Exec.ID, Current.DatabaseID, BatchNo, Evidence.CursorFrom, Evidence.CursorTo, Evidence.RowCount)
			if Err != nil {
				return Exec, ErrUnavailable
			}
			Server.logger().Info("migration batch reconciled from target evidence",
				"event", "migration.batch.reconciled", "database_id", Current.DatabaseID,
				"migration_id", Current.ID, "execution_id", Exec.ID, "batch", BatchNo,
				"cursor_from", Evidence.CursorFrom, "cursor_to", Evidence.CursorTo, "rows", Evidence.RowCount)
			Cursor = Evidence.CursorTo
			BatchNo++
			continue
		}
		TargetTx, Err := Target.Begin(Ctx)
		if Err != nil {
			return Server.finishBatchFailure(Ctx, Repo, Exec, Attempt, Err)
		}
		Rows, Err := TargetTx.Query(Ctx, Exec.SQLText, Cursor, *Exec.BatchSize)
		if Err != nil {
			_ = TargetTx.Rollback(Ctx)
			return Server.finishBatchFailure(Ctx, Repo, Exec, Attempt, Driver.ErrExecution)
		}
		Count := int64(0)
		CursorTo := Cursor
		for Rows.Next() {
			var Value int64
			if Err := Rows.Scan(&Value); Err != nil {
				Rows.Close()
				_ = TargetTx.Rollback(Ctx)
				return Server.finishBatchFailure(Ctx, Repo, Exec, Attempt, Driver.ErrExecution)
			}
			Count++
			if Value > CursorTo {
				CursorTo = Value
			}
		}
		if Err := Rows.Err(); Err != nil {
			Rows.Close()
			_ = TargetTx.Rollback(Ctx)
			return Server.finishBatchFailure(Ctx, Repo, Exec, Attempt, Driver.ErrExecution)
		}
		Rows.Close()
		if Count == 0 {
			_ = TargetTx.Rollback(Ctx)
			return Server.finishBatchSuccess(Ctx, Repo, Current, Exec, Attempt)
		}
		if Err := writeBatchEvidence(Ctx, TargetTx, Exec.ID, BatchNo, string(DirectionUp), Cursor, CursorTo, Count, Attempt.AttemptNo); Err != nil {
			_ = TargetTx.Rollback(Ctx)
			return Server.finishBatchFailure(Ctx, Repo, Exec, Attempt, Driver.ErrExecution)
		}
		if Err := TargetTx.Commit(Ctx); Err != nil {
			Server.logger().Error("migration batch commit response is uncertain",
				"event", "migration.batch.uncertain", "database_id", Current.DatabaseID,
				"migration_id", Current.ID, "execution_id", Exec.ID, "batch", BatchNo)
			return Exec, ErrUnavailable
		}
		Exec, Err = Repo.RecordBatchUp(Ctx, Exec.ID, Current.DatabaseID, BatchNo, Cursor, CursorTo, Count)
		if Err != nil {
			return Exec, ErrUnavailable
		}
		Server.logger().Info("migration batch committed",
			"event", "migration.batch.committed", "database_id", Current.DatabaseID,
			"migration_id", Current.ID, "execution_id", Exec.ID, "batch", BatchNo,
			"attempt", Attempt.AttemptNo, "cursor_from", Cursor, "cursor_to", CursorTo,
			"rows", Count, "duration_ms", Time.Since(Started).Milliseconds())
		Cursor = CursorTo
		BatchNo++
	}
}

func (Server *Service) runBatchDown(Ctx Context.Context, Repo MigrationRepository, User Auth.Principal, Current Migration, Exec Execution, Attempt Attempt) (Execution, error) {
	if Exec.RollbackOfExecutionID == nil {
		return Server.finishBatchFailure(Ctx, Repo, Exec, Attempt, ErrNotApplied)
	}
	DSN, Err := Server.Resolver.DialTargetDSN(Ctx, Current.DatabaseID)
	if Err != nil {
		return Server.finishBatchFailure(Ctx, Repo, Exec, Attempt, Err)
	}
	Target, Err := Server.getDriver().Open(DSN)
	if Err != nil {
		return Server.finishBatchFailure(Ctx, Repo, Exec, Attempt, Err)
	}
	defer Target.Close()
	if Err := Server.ensureBatchEvidence(Ctx, Target); Err != nil {
		return Server.finishBatchFailure(Ctx, Repo, Exec, Attempt, Err)
	}
	Batches, Err := Repo.ListCommittedBatches(Ctx, *Exec.RollbackOfExecutionID)
	if Err != nil {
		return Server.finishBatchFailure(Ctx, Repo, Exec, Attempt, Err)
	}
	for _, Batch := range Batches {
		Started := Time.Now()
		_, Found, Err := readBatchEvidence(Ctx, Target, Exec.ID, Batch.BatchNo)
		if Err != nil {
			return Server.finishBatchFailure(Ctx, Repo, Exec, Attempt, Err)
		}
		if Found {
			Exec, Err = Repo.RecordBatchReverted(Ctx, Exec.ID, *Exec.RollbackOfExecutionID, Current.DatabaseID, Batch.BatchNo)
			if Err != nil {
				return Exec, ErrUnavailable
			}
			Server.logger().Info("migration batch revert reconciled from target evidence",
				"event", "migration.batch.revert_reconciled", "database_id", Current.DatabaseID,
				"migration_id", Current.ID, "execution_id", Exec.ID, "batch", Batch.BatchNo)
			continue
		}
		TargetTx, Err := Target.Begin(Ctx)
		if Err != nil {
			return Server.finishBatchFailure(Ctx, Repo, Exec, Attempt, Err)
		}
		if Err := TargetTx.Exec(Ctx, Exec.SQLText, Batch.CursorTo, Batch.CursorFrom); Err != nil {
			_ = TargetTx.Rollback(Ctx)
			return Server.finishBatchFailure(Ctx, Repo, Exec, Attempt, Driver.ErrExecution)
		}
		if Err := writeBatchEvidence(Ctx, TargetTx, Exec.ID, Batch.BatchNo, string(DirectionDown), Batch.CursorFrom, Batch.CursorTo, Batch.RowCount, Attempt.AttemptNo); Err != nil {
			_ = TargetTx.Rollback(Ctx)
			return Server.finishBatchFailure(Ctx, Repo, Exec, Attempt, Driver.ErrExecution)
		}
		if Err := TargetTx.Commit(Ctx); Err != nil {
			Server.logger().Error("migration batch revert commit response is uncertain",
				"event", "migration.batch.uncertain", "database_id", Current.DatabaseID,
				"migration_id", Current.ID, "execution_id", Exec.ID, "batch", Batch.BatchNo)
			return Exec, ErrUnavailable
		}
		Exec, Err = Repo.RecordBatchReverted(Ctx, Exec.ID, *Exec.RollbackOfExecutionID, Current.DatabaseID, Batch.BatchNo)
		if Err != nil {
			return Exec, ErrUnavailable
		}
		Server.logger().Info("migration batch reverted",
			"event", "migration.batch.reverted", "database_id", Current.DatabaseID,
			"migration_id", Current.ID, "execution_id", Exec.ID, "batch", Batch.BatchNo,
			"attempt", Attempt.AttemptNo, "cursor_from", Batch.CursorFrom, "cursor_to", Batch.CursorTo,
			"rows", Batch.RowCount, "duration_ms", Time.Since(Started).Milliseconds())
	}
	return Server.finishBatchSuccess(Ctx, Repo, Current, Exec, Attempt)
}

func (Server *Service) finishBatchSuccess(Ctx Context.Context, Repo MigrationRepository, Current Migration, Exec Execution, Attempt Attempt) (Execution, error) {
	RecordCtx, Cancel := Context.WithTimeout(Context.WithoutCancel(Ctx), 5*Time.Second)
	defer Cancel()
	if Err := Repo.FinishAttempt(RecordCtx, Attempt.ID, AttemptSucceeded, nil); Err != nil {
		return Exec, ErrUnavailable
	}
	if Err := Repo.FinishExecution(RecordCtx, Exec.ID, ExecutionSucceeded, nil); Err != nil {
		return Exec, ErrUnavailable
	}
	if _, Err := Repo.RefreshStatus(RecordCtx, Current.DatabaseID, Current.ID); Err != nil {
		return Exec, ErrUnavailable
	}
	Updated, Err := Repo.GetExecution(RecordCtx, Current.DatabaseID, Exec.ID)
	if Err != nil {
		return Exec, ErrUnavailable
	}
	Server.logger().Info("migration batch execution succeeded",
		"event", "migration.execution.succeeded", "database_id", Current.DatabaseID,
		"migration_id", Current.ID, "execution_id", Exec.ID, "direction", Exec.Direction,
		"committed_batches", Updated.CommittedBatches, "committed_rows", Updated.CommittedRows,
		"reverted_batches", Updated.RevertedBatches, "reverted_rows", Updated.RevertedRows)
	return Updated, nil
}

func (Server *Service) finishBatchFailure(Ctx Context.Context, Repo MigrationRepository, Exec Execution, Attempt Attempt, Cause error) (Execution, error) {
	RecordCtx, Cancel := Context.WithTimeout(Context.WithoutCancel(Ctx), 5*Time.Second)
	defer Cancel()
	Safe := safeMessage(Cause)
	if Err := Repo.FinishAttempt(RecordCtx, Attempt.ID, AttemptFailed, Safe); Err != nil {
		return Exec, ErrUnavailable
	}
	Partial := false
	if Exec.Direction == DirectionUp {
		Partial = Exec.CommittedBatches > 0
	} else {
		Partial = Exec.RevertedBatches > 0
	}
	Status := ExecutionFailed
	if Partial {
		if Exec.Direction == DirectionUp {
			Status = ExecutionPartial
		} else {
			Status = ExecutionPartiallyReverted
		}
	}
	if Err := Repo.FinishExecution(RecordCtx, Exec.ID, Status, Safe); Err != nil {
		return Exec, ErrUnavailable
	}
	if _, Err := Repo.RefreshStatus(RecordCtx, Exec.DatabaseID, Exec.MigrationID); Err != nil {
		return Exec, ErrUnavailable
	}
	Updated, Err := Repo.GetExecution(RecordCtx, Exec.DatabaseID, Exec.ID)
	if Errors.Is(Err, ErrNotFound) {
		Updated = Exec
	}
	Server.logger().Error("migration batch execution failed",
		"event", "migration.batch.failed", "database_id", Exec.DatabaseID,
		"migration_id", Exec.MigrationID, "execution_id", Exec.ID,
		"direction", Exec.Direction, "status", Status,
		"committed_batches", Updated.CommittedBatches, "committed_rows", Updated.CommittedRows,
		"reverted_batches", Updated.RevertedBatches, "reverted_rows", Updated.RevertedRows,
		"error", *Safe)
	return Updated, Cause
}
