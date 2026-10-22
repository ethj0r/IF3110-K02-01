//go:build integration

package Auth

import (
	Context "context"
	Json "encoding/json"
	Http "net/http"
	Httptest "net/http/httptest"
	Strings "strings"
	Testing "testing"
	Time "time"
)

func TestAuthenticationEndpoints(T *Testing.T) {
	App := NewFixture(T)
	Unknown := App.Request("POST", "/api/auth/login", LoginBody("unknown", TestPassword), "", "")
	Wrong := App.Request("POST", "/api/auth/login", LoginBody("admin", "incorrect password"), "", "")
	if Unknown.Code != 401 || Unknown.Body.String() != Wrong.Body.String() {
		T.Fatal("login disclosed whether a username exists")
	}
	Response := App.Request("POST", "/api/auth/login", LoginBody("ADMIN", TestPassword), "", "")
	if Response.Code != 200 {
		T.Fatalf("login failed: %d %s", Response.Code, Response.Body.String())
	}
	Cookies := Response.Result().Cookies()
	if len(Cookies) != 1 || Cookies[0].Name != CookieName || !Cookies[0].HttpOnly || !Cookies[0].Secure || Cookies[0].SameSite != Http.SameSiteLaxMode || Cookies[0].Path != "/" {
		T.Fatal("session cookie is missing required protections")
	}
	Identifier := Cookies[0].Value
	var Body struct {
		User      Principal `json:"user"`
		CSRFToken string    `json:"csrf_token"`
		ExpiresAt Time.Time `json:"expires_at"`
	}
	if Err := Json.Unmarshal(Response.Body.Bytes(), &Body); Err != nil || Body.User.ID != App.Admin.ID || !ValidToken(Body.CSRFToken) {
		T.Fatal("unexpected login response")
	}
	if Strings.Contains(Response.Body.String(), Identifier) || Strings.Contains(Response.Body.String(), TestPassword) || Strings.Contains(Response.Body.String(), "password_hash") {
		T.Fatal("login response leaked credentials")
	}
	Me := App.Request("GET", "/api/auth/me", "", Identifier, "")
	if Me.Code != 200 || Me.Header().Get("Cache-Control") != "no-store" {
		T.Fatal("me must authenticate without caching")
	}
	CSRF := App.Request("GET", "/api/auth/csrf", "", Identifier, "")
	if CSRF.Code != 200 || !Strings.Contains(CSRF.Body.String(), Body.CSRFToken) {
		T.Fatal("CSRF endpoint returned an unrelated token")
	}
	Protected := App.Request("POST", "/protected", `{}`, Identifier, Body.CSRFToken)
	if Protected.Code != 200 || !Strings.Contains(Protected.Body.String(), App.Admin.ID) {
		T.Fatal("protected route did not receive principal")
	}
	App.Clock.Advance(App.Service.Settings.RotationInterval)
	Rotated := App.Request("GET", "/api/auth/me", "", Identifier, "")
	RotatedCookies := Rotated.Result().Cookies()
	if Rotated.Code != 200 || len(RotatedCookies) != 1 || RotatedCookies[0].Value == Identifier || !RotatedCookies[0].Expires.Equal(Cookies[0].Expires) || RotatedCookies[0].MaxAge >= Cookies[0].MaxAge {
		T.Fatal("cookie rotation extended expiration")
	}
	Logout := App.Request("POST", "/api/auth/logout", "", RotatedCookies[0].Value, Body.CSRFToken)
	if Logout.Code != 204 || len(Logout.Result().Cookies()) != 1 || Logout.Result().Cookies()[0].MaxAge != -1 {
		T.Fatal("logout must revoke and clear the session")
	}
	for _, Token := range []string{Identifier, RotatedCookies[0].Value} {
		if App.Request("GET", "/api/auth/me", "", Token, "").Code != 401 {
			T.Fatal("logout left an identifier usable")
		}
	}
}

func TestCSRFAndLoginRequestRejection(T *Testing.T) {
	App := NewFixture(T)
	State := App.Login(T)
	for _, Token := range []string{"", "invalid", Strings.Repeat("A", 43)} {
		if App.Request("POST", "/api/auth/logout", "", State.Identifier, Token).Code != 403 {
			T.Fatal("invalid CSRF token accepted")
		}
	}
	for _, Origin := range []string{"", "null", "https://app.test.attacker.test"} {
		Request := Httptest.NewRequest("POST", "/api/auth/logout", nil)
		Request.AddCookie(&Http.Cookie{Name: CookieName, Value: State.Identifier})
		Request.Header.Set("Origin", Origin)
		Request.Header.Set("X-CSRF-Token", State.CSRFToken)
		Response := Httptest.NewRecorder()
		App.Router.ServeHTTP(Response, Request)
		if Response.Code != 403 {
			T.Fatal("untrusted origin was accepted")
		}
	}
	if App.Request("GET", "/api/auth/me", "", State.Identifier, "").Code != 200 {
		T.Fatal("CSRF rejection revoked a valid session")
	}
	if App.Request("GET", "/api/auth/me?session_id="+State.Identifier, "", "", "").Code != 401 {
		T.Fatal("session identifier was accepted from URL")
	}
	for _, Case := range []struct {
		Body, ContentType, Origin string
		Status                    int
	}{
		{LoginBody("admin", TestPassword), "application/json", "https://attacker.test", 403},
		{"username=admin&password=anything", "application/x-www-form-urlencoded", App.Service.Settings.Origin, 415},
		{`{"username":"admin","password":"any","extra":true}`, "application/json", App.Service.Settings.Origin, 400},
		{`{} {}`, "application/json", App.Service.Settings.Origin, 400},
		{Strings.Repeat("x", 9000), "application/json", App.Service.Settings.Origin, 400},
	} {
		Request := Httptest.NewRequest("POST", "/api/auth/login", Strings.NewReader(Case.Body))
		Request.Header.Set("Origin", Case.Origin)
		Request.Header.Set("Content-Type", Case.ContentType)
		Response := Httptest.NewRecorder()
		App.Router.ServeHTTP(Response, Request)
		if Response.Code != Case.Status {
			T.Fatalf("unsafe login request status = %d, want %d", Response.Code, Case.Status)
		}
	}
	Request := Httptest.NewRequest("GET", "/api/auth/me", nil)
	Request.AddCookie(&Http.Cookie{Name: CookieName, Value: State.Identifier})
	Request.AddCookie(&Http.Cookie{Name: CookieName, Value: State.Identifier})
	Response := Httptest.NewRecorder()
	App.Router.ServeHTTP(Response, Request)
	if Response.Code != 401 {
		T.Fatal("ambiguous cookies accepted")
	}
}

func TestUnavailableAuthDoesNotExposeDatabaseError(T *Testing.T) {
	App := NewFixture(T)
	State := App.Login(T)
	App.Pool.Close()
	Response := App.Request("GET", "/api/auth/me", "", State.Identifier, "")
	if Response.Code != 503 || Strings.Contains(Response.Body.String(), "postgres") || len(Response.Result().Cookies()) != 0 {
		T.Fatal("database failure must return sanitized error without deleting a valid cookie")
	}
}

func TestStoredSessionSecretsAreProtected(T *Testing.T) {
	App := NewFixture(T)
	State := App.Login(T)
	var Hash, Ciphertext []byte
	if Err := App.Pool.QueryRow(Context.Background(), "SELECT I.token_hash, S.secret_ciphertext FROM sessions S JOIN session_identifiers I ON I.session_id = S.id WHERE S.id = $1", State.SessionID).Scan(&Hash, &Ciphertext); Err != nil {
		T.Fatal(Err)
	}
	if string(Hash) == State.Identifier || Strings.Contains(string(Ciphertext), State.Identifier) || Strings.Contains(string(Ciphertext), State.CSRFToken) {
		T.Fatal("plaintext session secrets reached database storage")
	}
}
