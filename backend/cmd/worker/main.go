package main

import (
	Context "context"
	Errors "errors"
	Fmt "fmt"
	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Config "github.com/ethj0r/IF3110-K02-01/backend/internal/config"
	Connection "github.com/ethj0r/IF3110-K02-01/backend/internal/connection"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
	Logging "github.com/ethj0r/IF3110-K02-01/backend/internal/logging"
	Migration "github.com/ethj0r/IF3110-K02-01/backend/internal/migration"
	Store "github.com/ethj0r/IF3110-K02-01/backend/internal/store"
	Tracing "github.com/ethj0r/IF3110-K02-01/backend/internal/tracing"
	Slog "log/slog"
	Os "os"
	Signal "os/signal"
	Strings "strings"
	Syscall "syscall"
	Time "time"
)

func main() {
	Slog.SetDefault(Logging.New(Os.Stdout, Logging.ParseLevel(Os.Getenv("LOG_LEVEL"))))
	if Err := Run(); Err != nil {
		Slog.Error("worker stopped", "event", "worker.stopped", "error", Err)
		Os.Exit(1)
	}
}

func Run() error {
	Cfg, Err := Config.Load()
	if Err != nil {
		return Err
	}
	Settings, Err := Auth.LoadOptions()
	if Err != nil {
		return Err
	}
	Ctx, Stop := Signal.NotifyContext(Context.Background(), Os.Interrupt, Syscall.SIGTERM)
	defer Stop()
	ShutdownTracing, Err := Tracing.Setup(Ctx, Tracing.OptionsFromEnv("wbd-worker"))
	if Err != nil {
		return Err
	}
	defer func() {
		Flush, Cancel := Context.WithTimeout(Context.Background(), 5*Time.Second)
		defer Cancel()
		_ = ShutdownTracing(Flush)
	}()
	LogHandler, ShutdownLogs, Err := Tracing.SetupLogs(Ctx, Tracing.OptionsFromEnv("wbd-worker").ServiceName, Tracing.LogExporterFromEnv())
	if Err != nil {
		return Err
	}
	defer func() {
		Flush, Cancel := Context.WithTimeout(Context.Background(), 5*Time.Second)
		defer Cancel()
		_ = ShutdownLogs(Flush)
	}()
	Slog.SetDefault(Logging.New(Os.Stdout, Logging.ParseLevel(Os.Getenv("LOG_LEVEL")), LogHandler))
	Startup, Cancel := Context.WithTimeout(Ctx, 10*Time.Second)
	Pool, Err := Store.Open(Startup, Cfg.DatabaseURL, Logging.SQLTracer{Database: "platform"})
	Cancel()
	if Err != nil {
		return Err
	}
	defer Pool.Close()
	Cipher, Err := Connection.NewCipher(Settings.EncryptionKey)
	if Err != nil {
		return Err
	}
	ConnectionService := &Connection.Service{Cipher: Cipher, Repo: &Connection.Repository{Pool: Pool}}
	AccessRepo := &Access.Repository{Pool: Pool}
	MigrationRepo := Migration.NewReviewedRepository(Pool)
	Service := &Migration.Service{
		Repo:    MigrationRepo,
		Reviews: MigrationRepo,
		Access:  AccessRepo,
		Resolver: Migration.DSNResolverFunc(func(Ctx Context.Context, DatabaseID string) (string, error) {
			_, DSN, Err := ConnectionService.DialDSN(Ctx, DatabaseID)
			return DSN, Err
		}),
		Driver: Driver.PostgresDriver{Tracer: Logging.SQLTracer{Database: "target"}},
		Logger: Slog.Default(),
	}
	WorkerID := Strings.TrimSpace(Os.Getenv("WORKER_ID"))
	if WorkerID == "" {
		Hostname, _ := Os.Hostname()
		WorkerID = Fmt.Sprintf("%s-%d", Hostname, Os.Getpid())
	}
	Lease := 2 * Time.Minute
	if Raw := Strings.TrimSpace(Os.Getenv("WORKER_LEASE")); Raw != "" {
		Parsed, Err := Time.ParseDuration(Raw)
		if Err != nil || Parsed <= 0 {
			return Errors.New("WORKER_LEASE must be a positive duration")
		}
		Lease = Parsed
	}
	Poll := 2 * Time.Second
	if Raw := Strings.TrimSpace(Os.Getenv("WORKER_POLL")); Raw != "" {
		Parsed, Err := Time.ParseDuration(Raw)
		if Err != nil || Parsed <= 0 {
			return Errors.New("WORKER_POLL must be a positive duration")
		}
		Poll = Parsed
	}
	Runner := &Migration.Worker{
		Repo:    Service.Repo,
		Service: Service,
		Logger:  Slog.Default(),
		ID:      WorkerID,
		Lease:   Lease,
		Poll:    Poll,
	}
	return Runner.Run(Ctx)
}
