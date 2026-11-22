//go:build integration

package Migration

import (
	Bytes "bytes"
	Context "context"
	Json "encoding/json"
	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
	Admin "github.com/ethj0r/IF3110-K02-01/backend/internal/admin"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Connection "github.com/ethj0r/IF3110-K02-01/backend/internal/connection"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
	Inspect "github.com/ethj0r/IF3110-K02-01/backend/internal/inspect"
	Testdb "github.com/ethj0r/IF3110-K02-01/backend/internal/testdb"
	Testutil "github.com/ethj0r/IF3110-K02-01/backend/internal/testutil"
	Chi "github.com/go-chi/chi/v5"
	Pgx "github.com/jackc/pgx/v5"
	Http "net/http"
	Httptest "net/http/httptest"
	Url "net/url"
	Os "os"
	Testing "testing"
)

const FlowPassword = "isolated integration password"

type FlowClient struct {
	Router  Http.Handler
	Cookies map[string]*Http.Cookie
	Tokens  map[string]string
}

func (Client *FlowClient) Request(T *Testing.T, Actor, Method, Path string, Input any, Status int) *Httptest.ResponseRecorder {
	T.Helper()
	var Data []byte
	if Input != nil {
		var Err error
		Data, Err = Json.Marshal(Input)
		if Err != nil {
			T.Fatal(Err)
		}
	}
	Request := Httptest.NewRequest(Method, Path, Bytes.NewReader(Data))
	Request.Header.Set("Origin", "https://app.test")
	Request.Header.Set("Content-Type", "application/json")
	if Cookie := Client.Cookies[Actor]; Cookie != nil {
		Request.AddCookie(Cookie)
	}
	if Token := Client.Tokens[Actor]; Token != "" {
		Request.Header.Set("X-CSRF-Token", Token)
	}
	Response := Httptest.NewRecorder()
	Client.Router.ServeHTTP(Response, Request)
	for _, Cookie := range Response.Result().Cookies() {
		if Cookie.Name == Auth.CookieName {
			Client.Cookies[Actor] = Cookie
		}
	}
	if Response.Code != Status {
		T.Fatalf("%s %s as %s: got %d, want %d", Method, Path, Actor, Response.Code, Status)
	}
	return Response
}

func FlowDecode[Value any](T *Testing.T, Response *Httptest.ResponseRecorder) Value {
	T.Helper()
	var Result Value
	if Err := Json.Unmarshal(Response.Body.Bytes(), &Result); Err != nil {
		T.Fatal(Err)
	}
	return Result
}

func (Client *FlowClient) Login(T *Testing.T, Actor string) {
	T.Helper()
	Response := Client.Request(T, Actor, "POST", "/api/auth/login", map[string]string{"username": Actor, "password": FlowPassword}, 200)
	Body := FlowDecode[struct {
		CSRFToken string `json:"csrf_token"`
	}](T, Response)
	Client.Tokens[Actor] = Body.CSRFToken
}

func TestRealGroupGrantsProtectAuthenticatedMigrationWorkflow(T *Testing.T) {
	Platform, Target := Testdb.PlatformDatabase(T), Testutil.Database(T)
	Settings := Auth.DefaultOptions()
	Settings.Origin, Settings.CookieSecure = "https://app.test", true
	Settings.EncryptionKey = Bytes.Repeat([]byte{31}, 32)
	AuthRepo := &Auth.Repository{Pool: Platform}
	if _, Err := Auth.BootstrapAdmin(Context.Background(), AuthRepo, "admin", FlowPassword); Err != nil {
		T.Fatal(Err)
	}
	Authentication, Err := Auth.NewService(AuthRepo, Settings)
	if Err != nil {
		T.Fatal(Err)
	}
	AuthRoutes := &Auth.Handler{Service: Authentication}
	Cipher, Err := Connection.NewCipher(Settings.EncryptionKey)
	if Err != nil {
		T.Fatal(Err)
	}
	Connections := &Connection.Service{Repo: &Connection.Repository{Pool: Platform}, Cipher: Cipher}
	Policy := &Access.Repository{Pool: Platform}
	AdminRoutes := &Admin.Handler{Auth: AuthRoutes, Service: Admin.NewService(&Admin.Repository{Pool: Platform})}
	ConnRoutes := &Connection.Handler{Auth: AuthRoutes, Service: Connections, Driver: Driver.PostgresDriver{}}
	AccessRoutes := &Access.Handler{Auth: AuthRoutes, Repo: Policy}
	InspectRoutes := &Inspect.Handler{Auth: AuthRoutes, Policy: Policy, Service: &Inspect.Service{Connections: Connections, Driver: Driver.PostgresDriver{}}}
	Repo := NewReviewedRepository(Platform)
	Service := &Service{Repo: Repo, Reviews: Repo, Access: Policy, Driver: Driver.PostgresDriver{}, Resolver: DSNResolverFunc(func(Ctx Context.Context, ID string) (string, error) {
		_, DSN, Err := Connections.DialDSN(Ctx, ID)
		return DSN, Err
	})}
	MigrationRoutes := &Handler{Auth: AuthRoutes, Service: Service}
	Router := Chi.NewRouter()
	Router.Route("/api", func(Api Chi.Router) {
		AuthRoutes.Register(Api)
		AdminRoutes.Register(Api)
		ConnRoutes.Register(Api)
		AccessRoutes.Register(Api)
		InspectRoutes.Register(Api)
		MigrationRoutes.Register(Api)
	})
	Client := &FlowClient{Router: Router, Cookies: map[string]*Http.Cookie{}, Tokens: map[string]string{}}
	Client.Login(T, "admin")
	Users := map[string]Admin.User{}
	for _, Name := range []string{"viewer", "editor", "blocked"} {
		Users[Name] = FlowDecode[Admin.User](T, Client.Request(T, "admin", "POST", "/api/admin/users", map[string]string{"username": Name, "password": FlowPassword}, 201))
	}
	Input, Err := Connection.ParseDatabaseURL(Os.Getenv("TEST_DATABASE_URL"))
	if Err != nil {
		T.Fatal(Err)
	}
	Input.Name = "Isolated workflow target"
	Database := FlowDecode[Connection.Connection](T, Client.Request(T, "admin", "POST", "/api/connections", Input, 201))
	Groups := map[string]Admin.Group{}
	for _, Name := range []string{"viewer", "editor"} {
		Group := FlowDecode[Admin.Group](T, Client.Request(T, "admin", "POST", "/api/admin/groups", map[string]string{"name": Name}, 201))
		Groups[Name] = Group
		Client.Request(T, "admin", "PUT", "/api/admin/groups/"+Group.ID+"/members/"+Users[Name].ID, nil, 204)
		Level := "view_only"
		if Name == "editor" {
			Level = "view_edit"
		}
		Client.Request(T, "admin", "PUT", "/api/admin/groups/"+Group.ID+"/access/"+Database.ID, map[string]string{"level": Level}, 204)
	}
	for _, Name := range []string{"viewer", "editor", "blocked"} {
		Client.Login(T, Name)
	}
	Base := "/api/databases/" + Database.ID
	Schema := Target.Config().ConnConfig.RuntimeParams["search_path"]
	Table := Pgx.Identifier{Schema, "order items"}.Sanitize()
	Payload := MigrationInput{Title: "Create quoted table", SQLText: "CREATE TABLE " + Table + " (id integer PRIMARY KEY, value text); INSERT INTO " + Table + " VALUES (1, 'visible')", DownSQL: "DROP TABLE " + Table + ";"}
	Client.Request(T, "anonymous", "GET", Base+"/migrations", nil, 401)
	for _, Actor := range []string{"admin", "blocked"} {
		Client.Request(T, Actor, "GET", Base+"/schemas", nil, 403)
		Client.Request(T, Actor, "GET", Base+"/migrations", nil, 403)
		Client.Request(T, Actor, "POST", Base+"/migrations", Payload, 403)
		Accessible := FlowDecode[[]Access.DatabaseAccess](T, Client.Request(T, Actor, "GET", "/api/databases", nil, 200))
		if len(Accessible) != 0 {
			T.Fatal("missing group grant exposed a database")
		}
	}
	Client.Request(T, "viewer", "GET", "/api/admin/users", nil, 403)
	Client.Request(T, "viewer", "POST", Base+"/migrations", Payload, 403)
	First := FlowDecode[Migration](T, Client.Request(T, "editor", "POST", Base+"/migrations", Payload, 201))
	Path := Base + "/migrations/" + First.ID
	Client.Request(T, "viewer", "GET", Path, nil, 200)
	Client.Request(T, "viewer", "PUT", Path, Payload, 403)
	Client.Request(T, "viewer", "DELETE", Path, nil, 403)
	Client.Request(T, "viewer", "POST", Path+"/execute", nil, 403)
	Client.Request(T, "blocked", "GET", Path, nil, 403)
	for _, ExecutePath := range []string{Path + "/execute", Path + "/apply", "/api/connections/" + Database.ID + "/migrations/" + First.ID + "/apply"} {
		Client.Request(T, "editor", "POST", ExecutePath, nil, 409)
	}
	ReviewPath := Path + "/reviews/apply"
	SetLeader := func(Name string) {
		Client.Request(T, "admin", "PUT", "/api/admin/groups/"+Groups[Name].ID+"/leader", map[string]string{"user_id": Users[Name].ID}, 204)
	}
	ApprovalPath := func(Item Review, Name string) string {
		return ReviewPath + "/" + Item.ID + "/approvals/" + Groups[Name].ID
	}
	SetLeader("editor")
	Client.Request(T, "viewer", "GET", ReviewPath, nil, 404)
	Client.Request(T, "viewer", "POST", ReviewPath, nil, 403)
	Client.Request(T, "blocked", "GET", ReviewPath, nil, 403)
	Review1 := FlowDecode[Review](T, Client.Request(T, "editor", "POST", ReviewPath, nil, 200))
	if Review1.Revision != First.Revision || Review1.SQLText != Payload.SQLText || Review1.Status != "pending_review" || len(Review1.Groups) != 2 {
		T.Fatalf("expected review for the original revision and both groups: %+v", Review1)
	}
	Client.Request(T, "editor", "PUT", ApprovalPath(Review1, "editor"), nil, 204)
	SetLeader("viewer")
	Client.Request(T, "editor", "PUT", ApprovalPath(Review1, "viewer"), nil, 403)
	if Pending := FlowDecode[Review](T, Client.Request(T, "viewer", "GET", ReviewPath, nil, 200)); Pending.Status != "pending_review" {
		T.Fatal("one leader's approval finalized a two-group review")
	}
	Client.Request(T, "editor", "POST", Path+"/execute", nil, 409)
	Client.Request(T, "viewer", "PUT", ApprovalPath(Review1, "viewer"), nil, 204)
	Approved := FlowDecode[Review](T, Client.Request(T, "editor", "POST", ReviewPath, nil, 200))
	if Approved.Status != "approved" || Approved.ID != Review1.ID {
		T.Fatal("both leaders must approve the same review, even after resubmission")
	}
	Payload.SQLText += "\n-- reviewed revision"
	Client.Request(T, "editor", "PUT", Path, Payload, 200)
	Client.Request(T, "editor", "POST", Path+"/execute", nil, 409)
	Client.Request(T, "viewer", "GET", ReviewPath, nil, 404)
	Client.Request(T, "viewer", "PUT", ApprovalPath(Review1, "viewer"), nil, 404)
	Review2 := FlowDecode[Review](T, Client.Request(T, "editor", "POST", ReviewPath, nil, 200))
	if Review2.ID == Review1.ID || Review2.Revision <= Review1.Revision || Review2.Status != "pending_review" || Review2.SQLText != Payload.SQLText {
		T.Fatal("editing SQL must require a new review")
	}
	Client.Request(T, "editor", "PUT", ApprovalPath(Review2, "editor"), nil, 204)
	Client.Request(T, "viewer", "PUT", ApprovalPath(Review2, "viewer"), nil, 204)
	Token := Client.Tokens["editor"]
	Client.Tokens["editor"] = ""
	Client.Request(T, "editor", "POST", Path+"/execute", nil, 403)
	Client.Tokens["editor"] = Token
	Done := FlowDecode[Execution](T, Client.Request(T, "editor", "POST", Path+"/execute", nil, 200))
	if Done.Status != ExecutionSucceeded || Done.FinishedAt == nil || Done.RequestedBy == nil || *Done.RequestedBy != Users["editor"].ID || Done.RequestedByUsername == nil || *Done.RequestedByUsername != "editor" {
		T.Fatal("execution audit fields are missing")
	}
	Client.Request(T, "editor", "POST", Path+"/execute", nil, 409)
	Client.Request(T, "editor", "GET", ReviewPath, nil, 404)
	Client.Request(T, "editor", "POST", Path+"/rollback", nil, 409)
	RollbackReview := FlowDecode[Review](T, Client.Request(T, "editor", "POST", Path+"/reviews/rollback", nil, 200))
	if RollbackReview.Action != KindRollback || RollbackReview.SQLText != Payload.DownSQL || RollbackReview.Status != "pending_review" {
		T.Fatalf("rollback must have a separate review for down SQL: %+v", RollbackReview)
	}
	RowsPath := Base + "/tables/order%20items/rows?schema=" + Url.QueryEscape(Schema)
	Page := FlowDecode[Inspect.TablePage](T, Client.Request(T, "viewer", "GET", RowsPath, nil, 200))
	if Page.Total != 1 || len(Page.Rows) != 1 || *Page.Rows[0][1] != "visible" {
		T.Fatal("view-only user could not read committed data")
	}
	for _, Name := range []string{"editor", "viewer"} {
		Client.Request(T, Name, "PUT", Path+"/reviews/rollback/"+RollbackReview.ID+"/approvals/"+Groups[Name].ID, nil, 204)
	}
	Client.Request(T, "editor", "POST", Path+"/rollback", nil, 200)
	Client.Request(T, "editor", "POST", Path+"/forward", nil, 409)
	ForwardPath := Path + "/reviews/forward"
	ForwardReview := FlowDecode[Review](T, Client.Request(T, "editor", "POST", ForwardPath, nil, 200))
	for _, Name := range []string{"editor", "viewer"} {
		Client.Request(T, Name, "PUT", ForwardPath+"/"+ForwardReview.ID+"/approvals/"+Groups[Name].ID, nil, 204)
	}
	Client.Request(T, "editor", "POST", Path+"/forward", nil, 200)
	BrokenTable := Pgx.Identifier{Schema, "must rollback"}.Sanitize()
	Broken := FlowDecode[Migration](T, Client.Request(T, "editor", "POST", Base+"/migrations", MigrationInput{Title: "Rollback", SQLText: "CREATE TABLE " + BrokenTable + " (id int); SELECT 1/0", DownSQL: "DROP TABLE " + BrokenTable + ";"}, 201))
	BrokenPath := Base + "/migrations/" + Broken.ID
	BrokenReviewPath := BrokenPath + "/reviews/apply"
	BrokenReview := FlowDecode[Review](T, Client.Request(T, "editor", "POST", BrokenReviewPath, nil, 200))
	for _, Name := range []string{"editor", "viewer"} {
		Client.Request(T, Name, "PUT", BrokenReviewPath+"/"+BrokenReview.ID+"/approvals/"+Groups[Name].ID, nil, 204)
	}
	Client.Request(T, "editor", "POST", BrokenPath+"/execute", nil, 422)
	Client.Request(T, "editor", "POST", BrokenPath+"/retry", nil, 409)
	var Exists bool
	if Err := Target.QueryRow(Context.Background(), "SELECT to_regclass($1) IS NOT NULL", BrokenTable).Scan(&Exists); Err != nil || Exists {
		T.Fatal("failed migration left target changes")
	}
	History := FlowDecode[[]Migration](T, Client.Request(T, "viewer", "GET", Base+"/migrations", nil, 200))
	if len(History) != 2 || History[0].Version != 1 || History[1].Version != 2 || History[1].Status != StatusFailed || History[1].ErrorMessage == nil {
		T.Fatal("linear history did not preserve the failed attempt")
	}
	Executions := FlowDecode[[]Execution](T, Client.Request(T, "viewer", "GET", Base+"/migrations/history", nil, 200))
	if len(Executions) != 4 || Executions[0].Status != ExecutionSucceeded || Executions[3].Status != ExecutionFailed {
		T.Fatal("execution history did not preserve success and failure")
	}
	OldCookie := Client.Cookies["viewer"].Value
	Client.Request(T, "admin", "PUT", "/api/admin/groups/"+Groups["editor"].ID+"/members/"+Users["viewer"].ID, nil, 204)
	Draft := FlowDecode[Migration](T, Client.Request(T, "viewer", "POST", Base+"/migrations", MigrationInput{Title: "Promoted", SQLText: "SELECT 1", DownSQL: "SELECT 1"}, 201))
	if Client.Cookies["viewer"].Value == OldCookie {
		T.Fatal("real membership promotion did not rotate the session")
	}
	Client.Request(T, "admin", "DELETE", "/api/admin/groups/"+Groups["editor"].ID+"/members/"+Users["viewer"].ID, nil, 204)
	Client.Request(T, "viewer", "POST", Base+"/migrations/"+Draft.ID+"/execute", nil, 401)
	Client.Login(T, "viewer")
	Client.Request(T, "viewer", "POST", Base+"/migrations/"+Draft.ID+"/execute", nil, 403)
	Client.Request(T, "viewer", "GET", Base+"/migrations", nil, 200)
	Client.Request(T, "admin", "DELETE", "/api/admin/groups/"+Groups["viewer"].ID+"/access/"+Database.ID, nil, 204)
	Client.Request(T, "viewer", "GET", RowsPath, nil, 401)
	Client.Login(T, "viewer")
	Client.Request(T, "viewer", "GET", RowsPath, nil, 403)
	Client.Request(T, "editor", "POST", "/api/auth/logout", nil, 204)
	Client.Request(T, "editor", "GET", Base+"/migrations", nil, 401)
}
