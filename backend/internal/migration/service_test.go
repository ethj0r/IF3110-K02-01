package Migration

import (
	Context "context"
	Errors "errors"
	Fmt "fmt"
	Io "io"
	Slog "log/slog"
	Sort "sort"
	Testing "testing"
	Time "time"

	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
	Pgx "github.com/jackc/pgx/v5"
)

type MockRepo struct {
	Items          map[string]Migration
	Executions     map[string]Execution
	Attempts       map[string][]Attempt
	Order          []string
	NextID         int
	NextExecution  int
	NextAttempt    int
	NextJob        int
	Jobs           map[string]Job
	RefreshFailure error
}

func NewMockRepo() *MockRepo {
	return &MockRepo{
		Items:      map[string]Migration{},
		Executions: map[string]Execution{},
		Attempts:   map[string][]Attempt{},
		Jobs:       map[string]Job{},
	}
}

func (M *MockRepo) WithDatabaseLock(_ Context.Context, _ string, Run func(MigrationRepository) (Migration, error)) (Migration, error) {
	return Run(M)
}

func (M *MockRepo) WithExecutionLock(_ Context.Context, _ string, Run func(MigrationRepository) (Execution, error)) (Execution, error) {
	return Run(M)
}

func (M *MockRepo) Create(_ Context.Context, DatabaseID string, Input MigrationInput, CreatedBy *string) (Migration, error) {
	Input = Input.Normalized()
	NextVersion := int64(0)
	for _, Item := range M.Items {
		if Item.DatabaseID == DatabaseID && Item.Version > NextVersion {
			NextVersion = Item.Version
		}
	}
	M.NextID++
	Item := Migration{
		ID:           Fmt.Sprintf("mig-%d", M.NextID),
		DatabaseID:   DatabaseID,
		Version:      NextVersion + 1,
		Revision:     1,
		Title:        Input.Title,
		SQLText:      Input.SQLText,
		BatchMode:    Input.BatchMode,
		Irreversible: Input.Irreversible,
		Status:       StatusDraft,
		CreatedBy:    CreatedBy,
		CreatedAt:    Time.Now().UTC(),
		UpdatedAt:    Time.Now().UTC(),
	}
	if !Input.Irreversible {
		Down := Input.DownSQL
		Item.DownSQL = &Down
	}
	if Input.BatchMode {
		Column := Input.BatchColumn
		Size := Input.BatchSize
		Item.BatchColumn = &Column
		Item.BatchSize = &Size
	}
	if Input.Irreversible {
		Reason := Input.IrreversibleReason
		Item.IrreversibleReason = &Reason
	}
	M.Items[Item.ID] = Item
	return Item, nil
}

func (M *MockRepo) Get(_ Context.Context, DatabaseID string, ID string) (Migration, error) {
	Item, Ok := M.Items[ID]
	if !Ok || Item.DatabaseID != DatabaseID {
		return Migration{}, ErrNotFound
	}
	Item.CanApply, Item.CanRollback, Item.CanForward, Item.CanRetry = false, false, false, false
	return Item, nil
}

func (M *MockRepo) GetByVersion(_ Context.Context, DatabaseID string, Version int64) (Migration, error) {
	for _, Item := range M.Items {
		if Item.DatabaseID == DatabaseID && Item.Version == Version {
			return Item, nil
		}
	}
	return Migration{}, ErrNotFound
}

func (M *MockRepo) List(_ Context.Context, DatabaseID string) ([]Migration, error) {
	Result := []Migration{}
	for _, Item := range M.Items {
		if Item.DatabaseID == DatabaseID {
			Result = append(Result, Item)
		}
	}
	Sort.Slice(Result, func(A, B int) bool { return Result[A].Version < Result[B].Version })
	return Result, nil
}

func (M *MockRepo) UpdateDraft(_ Context.Context, DatabaseID string, ID string, Input MigrationInput) (Migration, error) {
	Item, Ok := M.Items[ID]
	if !Ok || Item.DatabaseID != DatabaseID {
		return Migration{}, ErrNotFound
	}
	if Item.LockedAt != nil || !Item.Status.IsDraft() {
		return Migration{}, ErrLocked
	}
	Input = Input.Normalized()
	Item.Title = Input.Title
	Item.SQLText = Input.SQLText
	Item.Irreversible = Input.Irreversible
	Item.DownSQL = nil
	Item.IrreversibleReason = nil
	Item.BatchMode = Input.BatchMode
	Item.BatchColumn = nil
	Item.BatchSize = nil
	if Input.Irreversible {
		Reason := Input.IrreversibleReason
		Item.IrreversibleReason = &Reason
	} else {
		Down := Input.DownSQL
		Item.DownSQL = &Down
	}
	if Input.BatchMode {
		Column := Input.BatchColumn
		Size := Input.BatchSize
		Item.BatchColumn = &Column
		Item.BatchSize = &Size
	}
	Item.Revision++
	Item.UpdatedAt = Time.Now().UTC()
	M.Items[ID] = Item
	return Item, nil
}

func (M *MockRepo) DeleteDraft(_ Context.Context, DatabaseID string, ID string) error {
	Item, Ok := M.Items[ID]
	if !Ok || Item.DatabaseID != DatabaseID {
		return ErrNotFound
	}
	if Item.LockedAt != nil || !Item.Status.IsDraft() {
		return ErrLocked
	}
	delete(M.Items, ID)
	return nil
}

func (M *MockRepo) EnqueueJob(_ Context.Context, DatabaseID string, MigrationID string, Action JobAction, RequestedBy *string, TraceParent *string) (Job, error) {
	for _, Existing := range M.Jobs {
		if Existing.DatabaseID == DatabaseID && (Existing.Status == JobQueued || Existing.Status == JobRunning) {
			return Job{}, ErrJobConflict
		}
	}
	Item, Ok := M.Items[MigrationID]
	if !Ok || Item.DatabaseID != DatabaseID {
		return Job{}, ErrNotFound
	}
	M.NextJob++
	Now := Time.Now().UTC()
	Job := Job{
		ID:          Fmt.Sprintf("job-%d", M.NextJob),
		DatabaseID:  DatabaseID,
		MigrationID: MigrationID,
		Version:     Item.Version,
		Title:       Item.Title,
		Action:      Action,
		Status:      JobQueued,
		MaxAttempts: 3,
		AvailableAt: Now,
		RequestedBy: RequestedBy,
		TraceParent: TraceParent,
		CreatedAt:   Now,
		UpdatedAt:   Now,
	}
	M.Jobs[Job.ID] = Job
	return Job, nil
}

func (M *MockRepo) GetJob(_ Context.Context, DatabaseID string, JobID string) (Job, error) {
	Item, Ok := M.Jobs[JobID]
	if !Ok || Item.DatabaseID != DatabaseID {
		return Job{}, ErrJobNotFound
	}
	return Item, nil
}

func (M *MockRepo) ListJobs(_ Context.Context, DatabaseID string, Limit int) ([]Job, error) {
	Result := []Job{}
	for _, Item := range M.Jobs {
		if Item.DatabaseID == DatabaseID {
			Result = append(Result, Item)
		}
	}
	Sort.Slice(Result, func(I, J int) bool { return Result[I].ID > Result[J].ID })
	if len(Result) > Limit {
		Result = Result[:Limit]
	}
	return Result, nil
}

func (M *MockRepo) ClaimJob(_ Context.Context, WorkerID string, _ Time.Duration) (Job, error) {
	for ID, Item := range M.Jobs {
		if Item.Status != JobQueued {
			continue
		}
		Now := Time.Now().UTC()
		Item.Status = JobRunning
		Item.Attempts++
		Item.LockedBy = &WorkerID
		Item.LockedAt = &Now
		Item.UpdatedAt = Now
		M.Jobs[ID] = Item
		return Item, nil
	}
	return Job{}, ErrJobNotFound
}

func (M *MockRepo) FinishJob(_ Context.Context, JobID string, Status JobStatus, ExecutionID *string, LastError *string) error {
	Item, Ok := M.Jobs[JobID]
	if !Ok {
		return ErrJobNotFound
	}
	Now := Time.Now().UTC()
	Item.Status = Status
	Item.ExecutionID = ExecutionID
	Item.LastError = LastError
	Item.FinishedAt = &Now
	Item.LockedBy = nil
	Item.UpdatedAt = Now
	M.Jobs[JobID] = Item
	return nil
}

func (M *MockRepo) ExpireStaleJobs(_ Context.Context, _ int) (int, error) {
	return 0, nil
}

func (M *MockRepo) RequeueJob(_ Context.Context, JobID string, AvailableAt Time.Time, LastError string) error {
	Item, Ok := M.Jobs[JobID]
	if !Ok {
		return ErrJobNotFound
	}
	Item.Status = JobQueued
	Item.AvailableAt = AvailableAt
	Item.LastError = &LastError
	Item.LockedBy = nil
	Item.UpdatedAt = Time.Now().UTC()
	M.Jobs[JobID] = Item
	return nil
}

func (M *MockRepo) SetExecutionRunning(_ Context.Context, DatabaseID string, ExecutionID string) (Execution, error) {
	Exec, Ok := M.Executions[ExecutionID]
	if !Ok || Exec.DatabaseID != DatabaseID {
		return Execution{}, ErrNotFound
	}
	Exec.Status = ExecutionRunning
	M.Executions[ExecutionID] = Exec
	return Exec, nil
}

func (M *MockRepo) RecordBatchUp(_ Context.Context, ExecutionID string, DatabaseID string, _ int64, _ int64, _ int64, _ int64) (Execution, error) {
	return M.GetExecution(Context.Background(), DatabaseID, ExecutionID)
}

func (M *MockRepo) RecordBatchReverted(_ Context.Context, RollbackExecutionID string, _ string, DatabaseID string, _ int64) (Execution, error) {
	return M.GetExecution(Context.Background(), DatabaseID, RollbackExecutionID)
}

func (M *MockRepo) ListCommittedBatches(_ Context.Context, _ string) ([]Batch, error) {
	return []Batch{}, nil
}

func (M *MockRepo) CreateExecution(_ Context.Context, DatabaseID string, MigrationID string, Request ExecutionRequest) (Execution, error) {
	Item, Ok := M.Items[MigrationID]
	if !Ok || Item.DatabaseID != DatabaseID {
		return Execution{}, ErrNotFound
	}
	M.NextExecution++
	Now := Time.Now().UTC()
	LockedAt := Now
	Item.LockedAt = &LockedAt
	Item.UpdatedAt = Now
	M.Items[MigrationID] = Item
	Exec := Execution{
		ID:                    Fmt.Sprintf("exec-%d", M.NextExecution),
		MigrationID:           MigrationID,
		DatabaseID:            DatabaseID,
		Version:               Item.Version,
		Revision:              Item.Revision,
		Title:                 Item.Title,
		Kind:                  Request.Kind,
		Direction:             Request.Direction,
		Status:                ExecutionRunning,
		SQLText:               Request.SQLText,
		UpSQL:                 Item.SQLText,
		DownSQL:               Item.DownSQL,
		RollbackOfExecutionID: Request.RollbackOf,
		RetryOfExecutionID:    Request.RetryOf,
		RequestedBy:           Request.RequestedBy,
		CreatedAt:             Now,
		StartedAt:             &Now,
		Attempts:              []Attempt{},
	}
	M.Executions[Exec.ID] = Exec
	M.Order = append(M.Order, Exec.ID)
	return Exec, nil
}

func modeFor(Item Migration) string {
	if Item.BatchMode {
		return ModeBatch
	}
	return ModeNormal
}

func (M *MockRepo) GetExecution(_ Context.Context, DatabaseID string, ExecutionID string) (Execution, error) {
	Exec, Ok := M.Executions[ExecutionID]
	if !Ok || Exec.DatabaseID != DatabaseID {
		return Execution{}, ErrNotFound
	}
	Exec.Attempts = append([]Attempt{}, M.Attempts[ExecutionID]...)
	return Exec, nil
}

func (M *MockRepo) LatestExecution(_ Context.Context, DatabaseID string, MigrationID string) (Execution, error) {
	for Index := len(M.Order) - 1; Index >= 0; Index-- {
		Exec := M.Executions[M.Order[Index]]
		if Exec.DatabaseID == DatabaseID && Exec.MigrationID == MigrationID {
			Exec.Attempts = append([]Attempt{}, M.Attempts[Exec.ID]...)
			return Exec, nil
		}
	}
	return Execution{}, ErrNotFound
}

func (M *MockRepo) LatestSuccessfulExecution(_ Context.Context, DatabaseID string, MigrationID string) (Execution, error) {
	for Index := len(M.Order) - 1; Index >= 0; Index-- {
		Exec := M.Executions[M.Order[Index]]
		if Exec.DatabaseID == DatabaseID && Exec.MigrationID == MigrationID && Exec.Status == ExecutionSucceeded {
			Exec.Attempts = append([]Attempt{}, M.Attempts[Exec.ID]...)
			return Exec, nil
		}
	}
	return Execution{}, ErrNotFound
}

func (M *MockRepo) CreateAttempt(_ Context.Context, ExecutionID string, ActorID *string, SQLText string) (Attempt, error) {
	Exec, Ok := M.Executions[ExecutionID]
	if !Ok {
		return Attempt{}, ErrExecutionMissing
	}
	M.NextAttempt++
	Now := Time.Now().UTC()
	Attempt := Attempt{
		ID:          Fmt.Sprintf("attempt-%d", M.NextAttempt),
		ExecutionID: ExecutionID,
		AttemptNo:   len(M.Attempts[ExecutionID]) + 1,
		Version:     Exec.Version,
		Revision:    Exec.Revision,
		ActorID:     ActorID,
		SQLText:     SQLText,
		Status:      AttemptRunning,
		StartedAt:   Now,
	}
	M.Attempts[ExecutionID] = append(M.Attempts[ExecutionID], Attempt)
	return Attempt, nil
}

func (M *MockRepo) FinishAttempt(_ Context.Context, AttemptID string, Status AttemptStatus, ExecutionErr *string) error {
	for ExecutionID, Items := range M.Attempts {
		for Index := range Items {
			if Items[Index].ID != AttemptID {
				continue
			}
			Now := Time.Now().UTC()
			Items[Index].Status = Status
			Items[Index].ErrorMessage = ExecutionErr
			Items[Index].FinishedAt = &Now
			Duration := Now.Sub(Items[Index].StartedAt).Milliseconds()
			Items[Index].DurationMillis = &Duration
			M.Attempts[ExecutionID] = Items
			return nil
		}
	}
	return ErrExecutionMissing
}

func (M *MockRepo) FinishExecution(_ Context.Context, ExecutionID string, Status ExecutionStatus, ExecutionErr *string) error {
	Exec, Ok := M.Executions[ExecutionID]
	if !Ok {
		return ErrExecutionMissing
	}
	Now := Time.Now().UTC()
	Exec.Status = Status
	Exec.ErrorMessage = ExecutionErr
	Exec.FinishedAt = &Now
	M.Executions[ExecutionID] = Exec
	return nil
}

func (M *MockRepo) RefreshStatus(_ Context.Context, DatabaseID string, MigrationID string) (Migration, error) {
	if M.RefreshFailure != nil {
		return Migration{}, M.RefreshFailure
	}
	Item, Ok := M.Items[MigrationID]
	if !Ok || Item.DatabaseID != DatabaseID {
		return Migration{}, ErrNotFound
	}
	Latest, LatestOk := Execution{}, false
	Success, SuccessOk := Execution{}, false
	for _, ID := range M.Order {
		Exec := M.Executions[ID]
		if Exec.DatabaseID != DatabaseID || Exec.MigrationID != MigrationID {
			continue
		}
		Latest, LatestOk = Exec, true
		if Exec.Status == ExecutionSucceeded {
			Success, SuccessOk = Exec, true
		}
	}
	Applied, AppliedOk := Execution{}, false
	for _, ID := range M.Order {
		Exec := M.Executions[ID]
		if Exec.DatabaseID == DatabaseID && Exec.MigrationID == MigrationID && Exec.Direction == DirectionUp && (Exec.Status == ExecutionSucceeded || Exec.Status == ExecutionPartial) {
			Applied, AppliedOk = Exec, true
		}
	}
	switch {
	case !LatestOk:
		Item.Status = StatusDraft
	case Latest.Status == ExecutionQueued || Latest.Status == ExecutionRunning:
		Item.Status = StatusPending
	case Latest.Status == ExecutionPartial:
		Item.Status = StatusPartial
	case Latest.Status == ExecutionPartiallyReverted:
		Item.Status = StatusPartiallyReverted
	case Latest.Status == ExecutionSucceeded && Latest.Direction == DirectionUp:
		Item.Status = StatusApplied
	case Latest.Status == ExecutionSucceeded && Latest.Direction == DirectionDown:
		Item.Status = StatusRolledBack
	case AppliedOk && Applied.Status == ExecutionPartial:
		Item.Status = StatusPartial
	case AppliedOk && Applied.Direction == DirectionUp:
		Item.Status = StatusApplied
	case SuccessOk && Success.Direction == DirectionDown:
		Item.Status = StatusRolledBack
	default:
		Item.Status = StatusFailed
	}
	Item.ErrorMessage = nil
	if (Item.Status == StatusFailed || Item.Status == StatusPartial || Item.Status == StatusPartiallyReverted) && Latest.ErrorMessage != nil {
		Item.ErrorMessage = Latest.ErrorMessage
	}
	Item.ExecutedAt = nil
	Item.ExecutedBy = nil
	if SuccessOk {
		Item.ExecutedAt = Success.FinishedAt
		Item.ExecutedBy = Success.RequestedBy
	}
	Item.UpdatedAt = Time.Now().UTC()
	M.Items[MigrationID] = Item
	return Item, nil
}

func (M *MockRepo) ListHistory(_ Context.Context, DatabaseID string, HideRollback bool) ([]Execution, error) {
	Hidden := map[string]bool{}
	if HideRollback {
		for _, Exec := range M.Executions {
			if Exec.DatabaseID == DatabaseID && Exec.Kind == KindRollback && Exec.Status == ExecutionSucceeded && Exec.RollbackOfExecutionID != nil {
				Hidden[Exec.ID] = true
				Hidden[*Exec.RollbackOfExecutionID] = true
			}
		}
	}
	Result := []Execution{}
	for _, ID := range M.Order {
		Exec := M.Executions[ID]
		if Exec.DatabaseID != DatabaseID || Hidden[ID] {
			continue
		}
		Exec.Attempts = append([]Attempt{}, M.Attempts[ID]...)
		Result = append(Result, Exec)
	}
	return Result, nil
}

type MockTx struct {
	ExecError   error
	CommitError error
	ExecutedSQL string
	Committed   bool
	RolledBack  bool
}

func (T *MockTx) Query(_ Context.Context, _ string, _ ...any) (Pgx.Rows, error) {
	return nil, nil
}

func (T *MockTx) Exec(_ Context.Context, SQL string, _ ...any) error {
	T.ExecutedSQL = SQL
	return T.ExecError
}

func (T *MockTx) ExecScript(Ctx Context.Context, SQL string) error {
	return T.Exec(Ctx, SQL)
}

func (T *MockTx) Commit(_ Context.Context) error {
	T.Committed = true
	return T.CommitError
}

func (T *MockTx) Rollback(_ Context.Context) error {
	T.RolledBack = true
	return nil
}

type MockConn struct {
	Tx     *MockTx
	Closed bool
}

func (C *MockConn) Ping(_ Context.Context) error { return nil }
func (C *MockConn) Begin(_ Context.Context) (Driver.Tx, error) {
	return C.Tx, nil
}
func (C *MockConn) Query(_ Context.Context, _ string, _ ...any) (Pgx.Rows, error) { return nil, nil }
func (C *MockConn) Exec(_ Context.Context, _ string, _ ...any) error              { return nil }
func (C *MockConn) Close()                                                        { C.Closed = true }

type MockDriver struct {
	Conn      *MockConn
	OpenError error
}

func (D *MockDriver) Open(_ string) (Driver.Conn, error) {
	if D.OpenError != nil {
		return nil, D.OpenError
	}
	return D.Conn, nil
}

func newMockService(Repo *MockRepo, Tx *MockTx) *Service {
	return &Service{
		Repo:   Repo,
		Access: Access.AllowAll(),
		Resolver: DSNResolverFunc(func(Context.Context, string) (string, error) {
			return "target", nil
		}),
		Driver: &MockDriver{Conn: &MockConn{Tx: Tx}},
		Logger: Slog.New(Slog.NewTextHandler(Io.Discard, nil)),
	}
}

func validInput(Title, Up, Down string) MigrationInput {
	return MigrationInput{Title: Title, SQLText: Up, DownSQL: Down}
}

func TestServiceAccessControl(T *Testing.T) {
	Ctx := Context.Background()
	Viewer := Auth.Principal{ID: "viewer-1", Username: "viewer"}
	Editor := Auth.Principal{ID: "editor-1", Username: "editor"}
	Policy := Access.StaticEvaluator{
		DefaultLevel: Access.LevelNone,
		Grants: map[string]map[string]Access.Level{
			"viewer-1": {"db-1": Access.LevelViewOnly},
			"editor-1": {"db-1": Access.LevelViewEdit},
		},
	}
	Repo := NewMockRepo()
	Server := &Service{Repo: Repo, Access: Policy, Resolver: DSNResolverFunc(func(Context.Context, string) (string, error) { return "target", nil })}
	Input := validInput("New Table", "CREATE TABLE t (id INT);", "DROP TABLE t;")
	if _, Err := Server.List(Ctx, Viewer, "db-1"); Err != nil {
		T.Fatalf("viewer list should be allowed: %v", Err)
	}
	if _, Err := Server.Create(Ctx, Viewer, "db-1", Input); !Errors.Is(Err, ErrForbidden) {
		T.Fatalf("viewer create must return ErrForbidden, got %v", Err)
	}
	if _, Err := Server.UpdateDraft(Ctx, Viewer, "db-1", "mig-1", Input); !Errors.Is(Err, ErrForbidden) {
		T.Fatalf("viewer update draft must return ErrForbidden, got %v", Err)
	}
	if Err := Server.DeleteDraft(Ctx, Viewer, "db-1", "mig-1"); !Errors.Is(Err, ErrForbidden) {
		T.Fatalf("viewer delete draft must return ErrForbidden, got %v", Err)
	}
	for _, Action := range []func(Context.Context, Auth.Principal, string, string) (Execution, error){
		Server.Execute, Server.Apply, Server.Rollback, Server.Forward, Server.Retry,
	} {
		if _, Err := Action(Ctx, Viewer, "db-1", "mig-1"); !Errors.Is(Err, ErrForbidden) {
			T.Fatalf("viewer execution must return ErrForbidden, got %v", Err)
		}
	}
	Created, Err := Server.Create(Ctx, Editor, "db-1", Input)
	if Err != nil || Created.ID == "" {
		T.Fatalf("editor create failed: %v", Err)
	}
}

func TestServiceExecutionSuccessFailureAndRetry(T *Testing.T) {
	Ctx := Context.Background()
	Editor := Auth.Principal{ID: "editor-1", Username: "editor"}

	TxSuccess := &MockTx{}
	RepoSuccess := NewMockRepo()
	Server := newMockService(RepoSuccess, TxSuccess)
	Created, Err := Server.Create(Ctx, Editor, "db-1", validInput("Users", "CREATE TABLE users (id INT);", "DROP TABLE users;"))
	if Err != nil {
		T.Fatalf("create failed: %v", Err)
	}
	Executed, Err := Server.Execute(Ctx, Editor, "db-1", Created.ID)
	if Err != nil {
		T.Fatalf("execute failed: %v", Err)
	}
	if Executed.Status != ExecutionSucceeded || !TxSuccess.Committed {
		T.Fatalf("expected committed execution, got %+v", Executed)
	}
	if RepoSuccess.Items[Created.ID].Status != StatusApplied {
		T.Fatal("migration should be applied")
	}
	if _, Err := Server.Execute(Ctx, Editor, "db-1", Created.ID); !Errors.Is(Err, ErrAlreadyExecuted) {
		T.Fatalf("expected ErrAlreadyExecuted, got %v", Err)
	}
	if _, Err := Server.UpdateDraft(Ctx, Editor, "db-1", Created.ID, validInput("Changed", "SELECT 1;", "SELECT 1;")); !Errors.Is(Err, ErrLocked) {
		T.Fatalf("executed definition must be locked, got %v", Err)
	}

	TxFail := &MockTx{ExecError: Errors.New("syntax error")}
	RepoFail := NewMockRepo()
	ServerFail := newMockService(RepoFail, TxFail)
	CreatedFail, Err := ServerFail.Create(Ctx, Editor, "db-1", validInput("Broken", "BOGUS SQL;", "SELECT 1;"))
	if Err != nil {
		T.Fatalf("create failed: %v", Err)
	}
	Failed, Err := ServerFail.Execute(Ctx, Editor, "db-1", CreatedFail.ID)
	if !Errors.Is(Err, Driver.ErrExecution) {
		T.Fatalf("expected ErrExecution, got %v", Err)
	}
	if Failed.Status != ExecutionFailed || !TxFail.RolledBack || TxFail.Committed {
		T.Fatalf("expected rolled back execution failure, got %+v", Failed)
	}
	if RepoFail.Items[CreatedFail.ID].Status != StatusFailed {
		T.Fatal("migration should be failed")
	}
	TxFail.ExecError = nil
	Retried, Err := ServerFail.Execute(Ctx, Editor, "db-1", CreatedFail.ID)
	if Err != nil {
		T.Fatalf("retry after failure failed: %v", Err)
	}
	if Retried.Kind != KindRetry || Retried.RetryOfExecutionID == nil || *Retried.RetryOfExecutionID != Failed.ID {
		T.Fatalf("retry did not reference the failed execution: %+v", Retried)
	}
}

func TestServiceRollbackForwardAndOrdering(T *Testing.T) {
	Ctx := Context.Background()
	Editor := Auth.Principal{ID: "editor", Username: "editor"}
	Repo := NewMockRepo()
	Server := newMockService(Repo, &MockTx{})
	A, Err := Server.Create(Ctx, Editor, "db", validInput("A", "CREATE TABLE a (id INT);", "DROP TABLE a;"))
	if Err != nil {
		T.Fatal(Err)
	}
	B, Err := Server.Create(Ctx, Editor, "db", validInput("B", "CREATE TABLE b (id INT);", "DROP TABLE b;"))
	if Err != nil {
		T.Fatal(Err)
	}
	ApplyA, Err := Server.Execute(Ctx, Editor, "db", A.ID)
	if Err != nil {
		T.Fatal(Err)
	}
	ApplyB, Err := Server.Execute(Ctx, Editor, "db", B.ID)
	if Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Server.Rollback(Ctx, Editor, "db", A.ID); !Errors.Is(Err, ErrRollbackOrder) {
		T.Fatalf("expected ErrRollbackOrder, got %v", Err)
	}
	RollbackB, Err := Server.Rollback(Ctx, Editor, "db", B.ID)
	if Err != nil {
		T.Fatal(Err)
	}
	if RollbackB.Kind != KindRollback || RollbackB.Direction != DirectionDown || RollbackB.RollbackOfExecutionID == nil || *RollbackB.RollbackOfExecutionID != ApplyB.ID {
		T.Fatalf("rollback did not reference applied execution: %+v", RollbackB)
	}
	if Repo.Items[B.ID].Status != StatusRolledBack {
		T.Fatal("B should be rolled back")
	}
	Visible, Err := Server.History(Ctx, Editor, "db", false)
	if Err != nil || len(Visible) != 3 {
		T.Fatalf("full history should have 3 executions: %v %+v", Err, Visible)
	}
	Hidden, Err := Server.History(Ctx, Editor, "db", true)
	if Err != nil || len(Hidden) != 1 || Hidden[0].ID != ApplyA.ID {
		T.Fatalf("hidden history should contain only A apply: %v %+v", Err, Hidden)
	}
	if _, Err := Server.Rollback(Ctx, Editor, "db", A.ID); Err != nil {
		T.Fatalf("A can be rolled back after B is rolled back: %v", Err)
	}
	if _, Err := Server.Forward(Ctx, Editor, "db", A.ID); Err != nil {
		T.Fatalf("A should be forwardable: %v", Err)
	}
	ForwardB, Err := Server.Forward(Ctx, Editor, "db", B.ID)
	if Err != nil {
		T.Fatal(Err)
	}
	if ForwardB.Kind != KindForward || ForwardB.Direction != DirectionUp {
		T.Fatalf("unexpected forward execution: %+v", ForwardB)
	}
}

func TestServiceIrreversibleAndForwardOrdering(T *Testing.T) {
	Ctx := Context.Background()
	Editor := Auth.Principal{ID: "editor", Username: "editor"}
	Repo := NewMockRepo()
	Server := newMockService(Repo, &MockTx{})
	Irreversible, Err := Server.Create(Ctx, Editor, "db", MigrationInput{Title: "Data loss", SQLText: "DELETE FROM t;", Irreversible: true, IrreversibleReason: "Removed rows cannot be restored"})
	if Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Server.Execute(Ctx, Editor, "db", Irreversible.ID); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Server.Rollback(Ctx, Editor, "db", Irreversible.ID); !Errors.Is(Err, ErrIrreversible) {
		T.Fatalf("expected ErrIrreversible, got %v", Err)
	}

	Repo2 := NewMockRepo()
	Server2 := newMockService(Repo2, &MockTx{})
	A, _ := Server2.Create(Ctx, Editor, "db", validInput("A", "SELECT 1;", "SELECT 1;"))
	B, _ := Server2.Create(Ctx, Editor, "db", validInput("B", "SELECT 2;", "SELECT 2;"))
	if _, Err := Server2.Execute(Ctx, Editor, "db", A.ID); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Server2.Execute(Ctx, Editor, "db", B.ID); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Server2.Rollback(Ctx, Editor, "db", B.ID); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Server2.Rollback(Ctx, Editor, "db", A.ID); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Server2.Forward(Ctx, Editor, "db", B.ID); !Errors.Is(Err, ErrForwardOrder) {
		T.Fatalf("B cannot be forwarded before A: %v", Err)
	}
	if _, Err := Server2.Forward(Ctx, Editor, "db", A.ID); Err != nil {
		T.Fatalf("A should be forwardable: %v", Err)
	}
	if _, Err := Server2.Forward(Ctx, Editor, "db", B.ID); Err != nil {
		T.Fatalf("B should be forwardable after A: %v", Err)
	}
}

func TestUncertainExecutionCannotBeRetried(T *Testing.T) {
	Ctx := Context.Background()
	Editor := Auth.Principal{ID: "editor", Username: "editor"}
	Tx := &MockTx{CommitError: Driver.ErrUnavailable}
	Repo := NewMockRepo()
	Server := newMockService(Repo, Tx)
	Created, Err := Server.Create(Ctx, Editor, "db", validInput("Uncertain", "SELECT 1;", "SELECT 2;"))
	if Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Server.Execute(Ctx, Editor, "db", Created.ID); !Errors.Is(Err, ErrUnavailable) {
		T.Fatalf("unexpected execution error: %v", Err)
	}
	if Repo.Items[Created.ID].Status != StatusPending {
		T.Fatalf("uncertain execution must remain pending, got %s", Repo.Items[Created.ID].Status)
	}
	if _, Err := Server.Execute(Ctx, Editor, "db", Created.ID); !Errors.Is(Err, ErrPending) {
		T.Fatalf("uncertain execution was retried: %v", Err)
	}
}

func TestAnnotateCapabilities(T *Testing.T) {
	Items := []Migration{
		{ID: "a", Version: 1, Status: StatusApplied, DownSQL: strPtr("down"), CanRollback: false},
		{ID: "b", Version: 2, Status: StatusRolledBack, DownSQL: strPtr("down")},
		{ID: "c", Version: 3, Status: StatusDraft},
	}
	History := []Execution{
		{ID: "e1", MigrationID: "a", Status: ExecutionSucceeded, Direction: DirectionUp},
		{ID: "e2", MigrationID: "b", Status: ExecutionSucceeded, Direction: DirectionUp},
		{ID: "e3", MigrationID: "b", Status: ExecutionSucceeded, Direction: DirectionDown},
	}
	Result := AnnotateCapabilities(Items, History)
	if !Result[0].CanRollback || Result[0].CanApply || Result[0].CanForward {
		T.Fatalf("unexpected A capabilities: %+v", Result[0])
	}
	if !Result[1].CanForward || Result[1].CanRollback {
		T.Fatalf("unexpected B capabilities: %+v", Result[1])
	}
	if Result[2].CanApply {
		T.Fatalf("C must wait for B forward: %+v", Result[2])
	}
}

func strPtr(Value string) *string { return &Value }

type FlakyDriver struct {
	Conn     *MockConn
	Failures int
	Calls    int
}

func (D *FlakyDriver) Open(_ string) (Driver.Conn, error) {
	D.Calls++
	if D.Calls <= D.Failures {
		return nil, Driver.ErrUnavailable
	}
	return D.Conn, nil
}

func TestWorkerRetriesTransientFailureWithBackoff(T *Testing.T) {
	Ctx := Context.Background()
	Editor := Auth.Principal{ID: "editor", Username: "editor"}
	Repo := NewMockRepo()
	Flaky := &FlakyDriver{Conn: &MockConn{Tx: &MockTx{}}, Failures: 1}
	Service := &Service{
		Repo:     Repo,
		Access:   Access.AllowAll(),
		Resolver: DSNResolverFunc(func(Context.Context, string) (string, error) { return "target", nil }),
		Driver:   Flaky,
		Logger:   Slog.New(Slog.NewTextHandler(Io.Discard, nil)),
	}
	Created, Err := Service.Create(Ctx, Editor, "db", validInput("Retry me", "SELECT 1;", "SELECT 2;"))
	if Err != nil {
		T.Fatal(Err)
	}
	Queued, Err := Service.Enqueue(Ctx, Editor, "db", Created.ID, JobApply, "")
	if Err != nil {
		T.Fatal(Err)
	}
	Worker := &Worker{Repo: Repo, Service: Service, ID: "worker", Lease: Time.Minute}
	First, Err := Repo.ClaimJob(Ctx, "worker", Time.Minute)
	if Err != nil {
		T.Fatal(Err)
	}
	Worker.Process(Ctx, First)
	RetryJob, Ok := Repo.Jobs[Queued.ID]
	if !Ok || RetryJob.Status != JobQueued || RetryJob.Attempts != 1 || RetryJob.LastError == nil {
		T.Fatalf("transient failure was not requeued: %+v", RetryJob)
	}
	if !RetryJob.AvailableAt.After(Time.Now()) {
		T.Fatal("retry was not delayed")
	}
	Second, Err := Repo.ClaimJob(Ctx, "worker", Time.Minute)
	if Err != nil || Second.ID != Queued.ID || Second.Attempts != 2 {
		T.Fatalf("retry claim failed: %+v %v", Second, Err)
	}
	Worker.Process(Ctx, Second)
	Finished := Repo.Jobs[Queued.ID]
	if Finished.Status != JobSucceeded || Finished.ExecutionID == nil {
		T.Fatalf("retry did not succeed: %+v", Finished)
	}
	if Repo.Items[Created.ID].Status != StatusApplied {
		T.Fatal("retry did not apply the migration")
	}
}
