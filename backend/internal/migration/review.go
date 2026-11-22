package Migration

import (
	Context "context"
	Errors "errors"

	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
)

var (
	ErrReviewNotFound    = Errors.New("current migration review not found")
	ErrReviewUnavailable = Errors.New("migration action is not available for review")
)

type Review struct {
	ID       string        `json:"id"`
	Revision int64         `json:"revision"`
	Action   ExecutionKind `json:"action"`
	SQLText  string        `json:"sql_text"`
	Status   string        `json:"status"`
	Groups   []ReviewGroup `json:"groups"`
}

type ReviewGroup struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	LeaderID   *string `json:"leader_user_id,omitempty"`
	Approved   bool    `json:"approved"`
	CanApprove bool    `json:"can_approve"`
}

type reviewKey struct {
	DatabaseID  string
	MigrationID string
	Action      ExecutionKind
}

func (Server *Service) checkReviewAccess(Ctx Context.Context, User Auth.Principal, Key reviewKey, Permission Access.Action) error {
	switch Key.Action {
	case KindApply, KindRollback, KindForward, KindRetry:
	default:
		return ErrInvalidInput
	}
	if Err := Server.checkAccess(Ctx, User, Permission, Key.DatabaseID); Err != nil {
		return Err
	}
	if Server.Reviews == nil {
		return ErrUnavailable
	}
	return nil
}

// A review is current only for the same definition, action and latest execution.
// The execution service separately checks version order and database locks.
const reviewSubject = `WITH subject AS (
    SELECT M.id, M.database_id, M.revision, E.id AS previous_execution_id,
           CASE $3::text
               WHEN 'apply' THEN CASE WHEN M.status = 'draft' AND E.id IS NULL THEN M.sql_text END
               WHEN 'rollback' THEN CASE WHEN M.status IN ('applied', 'committed') AND NOT M.irreversible THEN M.down_sql END
               WHEN 'forward' THEN CASE WHEN M.status = 'rolled_back' THEN M.sql_text END
               WHEN 'retry' THEN CASE WHEN E.status IN ('failed', 'partial', 'partially_reverted') THEN E.sql_text END
           END AS sql_text
    FROM migrations M
    LEFT JOIN LATERAL (
        SELECT id, sql_text, status FROM migration_executions
        WHERE migration_id = M.id ORDER BY created_at DESC, id DESC LIMIT 1
    ) E ON TRUE
    WHERE M.database_id::text = $1 AND M.id::text = $2
)`

const currentReview = reviewSubject + `, current_review AS (
    SELECT R.id, R.revision, R.sql_text, S.database_id
    FROM subject S JOIN migration_reviews R ON R.migration_id = S.id
    WHERE R.revision = S.revision AND R.action = $3 AND R.sql_text = S.sql_text
      AND R.previous_execution_id IS NOT DISTINCT FROM S.previous_execution_id
)`

func (Repo *Repository) submitReview(Ctx Context.Context, Key reviewKey, ActorID string) error {
	Tag, Err := Repo.Pool.Exec(Ctx, reviewSubject+`
INSERT INTO migration_reviews (migration_id, revision, action, previous_execution_id, sql_text, requested_by)
SELECT id, revision, $3, previous_execution_id, sql_text, $4 FROM subject WHERE sql_text IS NOT NULL
ON CONFLICT (migration_id, revision, action, previous_execution_id) DO UPDATE SET action = EXCLUDED.action`,
		Key.DatabaseID, Key.MigrationID, Key.Action, ActorID)
	if Err != nil {
		return ErrUnavailable
	}
	if Tag.RowsAffected() == 0 {
		return ErrReviewUnavailable
	}
	return nil
}

func (Repo *Repository) getReview(Ctx Context.Context, Key reviewKey, ActorID string) (Review, error) {
	Rows, Err := Repo.query(Ctx, currentReview+`
SELECT R.id::text, R.revision, R.sql_text, G.id::text, G.name, U.id::text,
       COALESCE(V.approved_by = U.id, FALSE)
FROM current_review R
LEFT JOIN group_database_access A ON A.database_id = R.database_id
LEFT JOIN groups G ON G.id = A.group_id
LEFT JOIN group_leaders L ON L.group_id = G.id
LEFT JOIN users U ON U.id = L.user_id AND U.is_active
LEFT JOIN migration_review_approvals V ON V.review_id = R.id AND V.group_id = G.id
ORDER BY G.name, G.id`, Key.DatabaseID, Key.MigrationID, Key.Action)
	if Err != nil {
		return Review{}, ErrUnavailable
	}
	defer Rows.Close()
	Result := Review{Action: Key.Action, Status: "pending_review", Groups: []ReviewGroup{}}
	AllApproved := true
	for Rows.Next() {
		var Group ReviewGroup
		var ID, Name *string
		if Err := Rows.Scan(&Result.ID, &Result.Revision, &Result.SQLText, &ID, &Name, &Group.LeaderID, &Group.Approved); Err != nil {
			return Review{}, ErrUnavailable
		}
		if ID != nil {
			Group.ID, Group.Name = *ID, *Name
			Group.CanApprove = Group.LeaderID != nil && *Group.LeaderID == ActorID && !Group.Approved
			AllApproved = AllApproved && Group.Approved
			Result.Groups = append(Result.Groups, Group)
		}
	}
	if Rows.Err() != nil {
		return Review{}, ErrUnavailable
	}
	if Result.ID == "" {
		return Review{}, ErrReviewNotFound
	}
	if len(Result.Groups) > 0 && AllApproved {
		Result.Status = "approved"
	}
	return Result, nil
}

func (Repo *Repository) approveReview(Ctx Context.Context, Key reviewKey, ReviewID, GroupID, ActorID string) error {
	var Current, Recorded bool
	Err := Repo.queryRow(Ctx, currentReview+`, approved AS (
    INSERT INTO migration_review_approvals (review_id, group_id, approved_by)
    SELECT R.id, A.group_id, U.id FROM current_review R
    JOIN group_database_access A ON A.database_id = R.database_id
    JOIN group_leaders L ON L.group_id = A.group_id
    JOIN users U ON U.id = L.user_id AND U.is_active
    WHERE R.id::text = $4 AND A.group_id::text = $5 AND U.id::text = $6
    FOR SHARE OF A, L, U
    ON CONFLICT (review_id, group_id) DO UPDATE
    SET approved_by = EXCLUDED.approved_by,
        approved_at = CASE WHEN migration_review_approvals.approved_by = EXCLUDED.approved_by
                           THEN migration_review_approvals.approved_at ELSE CURRENT_TIMESTAMP END
    RETURNING review_id
)
SELECT EXISTS (SELECT 1 FROM current_review WHERE id::text = $4), EXISTS (SELECT 1 FROM approved)`,
		Key.DatabaseID, Key.MigrationID, Key.Action, ReviewID, GroupID, ActorID).Scan(&Current, &Recorded)
	if Err != nil {
		return ErrUnavailable
	}
	if !Current {
		return ErrReviewNotFound
	}
	if !Recorded {
		return ErrForbidden
	}
	return nil
}
