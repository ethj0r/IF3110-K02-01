package Migration

import (
	Context "context"
	Errors "errors"
	Testing "testing"

	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
	Pgx "github.com/jackc/pgx/v5"
)

type MockRepo struct {
	Items map[string]Migration
}

func NewMockRepo() *MockRepo {
	return &MockRepo{Items: make(map[string]Migration)}
}

func (M *MockRepo) Create(_ Context.Context, DatabaseID string, Input MigrationInput, CreatedBy *string) (Migration, error) {
	Item := Migration{
		ID:         "mig-1",
		DatabaseID: DatabaseID,
		Version:    int64(len(M.Items) + 1),
		Title:      Input.Title,
		SQLText:    Input.SQLText,
		Status:     StatusDraft,
		CreatedBy:  CreatedBy,
	}
	M.Items[Item.ID] = Item
	return Item, nil
}

func (M *MockRepo) Get(_ Context.Context, DatabaseID string, ID string) (Migration, error) {
	Item, Ok := M.Items[ID]
	if !Ok || Item.DatabaseID != DatabaseID {
		return Migration{}, ErrNotFound
	}
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
	return Result, nil
}

func (M *MockRepo) UpdateDraft(_ Context.Context, DatabaseID string, ID string, Input MigrationInput) (Migration, error) {
	Item, Ok := M.Items[ID]
	if !Ok || Item.DatabaseID != DatabaseID {
		return Migration{}, ErrNotFound
	}
	if !Item.Status.IsDraft() {
		return Migration{}, ErrAlreadyExecuted
	}
	Item.Title = Input.Title
	Item.SQLText = Input.SQLText
	M.Items[ID] = Item
	return Item, nil
}

func (M *MockRepo) RecordExecution(_ Context.Context, DatabaseID string, ID string, ExecutedBy *string, Succeeded bool, ExecutionErr *string) (Migration, error) {
	Item, Ok := M.Items[ID]
	if !Ok || Item.DatabaseID != DatabaseID {
		return Migration{}, ErrNotFound
	}
	if Succeeded {
		Item.Status = StatusCommitted
	} else {
		Item.Status = StatusRolledBack
	}
	Item.ExecutedBy = ExecutedBy
	Item.ErrorMessage = ExecutionErr
	M.Items[ID] = Item
	return Item, nil
}

func (M *MockRepo) DeleteDraft(_ Context.Context, DatabaseID string, ID string) error {
	Item, Ok := M.Items[ID]
	if !Ok || Item.DatabaseID != DatabaseID {
		return ErrNotFound
	}
	if !Item.Status.IsDraft() {
		return ErrAlreadyExecuted
	}
	delete(M.Items, ID)
	return nil
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
	Service := &Service{
		Repo:   Repo,
		Access: Policy,
		Resolver: DSNResolverFunc(func(_ Context.Context, _ string) (string, error) {
			return "postgres://user:pass@localhost/db", nil
		}),
	}

	// Viewer can list and get
	if _, Err := Service.List(Ctx, Viewer, "db-1"); Err != nil {
		T.Fatalf("viewer list should be allowed: %v", Err)
	}

	// Viewer is denied for create, update, delete, execute
	Input := MigrationInput{Title: "New Table", SQLText: "CREATE TABLE t (id INT);"}
	if _, Err := Service.Create(Ctx, Viewer, "db-1", Input); !Errors.Is(Err, ErrForbidden) {
		T.Fatalf("viewer create must return ErrForbidden, got %v", Err)
	}
	if _, Err := Service.UpdateDraft(Ctx, Viewer, "db-1", "mig-1", Input); !Errors.Is(Err, ErrForbidden) {
		T.Fatalf("viewer update draft must return ErrForbidden, got %v", Err)
	}
	if Err := Service.DeleteDraft(Ctx, Viewer, "db-1", "mig-1"); !Errors.Is(Err, ErrForbidden) {
		T.Fatalf("viewer delete draft must return ErrForbidden, got %v", Err)
	}
	if _, Err := Service.Execute(Ctx, Viewer, "db-1", "mig-1"); !Errors.Is(Err, ErrForbidden) {
		T.Fatalf("viewer execute must return ErrForbidden, got %v", Err)
	}

	// Editor can create
	Created, Err := Service.Create(Ctx, Editor, "db-1", Input)
	if Err != nil || Created.ID != "mig-1" {
		T.Fatalf("editor create failed: %v", Err)
	}
}

func TestServiceExecutionSuccessAndFailure(T *Testing.T) {
	Ctx := Context.Background()
	Editor := Auth.Principal{ID: "editor-1", Username: "editor"}
	Policy := Access.AllowAll()

	// 1. Success execution test
	TxSuccess := &MockTx{}
	ConnSuccess := &MockConn{Tx: TxSuccess}
	DriverSuccess := &MockDriver{Conn: ConnSuccess}
	RepoSuccess := NewMockRepo()

	ServiceSuccess := &Service{
		Repo:   RepoSuccess,
		Access: Policy,
		Resolver: DSNResolverFunc(func(_ Context.Context, _ string) (string, error) {
			return "postgres://test", nil
		}),
		Driver: DriverSuccess,
	}

	Created, Err := ServiceSuccess.Create(Ctx, Editor, "db-1", MigrationInput{Title: "Users Table", SQLText: "CREATE TABLE users (id INT);"})
	if Err != nil {
		T.Fatalf("create failed: %v", Err)
	}

	Executed, Err := ServiceSuccess.Execute(Ctx, Editor, "db-1", Created.ID)
	if Err != nil {
		T.Fatalf("execute failed: %v", Err)
	}
	if Executed.Status != StatusCommitted || !TxSuccess.Committed || !ConnSuccess.Closed {
		T.Fatalf("expected committed transaction and closed connection, got %+v", Executed)
	}

	// 2. Re-executing committed migration returns ErrAlreadyExecuted
	if _, Err := ServiceSuccess.Execute(Ctx, Editor, "db-1", Created.ID); !Errors.Is(Err, ErrAlreadyExecuted) {
		T.Fatalf("expected ErrAlreadyExecuted, got %v", Err)
	}

	// 3. Failure execution test: SQL error causes rollback and records rolled_back status
	SQLFailErr := Errors.New("syntax error at or near BOGUS")
	TxFail := &MockTx{ExecError: SQLFailErr}
	ConnFail := &MockConn{Tx: TxFail}
	DriverFail := &MockDriver{Conn: ConnFail}
	RepoFail := NewMockRepo()

	ServiceFail := &Service{
		Repo:   RepoFail,
		Access: Policy,
		Resolver: DSNResolverFunc(func(_ Context.Context, _ string) (string, error) {
			return "postgres://test", nil
		}),
		Driver: DriverFail,
	}

	CreatedFail, Err := ServiceFail.Create(Ctx, Editor, "db-1", MigrationInput{Title: "Broken SQL", SQLText: "BOGUS SQL;"})
	if Err != nil {
		T.Fatalf("create failed: %v", Err)
	}

	ExecutedFail, Err := ServiceFail.Execute(Ctx, Editor, "db-1", CreatedFail.ID)
	if !Errors.Is(Err, Driver.ErrExecution) {
		T.Fatalf("expected ErrExecution, got %v", Err)
	}
	if ExecutedFail.Status != StatusRolledBack || !TxFail.RolledBack || TxFail.Committed {
		T.Fatalf("expected rolled back transaction on failure, got %+v", ExecutedFail)
	}
	if ExecutedFail.ErrorMessage == nil || *ExecutedFail.ErrorMessage != SQLFailErr.Error() {
		T.Fatalf("expected error message recorded, got %v", ExecutedFail.ErrorMessage)
	}
}
