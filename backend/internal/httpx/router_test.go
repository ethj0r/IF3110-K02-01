package Httpx

import (
	Context "context"
	Json "encoding/json"
	Errors "errors"
	Chi "github.com/go-chi/chi/v5"
	Http "net/http"
	Httptest "net/http/httptest"
	Strings "strings"
	Testing "testing"
)

func TestRouterRegistersFeatures(T *Testing.T) {
	Router := NewRouter(func(Context.Context) error { return nil }, Http.NotFoundHandler(), func(Api Chi.Router) {
		Api.Get("/feature", func(W Http.ResponseWriter, R *Http.Request) { JSON(W, 200, map[string]bool{"registered": true}) })
	})
	Response := Httptest.NewRecorder()
	Router.ServeHTTP(Response, Httptest.NewRequest("GET", "/api/feature", nil))
	if Response.Code != 200 || !Strings.Contains(Response.Body.String(), "registered") {
		T.Fatal("feature routes were not registered inside API router")
	}
}

func TestHealthAndAPIErrors(T *Testing.T) {
	Frontend := Http.HandlerFunc(func(W Http.ResponseWriter, R *Http.Request) {
		_, _ = W.Write([]byte("frontend"))
	})
	for _, Tc := range []struct {
		Name, Method, Path, Code string
		DatabaseError            error
		Status                   int
	}{
		{"liveness", "GET", "/api/health", "", nil, 200},
		{"readiness", "GET", "/api/ready", "", nil, 200},
		{"database unavailable", "GET", "/api/ready", "database_unavailable", Errors.New("password=secret"), 503},
		{"unknown API", "GET", "/api/missing", "not_found", nil, 404},
		{"bare API", "GET", "/api", "not_found", nil, 404},
		{"wrong method", "POST", "/api/health", "method_not_allowed", nil, 405},
	} {
		T.Run(Tc.Name, func(T *Testing.T) {
			Router := NewRouter(func(Context.Context) error { return Tc.DatabaseError }, Frontend)
			Response := Httptest.NewRecorder()
			Router.ServeHTTP(Response, Httptest.NewRequest(Tc.Method, Tc.Path, nil))
			if Response.Code != Tc.Status {
				T.Fatalf("status = %d, want %d; body = %s", Response.Code, Tc.Status, Response.Body.String())
			}
			if !Strings.HasPrefix(Response.Header().Get("Content-Type"), "application/json") {
				T.Fatal("API must return JSON, including unknown routes")
			}
			if Strings.Contains(Response.Body.String(), "secret") {
				T.Fatal("response exposed raw dependency error")
			}
			if Tc.Code != "" {
				var Body ErrorResponse
				if Err := Json.Unmarshal(Response.Body.Bytes(), &Body); Err != nil || Body.Error.Code != Tc.Code {
					T.Fatalf("unexpected error response: %s", Response.Body.String())
				}
			}
		})
	}
}

func TestRecoverDoesNotExposePanic(T *Testing.T) {
	Router := NewRouter(func(Context.Context) error { panic("session=secret") }, Http.NotFoundHandler())
	Response := Httptest.NewRecorder()
	Router.ServeHTTP(Response, Httptest.NewRequest("GET", "/api/ready", nil))
	if Response.Code != 500 || Strings.Contains(Response.Body.String(), "secret") {
		T.Fatalf("unexpected panic response: %d %s", Response.Code, Response.Body.String())
	}
}

func TestJSONEncodingFailure(T *Testing.T) {
	Response := Httptest.NewRecorder()
	JSON(Response, Http.StatusOK, make(chan int))
	if Response.Code != 500 || !Strings.Contains(Response.Body.String(), `"internal_error"`) {
		T.Fatalf("encoding failure must produce a JSON error: %d %s", Response.Code, Response.Body.String())
	}
}
