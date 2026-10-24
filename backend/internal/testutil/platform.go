//go:build integration

package Testutil

import (
	Context "context"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
	Testing "testing"
)

func CreateUser(T *Testing.T, Pool *Pgxpool.Pool, Username string, Admin bool) string {
	T.Helper()
	var ID string
	Err := Pool.QueryRow(Context.Background(), `INSERT INTO users (username, password_hash, is_platform_admin)
VALUES ($1, 'integration-test-hash', $2) RETURNING id::text`, Username, Admin).Scan(&ID)
	if Err != nil {
		T.Fatal(Err)
	}
	return ID
}

func CreateConnection(T *Testing.T, Pool *Pgxpool.Pool, Name string) string {
	T.Helper()
	var ID string
	Err := Pool.QueryRow(Context.Background(), `INSERT INTO db_connections (name, host, port, dbname, username, password_ciphertext)
VALUES ($1, '127.0.0.1', 5432, 'targetdb', 'target', $2) RETURNING id::text`, Name, []byte{1}).Scan(&ID)
	if Err != nil {
		T.Fatal(Err)
	}
	return ID
}
