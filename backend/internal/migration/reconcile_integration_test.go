//go:build integration

package Migration

import (
	Context "context"
	Errors "errors"
	Testing "testing"
	Time "time"

	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
)

func reconcileService(Platform *Pgxpool.Pool, Target *Pgxpool.Pool) *Service {
	return &Service{
		Repo:     &Repository{Pool: Platform},
		Access:   Access.AllowAll(),
		Resolver: DSNResolverFunc(func(Context.Context, string) (string, error) { return "target", nil }),
		Driver:   testTargetDriver{Pool: Target},
	}
}

// markUncertain puts a finished execution back into the state a lost commit
// result leaves behind: execution and attempt still running.
func markUncertain(T *Testing.T, Platform *Pgxpool.Pool, ExecutionID string) {
	T.Helper()
	Ctx := Context.Background()
	if _, Err := Platform.Exec(Ctx, "UPDATE migration_executions SET status = 'running', finished_at = NULL, error_message = NULL WHERE id = $1", ExecutionID); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Platform.Exec(Ctx, `UPDATE migration_attempts SET status = 'running', finished_at = NULL, error_message = NULL
WHERE id = (SELECT id FROM migration_attempts WHERE execution_id = $1 ORDER BY attempt_no DESC LIMIT 1)`, ExecutionID); Err != nil {
		T.Fatal(Err)
	}
}

func TestReconcileFollowsNormalEvidence(T *Testing.T) {
	Platform := NewMigrationDatabase(T)
	Target := NewMigrationDatabase(T)
	Ctx := Context.Background()
	DBID, UserID := setupFixture(T, Platform)
	Admin := Auth.Principal{ID: UserID, Username: "test_migrator", IsPlatformAdmin: true}
	Service := reconcileService(Platform, Target)
	Created, Err := Service.Create(Ctx, Admin, DBID, MigrationInput{Title: "Committed", SQLText: "CREATE TABLE reconcile_committed (id INT);", DownSQL: "DROP TABLE reconcile_committed;"})
	if Err != nil {
		T.Fatal(Err)
	}
	Done, Err := Service.Apply(Ctx, Admin, DBID, Created.ID)
	if Err != nil {
		T.Fatal(Err)
	}
	markUncertain(T, Platform, Done.ID)
	if _, Err := Service.Repo.RefreshStatus(Ctx, DBID, Created.ID); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Service.Reconcile(Ctx, Admin, DBID, Created.ID, Done.ID, "failed", "guess"); !Errors.Is(Err, ErrEvidenceMismatch) {
		T.Fatalf("failed outcome must contradict committed evidence, got %v", Err)
	}
	Settled, Err := Service.Reconcile(Ctx, Admin, DBID, Created.ID, Done.ID, "succeeded", "evidence row present")
	if Err != nil || Settled.Status != ExecutionSucceeded || Settled.Attempts[len(Settled.Attempts)-1].Status != AttemptSucceeded {
		T.Fatalf("committed execution was not settled: %+v %v", Settled, Err)
	}
	if Migration, Err := Service.Repo.Get(Ctx, DBID, Created.ID); Err != nil || Migration.Status != StatusApplied {
		T.Fatalf("migration should be applied: %+v %v", Migration, Err)
	}
	if _, Err := Service.Reconcile(Ctx, Admin, DBID, Created.ID, Done.ID, "succeeded", ""); !Errors.Is(Err, ErrReconcileNotAllowed) {
		T.Fatalf("a settled execution must not be reconciled twice, got %v", Err)
	}
}

func TestReconcileAfterWorkerLoss(T *Testing.T) {
	Platform := NewMigrationDatabase(T)
	Target := NewMigrationDatabase(T)
	Ctx := Context.Background()
	DBID, UserID := setupFixture(T, Platform)
	Admin := Auth.Principal{ID: UserID, Username: "test_migrator", IsPlatformAdmin: true}
	Service := reconcileService(Platform, Target)
	Repo := Service.Repo.(*Repository)
	Created, Err := Service.Create(Ctx, Admin, DBID, MigrationInput{Title: "Crashed", SQLText: "CREATE TABLE reconcile_crashed (id INT);", DownSQL: "DROP TABLE reconcile_crashed;"})
	if Err != nil {
		T.Fatal(Err)
	}
	Queued, Err := Repo.EnqueueJob(Ctx, DBID, Created.ID, JobApply, &UserID, nil)
	if Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Repo.ClaimJob(Ctx, "dead-worker", Time.Second); Err != nil {
		T.Fatal(Err)
	}
	// The worker created the execution and its attempt, then died before the
	// target statement committed.
	Exec, Err := Repo.CreateExecution(Ctx, DBID, Created.ID, ExecutionRequest{Kind: KindApply, Direction: DirectionUp, SQLText: Created.SQLText, RequestedBy: &UserID})
	if Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Repo.CreateAttempt(Ctx, Exec.ID, &UserID, Created.SQLText); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Service.Reconcile(Ctx, Admin, DBID, Created.ID, Exec.ID, "failed", ""); !Errors.Is(Err, ErrDatabaseBusy) {
		T.Fatalf("a job with a live lease must block reconciliation, got %v", Err)
	}
	Time.Sleep(1200 * Time.Millisecond)
	if Count, Err := Repo.ExpireStaleJobs(Ctx, 10); Err != nil || Count != 1 {
		T.Fatalf("stale job was not expired: %d %v", Count, Err)
	}
	Expired, Err := Repo.GetJob(Ctx, DBID, Queued.ID)
	if Err != nil || Expired.Status != JobFailed || Expired.ExecutionID == nil || *Expired.ExecutionID != Exec.ID {
		T.Fatalf("expired job should point at the stuck execution: %+v %v", Expired, Err)
	}
	if _, Err := Service.Apply(Ctx, Admin, DBID, Created.ID); !Errors.Is(Err, ErrPending) && !Errors.Is(Err, ErrAlreadyExecuted) {
		T.Fatalf("the stuck execution must hold the database, got %v", Err)
	}
	Holder, Err := Platform.Acquire(Ctx)
	if Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Holder.Exec(Ctx, AdvisoryLockSQL, DBID); Err != nil {
		T.Fatal(Err)
	}
	_, BusyErr := Service.Reconcile(Ctx, Admin, DBID, Created.ID, Exec.ID, "failed", "")
	if _, Err := Holder.Exec(Ctx, "SELECT pg_advisory_unlock(hashtext('migration_' || $1::uuid::text))", DBID); Err != nil {
		T.Fatal(Err)
	}
	Holder.Release()
	if !Errors.Is(BusyErr, ErrDatabaseBusy) {
		T.Fatalf("a held advisory lock must fail fast, got %v", BusyErr)
	}
	if _, Err := Service.Reconcile(Ctx, Admin, DBID, Created.ID, Exec.ID, "succeeded", ""); !Errors.Is(Err, ErrEvidenceMismatch) {
		T.Fatalf("succeeded without target evidence must be rejected, got %v", Err)
	}
	Settled, Err := Service.Reconcile(Ctx, Admin, DBID, Created.ID, Exec.ID, "failed", "worker host lost")
	if Err != nil || Settled.Status != ExecutionFailed || Settled.Attempts[0].Status != AttemptFailed {
		T.Fatalf("crashed execution was not settled: %+v %v", Settled, Err)
	}
	Stamped, Err := Repo.GetJob(Ctx, DBID, Queued.ID)
	if Err != nil || Stamped.Status != JobFailed || Stamped.ReconciledAt == nil {
		T.Fatalf("job should keep its status and record the reconciliation: %+v %v", Stamped, Err)
	}
	Retried, Err := Service.Retry(Ctx, Admin, DBID, Created.ID)
	if Err != nil || Retried.Status != ExecutionSucceeded || Retried.RetryOfExecutionID == nil || *Retried.RetryOfExecutionID != Exec.ID {
		T.Fatalf("retry after reconciliation failed: %+v %v", Retried, Err)
	}
}

func TestReconcileBatchStopsAtCheckpoint(T *Testing.T) {
	Platform := NewMigrationDatabase(T)
	Target := NewMigrationDatabase(T)
	Ctx := Context.Background()
	DBID, UserID := setupFixture(T, Platform)
	Admin := Auth.Principal{ID: UserID, Username: "test_migrator", IsPlatformAdmin: true}
	Service := reconcileService(Platform, Target)
	if _, Err := Target.Exec(Ctx, `CREATE TABLE batch_reconcile (id BIGINT PRIMARY KEY, value INT NOT NULL);
INSERT INTO batch_reconcile SELECT g, 0 FROM generate_series(1, 25) AS g`); Err != nil {
		T.Fatal(Err)
	}
	Created, Err := Service.Create(Ctx, Admin, DBID, MigrationInput{
		Title:       "Batch reconcile",
		SQLText:     `UPDATE batch_reconcile SET value = 1 / (CASE WHEN id = 15 THEN 0 ELSE 1 END) WHERE id IN (SELECT id FROM batch_reconcile WHERE id > $1 ORDER BY id LIMIT $2) RETURNING id`,
		DownSQL:     `UPDATE batch_reconcile SET value = 0 WHERE id > $2 AND id <= $1`,
		BatchMode:   true,
		BatchColumn: "id",
		BatchSize:   10,
	})
	if Err != nil {
		T.Fatal(Err)
	}
	Partial, Err := Service.Apply(Ctx, Admin, DBID, Created.ID)
	if !Errors.Is(Err, Driver.ErrExecution) || Partial.CommittedBatches != 1 {
		T.Fatalf("unexpected partial apply: %+v %v", Partial, Err)
	}
	markUncertain(T, Platform, Partial.ID)
	if _, Err := Service.Reconcile(Ctx, Admin, DBID, Created.ID, Partial.ID, "succeeded", ""); !Errors.Is(Err, ErrBatchReconcile) {
		T.Fatalf("batch execution must not be reconciled as succeeded, got %v", Err)
	}
	Settled, Err := Service.Reconcile(Ctx, Admin, DBID, Created.ID, Partial.ID, "failed", "worker host lost")
	if Err != nil || Settled.Status != ExecutionPartial || Settled.CommittedBatches != 1 {
		T.Fatalf("batch execution should stop at its checkpoint: %+v %v", Settled, Err)
	}
	if Migration, Err := Service.Repo.Get(Ctx, DBID, Created.ID); Err != nil || Migration.Status != StatusPartial {
		T.Fatalf("migration should be partial: %+v %v", Migration, Err)
	}
	if _, Err := Target.Exec(Ctx, "DELETE FROM batch_reconcile WHERE id = 15"); Err != nil {
		T.Fatal(Err)
	}
	Resumed, Err := Service.Retry(Ctx, Admin, DBID, Created.ID)
	if Err != nil || Resumed.ID != Partial.ID || Resumed.Status != ExecutionSucceeded || Resumed.CommittedBatches != 3 || Resumed.CommittedRows != 24 {
		T.Fatalf("retry did not resume from the checkpoint: %+v %v", Resumed, Err)
	}
}

func TestExecutionEvidenceScopedToMigration(T *Testing.T) {
	Platform := NewMigrationDatabase(T)
	Target := NewMigrationDatabase(T)
	Ctx := Context.Background()
	DBID, UserID := setupFixture(T, Platform)
	User := Auth.Principal{ID: UserID, Username: "test_migrator"}
	Service := reconcileService(Platform, Target)
	First, Err := Service.Create(Ctx, User, DBID, MigrationInput{Title: "First", SQLText: "SELECT 1;", DownSQL: "SELECT 1;"})
	if Err != nil {
		T.Fatal(Err)
	}
	Second, Err := Service.Create(Ctx, User, DBID, MigrationInput{Title: "Second", SQLText: "SELECT 1;", DownSQL: "SELECT 1;"})
	if Err != nil {
		T.Fatal(Err)
	}
	// No platform migration ran on the target yet, so it has no evidence table.
	Pending, Err := Service.Repo.CreateExecution(Ctx, DBID, First.ID, ExecutionRequest{Kind: KindApply, Direction: DirectionUp, SQLText: First.SQLText, RequestedBy: &UserID})
	if Err != nil {
		T.Fatal(Err)
	}
	if Rows, Err := Service.ExecutionEvidence(Ctx, User, DBID, First.ID, Pending.ID); Err != nil || len(Rows) != 0 {
		T.Fatalf("a target without the evidence table should return no rows: %+v %v", Rows, Err)
	}
	if _, Err := Service.ExecutionEvidence(Ctx, User, DBID, Second.ID, Pending.ID); !Errors.Is(Err, ErrExecutionMissing) {
		T.Fatalf("evidence must be scoped to its migration, got %v", Err)
	}
}
