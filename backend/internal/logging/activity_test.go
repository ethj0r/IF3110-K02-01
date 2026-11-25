package Logging

import (
	Context "context"
	Slog "log/slog"
	Http "net/http"
	Httptest "net/http/httptest"
	Testing "testing"

	Chi "github.com/go-chi/chi/v5"
	Middleware "github.com/go-chi/chi/v5/middleware"
)

func useDefault(T *Testing.T, Logger *Slog.Logger) {
	T.Helper()
	Previous := Slog.Default()
	Slog.SetDefault(Logger)
	T.Cleanup(func() { Slog.SetDefault(Previous) })
}

func TestActivityRecordsOutcomeTargetAndScope(T *Testing.T) {
	Logger, Buffer := capture(Slog.LevelInfo)
	useDefault(T, Logger)
	Router := Chi.NewRouter()
	Router.Use(Middleware.RequestID, RequestLogger(Logger))
	Router.With(DatabaseParam("databaseId"), Activity("admin.access.grant")).Put("/groups/{id}/access/{databaseId}", func(W Http.ResponseWriter, R *Http.Request) {
		Annotate(R.Context(), "actor", "root")
		W.WriteHeader(Http.StatusNoContent)
	})
	Router.With(Activity("admin.user.delete")).Delete("/users/{id}", func(W Http.ResponseWriter, R *Http.Request) {
		W.WriteHeader(Http.StatusForbidden)
	})
	Router.With(Activity("admin.user.create")).Post("/users", func(W Http.ResponseWriter, R *Http.Request) {
		W.WriteHeader(Http.StatusConflict)
	})
	for _, Request := range []*Http.Request{
		Httptest.NewRequest("PUT", "/groups/g-1/access/db-9", nil),
		Httptest.NewRequest("DELETE", "/users/u-2", nil),
		Httptest.NewRequest("POST", "/users", nil),
	} {
		Router.ServeHTTP(Httptest.NewRecorder(), Request)
	}
	var Events []map[string]any
	for _, Record := range records(T, Buffer) {
		if Record["event"] != "http.request" {
			Events = append(Events, Record)
		}
	}
	if len(Events) != 3 {
		T.Fatalf("expected 3 activity records, got %v", Events)
	}
	Grant, Delete, Create := Events[0], Events[1], Events[2]
	Target, _ := Grant["target"].(map[string]any)
	if Grant["event"] != "admin.access.grant" || Grant["outcome"] != "succeeded" || Grant["status"] != float64(204) ||
		Grant["actor"] != "root" || Grant["database_id"] != "db-9" || Target["id"] != "g-1" || Target["databaseId"] != "db-9" || Grant["request_id"] == nil {
		T.Fatalf("unexpected grant activity: %v", Grant)
	}
	if Delete["outcome"] != "denied" || Delete["level"] != "WARN" {
		T.Fatalf("403 must be recorded as denied: %v", Delete)
	}
	if Create["outcome"] != "failed" || Create["target"] != nil {
		T.Fatalf("409 must be recorded as failed without target: %v", Create)
	}
}

func TestRecordUsesDefaultLoggerAndScope(T *Testing.T) {
	Logger, Buffer := capture(Slog.LevelInfo)
	useDefault(T, Logger)
	Record(NewScope(Context.Background(), "request_id", "r-1"), Slog.LevelWarn, "auth.csrf.rejected", "reason", "origin")
	Lines := records(T, Buffer)
	if len(Lines) != 1 || Lines[0]["event"] != "auth.csrf.rejected" || Lines[0]["msg"] != "auth.csrf.rejected" || Lines[0]["request_id"] != "r-1" || Lines[0]["level"] != "WARN" {
		T.Fatalf("unexpected record: %v", Lines)
	}
}

func TestRecordAndRequestLoggerSanitizeLogInjection(T *Testing.T) {
	Logger, Buffer := capture(Slog.LevelInfo)
	useDefault(T, Logger)

	Record(Context.Background(), Slog.LevelWarn, "access.denied", "target_database_id", "db-1\r\nforged=true")
	Lines := records(T, Buffer)
	if len(Lines) != 1 || Lines[0]["target_database_id"] != "db-1forged=true" {
		T.Fatalf("expected CR/LF stripped in Record, got %v", Lines)
	}

	if Got := SanitizeLogValue("/api/test\r\nfake"); Got != "/api/testfake" {
		T.Fatalf("expected CR/LF stripped in SanitizeLogValue, got %q", Got)
	}
}
