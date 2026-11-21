-- +goose Up
ALTER TABLE migrations ADD COLUMN down_sql TEXT;
ALTER TABLE migrations ADD COLUMN irreversible BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE migrations ADD COLUMN irreversible_reason TEXT;
ALTER TABLE migrations ADD COLUMN revision BIGINT NOT NULL DEFAULT 1;
ALTER TABLE migrations ADD COLUMN locked_at TIMESTAMPTZ;

UPDATE migrations
SET irreversible = TRUE,
    irreversible_reason = 'Legacy migration imported without a down SQL'
WHERE down_sql IS NULL AND irreversible = FALSE;

UPDATE migrations SET status = 'applied' WHERE status = 'committed';
UPDATE migrations SET status = 'failed' WHERE status = 'rolled_back';

UPDATE migrations
SET locked_at = COALESCE(executed_at, updated_at)
WHERE status <> 'draft' AND locked_at IS NULL;

ALTER TABLE migrations ADD CONSTRAINT migrations_revision_positive CHECK (revision > 0);
ALTER TABLE migrations ADD CONSTRAINT migrations_reversible_check CHECK (
    (irreversible AND down_sql IS NULL AND irreversible_reason IS NOT NULL AND char_length(trim(irreversible_reason)) > 0)
    OR
    (NOT irreversible AND down_sql IS NOT NULL AND char_length(trim(down_sql)) > 0)
);

CREATE TABLE migration_executions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    migration_id UUID NOT NULL REFERENCES migrations(id) ON DELETE CASCADE,
    database_id UUID NOT NULL REFERENCES db_connections(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('apply', 'rollback', 'forward', 'retry')),
    direction TEXT NOT NULL CHECK (direction IN ('up', 'down')),
    revision BIGINT NOT NULL CHECK (revision > 0),
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'partial', 'partially_reverted')),
    sql_text TEXT NOT NULL CHECK (char_length(trim(sql_text)) > 0),
    up_sql TEXT NOT NULL,
    down_sql TEXT,
    rollback_of_execution_id UUID REFERENCES migration_executions(id) ON DELETE SET NULL,
    retry_of_execution_id UUID REFERENCES migration_executions(id) ON DELETE SET NULL,
    requested_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    error_message TEXT
);

CREATE INDEX migration_executions_database_created_idx ON migration_executions (database_id, created_at, id);
CREATE INDEX migration_executions_migration_created_idx ON migration_executions (migration_id, created_at, id);
CREATE INDEX migration_executions_rollback_of ON migration_executions (rollback_of_execution_id);

CREATE TABLE migration_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    execution_id UUID NOT NULL REFERENCES migration_executions(id) ON DELETE CASCADE,
    migration_id UUID NOT NULL REFERENCES migrations(id) ON DELETE CASCADE,
    database_id UUID NOT NULL REFERENCES db_connections(id) ON DELETE CASCADE,
    attempt_no INTEGER NOT NULL CHECK (attempt_no > 0),
    version BIGINT NOT NULL,
    revision BIGINT NOT NULL,
    actor_id UUID REFERENCES users(id) ON DELETE SET NULL,
    sql_text TEXT NOT NULL CHECK (char_length(trim(sql_text)) > 0),
    status TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed')),
    error_message TEXT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at TIMESTAMPTZ,
    duration_ms BIGINT,
    CONSTRAINT migration_attempts_execution_attempt_unique UNIQUE (execution_id, attempt_no)
);

CREATE INDEX migration_attempts_database_started_idx ON migration_attempts (database_id, started_at DESC, id);
CREATE INDEX migration_attempts_execution_idx ON migration_attempts (execution_id, attempt_no);

WITH Source AS MATERIALIZED (
    SELECT M.id AS migration_id,
           M.database_id,
           M.version,
           M.revision,
           M.sql_text,
           M.executed_by,
           M.created_by,
           M.error_message,
           COALESCE(M.executed_at, M.updated_at, M.created_at) AS event_at,
           CASE
               WHEN M.status = 'applied' THEN 'succeeded'
               WHEN M.status = 'pending' THEN 'running'
               ELSE 'failed'
           END AS status,
           gen_random_uuid() AS execution_id
    FROM migrations M
    WHERE M.status <> 'draft'
),
Inserted AS (
    INSERT INTO migration_executions
        (id, migration_id, database_id, kind, direction, revision, status, sql_text, up_sql, down_sql,
         requested_by, created_at, started_at, finished_at, error_message)
    SELECT execution_id, migration_id, database_id, 'apply', 'up', revision, status, sql_text, sql_text, NULL,
           COALESCE(executed_by, created_by), event_at, event_at,
           CASE WHEN status IN ('succeeded', 'failed') THEN event_at END, error_message
    FROM Source
    RETURNING id
)
INSERT INTO migration_attempts
    (execution_id, migration_id, database_id, attempt_no, version, revision, actor_id, sql_text, status,
     error_message, started_at, finished_at)
SELECT S.execution_id, S.migration_id, S.database_id, 1, S.version, S.revision,
       COALESCE(S.executed_by, S.created_by), S.sql_text, S.status, S.error_message,
       S.event_at, CASE WHEN S.status IN ('succeeded', 'failed') THEN S.event_at END
FROM Source S
JOIN Inserted I ON I.id = S.execution_id;

-- +goose Down
DROP TABLE migration_attempts;
DROP TABLE migration_executions;
ALTER TABLE migrations DROP CONSTRAINT migrations_reversible_check;
ALTER TABLE migrations DROP CONSTRAINT migrations_revision_positive;
ALTER TABLE migrations DROP COLUMN locked_at;
ALTER TABLE migrations DROP COLUMN revision;
ALTER TABLE migrations DROP COLUMN irreversible_reason;
ALTER TABLE migrations DROP COLUMN irreversible;
ALTER TABLE migrations DROP COLUMN down_sql;
