package Logging

import (
	Context "context"
	Fmt "fmt"
	Slog "log/slog"
	Http "net/http"
	Time "time"

	Chi "github.com/go-chi/chi/v5"
	Middleware "github.com/go-chi/chi/v5/middleware"
)

// RequestLogger opens a correlation scope for each API request and writes one
// http.request record when it finishes. Inner layers add actor and database
// attributes to the same scope through Annotate, so they appear on this record
// and on every SQL and application log written while serving the request.
func RequestLogger(Logger *Slog.Logger) func(Http.Handler) Http.Handler {
	return func(Next Http.Handler) Http.Handler {
		return Http.HandlerFunc(func(W Http.ResponseWriter, R *Http.Request) {
			Logger := Logger
			if Logger == nil {
				Logger = Slog.Default()
			}
			Started := Time.Now()
			Ctx := NewScope(R.Context(), "request_id", Middleware.GetReqID(R.Context()))
			Writer := Middleware.NewWrapResponseWriter(W, R.ProtoMajor)
			Request := R.WithContext(Ctx)
			Panicked := true
			defer func() {
				Status := Writer.Status()
				if Status == 0 {
					Status = Http.StatusOK
				}
				if Panicked {
					Status = Http.StatusInternalServerError
				}
				Level := Slog.LevelInfo
				switch {
				case Status >= 500:
					Level = Slog.LevelError
				case Status >= 400:
					Level = Slog.LevelWarn
				}
				Route := R.URL.Path
				if Pattern := Chi.RouteContext(Request.Context()); Pattern != nil && Pattern.RoutePattern() != "" {
					Route = Pattern.RoutePattern()
				}
				Logger.LogAttrs(Ctx, Level, "http request completed",
					Slog.String("event", "http.request"),
					Slog.String("method", R.Method),
					Slog.String("route", SanitizeLogValue(Route)),
					Slog.Int("status", Status),
					Slog.Int("bytes", Writer.BytesWritten()),
					Slog.Float64("duration_ms", float64(Time.Since(Started).Microseconds())/1000),
				)
			}()
			Next.ServeHTTP(Writer, Request)
			Panicked = false
		})
	}
}

// Activity records a named audit-style event after the wrapped route runs:
// outcome "succeeded" (status < 400), "denied" (401/403), or "failed", plus the
// HTTP status and the route's URL parameters (resource IDs) under "target".
// Actor, request, database, and trace attributes come from the request scope.
func Activity(Event string) func(Http.Handler) Http.Handler {
	return func(Next Http.Handler) Http.Handler {
		return Http.HandlerFunc(func(W Http.ResponseWriter, R *Http.Request) {
			Writer, Ok := W.(Middleware.WrapResponseWriter)
			if !Ok {
				Writer = Middleware.NewWrapResponseWriter(W, R.ProtoMajor)
			}
			defer func() {
				Status := Writer.Status()
				if Status == 0 {
					Status = Http.StatusOK
				}
				Outcome, Level := "succeeded", Slog.LevelInfo
				switch {
				case Status == Http.StatusUnauthorized || Status == Http.StatusForbidden:
					Outcome, Level = "denied", Slog.LevelWarn
				case Status >= 400:
					Outcome, Level = "failed", Slog.LevelWarn
				}
				Attrs := []any{"event", Event, "outcome", Outcome, "status", Status}
				if Route := Chi.RouteContext(R.Context()); Route != nil && len(Route.URLParams.Keys) > 0 {
					Target := make([]any, 0, len(Route.URLParams.Keys)*2)
					for Index, Key := range Route.URLParams.Keys {
						if Key != "*" && Index < len(Route.URLParams.Values) {
							Target = append(Target, Slog.String(Key, Route.URLParams.Values[Index]))
						}
					}
					if len(Target) > 0 {
						Attrs = append(Attrs, Slog.Group("target", Target...))
					}
				}
				Slog.Default().Log(R.Context(), Level, Event, Attrs...)
			}()
			Next.ServeHTTP(Writer, R)
		})
	}
}

// Record writes a named event using the default logger and the context scope.
func Record(Ctx Context.Context, Level Slog.Level, Event string, Args ...any) {
	SafeEvent := SanitizeLogValue(Event)
	Attrs := make([]Slog.Attr, 0, len(Args)/2+1)
	Attrs = append(Attrs, Slog.String("event", SafeEvent))
	for I := 0; I < len(Args); {
		if Attr, Ok := Args[I].(Slog.Attr); Ok {
			Attrs = append(Attrs, Slog.String(SanitizeLogValue(Attr.Key), SanitizeLogValue(Attr.Value.String())))
			I++
			continue
		}
		if Key, Ok := Args[I].(string); Ok && I+1 < len(Args) {
			Attrs = append(Attrs, Slog.String(SanitizeLogValue(Key), SanitizeLogValue(Fmt.Sprint(Args[I+1]))))
			I += 2
			continue
		}
		Attrs = append(Attrs, Slog.String("!BADKEY", SanitizeLogValue(Fmt.Sprint(Args[I]))))
		I++
	}
	Slog.Default().LogAttrs(Ctx, Level, SafeEvent, Attrs...)
}

// DatabaseParam adds the named URL parameter as database_id to the request
// scope, for routes that act on a database without an access-control check.
func DatabaseParam(Name string) func(Http.Handler) Http.Handler {
	return func(Next Http.Handler) Http.Handler {
		return Http.HandlerFunc(func(W Http.ResponseWriter, R *Http.Request) {
			if Value := Chi.URLParam(R, Name); Value != "" {
				AnnotateOnce(R.Context(), "database_id", Value)
			}
			Next.ServeHTTP(W, R)
		})
	}
}
