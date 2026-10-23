//go:build integration

package Migration

import (
	Context "context"
	Errors "errors"
	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
	Testing "testing"
)

func TestServiceRealDatabaseExecution(T *Testing.T) {
	PlatformPool := NewMigrationDatabase(T)
	TargetPool := NewMigrationDatabase(T)

	Ctx := Context.Background()
	DBID, UserID := setupFixture(T, PlatformPool)
	Editor := Auth.Principal{ID: UserID, Username: "test_migrator"}

	TargetDsnStr := TargetPool.Config().ConnConfig.ConnString()

	Service := &Service{
		Repo:   &Repository{Pool: PlatformPool},
		Access: Access.AllowAll(),
		Resolver: DSNResolverFunc(func(_ Context.Context, _ string) (string, error) {
			return TargetDsnStr, nil
		}),
		Driver: Driver.PostgresDriver{},
	}

	// 1. Valid migration: create real table
	ValidInput := MigrationInput{
		Title:   "Create real table",
		SQLText: "CREATE TABLE test_real_table (id SERIAL PRIMARY KEY, val TEXT);",
	}
	Created, Err := Service.Create(Ctx, Editor, DBID, ValidInput)
	if Err != nil {
		T.Fatalf("failed to create migration: %v", Err)
	}

	Executed, Err := Service.Execute(Ctx, Editor, DBID, Created.ID)
	if Err != nil {
		T.Fatalf("failed to execute real migration: %v", Err)
	}
	if Executed.Status != StatusCommitted || Executed.ExecutedAt == nil {
		T.Fatalf("expected committed status, got %+v", Executed)
	}

	// Verify table exists on target DB
	var Exists bool
	if Err := TargetPool.QueryRow(Ctx, "SELECT to_regclass('test_real_table') IS NOT NULL").Scan(&Exists); Err != nil || !Exists {
		T.Fatalf("target table test_real_table was not found in target database: %v", Err)
	}

	// 2. Broken SQL migration: verify rollback
	BrokenInput := MigrationInput{
		Title:   "Broken SQL migration",
		SQLText: "CREATE TABLE test_broken_table (id INT); INVALID SQL SYNTAX;",
	}
	CreatedBroken, Err := Service.Create(Ctx, Editor, DBID, BrokenInput)
	if Err != nil {
		T.Fatalf("failed to create broken migration: %v", Err)
	}

	ExecutedBroken, Err := Service.Execute(Ctx, Editor, DBID, CreatedBroken.ID)
	if !Errors.Is(Err, Driver.ErrExecution) {
		T.Fatalf("expected ErrExecution, got %v", Err)
	}
	if ExecutedBroken.Status != StatusRolledBack || ExecutedBroken.ErrorMessage == nil {
		T.Fatalf("expected rolled_back status with error recorded, got %+v", ExecutedBroken)
	}

	// Verify table was rolled back and does NOT exist in target database
	var BrokenTableExists bool
	if Err := TargetPool.QueryRow(Ctx, "SELECT to_regclass('test_broken_table') IS NOT NULL").Scan(&BrokenTableExists); Err != nil || BrokenTableExists {
		T.Fatalf("broken table should have been rolled back and not exist: %v", Err)
	}
}
