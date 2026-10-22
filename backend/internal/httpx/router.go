package Httpx

import (
	Context "context"
	Chi "github.com/go-chi/chi/v5"
	Middleware "github.com/go-chi/chi/v5/middleware"
	Http "net/http"
	Time "time"
)

func NewRouter(Ping func(Context.Context) error, Frontend Http.Handler, Configure ...func(Chi.Router)) Http.Handler {
	R := Chi.NewRouter()
	R.Use(Middleware.RequestID)
	R.Use(RecoverErrors)
	R.NotFound(NotFound)
	R.MethodNotAllowed(MethodNotAllowed)
	R.Route("/api", func(Api Chi.Router) {
		Api.NotFound(NotFound)
		Api.MethodNotAllowed(MethodNotAllowed)
		Api.Get("/health", func(W Http.ResponseWriter, R *Http.Request) {
			JSON(W, Http.StatusOK, map[string]string{"status": "ok"})
		})
		Api.Get("/ready", func(W Http.ResponseWriter, R *Http.Request) {
			Ctx, Cancel := Context.WithTimeout(R.Context(), 2*Time.Second)
			defer Cancel()
			if Err := Ping(Ctx); Err != nil {
				Error(W, Http.StatusServiceUnavailable, "database_unavailable", "Platform database is unavailable")
				return
			}
			JSON(W, Http.StatusOK, map[string]string{"status": "ready"})
		})
		for _, Register := range Configure {
			Register(Api)
		}
	})
	R.Mount("/", Frontend)
	return R
}

func NotFound(W Http.ResponseWriter, R *Http.Request) {
	Error(W, Http.StatusNotFound, "not_found", "Resource not found")
}

func MethodNotAllowed(W Http.ResponseWriter, R *Http.Request) {
	Error(W, Http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
}

func RecoverErrors(Next Http.Handler) Http.Handler {
	return Http.HandlerFunc(func(W Http.ResponseWriter, R *Http.Request) {
		defer func() {
			if Value := recover(); Value != nil {
				if Value == Http.ErrAbortHandler {
					panic(Value)
				}

				Error(W, Http.StatusInternalServerError, "internal_error", "An unexpected error occurred")
			}
		}()
		Next.ServeHTTP(W, R)
	})
}
