package Inspect

import (
	Context "context"
	Errors "errors"
	Connection "github.com/ethj0r/IF3110-K02-01/backend/internal/connection"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
	Pgx "github.com/jackc/pgx/v5"
	Math "math"
	Strings "strings"
	Testing "testing"
)

type ProbeConnector struct {
	Err   error
	Calls int
}

func (Probe *ProbeConnector) DialDSN(Context.Context, string) (Connection.Connection, string, error) {
	Probe.Calls++
	return Connection.Connection{}, "private-target-dsn", Probe.Err
}

type ProbeDriver struct {
	Target Driver.Conn
	Err    error
	Calls  int
}

func (Probe *ProbeDriver) Open(string) (Driver.Conn, error) {
	Probe.Calls++
	return Probe.Target, Probe.Err
}

type ProbeConn struct {
	Driver.Conn
	Rows   Pgx.Rows
	Err    error
	Closed bool
}

func (Probe *ProbeConn) Query(Context.Context, string, ...any) (Pgx.Rows, error) {
	return Probe.Rows, Probe.Err
}

func (Probe *ProbeConn) Close() { Probe.Closed = true }

type ProbeRows struct {
	Pgx.Rows
	Names     []string
	Index     int
	ScanErr   error
	StreamErr error
	Closed    bool
}

func (Probe *ProbeRows) Next() bool {
	if Probe.Index >= len(Probe.Names) {
		return false
	}
	Probe.Index++
	return true
}

func (Probe *ProbeRows) Scan(Values ...any) error {
	if Probe.ScanErr != nil {
		return Probe.ScanErr
	}
	*Values[0].(*string) = Probe.Names[Probe.Index-1]
	return nil
}

func (Probe *ProbeRows) Err() error { return Probe.StreamErr }
func (Probe *ProbeRows) Close()     { Probe.Closed = true }

func TestInvalidInspectionNeverOpensTarget(T *Testing.T) {
	Server := &Service{}
	for _, Case := range []struct{ Schema, Table string }{
		{"bad\x00schema", "items"}, {"public", "bad\x00table"}, {"public", ""},
		{Strings.Repeat("a", 64), "items"}, {"public", Strings.Repeat("表", 22)},
	} {
		if Err := CheckTableRef(Case.Schema, Case.Table); !Errors.Is(Err, ErrInvalidInput) {
			T.Fatal("invalid table reference accepted")
		}
		if _, Err := Server.Columns(Context.Background(), "db", Case.Schema, Case.Table); !Errors.Is(Err, ErrInvalidInput) {
			T.Fatal("columns did not validate before connecting")
		}
		if _, Err := Server.Constraints(Context.Background(), "db", Case.Schema, Case.Table); !Errors.Is(Err, ErrInvalidInput) {
			T.Fatal("constraints did not validate before connecting")
		}
		if _, Err := Server.Indexes(Context.Background(), "db", Case.Schema, Case.Table); !Errors.Is(Err, ErrInvalidInput) {
			T.Fatal("indexes did not validate before connecting")
		}
	}
	for _, Case := range []struct{ Page, Size int }{{0, 25}, {-1, 25}, {1, 0}, {1, 101}, {Math.MaxInt, 100}} {
		if _, Err := Server.Rows(Context.Background(), "db", "public", "items", Case.Page, Case.Size); !Errors.Is(Err, ErrInvalidInput) {
			T.Fatal("invalid pagination reached target")
		}
	}
	if _, Err := Server.Tables(Context.Background(), "db", "bad\x00schema"); !Errors.Is(Err, ErrInvalidInput) {
		T.Fatal("invalid schema reached target")
	}
	for _, Name := range []string{"order items", "1table", "表", `a"b`, "a/b", "a%20b", `x"; DROP TABLE users;--`} {
		if Err := CheckTableRef("", Name); Err != nil {
			T.Fatalf("quoted identifier rejected: %q", Name)
		}
	}
}

func TestTargetErrorsAreSanitized(T *Testing.T) {
	for _, Case := range []struct {
		Name, ID                       string
		ConnectionErr, DriverErr, Want error
	}{
		{"missing id", "", nil, nil, ErrNotFound},
		{"missing connection", "db", Connection.ErrNotFound, nil, ErrNotFound},
		{"store failure", "db", Errors.New("private database password"), nil, ErrUnavailable},
		{"target failure", "db", nil, Errors.New("private target password"), ErrUnavailable},
	} {
		T.Run(Case.Name, func(T *Testing.T) {
			Connector := &ProbeConnector{Err: Case.ConnectionErr}
			Target := &ProbeDriver{Err: Case.DriverErr}
			Server := &Service{Connections: Connector, Driver: Target}
			if _, Err := Server.Schemas(Context.Background(), Case.ID); !Errors.Is(Err, Case.Want) {
				T.Fatalf("got %v, want %v", Err, Case.Want)
			}
			if Case.ID == "" && Connector.Calls != 0 {
				T.Fatal("empty id reached store")
			}
			if Case.ConnectionErr != nil && Target.Calls != 0 {
				T.Fatal("store failure reached driver")
			}
		})
	}
}

func TestSchemasCloseResourcesOnSuccessAndFailure(T *Testing.T) {
	for _, Stage := range []string{"success", "query", "scan", "stream"} {
		T.Run(Stage, func(T *Testing.T) {
			Private := Errors.New("private query detail")
			Rows := &ProbeRows{Names: []string{"public", "表 schema"}}
			Target := &ProbeConn{Rows: Rows}
			switch Stage {
			case "query":
				Target.Err = Private
			case "scan":
				Rows.ScanErr = Private
			case "stream":
				Rows.StreamErr = Private
			}
			Server := &Service{Connections: &ProbeConnector{}, Driver: &ProbeDriver{Target: Target}}
			Names, Err := Server.Schemas(Context.Background(), "db")
			if Stage == "success" {
				if Err != nil || len(Names) != 2 || Names[1] != "表 schema" {
					T.Fatal("schema names changed")
				}
			} else if !Errors.Is(Err, ErrUnavailable) {
				T.Fatal("query error was not sanitized")
			}
			if !Target.Closed || (Stage != "query" && !Rows.Closed) {
				T.Fatal("inspection leaked a connection or result set")
			}
		})
	}
}
