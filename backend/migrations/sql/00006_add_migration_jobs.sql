-- +goose Up
ALTER TABLE migrations DROP CONSTRAINT IF EXISTS migrations_status_check;
ALTER TABLE migrations ADD CONSTRAINT migrations_status_check CHECK (status IN ('draft', 'pending', 'committed', 'applied', 'rolled_back', 'failed', 'partial', 'partially_reverted'));

CREATE TABLE migration_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    database_id UUID NOT NULL REFERENCES db_connections(id) ON DELETE CASCADE,
    migration_id UUID NOT NULL REFERENCES migrations(id) ON DELETE CASCADE,
    action TEXT NOT NULL CHECK (action IN ('apply', 'rollback', 'forward', 'retry')),
    status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts INTEGER NOT NULL DEFAULT 3 CHECK (max_attempts > 0),
    available_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    locked_by TEXT,
    locked_at TIMESTAMPTZ,
    lease_expires_at TIMESTAMPTZ,
    last_error TEXT,
    execution_id UUID REFERENCES migration_executions(id) ON DELETE SET NULL,
    requested_by UUID REFERENCES users(id) ON DELETE SET NULL,
    traceparent TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at TIMESTAMPTZ
);

CREATE INDEX migration_jobs_claim_idx ON migration_jobs (status, available_at, created_at);
CREATE INDEX migration_jobs_database_created_idx ON migration_jobs (database_id, created_at DESC);
CREATE UNIQUE INDEX migration_jobs_active_database ON migration_jobs (database_id) WHERE status IN ('queued', 'running');

-- +goose Down
DROP TABLE migration_jobs;
ALTER TABLE migrations DROP CONSTRAINT IF EXISTS migrations_status_check;
ALTER TABLE migrations ADD CONSTRAINT migrations_status_check CHECK (status IN ('draft', 'pending', 'committed', 'applied', 'rolled_back', 'failed'));
