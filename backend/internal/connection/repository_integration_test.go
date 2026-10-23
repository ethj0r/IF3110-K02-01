//go:build integration

package Connection

import (
	Bytes "bytes"
	Context "context"
	Errors "errors"
	Testutil "github.com/ethj0r/IF3110-K02-01/backend/internal/testutil"
	Migrations "github.com/ethj0r/IF3110-K02-01/backend/migrations"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
	Stdlib "github.com/jackc/pgx/v5/stdlib"
	Goose "github.com/pressly/goose/v3"
	Testing "testing"
)

func NewConnectionDatabase(T *Testing.T) *Pgxpool.Pool {
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

func NewConnectionService(T *Testing.T) *Service {
	T.Helper()
	AEAD, Err := NewCipher(Bytes.Repeat([]byte{11}, 32))
	if Err != nil {
		T.Fatal(Err)
	}
	return &Service{Cipher: AEAD, Repo: &Repository{Pool: NewConnectionDatabase(T)}}
}

func TestConnectionStoreRoundtrip(T *Testing.T) {
	Server := NewConnectionService(T)
	Ctx := Context.Background()
	Created, Err := Server.Create(Ctx, validInput(), nil)
	if Err != nil || Created.ID == "" {
		T.Fatalf("connection create failed: %v", Err)
	}
	if _, Err := Server.Create(Ctx, validInput(), nil); !Errors.Is(Err, ErrConflict) {
		T.Fatal("duplicate connection name was accepted")
	}
	Fetched, Err := Server.Get(Ctx, Created.ID)
	if Err != nil || Fetched.Name != "local-target" {
		T.Fatalf("connection fetch failed: %v", Err)
	}
	Listed, Err := Server.List(Ctx)
	if Err != nil || len(Listed) != 1 {
		T.Fatalf("connection list failed: %v", Err)
	}
	Changed := validInput()
	Changed.Host = "10.0.0.2"
	Updated, Err := Server.Update(Ctx, Created.ID, Changed)
	if Err != nil || Updated.Host != "10.0.0.2" {
		T.Fatalf("connection update failed: %v", Err)
	}
	Item, DSN, Err := Server.DialDSN(Ctx, Created.ID)
	if Err != nil || Item.ID != Created.ID || DSN == "" {
		T.Fatalf("connection DSN failed: %v", Err)
	}
	if Err := Server.Delete(Ctx, Created.ID); Err != nil {
		T.Fatalf("connection delete failed: %v", Err)
	}
	if _, Err := Server.Get(Ctx, Created.ID); !Errors.Is(Err, ErrNotFound) {
		T.Fatal("deleted connection still readable")
	}
	if _, _, Err := Server.DialDSN(Ctx, Created.ID); !Errors.Is(Err, ErrNotFound) {
		T.Fatal("deleted connection still dialable")
	}
}

func TestConnectionStoreRejectsInvalidInput(T *Testing.T) {
	Server := NewConnectionService(T)
	Ctx := Context.Background()
	Bad := validInput()
	Bad.Port = 0
	if _, Err := Server.Create(Ctx, Bad, nil); !Errors.Is(Err, ErrInvalidInput) {
		T.Fatal("invalid connection input was accepted")
	}
	if _, Err := Server.Get(Ctx, ""); !Errors.Is(Err, ErrNotFound) {
		T.Fatal("empty connection id was accepted")
	}
	if _, _, Err := Server.DialDSN(Ctx, "00000000-0000-0000-0000-000000000000"); !Errors.Is(Err, ErrNotFound) {
		T.Fatal("unknown connection was dialable")
	}
}

func TestConnectionSecretsNeverStoredPlaintext(T *Testing.T) {
	Server := NewConnectionService(T)
	Ctx := Context.Background()
	Created, Err := Server.Create(Ctx, validInput(), nil)
	if Err != nil {
		T.Fatal(Err)
	}
	var Raw []byte
	if Err := Server.Repo.Pool.QueryRow(Ctx, "SELECT password_ciphertext FROM db_connections WHERE id = $1", Created.ID).Scan(&Raw); Err != nil {
		T.Fatal(Err)
	}
	if Bytes.Contains(Raw, []byte("target_dev")) {
		T.Fatal("plaintext password reached database storage")
	}
}
