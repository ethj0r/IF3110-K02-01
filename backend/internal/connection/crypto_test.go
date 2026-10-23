package Connection

import (
	Bytes "bytes"
	Testing "testing"
)

func TestPasswordEncryption(T *Testing.T) {
	AEAD, Err := NewCipher(Bytes.Repeat([]byte{3}, 32))
	if Err != nil {
		T.Fatal(Err)
	}
	First, Err := SealPassword(AEAD, "target_dev")
	if Err != nil {
		T.Fatal(Err)
	}
	Second, _ := SealPassword(AEAD, "target_dev")
	if Bytes.Equal(First, Second) || Bytes.Contains(First, []byte("target_dev")) {
		T.Fatal("passwords must use randomized authenticated encryption")
	}
	Plaintext, Err := OpenPassword(AEAD, First)
	if Err != nil || Plaintext != "target_dev" {
		T.Fatal("password encryption roundtrip failed")
	}
	Tampered := append([]byte{}, First...)
	Tampered[len(Tampered)-1] ^= 1
	if _, Err := OpenPassword(AEAD, Tampered); Err == nil {
		T.Fatal("tampered password was accepted")
	}
	Other, _ := NewCipher(Bytes.Repeat([]byte{5}, 32))
	if _, Err := OpenPassword(Other, First); Err == nil {
		T.Fatal("password opened with another key")
	}
	if _, Err := NewCipher([]byte("short")); Err == nil {
		T.Fatal("short encryption key accepted")
	}
}
