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

type Column struct {
	Name     string  `json:"name"`
	DataType string  `json:"data_type"`
	Nullable bool    `json:"nullable"`
	Default  *string `json:"default"`
}

type Constraint struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Columns string `json:"columns"`
}

type Index struct {
	Name       string `json:"name"`
	Definition string `json:"definition"`
}

type TablePage struct {
	Columns  []string `json:"columns"`
	Rows     [][]any  `json:"rows"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
	Total    int      `json:"total"`
}
