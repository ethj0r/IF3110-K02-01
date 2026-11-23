package Tracing

import (
	Http "net/http"

	Chi "github.com/go-chi/chi/v5"
	Middleware "github.com/go-chi/chi/v5/middleware"
	Otel "go.opentelemetry.io/otel"
	Attribute "go.opentelemetry.io/otel/attribute"
	Codes "go.opentelemetry.io/otel/codes"
	Propagation "go.opentelemetry.io/otel/propagation"
	Trace "go.opentelemetry.io/otel/trace"
)

// HTTPMiddleware starts a server span for each request, continuing an incoming
// W3C traceparent header. The span is renamed to the matched chi route after
// routing so names stay low-cardinality (no IDs in span names).
func HTTPMiddleware(Next Http.Handler) Http.Handler {
	return Http.HandlerFunc(func(W Http.ResponseWriter, R *Http.Request) {
		Ctx := Otel.GetTextMapPropagator().Extract(R.Context(), Propagation.HeaderCarrier(R.Header))
		Ctx, Span := Tracer().Start(Ctx, "HTTP "+R.Method,
			Trace.WithSpanKind(Trace.SpanKindServer),
			Trace.WithAttributes(
				Attribute.String("http.request.method", R.Method),
				Attribute.String("url.path", R.URL.Path),
			),
		)
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
			if Route := Chi.RouteContext(Request.Context()); Route != nil && Route.RoutePattern() != "" {
				Span.SetName("HTTP " + R.Method + " " + Route.RoutePattern())
				Span.SetAttributes(Attribute.String("http.route", Route.RoutePattern()))
			}
			if ID := Middleware.GetReqID(R.Context()); ID != "" {
				Span.SetAttributes(Attribute.String("request_id", ID))
			}
			Span.SetAttributes(Attribute.Int("http.response.status_code", Status))
			if Status >= 500 {
				Span.SetStatus(Codes.Error, Http.StatusText(Status))
			}
			Span.End()
		}()
		Next.ServeHTTP(Writer, Request)
		Panicked = false
	})
}
