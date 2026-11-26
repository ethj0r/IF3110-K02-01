//go:build integration

package Migration

import (
	Context "context"
	Errors "errors"
	Testing "testing"

	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
)

func TestFailedForwardKeepsMigrationRolledBack(T *Testing.T) {
	Platform := NewMigrationDatabase(T)
	Target := NewMigrationDatabase(T)
	Ctx := Context.Background()
	DBID, UserID := setupFixture(T, Platform)
	User := Auth.Principal{ID: UserID, Username: "test_migrator"}
	Service := reconcileService(Platform, Target)
	B, Err := Service.Create(Ctx, User, DBID, MigrationInput{Title: "B", SQLText: "CREATE TABLE linear_b (id INT);", DownSQL: "DROP TABLE linear_b;"})
	if Err != nil {
		T.Fatal(Err)
	}
	C, Err := Service.Create(Ctx, User, DBID, MigrationInput{Title: "C", SQLText: "CREATE TABLE linear_c (id INT);", DownSQL: "DROP TABLE linear_c;"})
	if Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Service.Apply(Ctx, User, DBID, B.ID); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Service.Rollback(Ctx, User, DBID, B.ID); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Target.Exec(Ctx, "CREATE TABLE linear_b (id INT)"); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Service.Forward(Ctx, User, DBID, B.ID); !Errors.Is(Err, Driver.ErrExecution) {
		T.Fatalf("forward should fail on the conflicting table, got %v", Err)
	}
	if Item, Err := Service.Repo.Get(Ctx, DBID, B.ID); Err != nil || Item.Status != StatusRolledBack {
		T.Fatalf("a failed forward must leave B rolled back: %+v %v", Item, Err)
	}
	if _, Err := Service.Apply(Ctx, User, DBID, C.ID); !Errors.Is(Err, ErrOutOfOrder) {
		T.Fatalf("C must wait while B is rolled back, got %v", Err)
	}
	if _, Err := Target.Exec(Ctx, "DROP TABLE linear_b"); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Service.Forward(Ctx, User, DBID, B.ID); Err != nil {
		T.Fatalf("B can be forwarded again after a failed forward: %v", Err)
	}
	if _, Err := Service.Apply(Ctx, User, DBID, C.ID); Err != nil {
		T.Fatalf("C applies after B is forwarded: %v", Err)
	}
}

func TestFailedRollbackCanBeRepeatedLater(T *Testing.T) {
	Platform := NewMigrationDatabase(T)
	Target := NewMigrationDatabase(T)
	Ctx := Context.Background()
	DBID, UserID := setupFixture(T, Platform)
	User := Auth.Principal{ID: UserID, Username: "test_migrator"}
	Service := reconcileService(Platform, Target)
	B, Err := Service.Create(Ctx, User, DBID, MigrationInput{Title: "B", SQLText: "CREATE TABLE repeat_b (id INT);", DownSQL: "DROP TABLE repeat_b;"})
	if Err != nil {
		T.Fatal(Err)
	}
	C, Err := Service.Create(Ctx, User, DBID, MigrationInput{Title: "C", SQLText: "CREATE TABLE repeat_c (id INT);", DownSQL: "DROP TABLE repeat_c;"})
	if Err != nil {
		T.Fatal(Err)
	}
	ApplyB, Err := Service.Apply(Ctx, User, DBID, B.ID)
	if Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Service.Apply(Ctx, User, DBID, C.ID); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Service.Rollback(Ctx, User, DBID, C.ID); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Target.Exec(Ctx, "CREATE VIEW repeat_b_view AS SELECT id FROM repeat_b"); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Service.Rollback(Ctx, User, DBID, B.ID); !Errors.Is(Err, Driver.ErrExecution) {
		T.Fatalf("rollback should fail on the dependent view, got %v", Err)
	}
	if Item, Err := Service.Repo.Get(Ctx, DBID, B.ID); Err != nil || Item.Status != StatusApplied {
		T.Fatalf("a failed rollback must leave B applied: %+v %v", Item, Err)
	}
	// Continue with another migration, then come back to B: the failed rollback
	// is no longer the last execution, so a retry is out of order, but a new
	// rollback must still be possible.
	if _, Err := Service.Forward(Ctx, User, DBID, C.ID); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Service.Rollback(Ctx, User, DBID, C.ID); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Target.Exec(Ctx, "DROP VIEW repeat_b_view"); Err != nil {
		T.Fatal(Err)
	}
	Items, History, Err := Service.loadState(Ctx, Service.Repo, DBID)
	if Err != nil {
		T.Fatal(Err)
	}
	for _, Item := range AnnotateCapabilities(Items, History) {
		if Item.ID == B.ID && !Item.CanRollback {
			T.Fatalf("B should offer rollback again: %+v", Item)
		}
	}
	Done, Err := Service.Rollback(Ctx, User, DBID, B.ID)
	if Err != nil || Done.RollbackOfExecutionID == nil || *Done.RollbackOfExecutionID != ApplyB.ID {
		T.Fatalf("B rollback should undo its apply: %+v %v", Done, Err)
	}
	Hidden, Err := Service.History(Ctx, User, DBID, true)
	if Err != nil {
		T.Fatal(Err)
	}
	Failed := 0
	for _, Item := range Hidden {
		if Item.ID == ApplyB.ID || Item.ID == Done.ID {
			T.Fatalf("hide rollback must hide the undone pair: %+v", Item)
		}
		if Item.Status == ExecutionFailed {
			Failed++
		}
	}
	if Failed != 1 {
		T.Fatalf("the failed rollback must stay visible, got %d failures", Failed)
	}
}
