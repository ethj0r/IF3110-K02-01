package Migration

import (
	Errors "errors"
	Strings "strings"
	Time "time"
)

type Status string

const (
	StatusDraft      Status = "draft"
	StatusPending    Status = "pending"
	StatusCommitted  Status = "committed"
	StatusApplied    Status = "applied"
	StatusRolledBack Status = "rolled_back"
	StatusFailed     Status = "failed"
)

func (S Status) IsTerminal() bool {
	return S == StatusCommitted || S == StatusApplied || S == StatusRolledBack || S == StatusFailed
}

func (S Status) IsSuccess() bool {
	return S == StatusCommitted || S == StatusApplied
}

func (S Status) IsFailure() bool {
	return S == StatusRolledBack || S == StatusFailed
}

func (S Status) IsDraft() bool {
	return S == StatusDraft || S == StatusPending
}

var (
	ErrInvalidInput    = Errors.New("invalid migration input")
	ErrNotFound        = Errors.New("migration not found")
	ErrConflict        = Errors.New("migration version already exists")
	ErrAlreadyExecuted = Errors.New("migration has already been executed")
	ErrUnavailable     = Errors.New("migration store is unavailable")
	ErrForbidden       = Errors.New("permission denied for database migration")
)

type Migration struct {
	ID           string     `json:"id"`
	DatabaseID   string     `json:"database_id"`
	Version      int64      `json:"version"`
	Title        string     `json:"title"`
	SQLText      string     `json:"sql_text"`
	Status       Status     `json:"status"`
	ErrorMessage *string    `json:"error_message,omitempty"`
	CreatedBy    *string    `json:"created_by,omitempty"`
	ExecutedBy   *string    `json:"executed_by,omitempty"`
	CreatedAt    Time.Time  `json:"created_at"`
	UpdatedAt    Time.Time  `json:"updated_at"`
	ExecutedAt   *Time.Time `json:"executed_at,omitempty"`
}

type MigrationInput struct {
	Title   string `json:"title"`
	SQLText string `json:"sql_text"`
}

func (Input MigrationInput) Validate() error {
	Title := Strings.TrimSpace(Input.Title)
	if len(Title) < 1 || len(Title) > 128 {
		return ErrInvalidInput
	}
	SQLText := Strings.TrimSpace(Input.SQLText)
	if len(SQLText) < 1 {
		return ErrInvalidInput
	}
	return nil
}

func (Input MigrationInput) Normalized() MigrationInput {
	return MigrationInput{
		Title:   Strings.TrimSpace(Input.Title),
		SQLText: Strings.TrimSpace(Input.SQLText),
	}
}

