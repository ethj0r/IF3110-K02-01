-- +goose Up
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username TEXT NOT NULL CHECK (username ~ '^[A-Za-z0-9_.-]{3,64}$'),
    password_hash TEXT NOT NULL,
    is_platform_admin BOOLEAN NOT NULL DEFAULT FALSE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    auth_version BIGINT NOT NULL DEFAULT 1 CHECK (auth_version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX users_username_unique ON users (lower(username));

CREATE TABLE sessions (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    secret_ciphertext BYTEA NOT NULL,
    user_auth_version BIGINT NOT NULL,
    user_is_platform_admin BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    rotated_at TIMESTAMPTZ NOT NULL,
    absolute_expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    CHECK (absolute_expires_at > created_at),
    CHECK (last_seen_at >= created_at),
    CHECK (rotated_at >= created_at AND rotated_at <= last_seen_at)
);

CREATE INDEX sessions_user_id ON sessions (user_id);

CREATE TABLE session_identifiers (
    token_hash BYTEA PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    valid_until TIMESTAMPTZ
);

CREATE INDEX session_identifiers_session_id ON session_identifiers (session_id);
CREATE UNIQUE INDEX session_identifiers_current ON session_identifiers (session_id) WHERE valid_until IS NULL;

-- +goose StatementBegin
CREATE FUNCTION invalidate_changed_user_sessions() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF (OLD.is_active AND NOT NEW.is_active) OR OLD.password_hash IS DISTINCT FROM NEW.password_hash OR (OLD.is_platform_admin AND NOT NEW.is_platform_admin) THEN
        NEW.auth_version := OLD.auth_version + 1;
        UPDATE sessions SET revoked_at = COALESCE(revoked_at, CURRENT_TIMESTAMP) WHERE user_id = NEW.id;
    ELSIF NOT OLD.is_platform_admin AND NEW.is_platform_admin THEN
        NEW.auth_version := OLD.auth_version + 1;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER users_auth_change BEFORE UPDATE OF is_active, password_hash, is_platform_admin ON users FOR EACH ROW EXECUTE FUNCTION invalidate_changed_user_sessions();

-- +goose Down
DROP TABLE session_identifiers;
DROP TABLE sessions;
DROP TABLE users;
DROP FUNCTION invalidate_changed_user_sessions();
