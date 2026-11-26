package main

import (
	Context "context"
	Errors "errors"
	Fmt "fmt"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Config "github.com/ethj0r/IF3110-K02-01/backend/internal/config"
	Logging "github.com/ethj0r/IF3110-K02-01/backend/internal/logging"
	Store "github.com/ethj0r/IF3110-K02-01/backend/internal/store"
	Slog "log/slog"
	Os "os"
	Time "time"
)

func main() {
	Slog.SetDefault(Logging.New(Os.Stderr, Logging.ParseLevel(Os.Getenv("LOG_LEVEL"))))
	if Err := Run(); Err != nil {
		Slog.Error("administrator bootstrap failed", "error", Err)
		Os.Exit(1)
	}
}

func Run() error {
	Username, Password := Os.Getenv("BOOTSTRAP_ADMIN_USERNAME"), Os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")
	if Username == "" || Password == "" {
		return Errors.New("BOOTSTRAP_ADMIN_USERNAME and BOOTSTRAP_ADMIN_PASSWORD are required")
	}
	Cfg, Err := Config.Load()
	if Err != nil {
		return Err
	}
	Ctx, Cancel := Context.WithTimeout(Context.Background(), 30*Time.Second)
	defer Cancel()
	Pool, Err := Store.Open(Ctx, Cfg.DatabaseURL, Logging.SQLTracer{Database: "platform"})
	if Err != nil {
		return Err
	}
	defer Pool.Close()
	if _, Err := Auth.BootstrapAdmin(Ctx, &Auth.Repository{Pool: Pool}, Username, Password); Err != nil {
		return Err
	}
	Fmt.Println("Administrator is ready.")
	return nil
}
