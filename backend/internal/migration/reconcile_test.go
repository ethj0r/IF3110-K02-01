package Migration

import (
	Context "context"
	Errors "errors"
	Sync "sync"
	Testing "testing"
	Time "time"

	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
)

func TestReconcileStatus(T *Testing.T) {
	Cases := []struct {
		Name      string
		Exec      Execution
		Succeeded bool
		Want      ExecutionStatus
		Err       error
	}{
		{"normal committed", Execution{Mode: ModeNormal, Direction: DirectionUp}, true, ExecutionSucceeded, nil},
		{"normal rolled back", Execution{Mode: ModeNormal, Direction: DirectionDown}, false, ExecutionFailed, nil},
		{"batch cannot jump to succeeded", Execution{Mode: ModeBatch, Direction: DirectionUp, CommittedBatches: 3}, true, "", ErrBatchReconcile},
		{"batch up keeps committed batches", Execution{Mode: ModeBatch, Direction: DirectionUp, CommittedBatches: 3}, false, ExecutionPartial, nil},
		{"batch down keeps reverted batches", Execution{Mode: ModeBatch, Direction: DirectionDown, RevertedBatches: 1}, false, ExecutionPartiallyReverted, nil},
		{"batch without progress", Execution{Mode: ModeBatch, Direction: DirectionUp}, false, ExecutionFailed, nil},
	}
	for _, Case := range Cases {
		Got, Err := ReconcileStatus(Case.Exec, Case.Succeeded)
		if Got != Case.Want || !Errors.Is(Err, Case.Err) {
			T.Fatalf("%s: got %q %v, want %q %v", Case.Name, Got, Err, Case.Want, Case.Err)
		}
	}
}

func TestReconcileRequiresAdminWithEditGrant(T *Testing.T) {
	Ctx := Context.Background()
	Policy := Access.StaticEvaluator{
		DefaultLevel: Access.LevelNone,
		Grants: map[string]map[string]Access.Level{
			"editor-1":       {"db-1": Access.LevelViewEdit},
			"admin-viewer-1": {"db-1": Access.LevelViewOnly},
			"admin-editor-1": {"db-1": Access.LevelViewEdit},
		},
	}
	Repo := NewMockRepo()
	Repo.Executions["exec-1"] = Execution{ID: "exec-1", DatabaseID: "db-1", MigrationID: "mig-1", Status: ExecutionSucceeded, Mode: ModeBatch}
	Server := &Service{Repo: Repo, Access: Policy}
	Denied := []Auth.Principal{
		{ID: "editor-1", Username: "editor"},
		{ID: "admin-none-1", Username: "admin_none", IsPlatformAdmin: true},
		{ID: "admin-viewer-1", Username: "admin_viewer", IsPlatformAdmin: true},
	}
	for _, User := range Denied {
		if _, Err := Server.Reconcile(Ctx, User, "db-1", "mig-1", "exec-1", "failed", ""); !Errors.Is(Err, ErrForbidden) {
			T.Fatalf("%s must be denied, got %v", User.Username, Err)
		}
	}
	Admin := Auth.Principal{ID: "admin-editor-1", Username: "admin", IsPlatformAdmin: true}
	if _, Err := Server.Reconcile(Ctx, Admin, "db-1", "mig-1", "exec-1", "maybe", ""); !Errors.Is(Err, ErrInvalidInput) {
		T.Fatalf("unknown outcome must be rejected, got %v", Err)
	}
	if _, Err := Server.Reconcile(Ctx, Admin, "db-1", "mig-other", "exec-1", "failed", ""); !Errors.Is(Err, ErrExecutionMissing) {
		T.Fatalf("execution of another migration must not be found, got %v", Err)
	}
	if _, Err := Server.Reconcile(Ctx, Admin, "db-1", "mig-1", "exec-1", "failed", ""); !Errors.Is(Err, ErrReconcileNotAllowed) {
		T.Fatalf("a finished execution must not be reconciled, got %v", Err)
	}
	Repo.Executions["exec-1"] = Execution{ID: "exec-1", DatabaseID: "db-1", MigrationID: "mig-1", Status: ExecutionRunning, Mode: ModeBatch, Direction: DirectionUp, CommittedBatches: 2}
	if _, Err := Server.Reconcile(Ctx, Admin, "db-1", "mig-1", "exec-1", "succeeded", ""); !Errors.Is(Err, ErrBatchReconcile) {
		T.Fatalf("a batch execution must not be reconciled as succeeded, got %v", Err)
	}
	Settled, Err := Server.Reconcile(Ctx, Admin, "db-1", "mig-1", "exec-1", "failed", "worker host lost")
	if Err != nil || Settled.Status != ExecutionPartial || Settled.ReconciledAt == nil {
		T.Fatalf("batch execution should stop at its checkpoint: %+v %v", Settled, Err)
	}
}

type flakyLeaseRepo struct {
	*MockRepo
	Mutex Sync.Mutex
	Calls int
}

func (R *flakyLeaseRepo) RenewJobLease(_ Context.Context, _ string, _ string, _ Time.Duration) error {
	R.Mutex.Lock()
	defer R.Mutex.Unlock()
	R.Calls++
	if R.Calls == 1 {
		return ErrJobUnavailable
	}
	return nil
}

func (R *flakyLeaseRepo) count() int {
	R.Mutex.Lock()
	defer R.Mutex.Unlock()
	return R.Calls
}

func TestHeartbeatSurvivesTransientRenewalError(T *Testing.T) {
	Repo := &flakyLeaseRepo{MockRepo: NewMockRepo()}
	W := &Worker{Repo: Repo, ID: "worker-1", Lease: 3 * Time.Second}
	Ctx, Cancel := Context.WithTimeout(Context.Background(), 2500*Time.Millisecond)
	defer Cancel()
	W.heartbeat(Ctx, "job-1")
	if Calls := Repo.count(); Calls < 2 {
		T.Fatalf("heartbeat stopped after a transient error: %d renewals", Calls)
	}
}

type lostLeaseRepo struct {
	*MockRepo
	Calls int
}

func (R *lostLeaseRepo) RenewJobLease(_ Context.Context, _ string, _ string, _ Time.Duration) error {
	R.Calls++
	return ErrJobNotFound
}

func TestHeartbeatStopsWhenLeaseIsLost(T *Testing.T) {
	Repo := &lostLeaseRepo{MockRepo: NewMockRepo()}
	W := &Worker{Repo: Repo, ID: "worker-1", Lease: 3 * Time.Second}
	Ctx, Cancel := Context.WithTimeout(Context.Background(), 2500*Time.Millisecond)
	defer Cancel()
	Started := Time.Now()
	W.heartbeat(Ctx, "job-1")
	if Repo.Calls != 1 || Time.Since(Started) > 2*Time.Second {
		T.Fatalf("heartbeat should stop on a lost lease: %d renewals after %s", Repo.Calls, Time.Since(Started))
	}
}
