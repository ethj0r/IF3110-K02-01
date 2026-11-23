//go:build integration

package Logging

import (
	Context "context"
	Slog "log/slog"
	Testing "testing"
	Time "time"

	Testutil "github.com/ethj0r/IF3110-K02-01/backend/internal/testutil"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
)

func TestSQLTracerAgainstPostgreSQL(T *Testing.T) {
	Base := Testutil.Database(T)
	Logger, Buffer := capture(Slog.LevelInfo)
	Cfg := Base.Config().Copy()
	Cfg.ConnConfig.Tracer = SQLTracer{Database: "target", Logger: Logger}
	Ctx, Cancel := Context.WithTimeout(Context.Background(), 10*Time.Second)
	defer Cancel()
	Pool, Err := Pgxpool.NewWithConfig(Ctx, Cfg)
	if Err != nil {
		T.Fatal(Err)
	}
	defer Pool.Close()
	Ctx = NewScope(Ctx, "request_id", "req-int", "migration_id", "mig-int")

	if _, Err := Pool.Exec(Ctx, "CREATE TABLE traced (id integer PRIMARY KEY, title text NOT NULL)"); Err != nil {
		T.Fatal(Err)
	}
	Tx, Err := Pool.Begin(Ctx)
	if Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Tx.Exec(Ctx, "INSERT INTO traced VALUES ($1, $2), ($3, $4)", 1, "one", 2, "two"); Err != nil {
		T.Fatal(Err)
	}
	if Err := Tx.Commit(Ctx); Err != nil {
		T.Fatal(Err)
	}
	Tx, Err = Pool.Begin(Ctx)
	if Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Tx.Exec(Ctx, "INSERT INTO traced VALUES ($1, $2)", 1, "duplicate"); Err == nil {
		T.Fatal("duplicate key must fail")
	}
	if Err := Tx.Rollback(Ctx); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Pool.Exec(Ctx, "SELECT * FROM missing_table"); Err == nil {
		T.Fatal("missing table must fail")
	}

	Counts := map[string]int{}
	var Inserted, Duplicate, Missing map[string]any
	for _, Record := range records(T, Buffer) {
		Event, _ := Record["event"].(string)
		Counts[Event]++
		if Record["request_id"] != "req-int" || Record["migration_id"] != "mig-int" || Record["db"] != "target" {
			T.Fatalf("SQL record lost correlation attributes: %v", Record)
		}
		switch {
		case Event == "sql.finish" && Record["rows"] == float64(2):
			Inserted = Record
		case Record["sqlstate"] == "23505":
			Duplicate = Record
		case Record["sqlstate"] == "42P01":
			Missing = Record
		}
	}
	if Counts["sql.begin"] != 2 || Counts["sql.commit"] != 1 || Counts["sql.rollback"] != 1 {
		T.Fatalf("transaction control was not logged separately: %v", Counts)
	}
	if Counts["sql.start"] != 8 || Counts["sql.finish"] != 4 {
		T.Fatalf("every statement must log its start, got %v", Counts)
	}
	if Inserted == nil || Inserted["command"] != "INSERT" {
		T.Fatal("row count of the committed insert was not logged")
	}
	if Duplicate == nil || Duplicate["level"] != "ERROR" || Duplicate["constraint"] != "traced_pkey" {
		T.Fatalf("unique violation was not logged with SQLSTATE: %v", Duplicate)
	}
	if Missing == nil || Missing["status"] != "error" {
		T.Fatal("undefined table was not logged with SQLSTATE 42P01")
	}
}
