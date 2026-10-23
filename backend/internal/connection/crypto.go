package Connection

import (
	Aes "crypto/aes"
	Cipher "crypto/cipher"
	Rand "crypto/rand"
)

func NewCipher(Key []byte) (Cipher.AEAD, error) {
	Block, Err := Aes.NewCipher(Key)
	if Err != nil {
		return nil, ErrUnavailable
	}
	return Cipher.NewGCM(Block)
}

func SealPassword(AEAD Cipher.AEAD, Password string) ([]byte, error) {
	Nonce := make([]byte, AEAD.NonceSize())
	if _, Err := Rand.Read(Nonce); Err != nil {
		return nil, ErrUnavailable
	}
	return AEAD.Seal(Nonce, Nonce, []byte(Password), nil), nil
}

func OpenPassword(AEAD Cipher.AEAD, Ciphertext []byte) (string, error) {
	if len(Ciphertext) < AEAD.NonceSize()+AEAD.Overhead() {
		return "", ErrUnavailable
	}
	Plaintext, Err := AEAD.Open(nil, Ciphertext[:AEAD.NonceSize()], Ciphertext[AEAD.NonceSize():], nil)
	if Err != nil {
		return "", ErrUnavailable
	}
	return string(Plaintext), nil
}
