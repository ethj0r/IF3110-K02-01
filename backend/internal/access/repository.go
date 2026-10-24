package Access

import (
	Context "context"
	Errors "errors"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Pgx "github.com/jackc/pgx/v5"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
	Strings "strings"
)

type Repository struct{ Pool *Pgxpool.Pool }

const AccessibleQuery = `SELECT C.id::text, C.name, C.host, C.dbname,
       MAX(CASE A.access_level WHEN 'view_edit' THEN 2 WHEN 'view_only' THEN 1 ELSE 0 END) AS access_rank
FROM db_connections C
JOIN group_database_access A ON A.database_id = C.id
JOIN user_groups UG ON UG.group_id = A.group_id
WHERE UG.user_id::text = $1`

func (Repo *Repository) EffectiveLevel(Ctx Context.Context, User Auth.Principal, DatabaseID string) (Level, error) {
	UserID := Strings.TrimSpace(User.ID)
	DatabaseID = Strings.TrimSpace(DatabaseID)
	if UserID == "" || DatabaseID == "" {
		return LevelNone, nil
	}
	Rows, Err := Repo.Pool.Query(Ctx, `SELECT A.access_level
FROM group_database_access A
JOIN user_groups UG ON UG.group_id = A.group_id
WHERE UG.user_id::text = $1 AND A.database_id::text = $2`, UserID, DatabaseID)
	if Err != nil {
		return LevelNone, ErrUnavailable
	}
	defer Rows.Close()
	Levels := []Level{}
	for Rows.Next() {
		var Raw string
		if Err := Rows.Scan(&Raw); Err != nil {
			return LevelNone, ErrUnavailable
		}
		Selected, Err := ParseLevel(Raw)
		if Err != nil {
			return LevelNone, ErrUnavailable
		}
		Levels = append(Levels, Selected)
	}
	if Err := Rows.Err(); Err != nil {
		return LevelNone, ErrUnavailable
	}
	return ResolveEffectiveLevel(Levels...), nil
}

func (Repo *Repository) Can(Ctx Context.Context, User Auth.Principal, Act Action, DatabaseID string) (bool, error) {
	if Act != ActionView && Act != ActionEdit {
		return false, ErrInvalidAction
	}
	Selected, Err := Repo.EffectiveLevel(Ctx, User, DatabaseID)
	if Err != nil {
		return false, Err
	}
	return Selected.Allows(Act), nil
}

func (Repo *Repository) ListAccessible(Ctx Context.Context, User Auth.Principal) ([]DatabaseAccess, error) {
	UserID := Strings.TrimSpace(User.ID)
	if UserID == "" {
		return []DatabaseAccess{}, nil
	}
	Rows, Err := Repo.Pool.Query(Ctx, AccessibleQuery+" GROUP BY C.id, C.name, C.host, C.dbname ORDER BY lower(C.name), C.name", UserID)
	if Err != nil {
		return nil, ErrUnavailable
	}
	defer Rows.Close()
	Result := []DatabaseAccess{}
	for Rows.Next() {
		var Item DatabaseAccess
		var Rank int
		if Err := Rows.Scan(&Item.ID, &Item.Name, &Item.Host, &Item.DBName, &Rank); Err != nil {
			return nil, ErrUnavailable
		}
		Item.Level = FromRank(Rank)
		Result = append(Result, Item)
	}
	if Err := Rows.Err(); Err != nil {
		return nil, ErrUnavailable
	}
	return Result, nil
}

func (Repo *Repository) GetAccessible(Ctx Context.Context, User Auth.Principal, DatabaseID string) (DatabaseAccess, error) {
	UserID := Strings.TrimSpace(User.ID)
	DatabaseID = Strings.TrimSpace(DatabaseID)
	if UserID == "" || DatabaseID == "" {
		return DatabaseAccess{}, ErrNotFound
	}
	var Item DatabaseAccess
	var Rank int
	Err := Repo.Pool.QueryRow(Ctx, AccessibleQuery+" AND C.id::text = $2 GROUP BY C.id, C.name, C.host, C.dbname", UserID, DatabaseID).
		Scan(&Item.ID, &Item.Name, &Item.Host, &Item.DBName, &Rank)
	if Errors.Is(Err, Pgx.ErrNoRows) {
		return DatabaseAccess{}, ErrNotFound
	}
	if Err != nil {
		return DatabaseAccess{}, ErrUnavailable
	}
	Item.Level = FromRank(Rank)
	return Item, nil
}
