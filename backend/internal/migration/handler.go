package Migration

import (
	Json "encoding/json"
	Errors "errors"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
	Httpx "github.com/ethj0r/IF3110-K02-01/backend/internal/httpx"
	Chi "github.com/go-chi/chi/v5"
	Io "io"
	Http "net/http"
)

type Handler struct {
	Auth    *Auth.Handler
	Service *Service
}

func (Routes *Handler) Register(Api Chi.Router) {
	registerRoutes := func(Prefix string) {
		Api.Group(func(R Chi.Router) {
			R.Use(Auth.NoStore)
			R.Use(Routes.Auth.RequireSession)
			R.Get(Prefix, Routes.List)
			R.Post(Prefix, Routes.Create)
			R.Get(Prefix+"/{migrationID}", Routes.Show)
			R.Put(Prefix+"/{migrationID}", Routes.Update)
			R.Delete(Prefix+"/{migrationID}", Routes.Delete)
			R.Post(Prefix+"/{migrationID}/execute", Routes.Execute)
		})
	}
	registerRoutes("/databases/{dbID}/migrations")
	registerRoutes("/connections/{dbID}/migrations")
}

func DecodeInput(W Http.ResponseWriter, R *Http.Request) (MigrationInput, bool) {
	var Input MigrationInput
	Decoder := Json.NewDecoder(Http.MaxBytesReader(W, R.Body, 65536))
	Decoder.DisallowUnknownFields()
	if Err := Decoder.Decode(&Input); Err != nil {
		Httpx.Error(W, 400, "invalid_request", "Invalid migration request parameters")
		return Input, false
	}
	if Err := Decoder.Decode(new(any)); Err != Io.EOF {
		Httpx.Error(W, 400, "invalid_request", "Invalid migration request parameters")
		return Input, false
	}
	return Input, true
}

func WriteMigrationError(W Http.ResponseWriter, Err error) {
	switch {
	case Errors.Is(Err, ErrInvalidInput):
		Httpx.Error(W, 400, "invalid_request", "Invalid migration request parameters")
	case Errors.Is(Err, ErrForbidden):
		Httpx.Error(W, 403, "forbidden", "Insufficient database permissions for migration")
	case Errors.Is(Err, ErrNotFound):
		Httpx.Error(W, 404, "not_found", "Migration or target database not found")
	case Errors.Is(Err, ErrAlreadyExecuted):
		Httpx.Error(W, 409, "already_executed", "Migration has already been executed or terminal")
	case Errors.Is(Err, ErrConflict):
		Httpx.Error(W, 409, "conflict", "Migration version conflict")
	case Errors.Is(Err, Driver.ErrExecution):
		Httpx.Error(W, 422, "execution_failed", "Target database migration execution failed")
	case Errors.Is(Err, Driver.ErrUnavailable), Errors.Is(Err, ErrUnavailable):
		Httpx.Error(W, 502, "database_unavailable", "Target database or migration store is unavailable")
	default:
		Httpx.Error(W, 500, "internal_error", "An unexpected error occurred")
	}
}

func (Routes *Handler) Create(W Http.ResponseWriter, R *Http.Request) {
	User, _ := Auth.PrincipalFromContext(R.Context())
	DBID := Chi.URLParam(R, "dbID")
	Input, Valid := DecodeInput(W, R)
	if !Valid {
		return
	}
	Result, Err := Routes.Service.Create(R.Context(), User, DBID, Input)
	if Err != nil {
		WriteMigrationError(W, Err)
		return
	}
	Httpx.JSON(W, 201, Result)
}

func (Routes *Handler) List(W Http.ResponseWriter, R *Http.Request) {
	User, _ := Auth.PrincipalFromContext(R.Context())
	DBID := Chi.URLParam(R, "dbID")
	Result, Err := Routes.Service.List(R.Context(), User, DBID)
	if Err != nil {
		WriteMigrationError(W, Err)
		return
	}
	Httpx.JSON(W, 200, Result)
}

func (Routes *Handler) Show(W Http.ResponseWriter, R *Http.Request) {
	User, _ := Auth.PrincipalFromContext(R.Context())
	DBID := Chi.URLParam(R, "dbID")
	ID := Chi.URLParam(R, "migrationID")
	Result, Err := Routes.Service.Get(R.Context(), User, DBID, ID)
	if Err != nil {
		WriteMigrationError(W, Err)
		return
	}
	Httpx.JSON(W, 200, Result)
}

func (Routes *Handler) Update(W Http.ResponseWriter, R *Http.Request) {
	User, _ := Auth.PrincipalFromContext(R.Context())
	DBID := Chi.URLParam(R, "dbID")
	ID := Chi.URLParam(R, "migrationID")
	Input, Valid := DecodeInput(W, R)
	if !Valid {
		return
	}
	Result, Err := Routes.Service.UpdateDraft(R.Context(), User, DBID, ID, Input)
	if Err != nil {
		WriteMigrationError(W, Err)
		return
	}
	Httpx.JSON(W, 200, Result)
}

func (Routes *Handler) Delete(W Http.ResponseWriter, R *Http.Request) {
	User, _ := Auth.PrincipalFromContext(R.Context())
	DBID := Chi.URLParam(R, "dbID")
	ID := Chi.URLParam(R, "migrationID")
	if Err := Routes.Service.DeleteDraft(R.Context(), User, DBID, ID); Err != nil {
		WriteMigrationError(W, Err)
		return
	}
	W.WriteHeader(Http.StatusNoContent)
}

func (Routes *Handler) Execute(W Http.ResponseWriter, R *Http.Request) {
	User, _ := Auth.PrincipalFromContext(R.Context())
	DBID := Chi.URLParam(R, "dbID")
	ID := Chi.URLParam(R, "migrationID")
	Result, Err := Routes.Service.Execute(R.Context(), User, DBID, ID)
	if Err != nil {
		WriteMigrationError(W, Err)
		return
	}
	Httpx.JSON(W, 200, Result)
}

