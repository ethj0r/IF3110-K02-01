package Logging

import (
	Bytes "bytes"
	Context "context"
	Json "encoding/json"
	Errors "errors"
	Slog "log/slog"
	Http "net/http"
	Httptest "net/http/httptest"
	Strings "strings"
	Testing "testing"

	Chi "github.com/go-chi/chi/v5"
	Middleware "github.com/go-chi/chi/v5/middleware"
	Pgx "github.com/jackc/pgx/v5"
	Pgconn "github.com/jackc/pgx/v5/pgconn"
)

func capture(Level Slog.Level) (*Slog.Logger, *Bytes.Buffer) {
	Buffer := &Bytes.Buffer{}
	return New(Buffer, Level), Buffer
}

func records(T *Testing.T, Buffer *Bytes.Buffer) []map[string]any {
	T.Helper()
	var Result []map[string]any
	for _, Line := range Strings.Split(Strings.TrimSpace(Buffer.String()), "\n") {
		if Line == "" {
			continue
		}
		var Record map[string]any
		if Err := Json.Unmarshal([]byte(Line), &Record); Err != nil {
			T.Fatalf("log line is not JSON: %q", Line)
		}
		Result = append(Result, Record)
	}
	return Result
}

func TestLoggerAddsScopeAttributesAndRedactsSecrets(T *Testing.T) {
	Logger, Buffer := capture(Slog.LevelInfo)
	Ctx := NewScope(Context.Background(), "request_id", "req-1")
	Annotate(Ctx, "actor", "alice")
	Child := With(Ctx, "migration_id", "mig-1")
	Logger.InfoContext(Child, "work",
		"event", "test.work",
		"password", "hunter2",
		"csrf_token", "abc",
		"target", "postgres://app:s3cret@db:5432/app?sslmode=disable",
		"error", Errors.New("connect failed: password=hunter2 host=db"),
		"sql", "ALTER ROLE app PASSWORD 'hunter2'",
	)
	Logger.InfoContext(Ctx, "parent")
	Lines := records(T, Buffer)
	if len(Lines) != 2 {
		T.Fatalf("expected 2 records, got %d", len(Lines))
	}
	First := Lines[0]
	for Key, Want := range map[string]string{"request_id": "req-1", "actor": "alice", "migration_id": "mig-1", "password": Redacted, "csrf_token": Redacted} {
		if First[Key] != Want {
			T.Fatalf("%s = %v, want %q", Key, First[Key], Want)
		}
	}
	Raw := Buffer.String()
	if Strings.Contains(Raw, "hunter2") || Strings.Contains(Raw, "s3cret") {
		T.Fatalf("secret leaked into log output: %s", Raw)
	}
	if First["target"] != "postgres://app:"+Redacted+"@db:5432/app?sslmode=disable" {
		T.Fatalf("DSN credentials were not masked: %v", First["target"])
	}
	if _, Leaked := Lines[1]["migration_id"]; Leaked {
		T.Fatal("With modified the parent scope")
	}
	if Lines[1]["actor"] != "alice" {
		T.Fatal("Annotate did not update the shared request scope")
	}
}

func TestAnnotateOnceKeepsFirstValueAndIgnoresMissingScope(T *Testing.T) {
	Ctx := NewScope(Context.Background())
	AnnotateOnce(Ctx, "database_id", "db-1")
	AnnotateOnce(Ctx, "database_id", "db-2")
	Attrs := Attrs(Ctx)
	if len(Attrs) != 1 || Attrs[0].Value.String() != "db-1" {
		T.Fatalf("unexpected attributes: %v", Attrs)
	}
	Annotate(Context.Background(), "actor", "nobody")
	if len(Attrs) != 1 {
		T.Fatal("annotating a context without scope must be a no-op")
	}
}

func TestExplicitAttributesWinOverScope(T *Testing.T) {
	Logger, Buffer := capture(Slog.LevelInfo)
	Ctx := NewScope(Context.Background(), "status", "scope")
	Logger.InfoContext(Ctx, "explicit", "status", "record")
	Raw := Buffer.String()
	if Strings.Count(Raw, `"status"`) != 1 || !Strings.Contains(Raw, `"status":"record"`) {
		T.Fatalf("scope attribute duplicated or overrode record attribute: %s", Raw)
	}
}

func TestParseLevel(T *Testing.T) {
	for Input, Want := range map[string]Slog.Level{"debug": Slog.LevelDebug, " WARN ": Slog.LevelWarn, "error": Slog.LevelError, "": Slog.LevelInfo, "verbose": Slog.LevelInfo} {
		if Got := ParseLevel(Input); Got != Want {
			T.Fatalf("ParseLevel(%q) = %v, want %v", Input, Got, Want)
		}
	}
}

func TestSafeParams(T *Testing.T) {
	Long := Strings.Repeat("x", 300)
	Params := SafeParams("SELECT * FROM movies WHERE id = $1 AND title = $2 AND note = $3 AND raw = $4 AND dsn = $5", []any{42, Long, nil, []byte{1, 2, 3}, "postgres://u:pw@h/db"})
	if Params[0] != "42" || Params[2] != "NULL" || Params[3] != "[3 bytes]" {
		T.Fatalf("unexpected params: %v", Params)
	}
	if !Strings.HasSuffix(Params[1], "…(300 bytes)") || len(Params[1]) > 300 {
		T.Fatalf("long parameter was not truncated: %q", Params[1])
	}
	if Strings.Contains(Params[4], "pw") {
		T.Fatalf("credential leaked in parameter: %q", Params[4])
	}
	for _, SQL := range []string{
		"UPDATE users SET password_hash = $2 WHERE id = $1",
		"SELECT session_id FROM session_identifiers WHERE token_hash = $1",
		"INSERT INTO db_connections (password_ciphertext) VALUES ($1)",
	} {
		for _, Value := range SafeParams(SQL, []any{"plain", 7}) {
			if Value != Redacted {
				T.Fatalf("parameters of %q must be masked, got %v", SQL, Value)
			}
		}
	}
	if SafeParams("SELECT 1", nil) != nil {
		T.Fatal("no parameters should produce nil")
	}
}

func TestBoundArgsDropsPgxOptions(T *Testing.T) {
	Args := BoundArgs([]any{Pgx.QueryResultFormats{Pgx.TextFormatCode}, Pgx.QueryExecModeSimpleProtocol, 2, "x"})
	if len(Args) != 2 || Args[0] != 2 || Args[1] != "x" {
		T.Fatalf("pgx options must not be logged as parameters: %v", Args)
	}
	if len(BoundArgs(nil)) != 0 {
		T.Fatal("nil args must stay empty")
	}
}

func TestStatementEvent(T *Testing.T) {
	for SQL, Want := range map[string]string{
		"begin":                                  "sql.begin",
		"BEGIN ISOLATION LEVEL REPEATABLE READ":  "sql.begin",
		"start transaction read only":            "sql.begin",
		"commit":                                 "sql.commit",
		"COMMIT;":                                "sql.commit",
		"rollback":                               "sql.rollback",
		"ROLLBACK TO SAVEPOINT sp":               "sql.finish",
		"SELECT 1":                               "sql.finish",
		"   ":                                    "sql.finish",
		"UPDATE batch_demo.movies SET views = 1": "sql.finish",
	} {
		if Got := StatementEvent(SQL); Got != Want {
			T.Fatalf("StatementEvent(%q) = %q, want %q", SQL, Got, Want)
		}
	}
}

func TestSQLTracerLogsStartFinishAndFailure(T *Testing.T) {
	Logger, Buffer := capture(Slog.LevelInfo)
	Tracer := SQLTracer{Database: "target", Logger: Logger}
	Ctx := NewScope(Context.Background(), "request_id", "req-9", "execution_id", "exec-1")

	Started := Tracer.TraceQueryStart(Ctx, nil, Pgx.TraceQueryStartData{SQL: "UPDATE movies SET views = views + 1 WHERE id > $1", Args: []any{10}})
	Tracer.TraceQueryEnd(Started, nil, Pgx.TraceQueryEndData{CommandTag: Pgconn.NewCommandTag("UPDATE 3")})

	Committed := Tracer.TraceQueryStart(Ctx, nil, Pgx.TraceQueryStartData{SQL: "commit"})
	Tracer.TraceQueryEnd(Committed, nil, Pgx.TraceQueryEndData{CommandTag: Pgconn.NewCommandTag("COMMIT")})

	Failed := Tracer.TraceQueryStart(Ctx, nil, Pgx.TraceQueryStartData{SQL: "INSERT INTO movies (id) VALUES ($1)", Args: []any{1}})
	Tracer.TraceQueryEnd(Failed, nil, Pgx.TraceQueryEndData{Err: &Pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint", Detail: "Key (id)=(1) already exists.", ConstraintName: "movies_pkey"}})

	Lines := records(T, Buffer)
	if len(Lines) != 6 {
		T.Fatalf("expected 6 records, got %d: %s", len(Lines), Buffer.String())
	}
	Start, Finish, Commit, Failure := Lines[0], Lines[1], Lines[3], Lines[5]
	if Start["event"] != "sql.start" || Start["db"] != "target" || Start["request_id"] != "req-9" || Start["execution_id"] != "exec-1" {
		T.Fatalf("unexpected start record: %v", Start)
	}
	if Params, _ := Start["params"].([]any); len(Params) != 1 || Params[0] != "10" {
		T.Fatalf("parameters missing from start record: %v", Start)
	}
	if Finish["event"] != "sql.finish" || Finish["rows"] != float64(3) || Finish["status"] != "ok" || Finish["command"] != "UPDATE" || Finish["query_id"] != Start["query_id"] {
		T.Fatalf("unexpected finish record: %v", Finish)
	}
	if _, Ok := Finish["duration_ms"].(float64); !Ok {
		T.Fatal("finish record has no duration")
	}
	if Commit["event"] != "sql.commit" || Commit["msg"] != "sql transaction committed" {
		T.Fatalf("commit was not logged as its own event: %v", Commit)
	}
	if Failure["level"] != "ERROR" || Failure["sqlstate"] != "23505" || Failure["constraint"] != "movies_pkey" || Failure["status"] != "error" {
		T.Fatalf("unexpected failure record: %v", Failure)
	}
	if Strings.Contains(Buffer.String(), "already exists") {
		T.Fatal("PostgreSQL detail with row values must not be logged")
	}
}

func TestSQLTracerRespectsLevel(T *Testing.T) {
	Logger, Buffer := capture(Slog.LevelWarn)
	Tracer := SQLTracer{Database: "platform", Logger: Logger}
	Ctx := Tracer.TraceQueryStart(Context.Background(), nil, Pgx.TraceQueryStartData{SQL: "SELECT 1"})
	Tracer.TraceQueryEnd(Ctx, nil, Pgx.TraceQueryEndData{CommandTag: Pgconn.NewCommandTag("SELECT 1")})
	if Buffer.Len() != 0 {
		T.Fatalf("info SQL records must be filtered at warn level: %s", Buffer.String())
	}
	Ctx = Tracer.TraceQueryStart(Context.Background(), nil, Pgx.TraceQueryStartData{SQL: "SELECT broken"})
	Tracer.TraceQueryEnd(Ctx, nil, Pgx.TraceQueryEndData{Err: Context.DeadlineExceeded})
	Lines := records(T, Buffer)
	if len(Lines) != 1 || Lines[0]["error"] != "deadline exceeded" {
		T.Fatalf("failures must still be logged: %v", Lines)
	}
}

func TestRequestLoggerCorrelatesRequestRecords(T *Testing.T) {
	Logger, Buffer := capture(Slog.LevelInfo)
	Router := Chi.NewRouter()
	Router.Use(Middleware.RequestID, RequestLogger(Logger))
	Router.Get("/databases/{dbId}/items", func(W Http.ResponseWriter, R *Http.Request) {
		Annotate(R.Context(), "actor", "alice")
		AnnotateOnce(R.Context(), "database_id", Chi.URLParam(R, "dbId"))
		Logger.InfoContext(R.Context(), "inside handler", "event", "test.inner")
		W.WriteHeader(Http.StatusNotFound)
	})
	Router.Get("/boom", func(Http.ResponseWriter, *Http.Request) { panic("boom") })

	Router.ServeHTTP(Httptest.NewRecorder(), Httptest.NewRequest("GET", "/databases/db-7/items?page=2", nil))
	Lines := records(T, Buffer)
	if len(Lines) != 2 {
		T.Fatalf("expected inner and request records, got %v", Lines)
	}
	Inner, Request := Lines[0], Lines[1]
	if Inner["request_id"] == nil || Inner["request_id"] != Request["request_id"] {
		T.Fatal("records of one request must share request_id")
	}
	if Request["event"] != "http.request" || Request["status"] != float64(404) || Request["level"] != "WARN" ||
		Request["route"] != "/databases/{dbId}/items" || Request["actor"] != "alice" || Request["database_id"] != "db-7" {
		T.Fatalf("unexpected request record: %v", Request)
	}

	Buffer.Reset()
	func() {
		defer func() {
			if recover() == nil {
				T.Fatal("panic must propagate to the recovery middleware")
			}
		}()
		Router.ServeHTTP(Httptest.NewRecorder(), Httptest.NewRequest("GET", "/boom", nil))
	}()
	Lines = records(T, Buffer)
	if len(Lines) != 1 || Lines[0]["status"] != float64(500) || Lines[0]["level"] != "ERROR" {
		T.Fatalf("panicking request must be logged as a 500 error: %v", Lines)
	}
}
