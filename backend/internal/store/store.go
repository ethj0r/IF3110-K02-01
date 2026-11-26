package Store

import (
	Context "context"
	Errors "errors"
	Pgx "github.com/jackc/pgx/v5"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
)

// KeepaliveParams make PostgreSQL probe idle platform sessions, so a worker that
// dies or is partitioned while holding a migration advisory lock loses that lock
// (and its session) within about a minute instead of the OS default of hours.
// Values already present in the DSN win.
var KeepaliveParams = map[string]string{
	"tcp_keepalives_idle":     "30",
	"tcp_keepalives_interval": "10",
	"tcp_keepalives_count":    "3",
}

// Open connects the platform pool. An optional tracer receives every statement.
func Open(Ctx Context.Context, DatabaseURL string, Tracers ...Pgx.QueryTracer) (*Pgxpool.Pool, error) {
	Cfg, Err := Pgxpool.ParseConfig(DatabaseURL)
	if Err != nil {
		return nil, Errors.New("invalid platform database configuration")
	}
	for Name, Value := range KeepaliveParams {
		if _, Set := Cfg.ConnConfig.RuntimeParams[Name]; !Set {
			Cfg.ConnConfig.RuntimeParams[Name] = Value
		}
	}
	if len(Tracers) > 0 && Tracers[0] != nil {
		Cfg.ConnConfig.Tracer = Tracers[0]
	}
	Pool, Err := Pgxpool.NewWithConfig(Ctx, Cfg)
	if Err != nil {
		return nil, Errors.New("cannot initialize platform database pool")
	}
	if Err := Pool.Ping(Ctx); Err != nil {
		Pool.Close()
		return nil, Errors.New("platform database is unavailable")
	}
	return Pool, nil
}
