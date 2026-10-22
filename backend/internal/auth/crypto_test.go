package Auth

import (
	Bytes "bytes"
	Testing "testing"
)

func TestSessionEncryption(T *Testing.T) {
	AEAD, Err := NewCipher(Bytes.Repeat([]byte{7}, 32))
	if Err != nil {
		T.Fatal(Err)
	}
	ID, _ := RandomID()
	Identifier, _ := RandomToken()
	CSRFToken, _ := RandomToken()
	First, Err := SealSecrets(AEAD, ID, Identifier, CSRFToken)
	if Err != nil {
		T.Fatal(Err)
	}
	Second, _ := SealSecrets(AEAD, ID, Identifier, CSRFToken)
	if Bytes.Equal(First, Second) || Bytes.Contains(First, []byte(Identifier)) || len(TokenHash(Identifier)) != 32 {
		T.Fatal("session secrets must use randomized authenticated encryption")
	}
	Token, CSRF, Err := OpenSecrets(AEAD, ID, First)
	if Err != nil || Token != Identifier || CSRF != CSRFToken {
		T.Fatal("session encryption roundtrip failed")
	}
	for _, Case := range []struct {
		ID         string
		Ciphertext []byte
	}{{"different-session", First}, {ID, []byte("short")}, {ID, append(append([]byte{}, First[:len(First)-1]...), First[len(First)-1]^1)}} {
		if _, _, Err := OpenSecrets(AEAD, Case.ID, Case.Ciphertext); Err == nil {
			T.Fatal("tampered session secret was accepted")
		}
	}
	if ValidToken("fake") || ValidToken(Identifier+"=") {
		T.Fatal("invalid identifier format accepted")
	}
}
