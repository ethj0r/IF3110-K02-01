//go:build integration

package Migration

import (
	Context "context"
	Errors "errors"
	Fmt "fmt"
	Sync "sync"
	Testing "testing"

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

func TestRepositoryLifecycleAndLinearVersions(T *Testing.T) {
	Pool := NewMigrationDatabase(T)
	Repo := &Repository{Pool: Pool}
	Ctx := Context.Background()

	DBID, UserID := setupFixture(T, Pool)

	// Create first migration
	M1, Err := Repo.Create(Ctx, DBID, MigrationInput{Title: "Create table A", SQLText: "CREATE TABLE a (id INT);"}, &UserID)
	if Err != nil {
		T.Fatalf("failed to create m1: %v", Err)
	}
	if M1.Version != 1 || M1.Status != StatusDraft || M1.Title != "Create table A" {
		T.Fatalf("unexpected m1: %+v", M1)
	}

	// Create second migration
	M2, Err := Repo.Create(Ctx, DBID, MigrationInput{Title: "Create table B", SQLText: "CREATE TABLE b (id INT);"}, &UserID)
	if Err != nil {
		T.Fatalf("failed to create m2: %v", Err)
	}
	if M2.Version != 2 || M2.Status != StatusDraft {
		T.Fatalf("unexpected m2: %+v", M2)
	}

	// Get by ID
	Fetched1, Err := Repo.Get(Ctx, DBID, M1.ID)
	if Err != nil || Fetched1.ID != M1.ID {
		T.Fatalf("failed to fetch m1: %v", Err)
	}

	// Get by Version
	FetchedV2, Err := Repo.GetByVersion(Ctx, DBID, 2)
	if Err != nil || FetchedV2.ID != M2.ID {
		T.Fatalf("failed to fetch m2 by version: %v", Err)
	}

	// List linear
	List, Err := Repo.List(Ctx, DBID)
	if Err != nil || len(List) != 2 {
		T.Fatalf("expected 2 migrations in list, got %v (len %d)", Err, len(List))
	}
	if List[0].Version != 1 || List[1].Version != 2 {
		T.Fatalf("migrations list not in linear version order: %+v", List)
	}

	// Update draft
	Updated1, Err := Repo.UpdateDraft(Ctx, DBID, M1.ID, MigrationInput{Title: "Create table A updated", SQLText: "CREATE TABLE a (id BIGINT);"})
	if Err != nil || Updated1.Title != "Create table A updated" {
		T.Fatalf("failed to update draft m1: %v", Err)
	}

	// Record execution: commit M1
	Committed1, Err := Repo.RecordExecution(Ctx, DBID, M1.ID, &UserID, true, nil)
	if Err != nil || Committed1.Status != StatusCommitted || Committed1.ExecutedAt == nil {
		T.Fatalf("failed to commit execution m1: %v", Err)
	}

	// Updating or deleting committed migration must fail
	if _, Err := Repo.UpdateDraft(Ctx, DBID, M1.ID, MigrationInput{Title: "Hacked", SQLText: "DROP TABLE a;"}); !Errors.Is(Err, ErrAlreadyExecuted) {
		T.Fatalf("expected ErrAlreadyExecuted when updating committed migration, got %v", Err)
	}
	if Err := Repo.DeleteDraft(Ctx, DBID, M1.ID); !Errors.Is(Err, ErrAlreadyExecuted) {
		T.Fatalf("expected ErrAlreadyExecuted when deleting committed migration, got %v", Err)
	}

	// Record execution: rollback M2 with error message
	ErrMsg := "syntax error at or near INVALID"
	RolledBack2, Err := Repo.RecordExecution(Ctx, DBID, M2.ID, &UserID, false, &ErrMsg)
	if Err != nil || RolledBack2.Status != StatusRolledBack || RolledBack2.ErrorMessage == nil || *RolledBack2.ErrorMessage != ErrMsg {
		T.Fatalf("failed to record rolled back execution m2: %v", Err)
	}

	// Create and delete M3 draft
	M3, Err := Repo.Create(Ctx, DBID, MigrationInput{Title: "Temporary draft", SQLText: "SELECT 1;"}, &UserID)
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
			Item, Err := Repo.Create(Ctx, DBID, MigrationInput{Title: Title, SQLText: SQL}, &UserID)
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
		ExpectedVersion := int64(i + 1)
		if List[i].Version != ExpectedVersion {
			T.Fatalf("index %d expected version %d, got %d", i, ExpectedVersion, List[i].Version)
		}
	}
}
