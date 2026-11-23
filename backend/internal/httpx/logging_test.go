package Httpx

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
	Otel "go.opentelemetry.io/otel"
	Sdktrace "go.opentelemetry.io/otel/sdk/trace"
	Tracetest "go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestRouterTracesAndLogsAPIRequestsOnly(T *Testing.T) {
	Recorder := Tracetest.NewSpanRecorder()
	PreviousProvider := Otel.GetTracerProvider()
	Otel.SetTracerProvider(Sdktrace.NewTracerProvider(Sdktrace.WithSpanProcessor(Recorder)))
	Buffer := &Bytes.Buffer{}
	PreviousLogger := Slog.Default()
	Slog.SetDefault(Logging.New(Buffer, Slog.LevelInfo))
	T.Cleanup(func() {
		Otel.SetTracerProvider(PreviousProvider)
		Slog.SetDefault(PreviousLogger)
	})
	Router := NewRouter(func(Context.Context) error { return nil }, Http.HandlerFunc(func(W Http.ResponseWriter, R *Http.Request) {
		_, _ = W.Write([]byte("frontend"))
	}), func(Api Chi.Router) {
		Api.Get("/items/{id}", func(W Http.ResponseWriter, R *Http.Request) { JSON(W, 200, map[string]bool{"ok": true}) })
	})
	Router.ServeHTTP(Httptest.NewRecorder(), Httptest.NewRequest("GET", "/api/items/7", nil))
	Router.ServeHTTP(Httptest.NewRecorder(), Httptest.NewRequest("GET", "/assets/app.js", nil))

	Spans := Recorder.Ended()
	if len(Spans) != 1 || Spans[0].Name() != "HTTP GET /api/items/{id}" {
		T.Fatalf("expected exactly one API server span, got %d", len(Spans))
	}
	Lines := Strings.Split(Strings.TrimSpace(Buffer.String()), "\n")
	if len(Lines) != 1 {
		T.Fatalf("static frontend requests must not be logged: %v", Lines)
	}
	var Record map[string]any
	if Err := Json.Unmarshal([]byte(Lines[0]), &Record); Err != nil {
		T.Fatal(Err)
	}
	if Record["event"] != "http.request" || Record["route"] != "/api/items/{id}" || Record["request_id"] == "" ||
		Record["trace_id"] != Spans[0].SpanContext().TraceID().String() {
		T.Fatalf("API request log is not correlated with its span: %v", Record)
	}
}
