package Migration

import (
	Bytes "bytes"
	Context "context"
	Json "encoding/json"
	Errors "errors"
	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
	Chi "github.com/go-chi/chi/v5"
	Http "net/http"
	Httptest "net/http/httptest"
	Strings "strings"
	Testing "testing"
)

func newTestHandler(Policy Access.Evaluator, DriverMock Driver.Driver) (Http.Handler, *MockRepo, *Auth.Principal) {
	CurrentPrincipal := &Auth.Principal{ID: "user-editor", Username: "editor"}
	Repo := NewMockRepo()
	Service := &Service{
		Repo:   Repo,
		Access: Policy,
		Resolver: DSNResolverFunc(func(_ Context.Context, _ string) (string, error) {
			return "postgres://user:pass@localhost:5432/db", nil
		}),
		Driver: DriverMock,
	}

	R := Chi.NewRouter()
	R.Use(func(Next Http.Handler) Http.Handler {
		return Http.HandlerFunc(func(W Http.ResponseWriter, Req *Http.Request) {
			if CurrentPrincipal != nil {
				Ctx := Context.WithValue(Req.Context(), Auth.SessionContext, Auth.Authenticated{
					Principal: *CurrentPrincipal,
				})
				Req = Req.WithContext(Ctx)
			}
			Next.ServeHTTP(W, Req)
		})
	})

	registerRoutes := func(Prefix string) {
		Routes := &Handler{Service: Service}
		R.Get(Prefix, Routes.List)
		R.Post(Prefix, Routes.Create)
		R.Get(Prefix+"/{migrationID}", Routes.Show)
		R.Put(Prefix+"/{migrationID}", Routes.Update)
		R.Delete(Prefix+"/{migrationID}", Routes.Delete)
		R.Post(Prefix+"/{migrationID}/execute", Routes.Execute)
	}
	registerRoutes("/databases/{dbID}/migrations")
	registerRoutes("/connections/{dbID}/migrations")

	return R, Repo, CurrentPrincipal
}

func TestHandlerLifecycleAndAccess(T *Testing.T) {
	Policy := Access.StaticEvaluator{
		DefaultLevel: Access.LevelNone,
		Grants: map[string]map[string]Access.Level{
			"user-editor": {"db-1": Access.LevelViewEdit},
			"user-viewer": {"db-1": Access.LevelViewOnly},
		},
	}

	TxMock := &MockTx{}
	ConnMock := &MockConn{Tx: TxMock}
	DriverMock := &MockDriver{Conn: ConnMock}

	Router, _, CurrentPrincipal := newTestHandler(Policy, DriverMock)

	// 1. Create Migration as Editor -> 201 Created
	CreatePayload := `{"title":"Add users table","sql_text":"CREATE TABLE users (id SERIAL);"}`
	Req := Httptest.NewRequest("POST", "/databases/db-1/migrations", Bytes.NewBufferString(CreatePayload))
	Req.Header.Set("Content-Type", "application/json")
	Res := Httptest.NewRecorder()
	Router.ServeHTTP(Res, Req)

	if Res.Code != 201 {
		T.Fatalf("expected 201 Created, got %d: %s", Res.Code, Res.Body.String())
	}
	var Created Migration
	if Err := Json.NewDecoder(Res.Body).Decode(&Created); Err != nil || Created.ID == "" {
		T.Fatalf("failed to decode created migration: %v", Err)
	}

	// 2. List Migrations as Editor -> 200 OK
	Req = Httptest.NewRequest("GET", "/databases/db-1/migrations", nil)
	Res = Httptest.NewRecorder()
	Router.ServeHTTP(Res, Req)
	if Res.Code != 200 {
		T.Fatalf("expected 200 OK, got %d", Res.Code)
	}

	// 3. Show Migration -> 200 OK
	Req = Httptest.NewRequest("GET", "/databases/db-1/migrations/"+Created.ID, nil)
	Res = Httptest.NewRecorder()
	Router.ServeHTTP(Res, Req)
	if Res.Code != 200 {
		T.Fatalf("expected 200 OK, got %d", Res.Code)
	}

	// 4. Update Migration Draft -> 200 OK
	UpdatePayload := `{"title":"Add users table updated","sql_text":"CREATE TABLE users (id BIGSERIAL);"}`
	Req = Httptest.NewRequest("PUT", "/databases/db-1/migrations/"+Created.ID, Bytes.NewBufferString(UpdatePayload))
	Req.Header.Set("Content-Type", "application/json")
	Res = Httptest.NewRecorder()
	Router.ServeHTTP(Res, Req)
	if Res.Code != 200 {
		T.Fatalf("expected 200 OK, got %d: %s", Res.Code, Res.Body.String())
	}

	// 5. Test Access Denied for Viewer
	*CurrentPrincipal = Auth.Principal{ID: "user-viewer", Username: "viewer"}

	// Viewer can list
	Req = Httptest.NewRequest("GET", "/databases/db-1/migrations", nil)
	Res = Httptest.NewRecorder()
	Router.ServeHTTP(Res, Req)
	if Res.Code != 200 {
		T.Fatalf("viewer should be allowed to list, got %d", Res.Code)
	}

	// Viewer cannot create -> 403 Forbidden
	Req = Httptest.NewRequest("POST", "/databases/db-1/migrations", Bytes.NewBufferString(CreatePayload))
	Req.Header.Set("Content-Type", "application/json")
	Res = Httptest.NewRecorder()
	Router.ServeHTTP(Res, Req)
	if Res.Code != 403 {
		T.Fatalf("viewer create should return 403 Forbidden, got %d: %s", Res.Code, Res.Body.String())
	}

	// Viewer cannot execute -> 403 Forbidden
	Req = Httptest.NewRequest("POST", "/databases/db-1/migrations/"+Created.ID+"/execute", nil)
	Res = Httptest.NewRecorder()
	Router.ServeHTTP(Res, Req)
	if Res.Code != 403 {
		T.Fatalf("viewer execute should return 403 Forbidden, got %d: %s", Res.Code, Res.Body.String())
	}

	// 6. Switch back to Editor and Execute -> 200 OK
	*CurrentPrincipal = Auth.Principal{ID: "user-editor", Username: "editor"}
	Req = Httptest.NewRequest("POST", "/databases/db-1/migrations/"+Created.ID+"/execute", nil)
	Res = Httptest.NewRecorder()
	Router.ServeHTTP(Res, Req)
	if Res.Code != 200 {
		T.Fatalf("expected 200 OK for execute, got %d: %s", Res.Code, Res.Body.String())
	}

	// 7. Re-executing already committed migration -> 409 Conflict
	Req = Httptest.NewRequest("POST", "/databases/db-1/migrations/"+Created.ID+"/execute", nil)
	Res = Httptest.NewRecorder()
	Router.ServeHTTP(Res, Req)
	if Res.Code != 409 {
		T.Fatalf("re-executing committed migration must return 409, got %d", Res.Code)
	}

	// 8. Updating committed migration -> 409 Conflict
	Req = Httptest.NewRequest("PUT", "/databases/db-1/migrations/"+Created.ID, Bytes.NewBufferString(UpdatePayload))
	Req.Header.Set("Content-Type", "application/json")
	Res = Httptest.NewRecorder()
	Router.ServeHTTP(Res, Req)
	if Res.Code != 409 {
		T.Fatalf("updating committed migration must return 409, got %d", Res.Code)
	}

	// 9. Unknown migration -> 404
	Req = Httptest.NewRequest("GET", "/databases/db-1/migrations/missing-id", nil)
	Res = Httptest.NewRecorder()
	Router.ServeHTTP(Res, Req)
	if Res.Code != 404 {
		T.Fatalf("expected 404 for missing migration, got %d", Res.Code)
	}

	// 10. Invalid request body -> 400
	Req = Httptest.NewRequest("POST", "/databases/db-1/migrations", Strings.NewReader("not json"))
	Req.Header.Set("Content-Type", "application/json")
	Res = Httptest.NewRecorder()
	Router.ServeHTTP(Res, Req)
	if Res.Code != 400 {
		T.Fatalf("expected 400 for malformed json, got %d", Res.Code)
	}
}

func TestHandlerExecutionFailureResponse(T *Testing.T) {
	Policy := Access.AllowAll()
	TxFail := &MockTx{ExecError: Errors.New("syntax error")}
	ConnFail := &MockConn{Tx: TxFail}
	DriverFail := &MockDriver{Conn: ConnFail}

	Router, _, _ := newTestHandler(Policy, DriverFail)

	// Create migration
	CreatePayload := `{"title":"Broken SQL","sql_text":"INVALID SQL;"}`
	Req := Httptest.NewRequest("POST", "/databases/db-1/migrations", Bytes.NewBufferString(CreatePayload))
	Req.Header.Set("Content-Type", "application/json")
	Res := Httptest.NewRecorder()
	Router.ServeHTTP(Res, Req)

	var Created Migration
	_ = Json.NewDecoder(Res.Body).Decode(&Created)

	// Execute -> should return 422 execution_failed
	Req = Httptest.NewRequest("POST", "/databases/db-1/migrations/"+Created.ID+"/execute", nil)
	Res = Httptest.NewRecorder()
	Router.ServeHTTP(Res, Req)

	if Res.Code != 422 {
		T.Fatalf("expected 422 execution_failed, got %d: %s", Res.Code, Res.Body.String())
	}
	if !Strings.Contains(Res.Body.String(), "execution_failed") {
		T.Fatalf("response body should contain execution_failed: %s", Res.Body.String())
	}
}
