//go:build integration

package Migrations

import (
	Context "context"
	Testutil "github.com/ethj0r/IF3110-K02-01/backend/internal/testutil"
	Stdlib "github.com/jackc/pgx/v5/stdlib"
	Goose "github.com/pressly/goose/v3"
	Testing "testing"
)

func TestAccessSchemaMigration(T *Testing.T) {
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
	UserID := Testutil.CreateUser(T, Pool, "access-member", false)
	ConnectionID := Testutil.CreateConnection(T, Pool, "access-target")

	var GroupID string
	if Err := Pool.QueryRow(Ctx, "INSERT INTO groups (name, description) VALUES ('Readers', 'Read access') RETURNING id::text").Scan(&GroupID); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Pool.Exec(Ctx, "INSERT INTO groups (name) VALUES ('readers')"); Err == nil {
		T.Fatal("case-insensitive group name uniqueness was not enforced")
	}
	if _, Err := Pool.Exec(Ctx, "INSERT INTO groups (name) VALUES ('')"); Err == nil {
		T.Fatal("empty group name was accepted")
	}
	if _, Err := Pool.Exec(Ctx, "INSERT INTO user_groups (user_id, group_id) VALUES ($1, $2)", UserID, GroupID); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Pool.Exec(Ctx, "INSERT INTO group_database_access (group_id, database_id, access_level) VALUES ($1, $2, 'view_edit')", GroupID, ConnectionID); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Pool.Exec(Ctx, "INSERT INTO group_database_access (group_id, database_id, access_level) VALUES ($1, $2, 'owner')", GroupID, ConnectionID); Err == nil {
		T.Fatal("invalid access level was accepted")
	}
	if _, Err := Pool.Exec(Ctx, "DELETE FROM groups WHERE id = $1", GroupID); Err != nil {
		T.Fatal(Err)
	}
	var Remaining int
	if Err := Pool.QueryRow(Ctx, "SELECT (SELECT count(*) FROM user_groups) + (SELECT count(*) FROM group_database_access)").Scan(&Remaining); Err != nil || Remaining != 0 {
		T.Fatalf("group deletion did not cascade: remaining=%d err=%v", Remaining, Err)
	}
}
