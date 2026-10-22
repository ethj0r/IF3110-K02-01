//go:build integration

package Auth

import (
	Context "context"
	Errors "errors"
	Testutil "github.com/ethj0r/IF3110-K02-01/backend/internal/testutil"
	Migrations "github.com/ethj0r/IF3110-K02-01/backend/migrations"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
	Stdlib "github.com/jackc/pgx/v5/stdlib"
	Goose "github.com/pressly/goose/v3"
	Sync "sync"
	Testing "testing"
)

func NewAuthDatabase(T *Testing.T) *Pgxpool.Pool {
	T.Helper()
	Pool := Testutil.Database(T)
	Db := Stdlib.OpenDB(*Pool.Config().ConnConfig)
	defer Db.Close()
	Provider, Err := Goose.NewProvider(Goose.DialectPostgres, Db, Migrations.FS())
	if Err != nil {
		T.Fatal(Err)
	}
	if _, Err := Provider.Up(Context.Background()); Err != nil {
		T.Fatal(Err)
	}
	return Pool
}

func TestAdminBootstrap(T *Testing.T) {
	Repo := &Repository{Pool: NewAuthDatabase(T)}
	First, Err := BootstrapAdmin(Context.Background(), Repo, "admin", "original admin password")
	if Err != nil || !First.IsPlatformAdmin {
		T.Fatal("administrator bootstrap failed", Err)
	}
	Again, Err := BootstrapAdmin(Context.Background(), Repo, "ADMIN", "different admin password")
	if Err != nil || Again.ID != First.ID {
		T.Fatal("bootstrap must be idempotent")
	}
	User, Err := Repo.FindUser(Context.Background(), "AdMiN")
	if Err != nil || !VerifyPassword("original admin password", User.PasswordHash) || VerifyPassword("different admin password", User.PasswordHash) {
		T.Fatal("bootstrap overwrote credentials")
	}
	if _, Err := BootstrapAdmin(Context.Background(), Repo, "another", "another admin password"); !Errors.Is(Err, ErrAdminExists) {
		T.Fatal("bootstrap created another administrator")
	}
	if _, Err := BootstrapAdmin(Context.Background(), Repo, "invalid name", "another admin password"); Err == nil {
		T.Fatal("invalid username accepted")
	}
	if _, Err := BootstrapAdmin(Context.Background(), Repo, "another", "short"); Err == nil {
		T.Fatal("invalid password accepted")
	}
}

func TestBootstrapDoesNotPromoteExistingUser(T *Testing.T) {
	Repo := &Repository{Pool: NewAuthDatabase(T)}
	Hash, _ := HashPassword("regular user password")
	if _, Err := Repo.Pool.Exec(Context.Background(), "INSERT INTO users (username, password_hash) VALUES ('regular', $1)", Hash); Err != nil {
		T.Fatal(Err)
	}
	if _, Err := BootstrapAdmin(Context.Background(), Repo, "regular", "another user password"); !Errors.Is(Err, ErrAdminExists) {
		T.Fatal("bootstrap promoted an existing account")
	}
}

func TestConcurrentBootstrap(T *Testing.T) {
	Repo := &Repository{Pool: NewAuthDatabase(T)}
	var Workers Sync.WaitGroup
	Results := make(chan error, 2)
	for _, Username := range []string{"first", "second"} {
		Workers.Add(1)
		go func(Username string) {
			defer Workers.Done()
			_, Err := BootstrapAdmin(Context.Background(), Repo, Username, "initial administrator password")
			Results <- Err
		}(Username)
	}
	Workers.Wait()
	close(Results)
	Successes := 0
	for Err := range Results {
		if Err == nil {
			Successes++
		} else if !Errors.Is(Err, ErrAdminExists) {
			T.Fatal(Err)
		}
	}
	if Successes != 1 {
		T.Fatal("concurrent bootstrap must create one administrator")
	}
}
