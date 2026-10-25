//go:build integration

package Schemadiff

import (
	Bytes "bytes"
	Context "context"
	Json "encoding/json"
	Errors "errors"
	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Connection "github.com/ethj0r/IF3110-K02-01/backend/internal/connection"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
	Migration "github.com/ethj0r/IF3110-K02-01/backend/internal/migration"
	Testdb "github.com/ethj0r/IF3110-K02-01/backend/internal/testdb"
	Chi "github.com/go-chi/chi/v5"
	Pgx "github.com/jackc/pgx/v5"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
	Http "net/http"
	Httptest "net/http/httptest"
	Url "net/url"
	Os "os"
	Strings "strings"
	Testing "testing"
	Time "time"
)

func ComparisonDatabase(T *Testing.T, Admin *Pgxpool.Pool) (string, *Pgxpool.Pool) {
	T.Helper()
	ID, Err := Auth.RandomID()
	if Err != nil {
		T.Fatal(Err)
	}
	Name := "schema_diff_" + Strings.ReplaceAll(ID, "-", "")
	Ctx := Context.Background()
	if _, Err := Admin.Exec(Ctx, "CREATE DATABASE "+Pgx.Identifier{Name}.Sanitize()); Err != nil {
		T.Fatal(Err)
	}
	T.Cleanup(func() {
		Cleanup, Cancel := Context.WithTimeout(Context.Background(), 10*Time.Second)
		defer Cancel()
		if _, Err := Admin.Exec(Cleanup, "DROP DATABASE "+Pgx.Identifier{Name}.Sanitize()+" WITH (FORCE)"); Err != nil {
			T.Error(Err)
		}
	})
	Parsed, Err := Url.Parse(Os.Getenv("TEST_DATABASE_URL"))
	if Err != nil {
		T.Fatal("invalid test URL")
	}
	Parsed.Path, Parsed.RawPath = "/"+Name, ""
	DSN := Parsed.String()
	Pool, Err := Pgxpool.New(Ctx, DSN)
	if Err != nil {
		T.Fatal("cannot open comparison fixture")
	}
	T.Cleanup(Pool.Close)
	return DSN, Pool
}

func TestGeneratedMigrationConvergesRealSchemasWithoutAutomaticWrites(T *Testing.T) {
	Platform := Testdb.PlatformDatabase(T)
	CurrentDSN, Current := ComparisonDatabase(T, Platform)
	ReferenceDSN, Reference := ComparisonDatabase(T, Platform)
	Ctx := Context.Background()
	Schema := `schema " 表`
	Namespace := Pgx.Identifier{Schema}.Sanitize()
	Table := Pgx.Identifier{Schema, `order " items`}.Sanitize()
	Base := "CREATE SCHEMA " + Namespace + "; CREATE TABLE " + Table + " (id integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY, value text NOT NULL);"
	for _, Pool := range []*Pgxpool.Pool{Current, Reference} {
		if _, Err := Pool.Exec(Ctx, Base); Err != nil {
			T.Fatal(Err)
		}
	}
	if _, Err := Current.Exec(Ctx, "INSERT INTO "+Table+" (value) VALUES ('preserved'); CREATE TABLE "+Namespace+".obsolete (id integer)"); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Reference.Exec(Ctx, "ALTER TABLE "+Table+" ADD COLUMN enabled boolean NOT NULL DEFAULT true; ALTER TABLE "+Table+" ALTER COLUMN value DROP NOT NULL; CREATE INDEX value_index ON "+Table+" (value); CREATE TABLE "+Namespace+".child (id integer PRIMARY KEY, parent integer REFERENCES "+Table+"(id)); CREATE FUNCTION "+Namespace+".message() RETURNS text LANGUAGE sql AS $$ SELECT 'semi;colon'::text; $$"); Err != nil {
		T.Fatal(Err)
	}
	Generator := PostgresGenerator{}
	Before, Err := Generator.Generate(Ctx, CurrentDSN, ReferenceDSN, Schema)
	if Err != nil {
		T.Fatalf("comparison failed: %v", Err)
	}
	if Before.SQLText == "" || len(Before.Statements) < 4 || Strings.Contains(Before.SQLText, "CONCURRENTLY") || Before.CurrentSchemaHash == "" {
		T.Fatal("comparison omitted changes or generated nontransactional indexes")
	}
	var Exists bool
	if Err := Current.QueryRow(Ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema=$1 AND column_name='enabled')", Schema).Scan(&Exists); Err != nil || Exists {
		T.Fatal("generation changed the current schema")
	}
	if Err := Current.QueryRow(Ctx, "SELECT to_regclass($1) IS NOT NULL", Namespace+".obsolete").Scan(&Exists); Err != nil || !Exists {
		T.Fatal("generation executed destructive SQL")
	}
	FoundRisk := false
	for _, Item := range Before.Statements {
		for _, Hazard := range Item.Hazards {
			FoundRisk = FoundRisk || Hazard.Type == "DELETES_DATA"
		}
	}
	if !FoundRisk {
		T.Fatal("destructive difference has no hazard")
	}
	Cipher, Err := Connection.NewCipher(Bytes.Repeat([]byte{49}, 32))
	if Err != nil {
		T.Fatal(Err)
	}
	Connections := &Connection.Service{Repo: &Connection.Repository{Pool: Platform}, Cipher: Cipher}
	Input, Err := Connection.ParseDatabaseURL(CurrentDSN)
	if Err != nil {
		T.Fatal("invalid current URL")
	}
	Input.Name = "Current schema"
	Database, Err := Connections.Create(Ctx, Input, nil)
	if Err != nil {
		T.Fatal(Err)
	}
	var UserID string
	if Err := Platform.QueryRow(Ctx, "INSERT INTO users (username,password_hash) VALUES ('editor','unused') RETURNING id").Scan(&UserID); Err != nil {
		T.Fatal(Err)
	}
	Policy := Access.StaticEvaluator{Grants: map[string]map[string]Access.Level{UserID: {Database.ID: Access.LevelViewEdit}}}
	Migrations := &Migration.Service{Repo: &Migration.Repository{Pool: Platform}, Access: Policy, Driver: Driver.PostgresDriver{}, Resolver: Migration.DSNResolverFunc(func(Context.Context, string) (string, error) { return CurrentDSN, nil })}
	User := Auth.Principal{ID: UserID}
	Draft, Err := Migrations.Create(Ctx, User, Database.ID, Migration.MigrationInput{Title: "Generated and reviewed", SQLText: Before.SQLText + "\nINSERT INTO " + Table + " (value) VALUES ('reviewed');"})
	if Err != nil {
		T.Fatal(Err)
	}
	Done, Err := Migrations.Execute(Ctx, User, Database.ID, Draft.ID)
	if Err != nil || Done.Status != Migration.StatusCommitted {
		T.Fatalf("generated SQL failed the transaction executor: %v", Err)
	}
	After, Err := Generator.Generate(Ctx, CurrentDSN, ReferenceDSN, Schema)
	if Err != nil || len(After.Statements) != 0 || After.SQLText != "" {
		T.Fatalf("schemas did not converge after migration: %v; SQL: %s", Err, After.SQLText)
	}
	var Count int
	if Err := Current.QueryRow(Ctx, "SELECT count(*) FROM "+Table+" WHERE value IN ('preserved','reviewed')").Scan(&Count); Err != nil || Count != 2 {
		T.Fatal("generated migration lost existing data or user edits")
	}
	var Message string
	if Err := Current.QueryRow(Ctx, "SELECT "+Namespace+".message()").Scan(&Message); Err != nil || Message != "semi;colon" {
		T.Fatal("function statement quoting was not preserved")
	}
	if _, Err := Generator.Generate(Ctx, CurrentDSN, ReferenceDSN, "missing"); !Errors.Is(Err, ErrInvalidInput) {
		T.Fatal("missing reference schema accepted")
	}
	if _, Err := Generator.Generate(Ctx, "postgres://127.0.0.1:1/unreachable?sslmode=disable", ReferenceDSN, Schema); !Errors.Is(Err, ErrUnavailable) {
		T.Fatal("unreachable current database was not sanitized")
	}
	if _, Err := Generator.Generate(Ctx, CurrentDSN, "invalid DSN", Schema); !Errors.Is(Err, ErrUnavailable) {
		T.Fatal("invalid reference connection was not sanitized")
	}
	for Index, Pool := range []*Pgxpool.Pool{Current, Reference} {
		Key := "id"
		if Index == 1 {
			Key = "value"
		}
		if _, Err := Pool.Exec(Ctx, "CREATE TABLE "+Namespace+".partitioned (id integer, value integer) PARTITION BY RANGE ("+Key+")"); Err != nil {
			T.Fatal(Err)
		}
	}
	if _, Err := Generator.Generate(Ctx, CurrentDSN, ReferenceDSN, Schema); !Errors.Is(Err, ErrUnsupported) {
		T.Fatalf("unsupported partition change was not rejected: %v", Err)
	}
	EmptySchema := `new schema 表`
	if _, Err := Reference.Exec(Ctx, "CREATE SCHEMA "+Pgx.Identifier{EmptySchema}.Sanitize()+"; CREATE TABLE "+Pgx.Identifier{EmptySchema, "new table"}.Sanitize()+" (id integer)"); Err != nil {
		T.Fatal(Err)
	}
	NewSchema, Err := Generator.Generate(Ctx, CurrentDSN, ReferenceDSN, EmptySchema)
	if Err != nil || !Strings.Contains(NewSchema.SQLText, "CREATE SCHEMA") || !Strings.Contains(NewSchema.SQLText, "CREATE TABLE") {
		T.Fatalf("missing current schema did not generate creation: %v", Err)
	}
}

func TestComparisonHTTPUsesRealSessionsAndBothGroupGrants(T *Testing.T) {
	Platform := Testdb.PlatformDatabase(T)
	Ctx := Context.Background()
	Settings := Auth.DefaultOptions()
	Settings.Origin, Settings.CookieSecure = "https://app.test", true
	Settings.EncryptionKey = Bytes.Repeat([]byte{21}, 32)
	Authentication, Err := Auth.NewService(&Auth.Repository{Pool: Platform}, Settings)
	if Err != nil {
		T.Fatal(Err)
	}
	AuthRoutes := &Auth.Handler{Service: Authentication}
	Cipher, Err := Connection.NewCipher(Settings.EncryptionKey)
	if Err != nil {
		T.Fatal(Err)
	}
	Connections := &Connection.Service{Repo: &Connection.Repository{Pool: Platform}, Cipher: Cipher}
	Databases := []Connection.Connection{}
	for _, Name := range []string{"current", "reference"} {
		Input, Err := Connection.ParseDatabaseURL(Os.Getenv("TEST_DATABASE_URL"))
		if Err != nil {
			T.Fatal("invalid test URL")
		}
		Input.Name = Name
		DB, Err := Connections.Create(Ctx, Input, nil)
		if Err != nil {
			T.Fatal(Err)
		}
		Databases = append(Databases, DB)
	}
	Hash, Err := Auth.HashPassword("integration comparison password")
	if Err != nil {
		T.Fatal(Err)
	}
	for _, Name := range []string{"editor", "viewer", "isolated", "admin"} {
		var UserID, GroupID string
		if Err := Platform.QueryRow(Ctx, "INSERT INTO users(username,password_hash,is_platform_admin) VALUES ($1,$2,$3) RETURNING id", Name, Hash, Name == "admin").Scan(&UserID); Err != nil {
			T.Fatal(Err)
		}
		if Err := Platform.QueryRow(Ctx, "INSERT INTO groups(name) VALUES ($1) RETURNING id", Name).Scan(&GroupID); Err != nil {
			T.Fatal(Err)
		}
		if _, Err := Platform.Exec(Ctx, "INSERT INTO user_groups(user_id,group_id) VALUES ($1,$2)", UserID, GroupID); Err != nil {
			T.Fatal(Err)
		}
		if Name == "admin" {
			continue
		}
		Level := "view_edit"
		if Name == "viewer" {
			Level = "view_only"
		}
		if _, Err := Platform.Exec(Ctx, "INSERT INTO group_database_access(group_id,database_id,access_level) VALUES ($1,$2,$3)", GroupID, Databases[0].ID, Level); Err != nil {
			T.Fatal(Err)
		}
		if Name != "isolated" {
			if _, Err := Platform.Exec(Ctx, "INSERT INTO group_database_access(group_id,database_id,access_level) VALUES ($1,$2,'view_only')", GroupID, Databases[1].ID); Err != nil {
				T.Fatal(Err)
			}
		}
	}
	Probe := &ProbeGenerator{}
	Routes := &Handler{Auth: AuthRoutes, Service: &Service{Policy: &Access.Repository{Pool: Platform}, Connections: Connections, Generator: Probe}}
	Router := Chi.NewRouter()
	Router.Route("/api", func(Api Chi.Router) { AuthRoutes.Register(Api); Routes.Register(Api) })
	Endpoint := "/api/databases/" + Databases[0].ID + "/schema-diff"
	Payload := `{"reference_database_id":"` + Databases[1].ID + `","schema":"public"}`
	Send := func(Path, Body, ContentType string, Cookie *Http.Cookie, Token string) *Httptest.ResponseRecorder {
		Request := Httptest.NewRequest("POST", Path, Strings.NewReader(Body))
		Request.Header.Set("Origin", Settings.Origin)
		Request.Header.Set("Content-Type", ContentType)
		Request.Header.Set("X-CSRF-Token", Token)
		if Cookie != nil {
			Request.AddCookie(Cookie)
		}
		Response := Httptest.NewRecorder()
		Router.ServeHTTP(Response, Request)
		return Response
	}
	if Send(Endpoint, Payload, "application/json", nil, "").Code != 401 {
		T.Fatal("anonymous comparison accepted")
	}
	for _, Actor := range []string{"viewer", "isolated", "admin", "editor"} {
		Login := Send("/api/auth/login", `{"username":"`+Actor+`","password":"integration comparison password"}`, "application/json", nil, "")
		if Login.Code != 200 {
			T.Fatal("fixture login failed")
		}
		var Session struct {
			CSRFToken string `json:"csrf_token"`
		}
		if Err := Json.Unmarshal(Login.Body.Bytes(), &Session); Err != nil {
			T.Fatal(Err)
		}
		Cookie := Login.Result().Cookies()[0]
		Before := Probe.Calls
		Response := Send(Endpoint, Payload, "application/json", Cookie, Session.CSRFToken)
		Expected := 403
		if Actor == "editor" {
			Expected = 200
		}
		if Response.Code != Expected {
			T.Fatalf("%s comparison status=%d", Actor, Response.Code)
		}
		if Actor != "editor" {
			if Probe.Calls != Before {
				T.Fatal("denied request reached generator")
			}
			continue
		}
		if Response.Header().Get("Cache-Control") != "no-store" || Strings.Contains(Response.Body.String(), "private-dsn") || Strings.Contains(Response.Body.String(), "password") {
			T.Fatal("comparison response leaked or can be cached")
		}
		for _, Case := range []struct {
			Body, ContentType, Token string
			Status                   int
		}{
			{Payload, "application/json", "", 403},
			{Payload, "text/plain", Session.CSRFToken, 415},
			{Payload + ` {}`, "application/json", Session.CSRFToken, 400},
			{`{"reference_database_id":"x","extra":true}`, "application/json", Session.CSRFToken, 400},
			{`{"reference_database_id":"` + Strings.Repeat("x", 2100) + `"}`, "application/json", Session.CSRFToken, 400},
			{`{"reference_database_id":"x","schema":"pg_catalog"}`, "application/json", Session.CSRFToken, 400},
		} {
			if Got := Send(Endpoint, Case.Body, Case.ContentType, Cookie, Case.Token); Got.Code != Case.Status {
				T.Fatalf("unsafe comparison status %d, want %d", Got.Code, Case.Status)
			}
		}
		if Probe.Calls != Before+1 {
			T.Fatal("invalid request reached generator")
		}
	}
}
