package Connection

import (
	Json "encoding/json"
	Errors "errors"
	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
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
	Driver  Driver.Driver
}

func (Routes *Handler) Register(Api Chi.Router) {
	Api.Group(func(R Chi.Router) {
		R.Use(Auth.NoStore)
		R.Use(Routes.Auth.RequireSession)
		R.Use(Access.RequireAdmin)
		R.Post("/connections", Routes.Create)
		R.Get("/connections", Routes.List)
		R.Get("/connections/{id}", Routes.Show)
		R.Put("/connections/{id}", Routes.Update)
		R.Delete("/connections/{id}", Routes.Delete)
		R.Post("/connections/{id}/test", Routes.Test)
	})
}

func DecodeInput(W Http.ResponseWriter, R *Http.Request) (ConnectionInput, bool) {
	var Input ConnectionInput
	Decoder := Json.NewDecoder(Http.MaxBytesReader(W, R.Body, 8192))
	Decoder.DisallowUnknownFields()
	if Err := Decoder.Decode(&Input); Err != nil {
		Httpx.Error(W, 400, "invalid_request", "Invalid connection request")
		return Input, false
	}
	if Err := Decoder.Decode(new(any)); Err != Io.EOF {
		Httpx.Error(W, 400, "invalid_request", "Invalid connection request")
		return Input, false
	}
	return Input, true
}

func WriteStoreError(W Http.ResponseWriter, Err error) {
	switch {
	case Errors.Is(Err, ErrInvalidInput):
		Httpx.Error(W, 400, "invalid_request", "Invalid connection parameters")
	case Errors.Is(Err, ErrNotFound):
		Httpx.Error(W, 404, "not_found", "Database connection not found")
	case Errors.Is(Err, ErrConflict):
		Httpx.Error(W, 409, "conflict", "Database connection name is already used")
	default:
		Httpx.Error(W, 500, "internal_error", "An unexpected error occurred")
	}
}

func (Routes *Handler) Create(W Http.ResponseWriter, R *Http.Request) {
	Admin, Ok := Auth.PrincipalFromContext(R.Context())
	if !Ok {
		Httpx.Error(W, 401, "unauthenticated", "Authentication required")
		return
	}
	Input, Valid := DecodeInput(W, R)
	if !Valid {
		return
	}
	Result, Err := Routes.Service.Create(R.Context(), Input, &Admin.ID)
	if Err != nil {
		WriteStoreError(W, Err)
		return
	}
	Httpx.JSON(W, 201, Result)
}

func (Routes *Handler) List(W Http.ResponseWriter, R *Http.Request) {
	Result, Err := Routes.Service.List(R.Context())
	if Err != nil {
		WriteStoreError(W, Err)
		return
	}
	Httpx.JSON(W, 200, Result)
}

func (Routes *Handler) Show(W Http.ResponseWriter, R *Http.Request) {
	Result, Err := Routes.Service.Get(R.Context(), Chi.URLParam(R, "id"))
	if Err != nil {
		WriteStoreError(W, Err)
		return
	}
	Httpx.JSON(W, 200, Result)
}

func (Routes *Handler) Update(W Http.ResponseWriter, R *Http.Request) {
	Input, Valid := DecodeInput(W, R)
	if !Valid {
		return
	}
	Result, Err := Routes.Service.Update(R.Context(), Chi.URLParam(R, "id"), Input)
	if Err != nil {
		WriteStoreError(W, Err)
		return
	}
	Httpx.JSON(W, 200, Result)
}

func (Routes *Handler) Delete(W Http.ResponseWriter, R *Http.Request) {
	if Err := Routes.Service.Delete(R.Context(), Chi.URLParam(R, "id")); Err != nil {
		WriteStoreError(W, Err)
		return
	}
	W.WriteHeader(Http.StatusNoContent)
}

func (Routes *Handler) Test(W Http.ResponseWriter, R *Http.Request) {
	_, DSN, Err := Routes.Service.DialDSN(R.Context(), Chi.URLParam(R, "id"))
	if Err != nil {
		WriteStoreError(W, Err)
		return
	}
	Target, Err := Routes.Driver.Open(DSN)
	if Err != nil {
		Httpx.Error(W, 502, "connection_unavailable", "Target database is unavailable")
		return
	}
	defer Target.Close()
	if Err := Target.Ping(R.Context()); Err != nil {
		Httpx.Error(W, 502, "connection_unavailable", "Target database is unavailable")
		return
	}
	Httpx.JSON(W, 200, map[string]string{"status": "ok"})
}
