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
	Migration "github.com/ethj0r/IF3110-K02-01/backend/internal/migration"
	Store "github.com/ethj0r/IF3110-K02-01/backend/internal/store"
	Slog "log/slog"
	Os "os"
	Signal "os/signal"
	Strings "strings"
	Syscall "syscall"
	Time "time"
)

func main() {
	Slog.SetDefault(Slog.New(Slog.NewJSONHandler(Os.Stdout, &Slog.HandlerOptions{Level: Slog.LevelInfo})))
	if Err := Run(); Err != nil {
		Slog.Error("worker stopped", "error", Err)
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
	Startup, Cancel := Context.WithTimeout(Ctx, 10*Time.Second)
	Pool, Err := Store.Open(Startup, Cfg.DatabaseURL)
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
	Service := &Migration.Service{
		Repo:   &Migration.Repository{Pool: Pool},
		Access: AccessRepo,
		Resolver: Migration.DSNResolverFunc(func(Ctx Context.Context, DatabaseID string) (string, error) {
			_, DSN, Err := ConnectionService.DialDSN(Ctx, DatabaseID)
			return DSN, Err
		}),
		Driver: Driver.PostgresDriver{},
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
