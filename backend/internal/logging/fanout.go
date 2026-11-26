package Logging

import (
	Context "context"
	Errors "errors"
	Slog "log/slog"
)

// redacting forwards records to another handler (for example an OTLP log
// exporter) after applying the same level filter and redaction as stdout.
type redacting struct {
	Inner Slog.Handler
	Level Slog.Leveler
}

func (Handler *redacting) Enabled(Ctx Context.Context, Level Slog.Level) bool {
	return Level >= Handler.Level.Level() && Handler.Inner.Enabled(Ctx, Level)
}

func (Handler *redacting) Handle(Ctx Context.Context, Record Slog.Record) error {
	Clean := Slog.NewRecord(Record.Time, Record.Level, Record.Message, Record.PC)
	Record.Attrs(func(Attr Slog.Attr) bool {
		Clean.AddAttrs(redactDeep(Attr))
		return true
	})
	return Handler.Inner.Handle(Ctx, Clean)
}

func (Handler *redacting) WithAttrs(Attrs []Slog.Attr) Slog.Handler {
	Clean := make([]Slog.Attr, len(Attrs))
	for Index, Attr := range Attrs {
		Clean[Index] = redactDeep(Attr)
	}
	return &redacting{Inner: Handler.Inner.WithAttrs(Clean), Level: Handler.Level}
}

func (Handler *redacting) WithGroup(Name string) Slog.Handler {
	return &redacting{Inner: Handler.Inner.WithGroup(Name), Level: Handler.Level}
}

func redactDeep(Attr Slog.Attr) Slog.Attr {
	if Attr.Value.Kind() == Slog.KindGroup {
		Members := Attr.Value.Group()
		Clean := make([]any, len(Members))
		for Index, Member := range Members {
			Clean[Index] = redactDeep(Member)
		}
		return Slog.Group(Attr.Key, Clean...)
	}
	return RedactAttr(nil, Attr)
}

// fanout sends each record to every handler that accepts its level.
type fanout []Slog.Handler

func (Handlers fanout) Enabled(Ctx Context.Context, Level Slog.Level) bool {
	for _, Handler := range Handlers {
		if Handler.Enabled(Ctx, Level) {
			return true
		}
	}
	return false
}

func (Handlers fanout) Handle(Ctx Context.Context, Record Slog.Record) error {
	var Result error
	for _, Handler := range Handlers {
		if Handler.Enabled(Ctx, Record.Level) {
			Result = Errors.Join(Result, Handler.Handle(Ctx, Record.Clone()))
		}
	}
	return Result
}

func (Handlers fanout) WithAttrs(Attrs []Slog.Attr) Slog.Handler {
	Next := make(fanout, len(Handlers))
	for Index, Handler := range Handlers {
		Next[Index] = Handler.WithAttrs(Attrs)
	}
	return Next
}

func (Handlers fanout) WithGroup(Name string) Slog.Handler {
	Next := make(fanout, len(Handlers))
	for Index, Handler := range Handlers {
		Next[Index] = Handler.WithGroup(Name)
	}
	return Next
}
