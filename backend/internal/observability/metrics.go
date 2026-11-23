package Observability

import (
	Context "context"
	Fmt "fmt"
	Http "net/http"
	Time "time"

	Httpx "github.com/ethj0r/IF3110-K02-01/backend/internal/httpx"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
)

type MetricsHandler struct {
	Pool *Pgxpool.Pool
}

const jobMetrics = `SELECT
    count(*) FILTER (WHERE status = 'queued'),
    count(*) FILTER (WHERE status = 'running'),
    count(*) FILTER (WHERE status = 'succeeded'),
    count(*) FILTER (WHERE status = 'failed'),
    count(*) FILTER (WHERE status = 'partial'),
    count(*) FILTER (WHERE status = 'partially_reverted'),
    count(*) FILTER (WHERE kind = 'retry'),
    COALESCE(avg(EXTRACT(EPOCH FROM finished_at - started_at))
        FILTER (WHERE finished_at >= started_at), 0)::double precision,
    (avg(EXTRACT(EPOCH FROM started_at - created_at))
        FILTER (WHERE started_at > created_at))::double precision
FROM migration_executions`

const prometheusMetrics = `# HELP wbd_migration_jobs Retained migration executions by status.
# TYPE wbd_migration_jobs gauge
wbd_migration_jobs{status="queued"} %d
wbd_migration_jobs{status="running"} %d
wbd_migration_jobs{status="succeeded"} %d
wbd_migration_jobs{status="failed"} %d
wbd_migration_jobs{status="partial"} %d
wbd_migration_jobs{status="partially_reverted"} %d
# HELP wbd_migration_retry_jobs Retained migration executions requested as retries.
# TYPE wbd_migration_retry_jobs gauge
wbd_migration_retry_jobs %d
# HELP wbd_migration_execution_mean_seconds Mean execution duration across retained jobs with start and finish timestamps.
# TYPE wbd_migration_execution_mean_seconds gauge
wbd_migration_execution_mean_seconds %g
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

	var Queued, Running, Succeeded, Failed, Partial, PartiallyReverted, Retries int64
	var MeanDuration float64
	var MeanQueueWait *float64
	Err := Metrics.Pool.QueryRow(Ctx, jobMetrics).Scan(
		&Queued, &Running, &Succeeded, &Failed, &Partial, &PartiallyReverted, &Retries, &MeanDuration, &MeanQueueWait,
	)
	if Err != nil {
		Httpx.Error(W, Http.StatusServiceUnavailable, "metrics_unavailable", "Metrics are temporarily unavailable")
		return
	}

	Stats := Metrics.Pool.Stat()
	W.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	W.Header().Set("Cache-Control", "no-store")
	_, _ = Fmt.Fprintf(W, prometheusMetrics, Queued, Running, Succeeded, Failed, Partial, PartiallyReverted,
		Retries, MeanDuration, Stats.AcquiredConns(), Stats.IdleConns(),
		Stats.EmptyAcquireWaitTime().Seconds(), Stats.EmptyAcquireCount())
	if MeanQueueWait != nil {
		_, _ = Fmt.Fprintf(W, `# HELP wbd_migration_queue_wait_mean_seconds Mean time from job creation to start for retained jobs with a recorded delay.
# TYPE wbd_migration_queue_wait_mean_seconds gauge
wbd_migration_queue_wait_mean_seconds %g
`, *MeanQueueWait)
	}
}
