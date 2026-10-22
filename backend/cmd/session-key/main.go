package main

import (
	Rand "crypto/rand"
	Base64 "encoding/base64"
	Fmt "fmt"
	Os "os"
)

func main() {
	Key := make([]byte, 32)
	if _, Err := Rand.Read(Key); Err != nil {
		Fmt.Fprintln(Os.Stderr, "Cannot generate session key")
		Os.Exit(1)
	}
	Fmt.Println(Base64.StdEncoding.EncodeToString(Key))
}
