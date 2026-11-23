package Migration

import (
	Errors "errors"
	Strings "strings"
	Testing "testing"
)

func TestStatusHelpers(T *Testing.T) {
	if !StatusDraft.IsDraft() || StatusApplied.IsDraft() || StatusRolledBack.IsDraft() {
		T.Fatal("only draft may be edited or deleted")
	}
	if !StatusApplied.IsSuccess() || !StatusCommitted.IsSuccess() {
		T.Fatal("applied and legacy committed are success")
	}
	if StatusRolledBack.IsSuccess() || StatusFailed.IsSuccess() || StatusDraft.IsSuccess() {
		T.Fatal("rolled back, failed, and draft are not success")
	}
	if !StatusFailed.IsFailure() {
		T.Fatal("failed must be a failure")
	}
	if StatusRolledBack.IsFailure() || StatusApplied.IsFailure() {
		T.Fatal("rolled back and applied are not execution failures")
	}
	if !StatusCommitted.IsTerminal() || !StatusRolledBack.IsTerminal() || !StatusApplied.IsTerminal() || !StatusFailed.IsTerminal() {
		T.Fatal("applied, committed, rolled_back, and failed must be terminal")
	}
	if StatusDraft.IsTerminal() || StatusPending.IsTerminal() {
		T.Fatal("draft and pending must not be terminal")
	}
	if !StatusApplied.IsApplied() || StatusRolledBack.IsApplied() {
		T.Fatal("applied helper is incorrect")
	}
	if !StatusRolledBack.IsRolledBack() || StatusApplied.IsRolledBack() {
		T.Fatal("rolled back helper is incorrect")
	}
}

func TestMigrationInputValidationAndNormalization(T *Testing.T) {
	Valid := MigrationInput{
		Title:   "  Add users table  ",
		SQLText: "  CREATE TABLE users (id SERIAL);  ",
		DownSQL: "  DROP TABLE users;  ",
	}
	if Err := Valid.Validate(); Err != nil {
		T.Fatalf("expected valid input, got %v", Err)
	}
	Normalized := Valid.Normalized()
	if Normalized.Title != "Add users table" || Normalized.SQLText != "CREATE TABLE users (id SERIAL);" || Normalized.DownSQL != "DROP TABLE users;" {
		T.Fatalf("unexpected normalized output: %+v", Normalized)
	}

	Batch := MigrationInput{
		Title:       "Batch update",
		SQLText:     "UPDATE t SET v = v + 1 WHERE id > $1 ORDER BY id LIMIT $2 RETURNING id",
		DownSQL:     "UPDATE t SET v = v - 1 WHERE id > $2 AND id <= $1",
		BatchMode:   true,
		BatchColumn: "  id  ",
		BatchSize:   1000,
	}
	if Err := Batch.Validate(); Err != nil {
		T.Fatalf("valid batch migration rejected: %v", Err)
	}
	if Batch.Normalized().BatchColumn != "id" {
		T.Fatal("batch column was not normalized")
	}

	Irreversible := MigrationInput{
		Title:              "Remove data",
		SQLText:            "DELETE FROM users;",
		Irreversible:       true,
		IrreversibleReason: "Deleted rows cannot be restored",
	}
	if Err := Irreversible.Validate(); Err != nil {
		T.Fatalf("valid irreversible migration rejected: %v", Err)
	}
	if Irreversible.Normalized().IrreversibleReason != "Deleted rows cannot be restored" {
		T.Fatal("irreversible reason was not normalized")
	}

	for _, Invalid := range []MigrationInput{
		{Title: "", SQLText: "SELECT 1;", DownSQL: "SELECT 1;"},
		{Title: "   ", SQLText: "SELECT 1;", DownSQL: "SELECT 1;"},
		{Title: Strings.Repeat("A", 129), SQLText: "SELECT 1;", DownSQL: "SELECT 1;"},
		{Title: "Valid Title", SQLText: "", DownSQL: "SELECT 1;"},
		{Title: "Valid Title", SQLText: "    ", DownSQL: "SELECT 1;"},
		{Title: "Valid Title", SQLText: "SELECT 1;"},
		{Title: "Valid Title", SQLText: "SELECT 1;", DownSQL: "   "},
		{Title: "Valid Title", SQLText: "SELECT 1;", DownSQL: "SELECT 1;", Irreversible: true},
		{Title: "Valid Title", SQLText: "SELECT 1;", Irreversible: true},
		{Title: "Valid Title", SQLText: "SELECT 1;", Irreversible: true, IrreversibleReason: Strings.Repeat("x", 501)},
		{Title: "Valid Title", SQLText: "SELECT 1;", DownSQL: "SELECT 1;", BatchMode: true, BatchColumn: "", BatchSize: 10},
		{Title: "Valid Title", SQLText: "SELECT 1;", DownSQL: "SELECT 1;", BatchMode: true, BatchColumn: "id", BatchSize: 0},
		{Title: "Valid Title", SQLText: "SELECT 1;", DownSQL: "SELECT 1;", BatchMode: true, BatchColumn: "id", BatchSize: 1000001},
		{Title: "Valid Title", SQLText: "SELECT 1;", DownSQL: "SELECT 1;", BatchColumn: "id"},
		{Title: "Valid Title", SQLText: "SELECT 1;", DownSQL: "SELECT 1;", BatchSize: 10},
	} {
		if Err := Invalid.Validate(); !Errors.Is(Err, ErrInvalidInput) {
			T.Fatalf("expected ErrInvalidInput for %+v, got %v", Invalid, Err)
		}
	}
}

func TestExecutionStatusHelpers(T *Testing.T) {
	if !ExecutionFailed.IsFailure() || !ExecutionPartial.IsFailure() || !ExecutionPartiallyReverted.IsFailure() {
		T.Fatal("failure execution statuses must report failure")
	}
	if ExecutionSucceeded.IsFailure() || ExecutionRunning.IsFailure() {
		T.Fatal("success/running must not report failure")
	}
	if !ExecutionSucceeded.IsFinished() || !ExecutionFailed.IsFinished() || ExecutionRunning.IsFinished() {
		T.Fatal("finished helper is incorrect")
	}
}
