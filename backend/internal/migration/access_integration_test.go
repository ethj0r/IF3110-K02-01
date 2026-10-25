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
	Service := &Service{Repo: &Repository{Pool: Platform}, Access: Policy, Driver: Driver.PostgresDriver{}, Resolver: DSNResolverFunc(func(Ctx Context.Context, ID string) (string, error) {
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
	Payload := MigrationInput{Title: "Create quoted table", SQLText: "CREATE TABLE " + Table + " (id integer PRIMARY KEY, value text); INSERT INTO " + Table + " VALUES (1, 'visible')"}
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
	Client.Request(T, "editor", "PUT", Path, Payload, 200)
	Token := Client.Tokens["editor"]
	Client.Tokens["editor"] = ""
	Client.Request(T, "editor", "POST", Path+"/execute", nil, 403)
	Client.Tokens["editor"] = Token
	Done := FlowDecode[Migration](T, Client.Request(T, "editor", "POST", Path+"/execute", nil, 200))
	if Done.Status != StatusCommitted || Done.ExecutedAt == nil || Done.ExecutedBy == nil || *Done.ExecutedBy != Users["editor"].ID || Done.ExecutedByUsername == nil || *Done.ExecutedByUsername != "editor" {
		T.Fatal("execution audit fields are missing")
	}
	Client.Request(T, "editor", "POST", Path+"/execute", nil, 409)
	RowsPath := Base + "/tables/order%20items/rows?schema=" + Url.QueryEscape(Schema)
	Page := FlowDecode[Inspect.TablePage](T, Client.Request(T, "viewer", "GET", RowsPath, nil, 200))
	if Page.Total != 1 || len(Page.Rows) != 1 || *Page.Rows[0][1] != "visible" {
		T.Fatal("view-only user could not read committed data")
	}
	BrokenTable := Pgx.Identifier{Schema, "must rollback"}.Sanitize()
	Broken := FlowDecode[Migration](T, Client.Request(T, "editor", "POST", Base+"/migrations", MigrationInput{Title: "Rollback", SQLText: "CREATE TABLE " + BrokenTable + " (id int); SELECT 1/0"}, 201))
	Client.Request(T, "editor", "POST", Base+"/migrations/"+Broken.ID+"/execute", nil, 422)
	var Exists bool
	if Err := Target.QueryRow(Context.Background(), "SELECT to_regclass($1) IS NOT NULL", BrokenTable).Scan(&Exists); Err != nil || Exists {
		T.Fatal("failed migration left target changes")
	}
	History := FlowDecode[[]Migration](T, Client.Request(T, "viewer", "GET", Base+"/migrations", nil, 200))
	if len(History) != 2 || History[0].Version != 1 || History[1].Version != 2 || History[1].Status != StatusRolledBack || History[1].ErrorMessage == nil {
		T.Fatal("linear history did not preserve rollback")
	}
	OldCookie := Client.Cookies["viewer"].Value
	Client.Request(T, "admin", "PUT", "/api/admin/groups/"+Groups["editor"].ID+"/members/"+Users["viewer"].ID, nil, 204)
	Draft := FlowDecode[Migration](T, Client.Request(T, "viewer", "POST", Base+"/migrations", MigrationInput{Title: "Promoted", SQLText: "SELECT 1"}, 201))
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
