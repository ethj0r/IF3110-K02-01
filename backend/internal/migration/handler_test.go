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
	Time "time"
)

func newTestHandler(Policy Access.Evaluator, DriverMock Driver.Driver) (Http.Handler, *MockRepo, *Auth.Principal, *Service) {
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
		R.Post(Prefix+"/analyze", Routes.Analyze)
		R.Get(Prefix+"/history", Routes.History)
		R.Get(Prefix+"/{migrationID}", Routes.Show)
		R.Put(Prefix+"/{migrationID}", Routes.Update)
		R.Delete(Prefix+"/{migrationID}", Routes.Delete)
		R.Post(Prefix+"/{migrationID}/execute", Routes.Execute)
		R.Post(Prefix+"/{migrationID}/apply", Routes.Apply)
		R.Post(Prefix+"/{migrationID}/rollback", Routes.Rollback)
		R.Post(Prefix+"/{migrationID}/forward", Routes.Forward)
		R.Post(Prefix+"/{migrationID}/retry", Routes.Retry)
		R.Post(Prefix+"/{migrationID}/executions/{executionID}/reconcile", Routes.Reconcile)
		R.Get(Prefix+"/{migrationID}/executions/{executionID}/evidence", Routes.Evidence)
	}
	registerRoutes("/databases/{dbID}/migrations")
	registerRoutes("/connections/{dbID}/migrations")
	Jobs := &Handler{Service: Service}
	R.Get("/databases/{dbID}/jobs", Jobs.Jobs)
	R.Get("/databases/{dbID}/jobs/{jobID}", Jobs.Job)
	R.Get("/connections/{dbID}/jobs/{jobID}", Jobs.Job)

	return R, Repo, CurrentPrincipal, Service
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

	Router, Repo, CurrentPrincipal, Service := newTestHandler(Policy, DriverMock)

	// 1. Create Migration as Editor -> 201 Created
	CreatePayload := `{"title":"Add users table","sql_text":"CREATE TABLE users (id SERIAL);","down_sql":"DROP TABLE users;"}`
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
	UpdatePayload := `{"title":"Add users table updated","sql_text":"CREATE TABLE users (id BIGSERIAL);","down_sql":"DROP TABLE users;"}`
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

	// 6. Switch back to Editor and enqueue Execute -> 202 Accepted
	*CurrentPrincipal = Auth.Principal{ID: "user-editor", Username: "editor"}
	Req = Httptest.NewRequest("POST", "/databases/db-1/migrations/"+Created.ID+"/execute", nil)
	Res = Httptest.NewRecorder()
	Router.ServeHTTP(Res, Req)
	if Res.Code != 202 {
		T.Fatalf("expected 202 Accepted for execute, got %d: %s", Res.Code, Res.Body.String())
	}
	var Queued Job
	if Err := Json.NewDecoder(Res.Body).Decode(&Queued); Err != nil || Queued.ID == "" || Queued.Status != JobQueued {
		T.Fatalf("invalid enqueued job: %+v %v", Queued, Err)
	}
	Worker := &Worker{Repo: Repo, Service: Service, ID: "test-worker", Lease: Time.Minute}
	Claimed, Err := Repo.ClaimJob(Context.Background(), "test-worker", Time.Minute)
	if Err != nil || Claimed.ID != Queued.ID {
		T.Fatalf("could not claim job: %+v %v", Claimed, Err)
	}
	Worker.Process(Context.Background(), Claimed)
	JobResponse := Httptest.NewRecorder()
	Router.ServeHTTP(JobResponse, Httptest.NewRequest("GET", "/databases/db-1/jobs/"+Queued.ID, nil))
	if JobResponse.Code != 200 {
		T.Fatalf("job status returned %d: %s", JobResponse.Code, JobResponse.Body.String())
	}
	var Finished Job
	_ = Json.NewDecoder(JobResponse.Body).Decode(&Finished)
	if Finished.Status != JobSucceeded || Finished.ExecutionID == nil {
		T.Fatalf("job did not succeed: %+v", Finished)
	}

	// 6b. Recent jobs: viewers can list them, bad limits and ungranted users are rejected.
	*CurrentPrincipal = Auth.Principal{ID: "user-viewer", Username: "viewer"}
	ListResponse := Httptest.NewRecorder()
	Router.ServeHTTP(ListResponse, Httptest.NewRequest("GET", "/databases/db-1/jobs?limit=5", nil))
	var Recent []Job
	if ListResponse.Code != 200 || Json.NewDecoder(ListResponse.Body).Decode(&Recent) != nil || len(Recent) != 1 || Recent[0].ID != Queued.ID || Recent[0].Status != JobSucceeded {
		T.Fatalf("job list returned %d: %+v", ListResponse.Code, Recent)
	}
	for _, Query := range []string{"?limit=0", "?limit=51", "?limit=many"} {
		BadLimit := Httptest.NewRecorder()
		Router.ServeHTTP(BadLimit, Httptest.NewRequest("GET", "/databases/db-1/jobs"+Query, nil))
		if BadLimit.Code != 400 {
			T.Fatalf("job list %s returned %d", Query, BadLimit.Code)
		}
	}
	*CurrentPrincipal = Auth.Principal{ID: "user-blocked", Username: "blocked"}
	Denied := Httptest.NewRecorder()
	Router.ServeHTTP(Denied, Httptest.NewRequest("GET", "/databases/db-1/jobs", nil))
	if Denied.Code != 403 {
		T.Fatalf("ungranted job list returned %d", Denied.Code)
	}
	*CurrentPrincipal = Auth.Principal{ID: "user-editor", Username: "editor"}

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

	Router, Repo, _, Service := newTestHandler(Policy, DriverFail)

	// Create migration
	CreatePayload := `{"title":"Broken SQL","sql_text":"INVALID SQL;","down_sql":"SELECT 1;"}`
	Req := Httptest.NewRequest("POST", "/databases/db-1/migrations", Bytes.NewBufferString(CreatePayload))
	Req.Header.Set("Content-Type", "application/json")
	Res := Httptest.NewRecorder()
	Router.ServeHTTP(Res, Req)

	var Created Migration
	_ = Json.NewDecoder(Res.Body).Decode(&Created)

	// Execute enqueues a job; the worker records the failure.
	Req = Httptest.NewRequest("POST", "/databases/db-1/migrations/"+Created.ID+"/execute", nil)
	Res = Httptest.NewRecorder()
	Router.ServeHTTP(Res, Req)
	if Res.Code != 202 {
		T.Fatalf("expected 202 Accepted, got %d: %s", Res.Code, Res.Body.String())
	}
	var Queued Job
	_ = Json.NewDecoder(Res.Body).Decode(&Queued)
	Worker := &Worker{Repo: Repo, Service: Service, ID: "test-worker", Lease: Time.Minute}
	Claimed, _ := Repo.ClaimJob(Context.Background(), "test-worker", Time.Minute)
	Worker.Process(Context.Background(), Claimed)
	JobResponse := Httptest.NewRecorder()
	Router.ServeHTTP(JobResponse, Httptest.NewRequest("GET", "/databases/db-1/jobs/"+Queued.ID, nil))
	var Failed Job
	_ = Json.NewDecoder(JobResponse.Body).Decode(&Failed)
	if Failed.Status != JobFailed || Failed.LastError == nil || !Strings.Contains(*Failed.LastError, "target SQL execution failed") {
		T.Fatalf("job failure was not recorded: %+v", Failed)
	}
}

func TestHandlerAnalyzeEndpoint(T *Testing.T) {
	Policy := Access.StaticEvaluator{
		DefaultLevel: Access.LevelViewOnly,
	}
	Router, _, _, _ := newTestHandler(Policy, nil)

	// Bad JSON -> 400
	Req := Httptest.NewRequest("POST", "/databases/db-1/migrations/analyze", Bytes.NewBufferString("invalid json"))
	Res := Httptest.NewRecorder()
	Router.ServeHTTP(Res, Req)
	if Res.Code != 400 {
		T.Fatalf("expected 400 for invalid json, got %d", Res.Code)
	}

	// Empty SQL -> 400
	Req = Httptest.NewRequest("POST", "/databases/db-1/migrations/analyze", Bytes.NewBufferString(`{"sql_text": "   "}`))
	Res = Httptest.NewRecorder()
	Router.ServeHTTP(Res, Req)
	if Res.Code != 400 {
		T.Fatalf("expected 400 for empty sql, got %d", Res.Code)
	}
}

func TestHandlerReconcileResponses(T *Testing.T) {
	Policy := Access.StaticEvaluator{
		DefaultLevel: Access.LevelNone,
		Grants: map[string]map[string]Access.Level{
			"user-editor": {"db-1": Access.LevelViewEdit},
		},
	}
	Router, Repo, Principal, _ := newTestHandler(Policy, &MockDriver{Conn: &MockConn{Tx: &MockTx{}}})
	Repo.Executions["exec-1"] = Execution{ID: "exec-1", DatabaseID: "db-1", MigrationID: "mig-1", Status: ExecutionRunning, Mode: ModeBatch, Direction: DirectionUp, CommittedBatches: 1}
	Path := "/databases/db-1/migrations/mig-1/executions/exec-1/reconcile"
	Send := func(Body string) (int, string) {
		Recorder := Httptest.NewRecorder()
		Router.ServeHTTP(Recorder, Httptest.NewRequest(Http.MethodPost, Path, Strings.NewReader(Body)))
		var Payload struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
			Status string `json:"status"`
		}
		_ = Json.Unmarshal(Recorder.Body.Bytes(), &Payload)
		if Payload.Error.Code != "" {
			return Recorder.Code, Payload.Error.Code
		}
		return Recorder.Code, Payload.Status
	}
	if Code, Value := Send(`{"outcome":"failed"}`); Code != 403 || Value != "forbidden" {
		T.Fatalf("editor without platform admin must get 403, got %d %s", Code, Value)
	}
	Principal.IsPlatformAdmin = true
	if Code, Value := Send(`{"outcome":"failed","extra":1}`); Code != 400 || Value != "invalid_request" {
		T.Fatalf("unknown fields must get 400, got %d %s", Code, Value)
	}
	if Code, Value := Send(`{"outcome":"succeeded"}`); Code != 409 || Value != "batch_reconcile" {
		T.Fatalf("batch succeeded must get 409 batch_reconcile, got %d %s", Code, Value)
	}
	if Code, Value := Send(`{"outcome":"failed","note":"worker host lost"}`); Code != 200 || Value != string(ExecutionPartial) {
		T.Fatalf("reconcile should stop the batch at its checkpoint, got %d %s", Code, Value)
	}
	if Code, Value := Send(`{"outcome":"failed"}`); Code != 409 || Value != "reconcile_not_allowed" {
		T.Fatalf("second reconcile must get 409, got %d %s", Code, Value)
	}
	Principal.ID = "user-stranger"
	Recorder := Httptest.NewRecorder()
	Router.ServeHTTP(Recorder, Httptest.NewRequest(Http.MethodGet, "/databases/db-1/migrations/mig-1/executions/exec-1/evidence", nil))
	if Recorder.Code != 403 {
		T.Fatalf("evidence without a grant must get 403, got %d", Recorder.Code)
	}
}
