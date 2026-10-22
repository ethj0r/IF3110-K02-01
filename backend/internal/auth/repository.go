package Auth

import (
	Context "context"
	Errors "errors"
	Pgx "github.com/jackc/pgx/v5"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
	Time "time"
)

type Repository struct{ Pool *Pgxpool.Pool }

func (Repo *Repository) CheckSchema(Ctx Context.Context) error {
	_, Err := Repo.Pool.Exec(Ctx, "SELECT U.id, S.user_auth_version, I.token_hash FROM users U LEFT JOIN sessions S ON S.user_id = U.id LEFT JOIN session_identifiers I ON I.session_id = S.id LIMIT 0")
	if Err != nil {
		return Errors.New("authentication schema unavailable; run make migrate-up")
	}
	return nil
}

const UserColumns = "U.id::text, U.username, U.is_platform_admin, U.password_hash, U.is_active, U.auth_version"

func ReadUser(Row Pgx.Row) (User, error) {
	var Result User
	Err := Row.Scan(&Result.ID, &Result.Username, &Result.IsPlatformAdmin, &Result.PasswordHash, &Result.IsActive, &Result.AuthVersion)
	if Errors.Is(Err, Pgx.ErrNoRows) {
		return User{}, ErrUnauthorized
	}
	if Err != nil {
		return User{}, ErrUnavailable
	}
	return Result, nil
}

func (Repo *Repository) FindUser(Ctx Context.Context, Username string) (User, error) {
	return ReadUser(Repo.Pool.QueryRow(Ctx, "SELECT "+UserColumns+" FROM users U WHERE lower(U.username) = lower($1)", Username))
}

func (Repo *Repository) CreateSession(Ctx Context.Context, Expected User, State *Session, Hash []byte) (User, error) {
	Tx, Err := Repo.Pool.Begin(Ctx)
	if Err != nil {
		return User{}, ErrUnavailable
	}
	defer Tx.Rollback(Ctx)
	Current, Err := ReadUser(Tx.QueryRow(Ctx, "SELECT "+UserColumns+" FROM users U WHERE U.id = $1 FOR UPDATE", Expected.ID))
	if Err != nil {
		return User{}, Err
	}
	if !Current.IsActive || Current.PasswordHash != Expected.PasswordHash {
		return User{}, ErrCredentials
	}
	State.UserAuthVersion = Current.AuthVersion
	State.UserIsPlatformAdmin = Current.IsPlatformAdmin
	_, Err = Tx.Exec(Ctx, `INSERT INTO sessions (id, user_id, secret_ciphertext, user_auth_version, user_is_platform_admin, created_at, last_seen_at, rotated_at, absolute_expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$6,$6,$7)`, State.ID, Current.ID, State.SecretCiphertext, Current.AuthVersion, Current.IsPlatformAdmin, State.CreatedAt, State.AbsoluteExpiresAt)
	if Err != nil {
		return User{}, ErrUnavailable
	}
	if _, Err := Tx.Exec(Ctx, "INSERT INTO session_identifiers (token_hash, session_id) VALUES ($1,$2)", Hash, State.ID); Err != nil {
		return User{}, ErrUnavailable
	}
	if Err := Tx.Commit(Ctx); Err != nil {
		return User{}, ErrUnavailable
	}
	return Current, nil
}

func (Repo *Repository) WithSession(Ctx Context.Context, Hash []byte, Visit func(User, Session) (SessionUpdate, error)) error {
	Tx, Err := Repo.Pool.Begin(Ctx)
	if Err != nil {
		return ErrUnavailable
	}
	defer Tx.Rollback(Ctx)
	Current, Err := ReadUser(Tx.QueryRow(Ctx, "SELECT "+UserColumns+` FROM users U JOIN sessions S ON S.user_id = U.id JOIN session_identifiers I ON I.session_id = S.id WHERE I.token_hash = $1 FOR UPDATE OF U`, Hash))
	if Err != nil {
		return Err
	}
	var State Session
	Err = Tx.QueryRow(Ctx, `SELECT S.id::text, S.user_id::text, S.secret_ciphertext, S.user_auth_version, S.user_is_platform_admin, S.created_at, S.last_seen_at, S.rotated_at, S.absolute_expires_at, S.revoked_at, I.valid_until
FROM sessions S JOIN session_identifiers I ON I.session_id = S.id WHERE I.token_hash = $1 FOR UPDATE OF S`, Hash).Scan(
		&State.ID, &State.UserID, &State.SecretCiphertext, &State.UserAuthVersion, &State.UserIsPlatformAdmin, &State.CreatedAt, &State.LastSeenAt, &State.RotatedAt, &State.AbsoluteExpiresAt, &State.RevokedAt, &State.IdentifierExpiresAt)
	if Errors.Is(Err, Pgx.ErrNoRows) {
		return ErrUnauthorized
	}
	if Err != nil {
		return ErrUnavailable
	}
	Update, VisitError := Visit(Current, State)
	if Update.Revoke {
		if _, Err := Tx.Exec(Ctx, "UPDATE sessions SET revoked_at = COALESCE(revoked_at, $2) WHERE id = $1", State.ID, Update.SeenAt); Err != nil {
			return ErrUnavailable
		}
		if Err := Tx.Commit(Ctx); Err != nil {
			return ErrUnavailable
		}
		return VisitError
	}
	if VisitError != nil {
		return VisitError
	}
	if Update.Rotate {
		if _, Err := Tx.Exec(Ctx, "UPDATE session_identifiers SET valid_until = $2 WHERE session_id = $1 AND valid_until IS NULL", State.ID, Update.GraceUntil); Err != nil {
			return ErrUnavailable
		}
		if _, Err := Tx.Exec(Ctx, "DELETE FROM session_identifiers WHERE session_id = $1 AND valid_until <= $2", State.ID, Update.SeenAt); Err != nil {
			return ErrUnavailable
		}
		if _, Err := Tx.Exec(Ctx, "INSERT INTO session_identifiers (token_hash, session_id) VALUES ($1,$2)", Update.IdentifierHash, State.ID); Err != nil {
			return ErrUnavailable
		}
		if _, Err := Tx.Exec(Ctx, `UPDATE sessions SET secret_ciphertext = $2, rotated_at = $3, last_seen_at = $3, user_auth_version = $4, user_is_platform_admin = $5 WHERE id = $1`, State.ID, Update.SecretCiphertext, Update.SeenAt, Update.UserAuthVersion, Update.UserIsPlatformAdmin); Err != nil {
			return ErrUnavailable
		}
	}
	if _, Err := Tx.Exec(Ctx, "UPDATE sessions SET last_seen_at = $2 WHERE id = $1", State.ID, Update.SeenAt); Err != nil {
		return ErrUnavailable
	}
	if Err := Tx.Commit(Ctx); Err != nil {
		return ErrUnavailable
	}
	return nil
}

func (Repo *Repository) RevokeIdentifier(Ctx Context.Context, Hash []byte, Now Time.Time) error {
	_, Err := Repo.Pool.Exec(Ctx, "UPDATE sessions SET revoked_at = COALESCE(revoked_at, $2) WHERE id = (SELECT session_id FROM session_identifiers WHERE token_hash = $1)", Hash, Now)
	if Err != nil {
		return ErrUnavailable
	}
	return nil
}

func (Repo *Repository) RevokeSession(Ctx Context.Context, SessionID string, Now Time.Time) error {
	_, Err := Repo.Pool.Exec(Ctx, "UPDATE sessions SET revoked_at = COALESCE(revoked_at, $2) WHERE id = $1", SessionID, Now)
	if Err != nil {
		return ErrUnavailable
	}
	return nil
}

func InvalidateUserSessions(Ctx Context.Context, Tx Pgx.Tx, UserID string, Now Time.Time) error {
	if _, Err := Tx.Exec(Ctx, "UPDATE users SET auth_version = auth_version + 1 WHERE id = $1", UserID); Err != nil {
		return ErrUnavailable
	}
	if _, Err := Tx.Exec(Ctx, "UPDATE sessions SET revoked_at = COALESCE(revoked_at, $2) WHERE user_id = $1", UserID, Now); Err != nil {
		return ErrUnavailable
	}
	return nil
}

func MarkPrivilegeChange(Ctx Context.Context, Tx Pgx.Tx, UserID string) error {
	if _, Err := Tx.Exec(Ctx, "UPDATE users SET auth_version = auth_version + 1 WHERE id = $1", UserID); Err != nil {
		return ErrUnavailable
	}
	return nil
}
