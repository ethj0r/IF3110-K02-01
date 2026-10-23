//go:build integration

package Testdb

import (
	Context "context"
	Testutil "github.com/ethj0r/IF3110-K02-01/backend/internal/testutil"
	Migrations "github.com/ethj0r/IF3110-K02-01/backend/migrations"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
	Stdlib "github.com/jackc/pgx/v5/stdlib"
	Goose "github.com/pressly/goose/v3"
	Testing "testing"
)

func PlatformDatabase(T *Testing.T) *Pgxpool.Pool {
	T.Helper()
	Pool := Testutil.Database(T)
	Db := Stdlib.OpenDB(*Pool.Config().ConnConfig)
	T.Cleanup(func() { _ = Db.Close() })
	Provider, Err := Goose.NewProvider(Goose.DialectPostgres, Db, Migrations.FS())
	if Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Provider.Up(Context.Background()); Err != nil {
		T.Fatal(Err)
	}
	return Pool
}
