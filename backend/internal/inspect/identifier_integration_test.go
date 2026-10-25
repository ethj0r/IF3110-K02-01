//go:build integration

package Inspect

import (
	Context "context"
	Json "encoding/json"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
	Pgx "github.com/jackc/pgx/v5"
	Url "net/url"
	Strings "strings"
	Testing "testing"
)

func TestQuotedIdentifiersAreLiteralAcrossViewerEndpoints(T *Testing.T) {
	App := NewFixture(T)
	App.SeedTarget(T)
	Target, Err := Driver.PostgresDriver{}.Open(App.Target)
	if Err != nil {
		T.Fatal(Err)
	}
	defer Target.Close()
	Schema := App.Schema + " 計画 space"
	if Err := Target.Exec(Context.Background(), "ALTER SCHEMA "+Pgx.Identifier{App.Schema}.Sanitize()+" RENAME TO "+Pgx.Identifier{Schema}.Sanitize()); Err != nil {
		T.Fatal(Err)
	}
	App.Schema = Schema
	ID := App.ConnectionID(T)
	for _, Name := range []string{"order items", "has-dash", "表", `a"b`, "a/b", "a%20b", ".", "..", `x"; DROP TABLE items;--`} {
		T.Run(Name, func(T *Testing.T) {
			Qualified := Pgx.Identifier{Schema, Name}.Sanitize()
			if Err := Target.Exec(Context.Background(), "CREATE TABLE "+Qualified+` ("id key" integer PRIMARY KEY, "cell value" text CHECK (length("cell value") > 0)); INSERT INTO `+Qualified+` VALUES (1, 'literal value')`); Err != nil {
				T.Fatal(Err)
			}
			Base := "/api/databases/" + ID + "/tables/" + Url.PathEscape(Name)
			Query := "?schema=" + Url.QueryEscape(Schema)
			if Name == "." || Name == ".." {
				Base = "/api/databases/" + ID + "/table"
				Query += "&table=" + Url.QueryEscape(Name)
			}
			for _, Endpoint := range []string{"columns", "constraints", "indexes", "rows"} {
				Response := App.Request("GET", Base+"/"+Endpoint+Query, App.Member)
				if Response.Code != 200 {
					T.Fatalf("%s for %q returned %d: %s", Endpoint, Name, Response.Code, Response.Body.String())
				}
				if Endpoint == "rows" {
					var Page TablePage
					if Err := Json.Unmarshal(Response.Body.Bytes(), &Page); Err != nil || Page.Total != 1 || len(Page.Rows) != 1 || *Page.Rows[0][1] != "literal value" {
						T.Fatal("quoted table data changed")
					}
				} else if !Strings.Contains(Response.Body.String(), "id key") {
					T.Fatalf("%s missed quoted column", Endpoint)
				}
			}
		})
	}
	Listed := App.Request("GET", "/api/databases/"+ID+"/tables?schema="+Url.QueryEscape(Schema), App.Member)
	if Listed.Code != 200 || !Strings.Contains(Listed.Body.String(), "order items") {
		T.Fatal("quoted schema was not listed")
	}
	Original := App.Request("GET", "/api/databases/"+ID+"/tables/items/rows?schema="+Url.QueryEscape(Schema), App.Member)
	if Original.Code != 200 {
		T.Fatal("identifier text executed SQL against the original table")
	}
}
