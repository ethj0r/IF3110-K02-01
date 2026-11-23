//go:build integration

package Migration

import (
	Bytes "bytes"
	Context "context"
	Json "encoding/json"
	Slog "log/slog"
	Strings "strings"
	Testing "testing"
	Time "time"

	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Logging "github.com/ethj0r/IF3110-K02-01/backend/internal/logging"
	Tracing "github.com/ethj0r/IF3110-K02-01/backend/internal/tracing"
	Otel "go.opentelemetry.io/otel"
	Propagation "go.opentelemetry.io/otel/propagation"
	Sdktrace "go.opentelemetry.io/otel/sdk/trace"
	Tracetest "go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestQueuedBatchJobJoinsTheEnqueuingTrace(T *Testing.T) {
	Recorder := Tracetest.NewSpanRecorder()
	PreviousProvider := Otel.GetTracerProvider()
	Otel.SetTracerProvider(Sdktrace.NewTracerProvider(Sdktrace.WithSpanProcessor(Recorder)))
	Otel.SetTextMapPropagator(Propagation.TraceContext{})
	Buffer := &Bytes.Buffer{}
	PreviousLogger := Slog.Default()
	Slog.SetDefault(Logging.New(Buffer, Slog.LevelInfo))
	T.Cleanup(func() {
		Otel.SetTracerProvider(PreviousProvider)
		Slog.SetDefault(PreviousLogger)
	})

	Platform := NewMigrationDatabase(T)
	Target := NewMigrationDatabase(T)
	DBID, UserID := setupFixture(T, Platform)
	User := Auth.Principal{ID: UserID, Username: "test_migrator"}
	Repo := &Repository{Pool: Platform}
	Service := &Service{
		Repo:     Repo,
		Access:   Access.AllowAll(),
		Resolver: DSNResolverFunc(func(Context.Context, string) (string, error) { return "target", nil }),
		Driver:   testTargetDriver{Pool: Target},
	}
	if _, Err := Target.Exec(Context.Background(), `CREATE TABLE traced_batch (id BIGINT PRIMARY KEY, value INT NOT NULL);
INSERT INTO traced_batch SELECT g, 0 FROM generate_series(1, 25) AS g`); Err != nil {
		T.Fatal(Err)
	}
	Created, Err := Service.Create(Context.Background(), User, DBID, MigrationInput{
		Title:       "Traced batch",
		SQLText:     `UPDATE traced_batch SET value = value + 1 WHERE id IN (SELECT id FROM traced_batch WHERE id > $1 ORDER BY id LIMIT $2) RETURNING id`,
		DownSQL:     `UPDATE traced_batch SET value = value - 1 WHERE id > $2 AND id <= $1`,
		BatchMode:   true,
		BatchColumn: "id",
		BatchSize:   10,
	})
	if Err != nil {
		T.Fatal(Err)
	}

	Before := len(Recorder.Ended())
	ApiCtx, ApiSpan := Tracing.Start(Logging.NewScope(Context.Background(), "request_id", "req-queue"), "HTTP POST /api/databases/{dbID}/migrations/{migrationID}/apply")
	Job, Err := Service.Enqueue(ApiCtx, User, DBID, Created.ID, JobApply, Tracing.Inject(ApiCtx))
	ApiSpan.End()
	if Err != nil {
		T.Fatal(Err)
	}
	Claimed, Err := Repo.ClaimJob(Context.Background(), "trace-worker", Time.Minute)
	if Err != nil || Claimed.ID != Job.ID {
		T.Fatalf("claim failed: %v", Err)
	}
	(&Worker{Repo: Repo, Service: Service, ID: "trace-worker", Lease: Time.Minute}).Process(Context.Background(), Claimed)
	if Finished, Err := Repo.GetJob(Context.Background(), DBID, Job.ID); Err != nil || Finished.Status != JobSucceeded {
		T.Fatalf("job did not succeed: %+v %v", Finished, Err)
	}

	TraceID := ApiSpan.SpanContext().TraceID()
	Names := map[string]int{}
	var JobSpan Sdktrace.ReadOnlySpan
	for _, Span := range Recorder.Ended()[Before:] {
		if Span.SpanContext().TraceID() != TraceID {
			T.Fatalf("span %q is not part of the enqueuing trace", Span.Name())
		}
		Names[Span.Name()]++
		if Span.Name() == "migration.job apply" {
			JobSpan = Span
		}
	}
	if JobSpan == nil || JobSpan.Parent().SpanID() != ApiSpan.SpanContext().SpanID() {
		T.Fatal("the worker job span must be a child of the API span stored in traceparent")
	}
	if Names["migration.lock"] != 1 || Names["migration.execute"] != 1 || Names["migration.batch"] < 3 {
		T.Fatalf("missing worker, lock, or batch spans: %v", Names)
	}

	Committed := 0
	for _, Line := range Strings.Split(Strings.TrimSpace(Buffer.String()), "\n") {
		var Record map[string]any
		if Err := Json.Unmarshal([]byte(Line), &Record); Err != nil {
			T.Fatalf("invalid log line %q", Line)
		}
		switch Record["event"] {
		case "migration.job.claimed":
			if _, Ok := Record["queue_wait_ms"].(float64); !Ok || Record["worker_id"] != "trace-worker" || Record["job_id"] != Job.ID {
				T.Fatalf("claim record is missing queue wait or job attributes: %v", Record)
			}
		case "migration.batch.committed":
			Committed++
			if Record["job_id"] != Job.ID || Record["batch"] == nil || Record["trace_id"] != TraceID.String() {
				T.Fatalf("batch record is not correlated with the job and trace: %v", Record)
			}
		case "sql.finish":
			if Record["db"] == "target" && Record["batch"] != nil && Record["job_id"] != Job.ID {
				T.Fatalf("target SQL inside a batch must carry job_id: %v", Record)
			}
		}
	}
	if Committed != 3 {
		T.Fatalf("expected 3 committed batch records, got %d", Committed)
	}
}
