package Config

import (
	Errors "errors"
	Net "net"
	Url "net/url"
	Os "os"
)

type Config struct {
	HTTPAddr    string
	DatabaseURL string
}

func Load() (Config, error) {
	Cfg := Config{HTTPAddr: "127.0.0.1:8080", DatabaseURL: Os.Getenv("DATABASE_URL")}
	if Addr, Ok := Os.LookupEnv("HTTP_ADDR"); Ok {
		Cfg.HTTPAddr = Addr
	}
	if _, _, Err := Net.SplitHostPort(Cfg.HTTPAddr); Err != nil {
		return Config{}, Errors.New("HTTP_ADDR must be a host:port address")
	}
	if Cfg.DatabaseURL == "" {
		return Config{}, Errors.New("DATABASE_URL is required")
	}
	U, Err := Url.Parse(Cfg.DatabaseURL)
	if Err != nil || (U.Scheme != "postgres" && U.Scheme != "postgresql") || U.Host == "" || U.Path == "" || U.Path == "/" {
		return Config{}, Errors.New("DATABASE_URL must be a PostgreSQL URL with a host and database")
	}
	return Cfg, nil
}
