//go:build integration

package Migration

import (
	Context "context"
	Errors "errors"
	Fmt "fmt"
	Sync "sync"
	Testing "testing"

	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
	Testutil "github.com/ethj0r/IF3110-K02-01/backend/internal/testutil"
	Migrations "github.com/ethj0r/IF3110-K02-01/backend/migrations"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
	Stdlib "github.com/jackc/pgx/v5/stdlib"
	Goose "github.com/pressly/goose/v3"
)

func NewMigrationDatabase(T *Testing.T) *Pgxpool.Pool {
	T.Helper()
	Pool := Testutil.Database(T)
	Db := Stdlib.OpenDB(*Pool.Config().ConnConfig)
	defer Db.Close()
	Provider, Err := Goose.NewProvider(Goose.DialectPostgres, Db, Migrations.FS())
	if Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Provider.Up(Context.Background()); Err != nil {
		T.Fatal(Err)
	}
	return Pool
}

func setupFixture(T *Testing.T, Pool *Pgxpool.Pool) (string, string) {
	T.Helper()
	Ctx := Context.Background()
	var UserID string
	if Err := Pool.QueryRow(Ctx, "INSERT INTO users (username, password_hash) VALUES ('test_migrator', 'test-hash') RETURNING id").Scan(&UserID); Err != nil {
		T.Fatal(Err)
	}
	var DBID string
	if Err := Pool.QueryRow(Ctx, "INSERT INTO db_connections (name, host, port, dbname, username, password_ciphertext, created_by) VALUES ('target_one', '127.0.0.1', 5432, 'target_one', 'postgres', '\\x01', $1) RETURNING id", UserID).Scan(&DBID); Err != nil {
		T.Fatal(Err)
	}
	return DBID, UserID
}

func recordSucceeded(T *Testing.T, Repo *Repository, Ctx Context.Context, DBID, UserID, MigrationID, SQL string) Execution {
	T.Helper()
	Exec, Err := Repo.CreateExecution(Ctx, DBID, MigrationID, ExecutionRequest{Kind: KindApply, Direction: DirectionUp, SQLText: SQL, RequestedBy: &UserID})
	if Err != nil {
		T.Fatalf("create execution: %v", Err)
	}
	Attempt, Err := Repo.CreateAttempt(Ctx, Exec.ID, &UserID, SQL)
	if Err != nil {
		T.Fatalf("create attempt: %v", Err)
	}
	if Err := Repo.FinishAttempt(Ctx, Attempt.ID, AttemptSucceeded, nil); Err != nil {
		T.Fatalf("finish attempt: %v", Err)
	}
	if Err := Repo.FinishExecution(Ctx, Exec.ID, ExecutionSucceeded, nil); Err != nil {
		T.Fatalf("finish execution: %v", Err)
	}
	if _, Err := Repo.RefreshStatus(Ctx, DBID, MigrationID); Err != nil {
		T.Fatalf("refresh status: %v", Err)
	}
	return Exec
}

func TestRepositoryLifecycleAndExecutions(T *Testing.T) {
	Pool := NewMigrationDatabase(T)
	Repo := &Repository{Pool: Pool}
	Ctx := Context.Background()
	DBID, UserID := setupFixture(T, Pool)

	M1, Err := Repo.Create(Ctx, DBID, MigrationInput{Title: "Create table A", SQLText: "CREATE TABLE a (id INT);", DownSQL: "DROP TABLE a;"}, &UserID)
	if Err != nil {
		T.Fatalf("failed to create m1: %v", Err)
	}
	if M1.Version != 1 || M1.Status != StatusDraft || M1.Revision != 1 || M1.DownSQL == nil {
		T.Fatalf("unexpected m1: %+v", M1)
	}
	M2, Err := Repo.Create(Ctx, DBID, MigrationInput{Title: "Create table B", SQLText: "CREATE TABLE b (id INT);", DownSQL: "DROP TABLE b;"}, &UserID)
	if Err != nil {
		T.Fatalf("failed to create m2: %v", Err)
	}
	if M2.Version != 2 || M2.Status != StatusDraft {
		T.Fatalf("unexpected m2: %+v", M2)
	}

	Updated1, Err := Repo.UpdateDraft(Ctx, DBID, M1.ID, MigrationInput{Title: "Create table A updated", SQLText: "CREATE TABLE a (id BIGINT);", DownSQL: "DROP TABLE a;"})
	if Err != nil || Updated1.Title != "Create table A updated" || Updated1.Revision != 2 {
		T.Fatalf("failed to update draft m1: %v", Err)
	}

	recordSucceeded(T, Repo, Ctx, DBID, UserID, M1.ID, Updated1.SQLText)

	Applied, Err := Repo.Get(Ctx, DBID, M1.ID)
	if Err != nil || Applied.Status != StatusApplied || Applied.ExecutedAt == nil || Applied.LockedAt == nil {
		T.Fatalf("failed to refresh applied status: %+v %v", Applied, Err)
	}
	if Applied.ExecutedByUsername == nil || *Applied.ExecutedByUsername != "test_migrator" {
		T.Fatal("execution response is missing the actor username")
	}
	if _, Err := Repo.UpdateDraft(Ctx, DBID, M1.ID, MigrationInput{Title: "Hacked", SQLText: "DROP TABLE a;", DownSQL: "SELECT 1;"}); !Errors.Is(Err, ErrLocked) {
		T.Fatalf("expected ErrLocked when updating executed migration, got %v", Err)
	}
	if Err := Repo.DeleteDraft(Ctx, DBID, M1.ID); !Errors.Is(Err, ErrLocked) {
		T.Fatalf("expected ErrLocked when deleting executed migration, got %v", Err)
	}

	FailedExec, Err := Repo.CreateExecution(Ctx, DBID, M2.ID, ExecutionRequest{Kind: KindApply, Direction: DirectionUp, SQLText: M2.SQLText, RequestedBy: &UserID})
	if Err != nil {
		T.Fatal(Err)
	}
	Message := Driver.ErrExecution.Error()
	Attempt, Err := Repo.CreateAttempt(Ctx, FailedExec.ID, &UserID, M2.SQLText)
	if Err != nil {
		T.Fatal(Err)
	}
	if Err := Repo.FinishAttempt(Ctx, Attempt.ID, AttemptFailed, &Message); Err != nil {
		T.Fatal(Err)
	}
	if Err := Repo.FinishExecution(Ctx, FailedExec.ID, ExecutionFailed, &Message); Err != nil {
		T.Fatal(Err)
	}
	Failed, Err := Repo.RefreshStatus(Ctx, DBID, M2.ID)
	if Err != nil || Failed.Status != StatusFailed || Failed.ErrorMessage == nil || *Failed.ErrorMessage != Driver.ErrExecution.Error() {
		T.Fatalf("failed to record failed execution m2: %+v %v", Failed, Err)
	}

	History, Err := Repo.ListHistory(Ctx, DBID, false)
	if Err != nil || len(History) != 2 {
		T.Fatalf("expected two executions, got %v (len %d)", Err, len(History))
	}
	if len(History[0].Attempts) != 1 || History[0].Attempts[0].Status != AttemptSucceeded {
		T.Fatalf("attempt history missing: %+v", History[0])
	}

	Rollback, Err := Repo.CreateExecution(Ctx, DBID, M1.ID, ExecutionRequest{Kind: KindRollback, Direction: DirectionDown, SQLText: *M1.DownSQL, RollbackOf: &History[0].ID, RequestedBy: &UserID})
	if Err != nil {
		T.Fatal(Err)
	}
	RollbackAttempt, _ := Repo.CreateAttempt(Ctx, Rollback.ID, &UserID, *M1.DownSQL)
	_ = Repo.FinishAttempt(Ctx, RollbackAttempt.ID, AttemptSucceeded, nil)
	_ = Repo.FinishExecution(Ctx, Rollback.ID, ExecutionSucceeded, nil)
	if _, Err := Repo.RefreshStatus(Ctx, DBID, M1.ID); Err != nil {
		T.Fatal(Err)
	}
	Full, _ := Repo.ListHistory(Ctx, DBID, false)
	Hidden, _ := Repo.ListHistory(Ctx, DBID, true)
	if len(Full) != 3 || len(Hidden) != 1 {
		T.Fatalf("unexpected history lengths full=%d hidden=%d", len(Full), len(Hidden))
	}
	for _, Item := range Hidden {
		if Item.ID == History[0].ID || Item.ID == Rollback.ID {
			T.Fatalf("hide rollback kept a cancelled execution: %+v", Item)
		}
	}

	M3, Err := Repo.Create(Ctx, DBID, MigrationInput{Title: "Temporary draft", SQLText: "SELECT 1;", DownSQL: "SELECT 1;"}, &UserID)
	if Err != nil || M3.Version != 3 {
		T.Fatalf("failed to create m3: %v", Err)
	}
	if Err := Repo.DeleteDraft(Ctx, DBID, M3.ID); Err != nil {
		T.Fatalf("failed to delete draft m3: %v", Err)
	}
	if _, Err := Repo.Get(Ctx, DBID, M3.ID); !Errors.Is(Err, ErrNotFound) {
		T.Fatalf("expected ErrNotFound for deleted m3, got %v", Err)
	}
}

func TestConcurrentMigrationCreationLinearity(T *Testing.T) {
	Pool := NewMigrationDatabase(T)
	Repo := &Repository{Pool: Pool}
	Ctx := Context.Background()
	DBID, UserID := setupFixture(T, Pool)

	const Concurrency = 10
	var WG Sync.WaitGroup
	Results := make([]Migration, Concurrency)
	ErrorsList := make([]error, Concurrency)

	for i := 0; i < Concurrency; i++ {
		Index := i
		WG.Add(1)
		go func() {
			defer WG.Done()
			Title := Fmt.Sprintf("Concurrent migration #%d", Index)
			SQL := Fmt.Sprintf("CREATE TABLE concurrent_%d (id INT);", Index)
			Item, Err := Repo.Create(Ctx, DBID, MigrationInput{Title: Title, SQLText: SQL, DownSQL: "DROP TABLE IF EXISTS concurrent_" + Fmt.Sprint(Index) + ";"}, &UserID)
			Results[Index] = Item
			ErrorsList[Index] = Err
		}()
	}
	WG.Wait()

	for i, Err := range ErrorsList {
		if Err != nil {
			T.Fatalf("goroutine %d failed to create migration: %v", i, Err)
		}
	}

	VersionsSeen := make(map[int64]bool)
	for _, Item := range Results {
		if Item.Version < 1 || Item.Version > Concurrency {
			T.Fatalf("unexpected version %d for concurrent migration", Item.Version)
		}
		if VersionsSeen[Item.Version] {
			T.Fatalf("duplicate version %d encountered in concurrent creation", Item.Version)
		}
		VersionsSeen[Item.Version] = true
	}

	List, Err := Repo.List(Ctx, DBID)
	if Err != nil || len(List) != Concurrency {
		T.Fatalf("expected %d migrations in list, got %v (len %d)", Concurrency, Err, len(List))
	}
	for i := 0; i < Concurrency; i++ {
		if List[i].Version != int64(i+1) {
			T.Fatalf("index %d expected version %d, got %d", i, i+1, List[i].Version)
		}
	}
}
