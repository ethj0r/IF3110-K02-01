package Migration

import (
	Context "context"
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
	Create(Ctx Context.Context, DatabaseID string, Input MigrationInput, CreatedBy *string) (Migration, error)
	Get(Ctx Context.Context, DatabaseID string, ID string) (Migration, error)
	GetByVersion(Ctx Context.Context, DatabaseID string, Version int64) (Migration, error)
	List(Ctx Context.Context, DatabaseID string) ([]Migration, error)
	UpdateDraft(Ctx Context.Context, DatabaseID string, ID string, Input MigrationInput) (Migration, error)
	StartExecution(Ctx Context.Context, DatabaseID, ID string, ExecutedBy *string) (Migration, error)
	RecordExecution(Ctx Context.Context, DatabaseID string, ID string, ExecutedBy *string, Succeeded bool, ExecutionErr *string) (Migration, error)
	DeleteDraft(Ctx Context.Context, DatabaseID string, ID string) error
}

type Service struct {
	Repo     MigrationRepository
	Access   Access.Evaluator
	Resolver DSNResolver
	Driver   Driver.Driver
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
	var UserID *string
	if User.ID != "" {
		UserID = &User.ID
	}
	return Server.Repo.Create(Ctx, DatabaseID, Input, UserID)
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
	return Server.Repo.List(Ctx, DatabaseID)
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

func (Server *Service) Execute(Ctx Context.Context, User Auth.Principal, DatabaseID string, ID string) (Migration, error) {
	Ctx, Cancel := Context.WithTimeout(Ctx, 30*Time.Second)
	defer Cancel()
	DatabaseID = Strings.TrimSpace(DatabaseID)
	ID = Strings.TrimSpace(ID)
	if DatabaseID == "" || ID == "" {
		return Migration{}, ErrNotFound
	}
	if Err := Server.checkAccess(Ctx, User, Access.ActionEdit, DatabaseID); Err != nil {
		return Migration{}, Err
	}

	if Server.Resolver == nil {
		return Migration{}, ErrUnavailable
	}
	DSN, Err := Server.Resolver.DialTargetDSN(Ctx, DatabaseID)
	if Err != nil {
		return Migration{}, Err
	}
	return Server.Repo.WithDatabaseLock(Ctx, DatabaseID, func(Locked MigrationRepository) (Migration, error) {
		return Server.execute(Ctx, Locked, User, DatabaseID, ID, DSN)
	})
}

func (Server *Service) execute(Ctx Context.Context, Repo MigrationRepository, User Auth.Principal, DatabaseID, ID, DSN string) (Migration, error) {
	Current, Err := Repo.Get(Ctx, DatabaseID, ID)
	if Err != nil {
		return Migration{}, Err
	}
	if Current.Status.IsSuccess() {
		return Current, ErrAlreadyExecuted
	}
	if Current.Status == StatusPending {
		return Current, ErrPending
	}
	History, Err := Repo.List(Ctx, DatabaseID)
	if Err != nil {
		return Migration{}, Err
	}
	for _, Item := range History {
		if (Item.Version < Current.Version && !Item.Status.IsTerminal()) || (Item.Version > Current.Version && !Item.Status.IsDraft()) {
			return Migration{}, ErrOutOfOrder
		}
	}

	TargetConn, Err := Server.getDriver().Open(DSN)
	if Err != nil {
		return Migration{}, ErrUnavailable
	}
	defer TargetConn.Close()

	TargetTx, Err := TargetConn.Begin(Ctx)
	if Err != nil {
		return Migration{}, ErrUnavailable
	}
	defer func() {
		Cleanup, Cancel := Context.WithTimeout(Context.Background(), 5*Time.Second)
		defer Cancel()
		_ = TargetTx.Rollback(Cleanup)
	}()

	var UserID *string
	if User.ID != "" {
		UserID = &User.ID
	}

	if _, Err := Repo.StartExecution(Ctx, DatabaseID, ID, UserID); Err != nil {
		return Migration{}, Err
	}
	ExecErr := TargetTx.ExecScript(Ctx, Current.SQLText)
	RecordCtx, Cancel := Context.WithTimeout(Context.WithoutCancel(Ctx), 5*Time.Second)
	defer Cancel()
	if ExecErr != nil {
		if Err := TargetTx.Rollback(RecordCtx); Err != nil {
			return Migration{}, ErrUnavailable
		}
		ErrDetail := Driver.ErrExecution.Error()
		Updated, RecordErr := Repo.RecordExecution(RecordCtx, DatabaseID, ID, UserID, false, &ErrDetail)
		if RecordErr != nil {
			return Migration{}, RecordErr
		}
		return Updated, Driver.ErrExecution
	}

	if Err := TargetTx.Commit(Ctx); Err != nil {
		// A failed commit response does not prove the target rolled back.
		return Migration{}, ErrUnavailable
	}

	return Repo.RecordExecution(RecordCtx, DatabaseID, ID, UserID, true, nil)
}
