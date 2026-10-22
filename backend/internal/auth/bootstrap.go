package Auth

import (
	Context "context"
	Errors "errors"
	Strings "strings"
)

func BootstrapAdmin(Ctx Context.Context, Repo *Repository, Username, Password string) (Principal, error) {
	Username = Strings.TrimSpace(Username)
	if !UsernamePattern.MatchString(Username) {
		return Principal{}, Errors.New("username must contain 3 to 64 letters, digits, dots, underscores or hyphens")
	}
	Hash, Err := HashPassword(Password)
	if Err != nil {
		return Principal{}, Err
	}
	Tx, Err := Repo.Pool.Begin(Ctx)
	if Err != nil {
		return Principal{}, ErrUnavailable
	}
	defer Tx.Rollback(Ctx)
	if _, Err := Tx.Exec(Ctx, "SELECT pg_advisory_xact_lock(hashtext('platform.bootstrap_admin'))"); Err != nil {
		return Principal{}, ErrUnavailable
	}
	var Existing User
	Existing, Err = ReadUser(Tx.QueryRow(Ctx, "SELECT "+UserColumns+" FROM users U WHERE U.is_platform_admin = TRUE OR lower(U.username) = lower($1) ORDER BY U.is_platform_admin DESC LIMIT 1 FOR UPDATE", Username))
	if Err == nil {
		if !Strings.EqualFold(Existing.Username, Username) || !Existing.IsPlatformAdmin || !Existing.IsActive {
			return Principal{}, ErrAdminExists
		}
		return Existing.Principal, nil
	}
	if !Errors.Is(Err, ErrUnauthorized) {
		return Principal{}, Err
	}
	Result, Err := ReadUser(Tx.QueryRow(Ctx, `INSERT INTO users AS U (username, password_hash, is_platform_admin) VALUES ($1,$2,TRUE) RETURNING `+UserColumns, Username, Hash))
	if Err != nil {
		return Principal{}, Err
	}
	if Err := Tx.Commit(Ctx); Err != nil {
		return Principal{}, ErrUnavailable
	}
	return Result.Principal, nil
}
