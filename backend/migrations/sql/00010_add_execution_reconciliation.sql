-- +goose Up
ALTER TABLE migration_executions ADD COLUMN reconciled_at TIMESTAMPTZ;
ALTER TABLE migration_executions ADD COLUMN reconciled_by UUID REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE migration_executions ADD COLUMN reconcile_note TEXT;
ALTER TABLE migration_jobs ADD COLUMN reconciled_at TIMESTAMPTZ;
ALTER TABLE migration_jobs ADD COLUMN reconciled_by UUID REFERENCES users(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE migration_jobs DROP COLUMN reconciled_by;
ALTER TABLE migration_jobs DROP COLUMN reconciled_at;
ALTER TABLE migration_executions DROP COLUMN reconcile_note;
ALTER TABLE migration_executions DROP COLUMN reconciled_by;
ALTER TABLE migration_executions DROP COLUMN reconciled_at;
