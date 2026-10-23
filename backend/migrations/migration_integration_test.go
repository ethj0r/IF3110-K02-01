//go:build integration

package Migrations

import (
	Context "context"
	Testing "testing"

	Testutil "github.com/ethj0r/IF3110-K02-01/backend/internal/testutil"
	Stdlib "github.com/jackc/pgx/v5/stdlib"
	Goose "github.com/pressly/goose/v3"
)

func TestMigrationSchema(T *Testing.T) {
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

	var UserID string
	if Err := Pool.QueryRow(Ctx, "INSERT INTO users (username, password_hash) VALUES ('migrator', 'test-hash') RETURNING id").Scan(&UserID); Err != nil {
		T.Fatal(Err)
	}

	var DB1ID, DB2ID string
	if Err := Pool.QueryRow(Ctx, "INSERT INTO db_connections (name, host, port, dbname, username, password_ciphertext, created_by) VALUES ('db-1', '127.0.0.1', 5432, 'target1', 'postgres', '\\x01', $1) RETURNING id", UserID).Scan(&DB1ID); Err != nil {
		T.Fatal(Err)
	}
	if Err := Pool.QueryRow(Ctx, "INSERT INTO db_connections (name, host, port, dbname, username, password_ciphertext, created_by) VALUES ('db-2', '127.0.0.1', 5432, 'target2', 'postgres', '\\x01', $1) RETURNING id", UserID).Scan(&DB2ID); Err != nil {
		T.Fatal(Err)
	}

	var MigID string
	if Err := Pool.QueryRow(Ctx, "INSERT INTO migrations (database_id, version, title, sql_text, status, created_by) VALUES ($1, 1, 'init schema', 'CREATE TABLE t1 (id INT);', 'committed', $2) RETURNING id", DB1ID, UserID).Scan(&MigID); Err != nil || MigID == "" {
		T.Fatalf("failed to insert migration: %v", Err)
	}

	if _, Err := Pool.Exec(Ctx, "INSERT INTO migrations (database_id, version, title, sql_text, status, created_by) VALUES ($1, 1, 'duplicate version', 'CREATE TABLE t2 (id INT);', 'committed', $2)", DB1ID, UserID); Err == nil {
		T.Fatal("duplicate version on same database was accepted")
	}

	if _, Err := Pool.Exec(Ctx, "INSERT INTO migrations (database_id, version, title, sql_text, status, created_by) VALUES ($1, 1, 'db2 init schema', 'CREATE TABLE t1 (id INT);', 'committed', $2)", DB2ID, UserID); Err != nil {
		T.Fatalf("same version on different database should be allowed: %v", Err)
	}

	if _, Err := Pool.Exec(Ctx, "INSERT INTO migrations (database_id, version, title, sql_text, status, created_by) VALUES ($1, 0, 'invalid version', 'SELECT 1;', 'draft', $2)", DB1ID, UserID); Err == nil {
		T.Fatal("version 0 was accepted")
	}

	if _, Err := Pool.Exec(Ctx, "INSERT INTO migrations (database_id, version, title, sql_text, status, created_by) VALUES ($1, 2, 'invalid status', 'SELECT 1;', 'unknown_status', $2)", DB1ID, UserID); Err == nil {
		T.Fatal("invalid status was accepted")
	}

	if _, Err := Provider.DownTo(Ctx, 0); Err != nil {
		T.Fatalf("rollback failed: %v", Err)
	}

	var Removed bool
	if Err := Pool.QueryRow(Ctx, "SELECT to_regclass('migrations') IS NULL AND to_regclass('db_connections') IS NULL AND to_regclass('users') IS NULL").Scan(&Removed); Err != nil || !Removed {
		T.Fatal("migrations schema rollback did not drop all tables")
	}

	if _, Err := Provider.Up(Ctx); Err != nil {
		T.Fatalf("schema cannot be reapplied: %v", Err)
	}
}

