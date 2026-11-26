package Tracing

import (
	Context "context"
	Errors "errors"
	Fmt "fmt"
	Io "io"
	Os "os"
	Strings "strings"
	Time "time"

	Otel "go.opentelemetry.io/otel"
	Attribute "go.opentelemetry.io/otel/attribute"
	Codes "go.opentelemetry.io/otel/codes"
	Otlptracehttp "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	Stdouttrace "go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	Propagation "go.opentelemetry.io/otel/propagation"
	Resource "go.opentelemetry.io/otel/sdk/resource"
	Sdktrace "go.opentelemetry.io/otel/sdk/trace"
	Trace "go.opentelemetry.io/otel/trace"
)

// InstrumentationName identifies spans created by this application.
const InstrumentationName = "github.com/ethj0r/IF3110-K02-01/backend"

// Options selects how spans are exported.
type Options struct {
	ServiceName string
	// Exporter is "console" (JSON spans to Writer), "otlp" (OTLP/HTTP to the
	// standard OTEL_EXPORTER_OTLP_* endpoint), or "none" (IDs only, no export).
	Exporter string
	Writer   Io.Writer
}

// OptionsFromEnv reads OTEL_SERVICE_NAME and OTEL_TRACES_EXPORTER. The console
// exporter writes to stderr so spans never interleave with stdout JSON logs.
func OptionsFromEnv(DefaultService string) Options {
	Service := Strings.TrimSpace(Os.Getenv("OTEL_SERVICE_NAME"))
	if Service == "" {
		Service = DefaultService
	}
	Exporter := Strings.ToLower(Strings.TrimSpace(Os.Getenv("OTEL_TRACES_EXPORTER")))
	if Exporter == "" {
		Exporter = "console"
	}
	return Options{ServiceName: Service, Exporter: Exporter, Writer: Os.Stderr}
}

// Setup installs the global tracer provider and W3C trace-context propagator.
// The returned function flushes and stops the exporter.
func Setup(Ctx Context.Context, Settings Options) (func(Context.Context) error, error) {
	Otel.SetTextMapPropagator(Propagation.NewCompositeTextMapPropagator(Propagation.TraceContext{}, Propagation.Baggage{}))
	var Exporter Sdktrace.SpanExporter
	var Err error
	switch Settings.Exporter {
	case "none", "off":
		// Keep generating trace and span IDs for log correlation and propagation.
		Provider := Sdktrace.NewTracerProvider()
		Otel.SetTracerProvider(Provider)
		return Provider.Shutdown, nil
	case "console", "stdout":
		Writer := Settings.Writer
		if Writer == nil {
			Writer = Os.Stderr
		}
		Exporter, Err = Stdouttrace.New(Stdouttrace.WithWriter(Writer))
	case "otlp":
		Exporter, Err = Otlptracehttp.New(Ctx)
	default:
		return nil, Fmt.Errorf("unsupported OTEL_TRACES_EXPORTER %q", Settings.Exporter)
	}
	if Err != nil {
		return nil, Errors.New("cannot initialize trace exporter")
	}
	Info, Err := Resource.Merge(Resource.Default(), Resource.NewSchemaless(Attribute.String("service.name", Settings.ServiceName)))
	if Err != nil {
		return nil, Errors.New("cannot build trace resource")
	}
	Provider := Sdktrace.NewTracerProvider(
		Sdktrace.WithBatcher(Exporter, Sdktrace.WithBatchTimeout(2*Time.Second)),
		Sdktrace.WithResource(Info),
	)
	Otel.SetTracerProvider(Provider)
	return Provider.Shutdown, nil
}

// Tracer returns the application tracer from the global provider (a no-op until Setup runs).
func Tracer() Trace.Tracer {
	return Otel.Tracer(InstrumentationName)
}

// Start opens a child span of the span in Ctx.
func Start(Ctx Context.Context, Name string, Attrs ...Attribute.KeyValue) (Context.Context, Trace.Span) {
	return Tracer().Start(Ctx, Name, Trace.WithAttributes(Attrs...))
}

// Fail marks a span as failed. The message must already be safe to export.
func Fail(Span Trace.Span, Err error, Message string) {
	if Err == nil {
		return
	}
	Span.RecordError(Errors.New(Message))
	Span.SetStatus(Codes.Error, Message)
}

// Inject serialises the span context of Ctx as a W3C traceparent value, for
// example to store with a queued job. It returns "" when Ctx has no span.
func Inject(Ctx Context.Context) string {
	Carrier := Propagation.MapCarrier{}
	Otel.GetTextMapPropagator().Inject(Ctx, Carrier)
	return Carrier.Get("traceparent")
}

// Extract continues a trace from a stored traceparent value. Invalid or empty
// values leave Ctx unchanged so the worker simply starts a new trace.
func Extract(Ctx Context.Context, TraceParent string) Context.Context {
	if Strings.TrimSpace(TraceParent) == "" {
		return Ctx
	}
	return Otel.GetTextMapPropagator().Extract(Ctx, Propagation.MapCarrier{"traceparent": TraceParent})
}

// IDs returns the trace and span IDs of the span in Ctx, or empty strings.
func IDs(Ctx Context.Context) (string, string) {
	Current := Trace.SpanContextFromContext(Ctx)
	if !Current.IsValid() {
		return "", ""
	}
	return Current.TraceID().String(), Current.SpanID().String()
}
