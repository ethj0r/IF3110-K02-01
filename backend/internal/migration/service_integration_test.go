//go:build integration

package Migration

import (
	Context "context"
	Errors "errors"
	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
	Pgx "github.com/jackc/pgx/v5"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
	Strings "strings"
	Testing "testing"
	Time "time"
)

type testTargetDriver struct {
	Pool     *Pgxpool.Pool
	Started  chan struct{}
	Continue <-chan struct{}
}

func (D testTargetDriver) Open(_ string) (Driver.Conn, error) {
	return &testTargetConn{testTargetDriver: D}, nil
}

type testTargetConn struct {
	testTargetDriver
}

type testTargetTx struct {
	Driver.Tx
	Started  chan struct{}
	Continue <-chan struct{}
}

func (Tx *testTargetTx) ExecScript(Ctx Context.Context, SQL string) error {
	if Tx.Started != nil {
		Tx.Started <- struct{}{}
		select {
		case <-Tx.Continue:
		case <-Ctx.Done():
			return Ctx.Err()
		}
	}
	return Tx.Tx.ExecScript(Ctx, SQL)
}

func (C *testTargetConn) Ping(Ctx Context.Context) error {
	return C.Pool.Ping(Ctx)
}

func (C *testTargetConn) Begin(Ctx Context.Context) (Driver.Tx, error) {
	RawTx, Err := C.Pool.Begin(Ctx)
	if Err != nil {
		return nil, Driver.ErrUnavailable
	}
	return &testTargetTx{Tx: &Driver.PostgresTx{Raw: RawTx}, Started: C.Started, Continue: C.Continue}, nil
}

func (C *testTargetConn) Query(Ctx Context.Context, SQL string, Args ...any) (Pgx.Rows, error) {
	return C.Pool.Query(Ctx, SQL, Args...)
}

func (C *testTargetConn) Exec(Ctx Context.Context, SQL string, Args ...any) error {
	_, Err := C.Pool.Exec(Ctx, SQL, Args...)
	return Err
}

func (C *testTargetConn) Close() {}

func TestServiceRealDatabaseExecution(T *Testing.T) {
	PlatformPool := NewMigrationDatabase(T)
	TargetPool := NewMigrationDatabase(T)

	Ctx := Context.Background()
	DBID, UserID := setupFixture(T, PlatformPool)
	Editor := Auth.Principal{ID: UserID, Username: "test_migrator"}

	Service := &Service{
		Repo:   &Repository{Pool: PlatformPool},
		Access: Access.AllowAll(),
		Resolver: DSNResolverFunc(func(_ Context.Context, _ string) (string, error) {
			return "postgres://target-db", nil
		}),
		Driver: testTargetDriver{Pool: TargetPool},
	}

	// 1. Valid migration: create real table
	ValidInput := MigrationInput{
		Title: "Create real table",
		SQLText: `CREATE TABLE test_real_table (id SERIAL PRIMARY KEY, val TEXT);
INSERT INTO test_real_table(val) VALUES ($value$quote ' and $$ and COMMIT; stay text$value$);`,
		DownSQL: "DROP TABLE test_real_table;",
	}
	Created, Err := Service.Create(Ctx, Editor, DBID, ValidInput)
	if Err != nil {
		T.Fatalf("failed to create migration: %v", Err)
	}

	Executed, Err := Service.Execute(Ctx, Editor, DBID, Created.ID)
	if Err != nil {
		T.Fatalf("failed to execute real migration: %v", Err)
	}
	if Executed.Status != ExecutionSucceeded || Executed.FinishedAt == nil || len(Executed.Attempts) != 1 {
		T.Fatalf("expected committed execution, got %+v", Executed)
	}

	// Verify table exists on target DB
	var Exists bool
	if Err := TargetPool.QueryRow(Ctx, "SELECT to_regclass('test_real_table') IS NOT NULL").Scan(&Exists); Err != nil || !Exists {
		T.Fatalf("target table test_real_table was not found in target database: %v", Err)
	}
	var Value string
	if Err := TargetPool.QueryRow(Ctx, "SELECT val FROM test_real_table").Scan(&Value); Err != nil || Value != "quote ' and $$ and COMMIT; stay text" {
		T.Fatalf("SQL quoting changed the migration value: %q %v", Value, Err)
	}

	// 2. Broken SQL migration: verify rollback
	BrokenInput := MigrationInput{
		Title:   "Broken SQL migration",
		SQLText: "CREATE TABLE test_broken_table (id INT); SELECT 1/0;",
		DownSQL: "DROP TABLE test_broken_table;",
	}
	CreatedBroken, Err := Service.Create(Ctx, Editor, DBID, BrokenInput)
	if Err != nil {
		T.Fatalf("failed to create broken migration: %v", Err)
	}

	ExecutedBroken, Err := Service.Execute(Ctx, Editor, DBID, CreatedBroken.ID)
	if !Errors.Is(Err, Driver.ErrExecution) {
		T.Fatalf("expected ErrExecution, got %v", Err)
	}
	if ExecutedBroken.Status != ExecutionFailed || ExecutedBroken.ErrorMessage == nil {
		T.Fatalf("expected failed execution with error recorded, got %+v", ExecutedBroken)
	}

	// Verify table was rolled back and does NOT exist in target database
	var BrokenTableExists bool
	if Err := TargetPool.QueryRow(Ctx, "SELECT to_regclass('test_broken_table') IS NOT NULL").Scan(&BrokenTableExists); Err != nil || BrokenTableExists {
		T.Fatalf("broken table should have been rolled back and not exist: %v", Err)
	}
	for _, Script := range []string{
		"CREATE TABLE escaped (id int); COMMIT; BEGIN; SELECT 1/0;",
		"CREATE TABLE escaped (id int); DO $body$ BEGIN COMMIT; END $body$;",
		"CREATE TABLE escaped (id int); DO $$ BEGIN RAISE EXCEPTION 'private-marker'; END $$;",
	} {
		Item, Err := Service.Create(Ctx, Editor, DBID, MigrationInput{Title: "Atomic script", SQLText: Script, DownSQL: "SELECT 1;"})
		if Err != nil {
			T.Fatal(Err)
		}
		Result, Err := Service.Execute(Ctx, Editor, DBID, Item.ID)
		if !Errors.Is(Err, Driver.ErrExecution) || Result.Status != ExecutionFailed || Result.ErrorMessage == nil || *Result.ErrorMessage != Driver.ErrExecution.Error() {
			T.Fatalf("script failure must be sanitized and rolled back: %+v %v", Result, Err)
		}
		if Err := TargetPool.QueryRow(Ctx, "SELECT to_regclass('escaped') IS NOT NULL").Scan(&Exists); Err != nil || Exists {
			T.Fatal("script escaped the transaction boundary")
		}
	}
	if _, Err := Service.Execute(Ctx, Editor, DBID, CreatedBroken.ID); !Errors.Is(Err, ErrOutOfOrder) {
		T.Fatalf("older failure was retried after later attempts: %v", Err)
	}
}

func TestConcurrentExecutionAndDraftMutations(T *Testing.T) {
	Platform, Target := NewMigrationDatabase(T), NewMigrationDatabase(T)
	DBID, UserID := setupFixture(T, Platform)
	User := Auth.Principal{ID: UserID}
	Cfg := Platform.Config().Copy()
	Cfg.MaxConns = 1
	Single, Err := Pgxpool.NewWithConfig(Context.Background(), Cfg)
	if Err != nil {
		T.Fatal(Err)
	}
	defer Single.Close()
	Repo := &Repository{Pool: Single}
	Started, Continue := make(chan struct{}, 1), make(chan struct{})
	Server := &Service{Repo: Repo, Access: Access.AllowAll(), Driver: testTargetDriver{Pool: Target, Started: Started, Continue: Continue},
		Resolver: DSNResolverFunc(func(Context.Context, string) (string, error) { return "target", nil })}
	Ctx, Cancel := Context.WithTimeout(Context.Background(), 5*Time.Second)
	defer Cancel()
	if _, Err := Target.Exec(Ctx, "CREATE TABLE effects (id int)"); Err != nil {
		T.Fatal(Err)
	}
	First, Err := Server.Create(Ctx, User, DBID, MigrationInput{Title: "First", SQLText: "INSERT INTO effects VALUES (1)", DownSQL: "DELETE FROM effects WHERE id = 1"})
	if Err != nil {
		T.Fatal(Err)
	}
	Second, Err := Server.Create(Ctx, User, DBID, MigrationInput{Title: "Second", SQLText: "INSERT INTO effects VALUES (2)", DownSQL: "DELETE FROM effects WHERE id = 2"})
	if Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Server.Execute(Ctx, User, DBID, Second.ID); !Errors.Is(Err, ErrOutOfOrder) {
		T.Fatalf("later draft executed before first: %v", Err)
	}
	Done := make(chan error, 1)
	go func() { _, Err := Server.Execute(Ctx, User, DBID, First.ID); Done <- Err }()
	select {
	case <-Started:
	case <-Ctx.Done():
		T.Fatal("execution did not start")
	}
	Pending, Err := (&Repository{Pool: Platform}).Get(Ctx, DBID, First.ID)
	if Err != nil || Pending.Status != StatusPending {
		T.Fatalf("execution state was not persisted: %+v %v", Pending, Err)
	}
	Results := make(chan error, 3)
	go func() { _, Err := Server.Execute(Ctx, User, Strings.ToUpper(DBID), First.ID); Results <- Err }()
	go func() {
		_, Err := Server.UpdateDraft(Ctx, User, Strings.ReplaceAll(DBID, "-", ""), First.ID, MigrationInput{Title: "Changed", SQLText: "DELETE FROM effects", DownSQL: "SELECT 1"})
		Results <- Err
	}()
	go func() { Results <- Server.DeleteDraft(Ctx, User, DBID, First.ID) }()
	close(Continue)
	if Err := <-Done; Err != nil {
		T.Fatal(Err)
	}
	for Index := 0; Index < 3; Index++ {
		if Err := <-Results; !Errors.Is(Err, ErrAlreadyExecuted) && !Errors.Is(Err, ErrLocked) {
			T.Fatalf("concurrent mutation was accepted: %v", Err)
		}
	}
	Stored, Err := Repo.Get(Ctx, DBID, First.ID)
	if Err != nil || Stored.SQLText != First.SQLText || Stored.Status != StatusApplied {
		T.Fatalf("history does not match execution: %+v %v", Stored, Err)
	}
	var Count int
	if Err := Target.QueryRow(Ctx, "SELECT count(*) FROM effects").Scan(&Count); Err != nil || Count != 1 {
		T.Fatalf("migration applied more than once: %d %v", Count, Err)
	}
	if _, Err := Server.Execute(Ctx, User, DBID, Second.ID); Err != nil {
		T.Fatalf("next version could not execute: %v", Err)
	}
}

func TestServiceBatchExecutionRollbackAndForward(T *Testing.T) {
	Platform := NewMigrationDatabase(T)
	Target := NewMigrationDatabase(T)
	Ctx := Context.Background()
	DBID, UserID := setupFixture(T, Platform)
	User := Auth.Principal{ID: UserID, Username: "test_migrator"}
	Service := &Service{
		Repo:     &Repository{Pool: Platform},
		Access:   Access.AllowAll(),
		Resolver: DSNResolverFunc(func(Context.Context, string) (string, error) { return "target", nil }),
		Driver:   testTargetDriver{Pool: Target},
	}
	if _, Err := Target.Exec(Ctx, `CREATE TABLE batch_items (id BIGINT PRIMARY KEY, value INT NOT NULL);
INSERT INTO batch_items SELECT g, 0 FROM generate_series(1, 25) AS g`); Err != nil {
		T.Fatal(Err)
	}
	Input := MigrationInput{
		Title:       "Batch increment",
		SQLText:     `UPDATE batch_items SET value = value + 1 WHERE id IN (SELECT id FROM batch_items WHERE id > $1 ORDER BY id LIMIT $2) RETURNING id`,
		DownSQL:     `UPDATE batch_items SET value = value - 1 WHERE id > $2 AND id <= $1`,
		BatchMode:   true,
		BatchColumn: "id",
		BatchSize:   10,
	}
	Created, Err := Service.Create(Ctx, User, DBID, Input)
	if Err != nil {
		T.Fatal(Err)
	}
	Applied, Err := Service.Apply(Ctx, User, DBID, Created.ID)
	if Err != nil {
		T.Fatalf("batch apply failed: %v", Err)
	}
	if Applied.Status != ExecutionSucceeded || Applied.CommittedBatches != 3 || Applied.CommittedRows != 25 {
		T.Fatalf("unexpected batch apply progress: %+v", Applied)
	}
	var Count int
	if Err := Target.QueryRow(Ctx, "SELECT count(*) FROM batch_items WHERE value = 1").Scan(&Count); Err != nil || Count != 25 {
		T.Fatalf("batch update did not touch every row: %d %v", Count, Err)
	}
	RolledBack, Err := Service.Rollback(Ctx, User, DBID, Created.ID)
	if Err != nil {
		T.Fatalf("batch rollback failed: %v", Err)
	}
	if RolledBack.Status != ExecutionSucceeded || RolledBack.RevertedBatches != 3 || RolledBack.RevertedRows != 25 {
		T.Fatalf("unexpected batch rollback progress: %+v", RolledBack)
	}
	if Err := Target.QueryRow(Ctx, "SELECT count(*) FROM batch_items WHERE value = 0").Scan(&Count); Err != nil || Count != 25 {
		T.Fatalf("batch rollback did not restore every row: %d %v", Count, Err)
	}
	Forward, Err := Service.Forward(Ctx, User, DBID, Created.ID)
	if Err != nil {
		T.Fatalf("batch forward failed: %v", Err)
	}
	if Forward.Status != ExecutionSucceeded || Forward.CommittedBatches != 3 {
		T.Fatalf("unexpected batch forward: %+v", Forward)
	}
	Migration, Err := Service.Repo.Get(Ctx, DBID, Created.ID)
	if Err != nil || Migration.Status != StatusApplied {
		T.Fatalf("batch migration state is wrong: %+v %v", Migration, Err)
	}
}

func TestServiceBatchPartialFailureAndRollback(T *Testing.T) {
	Platform := NewMigrationDatabase(T)
	Target := NewMigrationDatabase(T)
	Ctx := Context.Background()
	DBID, UserID := setupFixture(T, Platform)
	User := Auth.Principal{ID: UserID, Username: "test_migrator"}
	Service := &Service{
		Repo:     &Repository{Pool: Platform},
		Access:   Access.AllowAll(),
		Resolver: DSNResolverFunc(func(Context.Context, string) (string, error) { return "target", nil }),
		Driver:   testTargetDriver{Pool: Target},
	}
	if _, Err := Target.Exec(Ctx, `CREATE TABLE batch_partial (id BIGINT PRIMARY KEY, value INT NOT NULL);
INSERT INTO batch_partial SELECT g, 0 FROM generate_series(1, 25) AS g`); Err != nil {
		T.Fatal(Err)
	}
	Input := MigrationInput{
		Title:       "Batch partial",
		SQLText:     `UPDATE batch_partial SET value = 1 / (CASE WHEN id = 15 THEN 0 ELSE 1 END) WHERE id IN (SELECT id FROM batch_partial WHERE id > $1 ORDER BY id LIMIT $2) RETURNING id`,
		DownSQL:     `UPDATE batch_partial SET value = 0 WHERE id > $2 AND id <= $1`,
		BatchMode:   true,
		BatchColumn: "id",
		BatchSize:   10,
	}
	Created, Err := Service.Create(Ctx, User, DBID, Input)
	if Err != nil {
		T.Fatal(Err)
	}
	Partial, Err := Service.Apply(Ctx, User, DBID, Created.ID)
	if !Errors.Is(Err, Driver.ErrExecution) {
		T.Fatalf("expected batch SQL failure, got %v", Err)
	}
	if Partial.Status != ExecutionPartial || Partial.CommittedBatches != 1 || Partial.CommittedRows != 10 {
		T.Fatalf("unexpected partial progress: %+v", Partial)
	}
	Migration, Err := Service.Repo.Get(Ctx, DBID, Created.ID)
	if Err != nil || Migration.Status != StatusPartial {
		T.Fatalf("migration should be partial: %+v %v", Migration, Err)
	}
	Reverted, Err := Service.Rollback(Ctx, User, DBID, Created.ID)
	if Err != nil {
		T.Fatalf("partial rollback failed: %v", Err)
	}
	if Reverted.Status != ExecutionSucceeded || Reverted.RevertedBatches != 1 || Reverted.RevertedRows != 10 {
		T.Fatalf("unexpected partial rollback: %+v", Reverted)
	}
	var Count int
	if Err := Target.QueryRow(Ctx, "SELECT count(*) FROM batch_partial WHERE value = 0").Scan(&Count); Err != nil || Count != 25 {
		T.Fatalf("partial rollback did not restore committed batch: %d %v", Count, Err)
	}
}

func TestServiceBatchRetryResumesCheckpoint(T *Testing.T) {
	Platform := NewMigrationDatabase(T)
	Target := NewMigrationDatabase(T)
	Ctx := Context.Background()
	DBID, UserID := setupFixture(T, Platform)
	User := Auth.Principal{ID: UserID, Username: "test_migrator"}
	Service := &Service{
		Repo:     &Repository{Pool: Platform},
		Access:   Access.AllowAll(),
		Resolver: DSNResolverFunc(func(Context.Context, string) (string, error) { return "target", nil }),
		Driver:   testTargetDriver{Pool: Target},
	}
	if _, Err := Target.Exec(Ctx, `CREATE TABLE batch_retry (id BIGINT PRIMARY KEY, value INT NOT NULL);
INSERT INTO batch_retry SELECT g, 0 FROM generate_series(1, 25) AS g`); Err != nil {
		T.Fatal(Err)
	}
	Created, Err := Service.Create(Ctx, User, DBID, MigrationInput{
		Title:       "Batch retry",
		SQLText:     `UPDATE batch_retry SET value = 1 / (CASE WHEN id = 15 THEN 0 ELSE 1 END) WHERE id IN (SELECT id FROM batch_retry WHERE id > $1 ORDER BY id LIMIT $2) RETURNING id`,
		DownSQL:     `UPDATE batch_retry SET value = 0 WHERE id > $2 AND id <= $1`,
		BatchMode:   true,
		BatchColumn: "id",
		BatchSize:   10,
	})
	if Err != nil {
		T.Fatal(Err)
	}
	Partial, Err := Service.Apply(Ctx, User, DBID, Created.ID)
	if !Errors.Is(Err, Driver.ErrExecution) || Partial.Status != ExecutionPartial || Partial.CommittedBatches != 1 {
		T.Fatalf("unexpected partial apply: %+v %v", Partial, Err)
	}
	if _, Err := Target.Exec(Ctx, "DELETE FROM batch_retry WHERE id = 15"); Err != nil {
		T.Fatal(Err)
	}
	Retried, Err := Service.Retry(Ctx, User, DBID, Created.ID)
	if Err != nil {
		T.Fatalf("batch retry failed: %v", Err)
	}
	if Retried.ID != Partial.ID || Retried.Status != ExecutionSucceeded || Retried.CommittedBatches != 3 || Retried.CommittedRows != 24 {
		T.Fatalf("batch retry did not resume the checkpoint: %+v", Retried)
	}
	var Count int
	if Err := Target.QueryRow(Ctx, "SELECT count(*) FROM batch_retry WHERE value = 1").Scan(&Count); Err != nil || Count != 24 {
		T.Fatalf("batch retry did not apply remaining rows: %d %v", Count, Err)
	}
}
