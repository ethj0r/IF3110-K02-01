package main

import (
	Context "context"
	Errors "errors"
	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
	Admin "github.com/ethj0r/IF3110-K02-01/backend/internal/admin"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Config "github.com/ethj0r/IF3110-K02-01/backend/internal/config"
	Connection "github.com/ethj0r/IF3110-K02-01/backend/internal/connection"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
	Httpx "github.com/ethj0r/IF3110-K02-01/backend/internal/httpx"
	Inspect "github.com/ethj0r/IF3110-K02-01/backend/internal/inspect"
	Migration "github.com/ethj0r/IF3110-K02-01/backend/internal/migration"
	Store "github.com/ethj0r/IF3110-K02-01/backend/internal/store"
	Webui "github.com/ethj0r/IF3110-K02-01/backend/internal/webui"
	Slog "log/slog"
	Http "net/http"
	Os "os"
	Signal "os/signal"
	Syscall "syscall"
	Time "time"
)

func main() {
	if Err := Run(); Err != nil {
		Slog.Error("server stopped", "error", Err)
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
	if Err != nil {
		Cancel()
		return Err
	}
	defer Pool.Close()
	AuthRepository := &Auth.Repository{Pool: Pool}
	SchemaError := AuthRepository.CheckSchema(Startup)
	Cancel()
	if SchemaError != nil {
		return SchemaError
	}
	Authentication, Err := Auth.NewService(AuthRepository, Settings)
	if Err != nil {
		return Err
	}
	AuthRoutes := &Auth.Handler{Service: Authentication}
	ConnCipher, Err := Connection.NewCipher(Settings.EncryptionKey)
	if Err != nil {
		return Err
	}
	ConnectionService := &Connection.Service{Cipher: ConnCipher, Repo: &Connection.Repository{Pool: Pool}}
	SeedCtx, SeedCancel := Context.WithTimeout(Ctx, 5*Time.Second)
	SeedErr := Connection.SeedDefault(SeedCtx, ConnectionService, Os.Getenv("TARGET_DATABASE_URL"))
	SeedCancel()
	if SeedErr != nil {
		Slog.Warn("default target connection was not seeded", "error", SeedErr)
	}
	ConnRoutes := &Connection.Handler{Auth: AuthRoutes, Service: ConnectionService, Driver: Driver.PostgresDriver{}}
	AccessRepo := &Access.Repository{Pool: Pool}
	InspectService := &Inspect.Service{Connections: ConnectionService, Driver: Driver.PostgresDriver{}}
	InspectRoutes := &Inspect.Handler{Auth: AuthRoutes, Policy: AccessRepo, Service: InspectService}
	AccessRoutes := &Access.Handler{Auth: AuthRoutes, Repo: AccessRepo}
	AdminRoutes := &Admin.Handler{Auth: AuthRoutes, Service: Admin.NewService(&Admin.Repository{Pool: Pool})}
	MigrationService := &Migration.Service{
		Repo:   &Migration.Repository{Pool: Pool},
		Access: AccessRepo,
		Resolver: Migration.DSNResolverFunc(func(Ctx Context.Context, DatabaseID string) (string, error) {
			_, DSN, Err := ConnectionService.DialDSN(Ctx, DatabaseID)
			return DSN, Err
		}),
		Driver: Driver.PostgresDriver{},
	}
	MigrationRoutes := &Migration.Handler{Auth: AuthRoutes, Service: MigrationService}
	Server := &Http.Server{
		Addr: Cfg.HTTPAddr,
		Handler: Httpx.NewRouter(
			AuthRepository.CheckSchema,
			Webui.Handler(),
			AuthRoutes.Register,
			ConnRoutes.Register,
			InspectRoutes.Register,
			AccessRoutes.Register,
			AdminRoutes.Register,
			MigrationRoutes.Register,
		),
		ReadHeaderTimeout: 5 * Time.Second,
		ReadTimeout:       15 * Time.Second,
		WriteTimeout:      30 * Time.Second,
		IdleTimeout:       60 * Time.Second,
	}
	Done := make(chan error, 1)
	go func() { Done <- Server.ListenAndServe() }()
	Slog.Info("server listening", "address", Cfg.HTTPAddr)
	select {
	case Err := <-Done:
		if Errors.Is(Err, Http.ErrServerClosed) {
			return nil
		}
		return Errors.New("HTTP server failed to listen")
	case <-Ctx.Done():
		Shutdown, Cancel := Context.WithTimeout(Context.Background(), 10*Time.Second)
		defer Cancel()
		if Err := Server.Shutdown(Shutdown); Err != nil {
			_ = Server.Close()
			return Errors.New("HTTP server shutdown timed out")
		}
		return nil
	}
}
