package Inspect

import (
	Errors "errors"
)

var (
	ErrInvalidInput = Errors.New("invalid browse parameters")
	ErrNotFound     = Errors.New("database connection not found")
	ErrUnavailable  = Errors.New("target database is unavailable")
)

type Database struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Table struct {
	Name string `json:"name"`
	Type string `json:"type"`
}
