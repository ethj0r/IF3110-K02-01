package Logging

import (
	Context "context"
	Slog "log/slog"
	Testing "testing"

	Pgx "github.com/jackc/pgx/v5"
	Pgconn "github.com/jackc/pgx/v5/pgconn"
	Otel "go.opentelemetry.io/otel"
	Codes "go.opentelemetry.io/otel/codes"
	Sdktrace "go.opentelemetry.io/otel/sdk/trace"
	Tracetest "go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestSQLTracerCreatesCorrelatedSpans(T *Testing.T) {
	Recorder := Tracetest.NewSpanRecorder()
	Previous := Otel.GetTracerProvider()
	Otel.SetTracerProvider(Sdktrace.NewTracerProvider(Sdktrace.WithSpanProcessor(Recorder)))
	T.Cleanup(func() { Otel.SetTracerProvider(Previous) })
	Logger, Buffer := capture(Slog.LevelInfo)
	Tracer := SQLTracer{Database: "target", Logger: Logger}
	Parent, Request := Otel.Tracer("test").Start(Context.Background(), "HTTP GET")

	Ctx := Tracer.TraceQueryStart(Parent, nil, Pgx.TraceQueryStartData{SQL: "UPDATE movies SET views = views + 1 WHERE id > $1", Args: []any{0}})
	Tracer.TraceQueryEnd(Ctx, nil, Pgx.TraceQueryEndData{CommandTag: Pgconn.NewCommandTag("UPDATE 5")})
	Ctx = Tracer.TraceQueryStart(Parent, nil, Pgx.TraceQueryStartData{SQL: "ALTER ROLE app PASSWORD 'hunter2'"})
	Tracer.TraceQueryEnd(Ctx, nil, Pgx.TraceQueryEndData{Err: &Pgconn.PgError{Code: "42501", Message: "permission denied"}})
	Request.End()

	Spans := Recorder.Ended()
	if len(Spans) != 3 {
		T.Fatalf("expected 2 SQL spans and the parent, got %d", len(Spans))
	}
	Update, Alter := Spans[0], Spans[1]
	if Update.Name() != "sql UPDATE" || Update.Parent().SpanID() != Request.SpanContext().SpanID() {
		T.Fatalf("SQL span must be a child of the request span: %s", Update.Name())
	}
	Attrs := map[string]string{}
	for _, Attr := range Alter.Attributes() {
		Attrs[string(Attr.Key)] = Attr.Value.Emit()
	}
	if Alter.Status().Code != Codes.Error || Attrs["db.response.status_code"] != "42501" || Attrs["db.system.name"] != "postgresql" {
		T.Fatalf("failed SQL span is missing error data: %v %v", Alter.Status(), Attrs)
	}
	if Attrs["db.query.text"] != "ALTER ROLE app PASSWORD '"+Redacted+"'" {
		T.Fatalf("span query text must be redacted, got %q", Attrs["db.query.text"])
	}
	Lines := records(T, Buffer)
	if Lines[0]["span_id"] != Update.SpanContext().SpanID().String() || Lines[0]["trace_id"] != Request.SpanContext().TraceID().String() {
		T.Fatalf("SQL log must carry the SQL span IDs: %v", Lines[0])
	}
}
