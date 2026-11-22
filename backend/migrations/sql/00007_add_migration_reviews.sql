-- +goose Up
CREATE TABLE migration_reviews (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    migration_id UUID NOT NULL REFERENCES migrations(id) ON DELETE CASCADE,
    revision BIGINT NOT NULL CHECK (revision > 0),
    action TEXT NOT NULL CHECK (action IN ('apply', 'rollback', 'forward', 'retry')),
    previous_execution_id UUID REFERENCES migration_executions(id) ON DELETE CASCADE,
    sql_text TEXT NOT NULL CHECK (length(trim(sql_text)) > 0),
    requested_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE NULLS NOT DISTINCT (migration_id, revision, action, previous_execution_id)
);

CREATE TABLE migration_review_approvals (
    review_id UUID NOT NULL REFERENCES migration_reviews(id) ON DELETE CASCADE,
    group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    approved_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    approved_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (review_id, group_id)
);

-- +goose Down
DROP TABLE migration_review_approvals;
DROP TABLE migration_reviews;
