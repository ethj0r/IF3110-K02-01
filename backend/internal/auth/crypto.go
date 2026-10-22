package Auth

import (
	Aes "crypto/aes"
	Cipher "crypto/cipher"
	Rand "crypto/rand"
	Sha256 "crypto/sha256"
	Base64 "encoding/base64"
	Fmt "fmt"
	Strings "strings"
)

func RandomToken() (string, error) {
	Bytes := make([]byte, 32)
	if _, Err := Rand.Read(Bytes); Err != nil {
		return "", ErrUnavailable
	}
	return Base64.RawURLEncoding.EncodeToString(Bytes), nil
}

func ValidToken(Token string) bool {
	Bytes, Err := Base64.RawURLEncoding.Strict().DecodeString(Token)
	return Err == nil && len(Bytes) == 32 && len(Token) == 43
}

func TokenHash(Token string) []byte {
	Sum := Sha256.Sum256([]byte(Token))
	return Sum[:]
}

func RandomID() (string, error) {
	Bytes := make([]byte, 16)
	if _, Err := Rand.Read(Bytes); Err != nil {
		return "", ErrUnavailable
	}
	Bytes[6] = (Bytes[6] & 0x0f) | 0x40
	Bytes[8] = (Bytes[8] & 0x3f) | 0x80
	return Fmt.Sprintf("%x-%x-%x-%x-%x", Bytes[:4], Bytes[4:6], Bytes[6:8], Bytes[8:10], Bytes[10:]), nil
}

func NewCipher(Key []byte) (Cipher.AEAD, error) {
	Block, Err := Aes.NewCipher(Key)
	if Err != nil {
		return nil, ErrUnavailable
	}
	return Cipher.NewGCM(Block)
}

func SealSecrets(AEAD Cipher.AEAD, SessionID, Identifier, CSRFToken string) ([]byte, error) {
	Nonce := make([]byte, AEAD.NonceSize())
	if _, Err := Rand.Read(Nonce); Err != nil {
		return nil, ErrUnavailable
	}
	return AEAD.Seal(Nonce, Nonce, []byte(Identifier+"."+CSRFToken), []byte(SessionID)), nil
}

func OpenSecrets(AEAD Cipher.AEAD, SessionID string, Ciphertext []byte) (string, string, error) {
	if len(Ciphertext) < AEAD.NonceSize()+AEAD.Overhead() {
		return "", "", ErrUnavailable
	}
	Plaintext, Err := AEAD.Open(nil, Ciphertext[:AEAD.NonceSize()], Ciphertext[AEAD.NonceSize():], []byte(SessionID))
	if Err != nil {
		return "", "", ErrUnavailable
	}
	Parts := Strings.Split(string(Plaintext), ".")
	if len(Parts) != 2 || !ValidToken(Parts[0]) || !ValidToken(Parts[1]) {
		return "", "", ErrUnavailable
	}
	return Parts[0], Parts[1], nil
}
