//go:build integration

package Schemadiff

import (
	Context "context"
	Errors "errors"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
	Testdb "github.com/ethj0r/IF3110-K02-01/backend/internal/testdb"
	Pgx "github.com/jackc/pgx/v5"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
	Url "net/url"
	Strings "strings"
	Sync "sync"
	Testing "testing"
	Time "time"
)

func ApplyCandidate(T *Testing.T, DSN, SQL string) error {
	T.Helper()
	Ctx, Cancel := Context.WithTimeout(Context.Background(), 15*Time.Second)
	defer Cancel()
	Target, Err := (Driver.PostgresDriver{}).Open(DSN)
	if Err != nil {
		return Err
	}
	defer Target.Close()
	Tx, Err := Target.Begin(Ctx)
	if Err != nil {
		return Err
	}
	defer Tx.Rollback(Context.Background())
	if Err := Tx.ExecScript(Ctx, SQL); Err != nil {
		return Err
	}
	return Tx.Commit(Ctx)
}

func TestSchemaChangeMatrix(T *Testing.T) {
	Platform := Testdb.PlatformDatabase(T)
	CurrentDSN, Current := ComparisonDatabase(T, Platform)
	ReferenceDSN, Reference := ComparisonDatabase(T, Platform)
	Ctx := Context.Background()
	Generator := PostgresGenerator{}
	for _, Case := range []struct {
		Name, Current, Reference string
		Empty                    bool
	}{
		{Name: "table_dependencies", Current: `CREATE TABLE {{schema}}.old_parent (id int PRIMARY KEY); CREATE TABLE {{schema}}.old_child (id int REFERENCES {{schema}}.old_parent(id));`, Reference: `CREATE TABLE {{schema}}.new_parent (id int PRIMARY KEY); CREATE TABLE {{schema}}.new_child (id int REFERENCES {{schema}}.new_parent(id));`},
		{Name: "column_type_default_nullability", Current: `CREATE TABLE {{schema}}.items (id int PRIMARY KEY, count int, legacy text); INSERT INTO {{schema}}.items VALUES (1, 7, 'old');`, Reference: `CREATE TABLE {{schema}}.items (id int PRIMARY KEY, count bigint NOT NULL DEFAULT 0, added text DEFAULT 'a;b');`},
		{Name: "constraints", Current: `CREATE TABLE {{schema}}.parents(id int PRIMARY KEY); CREATE TABLE {{schema}}.items(id int PRIMARY KEY, parent int, name text);`, Reference: `CREATE TABLE {{schema}}.parents(id int PRIMARY KEY); CREATE TABLE {{schema}}.items(id int PRIMARY KEY, parent int REFERENCES {{schema}}.parents(id), name text UNIQUE, CONSTRAINT positive CHECK(id>0));`},
		{Name: "index_replacement", Current: `CREATE TABLE {{schema}}.items(id int PRIMARY KEY, value text); CREATE INDEX lookup ON {{schema}}.items(value); CREATE INDEX obsolete ON {{schema}}.items(id,value);`, Reference: `CREATE TABLE {{schema}}.items(id int PRIMARY KEY, value text); CREATE UNIQUE INDEX lookup ON {{schema}}.items(lower(value)) WHERE value IS NOT NULL;`},
		{Name: "enum_creation", Reference: `CREATE TYPE {{schema}}.state AS ENUM ('new','done'); CREATE TABLE {{schema}}.items(id int PRIMARY KEY, state {{schema}}.state DEFAULT 'new');`},
		{Name: "enum_new_default", Current: `CREATE TYPE {{schema}}.state AS ENUM ('new'); CREATE TABLE {{schema}}.items(id int PRIMARY KEY, state {{schema}}.state DEFAULT 'new');`, Reference: `CREATE TYPE {{schema}}.state AS ENUM ('new','done'); CREATE TABLE {{schema}}.items(id int PRIMARY KEY, state {{schema}}.state DEFAULT 'done');`},
		{Name: "quoted_identifiers", Reference: `CREATE TABLE {{schema}}."a""; DROP SCHEMA public;--" (".." integer PRIMARY KEY, "値/%%" text DEFAULT 'semi;colon');`},
		{Name: "enum_extension", Current: `CREATE TYPE {{schema}}.state AS ENUM ('new'); CREATE TABLE {{schema}}.items(id int PRIMARY KEY, state {{schema}}.state);`, Reference: `CREATE TYPE {{schema}}.state AS ENUM ('new','done'); CREATE TABLE {{schema}}.items(id int PRIMARY KEY, state {{schema}}.state);`},
		{Name: "enum_insert_order", Current: `CREATE TYPE {{schema}}.state AS ENUM ('new','done'); CREATE TABLE {{schema}}.items(state {{schema}}.state);`, Reference: `CREATE TYPE {{schema}}.state AS ENUM ('before','new','between','done','after'); CREATE TABLE {{schema}}.items(state {{schema}}.state);`},
		{Name: "serial_sequence", Reference: `CREATE TABLE {{schema}}.items(id bigserial PRIMARY KEY, value text);`},
		{Name: "view_update", Current: `CREATE TABLE {{schema}}.items(id int PRIMARY KEY, value text); CREATE VIEW {{schema}}.summary AS SELECT id,value FROM {{schema}}.items;`, Reference: `CREATE TABLE {{schema}}.items(id int PRIMARY KEY, value text); CREATE VIEW {{schema}}.summary AS SELECT id,upper(value) AS value FROM {{schema}}.items WHERE id>0;`},
		{Name: "materialized_view", Reference: `CREATE TABLE {{schema}}.items(id int PRIMARY KEY); CREATE MATERIALIZED VIEW {{schema}}.summary AS SELECT count(*) AS total FROM {{schema}}.items; CREATE UNIQUE INDEX summary_total ON {{schema}}.summary(total);`},
		{Name: "function_trigger", Reference: `CREATE TABLE {{schema}}.items(id int PRIMARY KEY, value text); CREATE FUNCTION {{schema}}.normalize() RETURNS trigger LANGUAGE plpgsql AS $body$ BEGIN NEW.value := upper(NEW.value) || ';'; RETURN NEW; END $body$; CREATE TRIGGER normalize BEFORE INSERT ON {{schema}}.items FOR EACH ROW EXECUTE FUNCTION {{schema}}.normalize();`},
		{Name: "rls_policy", Current: `CREATE TABLE {{schema}}.items(id int PRIMARY KEY);`, Reference: `CREATE TABLE {{schema}}.items(id int PRIMARY KEY); ALTER TABLE {{schema}}.items ENABLE ROW LEVEL SECURITY; CREATE POLICY visible ON {{schema}}.items USING (id>0);`},
		{Name: "data_only_difference", Current: `CREATE TABLE {{schema}}.items(id int PRIMARY KEY); INSERT INTO {{schema}}.items VALUES (1);`, Reference: `CREATE TABLE {{schema}}.items(id int PRIMARY KEY); INSERT INTO {{schema}}.items VALUES (2),(3);`, Empty: true},
		{Name: "empty_schema", Empty: true},
	} {
		T.Run(Case.Name, func(T *Testing.T) {
			Schema := "matrix_" + Case.Name
			Quoted := Pgx.Identifier{Schema}.Sanitize()
			for _, Side := range []struct {
				Pool *Pgxpool.Pool
				SQL  string
			}{{Current, Case.Current}, {Reference, Case.Reference}} {
				if _, Err := Side.Pool.Exec(Ctx, "CREATE SCHEMA "+Quoted+"; "+Strings.ReplaceAll(Side.SQL, "{{schema}}", Quoted)); Err != nil {
					T.Fatal(Err)
				}
			}
			Before, Err := Generator.Generate(Ctx, CurrentDSN, ReferenceDSN, Schema)
			if Err != nil {
				T.Fatal(Err)
			}
			if (len(Before.Statements) == 0) != Case.Empty {
				T.Fatalf("unexpected comparison result: %s", Before.SQLText)
			}
			ReferenceBefore, Err := Generator.Generate(Ctx, ReferenceDSN, ReferenceDSN, Schema)
			if Err != nil || len(ReferenceBefore.Statements) != 0 {
				T.Fatal("self comparison failed")
			}
			Candidate := Before
			Phases := 0
			for Candidate.SQLText != "" && Phases < 3 {
				Phases++
				if Case.Name == "enum_new_default" && Phases == 1 && (!Candidate.RequiresRecomparison || Strings.Contains(Candidate.SQLText, "SET DEFAULT")) {
					T.Fatal("enum preparation mixes dependent changes")
				}
				if Err := ApplyCandidate(T, CurrentDSN, Candidate.SQLText); Err != nil {
					T.Fatalf("candidate does not execute: %v; SQL: %s", Err, Candidate.SQLText)
				}
				Candidate, Err = Generator.Generate(Ctx, CurrentDSN, ReferenceDSN, Schema)
				if Err != nil {
					T.Fatal(Err)
				}
			}
			if len(Candidate.Statements) != 0 || Candidate.RequiresRecomparison {
				T.Fatalf("candidate did not converge: %s", Candidate.SQLText)
			}
			if Case.Name == "enum_new_default" && Phases != 2 {
				T.Fatal("enum default change must use two committed migrations")
			}

			ReferenceAfter, Err := Generator.Generate(Ctx, ReferenceDSN, ReferenceDSN, Schema)
			if Err != nil || ReferenceBefore.CurrentSchemaHash != ReferenceAfter.CurrentSchemaHash {
				T.Fatal("reference schema was changed")
			}
		})
	}
}

func TestComparisonSnapshotsCancellationAndConcurrency(T *Testing.T) {
	Platform := Testdb.PlatformDatabase(T)
	CurrentDSN, Current := ComparisonDatabase(T, Platform)
	ReferenceDSN, Reference := ComparisonDatabase(T, Platform)
	Ctx := Context.Background()
	for _, Side := range []*Pgxpool.Pool{Current, Reference} {
		if _, Err := Side.Exec(Ctx, "CREATE SCHEMA snapshots; CREATE TABLE snapshots.items(id int)"); Err != nil {
			T.Fatal(Err)
		}
	}
	DB, Snapshot, Err := OpenSnapshot(Ctx, CurrentDSN)
	if Err != nil {
		T.Fatal(Err)
	}
	defer DB.Close()
	defer Snapshot.Rollback()
	var ReadOnly, Isolation string
	if Err := Snapshot.QueryRowContext(Ctx, "SHOW transaction_read_only").Scan(&ReadOnly); Err != nil || ReadOnly != "on" {
		T.Fatal("snapshot permits writes")
	}
	if Err := Snapshot.QueryRowContext(Ctx, "SHOW transaction_isolation").Scan(&Isolation); Err != nil || Isolation != "repeatable read" {
		T.Fatal("snapshot is inconsistent")
	}
	if _, Err := Snapshot.ExecContext(Ctx, "CREATE TABLE snapshots.forbidden(id int)"); Err == nil {
		T.Fatal("snapshot executed DDL")
	}
	_ = Snapshot.Rollback()
	Generator := PostgresGenerator{}
	Cancelled, Cancel := Context.WithCancel(Ctx)
	Cancel()
	if _, Err := Generator.Generate(Cancelled, CurrentDSN, ReferenceDSN, "snapshots"); !Errors.Is(Err, ErrUnavailable) {
		T.Fatal("cancelled comparison accepted")
	}
	var Wait Sync.WaitGroup
	Results := make(chan error, 8)
	for Index := 0; Index < 8; Index++ {
		Wait.Go(func() {
			Result, Err := Generator.Generate(Ctx, CurrentDSN, ReferenceDSN, "snapshots")
			if Err == nil && len(Result.Statements) != 0 {
				Err = Errors.New("concurrent comparison changed schema")
			}
			Results <- Err
		})
	}
	Wait.Wait()
	close(Results)
	for Err := range Results {
		if Err != nil {
			T.Fatal(Err)
		}
	}
	Check, CancelCheck := Context.WithTimeout(Ctx, 5*Time.Second)
	defer CancelCheck()
	var Connections int
	if Err := Platform.QueryRow(Check, "SELECT count(*) FROM pg_stat_activity WHERE datname=ANY($1) AND backend_type='client backend' AND state LIKE 'idle in transaction%'", []string{Current.Config().ConnConfig.Database, Reference.Config().ConnConfig.Database}).Scan(&Connections); Err != nil || Connections != 0 {
		T.Fatal("comparison leaked snapshot transactions")
	}
}

func TestGeneratedConstraintFailureRollsBackAndCanBeReviewed(T *Testing.T) {
	Platform := Testdb.PlatformDatabase(T)
	CurrentDSN, Current := ComparisonDatabase(T, Platform)
	ReferenceDSN, Reference := ComparisonDatabase(T, Platform)
	Ctx := Context.Background()
	if _, Err := Current.Exec(Ctx, "CREATE SCHEMA review; CREATE TABLE review.items(id int PRIMARY KEY,value text); INSERT INTO review.items VALUES (1,NULL)"); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Reference.Exec(Ctx, "CREATE SCHEMA review; CREATE TABLE review.items(id int PRIMARY KEY,value text NOT NULL,enabled boolean DEFAULT true); CREATE INDEX value_index ON review.items(value)"); Err != nil {
		T.Fatal(Err)
	}
	Generator := PostgresGenerator{}
	Before, Err := Generator.Generate(Ctx, CurrentDSN, ReferenceDSN, "review")
	if Err != nil {
		T.Fatal(Err)
	}
	if Err := ApplyCandidate(T, CurrentDSN, Before.SQLText); !Errors.Is(Err, Driver.ErrExecution) {
		T.Fatal("incompatible row data did not reject the generated constraint")
	}
	Failed, Err := Generator.Generate(Ctx, CurrentDSN, ReferenceDSN, "review")
	if Err != nil || Failed.CurrentSchemaHash != Before.CurrentSchemaHash {
		T.Fatal("failed generated migration left partial schema changes")
	}
	var NullCount int
	if Err := Current.QueryRow(Ctx, "SELECT count(*) FROM review.items WHERE value IS NULL").Scan(&NullCount); Err != nil || NullCount != 1 {
		T.Fatal("failed migration changed existing data")
	}
	Reviewed := "UPDATE review.items SET value='reviewed' WHERE value IS NULL;\n" + Failed.SQLText
	if Err := ApplyCandidate(T, CurrentDSN, Reviewed); Err != nil {
		T.Fatal("reviewed backfill did not fix the migration")
	}
	After, Err := Generator.Generate(Ctx, CurrentDSN, ReferenceDSN, "review")
	if Err != nil || len(After.Statements) != 0 {
		T.Fatal("reviewed migration did not converge")
	}
}

func TestComparisonUsesSelectOnlyTargetCredentials(T *Testing.T) {
	Platform := Testdb.PlatformDatabase(T)
	CurrentDSN, Current := ComparisonDatabase(T, Platform)
	ReferenceDSN, Reference := ComparisonDatabase(T, Platform)
	Ctx := Context.Background()
	ID, Err := Auth.RandomID()
	if Err != nil {
		T.Fatal(Err)
	}
	RoleName := "diff_reader_" + Strings.ReplaceAll(ID, "-", "")
	Role := Pgx.Identifier{RoleName}.Sanitize()
	Password := "isolated comparison reader password"
	if _, Err := Platform.Exec(Ctx, "CREATE ROLE "+Role+" LOGIN PASSWORD 'isolated comparison reader password'"); Err != nil {
		T.Fatal("cannot create isolated read-only role")
	}
	T.Cleanup(func() {
		Cleanup, Cancel := Context.WithTimeout(Context.Background(), 5*Time.Second)
		defer Cancel()
		for _, Pool := range []*Pgxpool.Pool{Current, Reference} {
			if _, Err := Pool.Exec(Cleanup, "DROP OWNED BY "+Role); Err != nil {
				T.Error("cannot remove isolated role grants")
			}
		}
		if _, Err := Platform.Exec(Cleanup, "DROP ROLE "+Role); Err != nil {
			T.Error("cannot remove isolated reader role")
		}
	})
	for Index, Pool := range []*Pgxpool.Pool{Current, Reference} {
		SQL := "CREATE SCHEMA restricted; CREATE TABLE restricted.items(id int PRIMARY KEY"
		if Index == 1 {
			SQL += ",value text"
		}
		SQL += "); GRANT USAGE ON SCHEMA restricted TO " + Role + "; GRANT SELECT ON ALL TABLES IN SCHEMA restricted TO " + Role
		if _, Err := Pool.Exec(Ctx, SQL); Err != nil {
			T.Fatal(Err)
		}
	}
	ReadOnlyDSNs := []string{}
	for _, DSN := range []string{CurrentDSN, ReferenceDSN} {
		Parsed, Err := Url.Parse(DSN)
		if Err != nil {
			T.Fatal("invalid fixture DSN")
		}
		Parsed.User = Url.UserPassword(RoleName, Password)
		ReadOnlyDSNs = append(ReadOnlyDSNs, Parsed.String())
	}
	Result, Err := (PostgresGenerator{}).Generate(Ctx, ReadOnlyDSNs[0], ReadOnlyDSNs[1], "restricted")
	if Err != nil || !Strings.Contains(Result.SQLText, "ADD COLUMN") {
		T.Fatalf("select-only credentials cannot compare catalog: %v", Err)
	}
	if Err := ApplyCandidate(T, ReadOnlyDSNs[0], Result.SQLText); !Errors.Is(Err, Driver.ErrExecution) {
		T.Fatal("select-only target credentials unexpectedly altered schema")
	}
}

func TestUnsupportedTypeDifferencesAreNotReportedAsEqual(T *Testing.T) {
	Platform := Testdb.PlatformDatabase(T)
	CurrentDSN, Current := ComparisonDatabase(T, Platform)
	ReferenceDSN, Reference := ComparisonDatabase(T, Platform)
	Ctx := Context.Background()
	for _, Case := range []struct{ Name, Current, Reference string }{
		{"domain", "CREATE DOMAIN {{schema}}.value AS text CHECK(length(VALUE)<5)", "CREATE DOMAIN {{schema}}.value AS text CHECK(length(VALUE)<10)"},
		{"composite", "CREATE TYPE {{schema}}.value AS (id integer)", "CREATE TYPE {{schema}}.value AS (id integer,name text)"},
		{"range", "CREATE TYPE {{schema}}.value AS RANGE (subtype=int4)", "CREATE TYPE {{schema}}.value AS RANGE (subtype=int8)"},
		{"enum_order", "CREATE TYPE {{schema}}.value AS ENUM ('new','done')", "CREATE TYPE {{schema}}.value AS ENUM ('done','new')"},
		{"enum_removal", "CREATE TYPE {{schema}}.value AS ENUM ('new','done'); CREATE TABLE {{schema}}.items(state {{schema}}.value)", "CREATE TYPE {{schema}}.value AS ENUM ('new'); CREATE TABLE {{schema}}.items(state {{schema}}.value)"},
	} {
		T.Run(Case.Name, func(T *Testing.T) {
			Schema := "unsupported_" + Case.Name
			for _, Side := range []struct {
				Pool *Pgxpool.Pool
				SQL  string
			}{{Current, Case.Current}, {Reference, Case.Reference}} {
				if _, Err := Side.Pool.Exec(Ctx, "CREATE SCHEMA "+Pgx.Identifier{Schema}.Sanitize()+"; "+Strings.ReplaceAll(Side.SQL, "{{schema}}", Pgx.Identifier{Schema}.Sanitize())); Err != nil {
					T.Fatal(Err)
				}
			}
			if _, Err := (PostgresGenerator{}).Generate(Ctx, CurrentDSN, ReferenceDSN, Schema); !Errors.Is(Err, ErrUnsupported) {
				T.Fatalf("unsupported type difference must return an explicit error, got %v", Err)
			}
		})
	}
}
