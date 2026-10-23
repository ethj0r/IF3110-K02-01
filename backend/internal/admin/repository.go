package Admin

import (
	Context "context"
	Errors "errors"
	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
	Pgx "github.com/jackc/pgx/v5"
	Pgconn "github.com/jackc/pgx/v5/pgconn"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ Pool *Pgxpool.Pool }

const UserColumns = "U.id::text, U.username, U.is_platform_admin, U.is_active, U.created_at"

const UserReturning = "id::text, username, is_platform_admin, is_active, created_at"

const GroupColumns = `G.id::text, G.name, G.description,
 (SELECT count(*) FROM user_groups M WHERE M.group_id = G.id),
 (SELECT count(*) FROM group_database_access A WHERE A.group_id = G.id),
 G.created_at, G.updated_at`

const GroupReturning = `id::text, name, description,
 (SELECT count(*) FROM user_groups M WHERE M.group_id = groups.id),
 (SELECT count(*) FROM group_database_access A WHERE A.group_id = groups.id),
 created_at, updated_at`

func isConflict(Err error) bool {
	var PgErr *Pgconn.PgError
	return Errors.As(Err, &PgErr) && PgErr.Code == "23505"
}

func MutationError(Err error, Missing error) error {
	if Err == nil {
		return nil
	}
	if isConflict(Err) || Errors.Is(Err, ErrConflict) {
		return ErrConflict
	}
	if Errors.Is(Err, Pgx.ErrNoRows) || Errors.Is(Err, ErrNotFound) {
		return Missing
	}
	return ErrUnavailable
}

func ReadUser(Row Pgx.Row) (User, error) {
	var Result User
	Err := Row.Scan(&Result.ID, &Result.Username, &Result.IsPlatformAdmin, &Result.IsActive, &Result.CreatedAt)
	if Errors.Is(Err, Pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if isConflict(Err) {
		return User{}, ErrConflict
	}
	if Err != nil {
		return User{}, ErrUnavailable
	}
	Result.Groups = []GroupRef{}
	return Result, nil
}

func ReadGroup(Row Pgx.Row) (Group, error) {
	var Result Group
	Err := Row.Scan(&Result.ID, &Result.Name, &Result.Description, &Result.MemberCount, &Result.AccessCount, &Result.CreatedAt, &Result.UpdatedAt)
	if Errors.Is(Err, Pgx.ErrNoRows) {
		return Group{}, ErrNotFound
	}
	if isConflict(Err) {
		return Group{}, ErrConflict
	}
	if Err != nil {
		return Group{}, ErrUnavailable
	}
	return Result, nil
}

func (Repo *Repository) ListUsers(Ctx Context.Context) ([]User, error) {
	Rows, Err := Repo.Pool.Query(Ctx, "SELECT "+UserColumns+" FROM users U ORDER BY lower(U.username), U.username")
	if Err != nil {
		return nil, ErrUnavailable
	}
	defer Rows.Close()
	Result := []User{}
	for Rows.Next() {
		Item, Err := ReadUser(Rows)
		if Err != nil {
			return nil, Err
		}
		Result = append(Result, Item)
	}
	if Err := Rows.Err(); Err != nil {
		return nil, ErrUnavailable
	}
	if Err := Repo.attachGroups(Ctx, Result); Err != nil {
		return nil, Err
	}
	return Result, nil
}

func (Repo *Repository) GetUser(Ctx Context.Context, ID string) (User, error) {
	Result, Err := ReadUser(Repo.Pool.QueryRow(Ctx, "SELECT "+UserColumns+" FROM users U WHERE U.id::text = $1", ID))
	if Err != nil {
		return User{}, Err
	}
	Users := []User{Result}
	if Err := Repo.attachGroups(Ctx, Users); Err != nil {
		return User{}, Err
	}
	return Users[0], nil
}

func (Repo *Repository) attachGroups(Ctx Context.Context, Users []User) error {
	if len(Users) == 0 {
		return nil
	}
	IDs := make([]string, len(Users))
	Index := make(map[string]int, len(Users))
	for Position, Item := range Users {
		IDs[Position] = Item.ID
		Index[Item.ID] = Position
	}
	Rows, Err := Repo.Pool.Query(Ctx, `SELECT UG.user_id::text, G.id::text, G.name
FROM user_groups UG JOIN groups G ON G.id = UG.group_id
WHERE UG.user_id::text = ANY($1)
ORDER BY lower(G.name), G.name`, IDs)
	if Err != nil {
		return ErrUnavailable
	}
	defer Rows.Close()
	for Rows.Next() {
		var UserID string
		var Item GroupRef
		if Err := Rows.Scan(&UserID, &Item.ID, &Item.Name); Err != nil {
			return ErrUnavailable
		}
		if Position, Ok := Index[UserID]; Ok {
			Users[Position].Groups = append(Users[Position].Groups, Item)
		}
	}
	if Err := Rows.Err(); Err != nil {
		return ErrUnavailable
	}
	return nil
}

func (Repo *Repository) ListGroups(Ctx Context.Context) ([]Group, error) {
	Rows, Err := Repo.Pool.Query(Ctx, "SELECT "+GroupColumns+" FROM groups G ORDER BY lower(G.name), G.name")
	if Err != nil {
		return nil, ErrUnavailable
	}
	defer Rows.Close()
	Result := []Group{}
	for Rows.Next() {
		Item, Err := ReadGroup(Rows)
		if Err != nil {
			return nil, Err
		}
		Result = append(Result, Item)
	}
	if Err := Rows.Err(); Err != nil {
		return nil, ErrUnavailable
	}
	return Result, nil
}

func (Repo *Repository) GetGroup(Ctx Context.Context, ID string) (Group, error) {
	return ReadGroup(Repo.Pool.QueryRow(Ctx, "SELECT "+GroupColumns+" FROM groups G WHERE G.id::text = $1", ID))
}

func (Repo *Repository) ListMembers(Ctx Context.Context, GroupID string) ([]Member, error) {
	Rows, Err := Repo.Pool.Query(Ctx, `SELECT U.id::text, U.username, U.is_platform_admin, U.is_active
FROM user_groups UG JOIN users U ON U.id = UG.user_id
WHERE UG.group_id::text = $1
ORDER BY lower(U.username), U.username`, GroupID)
	if Err != nil {
		return nil, ErrUnavailable
	}
	defer Rows.Close()
	Result := []Member{}
	for Rows.Next() {
		var Item Member
		if Err := Rows.Scan(&Item.ID, &Item.Username, &Item.IsPlatformAdmin, &Item.IsActive); Err != nil {
			return nil, ErrUnavailable
		}
		Result = append(Result, Item)
	}
	if Err := Rows.Err(); Err != nil {
		return nil, ErrUnavailable
	}
	return Result, nil
}

func (Repo *Repository) ListGroupAccess(Ctx Context.Context, GroupID string) ([]GroupAccess, error) {
	Rows, Err := Repo.Pool.Query(Ctx, `SELECT C.id::text, C.name, C.host, C.dbname, A.access_level
FROM group_database_access A JOIN db_connections C ON C.id = A.database_id
WHERE A.group_id::text = $1
ORDER BY lower(C.name), C.name`, GroupID)
	if Err != nil {
		return nil, ErrUnavailable
	}
	defer Rows.Close()
	Result := []GroupAccess{}
	for Rows.Next() {
		var Item GroupAccess
		var Raw string
		if Err := Rows.Scan(&Item.DatabaseID, &Item.DatabaseName, &Item.Host, &Item.DBName, &Raw); Err != nil {
			return nil, ErrUnavailable
		}
		Selected, Err := Access.ParseLevel(Raw)
		if Err != nil {
			return nil, ErrUnavailable
		}
		Item.Level = Selected
		Result = append(Result, Item)
	}
	if Err := Rows.Err(); Err != nil {
		return nil, ErrUnavailable
	}
	return Result, nil
}

func (Repo *Repository) ListDatabases(Ctx Context.Context) ([]DatabaseRef, error) {
	Rows, Err := Repo.Pool.Query(Ctx, "SELECT id::text, name, host, dbname FROM db_connections ORDER BY lower(name), name")
	if Err != nil {
		return nil, ErrUnavailable
	}
	defer Rows.Close()
	Result := []DatabaseRef{}
	for Rows.Next() {
		var Item DatabaseRef
		if Err := Rows.Scan(&Item.ID, &Item.Name, &Item.Host, &Item.DBName); Err != nil {
			return nil, ErrUnavailable
		}
		Result = append(Result, Item)
	}
	if Err := Rows.Err(); Err != nil {
		return nil, ErrUnavailable
	}
	return Result, nil
}

func (Repo *Repository) Matrix(Ctx Context.Context) (AccessMatrix, error) {
	Groups, Err := Repo.ListGroups(Ctx)
	if Err != nil {
		return AccessMatrix{}, Err
	}
	Databases, Err := Repo.ListDatabases(Ctx)
	if Err != nil {
		return AccessMatrix{}, Err
	}
	Rows, Err := Repo.Pool.Query(Ctx, "SELECT group_id::text, database_id::text, access_level FROM group_database_access")
	if Err != nil {
		return AccessMatrix{}, ErrUnavailable
	}
	defer Rows.Close()
	Grants := []Grant{}
	for Rows.Next() {
		var Item Grant
		var Raw string
		if Err := Rows.Scan(&Item.GroupID, &Item.DatabaseID, &Raw); Err != nil {
			return AccessMatrix{}, ErrUnavailable
		}
		Selected, Err := Access.ParseLevel(Raw)
		if Err != nil {
			return AccessMatrix{}, ErrUnavailable
		}
		Item.Level = Selected
		Grants = append(Grants, Item)
	}
	if Err := Rows.Err(); Err != nil {
		return AccessMatrix{}, ErrUnavailable
	}
	return AccessMatrix{Groups: Groups, Databases: Databases, Grants: Grants}, nil
}

func (Repo *Repository) CreateUser(Ctx Context.Context, Tx Pgx.Tx, Username, PasswordHash string) (User, error) {
	Result, Err := ReadUser(Tx.QueryRow(Ctx, `INSERT INTO users (username, password_hash) VALUES ($1,$2) RETURNING `+UserReturning, Username, PasswordHash))
	if Err := MutationError(Err, ErrUnavailable); Err != nil {
		return User{}, Err
	}
	return Result, nil
}

func (Repo *Repository) UpdateUserActive(Ctx Context.Context, Tx Pgx.Tx, ID string, Active bool) (User, error) {
	Result, Err := ReadUser(Tx.QueryRow(Ctx, `UPDATE users SET is_active = $2 WHERE id::text = $1 RETURNING `+UserReturning, ID, Active))
	if Err := MutationError(Err, ErrNotFound); Err != nil {
		return User{}, Err
	}
	return Result, nil
}

func (Repo *Repository) ResetPassword(Ctx Context.Context, Tx Pgx.Tx, ID, PasswordHash string) (User, error) {
	Result, Err := ReadUser(Tx.QueryRow(Ctx, `UPDATE users SET password_hash = $2 WHERE id::text = $1 RETURNING `+UserReturning, ID, PasswordHash))
	if Err := MutationError(Err, ErrNotFound); Err != nil {
		return User{}, Err
	}
	return Result, nil
}

func (Repo *Repository) DeleteUser(Ctx Context.Context, Tx Pgx.Tx, ID string) error {
	Result, Err := Tx.Exec(Ctx, "DELETE FROM users WHERE id::text = $1", ID)
	if Err != nil {
		return ErrUnavailable
	}
	if Result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (Repo *Repository) GetUserForUpdate(Ctx Context.Context, Tx Pgx.Tx, ID string) (User, error) {
	return ReadUser(Tx.QueryRow(Ctx, "SELECT "+UserColumns+" FROM users U WHERE U.id::text = $1 FOR UPDATE", ID))
}

func (Repo *Repository) CountActiveAdminsExcluding(Ctx Context.Context, Tx Pgx.Tx, ID string) (int, error) {
	var Count int
	Err := Tx.QueryRow(Ctx, "SELECT count(*) FROM users WHERE is_platform_admin AND is_active AND id::text <> $1", ID).Scan(&Count)
	if Err != nil {
		return 0, ErrUnavailable
	}
	return Count, nil
}

func (Repo *Repository) CreateGroup(Ctx Context.Context, Tx Pgx.Tx, Name, Description string) (Group, error) {
	Result, Err := ReadGroup(Tx.QueryRow(Ctx, `INSERT INTO groups (name, description) VALUES ($1,$2) RETURNING `+GroupReturning, Name, Description))
	if Err := MutationError(Err, ErrUnavailable); Err != nil {
		return Group{}, Err
	}
	return Result, nil
}

func (Repo *Repository) UpdateGroup(Ctx Context.Context, Tx Pgx.Tx, ID, Name, Description string) (Group, error) {
	Result, Err := ReadGroup(Tx.QueryRow(Ctx, `UPDATE groups SET name = $2, description = $3, updated_at = CURRENT_TIMESTAMP WHERE id::text = $1 RETURNING `+GroupReturning, ID, Name, Description))
	if Err := MutationError(Err, ErrNotFound); Err != nil {
		return Group{}, Err
	}
	return Result, nil
}

func (Repo *Repository) DeleteGroup(Ctx Context.Context, Tx Pgx.Tx, ID string) error {
	Result, Err := Tx.Exec(Ctx, "DELETE FROM groups WHERE id::text = $1", ID)
	if Err != nil {
		return ErrUnavailable
	}
	if Result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (Repo *Repository) UserExists(Ctx Context.Context, Tx Pgx.Tx, ID string) (bool, error) {
	var Found bool
	if Err := Tx.QueryRow(Ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE id::text = $1)", ID).Scan(&Found); Err != nil {
		return false, ErrUnavailable
	}
	return Found, nil
}

func (Repo *Repository) GroupExists(Ctx Context.Context, Tx Pgx.Tx, ID string) (bool, error) {
	var Found bool
	if Err := Tx.QueryRow(Ctx, "SELECT EXISTS(SELECT 1 FROM groups WHERE id::text = $1)", ID).Scan(&Found); Err != nil {
		return false, ErrUnavailable
	}
	return Found, nil
}

func (Repo *Repository) ConnectionExists(Ctx Context.Context, Tx Pgx.Tx, ID string) (bool, error) {
	var Found bool
	if Err := Tx.QueryRow(Ctx, "SELECT EXISTS(SELECT 1 FROM db_connections WHERE id::text = $1)", ID).Scan(&Found); Err != nil {
		return false, ErrUnavailable
	}
	return Found, nil
}

func (Repo *Repository) AddMember(Ctx Context.Context, Tx Pgx.Tx, GroupID, UserID string) (bool, error) {
	Result, Err := Tx.Exec(Ctx, `INSERT INTO user_groups (user_id, group_id) VALUES ($1::uuid, $2::uuid) ON CONFLICT DO NOTHING`, UserID, GroupID)
	if Err != nil {
		return false, ErrUnavailable
	}
	return Result.RowsAffected() > 0, nil
}

func (Repo *Repository) RemoveMember(Ctx Context.Context, Tx Pgx.Tx, GroupID, UserID string) (bool, error) {
	Result, Err := Tx.Exec(Ctx, "DELETE FROM user_groups WHERE group_id::text = $1 AND user_id::text = $2", GroupID, UserID)
	if Err != nil {
		return false, ErrUnavailable
	}
	return Result.RowsAffected() > 0, nil
}

func (Repo *Repository) GroupMemberIDs(Ctx Context.Context, Tx Pgx.Tx, GroupID string) ([]string, error) {
	Rows, Err := Tx.Query(Ctx, "SELECT user_id::text FROM user_groups WHERE group_id::text = $1", GroupID)
	if Err != nil {
		return nil, ErrUnavailable
	}
	defer Rows.Close()
	Result := []string{}
	for Rows.Next() {
		var ID string
		if Err := Rows.Scan(&ID); Err != nil {
			return nil, ErrUnavailable
		}
		Result = append(Result, ID)
	}
	if Err := Rows.Err(); Err != nil {
		return nil, ErrUnavailable
	}
	return Result, nil
}

func (Repo *Repository) AccessLevel(Ctx Context.Context, Tx Pgx.Tx, GroupID, DatabaseID string) (Access.Level, error) {
	var Raw string
	Err := Tx.QueryRow(Ctx, "SELECT access_level FROM group_database_access WHERE group_id::text = $1 AND database_id::text = $2", GroupID, DatabaseID).Scan(&Raw)
	if Errors.Is(Err, Pgx.ErrNoRows) {
		return Access.LevelNone, nil
	}
	if Err != nil {
		return Access.LevelNone, ErrUnavailable
	}
	Selected, Err := Access.ParseLevel(Raw)
	if Err != nil {
		return Access.LevelNone, ErrUnavailable
	}
	return Selected, nil
}

func (Repo *Repository) SetAccess(Ctx Context.Context, Tx Pgx.Tx, GroupID, DatabaseID string, Selected Access.Level) error {
	if !Selected.Valid() || Selected == Access.LevelNone {
		return Access.ErrInvalidLevel
	}
	_, Err := Tx.Exec(Ctx, `INSERT INTO group_database_access (group_id, database_id, access_level)
VALUES ($1::uuid, $2::uuid, $3)
ON CONFLICT (group_id, database_id) DO UPDATE SET access_level = EXCLUDED.access_level, updated_at = CURRENT_TIMESTAMP`, GroupID, DatabaseID, string(Selected))
	if Err != nil {
		return ErrUnavailable
	}
	return nil
}

func (Repo *Repository) DeleteAccess(Ctx Context.Context, Tx Pgx.Tx, GroupID, DatabaseID string) (bool, error) {
	Result, Err := Tx.Exec(Ctx, "DELETE FROM group_database_access WHERE group_id::text = $1 AND database_id::text = $2", GroupID, DatabaseID)
	if Err != nil {
		return false, ErrUnavailable
	}
	return Result.RowsAffected() > 0, nil
}
