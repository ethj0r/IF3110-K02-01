//go:build integration

package Inspect

import (
	Context "context"
	Json "encoding/json"
	Connection "github.com/ethj0r/IF3110-K02-01/backend/internal/connection"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
	Pgx "github.com/jackc/pgx/v5"
	Strings "strings"
	Testing "testing"
)

func TestRowsPreservePostgresValues(T *Testing.T) {
	App := NewFixture(T)
	App.SeedTarget(T)
	Target, Err := Driver.PostgresDriver{}.Open(App.Target)
	if Err != nil {
		T.Fatal(Err)
	}
	defer Target.Close()
	Table := Pgx.Identifier{App.Schema, "values"}.Sanitize()
	SQL := `CREATE TABLE ` + Table + ` AS SELECT
9223372036854775807::bigint AS large, 12345678901234567890.12::numeric AS exact,
'01234567-89ab-cdef-0123-456789abcdef'::uuid AS identifier,
'NaN'::float8 AS nan, 'Infinity'::float8 AS positive, '-Infinity'::float8 AS negative,
NULL::text AS missing, ''::text AS empty, ARRAY[9223372036854775807::bigint] AS numbers,
'{"large":9223372036854775807}'::jsonb AS document, decode('00ff', 'hex') AS bytes,
'2026-10-24'::date AS day`
	if Err := Target.Exec(Context.Background(), SQL); Err != nil {
		T.Fatal(Err)
	}
	Response := App.Request("GET", "/api/databases/"+App.ConnectionID(T)+"/tables/values/rows?schema="+App.Schema, App.Member)
	if Response.Code != 200 {
		T.Fatalf("data serialization failed: %d %s", Response.Code, Response.Body.String())
	}
	var Page TablePage
	if Err := Json.Unmarshal(Response.Body.Bytes(), &Page); Err != nil {
		T.Fatal(Err)
	}
	if len(Page.Rows) != 1 || Page.Total != 1 || len(Page.Rows[0]) != 12 {
		T.Fatalf("unexpected page: %+v", Page)
	}
	Expected := []string{
		"9223372036854775807", "12345678901234567890.12", "01234567-89ab-cdef-0123-456789abcdef",
		"NaN", "Infinity", "-Infinity", "", "", "{9223372036854775807}",
		`{"large": 9223372036854775807}`, `\x00ff`, "2026-10-24",
	}
	for Index, Value := range Page.Rows[0] {
		if Index == 6 {
			if Value != nil {
				T.Fatal("SQL NULL must stay JSON null")
			}
		} else if Value == nil || *Value != Expected[Index] {
			T.Fatalf("column %s lost its text value, want %q", Page.Columns[Index], Expected[Index])
		}
	}
}

func TestConstraintsVisibleToReadOnlyTargetRole(T *Testing.T) {
	App := NewFixture(T)
	App.SeedTarget(T)
	Target, Err := Driver.PostgresDriver{}.Open(App.Target)
	if Err != nil {
		T.Fatal(Err)
	}
	defer Target.Close()
	Role := Pgx.Identifier{App.Schema + "_reader"}.Sanitize()
	Ctx := Context.Background()
	if Err := Target.Exec(Ctx, "CREATE ROLE "+Role+" LOGIN PASSWORD 'integration_reader_password'"); Err != nil {
		T.Fatal("test database user must be able to create isolated test roles")
	}
	defer func() {
		if Err := Target.Exec(Ctx, "DROP OWNED BY "+Role+"; DROP ROLE "+Role); Err != nil {
			T.Error("cannot remove target test role")
		}
	}()
	Table := Pgx.Identifier{App.Schema, "items"}.Sanitize()
	SQL := "ALTER TABLE " + Table + " ADD COLUMN parent_id integer CONSTRAINT parent_fk REFERENCES " + Table + " (id), " +
		"ADD COLUMN code varchar(12), ADD COLUMN amount numeric(12,2), " +
		"ADD CONSTRAINT name_check CHECK (name <> ''), ADD CONSTRAINT name_unique UNIQUE (name); " +
		"GRANT USAGE ON SCHEMA " + Pgx.Identifier{App.Schema}.Sanitize() + " TO " + Role + "; " +
		"GRANT SELECT ON " + Table + " TO " + Role
	if Err := Target.Exec(Ctx, SQL); Err != nil {
		T.Fatal(Err)
	}
	Input, Err := Connection.ParseDatabaseURL(App.Target)
	if Err != nil {
		T.Fatal(Err)
	}
	Input.Name, Input.Username, Input.Password = "reader", App.Schema+"_reader", "integration_reader_password"
	Stored, Err := App.Service.Connections.(*Connection.Service).Create(Ctx, Input, nil)
	if Err != nil {
		T.Fatal(Err)
	}
	Constraints, Err := App.Service.Constraints(Ctx, Stored.ID, App.Schema, "items")
	if Err != nil {
		T.Fatal(Err)
	}
	Found := map[string]string{}
	for _, Item := range Constraints {
		Found[Item.Name] = Item.Type
		if Item.Definition == "" || (Item.Name == "parent_fk" && !Strings.Contains(Item.Definition, "REFERENCES")) {
			T.Fatalf("constraint definition was omitted: %+v", Item)
		}
	}
	for Name, Kind := range map[string]string{"items_pkey": "PRIMARY KEY", "name_check": "CHECK", "name_unique": "UNIQUE", "parent_fk": "FOREIGN KEY"} {
		if Found[Name] != Kind {
			T.Fatalf("read-only role cannot inspect %s: %+v", Name, Constraints)
		}
	}
	Columns, Err := App.Service.Columns(Ctx, Stored.ID, App.Schema, "items")
	if Err != nil {
		T.Fatal(Err)
	}
	Types := map[string]string{}
	for _, Column := range Columns {
		Types[Column.Name] = Column.DataType
	}
	if Types["code"] != "character varying(12)" || Types["amount"] != "numeric(12,2)" {
		T.Fatalf("column type details were omitted: %+v", Columns)
	}
}
