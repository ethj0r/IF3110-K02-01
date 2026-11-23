package main

import (
	Context "context"
	Errors "errors"
	Fmt "fmt"
	Config "github.com/ethj0r/IF3110-K02-01/backend/internal/config"
	Logging "github.com/ethj0r/IF3110-K02-01/backend/internal/logging"
	Migrations "github.com/ethj0r/IF3110-K02-01/backend/migrations"
	Pgx "github.com/jackc/pgx/v5"
	Stdlib "github.com/jackc/pgx/v5/stdlib"
	Goose "github.com/pressly/goose/v3"
	Slog "log/slog"
	Os "os"
	Time "time"
)

func main() {
	Slog.SetDefault(Logging.New(Os.Stderr, Logging.ParseLevel(Os.Getenv("LOG_LEVEL"))))
	if Err := Run(); Err != nil {
		Slog.Error("migration command failed", "error", Err)
		Os.Exit(1)
	}
}

func Run() error {
	if len(Os.Args) != 2 || (Os.Args[1] != "up" && Os.Args[1] != "status") {
		return Errors.New("usage: migrate [up|status]")
	}
	Cfg, Err := Config.Load()
	if Err != nil {
		return Err
	}
	ConnConfig, Err := Pgx.ParseConfig(Cfg.DatabaseURL)
	if Err != nil {
		return Errors.New("invalid platform database configuration")
	}
	ConnConfig.Tracer = Logging.SQLTracer{Database: "platform"}
	Db := Stdlib.OpenDB(*ConnConfig)
	defer Db.Close()
	Ctx, Cancel := Context.WithTimeout(Context.Background(), Time.Minute)
	defer Cancel()
	if Err := Db.PingContext(Ctx); Err != nil {
		return Errors.New("platform database is unavailable")
	}
	Provider, Err := Goose.NewProvider(Goose.DialectPostgres, Db, Migrations.FS())
	if Errors.Is(Err, Goose.ErrNoMigrations) {
		Fmt.Println("No platform migrations yet; schema awaits the Phase 0 contract.")
		return nil
	}
	if Err != nil {
		return Errors.New("cannot initialize platform migrations")
	}
	if Os.Args[1] == "up" {
		Results, Err := Provider.Up(Ctx)
		if Err != nil {
			return Errors.New("platform migrations failed; inspect the migration files and database")
		}
		Fmt.Printf("Applied %d platform migrations.\n", len(Results))
		return nil
	}
	Statuses, Err := Provider.Status(Ctx)
	if Err != nil {
		return Errors.New("cannot read platform migration status")
	}
	for _, Status := range Statuses {
		Fmt.Printf("%s\t%s\n", Status.State, Status.Source.Path)
	}
	return nil
}
