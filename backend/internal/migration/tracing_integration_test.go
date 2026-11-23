//go:build integration

package Migration

import (
	Bytes "bytes"
	Context "context"
	Slog "log/slog"
	Strings "strings"
	Testing "testing"

	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Logging "github.com/ethj0r/IF3110-K02-01/backend/internal/logging"
	Tracing "github.com/ethj0r/IF3110-K02-01/backend/internal/tracing"
	Otel "go.opentelemetry.io/otel"
	Codes "go.opentelemetry.io/otel/codes"
	Sdktrace "go.opentelemetry.io/otel/sdk/trace"
	Tracetest "go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestExecutionIsTracedAndLoggedThroughLock(T *Testing.T) {
	Recorder := Tracetest.NewSpanRecorder()
	PreviousProvider := Otel.GetTracerProvider()
	Otel.SetTracerProvider(Sdktrace.NewTracerProvider(Sdktrace.WithSpanProcessor(Recorder)))
	Buffer := &Bytes.Buffer{}
	Logger := Logging.New(Buffer, Slog.LevelInfo)
	PreviousLogger := Slog.Default()
	Slog.SetDefault(Logger)
	T.Cleanup(func() {
		Otel.SetTracerProvider(PreviousProvider)
		Slog.SetDefault(PreviousLogger)
	})

	PlatformPool := NewMigrationDatabase(T)
	TargetPool := NewMigrationDatabase(T)
	DBID, UserID := setupFixture(T, PlatformPool)
	Editor := Auth.Principal{ID: UserID, Username: "test_migrator"}
	Service := &Service{
		Repo:     &Repository{Pool: PlatformPool},
		Access:   Access.AllowAll(),
		Resolver: DSNResolverFunc(func(Context.Context, string) (string, error) { return "postgres://target-db", nil }),
		Driver:   testTargetDriver{Pool: TargetPool},
		Logger:   Logger,
	}
	Ctx, Request := Tracing.Start(Logging.NewScope(Context.Background(), "request_id", "req-trace"), "HTTP POST /api/databases/{dbID}/migrations/{migrationID}/apply")
	Good, Err := Service.Create(Ctx, Editor, DBID, MigrationInput{Title: "Traced", SQLText: "CREATE TABLE traced_ok (id int)", DownSQL: "DROP TABLE traced_ok"})
	if Err != nil {
		T.Fatal(Err)
	}
	Executed, Err := Service.Execute(Ctx, Editor, DBID, Good.ID)
	if Err != nil || Executed.Status != ExecutionSucceeded {
		T.Fatalf("execution failed: %v", Err)
	}
	Bad, Err := Service.Create(Ctx, Editor, DBID, MigrationInput{Title: "Broken", SQLText: "SELECT 1/0", DownSQL: "SELECT 1"})
	if Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Service.Execute(Ctx, Editor, DBID, Bad.ID); Err == nil {
		T.Fatal("broken migration must fail")
	}
	Request.End()

	ByName := map[string][]Sdktrace.ReadOnlySpan{}
	Parents := map[string]string{}
	for _, Span := range Recorder.Ended() {
		ByName[Span.Name()] = append(ByName[Span.Name()], Span)
		Parents[Span.SpanContext().SpanID().String()] = Span.Parent().SpanID().String()
		if Span.SpanContext().TraceID() != Request.SpanContext().TraceID() {
			T.Fatalf("span %q left the request trace", Span.Name())
		}
	}
	if len(ByName["migration.lock"]) != 2 || len(ByName["migration.execute"]) != 2 || len(ByName["migration.lock.acquire"]) != 4 {
		T.Fatalf("unexpected span counts: lock=%d execute=%d acquire=%d (drafts also take the lock)",
			len(ByName["migration.lock"]), len(ByName["migration.execute"]), len(ByName["migration.lock.acquire"]))
	}
	Lock, Execute := ByName["migration.lock"][0], ByName["migration.execute"][0]
	if Parents[Execute.SpanContext().SpanID().String()] != Lock.SpanContext().SpanID().String() {
		T.Fatal("execute span must be a child of the lock span")
	}
	AcquiredUnderLock := 0
	for _, Acquire := range ByName["migration.lock.acquire"] {
		if Parents[Acquire.SpanContext().SpanID().String()] == Lock.SpanContext().SpanID().String() {
			AcquiredUnderLock++
		}
	}
	if AcquiredUnderLock != 1 {
		T.Fatal("lock wait must be recorded as a child of the lock span")
	}
	if ByName["migration.execute"][1].Status().Code != Codes.Error || ByName["migration.lock"][1].Status().Code != Codes.Error {
		T.Fatal("failed execution must mark execute and lock spans as errors")
	}

	Output := Buffer.String()
	for _, Want := range []string{`"event":"migration.lock.acquired"`, `"event":"migration.lock.released"`, `"wait_ms"`, `"held_ms"`, `"event":"migration.execution.failed"`, `"request_id":"req-trace"`} {
		if !Strings.Contains(Output, Want) {
			T.Fatalf("migration logs are missing %s", Want)
		}
	}
	if !Strings.Contains(Output, `"trace_id":"`+Request.SpanContext().TraceID().String()+`"`) {
		T.Fatal("migration logs are not correlated with the request trace")
	}
}
