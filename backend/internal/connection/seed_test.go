package Connection

import (
	Testing "testing"
)

func TestSeedTargetsEnabled(T *Testing.T) {
	Cases := []struct {
		Name     string
		AppEnv   string
		Override string
		Want     bool
	}{
		{"development", "development", "", true},
		{"empty env", "", "", true},
		{"production disabled", "production", "", false},
		{"production override", "production", "true", true},
		{"development override false", "development", "false", true},
	}
	for _, Case := range Cases {
		if Got := SeedTargetsEnabled(Case.AppEnv, Case.Override); Got != Case.Want {
			T.Fatalf("%s: got %v, want %v", Case.Name, Got, Case.Want)
		}
	}
}

func TestParseDatabaseURLNamed(T *Testing.T) {
	Input, Err := ParseDatabaseURL("postgres://target:secret@127.0.0.1:55433/target?sslmode=disable")
	if Err != nil {
		T.Fatal(Err)
	}
	if Input.Name != "local-target" || Input.DBName != "target" || Input.Port != 55433 {
		T.Fatalf("unexpected default parse: %+v", Input)
	}
	Named, Err := ParseDatabaseURLNamed("postgres://target_b:secret@127.0.0.1:55435/target_b?sslmode=disable", "")
	if Err != nil {
		T.Fatal(Err)
	}
	if Named.Name != "local-target_b" {
		T.Fatalf("derived name = %q", Named.Name)
	}
	Override, Err := ParseDatabaseURLNamed("postgres://target_b:secret@127.0.0.1:55435/target_b?sslmode=disable", "custom-target")
	if Err != nil || Override.Name != "custom-target" {
		T.Fatalf("explicit name was not preserved: %+v %v", Override, Err)
	}
	for _, Raw := range []string{"", "not-a-url", "mysql://u:p@localhost/db", "postgres://localhost", "postgres://u:p@localhost:0/db"} {
		if _, Err := ParseDatabaseURLNamed(Raw, ""); Err == nil {
			T.Fatalf("invalid seed URL accepted: %q", Raw)
		}
	}
}

func TestDerivedNameStaysWithinLimit(T *Testing.T) {
	Name := DerivedName("abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789")
	if len(Name) > 64 {
		T.Fatalf("derived name has %d bytes", len(Name))
	}
	if len(DerivedName("target")) != len("local-target") {
		T.Fatal("short derived name was changed")
	}
}
