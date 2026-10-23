package Access

import (
	Context "context"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Chi "github.com/go-chi/chi/v5"
	Http "net/http"
	Httptest "net/http/httptest"
	Testing "testing"
)

func PrincipalRequest(Principal *Auth.Principal) *Http.Request {
	Request := Httptest.NewRequest(Http.MethodGet, "/", nil)
	if Principal == nil {
		return Request
	}
	State := Auth.Authenticated{Principal: *Principal}
	return Request.WithContext(Context.WithValue(Request.Context(), Auth.SessionContext, State))
}

func TestRequireAdminRejectsAnonymousAndOrdinaryUsers(T *Testing.T) {
	Called := false
	Next := Http.HandlerFunc(func(Http.ResponseWriter, *Http.Request) { Called = true })
	for _, Tc := range []struct {
		Name      string
		Principal *Auth.Principal
		Status    int
	}{
		{"anonymous", nil, Http.StatusUnauthorized},
		{"ordinary user", &Auth.Principal{ID: "user", Username: "user"}, Http.StatusForbidden},
		{"platform administrator", &Auth.Principal{ID: "admin", Username: "admin", IsPlatformAdmin: true}, Http.StatusOK},
	} {
		T.Run(Tc.Name, func(T *Testing.T) {
			Called = false
			Response := Httptest.NewRecorder()
			RequireAdmin(Next).ServeHTTP(Response, PrincipalRequest(Tc.Principal))
			if Response.Code != Tc.Status {
				T.Fatalf("status = %d, want %d", Response.Code, Tc.Status)
			}
			if Called != (Tc.Status == Http.StatusOK) {
				T.Fatal("protected handler invocation mismatch")
			}
		})
	}
}

func TestRequireDatabaseUsesPolicy(T *Testing.T) {
	Policy := StaticEvaluator{Grants: map[string]map[string]Level{"user": {"db": LevelViewOnly}}}
	Next := Http.HandlerFunc(func(W Http.ResponseWriter, R *Http.Request) { W.WriteHeader(Http.StatusNoContent) })
	for _, Tc := range []struct {
		Name      string
		Act       Action
		Principal Auth.Principal
		Status    int
	}{
		{"view allowed", ActionView, Auth.Principal{ID: "user"}, Http.StatusNoContent},
		{"edit denied", ActionEdit, Auth.Principal{ID: "user"}, Http.StatusForbidden},
		{"unknown user denied", ActionView, Auth.Principal{ID: "other"}, Http.StatusForbidden},
	} {
		T.Run(Tc.Name, func(T *Testing.T) {
			Request := PrincipalRequest(&Tc.Principal)
			RouteContext := Chi.NewRouteContext()
			RouteContext.URLParams.Add("id", "db")
			Request = Request.WithContext(Context.WithValue(Request.Context(), Chi.RouteCtxKey, RouteContext))
			Response := Httptest.NewRecorder()
			RequireDatabase(Policy, Tc.Act, "id")(Next).ServeHTTP(Response, Request)
			if Response.Code != Tc.Status {
				T.Fatalf("status = %d, want %d", Response.Code, Tc.Status)
			}
		})
	}
}
