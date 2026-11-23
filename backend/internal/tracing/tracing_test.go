package Tracing

import (
	Bytes "bytes"
	Context "context"
	Json "encoding/json"
	Slog "log/slog"
	Http "net/http"
	Httptest "net/http/httptest"
	Strings "strings"
	Testing "testing"

	Logging "github.com/ethj0r/IF3110-K02-01/backend/internal/logging"
	Chi "github.com/go-chi/chi/v5"
	Middleware "github.com/go-chi/chi/v5/middleware"
	Otel "go.opentelemetry.io/otel"
	Codes "go.opentelemetry.io/otel/codes"
	Propagation "go.opentelemetry.io/otel/propagation"
	Sdktrace "go.opentelemetry.io/otel/sdk/trace"
	Tracetest "go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func record(T *Testing.T) *Tracetest.SpanRecorder {
	T.Helper()
	Recorder := Tracetest.NewSpanRecorder()
	Previous := Otel.GetTracerProvider()
	Otel.SetTracerProvider(Sdktrace.NewTracerProvider(Sdktrace.WithSpanProcessor(Recorder)))
	Otel.SetTextMapPropagator(Propagation.TraceContext{})
	T.Cleanup(func() { Otel.SetTracerProvider(Previous) })
	return Recorder
}

func lines(T *Testing.T, Buffer *Bytes.Buffer) []map[string]any {
	T.Helper()
	var Result []map[string]any
	for _, Line := range Strings.Split(Strings.TrimSpace(Buffer.String()), "\n") {
		var Record map[string]any
		if Err := Json.Unmarshal([]byte(Line), &Record); Err != nil {
			T.Fatalf("invalid JSON log line %q", Line)
		}
		Result = append(Result, Record)
	}
	return Result
}

func TestHTTPMiddlewareContinuesTraceAndCorrelatesLogs(T *Testing.T) {
	Recorder := record(T)
	Buffer := &Bytes.Buffer{}
	Logger := Logging.New(Buffer, Slog.LevelInfo)
	Router := Chi.NewRouter()
	Router.Use(Middleware.RequestID, HTTPMiddleware, Logging.RequestLogger(Logger))
	Router.Get("/databases/{id}/rows", func(W Http.ResponseWriter, R *Http.Request) {
		Logger.InfoContext(R.Context(), "inside", "event", "test.inside")
		W.WriteHeader(Http.StatusOK)
	})
	Router.Get("/broken", func(W Http.ResponseWriter, R *Http.Request) { W.WriteHeader(Http.StatusBadGateway) })

	const Parent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	Request := Httptest.NewRequest("GET", "/databases/db-1/rows", nil)
	Request.Header.Set("traceparent", Parent)
	Router.ServeHTTP(Httptest.NewRecorder(), Request)
	Router.ServeHTTP(Httptest.NewRecorder(), Httptest.NewRequest("GET", "/broken", nil))

	Spans := Recorder.Ended()
	if len(Spans) != 2 {
		T.Fatalf("expected 2 server spans, got %d", len(Spans))
	}
	Rows := Spans[0]
	if Rows.Name() != "HTTP GET /databases/{id}/rows" {
		T.Fatalf("span name must use the route pattern, got %q", Rows.Name())
	}
	if Rows.SpanContext().TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" || Rows.Parent().SpanID().String() != "00f067aa0ba902b7" {
		T.Fatal("incoming traceparent was not continued")
	}
	if Spans[1].Status().Code != Codes.Error {
		T.Fatal("5xx responses must mark the span as an error")
	}
	for _, Line := range lines(T, Buffer)[:2] {
		if Line["trace_id"] != Rows.SpanContext().TraceID().String() || Line["span_id"] != Rows.SpanContext().SpanID().String() {
			T.Fatalf("log record is not correlated with the request span: %v", Line)
		}
	}
}

func TestInjectExtractRoundTrip(T *Testing.T) {
	record(T)
	if Inject(Context.Background()) != "" {
		T.Fatal("a context without span must not produce a traceparent")
	}
	Ctx, Span := Start(Context.Background(), "api.enqueue")
	Parent := Inject(Ctx)
	Span.End()
	if !Strings.HasPrefix(Parent, "00-") {
		T.Fatalf("unexpected traceparent %q", Parent)
	}
	Worker := Extract(Context.Background(), Parent)
	TraceID, _ := IDs(Ctx)
	_, Child := Start(Worker, "worker.process")
	defer Child.End()
	if Child.SpanContext().TraceID().String() != TraceID {
		T.Fatal("worker span must join the API trace")
	}
	if Extract(Context.Background(), "   ") != Context.Background() {
		T.Fatal("empty traceparent must leave the context unchanged")
	}
	if A, B := IDs(Context.Background()); A != "" || B != "" {
		T.Fatal("IDs of an empty context must be empty")
	}
}

func TestFailMarksSpanWithSafeMessage(T *Testing.T) {
	Recorder := record(T)
	_, Span := Start(Context.Background(), "migration.execute")
	Fail(Span, nil, "ignored")
	Fail(Span, Context.DeadlineExceeded, "migration execution failed")
	Span.End()
	Ended := Recorder.Ended()[0]
	if Ended.Status().Code != Codes.Error || Ended.Status().Description != "migration execution failed" || len(Ended.Events()) != 1 {
		T.Fatalf("unexpected span status %+v", Ended.Status())
	}
}

func TestSetupExporters(T *Testing.T) {
	Previous := Otel.GetTracerProvider()
	T.Cleanup(func() { Otel.SetTracerProvider(Previous) })
	Buffer := &Bytes.Buffer{}
	Shutdown, Err := Setup(Context.Background(), Options{ServiceName: "wbd-test", Exporter: "console", Writer: Buffer})
	if Err != nil {
		T.Fatal(Err)
	}
	_, Span := Start(Context.Background(), "console.span")
	Span.End()
	if Err := Shutdown(Context.Background()); Err != nil {
		T.Fatal(Err)
	}
	if !Strings.Contains(Buffer.String(), `"Name":"console.span"`) || !Strings.Contains(Buffer.String(), "wbd-test") {
		T.Fatalf("console exporter did not write the span with its service name: %s", Buffer.String())
	}
	Shutdown, Err = Setup(Context.Background(), Options{Exporter: "none"})
	if Err != nil {
		T.Fatal("none exporter must be accepted")
	}
	Ctx, Quiet := Start(Context.Background(), "quiet")
	if TraceID, _ := IDs(Ctx); TraceID == "" || Inject(Ctx) == "" {
		T.Fatal("none exporter must still create trace IDs for logs and propagation")
	}
	Quiet.End()
	if Err := Shutdown(Context.Background()); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Setup(Context.Background(), Options{Exporter: "zipkin"}); Err == nil {
		T.Fatal("unknown exporter must be rejected")
	}
	T.Setenv("OTEL_SERVICE_NAME", "")
	T.Setenv("OTEL_TRACES_EXPORTER", "OTLP")
	if Settings := OptionsFromEnv("wbd-api"); Settings.ServiceName != "wbd-api" || Settings.Exporter != "otlp" {
		T.Fatalf("unexpected options %+v", Settings)
	}
}
