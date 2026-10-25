//go:build integration

package Inspect

import (
	Context "context"
	Json "encoding/json"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
	Pgx "github.com/jackc/pgx/v5"
	Math "math"
	Strings "strings"
	Testing "testing"
)

func (App *Fixture) SeedRows(T *Testing.T, Count int) {
	T.Helper()
	Target, Err := Driver.PostgresDriver{}.Open(App.Target)
	if Err != nil {
		T.Fatal(Err)
	}
	defer Target.Close()
	for Index := 0; Index < Count; Index++ {
		if Err := Target.Exec(Context.Background(), "INSERT INTO "+Pgx.Identifier{App.Schema, "items"}.Sanitize()+" (name) VALUES ($1)", "row"); Err != nil {
			T.Fatal(Err)
		}
	}
}

func TestDetailEndpoints(T *Testing.T) {
	App := NewFixture(T)
	App.SeedTarget(T)
	ID := App.ConnectionID(T)
	Columns := App.Request("GET", "/api/databases/"+ID+"/tables/items/columns?schema="+App.Schema, App.Member)
	if Columns.Code != 200 || !Strings.Contains(Columns.Body.String(), `"name":"id"`) || !Strings.Contains(Columns.Body.String(), `"name":"name"`) {
		T.Fatalf("column list failed: %d %s", Columns.Code, Columns.Body.String())
	}
	if !Strings.Contains(Columns.Body.String(), "integer") || !Strings.Contains(Columns.Body.String(), `"nullable":false`) {
		T.Fatalf("column detail missed types: %s", Columns.Body.String())
	}
	Constraints := App.Request("GET", "/api/databases/"+ID+"/tables/items/constraints?schema="+App.Schema, App.Member)
	if Constraints.Code != 200 || !Strings.Contains(Constraints.Body.String(), "PRIMARY KEY") {
		T.Fatalf("constraint list failed: %d %s", Constraints.Code, Constraints.Body.String())
	}
	Indexes := App.Request("GET", "/api/databases/"+ID+"/tables/items/indexes?schema="+App.Schema, App.Member)
	if Indexes.Code != 200 || !Strings.Contains(Indexes.Body.String(), "items_pkey") {
		T.Fatalf("index list failed: %d %s", Indexes.Code, Indexes.Body.String())
	}
	Missing := App.Request("GET", "/api/databases/"+ID+"/tables/missing/columns?schema="+App.Schema, App.Member)
	if Missing.Code != 200 || !Strings.Contains(Missing.Body.String(), `"columns":[]`) {
		T.Fatalf("missing table detail must be empty: %d %s", Missing.Code, Missing.Body.String())
	}
	if App.Request("GET", "/api/databases/"+ID+"/tables/bad%00name/columns?schema="+App.Schema, App.Member).Code != 400 {
		T.Fatal("NUL in table name was accepted")
	}
}

func TestTableRowsPagination(T *Testing.T) {
	App := NewFixture(T)
	App.SeedTarget(T)
	App.SeedRows(T, 3)
	Target, Err := Driver.PostgresDriver{}.Open(App.Target)
	if Err != nil {
		T.Fatal(Err)
	}
	defer Target.Close()
	if Err := Target.Exec(Context.Background(), "UPDATE "+Pgx.Identifier{App.Schema, "items"}.Sanitize()+" SET name = 'moved' WHERE id = 1"); Err != nil {
		T.Fatal(Err)
	}
	ID := App.ConnectionID(T)
	var First TablePage
	Response := App.Request("GET", "/api/databases/"+ID+"/tables/items/rows?schema="+App.Schema+"&page=1&page_size=2", App.Member)
	if Response.Code != 200 {
		T.Fatalf("row list failed: %d", Response.Code)
	}
	if Err := Json.Unmarshal(Response.Body.Bytes(), &First); Err != nil {
		T.Fatal(Err)
	}
	if First.Total != 3 || len(First.Rows) != 2 || len(First.Columns) != 2 || First.Page != 1 || First.PageSize != 2 {
		T.Fatalf("unexpected first page: %+v", First)
	}
	if *First.Rows[0][0] != "1" || *First.Rows[1][0] != "2" {
		T.Fatal("pagination must follow the primary key, not heap order")
	}
	var Second TablePage
	Next := App.Request("GET", "/api/databases/"+ID+"/tables/items/rows?schema="+App.Schema+"&page=2&page_size=2", App.Member)
	if Next.Code != 200 {
		T.Fatalf("second page failed: %d", Next.Code)
	}
	if Err := Json.Unmarshal(Next.Body.Bytes(), &Second); Err != nil || Second.Total != 3 || len(Second.Rows) != 1 {
		T.Fatalf("unexpected second page: %s %v", Next.Body.String(), Err)
	}
	if *Second.Rows[0][0] != "3" {
		T.Fatal("pagination repeated or skipped a primary key")
	}
	if _, Err := App.Service.Rows(Context.Background(), ID, App.Schema, "items", Math.MaxInt, 100); Err != ErrInvalidInput {
		T.Fatal("overflowing pagination offset was accepted")
	}
	for _, Path := range []string{
		"/api/databases/" + ID + "/tables/items/rows?schema=" + App.Schema + "&page=1&page_size=101",
		"/api/databases/" + ID + "/tables/items/rows?schema=" + App.Schema + "&page=0",
		"/api/databases/" + ID + "/tables/items/rows?schema=" + App.Schema + "&page=abc",
	} {
		if App.Request("GET", Path, App.Member).Code != 400 {
			T.Fatalf("invalid pagination accepted: %s", Path)
		}
	}
	if App.Request("GET", "/api/databases/"+ID+"/tables/missing/rows?schema="+App.Schema, App.Member).Code != 404 {
		T.Fatal("missing table rows did not return 404")
	}
	if App.Request("GET", "/api/databases/"+ID+"/tables/bad%00name/rows?schema="+App.Schema, App.Member).Code != 400 {
		T.Fatal("NUL in table name rows was accepted")
	}
}
