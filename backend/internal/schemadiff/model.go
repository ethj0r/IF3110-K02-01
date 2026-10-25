package Schemadiff

import (
	Errors "errors"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
	Strings "strings"
)

var (
	ErrInvalidInput = Errors.New("invalid schema comparison")
	ErrForbidden    = Errors.New("schema comparison access denied")
	ErrUnavailable  = Errors.New("schema comparison unavailable")
	ErrNotFound     = Errors.New("comparison database not found")
	ErrUnsupported  = Errors.New("schema change is not supported")
	ErrTooLarge     = Errors.New("generated migration exceeds size limit")
)

const MaxSQLBytes = 48000

type Input struct {
	ReferenceDatabaseID string `json:"reference_database_id"`
	Schema              string `json:"schema"`
}

func (Value Input) Normalized() Input {
	Value.ReferenceDatabaseID = Strings.TrimSpace(Value.ReferenceDatabaseID)
	if Value.Schema == "" {
		Value.Schema = "public"
	}
	return Value
}

func (Value Input) Validate() error {
	if Value.ReferenceDatabaseID == "" || len(Value.ReferenceDatabaseID) > 64 || !Driver.ValidIdentifier(Value.Schema) || Value.Schema == "information_schema" || Strings.HasPrefix(Value.Schema, "pg_") {
		return ErrInvalidInput
	}
	return nil
}

type Hazard struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type Statement struct {
	SQL     string   `json:"sql"`
	Hazards []Hazard `json:"hazards"`
}

type Result struct {
	CurrentDatabaseID    string      `json:"current_database_id"`
	ReferenceDatabaseID  string      `json:"reference_database_id"`
	Schema               string      `json:"schema"`
	CurrentSchemaHash    string      `json:"current_schema_hash"`
	RequiresRecomparison bool        `json:"requires_recomparison"`
	Statements           []Statement `json:"statements"`
	SQLText              string      `json:"sql_text"`
}
