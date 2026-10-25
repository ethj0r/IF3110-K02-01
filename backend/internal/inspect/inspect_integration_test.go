//go:build integration

package Inspect

import (
	Bytes "bytes"
	Context "context"
	Rand "crypto/rand"
	Hex "encoding/hex"
	Json "encoding/json"
	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Connection "github.com/ethj0r/IF3110-K02-01/backend/internal/connection"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
	Testutil "github.com/ethj0r/IF3110-K02-01/backend/internal/testutil"
	Migrations "github.com/ethj0r/IF3110-K02-01/backend/migrations"
	Chi "github.com/go-chi/chi/v5"
	Pgx "github.com/jackc/pgx/v5"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
	Stdlib "github.com/jackc/pgx/v5/stdlib"
	Goose "github.com/pressly/goose/v3"
	Http "net/http"
	Httptest "net/http/httptest"
	Os "os"
	Strings "strings"
	Testing "testing"
	Time "time"
)

type Fixture struct {
	Service *Service
	Auth    *Auth.Handler
	Router  Http.Handler
	Admin   Auth.Authenticated
	Member  Auth.Authenticated
	Target  string
	Schema  string
}

func NewInspectDatabase(T *Testing.T) *Pgxpool.Pool {
	T.Helper()
	Pool := Testutil.Database(T)
	Db := Stdlib.OpenDB(*Pool.Config().ConnConfig)
	defer Db.Close()
	Provider, Err := Goose.NewProvider(Goose.DialectPostgres, Db, Migrations.FS())
	if Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Provider.Up(Context.Background()); Err != nil {
		T.Fatal(Err)
	}
	return Pool
}

func NewFixture(T *Testing.T) *Fixture {
	T.Helper()
	Pool := NewInspectDatabase(T)
	AuthRepo := &Auth.Repository{Pool: Pool}
	if _, Err := Auth.BootstrapAdmin(Context.Background(), AuthRepo, "admin", "correct horse battery staple"); Err != nil {
		T.Fatal(Err)
	}
	Hash, Err := Auth.HashPassword("member secret password")
	if Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Pool.Exec(Context.Background(), "INSERT INTO users (username, password_hash) VALUES ('member', $1)", Hash); Err != nil {
		T.Fatal(Err)
	}
	Settings := Auth.DefaultOptions()
	Settings.Origin = "https://app.test"
	Settings.CookieSecure = true
	Settings.EncryptionKey = Bytes.Repeat([]byte{17}, 32)
	Settings.IdleTimeout = 5 * Time.Minute
	Settings.AbsoluteTimeout = Time.Hour
	Settings.RotationInterval = 2 * Time.Minute
	Settings.GracePeriod = 10 * Time.Second
	AuthService, Err := Auth.NewService(AuthRepo, Settings)
	if Err != nil {
		T.Fatal(Err)
	}
	AuthRoutes := &Auth.Handler{Service: AuthService}
	AEAD, Err := Connection.NewCipher(Settings.EncryptionKey)
	if Err != nil {
		T.Fatal(Err)
	}
	ConnService := &Connection.Service{Cipher: AEAD, Repo: &Connection.Repository{Pool: Pool}}
	Server := &Service{Connections: ConnService, Driver: Driver.PostgresDriver{}}
	Routes := &Handler{Auth: AuthRoutes, Policy: Access.AllowAll(), Service: Server}
	Router := Chi.NewRouter()
	Router.Route("/api", Routes.Register)
	Admin, Err := AuthService.Login(Context.Background(), "admin", "correct horse battery staple", "")
	if Err != nil {
		T.Fatal(Err)
	}
	Member, Err := AuthService.Login(Context.Background(), "member", "member secret password", "")
	if Err != nil {
		T.Fatal(Err)
	}
	return &Fixture{Service: Server, Auth: AuthRoutes, Router: Router, Admin: Admin, Member: Member}
}

func (App *Fixture) Request(Method, Path string, State Auth.Authenticated) *Httptest.ResponseRecorder {
	Request := Httptest.NewRequest(Method, Path, nil)
	Request.Header.Set("Origin", App.Auth.Service.Settings.Origin)
	if State.Identifier != "" {
		Request.AddCookie(&Http.Cookie{Name: Auth.CookieName, Value: State.Identifier})
	}
	Response := Httptest.NewRecorder()
	App.Router.ServeHTTP(Response, Request)
	return Response
}

func (App *Fixture) SeedTarget(T *Testing.T) {
	T.Helper()
	URL := Os.Getenv("TEST_DATABASE_URL")
	if URL == "" {
		T.Fatal("TEST_DATABASE_URL is required")
	}
	Target, Err := Driver.PostgresDriver{}.Open(URL)
	if Err != nil {
		T.Fatal(Err)
	}
	T.Cleanup(Target.Close)
	var Nonce [8]byte
	if _, Err := Rand.Read(Nonce[:]); Err != nil {
		T.Fatal(Err)
	}
	App.Target = URL
	App.Schema = "probe_" + Hex.EncodeToString(Nonce[:])
	Quoted := Pgx.Identifier{App.Schema, "items"}.Sanitize()
	Ctx := Context.Background()
	if Err := Target.Exec(Ctx, "CREATE SCHEMA "+Pgx.Identifier{App.Schema}.Sanitize()); Err != nil {
		T.Fatal(Err)
	}
	T.Cleanup(func() {
		Cleanup, Cancel := Context.WithTimeout(Context.Background(), 5*Time.Second)
		defer Cancel()
		if Err := Target.Exec(Cleanup, "DROP SCHEMA "+Pgx.Identifier{App.Schema}.Sanitize()+" CASCADE"); Err != nil {
			T.Error("cannot remove target test schema")
		}
	})
	if Err := Target.Exec(Ctx, "CREATE TABLE "+Quoted+" (id SERIAL PRIMARY KEY, name TEXT NOT NULL)"); Err != nil {
		T.Fatal(Err)
	}
	Input, Err := Connection.ParseDatabaseURL(URL)
	if Err != nil {
		T.Fatal(Err)
	}
	if _, Err := App.Service.Connections.(*Connection.Service).Create(Ctx, Input, nil); Err != nil {
		T.Fatal(Err)
	}
}

func (App *Fixture) ConnectionID(T *Testing.T) string {
	T.Helper()
	Items, Err := App.Service.Connections.(*Connection.Service).List(Context.Background())
	if Err != nil || len(Items) != 1 {
		T.Fatalf("expected one database, got %+v %v", Items, Err)
	}
	return Items[0].ID
}

func (App *Fixture) RouterWithPolicy(Policy Access.Evaluator) Http.Handler {
	Router := Chi.NewRouter()
	Routes := &Handler{Auth: App.Auth, Policy: Policy, Service: App.Service}
	Router.Route("/api", Routes.Register)
	return Router
}

func (App *Fixture) PolicyRequest(Router Http.Handler, Method, Path string, State Auth.Authenticated) *Httptest.ResponseRecorder {
	Request := Httptest.NewRequest(Method, Path, nil)
	Request.Header.Set("Origin", App.Auth.Service.Settings.Origin)
	if State.Identifier != "" {
		Request.AddCookie(&Http.Cookie{Name: Auth.CookieName, Value: State.Identifier})
	}
	Response := Httptest.NewRecorder()
	Router.ServeHTTP(Response, Request)
	return Response
}

func TestBrowseSchemasAndTables(T *Testing.T) {
	App := NewFixture(T)
	App.SeedTarget(T)
	ID := App.ConnectionID(T)
	Schemas := App.Request("GET", "/api/databases/"+ID+"/schemas", App.Member)
	if Schemas.Code != 200 || !Strings.Contains(Schemas.Body.String(), App.Schema) {
		T.Fatalf("schema list missed probe schema: %d %s", Schemas.Code, Schemas.Body.String())
	}
	var Parsed struct {
		Tables []Table `json:"tables"`
	}
	Tables := App.Request("GET", "/api/databases/"+ID+"/tables?schema="+App.Schema, App.Member)
	if Tables.Code != 200 {
		T.Fatalf("table list failed: %d", Tables.Code)
	}
	if Err := Json.Unmarshal(Tables.Body.Bytes(), &Parsed); Err != nil || len(Parsed.Tables) != 1 || Parsed.Tables[0].Name != "items" {
		T.Fatalf("unexpected table list: %s %v", Tables.Body.String(), Err)
	}
	Empty := App.Request("GET", "/api/databases/"+ID+"/tables?schema=missing_"+App.Schema, App.Member)
	if Empty.Code != 200 || !Strings.Contains(Empty.Body.String(), `"tables":[]`) {
		T.Fatalf("missing schema must return an empty list: %d %s", Empty.Code, Empty.Body.String())
	}
	Default := App.Request("GET", "/api/databases/"+ID+"/tables", App.Member)
	if Default.Code != 200 {
		T.Fatal("table list without schema must default to public")
	}
}

func TestBrowseRejectsBadInput(T *Testing.T) {
	App := NewFixture(T)
	App.SeedTarget(T)
	ID := App.ConnectionID(T)
	if App.Request("GET", "/api/databases/00000000-0000-0000-0000-000000000000/schemas", App.Admin).Code != 404 {
		T.Fatal("unknown database did not return 404")
	}
	if App.Request("GET", "/api/databases/"+ID+"/tables?schema=bad%00name", App.Admin).Code != 400 {
		T.Fatal("NUL in schema name was accepted")
	}
	Ctx := Context.Background()
	Dead, Err := App.Service.Connections.(*Connection.Service).Create(Ctx, Connection.ConnectionInput{Name: "dead", Host: "127.0.0.1", Port: 1, DBName: "target", Username: "target", Password: "x"}, nil)
	if Err != nil {
		T.Fatal(Err)
	}
	Response := App.Request("GET", "/api/databases/"+Dead.ID+"/schemas", App.Admin)
	if Response.Code != 502 || Strings.Contains(Response.Body.String(), "postgres") {
		T.Fatalf("unreachable target must fail sanitized: %d %s", Response.Code, Response.Body.String())
	}
}

func TestBrowseRequiresDatabaseAccess(T *Testing.T) {
	App := NewFixture(T)
	App.SeedTarget(T)
	ID := App.ConnectionID(T)
	Denied := App.RouterWithPolicy(Access.StaticEvaluator{})
	if App.PolicyRequest(Denied, "GET", "/api/databases/"+ID+"/schemas", App.Member).Code != 403 {
		T.Fatal("database browse without group access was accepted")
	}
	if App.PolicyRequest(Denied, "GET", "/api/databases/"+ID+"/schemas", Auth.Authenticated{}).Code != 401 {
		T.Fatal("anonymous database browse did not return 401")
	}
	Granted := App.RouterWithPolicy(Access.AllowAll())
	if Granted == nil || App.PolicyRequest(Granted, "GET", "/api/databases/"+ID+"/schemas", App.Member).Code != 200 {
		T.Fatal("database browse with access failed")
	}
}
