package Driver

import (
	Errors "errors"
	Regexp "regexp"
)

var (
	ErrInvalidName  = Errors.New("invalid database object name")
	ErrUnavailable  = Errors.New("target database is unavailable")
	ErrInvalidDSN   = Errors.New("invalid target database configuration")
	ErrInvalidInput = Errors.New("invalid connection parameters")
)

var identifierPattern = Regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

func ValidIdentifier(Name string) bool {
	return identifierPattern.MatchString(Name)
}

func CheckIdentifier(Name string) error {
	if !ValidIdentifier(Name) {
		return ErrInvalidName
	}
	return nil
}
