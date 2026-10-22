package Store

import (
	Context "context"
	Errors "errors"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
)

func Open(Ctx Context.Context, DatabaseURL string) (*Pgxpool.Pool, error) {
	Cfg, Err := Pgxpool.ParseConfig(DatabaseURL)
	if Err != nil {
		return nil, Errors.New("invalid platform database configuration")
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
