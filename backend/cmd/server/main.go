package main

import (
	Context "context"
	Errors "errors"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Config "github.com/ethj0r/IF3110-K02-01/backend/internal/config"
	Httpx "github.com/ethj0r/IF3110-K02-01/backend/internal/httpx"
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
	Server := &Http.Server{
		Addr:              Cfg.HTTPAddr,
		Handler:           Httpx.NewRouter(AuthRepository.CheckSchema, Webui.Handler(), AuthRoutes.Register),
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
