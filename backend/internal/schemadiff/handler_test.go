package Schemadiff

import (
	Errors "errors"
	Httptest "net/http/httptest"
	Strings "strings"
	Testing "testing"
)

func TestComparisonErrorsDoNotExposeCredentialsOrSQL(T *Testing.T) {
	for _, Case := range []struct {
		Err    error
		Status int
	}{
		{ErrInvalidInput, 400}, {ErrForbidden, 403}, {ErrNotFound, 404},
		{ErrUnsupported, 422}, {ErrTooLarge, 413}, {ErrUnavailable, 502},
		{Errors.New("private-dsn password=private-secret SQL private-query"), 502},
	} {
		Response := Httptest.NewRecorder()
		WriteError(Response, Case.Err)
		if Response.Code != Case.Status || Strings.Contains(Response.Body.String(), "private-") {
			T.Fatal("comparison error is not sanitized")
		}
	}
}
