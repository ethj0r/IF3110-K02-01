package Tracing

import (
	Context "context"
	Slog "log/slog"
	Sync "sync"
	Testing "testing"

	Logging "github.com/ethj0r/IF3110-K02-01/backend/internal/logging"
	Attribute "go.opentelemetry.io/otel/attribute"
	Sdklog "go.opentelemetry.io/otel/sdk/log"
)

type memoryExporter struct {
	Mutex   Sync.Mutex
	Records []Sdklog.Record
}

func (Exporter *memoryExporter) Export(_ Context.Context, Records []Sdklog.Record) error {
	Exporter.Mutex.Lock()
	defer Exporter.Mutex.Unlock()
	for _, Record := range Records {
		Exporter.Records = append(Exporter.Records, Record.Clone())
	}
	return nil
}
func (Exporter *memoryExporter) Shutdown(Context.Context) error   { return nil }
func (Exporter *memoryExporter) ForceFlush(Context.Context) error { return nil }

func TestLogHandlerShipsRecordsWithTraceContext(T *Testing.T) {
	record(T)
	Exporter := &memoryExporter{}
	Handler, Shutdown, Err := newLogHandler("wbd-test", Exporter)
	if Err != nil {
		T.Fatal(Err)
	}
	Logger := Logging.New(Discard{}, Slog.LevelInfo, Handler)
	Ctx, Span := Start(Context.Background(), "HTTP GET")
	Logger.InfoContext(Ctx, "migration lock acquired", "event", "migration.lock.acquired", "password", "hunter2")
	Span.End()
	if Err := Shutdown(Context.Background()); Err != nil {
		T.Fatal(Err)
	}
	if len(Exporter.Records) != 1 {
		T.Fatalf("expected one exported record, got %d", len(Exporter.Records))
	}
	Shipped := Exporter.Records[0]
	if Shipped.TraceID() != Span.SpanContext().TraceID() || Shipped.SpanID() != Span.SpanContext().SpanID() {
		T.Fatal("exported log record must carry the active trace and span IDs")
	}
	Found := map[string]string{}
	Shipped.WalkAttributes(func(KV Attribute.KeyValue) bool {
		Found[string(KV.Key)] = KV.Value.Emit()
		return true
	})
	if Found["event"] != "migration.lock.acquired" || Found["password"] != Logging.Redacted {
		T.Fatalf("exported attributes are missing or unredacted: %v", Found)
	}
	if Handler, _, Err := SetupLogs(Context.Background(), "x", "none"); Err != nil || Handler != nil {
		T.Fatal("none must disable log export")
	}
	if _, _, Err := SetupLogs(Context.Background(), "x", "kafka"); Err == nil {
		T.Fatal("unknown log exporter must be rejected")
	}
}

type Discard struct{}

func (Discard) Write(P []byte) (int, error) { return len(P), nil }
