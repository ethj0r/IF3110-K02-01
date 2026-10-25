package Schemadiff

import (
	Context "context"
	Errors "errors"
	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Connection "github.com/ethj0r/IF3110-K02-01/backend/internal/connection"
	Strings "strings"
	Testing "testing"
	Time "time"
)

type ProbeResolver struct {
	Calls []string
	Err   error
}

func (Probe *ProbeResolver) DialDSN(_ Context.Context, ID string) (Connection.Connection, string, error) {
	Probe.Calls = append(Probe.Calls, ID)
	return Connection.Connection{}, ID + "-private-dsn", Probe.Err
}

type ProbeGenerator struct {
	Calls int
	Args  []string
	Err   error
}

func (Probe *ProbeGenerator) Generate(Ctx Context.Context, Current, Reference, Schema string) (Result, error) {
	Probe.Calls++
	Probe.Args = []string{Current, Reference, Schema}
	if Deadline, Ok := Ctx.Deadline(); !Ok || Time.Until(Deadline) > 20*Time.Second {
		return Result{}, Errors.New("comparison has no deadline")
	}
	return Result{SQLText: "SELECT 1;", Statements: []Statement{{SQL: "SELECT 1;", Hazards: []Hazard{}}}}, Probe.Err
}

func TestSchemaComparisonRequiresBothDatabasePermissions(T *Testing.T) {
	for _, Case := range []struct {
		Name               string
		Current, Reference Access.Level
		Admin              bool
		Allow              bool
	}{
		{"no access", Access.LevelNone, Access.LevelNone, false, false},
		{"view-only current", Access.LevelViewOnly, Access.LevelViewOnly, false, false},
		{"missing reference grant", Access.LevelViewEdit, Access.LevelNone, false, false},
		{"admin without grants", Access.LevelNone, Access.LevelNone, true, false},
		{"editor with reference view", Access.LevelViewEdit, Access.LevelViewOnly, false, true},
	} {
		T.Run(Case.Name, func(T *Testing.T) {
			Resolver, Generator := &ProbeResolver{}, &ProbeGenerator{}
			Policy := Access.StaticEvaluator{Grants: map[string]map[string]Access.Level{"user": {"current": Case.Current, "reference": Case.Reference}}}
			Server := &Service{Policy: Policy, Connections: Resolver, Generator: Generator}
			Got, Err := Server.Compare(Context.Background(), Auth.Principal{ID: "user", IsPlatformAdmin: Case.Admin}, " current ", Input{ReferenceDatabaseID: " reference "})
			if !Case.Allow {
				if !Errors.Is(Err, ErrForbidden) || len(Resolver.Calls) != 0 || Generator.Calls != 0 {
					T.Fatal("denied comparison reached target credentials or generator")
				}
				return
			}
			if Err != nil || Got.CurrentDatabaseID != "current" || Got.ReferenceDatabaseID != "reference" || Got.Schema != "public" || Generator.Calls != 1 {
				T.Fatalf("valid comparison failed: %v", Err)
			}
			if Strings.Join(Generator.Args, ",") != "current-private-dsn,reference-private-dsn,public" {
				T.Fatal("comparison direction changed")
			}
		})
	}
}

func TestComparisonRejectsInvalidInputsBeforeOpeningConnections(T *Testing.T) {
	Server := &Service{}
	for _, Input := range []Input{
		{ReferenceDatabaseID: ""},
		{ReferenceDatabaseID: Strings.Repeat("x", 65)},
		{ReferenceDatabaseID: "reference", Schema: "bad\x00name"},
		{ReferenceDatabaseID: "reference", Schema: Strings.Repeat("表", 22)},
		{ReferenceDatabaseID: "reference", Schema: "pg_catalog"},
		{ReferenceDatabaseID: "reference", Schema: "information_schema"},
	} {
		if _, Err := Server.Compare(Context.Background(), Auth.Principal{}, "current", Input); !Errors.Is(Err, ErrInvalidInput) {
			T.Fatal("invalid comparison accepted")
		}
	}
	for _, Name := range []string{"public", "quoted schema", "表", ".", `a"b`} {
		if Err := (Input{ReferenceDatabaseID: "reference", Schema: Name}).Validate(); Err != nil {
			T.Fatal("literal schema name rejected")
		}
	}
}

func TestConnectionFailuresDoNotReachGenerator(T *Testing.T) {
	for _, Case := range []struct{ Source, Want error }{{Connection.ErrNotFound, ErrNotFound}, {Errors.New("private password in connection error"), ErrUnavailable}} {
		Resolver, Generator := &ProbeResolver{Err: Case.Source}, &ProbeGenerator{}
		Server := &Service{Policy: Access.AllowAll(), Connections: Resolver, Generator: Generator}
		if _, Err := Server.Compare(Context.Background(), Auth.Principal{ID: "user"}, "current", Input{ReferenceDatabaseID: "reference"}); !Errors.Is(Err, Case.Want) || Generator.Calls != 0 {
			T.Fatal("connection error leaked or generation continued")
		}
	}
}
