package Migration

import (
	Context "context"
	Errors "errors"
	Slog "log/slog"
	Time "time"

	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
	Logging "github.com/ethj0r/IF3110-K02-01/backend/internal/logging"
	Tracing "github.com/ethj0r/IF3110-K02-01/backend/internal/tracing"
	Attribute "go.opentelemetry.io/otel/attribute"
	Trace "go.opentelemetry.io/otel/trace"
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
	W.logger().InfoContext(Ctx, "worker started", "worker_id", W.ID, "lease", W.lease().String(), "poll", W.poll().String())
	for {
		if Ctx.Err() != nil {
			W.logger().InfoContext(Ctx, "worker stopped", "worker_id", W.ID)
			return nil
		}
		if Count, Err := W.Repo.ExpireStaleJobs(Ctx, 20); Err != nil {
			W.logger().ErrorContext(Ctx, "worker could not expire stale jobs", "worker_id", W.ID, "error", Err)
		} else if Count > 0 {
			W.logger().WarnContext(Ctx, "worker expired stale jobs", "worker_id", W.ID, "count", Count)
		}
		Job, Err := W.Repo.ClaimJob(Ctx, W.ID, W.lease())
		if Err != nil {
			if !Errors.Is(Err, ErrJobNotFound) {
				W.logger().ErrorContext(Ctx, "worker could not claim a job", "worker_id", W.ID, "error", Err)
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

// Process runs one claimed job. It continues the trace stored with the job, so
// the worker span joins the API request that enqueued it, and every record and
// SQL statement of the job carries job_id, worker_id, and attempt.
func (W *Worker) Process(Ctx Context.Context, Job Job) {
	User := Auth.Principal{}
	if Job.RequestedBy != nil {
		User.ID = *Job.RequestedBy
	}
	if Job.RequestedByUsername != nil {
		User.Username = *Job.RequestedByUsername
	}
	QueueWait := float64(Time.Since(Job.CreatedAt).Microseconds()) / 1000
	Ctx = Tracing.Extract(Ctx, stringValue(Job.TraceParent))
	Ctx, Span := Tracing.Tracer().Start(Ctx, "migration.job "+string(Job.Action),
		Trace.WithSpanKind(Trace.SpanKindConsumer),
		Trace.WithAttributes(
			Attribute.String("job_id", Job.ID), Attribute.String("worker_id", W.ID),
			Attribute.String("database_id", Job.DatabaseID), Attribute.String("migration_id", Job.MigrationID),
			Attribute.String("migration.action", string(Job.Action)), Attribute.Int("attempt", Job.Attempts),
			Attribute.Float64("queue.wait_ms", QueueWait),
		))
	defer Span.End()
	Ctx = Logging.NewScope(Ctx, "job_id", Job.ID, "worker_id", W.ID, "database_id", Job.DatabaseID,
		"migration_id", Job.MigrationID, "action", string(Job.Action), "attempt", Job.Attempts,
		"actor_id", User.ID, "actor", User.Username)
	HeartbeatCtx, StopHeartbeat := Context.WithCancel(Ctx)
	defer StopHeartbeat()
	go W.heartbeat(HeartbeatCtx, Job.ID)
	W.logger().InfoContext(Ctx, "worker claimed migration job",
		"event", "migration.job.claimed", "queue_wait_ms", QueueWait)

	Latest, LatestErr := W.Repo.LatestExecution(Ctx, Job.DatabaseID, Job.MigrationID)
	AutoRetry := Job.Attempts > 1 && LatestErr == nil && Latest.Status.IsFailure() && Latest.Kind == kindForJobAction(Job.Action)

	var Exec Execution
	var Err error
	if AutoRetry {
		Exec, Err = W.Service.RetryTransient(Ctx, User, Job.DatabaseID, Job.MigrationID, Latest.ID, kindForJobAction(Job.Action))
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
			W.logger().ErrorContext(Ctx, "worker could not record job success", "event", "migration.job.finish_failed", "worker_id", W.ID, "job_id", Job.ID, "error", FinishErr)
			return
		}
		W.logger().InfoContext(Ctx, "worker completed migration job",
			"event", "migration.job.succeeded", "worker_id", W.ID,
			"job_id", Job.ID, "execution_id", stringValue(ExecutionID), "action", Job.Action)
		return
	}

	if Errors.Is(Err, ErrAlreadyExecuted) {
		if FinishErr := W.Repo.FinishJob(Ctx, Job.ID, JobSucceeded, ExecutionID, nil); FinishErr == nil {
			W.logger().InfoContext(Ctx, "worker job was already applied",
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
			Span.SetAttributes(Attribute.String("job.outcome", "retry_scheduled"), Attribute.Float64("retry.delay_ms", float64(Delay.Milliseconds())))
			W.logger().WarnContext(Ctx, "worker scheduled a migration job retry",
				"event", "migration.job.retry_scheduled", "worker_id", W.ID,
				"job_id", Job.ID, "attempt", Job.Attempts, "delay", Delay.String(), "error", Message)
			return
		} else {
			W.logger().ErrorContext(Ctx, "worker could not requeue a migration job", "event", "migration.job.requeue_failed", "worker_id", W.ID, "job_id", Job.ID, "error", RequeueErr)
		}
	}
	if FinishErr := W.Repo.FinishJob(Ctx, Job.ID, JobFailed, ExecutionID, &Message); FinishErr != nil {
		W.logger().ErrorContext(Ctx, "worker could not record job failure", "event", "migration.job.finish_failed", "worker_id", W.ID, "job_id", Job.ID, "error", FinishErr)
		return
	}
	Tracing.Fail(Span, Err, Message)
	W.logger().ErrorContext(Ctx, "worker failed migration job",
		"event", "migration.job.failed", "worker_id", W.ID,
		"job_id", Job.ID, "execution_id", stringValue(ExecutionID), "action", Job.Action,
		"attempt", Job.Attempts, "error", Message)
}

func (W *Worker) heartbeat(Ctx Context.Context, JobID string) {
	Interval := W.lease() / 3
	if Interval < Time.Second {
		Interval = Time.Second
	}
	Ticker := Time.NewTicker(Interval)
	defer Ticker.Stop()
	for {
		select {
		case <-Ctx.Done():
			return
		case <-Ticker.C:
			// A transient store error is retried on the next tick (two more fit in
			// one lease). Only a lost lease stops the heartbeat: the job row was
			// expired or reassigned, and the advisory lock still prevents a second
			// execution on this database.
			Err := W.Repo.RenewJobLease(Ctx, JobID, W.ID, W.lease())
			if Err == nil || Ctx.Err() != nil {
				continue
			}
			W.logger().WarnContext(Ctx, "worker could not renew job lease",
				"event", "migration.job.heartbeat_failed", "worker_id", W.ID, "job_id", JobID, "error", Err)
			if Errors.Is(Err, ErrJobNotFound) {
				return
			}
		}
	}
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
	case Errors.Is(Err, ErrExecutionTimeout):
		return "migration exceeded its execution timeout; transaction rolled back"
	case Errors.Is(Err, Driver.ErrExecution):
		return "target SQL execution failed"
	case Errors.Is(Err, Driver.ErrUnavailable), Errors.Is(Err, ErrUnavailable), Errors.Is(Err, ErrJobUnavailable):
		return "migration store or target database unavailable"
	case Errors.Is(Err, ErrForbidden):
		return "permission denied for database migration"
	case Errors.Is(Err, ErrApprovalRequired):
		return "migration requires approval from every group"
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
