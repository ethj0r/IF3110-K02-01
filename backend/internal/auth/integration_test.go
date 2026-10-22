//go:build integration

package Auth

import (
	Bytes "bytes"
	Context "context"
	Json "encoding/json"
	Httpx "github.com/ethj0r/IF3110-K02-01/backend/internal/httpx"
	Chi "github.com/go-chi/chi/v5"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
	Http "net/http"
	Httptest "net/http/httptest"
	Atomic "sync/atomic"
	Testing "testing"
	Time "time"
)

const TestPassword = "correct horse battery staple"

type TestClock struct{ Value Atomic.Int64 }

func (Clock *TestClock) Now() Time.Time                 { return Time.Unix(0, Clock.Value.Load()).UTC() }
func (Clock *TestClock) Advance(Duration Time.Duration) { Clock.Value.Add(int64(Duration)) }

type Fixture struct {
	Pool    *Pgxpool.Pool
	Service *Service
	Handler *Handler
	Router  Http.Handler
	Clock   *TestClock
	Admin   Principal
}

func NewFixture(T *Testing.T) *Fixture {
	T.Helper()
	Pool := NewAuthDatabase(T)
	Repo := &Repository{Pool: Pool}
	Admin, Err := BootstrapAdmin(Context.Background(), Repo, "admin", TestPassword)
	if Err != nil {
		T.Fatal(Err)
	}
	Settings := DefaultOptions()
	Settings.Origin = "https://app.test"
	Settings.CookieSecure = true
	Settings.EncryptionKey = Bytes.Repeat([]byte{9}, 32)
	Settings.IdleTimeout = 5 * Time.Minute
	Settings.AbsoluteTimeout = Time.Hour
	Settings.RotationInterval = 2 * Time.Minute
	Settings.GracePeriod = 10 * Time.Second
	Server, Err := NewService(Repo, Settings)
	if Err != nil {
		T.Fatal(Err)
	}
	Clock := &TestClock{}
	Clock.Value.Store(Time.Now().UTC().Truncate(Time.Microsecond).UnixNano())
	Server.Now = Clock.Now
	Routes := &Handler{Service: Server}
	Router := Chi.NewRouter()
	Router.Route("/api", Routes.Register)
	Router.With(Routes.RequireSession).Post("/protected", func(W Http.ResponseWriter, R *Http.Request) {
		User, Ok := PrincipalFromContext(R.Context())
		if !Ok {
			T.Error("middleware did not provide principal")
		}
		Httpx.JSON(W, 200, User)
	})
	return &Fixture{Pool: Pool, Service: Server, Handler: Routes, Router: Router, Clock: Clock, Admin: Admin}
}

func (App *Fixture) Login(T *Testing.T) Authenticated {
	T.Helper()
	State, Err := App.Service.Login(Context.Background(), "admin", TestPassword, "")
	if Err != nil {
		T.Fatal(Err)
	}
	return State
}

func (App *Fixture) Request(Method, Path, Body, Identifier, CSRFToken string) *Httptest.ResponseRecorder {
	Request := Httptest.NewRequest(Method, Path, Bytes.NewBufferString(Body))
	Request.Header.Set("Origin", App.Service.Settings.Origin)
	if Body != "" {
		Request.Header.Set("Content-Type", "application/json")
	}
	if Identifier != "" {
		Request.AddCookie(&Http.Cookie{Name: CookieName, Value: Identifier})
	}
	if CSRFToken != "" {
		Request.Header.Set("X-CSRF-Token", CSRFToken)
	}
	Response := Httptest.NewRecorder()
	App.Router.ServeHTTP(Response, Request)
	return Response
}

func LoginBody(Username, Password string) string {
	Body, _ := Json.Marshal(map[string]string{"username": Username, "password": Password})
	return string(Body)
}
