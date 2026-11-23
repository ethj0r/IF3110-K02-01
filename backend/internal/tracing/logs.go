package Tracing

import (
	Context "context"
	Errors "errors"
	Fmt "fmt"
	Slog "log/slog"
	Os "os"
	Strings "strings"

	Otelslog "go.opentelemetry.io/contrib/bridges/otelslog"
	Attribute "go.opentelemetry.io/otel/attribute"
	Otlploghttp "go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	Sdklog "go.opentelemetry.io/otel/sdk/log"
	Resource "go.opentelemetry.io/otel/sdk/resource"
)

// LogExporterFromEnv reads OTEL_LOGS_EXPORTER ("otlp" or "none", default "none").
func LogExporterFromEnv() string {
	Value := Strings.ToLower(Strings.TrimSpace(Os.Getenv("OTEL_LOGS_EXPORTER")))
	if Value == "" {
		return "none"
	}
	return Value
}

// SetupLogs returns a slog handler that ships records to the OpenTelemetry
// Collector over OTLP/HTTP (OTEL_EXPORTER_OTLP_* endpoint), carrying the trace and
// span IDs from the record context. It returns a nil handler for "none".
func SetupLogs(Ctx Context.Context, ServiceName, Exporter string) (Slog.Handler, func(Context.Context) error, error) {
	NoOp := func(Context.Context) error { return nil }
	switch Exporter {
	case "none", "off", "":
		return nil, NoOp, nil
	case "otlp":
	default:
		return nil, nil, Fmt.Errorf("unsupported OTEL_LOGS_EXPORTER %q", Exporter)
	}
	Client, Err := Otlploghttp.New(Ctx)
	if Err != nil {
		return nil, nil, Errors.New("cannot initialize log exporter")
	}
	return newLogHandler(ServiceName, Client)
}

func newLogHandler(ServiceName string, Exporter Sdklog.Exporter) (Slog.Handler, func(Context.Context) error, error) {
	Info, Err := Resource.Merge(Resource.Default(), Resource.NewSchemaless(Attribute.String("service.name", ServiceName)))
	if Err != nil {
		return nil, nil, Errors.New("cannot build log resource")
	}
	Provider := Sdklog.NewLoggerProvider(
		Sdklog.WithProcessor(Sdklog.NewBatchProcessor(Exporter)),
		Sdklog.WithResource(Info),
	)
	return Otelslog.NewHandler(InstrumentationName, Otelslog.WithLoggerProvider(Provider)), Provider.Shutdown, nil
}
