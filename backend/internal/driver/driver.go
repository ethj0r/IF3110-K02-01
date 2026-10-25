package Driver

import (
	Errors "errors"
	Strings "strings"
	Utf8 "unicode/utf8"
)

var (
	ErrInvalidName  = Errors.New("invalid database object name")
	ErrUnavailable  = Errors.New("target database is unavailable")
	ErrInvalidDSN   = Errors.New("invalid target database configuration")
	ErrInvalidInput = Errors.New("invalid connection parameters")
	ErrExecution    = Errors.New("target SQL execution failed")
)

func ValidIdentifier(Name string) bool {
	return Name != "" && len(Name) <= 63 && Utf8.ValidString(Name) && !Strings.ContainsRune(Name, 0)
}

func CheckIdentifier(Name string) error {
	if !ValidIdentifier(Name) {
		return ErrInvalidName
	}
	return nil
}
