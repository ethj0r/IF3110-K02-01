//go:build integration

package Connection

import (
	Bytes "bytes"
	Context "context"
	Json "encoding/json"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
	Chi "github.com/go-chi/chi/v5"
	Http "net/http"
	Httptest "net/http/httptest"
	Os "os"
	Strings "strings"
	Testing "testing"
	Time "time"
)

type HandlerFixture struct {
	Service *Service
	Auth    *Auth.Handler
	Router  Http.Handler
	Admin   Auth.Authenticated
	Member  Auth.Authenticated
}

func NewHandlerFixture(T *Testing.T) *HandlerFixture {
	T.Helper()
	Pool := NewConnectionDatabase(T)
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
	Settings.EncryptionKey = Bytes.Repeat([]byte{13}, 32)
	Settings.IdleTimeout = 5 * Time.Minute
	Settings.AbsoluteTimeout = Time.Hour
	Settings.RotationInterval = 2 * Time.Minute
	Settings.GracePeriod = 10 * Time.Second
	AuthService, Err := Auth.NewService(AuthRepo, Settings)
	if Err != nil {
		T.Fatal(Err)
	}
	AuthRoutes := &Auth.Handler{Service: AuthService}
	AEAD, Err := NewCipher(Settings.EncryptionKey)
	if Err != nil {
		T.Fatal(Err)
	}
	Server := &Service{Cipher: AEAD, Repo: &Repository{Pool: Pool}}
	Routes := &Handler{Auth: AuthRoutes, Service: Server, Driver: Driver.PostgresDriver{}}
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
	return &HandlerFixture{Service: Server, Auth: AuthRoutes, Router: Router, Admin: Admin, Member: Member}
}

func (App *HandlerFixture) Request(Method, Path, Body string, State Auth.Authenticated, WithCSRF bool) *Httptest.ResponseRecorder {
	Request := Httptest.NewRequest(Method, Path, Bytes.NewBufferString(Body))
	Request.Header.Set("Origin", App.Auth.Service.Settings.Origin)
	if Body != "" {
		Request.Header.Set("Content-Type", "application/json")
	}
	if State.Identifier != "" {
		Request.AddCookie(&Http.Cookie{Name: Auth.CookieName, Value: State.Identifier})
	}
	if WithCSRF {
		Request.Header.Set("X-CSRF-Token", State.CSRFToken)
	}
	Response := Httptest.NewRecorder()
	App.Router.ServeHTTP(Response, Request)
	return Response
}

func (App *HandlerFixture) ConnectionBody(Input ConnectionInput) string {
	Body, _ := Json.Marshal(Input)
	return string(Body)
}

func TestConnectionEndpointsRequireAdmin(T *Testing.T) {
	App := NewHandlerFixture(T)
	Anonymous := Auth.Authenticated{}
	if App.Request("GET", "/api/connections", "", Anonymous, false).Code != 401 {
		T.Fatal("anonymous connection list was accepted")
	}
	Input := App.ConnectionBody(ConnectionInput{Name: "main", Host: "127.0.0.1", Port: 5432, DBName: "target", Username: "target", Password: "target_dev"})
	if App.Request("POST", "/api/connections", Input, App.Member, true).Code != 403 {
		T.Fatal("non-admin connection create was accepted")
	}
	if App.Request("GET", "/api/connections", "", App.Member, false).Code != 403 {
		T.Fatal("non-admin connection list was accepted")
	}
	Created := App.Request("POST", "/api/connections", Input, App.Admin, true)
	if Created.Code != 201 {
		T.Fatalf("admin connection create failed: %d %s", Created.Code, Created.Body.String())
	}
	if Strings.Contains(Created.Body.String(), "target_dev") || Strings.Contains(Created.Body.String(), `"password"`) || Strings.Contains(Created.Body.String(), "ciphertext") {
		T.Fatal("connection response leaked credentials")
	}
	var Item Connection
	if Err := Json.Unmarshal(Created.Body.Bytes(), &Item); Err != nil || Item.ID == "" {
		T.Fatal("connection create returned unexpected body")
	}
	if App.Request("POST", "/api/connections", Input, App.Admin, true).Code != 409 {
		T.Fatal("duplicate connection name was accepted")
	}
	if App.Request("POST", "/api/connections", `{"name":"bad"}`, App.Admin, true).Code != 400 {
		T.Fatal("invalid connection input was accepted")
	}
	if App.Request("GET", "/api/connections", "", App.Admin, false).Code != 200 {
		T.Fatal("admin connection list failed")
	}
	if App.Request("GET", "/api/connections/"+Item.ID, "", App.Admin, false).Code != 200 {
		T.Fatal("admin connection detail failed")
	}
	if App.Request("GET", "/api/connections/00000000-0000-0000-0000-000000000000", "", App.Admin, false).Code != 404 {
		T.Fatal("unknown connection did not return 404")
	}
	for _, Method := range []string{"GET", "PUT", "DELETE", "POST"} {
		Path := "/api/connections/not-a-uuid"
		if Method == "POST" {
			Path += "/test"
		}
		if Response := App.Request(Method, Path, Input, App.Admin, true); Response.Code != 404 {
			T.Fatalf("malformed ID returned %d for %s", Response.Code, Method)
		}
	}
	Changed := App.ConnectionBody(ConnectionInput{Name: "main", Host: "10.0.0.2", Port: 5432, DBName: "target", Username: "target", Password: "target_dev"})
	if App.Request("PUT", "/api/connections/"+Item.ID, Changed, App.Admin, true).Code != 200 {
		T.Fatal("admin connection update failed")
	}
	if App.Request("DELETE", "/api/connections/"+Item.ID, "", App.Admin, true).Code != 204 {
		T.Fatal("admin connection delete failed")
	}
	if App.Request("GET", "/api/connections/"+Item.ID, "", App.Admin, false).Code != 404 {
		T.Fatal("deleted connection still readable")
	}
}

func TestConnectionTestEndpoint(T *Testing.T) {
	App := NewHandlerFixture(T)
	Ctx := Context.Background()
	Dead, Err := App.Service.Create(Ctx, ConnectionInput{Name: "dead", Host: "127.0.0.1", Port: 1, DBName: "target", Username: "target", Password: "dead-secret"}, nil)
	if Err != nil {
		T.Fatal(Err)
	}
	Unreachable := App.Request("POST", "/api/connections/"+Dead.ID+"/test", "", App.Admin, true)
	if Unreachable.Code != 502 {
		T.Fatalf("unreachable target returned %d, want 502", Unreachable.Code)
	}
	if Strings.Contains(Unreachable.Body.String(), "dead-secret") || Strings.Contains(Unreachable.Body.String(), "password") {
		T.Fatal("failed connection test leaked credentials")
	}
	Live, Err := ParseDatabaseURL(Os.Getenv("TEST_DATABASE_URL"))
	if Err != nil {
		T.Fatal(Err)
	}
	Stored, Err := App.Service.Create(Ctx, Live, nil)
	if Err != nil {
		T.Fatal(Err)
	}
	if App.Request("POST", "/api/connections/"+Stored.ID+"/test", "", App.Admin, true).Code != 200 {
		T.Fatal("reachable target test failed")
	}
	if App.Request("POST", "/api/connections/00000000-0000-0000-0000-000000000000/test", "", App.Admin, true).Code != 404 {
		T.Fatal("unknown connection test did not return 404")
	}
	if App.Request("POST", "/api/connections/"+Stored.ID+"/test", "", App.Member, true).Code != 403 {
		T.Fatal("non-admin connection test was accepted")
	}
}

func TestSeedDefaultIsIdempotent(T *Testing.T) {
	Server := NewHandlerFixture(T).Service
	Ctx := Context.Background()
	if Err := SeedDefault(Ctx, Server, ""); Err != nil {
		T.Fatal("empty seed URL must be a no-op")
	}
	URL := Os.Getenv("TEST_DATABASE_URL")
	if URL == "" {
		T.Fatal("TEST_DATABASE_URL is required")
	}
	if Err := SeedDefault(Ctx, Server, "not-a-url"); Err == nil {
		T.Fatal("invalid seed URL was accepted")
	}
	if Err := SeedDefault(Ctx, Server, URL); Err != nil {
		T.Fatal(Err)
	}
	if Err := SeedDefault(Ctx, Server, URL); Err != nil {
		T.Fatal("repeated seed must be a no-op")
	}
	Listed, Err := Server.List(Ctx)
	if Err != nil || len(Listed) != 1 || Listed[0].Name != "local-target" {
		T.Fatalf("seed created unexpected connections: %+v", Listed)
	}
}
