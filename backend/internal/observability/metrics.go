package Observability

import (
	Context "context"
	Fmt "fmt"
	Http "net/http"
	Time "time"

	Httpx "github.com/ethj0r/IF3110-K02-01/backend/internal/httpx"
	Migration "github.com/ethj0r/IF3110-K02-01/backend/internal/migration"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
)

type MetricsHandler struct {
	Pool *Pgxpool.Pool
}

const jobMetrics = `WITH Jobs AS (
    SELECT count(*) FILTER (WHERE status = 'queued') AS queued,
           count(*) FILTER (WHERE status = 'running') AS running,
           count(*) FILTER (WHERE status = 'succeeded') AS succeeded,
           count(*) FILTER (WHERE status = 'failed') AS failed,
           count(*) FILTER (WHERE status = 'cancelled') AS cancelled,
           count(*) FILTER (WHERE action = 'retry') AS retry_jobs,
           COALESCE(sum(GREATEST(attempts - 1, 0)), 0) AS automatic_retries,
           (avg(EXTRACT(EPOCH FROM locked_at - available_at))
               FILTER (WHERE locked_at >= available_at))::double precision AS mean_queue_wait
    FROM migration_jobs
), Executions AS (
    SELECT COALESCE(avg(EXTRACT(EPOCH FROM finished_at - started_at))
        FILTER (WHERE finished_at >= started_at), 0)::double precision AS mean_duration
    FROM migration_executions
), Batches AS (
    SELECT count(*) AS up_batches,
           count(*) FILTER (WHERE status = 'reverted') AS down_batches,
           COALESCE(sum(row_count), 0)::bigint AS up_rows,
           COALESCE(sum(row_count) FILTER (WHERE status = 'reverted'), 0)::bigint AS down_rows
    FROM migration_batches
), Locks AS (
    SELECT COALESCE(max(EXTRACT(EPOCH FROM clock_timestamp() - L.waitstart)), 0)::double precision AS max_wait
    FROM pg_catalog.pg_locks L
    JOIN pg_catalog.pg_stat_activity A ON A.pid = L.pid
    WHERE L.locktype = 'advisory' AND NOT L.granted
      AND A.datname = current_database() AND A.query = $1
)
SELECT J.queued, J.running, J.succeeded, J.failed, J.cancelled, J.retry_jobs, J.automatic_retries, J.mean_queue_wait,
       E.mean_duration, B.up_batches, B.down_batches, B.up_rows, B.down_rows, L.max_wait
FROM Jobs J CROSS JOIN Executions E CROSS JOIN Batches B CROSS JOIN Locks L`

const prometheusMetrics = `# HELP wbd_migration_jobs Retained worker jobs by status.
# TYPE wbd_migration_jobs gauge
wbd_migration_jobs{status="queued"} %d
wbd_migration_jobs{status="running"} %d
wbd_migration_jobs{status="succeeded"} %d
wbd_migration_jobs{status="failed"} %d
wbd_migration_jobs{status="cancelled"} %d
# HELP wbd_migration_retry_jobs Retained jobs explicitly requested as retries.
# TYPE wbd_migration_retry_jobs gauge
wbd_migration_retry_jobs %d
# HELP wbd_migration_retry_attempts Automatic retry attempts across retained jobs.
# TYPE wbd_migration_retry_attempts gauge
wbd_migration_retry_attempts %d
# HELP wbd_migration_execution_mean_seconds Mean execution duration across retained jobs with start and finish timestamps.
# TYPE wbd_migration_execution_mean_seconds gauge
wbd_migration_execution_mean_seconds %g
# HELP wbd_migration_lock_wait_max_seconds Longest current wait for a migration advisory lock.
# TYPE wbd_migration_lock_wait_max_seconds gauge
wbd_migration_lock_wait_max_seconds %g
# HELP wbd_migration_batches_processed Batches applied or reverted across retained executions.
# TYPE wbd_migration_batches_processed gauge
wbd_migration_batches_processed{direction="up"} %d
wbd_migration_batches_processed{direction="down"} %d
# HELP wbd_migration_rows_processed Rows applied or reverted across retained batches.
# TYPE wbd_migration_rows_processed gauge
wbd_migration_rows_processed{direction="up"} %d
wbd_migration_rows_processed{direction="down"} %d
# HELP wbd_platform_pool_connections Connections currently checked out or idle in the platform pool.
# TYPE wbd_platform_pool_connections gauge
wbd_platform_pool_connections{state="active"} %d
wbd_platform_pool_connections{state="idle"} %d
# HELP wbd_platform_pool_wait_seconds_total Time spent waiting for successful acquisitions from an empty platform pool.
# TYPE wbd_platform_pool_wait_seconds_total counter
wbd_platform_pool_wait_seconds_total %g
# HELP wbd_platform_pool_waited_acquires_total Successful acquisitions that waited for an empty platform pool.
# TYPE wbd_platform_pool_waited_acquires_total counter
wbd_platform_pool_waited_acquires_total %d
`

func (Metrics MetricsHandler) ServeHTTP(W Http.ResponseWriter, R *Http.Request) {
	Ctx, Cancel := Context.WithTimeout(R.Context(), 2*Time.Second)
	defer Cancel()

	var Queued, Running, Succeeded, Failed, Cancelled, RetryJobs, AutomaticRetries int64
	var UpBatches, DownBatches, UpRows, DownRows int64
	var MeanDuration, MaxLockWait float64
	var MeanQueueWait *float64
	Err := Metrics.Pool.QueryRow(Ctx, jobMetrics, Migration.AdvisoryLockSQL).Scan(
		&Queued, &Running, &Succeeded, &Failed, &Cancelled, &RetryJobs, &AutomaticRetries, &MeanQueueWait,
		&MeanDuration, &UpBatches, &DownBatches, &UpRows, &DownRows, &MaxLockWait,
	)
	if Err != nil {
		Httpx.Error(W, Http.StatusServiceUnavailable, "metrics_unavailable", "Metrics are temporarily unavailable")
		return
	}

	Stats := Metrics.Pool.Stat()
	W.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	W.Header().Set("Cache-Control", "no-store")
	_, _ = Fmt.Fprintf(W, prometheusMetrics, Queued, Running, Succeeded, Failed, Cancelled,
		RetryJobs, AutomaticRetries, MeanDuration, MaxLockWait, UpBatches, DownBatches, UpRows, DownRows,
		Stats.AcquiredConns(), Stats.IdleConns(),
		Stats.EmptyAcquireWaitTime().Seconds(), Stats.EmptyAcquireCount())
	if MeanQueueWait != nil {
		_, _ = Fmt.Fprintf(W, `# HELP wbd_migration_queue_wait_mean_seconds Mean time from eligibility to latest recorded worker claim across retained jobs.
# TYPE wbd_migration_queue_wait_mean_seconds gauge
wbd_migration_queue_wait_mean_seconds %g
`, *MeanQueueWait)
	}
}
