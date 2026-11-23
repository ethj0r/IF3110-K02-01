package Migration

import (
	Context "context"
	Errors "errors"
	Slog "log/slog"
	Time "time"

	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
)

type Worker struct {
	Repo    MigrationRepository
	Service *Service
	Logger  *Slog.Logger
	ID      string
	Lease   Time.Duration
	Poll    Time.Duration
}

func (W *Worker) logger() *Slog.Logger {
	if W.Logger != nil {
		return W.Logger
	}
	return Slog.Default()
}

func (W *Worker) lease() Time.Duration {
	if W.Lease > 0 {
		return W.Lease
	}
	return 2 * Time.Minute
}

func (W *Worker) poll() Time.Duration {
	if W.Poll > 0 {
		return W.Poll
	}
	return 2 * Time.Second
}

func (W *Worker) Run(Ctx Context.Context) error {
	W.logger().Info("worker started", "worker_id", W.ID, "lease", W.lease().String(), "poll", W.poll().String())
	for {
		if Ctx.Err() != nil {
			W.logger().Info("worker stopped", "worker_id", W.ID)
			return nil
		}
		if Count, Err := W.Repo.ExpireStaleJobs(Ctx, 20); Err != nil {
			W.logger().Error("worker could not expire stale jobs", "worker_id", W.ID, "error", Err)
		} else if Count > 0 {
			W.logger().Warn("worker expired stale jobs", "worker_id", W.ID, "count", Count)
		}
		Job, Err := W.Repo.ClaimJob(Ctx, W.ID, W.lease())
		if Err != nil {
			if !Errors.Is(Err, ErrJobNotFound) {
				W.logger().Error("worker could not claim a job", "worker_id", W.ID, "error", Err)
			}
			if !sleep(Ctx, W.poll()) {
				return nil
			}
			continue
		}
		W.Process(Ctx, Job)
	}
}

func sleep(Ctx Context.Context, Duration Time.Duration) bool {
	Timer := Time.NewTimer(Duration)
	defer Timer.Stop()
	select {
	case <-Ctx.Done():
		return false
	case <-Timer.C:
		return true
	}
}

func (W *Worker) Process(Ctx Context.Context, Job Job) {
	User := Auth.Principal{}
	if Job.RequestedBy != nil {
		User.ID = *Job.RequestedBy
	}
	if Job.RequestedByUsername != nil {
		User.Username = *Job.RequestedByUsername
	}
	W.logger().Info("worker claimed migration job",
		"event", "migration.job.claimed", "worker_id", W.ID,
		"job_id", Job.ID, "database_id", Job.DatabaseID, "migration_id", Job.MigrationID,
		"action", Job.Action, "attempt", Job.Attempts, "traceparent", stringValue(Job.TraceParent))

	Latest, LatestErr := W.Repo.LatestExecution(Ctx, Job.DatabaseID, Job.MigrationID)
	UseRetry := LatestErr == nil && (Latest.Status.IsFailure() || Latest.Status == ExecutionPartial || Latest.Status == ExecutionPartiallyReverted)

	var Exec Execution
	var Err error
	if UseRetry {
		Exec, Err = W.Service.Retry(Ctx, User, Job.DatabaseID, Job.MigrationID)
	} else {
		switch Job.Action {
		case JobApply:
			Exec, Err = W.Service.Apply(Ctx, User, Job.DatabaseID, Job.MigrationID)
		case JobRollback:
			Exec, Err = W.Service.Rollback(Ctx, User, Job.DatabaseID, Job.MigrationID)
		case JobForward:
			Exec, Err = W.Service.Forward(Ctx, User, Job.DatabaseID, Job.MigrationID)
		case JobRetry:
			Exec, Err = W.Service.Retry(Ctx, User, Job.DatabaseID, Job.MigrationID)
		default:
			Err = ErrInvalidInput
		}
	}

	var ExecutionID *string
	if Exec.ID != "" {
		ExecutionID = &Exec.ID
	} else if Latest.ID != "" {
		ExecutionID = &Latest.ID
	} else if Latest, LatestErr = W.Repo.LatestExecution(Ctx, Job.DatabaseID, Job.MigrationID); LatestErr == nil {
		ExecutionID = &Latest.ID
	}
	if Err == nil {
		if FinishErr := W.Repo.FinishJob(Ctx, Job.ID, JobSucceeded, ExecutionID, nil); FinishErr != nil {
			W.logger().Error("worker could not record job success", "event", "migration.job.finish_failed", "worker_id", W.ID, "job_id", Job.ID, "error", FinishErr)
			return
		}
		W.logger().Info("worker completed migration job",
			"event", "migration.job.succeeded", "worker_id", W.ID,
			"job_id", Job.ID, "execution_id", stringValue(ExecutionID), "action", Job.Action)
		return
	}

	if Errors.Is(Err, ErrAlreadyExecuted) {
		if FinishErr := W.Repo.FinishJob(Ctx, Job.ID, JobSucceeded, ExecutionID, nil); FinishErr == nil {
			W.logger().Info("worker job was already applied",
				"event", "migration.job.idempotent", "worker_id", W.ID,
				"job_id", Job.ID, "execution_id", stringValue(ExecutionID), "action", Job.Action)
			return
		}
	}

	Latest, LatestErr = W.Repo.LatestExecution(Ctx, Job.DatabaseID, Job.MigrationID)
	Uncertain := LatestErr == nil && (Latest.Status == ExecutionQueued || Latest.Status == ExecutionRunning || Latest.Status == ExecutionPartial || Latest.Status == ExecutionPartiallyReverted)
	Message := jobErrorMessage(Err)
	if !Uncertain && W.retryable(Err) && Job.Attempts < Job.MaxAttempts {
		Delay := retryDelay(Job.Attempts)
		if RequeueErr := W.Repo.RequeueJob(Ctx, Job.ID, Time.Now().Add(Delay), Message); RequeueErr == nil {
			W.logger().Warn("worker scheduled a migration job retry",
				"event", "migration.job.retry_scheduled", "worker_id", W.ID,
				"job_id", Job.ID, "attempt", Job.Attempts, "delay", Delay.String(), "error", Message)
			return
		} else {
			W.logger().Error("worker could not requeue a migration job", "event", "migration.job.requeue_failed", "worker_id", W.ID, "job_id", Job.ID, "error", RequeueErr)
		}
	}
	if FinishErr := W.Repo.FinishJob(Ctx, Job.ID, JobFailed, ExecutionID, &Message); FinishErr != nil {
		W.logger().Error("worker could not record job failure", "event", "migration.job.finish_failed", "worker_id", W.ID, "job_id", Job.ID, "error", FinishErr)
		return
	}
	W.logger().Error("worker failed migration job",
		"event", "migration.job.failed", "worker_id", W.ID,
		"job_id", Job.ID, "execution_id", stringValue(ExecutionID), "action", Job.Action,
		"attempt", Job.Attempts, "error", Message)
}

func (W *Worker) retryable(Err error) bool {
	return Errors.Is(Err, ErrUnavailable) || Errors.Is(Err, Driver.ErrUnavailable) || Errors.Is(Err, ErrJobUnavailable)
}

func retryDelay(Attempts int) Time.Duration {
	if Attempts < 1 {
		Attempts = 1
	}
	Shift := Attempts - 1
	if Shift > 4 {
		Shift = 4
	}
	Delay := Time.Duration(1<<Shift) * 2 * Time.Second
	if Delay > 30*Time.Second {
		return 30 * Time.Second
	}
	return Delay
}

func jobErrorMessage(Err error) string {
	switch {
	case Errors.Is(Err, ErrPending):
		return "execution outcome requires verification"
	case Errors.Is(Err, Driver.ErrExecution):
		return "target SQL execution failed"
	case Errors.Is(Err, Driver.ErrUnavailable), Errors.Is(Err, ErrUnavailable), Errors.Is(Err, ErrJobUnavailable):
		return "migration store or target database unavailable"
	case Errors.Is(Err, ErrForbidden):
		return "permission denied for database migration"
	case Errors.Is(Err, ErrInvalidInput):
		return "invalid migration request"
	case Errors.Is(Err, ErrNotFound):
		return "migration or target database not found"
	case Errors.Is(Err, ErrConflict):
		return "migration version conflict"
	case Errors.Is(Err, ErrLocked):
		return "migration definition is locked"
	case Errors.Is(Err, ErrIrreversible):
		return "migration is irreversible"
	case Errors.Is(Err, ErrNotApplied), Errors.Is(Err, ErrNotRolledBack):
		return "migration is not in the required state"
	case Errors.Is(Err, ErrRollbackOrder), Errors.Is(Err, ErrForwardOrder), Errors.Is(Err, ErrOutOfOrder):
		return "migration is out of order"
	case Errors.Is(Err, ErrNoFailedAttempt):
		return "migration has no failed execution to retry"
	default:
		return "migration job failed"
	}
}

func stringValue(Value *string) string {
	if Value == nil {
		return ""
	}
	return *Value
}
