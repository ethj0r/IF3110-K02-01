//go:build integration

package Testutil

import (
	Context "context"
	Rand "crypto/rand"
	Hex "encoding/hex"
	Pgx "github.com/jackc/pgx/v5"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
	Os "os"
	Testing "testing"
	Time "time"
)

func Database(T *Testing.T) *Pgxpool.Pool {
	T.Helper()
	Dsn := Os.Getenv("TEST_DATABASE_URL")
	if Dsn == "" {
		T.Fatal("TEST_DATABASE_URL must point to a dedicated test database")
	}
	Ctx, Cancel := Context.WithTimeout(Context.Background(), 10*Time.Second)
	defer Cancel()
	Admin, Err := Pgxpool.New(Ctx, Dsn)
	if Err != nil {
		T.Fatal("invalid test database configuration")
	}
	T.Cleanup(Admin.Close)
	if Err := Admin.Ping(Ctx); Err != nil {
		T.Fatal("test database is unavailable")
	}
	var Nonce [12]byte
	if _, Err := Rand.Read(Nonce[:]); Err != nil {
		T.Fatal("cannot generate test schema name")
	}
	Schema := "test_" + Hex.EncodeToString(Nonce[:])
	Quoted := Pgx.Identifier{Schema}.Sanitize()
	if _, Err := Admin.Exec(Ctx, "CREATE SCHEMA "+Quoted); Err != nil {
		T.Fatal("cannot create isolated test schema")
	}
	T.Cleanup(func() {
		Cleanup, Cancel := Context.WithTimeout(Context.Background(), 10*Time.Second)
		defer Cancel()
		if _, Err := Admin.Exec(Cleanup, "DROP SCHEMA "+Quoted+" CASCADE"); Err != nil {
			T.Error("cannot remove isolated test schema")
		}
	})
	Cfg := Admin.Config().Copy()
	Cfg.ConnConfig.RuntimeParams["search_path"] = Schema
	Pool, Err := Pgxpool.NewWithConfig(Ctx, Cfg)
	if Err != nil {
		T.Fatal("cannot create isolated test pool")
	}
	T.Cleanup(Pool.Close)
	return Pool
}
