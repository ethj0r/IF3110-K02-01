package Migration

import (
	Errors "errors"
	Strings "strings"
	Testing "testing"
)

func TestStatusHelpers(T *Testing.T) {
	if !StatusDraft.IsDraft() || StatusPending.IsDraft() {
		T.Fatal("only draft may be edited or deleted")
	}
	if StatusCommitted.IsDraft() || StatusRolledBack.IsDraft() {
		T.Fatal("committed and rolled_back are not draft")
	}

	if !StatusCommitted.IsSuccess() || !StatusApplied.IsSuccess() {
		T.Fatal("committed and applied must be success")
	}
	if StatusRolledBack.IsSuccess() || StatusDraft.IsSuccess() {
		T.Fatal("rolled back and draft are not success")
	}

	if !StatusRolledBack.IsFailure() || !StatusFailed.IsFailure() {
		T.Fatal("rolled_back and failed must be failure")
	}
	if StatusCommitted.IsFailure() || StatusDraft.IsFailure() {
		T.Fatal("committed and draft are not failure")
	}

	if !StatusCommitted.IsTerminal() || !StatusRolledBack.IsTerminal() || !StatusApplied.IsTerminal() || !StatusFailed.IsTerminal() {
		T.Fatal("committed, rolled_back, applied, and failed must be terminal")
	}
	if StatusDraft.IsTerminal() || StatusPending.IsTerminal() {
		T.Fatal("draft and pending must not be terminal")
	}
}

func TestMigrationInputValidationAndNormalization(T *Testing.T) {
	Valid := MigrationInput{
		Title:   "  Add users table  ",
		SQLText: "  CREATE TABLE users (id SERIAL);  ",
	}
	if Err := Valid.Validate(); Err != nil {
		T.Fatalf("expected valid input, got %v", Err)
	}

	Normalized := Valid.Normalized()
	if Normalized.Title != "Add users table" || Normalized.SQLText != "CREATE TABLE users (id SERIAL);" {
		T.Fatalf("unexpected normalized output: %+v", Normalized)
	}

	for _, Invalid := range []MigrationInput{
		{Title: "", SQLText: "SELECT 1;"},
		{Title: "   ", SQLText: "SELECT 1;"},
		{Title: Strings.Repeat("A", 129), SQLText: "SELECT 1;"},
		{Title: "Valid Title", SQLText: ""},
		{Title: "Valid Title", SQLText: "    "},
	} {
		if Err := Invalid.Validate(); !Errors.Is(Err, ErrInvalidInput) {
			T.Fatalf("expected ErrInvalidInput for %+v, got %v", Invalid, Err)
		}
	}
}
