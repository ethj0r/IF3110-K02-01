//go:build integration

package Migrations

import (
	Context "context"
	Testutil "github.com/ethj0r/IF3110-K02-01/backend/internal/testutil"
	Stdlib "github.com/jackc/pgx/v5/stdlib"
	Goose "github.com/pressly/goose/v3"
	Testing "testing"
)

func TestAuthSchemaMigration(T *Testing.T) {
	Pool := Testutil.Database(T)
	Db := Stdlib.OpenDB(*Pool.Config().ConnConfig)
	defer Db.Close()
	Provider, Err := Goose.NewProvider(Goose.DialectPostgres, Db, FS())
	if Err != nil {
		T.Fatal(Err)
	}
	Ctx := Context.Background()
	if _, Err := Provider.Up(Ctx); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Pool.Exec(Ctx, "INSERT INTO users (username, password_hash) VALUES ('Admin', 'test-hash')"); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Pool.Exec(Ctx, "INSERT INTO users (username, password_hash) VALUES ('admin', 'test-hash')"); Err == nil {
		T.Fatal("case-insensitive username uniqueness was not enforced")
	}
	if _, Err := Pool.Exec(Ctx, "INSERT INTO users (username, password_hash) VALUES ('x', 'test-hash')"); Err == nil {
		T.Fatal("invalid username was accepted")
	}
	if _, Err := Provider.Down(Ctx); Err != nil {
		T.Fatal(Err)
	}
	var Removed bool
	if Err := Pool.QueryRow(Ctx, "SELECT to_regclass('users') IS NULL AND to_regclass('sessions') IS NULL AND to_regclass('session_identifiers') IS NULL").Scan(&Removed); Err != nil || !Removed {
		T.Fatal("auth schema rollback failed")
	}
	if _, Err := Provider.Up(Ctx); Err != nil {
		T.Fatal("auth schema cannot be reapplied", Err)
	}
}
