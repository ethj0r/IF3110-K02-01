package Auth

import (
	Rand "crypto/rand"
	Subtle "crypto/subtle"
	Base64 "encoding/base64"
	Errors "errors"
	Fmt "fmt"
	Argon2 "golang.org/x/crypto/argon2"
	Strings "strings"
)

const PasswordParameters = "m=19456,t=2,p=1"

func HashPassword(Password string) (string, error) {
	if len(Password) < 12 || len(Password) > 1024 {
		return "", Errors.New("password must contain 12 to 1024 bytes")
	}
	Salt := make([]byte, 16)
	if _, Err := Rand.Read(Salt); Err != nil {
		return "", ErrUnavailable
	}
	Hash := Argon2.IDKey([]byte(Password), Salt, 2, 19456, 1, 32)
	return Fmt.Sprintf("$argon2id$v=19$%s$%s$%s", PasswordParameters, Base64.RawStdEncoding.EncodeToString(Salt), Base64.RawStdEncoding.EncodeToString(Hash)), nil
}

func VerifyPassword(Password, Encoded string) bool {
	if len(Password) > 1024 || len(Encoded) > 256 {
		return false
	}
	Parts := Strings.Split(Encoded, "$")
	if len(Parts) != 6 || Parts[0] != "" || Parts[1] != "argon2id" || Parts[2] != "v=19" || Parts[3] != PasswordParameters {
		return false
	}
	Salt, SaltError := Base64.RawStdEncoding.Strict().DecodeString(Parts[4])
	Expected, HashError := Base64.RawStdEncoding.Strict().DecodeString(Parts[5])
	if SaltError != nil || HashError != nil || len(Salt) != 16 || len(Expected) != 32 {
		return false
	}
	Actual := Argon2.IDKey([]byte(Password), Salt, 2, 19456, 1, 32)
	return Subtle.ConstantTimeCompare(Actual, Expected) == 1
}
