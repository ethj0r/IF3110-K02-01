package Admin

import (
	Json "encoding/json"
	Errors "errors"
	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
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
	Api.Route("/admin", func(R Chi.Router) {
		R.Use(Routes.Auth.RequireSession)
		R.Use(Access.RequireAdmin)
		R.Get("/users", Routes.ListUsers)
		R.Post("/users", Routes.CreateUser)
		R.Get("/users/{id}", Routes.GetUser)
		R.Patch("/users/{id}", Routes.UpdateUser)
		R.Delete("/users/{id}", Routes.DeleteUser)
		R.Put("/users/{id}/password", Routes.ResetPassword)
		R.Get("/groups", Routes.ListGroups)
		R.Post("/groups", Routes.CreateGroup)
		R.Get("/groups/{id}", Routes.GetGroup)
		R.Patch("/groups/{id}", Routes.UpdateGroup)
		R.Delete("/groups/{id}", Routes.DeleteGroup)
		R.Get("/groups/{id}/members", Routes.ListMembers)
		R.Put("/groups/{id}/members/{userId}", Routes.AddMember)
		R.Delete("/groups/{id}/members/{userId}", Routes.RemoveMember)
		R.Get("/groups/{id}/access", Routes.ListGroupAccess)
		R.Put("/groups/{id}/access/{databaseId}", Routes.SetAccess)
		R.Delete("/groups/{id}/access/{databaseId}", Routes.RemoveAccess)
		R.Get("/access", Routes.Matrix)
	})
}

func DecodeInput(W Http.ResponseWriter, R *Http.Request, Target any) bool {
	MediaType, _, Err := Mime.ParseMediaType(R.Header.Get("Content-Type"))
	if Err != nil || MediaType != "application/json" {
		Httpx.Error(W, Http.StatusUnsupportedMediaType, "unsupported_media_type", "Use application/json")
		return false
	}
	Decoder := Json.NewDecoder(Http.MaxBytesReader(W, R.Body, 8192))
	Decoder.DisallowUnknownFields()
	if Err := Decoder.Decode(Target); Err != nil {
		Httpx.Error(W, Http.StatusBadRequest, "invalid_request", "Invalid administration request")
		return false
	}
	if Err := Decoder.Decode(new(any)); Err != Io.EOF {
		Httpx.Error(W, Http.StatusBadRequest, "invalid_request", "Invalid administration request")
		return false
	}
	return true
}

func WriteAdminError(W Http.ResponseWriter, Err error) {
	switch {
	case Errors.Is(Err, ErrInvalidInput), Errors.Is(Err, Access.ErrInvalidLevel):
		Httpx.Error(W, Http.StatusBadRequest, "invalid_request", "Invalid administration request")
	case Errors.Is(Err, ErrNotFound), Errors.Is(Err, Access.ErrNotFound):
		Httpx.Error(W, Http.StatusNotFound, "not_found", "Administration resource not found")
	case Errors.Is(Err, ErrConflict):
		Httpx.Error(W, Http.StatusConflict, "conflict", "Administration resource already exists")
	case Errors.Is(Err, ErrLastAdmin):
		Httpx.Error(W, Http.StatusConflict, "last_admin", "At least one active platform administrator is required")
	default:
		Httpx.Error(W, Http.StatusInternalServerError, "internal_error", "An unexpected error occurred")
	}
}

func (Routes *Handler) ListUsers(W Http.ResponseWriter, R *Http.Request) {
	Result, Err := Routes.Service.ListUsers(R.Context())
	if Err != nil {
		WriteAdminError(W, Err)
		return
	}
	Httpx.JSON(W, Http.StatusOK, Result)
}

func (Routes *Handler) CreateUser(W Http.ResponseWriter, R *Http.Request) {
	var Input CreateUserInput
	if !DecodeInput(W, R, &Input) {
		return
	}
	Result, Err := Routes.Service.CreateUser(R.Context(), Input)
	if Err != nil {
		WriteAdminError(W, Err)
		return
	}
	Httpx.JSON(W, Http.StatusCreated, Result)
}

func (Routes *Handler) GetUser(W Http.ResponseWriter, R *Http.Request) {
	Result, Err := Routes.Service.GetUser(R.Context(), Chi.URLParam(R, "id"))
	if Err != nil {
		WriteAdminError(W, Err)
		return
	}
	Httpx.JSON(W, Http.StatusOK, Result)
}

func (Routes *Handler) UpdateUser(W Http.ResponseWriter, R *Http.Request) {
	var Input UpdateUserInput
	if !DecodeInput(W, R, &Input) {
		return
	}
	if Err := Input.Validate(); Err != nil {
		WriteAdminError(W, Err)
		return
	}
	Result, Err := Routes.Service.UpdateUserActive(R.Context(), Chi.URLParam(R, "id"), *Input.IsActive)
	if Err != nil {
		WriteAdminError(W, Err)
		return
	}
	Httpx.JSON(W, Http.StatusOK, Result)
}

func (Routes *Handler) DeleteUser(W Http.ResponseWriter, R *Http.Request) {
	if Err := Routes.Service.DeleteUser(R.Context(), Chi.URLParam(R, "id")); Err != nil {
		WriteAdminError(W, Err)
		return
	}
	W.WriteHeader(Http.StatusNoContent)
}

func (Routes *Handler) ResetPassword(W Http.ResponseWriter, R *Http.Request) {
	var Input PasswordInput
	if !DecodeInput(W, R, &Input) {
		return
	}
	Result, Err := Routes.Service.ResetPassword(R.Context(), Chi.URLParam(R, "id"), Input)
	if Err != nil {
		WriteAdminError(W, Err)
		return
	}
	Httpx.JSON(W, Http.StatusOK, Result)
}

func (Routes *Handler) ListGroups(W Http.ResponseWriter, R *Http.Request) {
	Result, Err := Routes.Service.ListGroups(R.Context())
	if Err != nil {
		WriteAdminError(W, Err)
		return
	}
	Httpx.JSON(W, Http.StatusOK, Result)
}

func (Routes *Handler) CreateGroup(W Http.ResponseWriter, R *Http.Request) {
	var Input CreateGroupInput
	if !DecodeInput(W, R, &Input) {
		return
	}
	Result, Err := Routes.Service.CreateGroup(R.Context(), Input)
	if Err != nil {
		WriteAdminError(W, Err)
		return
	}
	Httpx.JSON(W, Http.StatusCreated, Result)
}

func (Routes *Handler) GetGroup(W Http.ResponseWriter, R *Http.Request) {
	Result, Err := Routes.Service.GetGroup(R.Context(), Chi.URLParam(R, "id"))
	if Err != nil {
		WriteAdminError(W, Err)
		return
	}
	Httpx.JSON(W, Http.StatusOK, Result)
}

func (Routes *Handler) UpdateGroup(W Http.ResponseWriter, R *Http.Request) {
	var Input UpdateGroupInput
	if !DecodeInput(W, R, &Input) {
		return
	}
	Result, Err := Routes.Service.UpdateGroup(R.Context(), Chi.URLParam(R, "id"), Input)
	if Err != nil {
		WriteAdminError(W, Err)
		return
	}
	Httpx.JSON(W, Http.StatusOK, Result)
}

func (Routes *Handler) DeleteGroup(W Http.ResponseWriter, R *Http.Request) {
	if Err := Routes.Service.DeleteGroup(R.Context(), Chi.URLParam(R, "id")); Err != nil {
		WriteAdminError(W, Err)
		return
	}
	W.WriteHeader(Http.StatusNoContent)
}

func (Routes *Handler) ListMembers(W Http.ResponseWriter, R *Http.Request) {
	Result, Err := Routes.Service.ListMembers(R.Context(), Chi.URLParam(R, "id"))
	if Err != nil {
		WriteAdminError(W, Err)
		return
	}
	Httpx.JSON(W, Http.StatusOK, Result)
}

func (Routes *Handler) AddMember(W Http.ResponseWriter, R *Http.Request) {
	if Err := Routes.Service.AddMember(R.Context(), Chi.URLParam(R, "id"), Chi.URLParam(R, "userId")); Err != nil {
		WriteAdminError(W, Err)
		return
	}
	W.WriteHeader(Http.StatusNoContent)
}

func (Routes *Handler) RemoveMember(W Http.ResponseWriter, R *Http.Request) {
	if Err := Routes.Service.RemoveMember(R.Context(), Chi.URLParam(R, "id"), Chi.URLParam(R, "userId")); Err != nil {
		WriteAdminError(W, Err)
		return
	}
	W.WriteHeader(Http.StatusNoContent)
}

func (Routes *Handler) ListGroupAccess(W Http.ResponseWriter, R *Http.Request) {
	Result, Err := Routes.Service.ListGroupAccess(R.Context(), Chi.URLParam(R, "id"))
	if Err != nil {
		WriteAdminError(W, Err)
		return
	}
	Httpx.JSON(W, Http.StatusOK, Result)
}

func (Routes *Handler) SetAccess(W Http.ResponseWriter, R *Http.Request) {
	var Input AccessInput
	if !DecodeInput(W, R, &Input) {
		return
	}
	if Err := Routes.Service.SetAccess(R.Context(), Chi.URLParam(R, "id"), Chi.URLParam(R, "databaseId"), Input.Level); Err != nil {
		WriteAdminError(W, Err)
		return
	}
	W.WriteHeader(Http.StatusNoContent)
}

func (Routes *Handler) RemoveAccess(W Http.ResponseWriter, R *Http.Request) {
	if Err := Routes.Service.RemoveAccess(R.Context(), Chi.URLParam(R, "id"), Chi.URLParam(R, "databaseId")); Err != nil {
		WriteAdminError(W, Err)
		return
	}
	W.WriteHeader(Http.StatusNoContent)
}

func (Routes *Handler) Matrix(W Http.ResponseWriter, R *Http.Request) {
	Result, Err := Routes.Service.Matrix(R.Context())
	if Err != nil {
		WriteAdminError(W, Err)
		return
	}
	Httpx.JSON(W, Http.StatusOK, Result)
}
