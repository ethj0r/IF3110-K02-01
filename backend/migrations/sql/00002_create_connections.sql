-- +goose Up
CREATE TABLE db_connections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64),
    host TEXT NOT NULL CHECK (char_length(host) BETWEEN 1 AND 255),
    port INTEGER NOT NULL CHECK (port BETWEEN 1 AND 65535),
    dbname TEXT NOT NULL CHECK (char_length(dbname) BETWEEN 1 AND 63),
    username TEXT NOT NULL CHECK (char_length(username) BETWEEN 1 AND 63),
    password_ciphertext BYTEA NOT NULL CHECK (octet_length(password_ciphertext) > 0),
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX db_connections_name_unique ON db_connections (lower(name));

-- +goose Down
DROP TABLE db_connections;
