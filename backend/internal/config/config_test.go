package Config

import (
	Strings "strings"
	Testing "testing"
)

func TestLoad(T *Testing.T) {
	T.Setenv("DATABASE_URL", "postgres://dev:secret@localhost:5432/platform?sslmode=disable")
	T.Setenv("HTTP_ADDR", "127.0.0.1:8081")
	Cfg, Err := Load()
	if Err != nil || Cfg.HTTPAddr != "127.0.0.1:8081" || Cfg.DatabaseURL == "" {
		T.Fatalf("expected valid configuration, got error %v", Err)
	}
}

func TestLoadRejectsInvalidConfiguration(T *Testing.T) {
	for _, Tc := range []struct{ Name, Addr, Database string }{
		{"missing database", "127.0.0.1:8080", ""},
		{"invalid address", "invalid", "postgres://localhost/platform"},
		{"invalid scheme", "127.0.0.1:8080", "mysql://dev:secret@localhost/platform"},
		{"missing database name", "127.0.0.1:8080", "postgres://dev:secret@localhost/"},
		{"malformed credentials", "127.0.0.1:8080", "postgres://dev:secret%@localhost/platform"},
	} {
		T.Run(Tc.Name, func(T *Testing.T) {
			T.Setenv("HTTP_ADDR", Tc.Addr)
			T.Setenv("DATABASE_URL", Tc.Database)
			_, Err := Load()
			if Err == nil {
				T.Fatal("expected invalid configuration to fail")
			}
			if Strings.Contains(Err.Error(), "secret") {
				T.Fatal("configuration error exposed credentials")
			}
		})
	}
}
