package Schemadiff

import (
	Context "context"
	Sql "database/sql"
	Json "encoding/json"
	Errors "errors"
	Pgx "github.com/jackc/pgx/v5"
	Stdlib "github.com/jackc/pgx/v5/stdlib"
	Diff "github.com/stripe/pg-schema-diff/pkg/diff"
	Regexp "regexp"
	Slices "slices"
	Strings "strings"
	Time "time"
)

var EnumAddition = Regexp.MustCompile(`^ALTER TYPE "(?:""|[^"])*"\."(?:""|[^"])*" ADD VALUE `)

var TypeRemoval = Regexp.MustCompile(`^DROP TYPE "(?:""|[^"])*"\."(?:""|[^"])*"$`)

type PostgresGenerator struct{}
type SilentLogger struct{}

func (SilentLogger) Errorf(string, ...any) {}
func (SilentLogger) Warnf(string, ...any)  {}

func OpenSnapshot(Ctx Context.Context, DSN string) (*Sql.DB, *Sql.Tx, error) {
	Cfg, Err := Pgx.ParseConfig(DSN)
	if Err != nil {
		return nil, nil, ErrUnavailable
	}
	Cfg.ConnectTimeout = 3 * Time.Second
	DB := Stdlib.OpenDB(*Cfg)
	DB.SetMaxOpenConns(2)
	Tx, Err := DB.BeginTx(Ctx, &Sql.TxOptions{Isolation: Sql.LevelRepeatableRead, ReadOnly: true})
	if Err != nil {
		_ = DB.Close()
		return nil, nil, ErrUnavailable
	}
	if _, Err := Tx.ExecContext(Ctx, "SET LOCAL statement_timeout = '15s'"); Err != nil {
		_ = Tx.Rollback()
		_ = DB.Close()
		return nil, nil, ErrUnavailable
	}
	return DB, Tx, nil
}

func ReadEnumLabels(Ctx Context.Context, Snapshot *Sql.Tx, Schema string) (map[string][]string, error) {
	Rows, Err := Snapshot.QueryContext(Ctx, `SELECT T.typname,E.enumlabel
FROM pg_catalog.pg_type T
JOIN pg_catalog.pg_namespace N ON N.oid=T.typnamespace
JOIN pg_catalog.pg_enum E ON E.enumtypid=T.oid
WHERE N.nspname=$1 ORDER BY T.typname,E.enumsortorder`, Schema)
	if Err != nil {
		return nil, ErrUnavailable
	}
	defer Rows.Close()
	Labels := map[string][]string{}
	for Rows.Next() {
		var Name, Label string
		if Err := Rows.Scan(&Name, &Label); Err != nil {
			return nil, ErrUnavailable
		}
		Labels[Name] = append(Labels[Name], Label)
	}
	if Err := Rows.Err(); Err != nil {
		return nil, ErrUnavailable
	}
	return Labels, nil
}

func (PostgresGenerator) Generate(Ctx Context.Context, CurrentDSN, ReferenceDSN, Schema string) (Result, error) {
	CurrentDB, Current, Err := OpenSnapshot(Ctx, CurrentDSN)
	if Err != nil {
		return Result{}, Err
	}
	defer CurrentDB.Close()
	defer Current.Rollback()
	ReferenceDB, Reference, Err := OpenSnapshot(Ctx, ReferenceDSN)
	if Err != nil {
		return Result{}, Err
	}
	defer ReferenceDB.Close()
	defer Reference.Rollback()
	var Exists bool
	if Err := Reference.QueryRowContext(Ctx, "SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)", Schema).Scan(&Exists); Err != nil {
		return Result{}, ErrUnavailable
	}
	if !Exists {
		return Result{}, ErrInvalidInput
	}
	var PreviousEnums map[string][]string
	for _, Snapshot := range []*Sql.Tx{Current, Reference} {
		var Unsupported bool
		if Err := Snapshot.QueryRowContext(Ctx, `SELECT EXISTS (
SELECT 1 FROM pg_catalog.pg_type T
JOIN pg_catalog.pg_namespace N ON N.oid=T.typnamespace
LEFT JOIN pg_catalog.pg_class C ON C.oid=T.typrelid
WHERE N.nspname=$1
AND (T.typtype IN ('d','r','m') OR (T.typtype='c' AND C.relkind='c') OR (T.typtype='b' AND T.typelem=0))
AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_depend D WHERE D.classid='pg_catalog.pg_type'::regclass AND D.objid=T.oid AND D.deptype='e')
)`, Schema).Scan(&Unsupported); Err != nil {
			return Result{}, ErrUnavailable
		}
		if Unsupported {
			return Result{}, ErrUnsupported
		}
		Enums, Err := ReadEnumLabels(Ctx, Snapshot, Schema)
		if Err != nil {
			return Result{}, Err
		}
		for Name, Old := range PreviousEnums {
			Desired, Exists := Enums[Name]
			if !Exists {
				continue
			}
			OldValues := map[string]bool{}
			for _, Label := range Old {
				OldValues[Label] = true
			}
			Retained := []string{}
			for _, Label := range Desired {
				if OldValues[Label] {
					Retained = append(Retained, Label)
				}
			}
			if !Slices.Equal(Old, Retained) {
				return Result{}, ErrUnsupported
			}
		}
		PreviousEnums = Enums
	}
	Plan, Err := Diff.Generate(Ctx, Diff.DBSchemaSource(Current), Diff.DBSchemaSource(Reference), Diff.WithIncludeSchemas(Schema), Diff.WithDoNotValidatePlan(), Diff.WithNoConcurrentIndexOps(), Diff.WithLogger(SilentLogger{}))
	if Errors.Is(Err, Diff.ErrNotImplemented) {
		return Result{}, ErrUnsupported
	}
	if Err != nil {
		return Result{}, ErrUnavailable
	}
	return FormatPlan(Plan)
}

func FormatPlan(Plan Diff.Plan) (Result, error) {
	Output := Result{CurrentSchemaHash: Plan.CurrentSchemaHash, Statements: make([]Statement, 0, len(Plan.Statements))}
	SQL := make([]string, 0, len(Plan.Statements))
	EnumSQL := []string{}
	for _, Item := range Plan.Statements {
		if TypeRemoval.MatchString(Item.DDL) {
			return Result{}, ErrUnsupported
		}
		if EnumAddition.MatchString(Item.DDL) {
			EnumSQL = append(EnumSQL, Item.ToSQL())
		}
		Statement := Statement{SQL: Item.ToSQL(), Hazards: make([]Hazard, 0, len(Item.Hazards))}
		for _, Risk := range Item.Hazards {
			Statement.Hazards = append(Statement.Hazards, Hazard{Type: Risk.Type, Message: Risk.Message})
		}
		Output.Statements = append(Output.Statements, Statement)
		SQL = append(SQL, Statement.SQL)
	}
	if len(EnumSQL) > 0 && len(EnumSQL) < len(SQL) {
		SQL = EnumSQL
		Output.RequiresRecomparison = true
	}
	Output.SQLText = Strings.Join(SQL, "\n\n")
	Encoded, Err := Json.Marshal(Output.SQLText)
	if Err != nil || len(Output.SQLText) > MaxSQLBytes || len(Encoded) > 60000 {
		return Result{}, ErrTooLarge
	}
	return Output, nil
}
