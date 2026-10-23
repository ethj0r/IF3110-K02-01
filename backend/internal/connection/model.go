package Connection

import (
	Errors "errors"
	Strings "strings"
	Time "time"
)

var (
	ErrInvalidInput = Errors.New("invalid connection parameters")
	ErrNotFound     = Errors.New("database connection not found")
	ErrConflict     = Errors.New("database connection name is already used")
	ErrUnavailable  = Errors.New("connection store is unavailable")
)

type Connection struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	DBName    string    `json:"dbname"`
	Username  string    `json:"username"`
	CreatedAt Time.Time `json:"created_at"`
	UpdatedAt Time.Time `json:"updated_at"`
}

type ConnectionInput struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	DBName   string `json:"dbname"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (Input ConnectionInput) Validate() error {
	if len(Strings.TrimSpace(Input.Name)) < 1 || len(Input.Name) > 64 {
		return ErrInvalidInput
	}
	if len(Strings.TrimSpace(Input.Host)) < 1 || len(Input.Host) > 255 {
		return ErrInvalidInput
	}
	if Input.Port < 1 || Input.Port > 65535 {
		return ErrInvalidInput
	}
	if len(Strings.TrimSpace(Input.DBName)) < 1 || len(Input.DBName) > 63 {
		return ErrInvalidInput
	}
	if len(Strings.TrimSpace(Input.Username)) < 1 || len(Input.Username) > 63 {
		return ErrInvalidInput
	}
	if len(Input.Password) < 1 || len(Input.Password) > 1024 {
		return ErrInvalidInput
	}
	return nil
}

func (Input ConnectionInput) Normalized() ConnectionInput {
	Result := Input
	Result.Name = Strings.TrimSpace(Result.Name)
	Result.Host = Strings.TrimSpace(Result.Host)
	Result.DBName = Strings.TrimSpace(Result.DBName)
	Result.Username = Strings.TrimSpace(Result.Username)
	return Result
}
