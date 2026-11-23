package Logging

import (
	Context "context"
	Errors "errors"
	Slog "log/slog"
	Strings "strings"
	Atomic "sync/atomic"
	Time "time"

	Pgx "github.com/jackc/pgx/v5"
	Pgconn "github.com/jackc/pgx/v5/pgconn"
	Otel "go.opentelemetry.io/otel"
	Attribute "go.opentelemetry.io/otel/attribute"
	Codes "go.opentelemetry.io/otel/codes"
	Trace "go.opentelemetry.io/otel/trace"
)

// SQLTracer logs every statement sent through a pgx connection: one record when it
// starts and one when it finishes. Transaction control statements finish as
// sql.begin, sql.commit, or sql.rollback so commits and rollbacks are separate events.
type SQLTracer struct {
	// Database labels the connection, for example "platform" or "target".
	Database string
	// Logger defaults to slog.Default when nil.
	Logger *Slog.Logger
}

type traceKey struct{}

type traceState struct {
	ID      uint64
	Started Time.Time
	SQL     string
	Event   string
	Span    Trace.Span
}

// SpanInstrumentation names the tracer used for SQL spans.
const SpanInstrumentation = "github.com/ethj0r/IF3110-K02-01/backend/sql"

// Operation returns the upper-case leading SQL keyword, used as the span name.
func Operation(SQL string) string {
	Fields := Strings.Fields(SQL)
	if len(Fields) == 0 {
		return "SQL"
	}
	Word := Strings.ToUpper(Strings.TrimSuffix(Fields[0], ";"))
	if len(Word) > 24 {
		return "SQL"
	}
	return Word
}

var traceSequence Atomic.Uint64

const sqlLimit = 4096

func (Tracer SQLTracer) logger() *Slog.Logger {
	if Tracer.Logger != nil {
		return Tracer.Logger
	}
	return Slog.Default()
}

// StatementEvent classifies a statement into the event name used when it finishes.
func StatementEvent(SQL string) string {
	Fields := Strings.Fields(Strings.ToLower(Strings.TrimSpace(SQL)))
	if len(Fields) == 0 {
		return "sql.finish"
	}
	switch Strings.TrimSuffix(Fields[0], ";") {
	case "begin", "start":
		return "sql.begin"
	case "commit", "end":
		return "sql.commit"
	case "rollback", "abort":
		if len(Fields) > 1 && Fields[1] == "to" {
			return "sql.finish"
		}
		return "sql.rollback"
	}
	return "sql.finish"
}

func (Tracer SQLTracer) TraceQueryStart(Ctx Context.Context, _ *Pgx.Conn, Data Pgx.TraceQueryStartData) Context.Context {
	State := &traceState{ID: traceSequence.Add(1), Started: Time.Now(), SQL: Data.SQL, Event: StatementEvent(Data.SQL)}
	Ctx, State.Span = Otel.Tracer(SpanInstrumentation).Start(Ctx, "sql "+Operation(Data.SQL),
		Trace.WithSpanKind(Trace.SpanKindClient),
		Trace.WithAttributes(
			Attribute.String("db.system.name", "postgresql"),
			Attribute.String("db.namespace", Tracer.Database),
			Attribute.String("db.operation.name", Operation(Data.SQL)),
			Attribute.String("db.query.text", Truncate(RedactString(Data.SQL), sqlLimit)),
			Attribute.Int64("query_id", int64(State.ID)),
		),
	)
	Logger := Tracer.logger()
	if Logger.Enabled(Ctx, Slog.LevelInfo) {
		Attrs := []Slog.Attr{
			Slog.String("event", "sql.start"),
			Slog.String("db", Tracer.Database),
			Slog.Uint64("query_id", State.ID),
			Slog.String("sql", Truncate(Data.SQL, sqlLimit)),
		}
		if Params := SafeParams(Data.SQL, BoundArgs(Data.Args)); len(Params) > 0 {
			Attrs = append(Attrs, Slog.Any("params", Params))
		}
		Logger.LogAttrs(Ctx, Slog.LevelInfo, "sql statement started", Attrs...)
	}
	return Context.WithValue(Ctx, traceKey{}, State)
}

func (Tracer SQLTracer) TraceQueryEnd(Ctx Context.Context, _ *Pgx.Conn, Data Pgx.TraceQueryEndData) {
	State, _ := Ctx.Value(traceKey{}).(*traceState)
	if State == nil {
		State = &traceState{Started: Time.Now(), Event: "sql.finish"}
	}
	Duration := Time.Since(State.Started)
	Attrs := []Slog.Attr{
		Slog.String("event", State.Event),
		Slog.String("db", Tracer.Database),
		Slog.Uint64("query_id", State.ID),
		Slog.Float64("duration_ms", float64(Duration.Microseconds())/1000),
	}
	if State.Event == "sql.finish" {
		Attrs = append(Attrs, Slog.String("sql", Truncate(State.SQL, sqlLimit)))
	}
	Level := Slog.LevelInfo
	Message := "sql statement finished"
	switch State.Event {
	case "sql.begin":
		Message = "sql transaction started"
	case "sql.commit":
		Message = "sql transaction committed"
	case "sql.rollback":
		Message = "sql transaction rolled back"
	}
	if Data.Err != nil {
		Level = Slog.LevelError
		Message = "sql statement failed"
		Attrs = append(Attrs, Slog.String("status", "error"))
		Attrs = append(Attrs, ErrorAttrs(Data.Err)...)
	} else {
		Attrs = append(Attrs, Slog.String("status", "ok"), Slog.Int64("rows", Data.CommandTag.RowsAffected()))
		if Tag := Data.CommandTag.String(); Tag != "" {
			Attrs = append(Attrs, Slog.String("command", Strings.Fields(Tag)[0]))
		}
	}
	Tracer.logger().LogAttrs(Ctx, Level, Message, Attrs...)
	if State.Span != nil {
		if Data.Err != nil {
			Safe := "sql statement failed"
			for _, Attr := range ErrorAttrs(Data.Err) {
				if Attr.Key == "sqlstate" {
					State.Span.SetAttributes(Attribute.String("db.response.status_code", Attr.Value.String()))
					Safe = "SQLSTATE " + Attr.Value.String()
				}
			}
			State.Span.RecordError(Errors.New(Safe))
			State.Span.SetStatus(Codes.Error, Safe)
		} else {
			State.Span.SetAttributes(Attribute.Int64("db.response.rows", Data.CommandTag.RowsAffected()))
		}
		State.Span.End()
	}
}

// BoundArgs drops pgx query options (result formats, exec mode, rewriters) that
// pgx accepts as leading arguments, leaving only values bound to placeholders.
func BoundArgs(Args []any) []any {
	for len(Args) > 0 {
		switch Args[0].(type) {
		case Pgx.QueryResultFormats, Pgx.QueryResultFormatsByOID, Pgx.QueryExecMode, Pgx.QueryRewriter:
			Args = Args[1:]
		default:
			return Args
		}
	}
	return Args
}

// ErrorAttrs describes a database error without leaking credentials: SQLSTATE,
// severity, and the primary message. PostgreSQL detail lines, which can contain
// row values, are omitted.
func ErrorAttrs(Err error) []Slog.Attr {
	var PgErr *Pgconn.PgError
	if Errors.As(Err, &PgErr) {
		Attrs := []Slog.Attr{Slog.String("sqlstate", PgErr.Code), Slog.String("error", RedactString(PgErr.Message))}
		if PgErr.ConstraintName != "" {
			Attrs = append(Attrs, Slog.String("constraint", PgErr.ConstraintName))
		}
		return Attrs
	}
	switch {
	case Errors.Is(Err, Context.Canceled):
		return []Slog.Attr{Slog.String("error", "context canceled")}
	case Errors.Is(Err, Context.DeadlineExceeded):
		return []Slog.Attr{Slog.String("error", "deadline exceeded")}
	}
	return []Slog.Attr{Slog.String("error", RedactString(Err.Error()))}
}
