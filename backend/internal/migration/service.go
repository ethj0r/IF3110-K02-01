package Migration

import (
	Context "context"
	Errors "errors"
	Slog "log/slog"
	Strings "strings"
	Time "time"

	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
)

type DSNResolver interface {
	DialTargetDSN(Ctx Context.Context, DatabaseID string) (string, error)
}

type DSNResolverFunc func(Ctx Context.Context, DatabaseID string) (string, error)

func (F DSNResolverFunc) DialTargetDSN(Ctx Context.Context, DatabaseID string) (string, error) {
	return F(Ctx, DatabaseID)
}

type MigrationRepository interface {
	WithDatabaseLock(Context.Context, string, func(MigrationRepository) (Migration, error)) (Migration, error)
	WithExecutionLock(Context.Context, string, func(MigrationRepository) (Execution, error)) (Execution, error)
	Create(Ctx Context.Context, DatabaseID string, Input MigrationInput, CreatedBy *string) (Migration, error)
	Get(Ctx Context.Context, DatabaseID string, ID string) (Migration, error)
	GetByVersion(Ctx Context.Context, DatabaseID string, Version int64) (Migration, error)
	List(Ctx Context.Context, DatabaseID string) ([]Migration, error)
	UpdateDraft(Ctx Context.Context, DatabaseID string, ID string, Input MigrationInput) (Migration, error)
	DeleteDraft(Ctx Context.Context, DatabaseID string, ID string) error
	CreateExecution(Ctx Context.Context, DatabaseID string, MigrationID string, Request ExecutionRequest) (Execution, error)
	GetExecution(Ctx Context.Context, DatabaseID string, ExecutionID string) (Execution, error)
	LatestExecution(Ctx Context.Context, DatabaseID string, MigrationID string) (Execution, error)
	LatestSuccessfulExecution(Ctx Context.Context, DatabaseID string, MigrationID string) (Execution, error)
	CreateAttempt(Ctx Context.Context, ExecutionID string, ActorID *string, SQLText string) (Attempt, error)
	FinishAttempt(Ctx Context.Context, AttemptID string, Status AttemptStatus, ExecutionErr *string) error
	FinishExecution(Ctx Context.Context, ExecutionID string, Status ExecutionStatus, ExecutionErr *string) error
	RefreshStatus(Ctx Context.Context, DatabaseID string, MigrationID string) (Migration, error)
	ListHistory(Ctx Context.Context, DatabaseID string, HideRollback bool) ([]Execution, error)
}

type Service struct {
	Repo     MigrationRepository
	Access   Access.Evaluator
	Resolver DSNResolver
	Driver   Driver.Driver
	Logger   *Slog.Logger
}

func (Server *Service) logger() *Slog.Logger {
	if Server.Logger != nil {
		return Server.Logger
	}
	return Slog.Default()
}

func (Server *Service) checkAccess(Ctx Context.Context, User Auth.Principal, Act Access.Action, DatabaseID string) error {
	Allowed, Err := Access.Can(Ctx, Server.Access, User, Act, DatabaseID)
	if Err != nil {
		return Err
	}
	if !Allowed {
		return ErrForbidden
	}
	return nil
}

func (Server *Service) getDriver() Driver.Driver {
	if Server.Driver != nil {
		return Server.Driver
	}
	return Driver.PostgresDriver{}
}

func actorID(User Auth.Principal) *string {
	if User.ID == "" {
		return nil
	}
	return &User.ID
}

func safeMessage(Err error) *string {
	Message := Driver.ErrExecution.Error()
	if Errors.Is(Err, ErrUnavailable) || Errors.Is(Err, Driver.ErrUnavailable) || Errors.Is(Err, Driver.ErrInvalidDSN) {
		Message = Driver.ErrUnavailable.Error()
	}
	return &Message
}

func (Server *Service) Create(Ctx Context.Context, User Auth.Principal, DatabaseID string, Input MigrationInput) (Migration, error) {
	DatabaseID = Strings.TrimSpace(DatabaseID)
	if DatabaseID == "" {
		return Migration{}, ErrNotFound
	}
	if Err := Server.checkAccess(Ctx, User, Access.ActionEdit, DatabaseID); Err != nil {
		return Migration{}, Err
	}
	if Err := Input.Validate(); Err != nil {
		return Migration{}, Err
	}
	return Server.Repo.Create(Ctx, DatabaseID, Input, actorID(User))
}

func (Server *Service) Get(Ctx Context.Context, User Auth.Principal, DatabaseID string, ID string) (Migration, error) {
	DatabaseID = Strings.TrimSpace(DatabaseID)
	ID = Strings.TrimSpace(ID)
	if DatabaseID == "" || ID == "" {
		return Migration{}, ErrNotFound
	}
	if Err := Server.checkAccess(Ctx, User, Access.ActionView, DatabaseID); Err != nil {
		return Migration{}, Err
	}
	return Server.Repo.Get(Ctx, DatabaseID, ID)
}

func (Server *Service) List(Ctx Context.Context, User Auth.Principal, DatabaseID string) ([]Migration, error) {
	DatabaseID = Strings.TrimSpace(DatabaseID)
	if DatabaseID == "" {
		return nil, ErrNotFound
	}
	if Err := Server.checkAccess(Ctx, User, Access.ActionView, DatabaseID); Err != nil {
		return nil, Err
	}
	Items, Err := Server.Repo.List(Ctx, DatabaseID)
	if Err != nil {
		return nil, Err
	}
	History, Err := Server.Repo.ListHistory(Ctx, DatabaseID, false)
	if Err != nil {
		return nil, Err
	}
	return AnnotateCapabilities(Items, History), nil
}

func (Server *Service) History(Ctx Context.Context, User Auth.Principal, DatabaseID string, HideRollback bool) ([]Execution, error) {
	DatabaseID = Strings.TrimSpace(DatabaseID)
	if DatabaseID == "" {
		return nil, ErrNotFound
	}
	if Err := Server.checkAccess(Ctx, User, Access.ActionView, DatabaseID); Err != nil {
		return nil, Err
	}
	return Server.Repo.ListHistory(Ctx, DatabaseID, HideRollback)
}

func (Server *Service) UpdateDraft(Ctx Context.Context, User Auth.Principal, DatabaseID string, ID string, Input MigrationInput) (Migration, error) {
	DatabaseID = Strings.TrimSpace(DatabaseID)
	ID = Strings.TrimSpace(ID)
	if DatabaseID == "" || ID == "" {
		return Migration{}, ErrNotFound
	}
	if Err := Server.checkAccess(Ctx, User, Access.ActionEdit, DatabaseID); Err != nil {
		return Migration{}, Err
	}
	if Err := Input.Validate(); Err != nil {
		return Migration{}, Err
	}
	return Server.Repo.UpdateDraft(Ctx, DatabaseID, ID, Input)
}

func (Server *Service) DeleteDraft(Ctx Context.Context, User Auth.Principal, DatabaseID string, ID string) error {
	DatabaseID = Strings.TrimSpace(DatabaseID)
	ID = Strings.TrimSpace(ID)
	if DatabaseID == "" || ID == "" {
		return ErrNotFound
	}
	if Err := Server.checkAccess(Ctx, User, Access.ActionEdit, DatabaseID); Err != nil {
		return Err
	}
	return Server.Repo.DeleteDraft(Ctx, DatabaseID, ID)
}

func (Server *Service) Execute(Ctx Context.Context, User Auth.Principal, DatabaseID string, ID string) (Execution, error) {
	return Server.withAction(Ctx, User, DatabaseID, ID, func(Ctx Context.Context, Repo MigrationRepository, User Auth.Principal, Current Migration) (Execution, error) {
		Latest, Err := Repo.LatestExecution(Ctx, Current.DatabaseID, Current.ID)
		if Errors.Is(Err, ErrNotFound) {
			return Server.applyLocked(Ctx, Repo, User, Current)
		}
		if Err != nil {
			return Execution{}, Err
		}
		switch {
		case Latest.Status == ExecutionQueued || Latest.Status == ExecutionRunning:
			return Execution{}, ErrPending
		case Latest.Status.IsFailure():
			return Server.retryLocked(Ctx, Repo, User, Current, Latest)
		default:
			return Execution{}, ErrAlreadyExecuted
		}
	})
}

func (Server *Service) Apply(Ctx Context.Context, User Auth.Principal, DatabaseID string, ID string) (Execution, error) {
	return Server.withAction(Ctx, User, DatabaseID, ID, func(Ctx Context.Context, Repo MigrationRepository, User Auth.Principal, Current Migration) (Execution, error) {
		return Server.applyLocked(Ctx, Repo, User, Current)
	})
}

func (Server *Service) Rollback(Ctx Context.Context, User Auth.Principal, DatabaseID string, ID string) (Execution, error) {
	return Server.withAction(Ctx, User, DatabaseID, ID, func(Ctx Context.Context, Repo MigrationRepository, User Auth.Principal, Current Migration) (Execution, error) {
		return Server.rollbackLocked(Ctx, Repo, User, Current)
	})
}

func (Server *Service) Forward(Ctx Context.Context, User Auth.Principal, DatabaseID string, ID string) (Execution, error) {
	return Server.withAction(Ctx, User, DatabaseID, ID, func(Ctx Context.Context, Repo MigrationRepository, User Auth.Principal, Current Migration) (Execution, error) {
		return Server.forwardLocked(Ctx, Repo, User, Current)
	})
}

func (Server *Service) Retry(Ctx Context.Context, User Auth.Principal, DatabaseID string, ID string) (Execution, error) {
	return Server.withAction(Ctx, User, DatabaseID, ID, func(Ctx Context.Context, Repo MigrationRepository, User Auth.Principal, Current Migration) (Execution, error) {
		Latest, Err := Repo.LatestExecution(Ctx, Current.DatabaseID, Current.ID)
		if Err != nil {
			if Errors.Is(Err, ErrNotFound) {
				return Execution{}, ErrNoFailedAttempt
			}
			return Execution{}, Err
		}
		return Server.retryLocked(Ctx, Repo, User, Current, Latest)
	})
}

func (Server *Service) withAction(Ctx Context.Context, User Auth.Principal, DatabaseID string, ID string, Run func(Context.Context, MigrationRepository, Auth.Principal, Migration) (Execution, error)) (Execution, error) {
	Ctx, Cancel := Context.WithTimeout(Ctx, 30*Time.Second)
	defer Cancel()
	DatabaseID = Strings.TrimSpace(DatabaseID)
	ID = Strings.TrimSpace(ID)
	if DatabaseID == "" || ID == "" {
		return Execution{}, ErrNotFound
	}
	if Err := Server.checkAccess(Ctx, User, Access.ActionEdit, DatabaseID); Err != nil {
		return Execution{}, Err
	}
	if Server.Resolver == nil {
		return Execution{}, ErrUnavailable
	}
	return Server.Repo.WithExecutionLock(Ctx, DatabaseID, func(Repo MigrationRepository) (Execution, error) {
		Current, Err := Repo.Get(Ctx, DatabaseID, ID)
		if Err != nil {
			return Execution{}, Err
		}
		return Run(Ctx, Repo, User, Current)
	})
}

func (Server *Service) applyLocked(Ctx Context.Context, Repo MigrationRepository, User Auth.Principal, Current Migration) (Execution, error) {
	if Current.LockedAt != nil || !Current.Status.IsDraft() {
		return Execution{}, ErrAlreadyExecuted
	}
	if _, Err := Repo.LatestExecution(Ctx, Current.DatabaseID, Current.ID); Err == nil {
		return Execution{}, ErrAlreadyExecuted
	} else if !Errors.Is(Err, ErrNotFound) {
		return Execution{}, Err
	}
	Items, History, Err := Server.loadState(Ctx, Repo, Current.DatabaseID)
	if Err != nil {
		return Execution{}, Err
	}
	if HasPending(History) {
		return Execution{}, ErrPending
	}
	if !CanApplyNow(Items, Current.Version) {
		return Execution{}, ErrOutOfOrder
	}
	return Server.startExecution(Ctx, Repo, User, Current, ExecutionRequest{
		Kind:        KindApply,
		Direction:   DirectionUp,
		SQLText:     Current.SQLText,
		RequestedBy: actorID(User),
	})
}

func (Server *Service) rollbackLocked(Ctx Context.Context, Repo MigrationRepository, User Auth.Principal, Current Migration) (Execution, error) {
	if Current.Irreversible || Current.DownSQL == nil {
		return Execution{}, ErrIrreversible
	}
	Success, Err := Repo.LatestSuccessfulExecution(Ctx, Current.DatabaseID, Current.ID)
	if Err != nil {
		if Errors.Is(Err, ErrNotFound) {
			return Execution{}, ErrNotApplied
		}
		return Execution{}, Err
	}
	if Success.Direction != DirectionUp {
		return Execution{}, ErrNotApplied
	}
	Items, History, Err := Server.loadState(Ctx, Repo, Current.DatabaseID)
	if Err != nil {
		return Execution{}, Err
	}
	if HasPending(History) {
		return Execution{}, ErrPending
	}
	if Current.Version != MaxAppliedVersion(Items) {
		return Execution{}, ErrRollbackOrder
	}
	return Server.startExecution(Ctx, Repo, User, Current, ExecutionRequest{
		Kind:        KindRollback,
		Direction:   DirectionDown,
		SQLText:     *Current.DownSQL,
		RollbackOf:  &Success.ID,
		RequestedBy: actorID(User),
	})
}

func (Server *Service) forwardLocked(Ctx Context.Context, Repo MigrationRepository, User Auth.Principal, Current Migration) (Execution, error) {
	Success, Err := Repo.LatestSuccessfulExecution(Ctx, Current.DatabaseID, Current.ID)
	if Err != nil && !Errors.Is(Err, ErrNotFound) {
		return Execution{}, Err
	}
	if Errors.Is(Err, ErrNotFound) || Success.Direction != DirectionDown {
		return Execution{}, ErrNotRolledBack
	}
	Items, History, Err := Server.loadState(Ctx, Repo, Current.DatabaseID)
	if Err != nil {
		return Execution{}, Err
	}
	if HasPending(History) {
		return Execution{}, ErrPending
	}
	if Current.Version != NextVersionAfter(Items, MaxAppliedVersion(Items)) {
		return Execution{}, ErrForwardOrder
	}
	return Server.startExecution(Ctx, Repo, User, Current, ExecutionRequest{
		Kind:        KindForward,
		Direction:   DirectionUp,
		SQLText:     Current.SQLText,
		RequestedBy: actorID(User),
	})
}

func (Server *Service) retryLocked(Ctx Context.Context, Repo MigrationRepository, User Auth.Principal, Current Migration, Latest Execution) (Execution, error) {
	if !Latest.Status.IsFailure() {
		return Execution{}, ErrNoFailedAttempt
	}
	_, History, Err := Server.loadState(Ctx, Repo, Current.DatabaseID)
	if Err != nil {
		return Execution{}, Err
	}
	if HasPending(History) {
		return Execution{}, ErrPending
	}
	if len(History) == 0 || History[len(History)-1].ID != Latest.ID {
		return Execution{}, ErrOutOfOrder
	}
	return Server.startExecution(Ctx, Repo, User, Current, ExecutionRequest{
		Kind:        KindRetry,
		Direction:   Latest.Direction,
		SQLText:     Latest.SQLText,
		RetryOf:     &Latest.ID,
		RequestedBy: actorID(User),
	})
}

func (Server *Service) loadState(Ctx Context.Context, Repo MigrationRepository, DatabaseID string) ([]Migration, []Execution, error) {
	Items, Err := Repo.List(Ctx, DatabaseID)
	if Err != nil {
		return nil, nil, Err
	}
	History, Err := Repo.ListHistory(Ctx, DatabaseID, false)
	if Err != nil {
		return nil, nil, Err
	}
	return Items, History, nil
}

func (Server *Service) startExecution(Ctx Context.Context, Repo MigrationRepository, User Auth.Principal, Current Migration, Request ExecutionRequest) (Execution, error) {
	Exec, Err := Repo.CreateExecution(Ctx, Current.DatabaseID, Current.ID, Request)
	if Err != nil {
		return Execution{}, Err
	}
	RecordCtx, Cancel := Context.WithTimeout(Context.WithoutCancel(Ctx), 5*Time.Second)
	defer Cancel()
	if _, Err := Repo.RefreshStatus(RecordCtx, Current.DatabaseID, Current.ID); Err != nil {
		return Exec, ErrUnavailable
	}
	return Server.runExecution(Ctx, Repo, User, Current, Exec)
}

func (Server *Service) runExecution(Ctx Context.Context, Repo MigrationRepository, User Auth.Principal, Current Migration, Exec Execution) (Execution, error) {
	Attempt, Err := Repo.CreateAttempt(Ctx, Exec.ID, actorID(User), Exec.SQLText)
	if Err != nil {
		return Exec, ErrUnavailable
	}
	Server.logger().Info("migration execution started",
		"event", "migration.execution.started",
		"actor_id", User.ID, "actor", User.Username,
		"database_id", Current.DatabaseID, "migration_id", Current.ID,
		"execution_id", Exec.ID, "version", Current.Version, "revision", Exec.Revision,
		"kind", Exec.Kind, "direction", Exec.Direction, "attempt", Attempt.AttemptNo,
		"sql", Exec.SQLText)

	if Server.Resolver == nil {
		return Server.finishFailure(Ctx, Repo, Exec, Attempt, Driver.ErrUnavailable)
	}
	DSN, Err := Server.Resolver.DialTargetDSN(Ctx, Current.DatabaseID)
	if Err != nil {
		return Server.finishFailure(Ctx, Repo, Exec, Attempt, Err)
	}
	TargetConn, Err := Server.getDriver().Open(DSN)
	if Err != nil {
		return Server.finishFailure(Ctx, Repo, Exec, Attempt, Err)
	}
	defer TargetConn.Close()

	TargetTx, Err := TargetConn.Begin(Ctx)
	if Err != nil {
		return Server.finishFailure(Ctx, Repo, Exec, Attempt, Err)
	}
	Closed := false
	defer func() {
		if Closed {
			return
		}
		Cleanup, Cancel := Context.WithTimeout(Context.Background(), 5*Time.Second)
		defer Cancel()
		_ = TargetTx.Rollback(Cleanup)
	}()

	if Err := TargetTx.ExecScript(Ctx, Exec.SQLText); Err != nil {
		if RollbackErr := TargetTx.Rollback(Ctx); RollbackErr != nil {
			Server.logger().Error("migration execution outcome is uncertain after rollback failure",
				"event", "migration.execution.uncertain",
				"database_id", Current.DatabaseID, "migration_id", Current.ID,
				"execution_id", Exec.ID, "sql", Exec.SQLText)
			return Exec, ErrUnavailable
		}
		Closed = true
		Server.logger().Error("migration SQL failed; target transaction rolled back",
			"event", "migration.execution.rolled_back",
			"database_id", Current.DatabaseID, "migration_id", Current.ID,
			"execution_id", Exec.ID, "attempt", Attempt.AttemptNo, "sql", Exec.SQLText)
		return Server.finishFailure(Ctx, Repo, Exec, Attempt, Driver.ErrExecution)
	}

	if Err := TargetTx.Commit(Ctx); Err != nil {
		Server.logger().Error("migration commit response is uncertain",
			"event", "migration.execution.uncertain",
			"database_id", Current.DatabaseID, "migration_id", Current.ID,
			"execution_id", Exec.ID, "sql", Exec.SQLText)
		return Exec, ErrUnavailable
	}
	Closed = true
	Server.logger().Info("migration target transaction committed",
		"event", "migration.execution.committed",
		"database_id", Current.DatabaseID, "migration_id", Current.ID,
		"execution_id", Exec.ID, "attempt", Attempt.AttemptNo, "sql", Exec.SQLText)

	RecordCtx, Cancel := Context.WithTimeout(Context.WithoutCancel(Ctx), 5*Time.Second)
	defer Cancel()
	if Err := Repo.FinishAttempt(RecordCtx, Attempt.ID, AttemptSucceeded, nil); Err != nil {
		return Exec, ErrUnavailable
	}
	if Err := Repo.FinishExecution(RecordCtx, Exec.ID, ExecutionSucceeded, nil); Err != nil {
		return Exec, ErrUnavailable
	}
	if _, Err := Repo.RefreshStatus(RecordCtx, Current.DatabaseID, Current.ID); Err != nil {
		return Exec, ErrUnavailable
	}
	Updated, Err := Repo.GetExecution(RecordCtx, Current.DatabaseID, Exec.ID)
	if Err != nil {
		return Exec, ErrUnavailable
	}
	Server.logger().Info("migration execution succeeded",
		"event", "migration.execution.succeeded",
		"actor_id", User.ID, "actor", User.Username,
		"database_id", Current.DatabaseID, "migration_id", Current.ID,
		"execution_id", Exec.ID, "version", Current.Version, "revision", Exec.Revision,
		"kind", Exec.Kind, "direction", Exec.Direction, "attempt", Attempt.AttemptNo)
	return Updated, nil
}

func (Server *Service) finishFailure(Ctx Context.Context, Repo MigrationRepository, Exec Execution, Attempt Attempt, Cause error) (Execution, error) {
	RecordCtx, Cancel := Context.WithTimeout(Context.WithoutCancel(Ctx), 5*Time.Second)
	defer Cancel()
	Safe := safeMessage(Cause)
	if Err := Repo.FinishAttempt(RecordCtx, Attempt.ID, AttemptFailed, Safe); Err != nil {
		return Exec, ErrUnavailable
	}
	if Err := Repo.FinishExecution(RecordCtx, Exec.ID, ExecutionFailed, Safe); Err != nil {
		return Exec, ErrUnavailable
	}
	if _, Err := Repo.RefreshStatus(RecordCtx, Exec.DatabaseID, Exec.MigrationID); Err != nil {
		return Exec, ErrUnavailable
	}
	Updated, Err := Repo.GetExecution(RecordCtx, Exec.DatabaseID, Exec.ID)
	if Err != nil {
		return Exec, Cause
	}
	Server.logger().Error("migration execution failed",
		"event", "migration.execution.failed",
		"database_id", Exec.DatabaseID, "migration_id", Exec.MigrationID,
		"execution_id", Exec.ID, "attempt", Attempt.AttemptNo,
		"status", Updated.Status, "error", *Safe)
	return Updated, Cause
}

func HasPending(History []Execution) bool {
	for _, Item := range History {
		if Item.Status == ExecutionQueued || Item.Status == ExecutionRunning {
			return true
		}
	}
	return false
}

func MaxAppliedVersion(Items []Migration) int64 {
	Result := int64(0)
	for _, Item := range Items {
		if Item.Status.IsApplied() && Item.Version > Result {
			Result = Item.Version
		}
	}
	return Result
}

func NextVersionAfter(Items []Migration, Version int64) int64 {
	Result := int64(0)
	for _, Item := range Items {
		if Item.Version > Version && (Result == 0 || Item.Version < Result) {
			Result = Item.Version
		}
	}
	return Result
}

func CanApplyNow(Items []Migration, Version int64) bool {
	for _, Item := range Items {
		if Item.Version < Version && !Item.Status.IsApplied() && !Item.Status.IsFailure() {
			return false
		}
		if Item.Version > Version && (Item.LockedAt != nil || !Item.Status.IsDraft()) {
			return false
		}
	}
	return true
}

func AnnotateCapabilities(Items []Migration, History []Execution) []Migration {
	Latest := map[string]Execution{}
	Pending := false
	for _, Item := range History {
		Latest[Item.MigrationID] = Item
		if Item.Status == ExecutionQueued || Item.Status == ExecutionRunning {
			Pending = true
		}
	}
	Last := Execution{}
	if len(History) > 0 {
		Last = History[len(History)-1]
	}
	MaxApplied := MaxAppliedVersion(Items)
	Next := NextVersionAfter(Items, MaxApplied)
	Result := make([]Migration, len(Items))
	copy(Result, Items)
	for Index := range Result {
		Item := &Result[Index]
		Item.CanApply = false
		Item.CanRollback = false
		Item.CanForward = false
		Item.CanRetry = false
		if Pending {
			continue
		}
		switch {
		case Item.LockedAt == nil && Item.Status.IsDraft():
			Item.CanApply = CanApplyNow(Result, Item.Version)
		case Item.Status.IsApplied():
			Item.CanRollback = Item.Version == MaxApplied && !Item.Irreversible && Item.DownSQL != nil
		case Item.Status.IsRolledBack():
			Item.CanForward = Item.Version == Next
		}
		if LatestItem, Ok := Latest[Item.ID]; Ok && LatestItem.Status.IsFailure() && LatestItem.ID == Last.ID {
			Item.CanRetry = true
		}
	}
	return Result
}

func (Server *Service) AnalyzePlan(Ctx Context.Context, User Auth.Principal, DatabaseID string, SQL string) (QueryPlanAnalysis, error) {
	Ctx, Cancel := Context.WithTimeout(Ctx, 15*Time.Second)
	defer Cancel()
	DatabaseID = Strings.TrimSpace(DatabaseID)
	SQL = Strings.TrimSpace(SQL)
	if DatabaseID == "" {
		return QueryPlanAnalysis{}, ErrNotFound
	}
	if SQL == "" {
		return QueryPlanAnalysis{}, ErrInvalidInput
	}
	if Err := Server.checkAccess(Ctx, User, Access.ActionView, DatabaseID); Err != nil {
		return QueryPlanAnalysis{}, Err
	}
	if Server.Resolver == nil {
		return QueryPlanAnalysis{}, ErrUnavailable
	}
	DSN, Err := Server.Resolver.DialTargetDSN(Ctx, DatabaseID)
	if Err != nil {
		return QueryPlanAnalysis{}, Err
	}
	TargetConn, Err := Server.getDriver().Open(DSN)
	if Err != nil {
		return QueryPlanAnalysis{}, ErrUnavailable
	}
	defer TargetConn.Close()

	TargetTx, Err := TargetConn.Begin(Ctx)
	if Err != nil {
		return QueryPlanAnalysis{}, ErrUnavailable
	}
	defer func() {
		Cleanup, CleanupCancel := Context.WithTimeout(Context.Background(), 5*Time.Second)
		defer CleanupCancel()
		_ = TargetTx.Rollback(Cleanup)
	}()

	CleanSQL := Strings.TrimRight(SQL, "; \t\r\n")
	Rows, Err := TargetTx.Query(Ctx, "EXPLAIN (ANALYZE, COSTS, VERBOSE, BUFFERS, FORMAT JSON) "+CleanSQL)
	if Err != nil {
		return QueryPlanAnalysis{}, Driver.ErrExecution
	}
	defer Rows.Close()

	var RawJSON string
	if !Rows.Next() {
		return QueryPlanAnalysis{}, Driver.ErrExecution
	}
	if Err := Rows.Scan(&RawJSON); Err != nil {
		return QueryPlanAnalysis{}, Driver.ErrExecution
	}
	Rows.Close()

	return ParseQueryPlanJSON([]byte(RawJSON))
}
