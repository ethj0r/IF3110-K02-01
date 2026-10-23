package Inspect

import (
	Errors "errors"
	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Httpx "github.com/ethj0r/IF3110-K02-01/backend/internal/httpx"
	Chi "github.com/go-chi/chi/v5"
	Http "net/http"
	Strconv "strconv"
)

type Handler struct {
	Auth    *Auth.Handler
	Policy  Access.Evaluator
	Service *Service
}

func (Routes *Handler) Register(Api Chi.Router) {
	Api.Group(func(R Chi.Router) {
		R.Use(Auth.NoStore)
		R.Use(Routes.Auth.RequireSession)
		R.Use(Access.RequireDatabase(Routes.Policy, Access.ActionView, "id"))
		R.Get("/databases/{id}/schemas", Routes.ListSchemas)
		R.Get("/databases/{id}/tables", Routes.ListTables)
		R.Get("/databases/{id}/tables/{table}/columns", Routes.ListColumns)
		R.Get("/databases/{id}/tables/{table}/constraints", Routes.ListConstraints)
		R.Get("/databases/{id}/tables/{table}/indexes", Routes.ListIndexes)
		R.Get("/databases/{id}/tables/{table}/rows", Routes.ListRows)
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

func tableRef(R *Http.Request) (string, string) {
	return Chi.URLParam(R, "id"), Chi.URLParam(R, "table")
}

func (Routes *Handler) ListColumns(W Http.ResponseWriter, R *Http.Request) {
	ID, Table := tableRef(R)
	Result, Err := Routes.Service.Columns(R.Context(), ID, R.URL.Query().Get("schema"), Table)
	if Err != nil {
		WriteInspectError(W, Err)
		return
	}
	Httpx.JSON(W, 200, map[string]any{"columns": Result})
}

func (Routes *Handler) ListConstraints(W Http.ResponseWriter, R *Http.Request) {
	ID, Table := tableRef(R)
	Result, Err := Routes.Service.Constraints(R.Context(), ID, R.URL.Query().Get("schema"), Table)
	if Err != nil {
		WriteInspectError(W, Err)
		return
	}
	Httpx.JSON(W, 200, map[string]any{"constraints": Result})
}

func (Routes *Handler) ListIndexes(W Http.ResponseWriter, R *Http.Request) {
	ID, Table := tableRef(R)
	Result, Err := Routes.Service.Indexes(R.Context(), ID, R.URL.Query().Get("schema"), Table)
	if Err != nil {
		WriteInspectError(W, Err)
		return
	}
	Httpx.JSON(W, 200, map[string]any{"indexes": Result})
}

func (Routes *Handler) ListRows(W Http.ResponseWriter, R *Http.Request) {
	ID, Table := tableRef(R)
	Query := R.URL.Query()
	Page, Err := Strconv.Atoi(Query.Get("page"))
	if Query.Get("page") == "" {
		Page, Err = 1, nil
	}
	PageSize, SizeErr := Strconv.Atoi(Query.Get("page_size"))
	if Query.Get("page_size") == "" {
		PageSize, SizeErr = 25, nil
	}
	if Err != nil || SizeErr != nil {
		WriteInspectError(W, ErrInvalidInput)
		return
	}
	Result, Err := Routes.Service.Rows(R.Context(), ID, Query.Get("schema"), Table, Page, PageSize)
	if Err != nil {
		WriteInspectError(W, Err)
		return
	}
	Httpx.JSON(W, 200, Result)
}
