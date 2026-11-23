package Store

import (
	Context "context"
	Errors "errors"
	Pgx "github.com/jackc/pgx/v5"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
)

// Open connects the platform pool. An optional tracer receives every statement.
func Open(Ctx Context.Context, DatabaseURL string, Tracers ...Pgx.QueryTracer) (*Pgxpool.Pool, error) {
	Cfg, Err := Pgxpool.ParseConfig(DatabaseURL)
	if Err != nil {
		return nil, Errors.New("invalid platform database configuration")
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
