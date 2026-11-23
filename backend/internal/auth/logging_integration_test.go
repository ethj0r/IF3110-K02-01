//go:build integration

package Auth

import (
	Bytes "bytes"
	Json "encoding/json"
	Slog "log/slog"
	Http "net/http"
	Httptest "net/http/httptest"
	Strings "strings"
	Testing "testing"

	Logging "github.com/ethj0r/IF3110-K02-01/backend/internal/logging"
	Chi "github.com/go-chi/chi/v5"
)

func TestAuthenticationActivityIsLoggedWithoutSecrets(T *Testing.T) {
	App := NewFixture(T)
	Buffer := &Bytes.Buffer{}
	Logger := Logging.New(Buffer, Slog.LevelInfo)
	Previous := Slog.Default()
	Slog.SetDefault(Logger)
	T.Cleanup(func() { Slog.SetDefault(Previous) })
	Routes := &Handler{Service: App.Service}
	Router := Chi.NewRouter()
	Router.Use(Logging.RequestLogger(Logger))
	Router.Route("/api", Routes.Register)
	Send := func(Method, Path, Body, Identifier, Token string) *Httptest.ResponseRecorder {
		Request := Httptest.NewRequest(Method, Path, Strings.NewReader(Body))
		Request.Header.Set("Origin", App.Service.Settings.Origin)
		if Body != "" {
			Request.Header.Set("Content-Type", "application/json")
		}
		if Identifier != "" {
			Request.AddCookie(&Http.Cookie{Name: CookieName, Value: Identifier})
		}
		if Token != "" {
			Request.Header.Set("X-CSRF-Token", Token)
		}
		Response := Httptest.NewRecorder()
		Router.ServeHTTP(Response, Request)
		return Response
	}

	Send("POST", "/api/auth/login", LoginBody("admin", "incorrect password"), "", "")
	Login := Send("POST", "/api/auth/login", LoginBody("admin", TestPassword), "", "")
	Identifier := Login.Result().Cookies()[0].Value
	var Body struct {
		CSRFToken string `json:"csrf_token"`
	}
	_ = Json.Unmarshal(Login.Body.Bytes(), &Body)
	Send("GET", "/api/auth/me", "", "", "")
	Send("POST", "/api/auth/logout", "", Identifier, Strings.Repeat("A", len(Body.CSRFToken)))
	Send("POST", "/api/auth/logout", "", Identifier, Body.CSRFToken)

	var Events []map[string]any
	for _, Line := range Strings.Split(Strings.TrimSpace(Buffer.String()), "\n") {
		var Record map[string]any
		if Err := Json.Unmarshal([]byte(Line), &Record); Err != nil {
			T.Fatalf("invalid JSON log line %q", Line)
		}
		if Event, _ := Record["event"].(string); Strings.HasPrefix(Event, "auth.") {
			Events = append(Events, Record)
		}
	}
	Want := []struct{ Event, Outcome, Reason, Actor string }{
		{"auth.login", "denied", "", ""},
		{"auth.login", "succeeded", "", "admin"},
		{"auth.session.rejected", "", "missing", ""},
		{"auth.csrf.rejected", "", "token", "admin"},
		{"auth.logout", "succeeded", "", "admin"},
	}
	if len(Events) != len(Want) {
		T.Fatalf("expected %d auth events, got %d: %v", len(Want), len(Events), Events)
	}
	for Index, Expect := range Want {
		Got := Events[Index]
		Actor, _ := Got["actor"].(string)
		Outcome, _ := Got["outcome"].(string)
		Reason, _ := Got["reason"].(string)
		if Got["event"] != Expect.Event || Outcome != Expect.Outcome || Reason != Expect.Reason || Actor != Expect.Actor {
			T.Fatalf("event %d = %v, want %+v", Index, Got, Expect)
		}
	}
	for _, Secret := range []string{TestPassword, "incorrect password", Identifier, Body.CSRFToken} {
		if Strings.Contains(Buffer.String(), Secret) {
			T.Fatal("authentication logs leaked a credential or session identifier")
		}
	}
}
