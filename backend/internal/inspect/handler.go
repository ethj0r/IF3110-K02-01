package Inspect

import (
	Errors "errors"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Httpx "github.com/ethj0r/IF3110-K02-01/backend/internal/httpx"
	Chi "github.com/go-chi/chi/v5"
	Http "net/http"
)

type Handler struct {
	Auth    *Auth.Handler
	Service *Service
}

func (Routes *Handler) Register(Api Chi.Router) {
	Api.Group(func(R Chi.Router) {
		R.Use(Auth.NoStore)
		R.Use(Routes.Auth.RequireSession)
		R.Get("/databases", Routes.ListDatabases)
		R.Get("/databases/{id}/schemas", Routes.ListSchemas)
		R.Get("/databases/{id}/tables", Routes.ListTables)
	})
}

func WriteInspectError(W Http.ResponseWriter, Err error) {
	switch {
	case Errors.Is(Err, ErrInvalidInput):
		Httpx.Error(W, 400, "invalid_request", "Invalid browse parameters")
	case Errors.Is(Err, ErrNotFound):
		Httpx.Error(W, 404, "not_found", "Database connection not found")
	case Errors.Is(Err, ErrUnavailable):
		Httpx.Error(W, 502, "connection_unavailable", "Target database is unavailable")
	default:
		Httpx.Error(W, 500, "internal_error", "An unexpected error occurred")
	}
}

func (Routes *Handler) ListDatabases(W Http.ResponseWriter, R *Http.Request) {
	Result, Err := Routes.Service.ListDatabases(R.Context())
	if Err != nil {
		WriteInspectError(W, Err)
		return
	}
	Httpx.JSON(W, 200, Result)
}

func (Routes *Handler) ListSchemas(W Http.ResponseWriter, R *Http.Request) {
	Result, Err := Routes.Service.Schemas(R.Context(), Chi.URLParam(R, "id"))
	if Err != nil {
		WriteInspectError(W, Err)
		return
	}
	Httpx.JSON(W, 200, map[string]any{"schemas": Result})
}

func (Routes *Handler) ListTables(W Http.ResponseWriter, R *Http.Request) {
	Result, Err := Routes.Service.Tables(R.Context(), Chi.URLParam(R, "id"), R.URL.Query().Get("schema"))
	if Err != nil {
		WriteInspectError(W, Err)
		return
	}
	Httpx.JSON(W, 200, map[string]any{"tables": Result})
}
