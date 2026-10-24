//go:build integration

package Inspect

import (
	Context "context"
	Errors "errors"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
	Pgx "github.com/jackc/pgx/v5"
	Strings "strings"
	Testing "testing"
	Time "time"
)

type changingDriver struct {
	Driver.Driver
	BeforeData func()
}

func (D changingDriver) Open(DSN string) (Driver.Conn, error) {
	Conn, Err := D.Driver.Open(DSN)
	return changingConn{Conn: Conn, BeforeData: D.BeforeData}, Err
}

type changingConn struct {
	Driver.Conn
	BeforeData func()
}

func (C changingConn) Begin(Ctx Context.Context) (Driver.Tx, error) {
	Tx, Err := C.Conn.Begin(Ctx)
	return changingTx{Tx: Tx, BeforeData: C.BeforeData}, Err
}

type changingTx struct {
	Driver.Tx
	BeforeData func()
}

func (Tx changingTx) Query(Ctx Context.Context, SQL string, Args ...any) (Pgx.Rows, error) {
	if Strings.Contains(SQL, " LIMIT $1 OFFSET $2") {
		Tx.BeforeData()
	}
	return Tx.Tx.Query(Ctx, SQL, Args...)
}

func TestRowsUseOneSnapshot(T *Testing.T) {
	App := NewFixture(T)
	App.SeedTarget(T)
	App.SeedRows(T, 2)
	Writer, Err := Driver.PostgresDriver{}.Open(App.Target)
	if Err != nil {
		T.Fatal(Err)
	}
	defer Writer.Close()
	Changed := false
	App.Service.Driver = changingDriver{Driver: Driver.PostgresDriver{}, BeforeData: func() {
		Changed = true
		if Err := Writer.Exec(Context.Background(), "INSERT INTO "+Pgx.Identifier{App.Schema, "items"}.Sanitize()+" (name) VALUES ('concurrent')"); Err != nil {
			T.Fatal(Err)
		}
	}}
	Page, Err := App.Service.Rows(Context.Background(), App.ConnectionID(T), App.Schema, "items", 1, 100)
	if Err != nil || !Changed || Page.Total != 2 || len(Page.Rows) != 2 {
		T.Fatalf("count and data used different snapshots: %+v %v", Page, Err)
	}
	App.Service.Driver = Driver.PostgresDriver{}
	Fresh, Err := App.Service.Rows(Context.Background(), App.ConnectionID(T), App.Schema, "items", 1, 100)
	if Err != nil || Fresh.Total != 3 || len(Fresh.Rows) != 3 {
		T.Fatalf("next request did not see committed changes: %+v %v", Fresh, Err)
	}
}

func TestRowsWithoutPrimaryKeyAndQueryCancellation(T *Testing.T) {
	App := NewFixture(T)
	App.SeedTarget(T)
	Target, Err := Driver.PostgresDriver{}.Open(App.Target)
	if Err != nil {
		T.Fatal(Err)
	}
	defer Target.Close()
	Ctx := Context.Background()
	Name := Pgx.Identifier{App.Schema, "plain"}.Sanitize()
	if Err := Target.Exec(Ctx, "CREATE TABLE "+Name+" (browse_row json, value text); INSERT INTO "+Name+` VALUES ('{}', 'c'), ('{}', 'a'), ('{}', 'b')`); Err != nil {
		T.Fatal(Err)
	}
	ID := App.ConnectionID(T)
	First, Err := App.Service.Rows(Ctx, ID, App.Schema, "plain", 1, 2)
	if Err != nil || len(First.Rows) != 2 || *First.Rows[0][1] != "a" || *First.Rows[1][1] != "b" {
		T.Fatalf("table without primary key was not ordered: %+v %v", First, Err)
	}
	Second, Err := App.Service.Rows(Ctx, ID, App.Schema, "plain", 2, 2)
	if Err != nil || len(Second.Rows) != 1 || *Second.Rows[0][1] != "c" {
		T.Fatalf("unstable fallback pagination: %+v %v", Second, Err)
	}
	Function := Pgx.Identifier{App.Schema, "write_during_read"}.Sanitize()
	SQL := "CREATE FUNCTION " + Function + "() RETURNS integer LANGUAGE plpgsql AS $$ BEGIN INSERT INTO " +
		Pgx.Identifier{App.Schema, "items"}.Sanitize() + " (name) VALUES ('unexpected'); RETURN 1; END $$; " +
		"CREATE VIEW " + Pgx.Identifier{App.Schema, "write_view"}.Sanitize() + " AS SELECT " + Function + "() AS id"
	if Err := Target.Exec(Ctx, SQL); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := App.Service.Rows(Ctx, ID, App.Schema, "write_view", 1, 25); !Errors.Is(Err, ErrUnavailable) {
		T.Fatal("viewer allowed a function to write target data")
	}
	Unchanged, Err := App.Service.Rows(Ctx, ID, App.Schema, "items", 1, 25)
	if Err != nil || Unchanged.Total != 0 {
		T.Fatal("view inspection changed the target table")
	}
	if Err := Target.Exec(Ctx, "CREATE VIEW "+Pgx.Identifier{App.Schema, "slow"}.Sanitize()+" AS SELECT 1 AS id FROM pg_sleep(5)"); Err != nil {
		T.Fatal(Err)
	}
	Budget, Cancel := Context.WithTimeout(Ctx, 100*Time.Millisecond)
	defer Cancel()
	Start := Time.Now()
	if _, Err := App.Service.Rows(Budget, ID, App.Schema, "slow", 1, 25); !Errors.Is(Err, ErrUnavailable) {
		T.Fatalf("slow target query was not cancelled safely: %v", Err)
	}
	if Time.Since(Start) > 2*Time.Second {
		T.Fatal("query kept running after the request deadline")
	}
}
