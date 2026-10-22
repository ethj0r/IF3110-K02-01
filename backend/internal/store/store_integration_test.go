//go:build integration

package Store_test

import (
	Context "context"
	Sql "database/sql"
	Httpx "github.com/ethj0r/IF3110-K02-01/backend/internal/httpx"
	Store "github.com/ethj0r/IF3110-K02-01/backend/internal/store"
	Testutil "github.com/ethj0r/IF3110-K02-01/backend/internal/testutil"
	Migrations "github.com/ethj0r/IF3110-K02-01/backend/migrations"
	Stdlib "github.com/jackc/pgx/v5/stdlib"
	Goose "github.com/pressly/goose/v3"
	Http "net/http"
	Httptest "net/http/httptest"
	Os "os"
	Strings "strings"
	Testing "testing"
	Time "time"
)

func TestOpenAndReadiness(T *Testing.T) {
	Ctx, Cancel := Context.WithTimeout(Context.Background(), 10*Time.Second)
	defer Cancel()
	Dsn := Os.Getenv("TEST_DATABASE_URL")
	if Dsn == "" {
		T.Fatal("TEST_DATABASE_URL is required")
	}
	Pool, Err := Store.Open(Ctx, Dsn)
	if Err != nil {
		T.Fatal(Err)
	}
	defer Pool.Close()
	Response := Httptest.NewRecorder()
	Httpx.NewRouter(Pool.Ping, Http.NotFoundHandler()).ServeHTTP(Response, Httptest.NewRequest("GET", "/api/ready", nil))
	if Response.Code != 200 {
		T.Fatalf("readiness with real PostgreSQL: %d", Response.Code)
	}
}

func TestOpenFailureDoesNotExposeCredentials(T *Testing.T) {
	Ctx, Cancel := Context.WithTimeout(Context.Background(), Time.Second)
	defer Cancel()
	Pool, Err := Store.Open(Ctx, "postgres://test:secret@127.0.0.1:1/unavailable?sslmode=disable")
	if Pool != nil {
		Pool.Close()
		T.Fatal("failed connection must not return a pool")
	}
	if Err == nil || Strings.Contains(Err.Error(), "secret") {
		T.Fatal("failed connection must return a sanitized error")
	}
}

func TestIsolatedSchemasAndGoose(T *Testing.T) {
	First := Testutil.Database(T)
	Second := Testutil.Database(T)
	Ctx, Cancel := Context.WithTimeout(Context.Background(), 10*Time.Second)
	defer Cancel()
	if _, Err := First.Exec(Ctx, "CREATE TABLE isolated (id integer)"); Err != nil {
		T.Fatal("cannot create test table")
	}
	var Absent bool
	if Err := Second.QueryRow(Ctx, "SELECT to_regclass('isolated') IS NULL").Scan(&Absent); Err != nil || !Absent {
		T.Fatal("schemas must isolate tables across integration tests")
	}

	Db := Stdlib.OpenDB(*First.Config().ConnConfig)
	defer Db.Close()
	Provider, Err := Goose.NewProvider(Goose.DialectPostgres, Db, Migrations.FS(), Goose.WithGoMigrations(
		Goose.NewGoMigration(1, &Goose.GoFunc{RunTx: func(Ctx Context.Context, Tx *Sql.Tx) error {
			_, Err := Tx.ExecContext(Ctx, "CREATE TABLE goose_probe (id integer)")
			return Err
		}}, nil),
	))
	if Err != nil {
		T.Fatal("cannot initialize goose provider:", Err)
	}
	if _, Err := Provider.Up(Ctx); Err != nil {
		T.Fatal("goose migration failed:", Err)
	}
	Results, Err := Provider.Up(Ctx)
	if Err != nil || len(Results) != 0 {
		T.Fatal("repeated goose up must be a no-op")
	}
	var Exists bool
	if Err := First.QueryRow(Ctx, "SELECT to_regclass('goose_probe') IS NOT NULL").Scan(&Exists); Err != nil || !Exists {
		T.Fatal("goose must apply migration to isolated schema")
	}
}
