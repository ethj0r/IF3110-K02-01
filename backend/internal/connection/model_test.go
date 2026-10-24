package Connection

import (
	Context "context"
	Json "encoding/json"
	Strings "strings"
	Testing "testing"
)

func TestServiceRejectsMalformedIDs(T *Testing.T) {
	Server := &Service{}
	for _, ID := range []string{"", "not-a-uuid", "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", "' OR true --"} {
		if _, Err := Server.Get(Context.Background(), ID); Err != ErrNotFound {
			T.Fatalf("invalid ID accepted by get: %q", ID)
		}
		if _, Err := Server.Update(Context.Background(), ID, validInput()); Err != ErrNotFound {
			T.Fatalf("invalid ID accepted by update: %q", ID)
		}
		if Err := Server.Delete(Context.Background(), ID); Err != ErrNotFound {
			T.Fatalf("invalid ID accepted by delete: %q", ID)
		}
	}
	if !validID("01234567-89ab-cdef-0123-456789abcdef") {
		T.Fatal("valid UUID rejected")
	}
}

func validInput() ConnectionInput {
	return ConnectionInput{Name: "local-target", Host: "127.0.0.1", Port: 55433, DBName: "target", Username: "target", Password: "target_dev"}
}

func TestConnectionInputValidation(T *Testing.T) {
	if Err := validInput().Validate(); Err != nil {
		T.Fatalf("valid input rejected: %v", Err)
	}
	Cases := []ConnectionInput{
		{Name: "", Host: "h", Port: 5432, DBName: "d", Username: "u", Password: "p"},
		{Name: Strings.Repeat("n", 65), Host: "h", Port: 5432, DBName: "d", Username: "u", Password: "p"},
		{Name: "n", Host: "", Port: 5432, DBName: "d", Username: "u", Password: "p"},
		{Name: "n", Host: "h", Port: 0, DBName: "d", Username: "u", Password: "p"},
		{Name: "n", Host: "h", Port: 70000, DBName: "d", Username: "u", Password: "p"},
		{Name: "n", Host: "h", Port: 5432, DBName: "", Username: "u", Password: "p"},
		{Name: "n", Host: "h", Port: 5432, DBName: "d", Username: "", Password: "p"},
		{Name: "n", Host: "h", Port: 5432, DBName: "d", Username: "u", Password: ""},
	}
	for Index, Case := range Cases {
		if Err := Case.Validate(); Err == nil {
			T.Fatalf("invalid input accepted at case %d", Index)
		}
	}
	Trimmed := ConnectionInput{Name: "  spaced  ", Host: "h", Port: 5432, DBName: "d", Username: "u", Password: "p"}.Normalized()
	if Trimmed.Name != "spaced" {
		T.Fatal("input was not trimmed")
	}
}

func TestConnectionResponseHidesSecrets(T *Testing.T) {
	Body, Err := Json.Marshal(Connection{ID: "id", Name: "n", Host: "h", Port: 5432, DBName: "d", Username: "u"})
	if Err != nil {
		T.Fatal(Err)
	}
	for _, Secret := range []string{"password", "ciphertext", "secret"} {
		if Strings.Contains(Strings.ToLower(string(Body)), Secret) {
			T.Fatalf("connection response mentions %q", Secret)
		}
	}
}
