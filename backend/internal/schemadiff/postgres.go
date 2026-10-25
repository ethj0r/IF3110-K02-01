package Schemadiff

import (
	Context "context"
	Sql "database/sql"
	Json "encoding/json"
	Errors "errors"
	Pgx "github.com/jackc/pgx/v5"
	Stdlib "github.com/jackc/pgx/v5/stdlib"
	Diff "github.com/stripe/pg-schema-diff/pkg/diff"
	Strings "strings"
	Time "time"
)

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
	for _, Item := range Plan.Statements {
		Statement := Statement{SQL: Item.ToSQL(), Hazards: make([]Hazard, 0, len(Item.Hazards))}
		for _, Risk := range Item.Hazards {
			Statement.Hazards = append(Statement.Hazards, Hazard{Type: Risk.Type, Message: Risk.Message})
		}
		Output.Statements = append(Output.Statements, Statement)
		SQL = append(SQL, Statement.SQL)
	}
	Output.SQLText = Strings.Join(SQL, "\n\n")
	Encoded, Err := Json.Marshal(Output.SQLText)
	if Err != nil || len(Output.SQLText) > MaxSQLBytes || len(Encoded) > 60000 {
		return Result{}, ErrTooLarge
	}
	return Output, nil
}
