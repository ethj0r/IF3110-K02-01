package Admin

import (
	Errors "errors"
	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Regexp "regexp"
	Strings "strings"
	Time "time"
)

var (
	ErrInvalidInput = Errors.New("invalid administration request")
	ErrNotFound     = Errors.New("administration resource not found")
	ErrConflict     = Errors.New("administration resource already exists")
	ErrLastAdmin    = Errors.New("at least one active platform administrator is required")
	ErrUnavailable  = Errors.New("administration store is unavailable")
)

var GroupNamePattern = Regexp.MustCompile(`^[A-Za-z0-9_. -]{1,64}$`)

type User struct {
	ID              string     `json:"id"`
	Username        string     `json:"username"`
	IsPlatformAdmin bool       `json:"is_platform_admin"`
	IsActive        bool       `json:"is_active"`
	CreatedAt       Time.Time  `json:"created_at"`
	Groups          []GroupRef `json:"groups"`
}

type GroupRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Group struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	MemberCount int       `json:"member_count"`
	AccessCount int       `json:"access_count"`
	CreatedAt   Time.Time `json:"created_at"`
	UpdatedAt   Time.Time `json:"updated_at"`
}

type Member struct {
	ID              string `json:"id"`
	Username        string `json:"username"`
	IsPlatformAdmin bool   `json:"is_platform_admin"`
	IsActive        bool   `json:"is_active"`
}

type GroupAccess struct {
	DatabaseID   string       `json:"database_id"`
	DatabaseName string       `json:"database_name"`
	Host         string       `json:"host"`
	DBName       string       `json:"dbname"`
	Level        Access.Level `json:"level"`
}

type DatabaseRef struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Host   string `json:"host"`
	DBName string `json:"dbname"`
}

type Grant struct {
	GroupID    string       `json:"group_id"`
	DatabaseID string       `json:"database_id"`
	Level      Access.Level `json:"level"`
}

type AccessMatrix struct {
	Groups    []Group       `json:"groups"`
	Databases []DatabaseRef `json:"databases"`
	Grants    []Grant       `json:"grants"`
}

type CreateUserInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (Input CreateUserInput) Normalized() CreateUserInput {
	Result := Input
	Result.Username = Strings.TrimSpace(Result.Username)
	return Result
}

func (Input CreateUserInput) Validate() error {
	if !Auth.UsernamePattern.MatchString(Input.Username) {
		return ErrInvalidInput
	}
	if len(Input.Password) < 12 || len(Input.Password) > 1024 {
		return ErrInvalidInput
	}
	return nil
}

type UpdateUserInput struct {
	IsActive *bool `json:"is_active"`
}

func (Input UpdateUserInput) Validate() error {
	if Input.IsActive == nil {
		return ErrInvalidInput
	}
	return nil
}

type PasswordInput struct {
	Password string `json:"password"`
}

func (Input PasswordInput) Validate() error {
	if len(Input.Password) < 12 || len(Input.Password) > 1024 {
		return ErrInvalidInput
	}
	return nil
}

type CreateGroupInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (Input CreateGroupInput) Normalized() CreateGroupInput {
	Result := Input
	Result.Name = Strings.TrimSpace(Result.Name)
	Result.Description = Strings.TrimSpace(Result.Description)
	return Result
}

func (Input CreateGroupInput) Validate() error {
	if !GroupNamePattern.MatchString(Input.Name) || len(Input.Description) > 500 {
		return ErrInvalidInput
	}
	return nil
}

type UpdateGroupInput struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

func (Input UpdateGroupInput) Validate() error {
	if Input.Name == nil && Input.Description == nil {
		return ErrInvalidInput
	}
	if Input.Name != nil {
		Name := Strings.TrimSpace(*Input.Name)
		if !GroupNamePattern.MatchString(Name) {
			return ErrInvalidInput
		}
	}
	if Input.Description != nil && len(Strings.TrimSpace(*Input.Description)) > 500 {
		return ErrInvalidInput
	}
	return nil
}

type AccessInput struct {
	Level string `json:"level"`
}
