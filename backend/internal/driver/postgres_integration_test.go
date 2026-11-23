//go:build integration

package Driver

import (
	Bytes "bytes"
	Context "context"
	Slog "log/slog"
	Os "os"
	Strings "strings"
	Testing "testing"
	Time "time"

	Logging "github.com/ethj0r/IF3110-K02-01/backend/internal/logging"
)

func TestPostgresDriverTracesTargetStatements(T *Testing.T) {
	DSN := Os.Getenv("TEST_DATABASE_URL")
	if DSN == "" {
		T.Fatal("TEST_DATABASE_URL is required")
	}
	Buffer := &Bytes.Buffer{}
	Target, Err := PostgresDriver{Tracer: Logging.SQLTracer{Database: "target", Logger: Logging.New(Buffer, Slog.LevelInfo)}}.Open(DSN)
	if Err != nil {
		T.Fatal(Err)
	}
	defer Target.Close()
	Ctx, Cancel := Context.WithTimeout(Context.Background(), 5*Time.Second)
	defer Cancel()
	Tx, Err := Target.Begin(Ctx)
	if Err != nil {
		T.Fatal(Err)
	}
	if Err := Tx.ExecScript(Ctx, "CREATE TEMP TABLE traced_driver (id int)"); Err != nil {
		T.Fatal(Err)
	}
	if Err := Tx.Rollback(Ctx); Err != nil {
		T.Fatal(Err)
	}
	Output := Buffer.String()
	for _, Event := range []string{`"event":"sql.begin"`, `"event":"sql.rollback"`, `"command":"DO"`, `"db":"target"`} {
		if !Strings.Contains(Output, Event) {
			T.Fatalf("target driver did not log %s: %s", Event, Output)
		}
	}
	if Strings.Contains(Output, "postgres://") {
		T.Fatal("target DSN leaked into SQL logs")
	}
}
