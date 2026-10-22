package Auth

import (
	Base64 "encoding/base64"
	Strings "strings"
	Testing "testing"
)

func TestAuthOptions(T *Testing.T) {
	T.Setenv("APP_ENV", "development")
	T.Setenv("APP_ORIGIN", "http://127.0.0.1:5173")
	T.Setenv("SESSION_KEY", Base64.StdEncoding.EncodeToString([]byte(Strings.Repeat("k", 32))))
	Settings, Err := LoadOptions()
	if Err != nil || Settings.CookieSecure || Settings.IdleTimeout != DefaultOptions().IdleTimeout {
		T.Fatalf("invalid development options: %v", Err)
	}
	T.Setenv("APP_ENV", "production")
	if _, Err := LoadOptions(); Err == nil {
		T.Fatal("production accepted HTTP origin")
	}
	T.Setenv("APP_ORIGIN", "https://example.test/")
	Settings, Err = LoadOptions()
	if Err != nil || !Settings.CookieSecure || Settings.Origin != "https://example.test" {
		T.Fatal("production must use a secure cookie and canonical origin")
	}
	for _, Case := range []struct{ Name, Value string }{
		{"SESSION_KEY", "invalid-secret"}, {"SESSION_IDLE_TIMEOUT", "invalid"}, {"SESSION_GRACE_PERIOD", "1h"}, {"APP_ORIGIN", "https://example.test/path"}, {"APP_ORIGIN", "https://user:password@example.test"}, {"APP_ENV", "unknown"},
	} {
		T.Run(Case.Name+Case.Value, func(T *Testing.T) {
			T.Setenv(Case.Name, Case.Value)
			if _, Err := LoadOptions(); Err == nil || Strings.Contains(Err.Error(), "invalid-secret") || Strings.Contains(Err.Error(), "password@") {
				T.Fatal("invalid options must fail without exposing input")
			}
		})
	}
}
