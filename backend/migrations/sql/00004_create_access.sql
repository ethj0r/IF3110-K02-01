-- +goose Up
CREATE TABLE groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64),
    description TEXT NOT NULL DEFAULT '' CHECK (char_length(description) <= 500),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX groups_name_unique ON groups (lower(name));

CREATE TABLE user_groups (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, group_id)
);

CREATE INDEX user_groups_group_id ON user_groups (group_id);

CREATE TABLE group_database_access (
    group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    database_id UUID NOT NULL REFERENCES db_connections(id) ON DELETE CASCADE,
    access_level TEXT NOT NULL CHECK (access_level IN ('view_only', 'view_edit')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (group_id, database_id)
);

CREATE INDEX group_database_access_database_id ON group_database_access (database_id);

-- +goose Down
DROP TABLE group_database_access;
DROP TABLE user_groups;
DROP TABLE groups;
