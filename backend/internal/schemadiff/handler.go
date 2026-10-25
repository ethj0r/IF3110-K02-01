package Schemadiff

import (
	Json "encoding/json"
	Errors "errors"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Httpx "github.com/ethj0r/IF3110-K02-01/backend/internal/httpx"
	Chi "github.com/go-chi/chi/v5"
	Io "io"
	Mime "mime"
	Http "net/http"
)

type Handler struct {
	Auth    *Auth.Handler
	Service *Service
}

func (Routes *Handler) Register(Api Chi.Router) {
	Api.With(Routes.Auth.RequireSession).Post("/databases/{id}/schema-diff", Routes.Compare)
}

func (Routes *Handler) Compare(W Http.ResponseWriter, R *Http.Request) {
	Type, _, Err := Mime.ParseMediaType(R.Header.Get("Content-Type"))
	if Err != nil || Type != "application/json" {
		Httpx.Error(W, 415, "unsupported_media_type", "Use application/json")
		return
	}
	var Input Input
	Decoder := Json.NewDecoder(Http.MaxBytesReader(W, R.Body, 2048))
	Decoder.DisallowUnknownFields()
	if Err := Decoder.Decode(&Input); Err != nil {
		WriteError(W, ErrInvalidInput)
		return
	}
	if Err := Decoder.Decode(new(any)); Err != Io.EOF {
		WriteError(W, ErrInvalidInput)
		return
	}
	User, Ok := Auth.PrincipalFromContext(R.Context())
	if !Ok {
		Httpx.Error(W, 401, "unauthenticated", "Authentication required")
		return
	}
	Result, Err := Routes.Service.Compare(R.Context(), User, Chi.URLParam(R, "id"), Input)
	if Err != nil {
		WriteError(W, Err)
		return
	}
	Httpx.JSON(W, 200, Result)
}

func WriteError(W Http.ResponseWriter, Err error) {
	switch {
	case Errors.Is(Err, ErrInvalidInput):
		Httpx.Error(W, 400, "invalid_request", "Choose a valid reference database and schema")
	case Errors.Is(Err, ErrForbidden):
		Httpx.Error(W, 403, "forbidden", "Edit access to the current database and view access to the reference are required")
	case Errors.Is(Err, ErrNotFound):
		Httpx.Error(W, 404, "not_found", "Comparison database not found")
	case Errors.Is(Err, ErrUnsupported):
		Httpx.Error(W, 422, "schema_diff_unsupported", "This schema change is not supported by the generator; write the migration manually")
	case Errors.Is(Err, ErrTooLarge):
		Httpx.Error(W, 413, "schema_diff_too_large", "Generated SQL exceeds the draft limit; compare a smaller schema")
	default:
		Httpx.Error(W, 502, "schema_diff_unavailable", "Unable to compare the database schemas")
	}
}
