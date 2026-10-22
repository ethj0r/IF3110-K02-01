package Migrations

import (
	Embed "embed"
	Fs "io/fs"
)

//go:embed all:sql
var Files Embed.FS

func FS() Fs.FS {
	Root, Err := Fs.Sub(Files, "sql")
	if Err != nil {
		panic("embedded platform migrations directory is missing")
	}
	return Root
}
