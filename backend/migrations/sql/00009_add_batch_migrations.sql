-- +goose Up
ALTER TABLE migrations ADD COLUMN batch_mode BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE migrations ADD COLUMN batch_column TEXT;
ALTER TABLE migrations ADD COLUMN batch_size INTEGER;
ALTER TABLE migrations ADD CONSTRAINT migrations_batch_config_check CHECK (
    (batch_mode AND batch_column IS NOT NULL AND char_length(trim(batch_column)) BETWEEN 1 AND 63 AND batch_size IS NOT NULL AND batch_size > 0)
    OR
    (NOT batch_mode AND batch_column IS NULL AND batch_size IS NULL)
);

ALTER TABLE migration_executions ADD COLUMN mode TEXT NOT NULL DEFAULT 'normal';
ALTER TABLE migration_executions ADD CONSTRAINT migration_executions_mode_check CHECK (mode IN ('normal', 'batch'));
ALTER TABLE migration_executions ADD COLUMN batch_column TEXT;
ALTER TABLE migration_executions ADD COLUMN batch_size INTEGER;
ALTER TABLE migration_executions ADD COLUMN committed_batches BIGINT NOT NULL DEFAULT 0;
ALTER TABLE migration_executions ADD COLUMN committed_rows BIGINT NOT NULL DEFAULT 0;
ALTER TABLE migration_executions ADD COLUMN reverted_batches BIGINT NOT NULL DEFAULT 0;
ALTER TABLE migration_executions ADD COLUMN reverted_rows BIGINT NOT NULL DEFAULT 0;
ALTER TABLE migration_executions ADD COLUMN checkpoint_cursor BIGINT NOT NULL DEFAULT 0;

CREATE TABLE migration_batches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    execution_id UUID NOT NULL REFERENCES migration_executions(id) ON DELETE CASCADE,
    database_id UUID NOT NULL REFERENCES db_connections(id) ON DELETE CASCADE,
    batch_no BIGINT NOT NULL CHECK (batch_no > 0),
    cursor_from BIGINT NOT NULL,
    cursor_to BIGINT NOT NULL,
    row_count BIGINT NOT NULL CHECK (row_count > 0),
    status TEXT NOT NULL DEFAULT 'committed' CHECK (status IN ('committed', 'reverted')),
    committed_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    reverted_at TIMESTAMPTZ,
    reverted_by_execution_id UUID REFERENCES migration_executions(id) ON DELETE SET NULL,
    CONSTRAINT migration_batches_execution_batch_unique UNIQUE (execution_id, batch_no)
);

CREATE INDEX migration_batches_execution_status_idx ON migration_batches (execution_id, status, batch_no DESC);
CREATE INDEX migration_batches_reverted_by_idx ON migration_batches (reverted_by_execution_id);

-- +goose Down
DROP TABLE migration_batches;
ALTER TABLE migration_executions DROP COLUMN checkpoint_cursor;
ALTER TABLE migration_executions DROP COLUMN reverted_rows;
ALTER TABLE migration_executions DROP COLUMN reverted_batches;
ALTER TABLE migration_executions DROP COLUMN committed_rows;
ALTER TABLE migration_executions DROP COLUMN committed_batches;
ALTER TABLE migration_executions DROP COLUMN batch_size;
ALTER TABLE migration_executions DROP COLUMN batch_column;
ALTER TABLE migration_executions DROP CONSTRAINT migration_executions_mode_check;
ALTER TABLE migration_executions DROP COLUMN mode;
ALTER TABLE migrations DROP CONSTRAINT migrations_batch_config_check;
ALTER TABLE migrations DROP COLUMN batch_size;
ALTER TABLE migrations DROP COLUMN batch_column;
ALTER TABLE migrations DROP COLUMN batch_mode;
