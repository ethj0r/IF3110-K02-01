package Logging

import (
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
					Slog.String("route", Route),
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
