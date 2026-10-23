package Access

import (
	Errors "errors"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Httpx "github.com/ethj0r/IF3110-K02-01/backend/internal/httpx"
	Chi "github.com/go-chi/chi/v5"
	Http "net/http"
)

type Handler struct {
	Auth *Auth.Handler
	Repo *Repository
}

func (Routes *Handler) Register(Api Chi.Router) {
	Api.Group(func(R Chi.Router) {
		R.Use(Routes.Auth.RequireSession)
		R.Get("/databases", Routes.List)
		R.Get("/databases/{id}", Routes.Show)
	})
}

func (Routes *Handler) List(W Http.ResponseWriter, R *Http.Request) {
	User, Ok := Auth.PrincipalFromContext(R.Context())
	if !Ok {
		Httpx.Error(W, Http.StatusUnauthorized, "unauthenticated", "Authentication required")
		return
	}
	Result, Err := Routes.Repo.ListAccessible(R.Context(), User)
	if Err != nil {
		WriteAccessError(W, Err)
		return
	}
	Httpx.JSON(W, Http.StatusOK, Result)
}

func (Routes *Handler) Show(W Http.ResponseWriter, R *Http.Request) {
	User, Ok := Auth.PrincipalFromContext(R.Context())
	if !Ok {
		Httpx.Error(W, Http.StatusUnauthorized, "unauthenticated", "Authentication required")
		return
	}
	Result, Err := Routes.Repo.GetAccessible(R.Context(), User, Chi.URLParam(R, "id"))
	if Err != nil {
		WriteAccessError(W, Err)
		return
	}
	Httpx.JSON(W, Http.StatusOK, Result)
}

func WriteAccessError(W Http.ResponseWriter, Err error) {
	switch {
	case Errors.Is(Err, ErrNotFound):
		Httpx.Error(W, Http.StatusNotFound, "not_found", "Database access not found")
	case Errors.Is(Err, ErrInvalidAction), Errors.Is(Err, ErrInvalidLevel):
		Httpx.Error(W, Http.StatusBadRequest, "invalid_request", "Invalid access request")
	default:
		Httpx.Error(W, Http.StatusInternalServerError, "internal_error", "An unexpected error occurred")
	}
}
