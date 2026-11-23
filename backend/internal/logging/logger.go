package Logging

import (
	Context "context"
	Io "io"
	Slog "log/slog"
	Strings "strings"

	Trace "go.opentelemetry.io/otel/trace"
)

// New returns a JSON logger that adds request/migration context attributes to
// every record and redacts credentials, session identifiers, and DSN secrets.
// Extra handlers (for example an OTLP log exporter) receive the same records
// after the same level filter and redaction.
func New(W Io.Writer, Level Slog.Leveler, Extra ...Slog.Handler) *Slog.Logger {
	var Inner Slog.Handler = Slog.NewJSONHandler(W, &Slog.HandlerOptions{Level: Level, ReplaceAttr: RedactAttr})
	Handlers := fanout{Inner}
	for _, Handler := range Extra {
		if Handler != nil {
			Handlers = append(Handlers, &redacting{Inner: Handler, Level: Level})
		}
	}
	if len(Handlers) > 1 {
		Inner = Handlers
	}
	return Slog.New(&ContextHandler{Inner: Inner})
}

// ParseLevel maps LOG_LEVEL values to slog levels. Unknown values fall back to info.
func ParseLevel(Value string) Slog.Level {
	switch Strings.ToLower(Strings.TrimSpace(Value)) {
	case "debug":
		return Slog.LevelDebug
	case "warn", "warning":
		return Slog.LevelWarn
	case "error":
		return Slog.LevelError
	default:
		return Slog.LevelInfo
	}
}

// ContextHandler copies attributes stored in the context (see With and Annotate)
// onto each record before delegating to the wrapped handler.
type ContextHandler struct {
	Inner Slog.Handler
}

func (Handler *ContextHandler) Enabled(Ctx Context.Context, Level Slog.Level) bool {
	return Handler.Inner.Enabled(Ctx, Level)
}

func (Handler *ContextHandler) Handle(Ctx Context.Context, Record Slog.Record) error {
	if Ctx != nil {
		if Span := Trace.SpanContextFromContext(Ctx); Span.IsValid() {
			Record.AddAttrs(Slog.String("trace_id", Span.TraceID().String()), Slog.String("span_id", Span.SpanID().String()))
		}
		if Attrs := Attrs(Ctx); len(Attrs) > 0 {
			Present := map[string]bool{}
			Record.Attrs(func(Attr Slog.Attr) bool {
				Present[Attr.Key] = true
				return true
			})
			for _, Attr := range Attrs {
				if !Present[Attr.Key] {
					Record.AddAttrs(Attr)
				}
			}
		}
	}
	return Handler.Inner.Handle(Ctx, Record)
}

func (Handler *ContextHandler) WithAttrs(Attrs []Slog.Attr) Slog.Handler {
	return &ContextHandler{Inner: Handler.Inner.WithAttrs(Attrs)}
}

func (Handler *ContextHandler) WithGroup(Name string) Slog.Handler {
	return &ContextHandler{Inner: Handler.Inner.WithGroup(Name)}
}
