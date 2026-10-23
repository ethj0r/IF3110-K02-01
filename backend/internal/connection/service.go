package Connection

import (
	Context "context"
	Cipher "crypto/cipher"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
)

type Service struct {
	Cipher Cipher.AEAD
	Repo   *Repository
}

func (Server *Service) Create(Ctx Context.Context, Input ConnectionInput, CreatedBy *string) (Connection, error) {
	if Err := Input.Validate(); Err != nil {
		return Connection{}, Err
	}
	Sealed, Err := SealPassword(Server.Cipher, Input.Password)
	if Err != nil {
		return Connection{}, Err
	}
	return Server.Repo.Create(Ctx, Input, Sealed, CreatedBy)
}

func (Server *Service) Get(Ctx Context.Context, ID string) (Connection, error) {
	if ID == "" {
		return Connection{}, ErrNotFound
	}
	return Server.Repo.Get(Ctx, ID)
}

func (Server *Service) List(Ctx Context.Context) ([]Connection, error) {
	return Server.Repo.List(Ctx)
}

func (Server *Service) Update(Ctx Context.Context, ID string, Input ConnectionInput) (Connection, error) {
	if ID == "" {
		return Connection{}, ErrNotFound
	}
	if Err := Input.Validate(); Err != nil {
		return Connection{}, Err
	}
	Sealed, Err := SealPassword(Server.Cipher, Input.Password)
	if Err != nil {
		return Connection{}, Err
	}
	return Server.Repo.Update(Ctx, ID, Input, Sealed)
}

func (Server *Service) Delete(Ctx Context.Context, ID string) error {
	if ID == "" {
		return ErrNotFound
	}
	return Server.Repo.Delete(Ctx, ID)
}

func (Server *Service) DialDSN(Ctx Context.Context, ID string) (Connection, string, error) {
	Item, Err := Server.Get(Ctx, ID)
	if Err != nil {
		return Connection{}, "", Err
	}
	Raw, Err := Server.Repo.Ciphertext(Ctx, ID)
	if Err != nil {
		return Connection{}, "", Err
	}
	Password, Err := OpenPassword(Server.Cipher, Raw)
	if Err != nil {
		return Connection{}, "", ErrUnavailable
	}
	DSN, Err := Driver.BuildDSN(Item.Host, Item.Port, Item.DBName, Item.Username, Password)
	if Err != nil {
		return Connection{}, "", ErrInvalidInput
	}
	return Item, DSN, nil
}
