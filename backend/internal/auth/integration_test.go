//go:build integration

package Auth

import (
	Bytes "bytes"
	Context "context"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
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
	return &Fixture{Pool: Pool, Service: Server, Clock: Clock, Admin: Admin}
}

func (App *Fixture) Login(T *Testing.T) Authenticated {
	T.Helper()
	State, Err := App.Service.Login(Context.Background(), "admin", TestPassword, "")
	if Err != nil {
		T.Fatal(Err)
	}
	return State
}
