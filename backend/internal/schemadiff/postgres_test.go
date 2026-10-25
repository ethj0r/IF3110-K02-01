package Schemadiff

import (
	Errors "errors"
	Diff "github.com/stripe/pg-schema-diff/pkg/diff"
	Strings "strings"
	Testing "testing"
)

func TestCandidatePreservesStatementsAndHazards(T *Testing.T) {
	Function := `CREATE FUNCTION public.sample() RETURNS text LANGUAGE sql AS $$ SELECT 'a;b' $$`
	Plan := Diff.Plan{CurrentSchemaHash: "snapshot-hash", Statements: []Diff.Statement{
		{DDL: Function},
		{DDL: `DROP TABLE public.obsolete`, Hazards: []Diff.MigrationHazard{{Type: Diff.MigrationHazardTypeDeletesData, Message: "Drops table data"}}},
	}}
	Result, Err := FormatPlan(Plan)
	if Err != nil || Result.CurrentSchemaHash != Plan.CurrentSchemaHash || len(Result.Statements) != 2 || Result.SQLText != Function+";\n\nDROP TABLE public.obsolete;" {
		T.Fatal("candidate SQL was split or changed")
	}
	if len(Result.Statements[0].Hazards) != 0 || Result.Statements[1].Hazards[0].Type != "DELETES_DATA" {
		T.Fatal("hazard information lost")
	}
	Empty, Err := FormatPlan(Diff.Plan{})
	if Err != nil || Empty.Statements == nil || len(Empty.Statements) != 0 || Empty.SQLText != "" {
		T.Fatal("no-difference result must be empty, not null")
	}
}

func TestCandidateFitsMigrationRequestLimit(T *Testing.T) {
	for _, SQL := range []string{Strings.Repeat("x", MaxSQLBytes+1), Strings.Repeat("\"", 31000)} {
		if _, Err := FormatPlan(Diff.Plan{Statements: []Diff.Statement{{DDL: SQL}}}); !Errors.Is(Err, ErrTooLarge) {
			T.Fatal("candidate exceeds migration HTTP limit")
		}
	}
}
