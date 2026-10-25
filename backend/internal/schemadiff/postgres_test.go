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

func TestEnumPreparationOnlyIncludesEnumAdditions(T *Testing.T) {
	Enum := `ALTER TYPE "quoted"."a"" ADD VALUE enum" ADD VALUE 'it''s new'`
	Dependent := `ALTER TABLE "quoted".items ALTER COLUMN state SET DEFAULT 'it''s new'`
	Candidate, Err := FormatPlan(Diff.Plan{Statements: []Diff.Statement{{DDL: Enum}, {DDL: Dependent}}})
	if Err != nil || !Candidate.RequiresRecomparison || Candidate.SQLText != Enum+";" || len(Candidate.Statements) != 2 {
		T.Fatal("enum additions were not isolated before dependent changes")
	}
	Only, Err := FormatPlan(Diff.Plan{Statements: []Diff.Statement{{DDL: Enum}}})
	if Err != nil || Only.RequiresRecomparison || Only.SQLText != Enum+";" {
		T.Fatal("enum-only migration incorrectly requires another comparison")
	}
	QuotedTable := `ALTER TABLE "ALTER TYPE fake ADD VALUE".items ADD COLUMN value text`
	Unrelated, Err := FormatPlan(Diff.Plan{Statements: []Diff.Statement{{DDL: QuotedTable}}})
	if Err != nil || Unrelated.RequiresRecomparison {
		T.Fatal("quoted SQL-like name was treated as an enum addition")
	}
	if _, Err := FormatPlan(Diff.Plan{Statements: []Diff.Statement{{DDL: `DROP TYPE "public"."state"`}}}); !Errors.Is(Err, ErrUnsupported) {
		T.Fatal("enum removal can produce an invalid dependent plan")
	}
}
