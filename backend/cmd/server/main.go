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
	Logging "github.com/ethj0r/IF3110-K02-01/backend/internal/logging"
	Migration "github.com/ethj0r/IF3110-K02-01/backend/internal/migration"
	Observability "github.com/ethj0r/IF3110-K02-01/backend/internal/observability"
	Schemadiff "github.com/ethj0r/IF3110-K02-01/backend/internal/schemadiff"
	Store "github.com/ethj0r/IF3110-K02-01/backend/internal/store"
	Webui "github.com/ethj0r/IF3110-K02-01/backend/internal/webui"
	Chi "github.com/go-chi/chi/v5"
	Slog "log/slog"
	Http "net/http"
	Os "os"
	Signal "os/signal"
	Syscall "syscall"
	Time "time"
)

func main() {
	Slog.SetDefault(Logging.New(Os.Stdout, Logging.ParseLevel(Os.Getenv("LOG_LEVEL"))))
	if Err := Run(); Err != nil {
		Slog.Error("server stopped", "event", "server.stopped", "error", Err)
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
	Pool, Err := Store.Open(Startup, Cfg.DatabaseURL, Logging.SQLTracer{Database: "platform"})
	TargetDriver := Driver.PostgresDriver{Tracer: Logging.SQLTracer{Database: "target"}}
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
	SeedCtx, SeedCancel := Context.WithTimeout(Ctx, 15*Time.Second)
	if Connection.SeedTargetsEnabled(Os.Getenv("APP_ENV"), Os.Getenv("SEED_TARGET_DATABASES")) {
		SeedErr := Connection.SeedTargets(SeedCtx, ConnectionService,
			Connection.SeedTarget{Name: "local-target", URL: Os.Getenv("TARGET_DATABASE_URL")},
			Connection.SeedTarget{URL: Os.Getenv("TARGET_DATABASE_URL_2")},
			Connection.SeedTarget{URL: Os.Getenv("TARGET_DATABASE_URL_3")},
		)
		if SeedErr != nil {
			Slog.Warn("target connections were not seeded", "event", "connection.seed_failed", "error", SeedErr)
		}
	}
	SeedCancel()
	ConnRoutes := &Connection.Handler{Auth: AuthRoutes, Service: ConnectionService, Driver: TargetDriver}
	AccessRepo := &Access.Repository{Pool: Pool}
	InspectService := &Inspect.Service{Connections: ConnectionService, Driver: TargetDriver}
	InspectRoutes := &Inspect.Handler{Auth: AuthRoutes, Policy: AccessRepo, Service: InspectService}
	AccessRoutes := &Access.Handler{Auth: AuthRoutes, Repo: AccessRepo}
	AdminRoutes := &Admin.Handler{Auth: AuthRoutes, Service: Admin.NewService(&Admin.Repository{Pool: Pool})}
	MigrationRepo := Migration.NewReviewedRepository(Pool)
	MigrationService := &Migration.Service{
		Repo:    MigrationRepo,
		Reviews: MigrationRepo,
		Access:  AccessRepo,
		Resolver: Migration.DSNResolverFunc(func(Ctx Context.Context, DatabaseID string) (string, error) {
			_, DSN, Err := ConnectionService.DialDSN(Ctx, DatabaseID)
			return DSN, Err
		}),
		Driver: TargetDriver,
	}
	MigrationRoutes := &Migration.Handler{Auth: AuthRoutes, Service: MigrationService}
	SchemaDiffRoutes := &Schemadiff.Handler{Auth: AuthRoutes, Service: &Schemadiff.Service{Policy: AccessRepo, Connections: ConnectionService, Generator: Schemadiff.PostgresGenerator{Tracer: Logging.SQLTracer{Database: "target"}}}}
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
			SchemaDiffRoutes.Register,
			func(Api Chi.Router) {
				if Os.Getenv("METRICS_ENABLED") == "true" {
					Api.Get("/metrics", Observability.MetricsHandler{Pool: Pool}.ServeHTTP)
				}
			},
		),
		ReadHeaderTimeout: 5 * Time.Second,
		ReadTimeout:       15 * Time.Second,
		WriteTimeout:      30 * Time.Second,
		IdleTimeout:       60 * Time.Second,
	}
	Done := make(chan error, 1)
	go func() { Done <- Server.ListenAndServe() }()
	Slog.Info("server listening", "event", "server.listening", "address", Cfg.HTTPAddr)
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
