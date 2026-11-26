package Migration

import (
	Errors "errors"
	Strings "strings"
	Time "time"
)

type Status string

const (
	StatusDraft             Status = "draft"
	StatusPending           Status = "pending"
	StatusCommitted         Status = "committed"
	StatusApplied           Status = "applied"
	StatusRolledBack        Status = "rolled_back"
	StatusFailed            Status = "failed"
	StatusPartial           Status = "partial"
	StatusPartiallyReverted Status = "partially_reverted"
)

func (S Status) IsTerminal() bool {
	return S.IsSuccess() || S == StatusRolledBack || S == StatusFailed
}

func (S Status) IsSuccess() bool {
	return S == StatusCommitted || S == StatusApplied
}

func (S Status) IsFailure() bool {
	return S == StatusFailed
}

func (S Status) IsDraft() bool {
	return S == StatusDraft
}

func (S Status) IsApplied() bool {
	return S.IsSuccess()
}

func (S Status) IsRolledBack() bool {
	return S == StatusRolledBack
}

func (S Status) IsPartial() bool {
	return S == StatusPartial || S == StatusPartiallyReverted
}

const (
	ModeNormal = "normal"
	ModeBatch  = "batch"
)

type ExecutionKind string

const (
	KindApply    ExecutionKind = "apply"
	KindRollback ExecutionKind = "rollback"
	KindForward  ExecutionKind = "forward"
	KindRetry    ExecutionKind = "retry"
)

type Direction string

const (
	DirectionUp   Direction = "up"
	DirectionDown Direction = "down"
)

type ExecutionStatus string

const (
	ExecutionQueued            ExecutionStatus = "queued"
	ExecutionRunning           ExecutionStatus = "running"
	ExecutionSucceeded         ExecutionStatus = "succeeded"
	ExecutionFailed            ExecutionStatus = "failed"
	ExecutionPartial           ExecutionStatus = "partial"
	ExecutionPartiallyReverted ExecutionStatus = "partially_reverted"
)

func (S ExecutionStatus) IsFinished() bool {
	return S == ExecutionSucceeded || S == ExecutionFailed || S == ExecutionPartial || S == ExecutionPartiallyReverted
}

func (S ExecutionStatus) IsFailure() bool {
	return S == ExecutionFailed || S == ExecutionPartial || S == ExecutionPartiallyReverted
}

type AttemptStatus string

const (
	AttemptRunning   AttemptStatus = "running"
	AttemptSucceeded AttemptStatus = "succeeded"
	AttemptFailed    AttemptStatus = "failed"
)

var (
	ErrInvalidInput     = Errors.New("invalid migration input")
	ErrNotFound         = Errors.New("migration not found")
	ErrConflict         = Errors.New("migration version already exists")
	ErrAlreadyExecuted  = Errors.New("migration has already been executed")
	ErrPending          = Errors.New("migration execution outcome requires verification")
	ErrOutOfOrder       = Errors.New("migration must execute in version order")
	ErrUnavailable      = Errors.New("migration store is unavailable")
	ErrForbidden        = Errors.New("permission denied for database migration")
	ErrIrreversible     = Errors.New("migration is irreversible")
	ErrNotApplied       = Errors.New("migration is not currently applied")
	ErrNotRolledBack    = Errors.New("migration is not currently rolled back")
	ErrRollbackOrder    = Errors.New("only the last active migration can be rolled back")
	ErrForwardOrder     = Errors.New("migration must be forwarded in linear order")
	ErrLocked           = Errors.New("migration definition is locked")
	ErrNoFailedAttempt  = Errors.New("migration has no failed execution to retry")
	ErrExecutionMissing = Errors.New("migration execution not found")
)

type Migration struct {
	ID                 string     `json:"id"`
	DatabaseID         string     `json:"database_id"`
	Version            int64      `json:"version"`
	Revision           int64      `json:"revision"`
	Title              string     `json:"title"`
	SQLText            string     `json:"sql_text"`
	DownSQL            *string    `json:"down_sql,omitempty"`
	BatchMode          bool       `json:"batch_mode"`
	BatchColumn        *string    `json:"batch_column,omitempty"`
	BatchSize          *int       `json:"batch_size,omitempty"`
	Irreversible       bool       `json:"irreversible"`
	IrreversibleReason *string    `json:"irreversible_reason,omitempty"`
	Status             Status     `json:"status"`
	ErrorMessage       *string    `json:"error_message,omitempty"`
	CreatedBy          *string    `json:"created_by,omitempty"`
	ExecutedBy         *string    `json:"executed_by,omitempty"`
	ExecutedByUsername *string    `json:"executed_by_username,omitempty"`
	CreatedAt          Time.Time  `json:"created_at"`
	UpdatedAt          Time.Time  `json:"updated_at"`
	ExecutedAt         *Time.Time `json:"executed_at,omitempty"`
	LockedAt           *Time.Time `json:"locked_at,omitempty"`
	CanApply           bool       `json:"can_apply"`
	CanRollback        bool       `json:"can_rollback"`
	CanForward         bool       `json:"can_forward"`
	CanRetry           bool       `json:"can_retry"`
}

type MigrationInput struct {
	Title              string `json:"title"`
	SQLText            string `json:"sql_text"`
	DownSQL            string `json:"down_sql"`
	BatchMode          bool   `json:"batch_mode"`
	BatchColumn        string `json:"batch_column"`
	BatchSize          int    `json:"batch_size"`
	Irreversible       bool   `json:"irreversible"`
	IrreversibleReason string `json:"irreversible_reason"`
}

func (Input MigrationInput) Validate() error {
	Title := Strings.TrimSpace(Input.Title)
	if len(Title) < 1 || len(Title) > 128 {
		return ErrInvalidInput
	}
	if len(Strings.TrimSpace(Input.SQLText)) < 1 {
		return ErrInvalidInput
	}
	if Input.Irreversible {
		if len(Strings.TrimSpace(Input.DownSQL)) > 0 || len(Strings.TrimSpace(Input.IrreversibleReason)) < 1 || len(Input.IrreversibleReason) > 500 {
			return ErrInvalidInput
		}
	} else if len(Strings.TrimSpace(Input.DownSQL)) < 1 {
		return ErrInvalidInput
	}
	if Input.BatchMode {
		Column := Strings.TrimSpace(Input.BatchColumn)
		if len(Column) < 1 || len(Column) > 63 || Input.BatchSize < 1 || Input.BatchSize > 1000000 {
			return ErrInvalidInput
		}
	} else if Strings.TrimSpace(Input.BatchColumn) != "" || Input.BatchSize != 0 {
		return ErrInvalidInput
	}
	return nil
}

func (Input MigrationInput) Normalized() MigrationInput {
	return MigrationInput{
		Title:              Strings.TrimSpace(Input.Title),
		SQLText:            Strings.TrimSpace(Input.SQLText),
		DownSQL:            Strings.TrimSpace(Input.DownSQL),
		BatchMode:          Input.BatchMode,
		BatchColumn:        Strings.TrimSpace(Input.BatchColumn),
		BatchSize:          Input.BatchSize,
		Irreversible:       Input.Irreversible,
		IrreversibleReason: Strings.TrimSpace(Input.IrreversibleReason),
	}
}

type Execution struct {
	ID                    string          `json:"id"`
	MigrationID           string          `json:"migration_id"`
	DatabaseID            string          `json:"database_id"`
	Version               int64           `json:"version"`
	Revision              int64           `json:"revision"`
	Title                 string          `json:"title"`
	Kind                  ExecutionKind   `json:"kind"`
	Direction             Direction       `json:"direction"`
	Status                ExecutionStatus `json:"status"`
	SQLText               string          `json:"sql_text"`
	UpSQL                 string          `json:"up_sql"`
	DownSQL               *string         `json:"down_sql,omitempty"`
	Mode                  string          `json:"mode"`
	BatchColumn           *string         `json:"batch_column,omitempty"`
	BatchSize             *int            `json:"batch_size,omitempty"`
	CommittedBatches      int64           `json:"committed_batches"`
	CommittedRows         int64           `json:"committed_rows"`
	RevertedBatches       int64           `json:"reverted_batches"`
	RevertedRows          int64           `json:"reverted_rows"`
	CheckpointCursor      int64           `json:"checkpoint_cursor"`
	RollbackOfExecutionID *string         `json:"rollback_of_execution_id,omitempty"`
	RetryOfExecutionID    *string         `json:"retry_of_execution_id,omitempty"`
	RequestedBy           *string         `json:"requested_by,omitempty"`
	RequestedByUsername   *string         `json:"requested_by_username,omitempty"`
	CreatedAt             Time.Time       `json:"created_at"`
	StartedAt             *Time.Time      `json:"started_at,omitempty"`
	FinishedAt            *Time.Time      `json:"finished_at,omitempty"`
	ErrorMessage          *string         `json:"error_message,omitempty"`
	Attempts              []Attempt       `json:"attempts"`
}

type Attempt struct {
	ID             string        `json:"id"`
	ExecutionID    string        `json:"execution_id"`
	AttemptNo      int           `json:"attempt_no"`
	Version        int64         `json:"version"`
	Revision       int64         `json:"revision"`
	ActorID        *string       `json:"actor_id,omitempty"`
	ActorUsername  *string       `json:"actor_username,omitempty"`
	SQLText        string        `json:"sql_text"`
	Status         AttemptStatus `json:"status"`
	ErrorMessage   *string       `json:"error_message,omitempty"`
	StartedAt      Time.Time     `json:"started_at"`
	FinishedAt     *Time.Time    `json:"finished_at,omitempty"`
	DurationMillis *int64        `json:"duration_ms,omitempty"`
}

type JobAction string

const (
	JobApply    JobAction = "apply"
	JobRollback JobAction = "rollback"
	JobForward  JobAction = "forward"
	JobRetry    JobAction = "retry"
)

type JobStatus string

const (
	JobQueued    JobStatus = "queued"
	JobRunning   JobStatus = "running"
	JobSucceeded JobStatus = "succeeded"
	JobFailed    JobStatus = "failed"
	JobCancelled JobStatus = "cancelled"
)

func (S JobStatus) IsTerminal() bool {
	return S == JobSucceeded || S == JobFailed || S == JobCancelled
}

type Job struct {
	ID                  string     `json:"id"`
	DatabaseID          string     `json:"database_id"`
	MigrationID         string     `json:"migration_id"`
	Version             int64      `json:"version"`
	Title               string     `json:"title"`
	Action              JobAction  `json:"action"`
	Status              JobStatus  `json:"status"`
	Attempts            int        `json:"attempts"`
	MaxAttempts         int        `json:"max_attempts"`
	AvailableAt         Time.Time  `json:"available_at"`
	LockedBy            *string    `json:"locked_by,omitempty"`
	LockedAt            *Time.Time `json:"locked_at,omitempty"`
	LeaseExpiresAt      *Time.Time `json:"lease_expires_at,omitempty"`
	LastError           *string    `json:"last_error,omitempty"`
	ExecutionID         *string    `json:"execution_id,omitempty"`
	RequestedBy         *string    `json:"requested_by,omitempty"`
	RequestedByUsername *string    `json:"requested_by_username,omitempty"`
	TraceParent         *string    `json:"traceparent,omitempty"`
	ExecutionStatus     *string    `json:"execution_status,omitempty"`
	Mode                *string    `json:"mode,omitempty"`
	CommittedBatches    int64      `json:"committed_batches"`
	CommittedRows       int64      `json:"committed_rows"`
	RevertedBatches     int64      `json:"reverted_batches"`
	RevertedRows        int64      `json:"reverted_rows"`
	CheckpointCursor    int64      `json:"checkpoint_cursor"`
	CreatedAt           Time.Time  `json:"created_at"`
	UpdatedAt           Time.Time  `json:"updated_at"`
	FinishedAt          *Time.Time `json:"finished_at,omitempty"`
}

var (
	ErrJobConflict    = Errors.New("a migration job is already active for this database")
	ErrJobNotFound    = Errors.New("migration job not found")
	ErrJobUnavailable = Errors.New("migration job store is unavailable")
)

type Batch struct {
	ID                    string     `json:"id"`
	ExecutionID           string     `json:"execution_id"`
	DatabaseID            string     `json:"database_id"`
	BatchNo               int64      `json:"batch_no"`
	CursorFrom            int64      `json:"cursor_from"`
	CursorTo              int64      `json:"cursor_to"`
	RowCount              int64      `json:"row_count"`
	Status                string     `json:"status"`
	CommittedAt           Time.Time  `json:"committed_at"`
	RevertedAt            *Time.Time `json:"reverted_at,omitempty"`
	RevertedByExecutionID *string    `json:"reverted_by_execution_id,omitempty"`
}
