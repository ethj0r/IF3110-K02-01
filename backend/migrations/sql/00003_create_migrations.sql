-- +goose Up
CREATE TABLE migrations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    database_id UUID NOT NULL REFERENCES db_connections(id) ON DELETE CASCADE,
    version BIGINT NOT NULL CHECK (version > 0),
    title TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 128),
    sql_text TEXT NOT NULL CHECK (char_length(trim(sql_text)) > 0),
    status TEXT NOT NULL CHECK (status IN ('draft', 'pending', 'committed', 'applied', 'rolled_back', 'failed')),
    error_message TEXT,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    executed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    executed_at TIMESTAMPTZ,
    CONSTRAINT migrations_database_version_unique UNIQUE (database_id, version)
);

CREATE INDEX migrations_database_id_version_idx ON migrations (database_id, version ASC);
CREATE INDEX migrations_database_id_status_idx ON migrations (database_id, status);

-- +goose Down
DROP TABLE migrations;

