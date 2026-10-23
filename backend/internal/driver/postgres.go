package Driver

import (
	Context "context"
	Errors "errors"
	Pgx "github.com/jackc/pgx/v5"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
	Net "net"
	Neturl "net/url"
	Strconv "strconv"
	Strings "strings"
	Time "time"
)

type Conn interface {
	Ping(Ctx Context.Context) error
	Query(Ctx Context.Context, SQL string, Args ...any) (Pgx.Rows, error)
	Exec(Ctx Context.Context, SQL string, Args ...any) error
	Close()
}

type Driver interface {
	Open(DSN string) (Conn, error)
}

type PostgresDriver struct{}

type PostgresConn struct{ Pool *Pgxpool.Pool }

func BuildDSN(Host string, Port int, DBName, Username, Password string) (string, error) {
	Host = Strings.TrimSpace(Host)
	DBName = Strings.TrimSpace(DBName)
	Username = Strings.TrimSpace(Username)
	if Host == "" || DBName == "" || Username == "" || Port < 1 || Port > 65535 || len(DBName) > 63 || len(Username) > 63 {
		return "", ErrInvalidInput
	}
	Value := Neturl.URL{Scheme: "postgres", User: Neturl.UserPassword(Username, Password), Host: Net.JoinHostPort(Host, Strconv.Itoa(Port)), Path: "/" + DBName, RawQuery: "sslmode=disable"}
	return Value.String(), nil
}

func openPool(DSN string) (*Pgxpool.Pool, error) {
	Cfg, Err := Pgxpool.ParseConfig(DSN)
	if Err != nil {
		return nil, ErrInvalidDSN
	}
	Cfg.ConnConfig.ConnectTimeout = 5 * Time.Second
	Ctx, Cancel := Context.WithTimeout(Context.Background(), 5*Time.Second)
	defer Cancel()
	Pool, Err := Pgxpool.NewWithConfig(Ctx, Cfg)
	if Err != nil {
		return nil, ErrUnavailable
	}
	if Err := Pool.Ping(Ctx); Err != nil {
		Pool.Close()
		return nil, ErrUnavailable
	}
	return Pool, nil
}

func (PostgresDriver) Open(DSN string) (Conn, error) {
	Pool, Err := openPool(DSN)
	if Err != nil {
		if Errors.Is(Err, ErrInvalidDSN) {
			return nil, ErrInvalidDSN
		}
		return nil, ErrUnavailable
	}
	return &PostgresConn{Pool: Pool}, nil
}

func (Target *PostgresConn) Ping(Ctx Context.Context) error {
	Ctx, Cancel := Context.WithTimeout(Ctx, 5*Time.Second)
	defer Cancel()
	if Err := Target.Pool.Ping(Ctx); Err != nil {
		return ErrUnavailable
	}
	return nil
}

func (Target *PostgresConn) Query(Ctx Context.Context, SQL string, Args ...any) (Pgx.Rows, error) {
	Rows, Err := Target.Pool.Query(Ctx, SQL, Args...)
	if Err != nil {
		return nil, ErrUnavailable
	}
	return Rows, nil
}

func (Target *PostgresConn) Exec(Ctx Context.Context, SQL string, Args ...any) error {
	if _, Err := Target.Pool.Exec(Ctx, SQL, Args...); Err != nil {
		return ErrUnavailable
	}
	return nil
}

func (Target *PostgresConn) Close() {
	Target.Pool.Close()
}
