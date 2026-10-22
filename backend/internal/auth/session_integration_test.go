//go:build integration

package Auth

import (
	Context "context"
	Errors "errors"
	Sync "sync"
	Testing "testing"
	Time "time"
)

func TestSessionRotationAndGrace(T *Testing.T) {
	App := NewFixture(T)
	Original := App.Login(T)
	App.Clock.Advance(2 * Time.Minute)
	Rotated, Err := App.Service.Authenticate(Context.Background(), Original.Identifier)
	if Err != nil || Rotated.Identifier == Original.Identifier || !Rotated.SetCookie || !Rotated.AbsoluteExpiresAt.Equal(Original.AbsoluteExpiresAt) {
		T.Fatalf("periodic rotation failed: %v", Err)
	}
	Recovered, Err := App.Service.Authenticate(Context.Background(), Original.Identifier)
	if Err != nil || Recovered.Identifier != Rotated.Identifier || !Recovered.SetCookie {
		T.Fatal("grace request must recover the current identifier")
	}
	App.Clock.Advance(App.Service.Settings.GracePeriod)
	if _, Err := App.Service.Authenticate(Context.Background(), Original.Identifier); !Errors.Is(Err, ErrUnauthorized) {
		T.Fatal("old identifier must expire at grace boundary")
	}
	if _, Err := App.Service.Authenticate(Context.Background(), Rotated.Identifier); Err != nil {
		T.Fatal("current identifier was invalidated")
	}
	Relogin, Err := App.Service.Login(Context.Background(), "ADMIN", TestPassword, Rotated.Identifier)
	if Err != nil || Relogin.Identifier == Rotated.Identifier || Relogin.SessionID != Rotated.SessionID || !Relogin.AbsoluteExpiresAt.Equal(Original.AbsoluteExpiresAt) {
		T.Fatal("login rotation must preserve the active session absolute expiration")
	}
	if _, Err := App.Service.Authenticate(Context.Background(), Rotated.Identifier); Err != nil {
		T.Fatal("login rotation must preserve grace")
	}
}

func TestConcurrentRotation(T *Testing.T) {
	App := NewFixture(T)
	State := App.Login(T)
	App.Clock.Advance(App.Service.Settings.RotationInterval)
	Results := make(chan Authenticated, 12)
	Failures := make(chan error, 12)
	var Workers Sync.WaitGroup
	for Range := 0; Range < 12; Range++ {
		Workers.Add(1)
		go func() {
			defer Workers.Done()
			Result, Err := App.Service.Authenticate(Context.Background(), State.Identifier)
			if Err != nil {
				Failures <- Err
				return
			}
			Results <- Result
		}()
	}
	Workers.Wait()
	close(Results)
	close(Failures)
	for Err := range Failures {
		T.Fatal(Err)
	}
	Current := ""
	for Result := range Results {
		if Current == "" {
			Current = Result.Identifier
		}
		if Result.Identifier != Current {
			T.Fatal("concurrent requests generated conflicting identifiers")
		}
	}
	var Count int
	if Err := App.Pool.QueryRow(Context.Background(), "SELECT count(*) FROM session_identifiers WHERE session_id = $1", State.SessionID).Scan(&Count); Err != nil || Count != 2 {
		T.Fatal("rotation must create exactly one replacement")
	}
}

func TestSessionIdleAndAbsoluteExpiration(T *Testing.T) {
	T.Run("idle boundary", func(T *Testing.T) {
		App := NewFixture(T)
		State := App.Login(T)
		App.Clock.Advance(App.Service.Settings.IdleTimeout)
		if _, Err := App.Service.Authenticate(Context.Background(), State.Identifier); !Errors.Is(Err, ErrUnauthorized) {
			T.Fatal("idle session remained active")
		}
		var Revoked bool
		if Err := App.Pool.QueryRow(Context.Background(), "SELECT revoked_at IS NOT NULL FROM sessions WHERE id = $1", State.SessionID).Scan(&Revoked); Err != nil || !Revoked {
			T.Fatal("idle expiration must revoke backend session")
		}
	})
	T.Run("absolute despite activity", func(T *Testing.T) {
		App := NewFixture(T)
		App.Service.Settings.AbsoluteTimeout = 5 * Time.Minute
		State := App.Login(T)
		Deadline := State.AbsoluteExpiresAt
		for Minute := 0; Minute < 4; Minute++ {
			App.Clock.Advance(Time.Minute)
			var Err error
			State, Err = App.Service.Authenticate(Context.Background(), State.Identifier)
			if Err != nil || !State.AbsoluteExpiresAt.Equal(Deadline) {
				T.Fatal("rotation changed absolute expiration")
			}
		}
		App.Clock.Advance(Time.Minute)
		if _, Err := App.Service.Authenticate(Context.Background(), State.Identifier); !Errors.Is(Err, ErrUnauthorized) {
			T.Fatal("activity extended absolute expiration")
		}
		Fresh, Err := App.Service.Login(Context.Background(), "admin", TestPassword, State.Identifier)
		if Err != nil || Fresh.SessionID == State.SessionID {
			T.Fatal("expired session must require a fresh login session")
		}
	})
}

func TestPrivilegeChangesAndRevocation(T *Testing.T) {
	App := NewFixture(T)
	State := App.Login(T)
	Tx, Err := App.Pool.Begin(Context.Background())
	if Err != nil {
		T.Fatal(Err)
	}
	if Err := MarkPrivilegeChange(Context.Background(), Tx, State.Principal.ID); Err != nil {
		T.Fatal(Err)
	}
	if Err := Tx.Commit(Context.Background()); Err != nil {
		T.Fatal(Err)
	}
	Rotated, Err := App.Service.Authenticate(Context.Background(), State.Identifier)
	if Err != nil || Rotated.Identifier == State.Identifier || !Rotated.AbsoluteExpiresAt.Equal(State.AbsoluteExpiresAt) {
		T.Fatal("privilege change must rotate without extending expiration")
	}
	Tx, Err = App.Pool.Begin(Context.Background())
	if Err != nil {
		T.Fatal(Err)
	}
	if Err := InvalidateUserSessions(Context.Background(), Tx, State.Principal.ID, App.Clock.Now()); Err != nil {
		T.Fatal(Err)
	}
	if Err := Tx.Rollback(Context.Background()); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := App.Service.Authenticate(Context.Background(), Rotated.Identifier); Err != nil {
		T.Fatal("rolled-back revocation changed sessions")
	}
	Tx, Err = App.Pool.Begin(Context.Background())
	if Err != nil {
		T.Fatal(Err)
	}
	if Err := InvalidateUserSessions(Context.Background(), Tx, State.Principal.ID, App.Clock.Now()); Err != nil {
		T.Fatal(Err)
	}
	if Err := Tx.Commit(Context.Background()); Err != nil {
		T.Fatal(Err)
	}
	for _, Identifier := range []string{State.Identifier, Rotated.Identifier} {
		if _, Err := App.Service.Authenticate(Context.Background(), Identifier); !Errors.Is(Err, ErrUnauthorized) {
			T.Fatal("access revocation must invalidate every identifier")
		}
	}
}

func TestCoreUserChangesInvalidateSessions(T *Testing.T) {
	for _, Change := range []string{"is_active = FALSE", "is_platform_admin = FALSE", "password_hash = 'changed'"} {
		T.Run(Change, func(T *Testing.T) {
			App := NewFixture(T)
			First := App.Login(T)
			Second := App.Login(T)
			if _, Err := App.Pool.Exec(Context.Background(), "UPDATE users SET "+Change+" WHERE id = $1", App.Admin.ID); Err != nil {
				T.Fatal(Err)
			}
			for _, State := range []Authenticated{First, Second} {
				if _, Err := App.Service.Authenticate(Context.Background(), State.Identifier); !Errors.Is(Err, ErrUnauthorized) {
					T.Fatal("user changes must revoke all backend sessions")
				}
			}
		})
	}
}

func TestPromotionAndCorruptSession(T *Testing.T) {
	App := NewFixture(T)
	if _, Err := App.Pool.Exec(Context.Background(), "UPDATE users SET is_platform_admin = FALSE WHERE id = $1", App.Admin.ID); Err != nil {
		T.Fatal(Err)
	}
	State := App.Login(T)
	if _, Err := App.Pool.Exec(Context.Background(), "UPDATE users SET is_platform_admin = TRUE WHERE id = $1", App.Admin.ID); Err != nil {
		T.Fatal(Err)
	}
	Promoted, Err := App.Service.Authenticate(Context.Background(), State.Identifier)
	if Err != nil || !Promoted.Principal.IsPlatformAdmin || Promoted.Identifier == State.Identifier {
		T.Fatal("admin promotion must rotate before granting the new principal")
	}
	if _, Err := App.Pool.Exec(Context.Background(), "UPDATE sessions SET secret_ciphertext = $2 WHERE id = $1", State.SessionID, []byte("corrupt")); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := App.Service.Authenticate(Context.Background(), Promoted.Identifier); !Errors.Is(Err, ErrUnauthorized) {
		T.Fatal("corrupt session must fail closed and require login")
	}
}
