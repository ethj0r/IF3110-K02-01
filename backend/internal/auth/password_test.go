package Auth

import (
	Strings "strings"
	Testing "testing"
)

func TestPasswordHashing(T *Testing.T) {
	Password := "correct horse battery staple"
	First, Err := HashPassword(Password)
	if Err != nil {
		T.Fatal(Err)
	}
	Second, Err := HashPassword(Password)
	if Err != nil {
		T.Fatal(Err)
	}
	if First == Second || Strings.Contains(First, Password) {
		T.Fatal("hashes must use independent salts")
	}
	if !VerifyPassword(Password, First) || VerifyPassword("incorrect password", First) {
		T.Fatal("password verification failed")
	}
}

func TestPasswordRejectsInvalidInput(T *Testing.T) {
	for _, Password := range []string{"short", Strings.Repeat("x", 1025)} {
		if _, Err := HashPassword(Password); Err == nil {
			T.Fatal("invalid password was accepted")
		}
	}
	for _, Encoded := range []string{"plaintext", "$argon2id$v=19$m=999999999,t=2,p=1$bad$bad", "$argon2id$v=19$" + PasswordParameters + "$bad$bad"} {
		if VerifyPassword("correct password", Encoded) {
			T.Fatal("malformed hash was accepted")
		}
	}
}
