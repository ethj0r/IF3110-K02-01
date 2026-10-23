package Driver

import (
	Strings "strings"
	Testing "testing"
)

func TestValidIdentifier(T *Testing.T) {
	for _, Name := range []string{"public", "users", "_tmp", "Table1", "a" + Strings.Repeat("b", 62)} {
		if !ValidIdentifier(Name) {
			T.Fatalf("valid identifier rejected: %q", Name)
		}
	}
	for _, Name := range []string{"", "1abc", "has space", "has-dash", "schema.table", Strings.Repeat("a", 64), "táble"} {
		if ValidIdentifier(Name) {
			T.Fatalf("invalid identifier accepted: %q", Name)
		}
		if CheckIdentifier(Name) == nil {
			T.Fatalf("invalid identifier passed check: %q", Name)
		}
	}
}

func TestBuildDSN(T *Testing.T) {
	DSN, Err := BuildDSN("127.0.0.1", 55433, "target", "target", "target_dev")
	if Err != nil || !Strings.Contains(DSN, "127.0.0.1:55433") || !Strings.Contains(DSN, "/target") {
		T.Fatalf("unexpected DSN: %q %v", DSN, Err)
	}
	for _, Case := range []struct {
		Host, DB, User string
		Port           int
	}{{"", "target", "target", 55433}, {"127.0.0.1", "", "target", 55433}, {"127.0.0.1", "target", "", 55433}, {"127.0.0.1", "target", "target", 0}, {"127.0.0.1", "target", "target", 70000}} {
		if _, Err := BuildDSN(Case.Host, Case.Port, Case.DB, Case.User, "secret"); Err == nil {
			T.Fatalf("invalid connection parameters accepted: %+v", Case)
		}
	}
	if _, Err := BuildDSN("127.0.0.1", 55433, "target", "target", "s3cr3t"); Err != nil && Strings.Contains(Err.Error(), "s3cr3t") {
		T.Fatal("DSN error exposed credentials")
	}
}
