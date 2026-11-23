package main

import (
	Context "context"
	Errors "errors"
	Fmt "fmt"
	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
	Admin "github.com/ethj0r/IF3110-K02-01/backend/internal/admin"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Config "github.com/ethj0r/IF3110-K02-01/backend/internal/config"
	Connection "github.com/ethj0r/IF3110-K02-01/backend/internal/connection"
	Logging "github.com/ethj0r/IF3110-K02-01/backend/internal/logging"
	Store "github.com/ethj0r/IF3110-K02-01/backend/internal/store"
	Pgx "github.com/jackc/pgx/v5"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
	Slog "log/slog"
	Os "os"
	Strings "strings"
	Time "time"
)

const DevelopersGroup = "developers"

func main() {
	Slog.SetDefault(Logging.New(Os.Stderr, Logging.ParseLevel(Os.Getenv("LOG_LEVEL"))))
	if Err := Run(); Err != nil {
		Slog.Error("development seed failed", "error", Err)
		Os.Exit(1)
	}
}

func Run() error {
	Username := Strings.TrimSpace(Os.Getenv("BOOTSTRAP_ADMIN_USERNAME"))
	if Username == "" {
		return Errors.New("BOOTSTRAP_ADMIN_USERNAME is required")
	}
	Cfg, Err := Config.Load()
	if Err != nil {
		return Err
	}
	Settings, Err := Auth.LoadOptions()
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

	var UserID string
	var IsAdmin bool
	Err = Pool.QueryRow(Ctx, `SELECT id::text, is_platform_admin FROM users WHERE lower(username) = lower($1) AND is_active`, Username).Scan(&UserID, &IsAdmin)
	if Errors.Is(Err, Pgx.ErrNoRows) {
		return Fmt.Errorf("administrator %q was not found; run make seed-admin first", Username)
	}
	if Err != nil {
		return Errors.New("cannot read the bootstrap administrator")
	}
	if !IsAdmin {
		return Fmt.Errorf("user %q is not a platform administrator", Username)
	}

	Cipher, Err := Connection.NewCipher(Settings.EncryptionKey)
	if Err != nil {
		return Errors.New("cannot initialize connection encryption")
	}
	ConnectionService := &Connection.Service{Cipher: Cipher, Repo: &Connection.Repository{Pool: Pool}}
	if Connection.SeedTargetsEnabled(Os.Getenv("APP_ENV"), Os.Getenv("SEED_TARGET_DATABASES")) {
		if Err := Connection.SeedTargets(Ctx, ConnectionService,
			Connection.SeedTarget{Name: "local-target", URL: Os.Getenv("TARGET_DATABASE_URL")},
			Connection.SeedTarget{URL: Os.Getenv("TARGET_DATABASE_URL_2")},
			Connection.SeedTarget{URL: Os.Getenv("TARGET_DATABASE_URL_3")},
		); Err != nil {
			return Err
		}
	}

	AdminService := Admin.NewService(&Admin.Repository{Pool: Pool})
	Group, Err := EnsureGroup(Ctx, AdminService, DevelopersGroup)
	if Err != nil {
		return Err
	}
	if Err := AdminService.AddMember(Ctx, Group.ID, UserID); Err != nil {
		return Err
	}
	Granted, Err := GrantLocalConnections(Ctx, Pool, AdminService, Group.ID)
	if Err != nil {
		return Err
	}
	Fmt.Printf("Development access ready: group %q, administrator %q, %d local connection(s) with view_edit.\n", Group.Name, Username, Granted)
	return nil
}

func EnsureGroup(Ctx Context.Context, Service *Admin.Service, Name string) (Admin.Group, error) {
	Groups, Err := Service.ListGroups(Ctx)
	if Err != nil {
		return Admin.Group{}, Err
	}
	for _, Group := range Groups {
		if Strings.EqualFold(Group.Name, Name) {
			return Group, nil
		}
	}
	Created, Err := Service.CreateGroup(Ctx, Admin.CreateGroupInput{Name: Name, Description: "Local development access"})
	if Err == nil {
		return Created, nil
	}
	if !Errors.Is(Err, Admin.ErrConflict) {
		return Admin.Group{}, Err
	}
	Groups, Err = Service.ListGroups(Ctx)
	if Err != nil {
		return Admin.Group{}, Err
	}
	for _, Group := range Groups {
		if Strings.EqualFold(Group.Name, Name) {
			return Group, nil
		}
	}
	return Admin.Group{}, Errors.New("cannot resolve the development group")
}

type localConnection struct {
	ID   string
	Name string
}

func GrantLocalConnections(Ctx Context.Context, Pool *Pgxpool.Pool, Service *Admin.Service, GroupID string) (int, error) {
	Rows, Err := Pool.Query(Ctx, "SELECT id::text, name FROM db_connections WHERE lower(name) LIKE 'local-%' ORDER BY lower(name), name")
	if Err != nil {
		return 0, Errors.New("cannot list target connections")
	}
	defer Rows.Close()
	Connections := []localConnection{}
	for Rows.Next() {
		var Item localConnection
		if Err := Rows.Scan(&Item.ID, &Item.Name); Err != nil {
			return 0, Errors.New("cannot read target connections")
		}
		Connections = append(Connections, Item)
	}
	if Err := Rows.Err(); Err != nil {
		return 0, Errors.New("cannot read target connections")
	}
	Rows.Close()
	for _, Item := range Connections {
		if Err := Service.SetAccess(Ctx, GroupID, Item.ID, string(Access.LevelViewEdit)); Err != nil {
			return 0, Err
		}
	}
	return len(Connections), nil
}
