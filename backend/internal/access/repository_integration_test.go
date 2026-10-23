//go:build integration

package Access

import (
	Context "context"
	Errors "errors"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Testdb "github.com/ethj0r/IF3110-K02-01/backend/internal/testdb"
	Testutil "github.com/ethj0r/IF3110-K02-01/backend/internal/testutil"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
	Testing "testing"
)

func CreateGroup(T *Testing.T, Pool *Pgxpool.Pool, Name string) string {
	T.Helper()
	var ID string
	Err := Pool.QueryRow(Context.Background(), "INSERT INTO groups (name) VALUES ($1) RETURNING id::text", Name).Scan(&ID)
	if Err != nil {
		T.Fatal(Err)
	}
	return ID
}

func Grant(T *Testing.T, Pool *Pgxpool.Pool, GroupID, DatabaseID, Level string) {
	T.Helper()
	_, Err := Pool.Exec(Context.Background(), "INSERT INTO group_database_access (group_id, database_id, access_level) VALUES ($1, $2, $3)", GroupID, DatabaseID, Level)
	if Err != nil {
		T.Fatal(Err)
	}
}

func Join(T *Testing.T, Pool *Pgxpool.Pool, GroupID, UserID string) {
	T.Helper()
	_, Err := Pool.Exec(Context.Background(), "INSERT INTO user_groups (user_id, group_id) VALUES ($1, $2)", UserID, GroupID)
	if Err != nil {
		T.Fatal(Err)
	}
}

func TestRepositoryResolvesHighestAccessLevel(T *Testing.T) {
	Pool := Testdb.PlatformDatabase(T)
	Repo := &Repository{Pool: Pool}
	Ctx := Context.Background()

	AdminID := Testutil.CreateUser(T, Pool, "platform-admin", true)
	ViewerID := Testutil.CreateUser(T, Pool, "viewer-user", false)
	EditorID := Testutil.CreateUser(T, Pool, "editor-user", false)
	ReaderID := Testutil.CreateUser(T, Pool, "reader-user", false)
	DatabaseID := Testutil.CreateConnection(T, Pool, "reporting")
	SecondID := Testutil.CreateConnection(T, Pool, "analytics")
	Readers := CreateGroup(T, Pool, "Readers")
	Editors := CreateGroup(T, Pool, "Editors")

	Join(T, Pool, Readers, ViewerID)
	Join(T, Pool, Editors, ViewerID)
	Join(T, Pool, Readers, ReaderID)
	Grant(T, Pool, Readers, DatabaseID, "view_only")
	Grant(T, Pool, Editors, DatabaseID, "view_edit")
	Grant(T, Pool, Editors, SecondID, "view_only")

	Admin := Auth.Principal{ID: AdminID, Username: "platform-admin", IsPlatformAdmin: true}
	Viewer := Auth.Principal{ID: ViewerID, Username: "viewer-user"}
	Editor := Auth.Principal{ID: EditorID, Username: "editor-user"}
	Reader := Auth.Principal{ID: ReaderID, Username: "reader-user"}

	if Selected, Err := Repo.EffectiveLevel(Ctx, Viewer, DatabaseID); Err != nil || Selected != LevelViewEdit {
		T.Fatalf("highest level did not win: level=%q err=%v", Selected, Err)
	}
	if Allowed, Err := Repo.Can(Ctx, Viewer, ActionEdit, DatabaseID); Err != nil || !Allowed {
		T.Fatalf("view_edit user was denied edit: allowed=%v err=%v", Allowed, Err)
	}
	if Allowed, Err := Repo.Can(Ctx, Reader, ActionEdit, DatabaseID); Err != nil || Allowed {
		T.Fatalf("view_only user was allowed edit: allowed=%v err=%v", Allowed, Err)
	}
	if Allowed, Err := Repo.Can(Ctx, Editor, ActionView, DatabaseID); Err != nil || Allowed {
		T.Fatalf("user without a grant was allowed: allowed=%v err=%v", Allowed, Err)
	}
	if Allowed, Err := Repo.Can(Ctx, Admin, ActionView, DatabaseID); Err != nil || Allowed {
		T.Fatalf("platform administrator bypassed database access: allowed=%v err=%v", Allowed, Err)
	}

	Accessible, Err := Repo.ListAccessible(Ctx, Viewer)
	if Err != nil || len(Accessible) != 2 {
		T.Fatalf("accessible listing mismatch: %+v err=%v", Accessible, Err)
	}
	if Accessible[0].ID != SecondID || Accessible[0].Level != LevelViewOnly {
		T.Fatalf("unexpected first accessible database: %+v", Accessible[0])
	}
	if Accessible[1].ID != DatabaseID || Accessible[1].Level != LevelViewEdit {
		T.Fatalf("unexpected second accessible database: %+v", Accessible[1])
	}

	if _, Err := Repo.GetAccessible(Ctx, Admin, DatabaseID); !Errors.Is(Err, ErrNotFound) {
		T.Fatalf("administrator without a grant could read database details: %v", Err)
	}
	if _, Err := Repo.GetAccessible(Ctx, Reader, SecondID); !Errors.Is(Err, ErrNotFound) {
		T.Fatalf("user without a grant could read database details: %v", Err)
	}

	if _, Err := Pool.Exec(Ctx, "DELETE FROM user_groups WHERE user_id = $1 AND group_id = $2", ViewerID, Editors); Err != nil {
		T.Fatal(Err)
	}
	if Selected, Err := Repo.EffectiveLevel(Ctx, Viewer, DatabaseID); Err != nil || Selected != LevelViewOnly {
		T.Fatalf("effective level did not decrease after membership removal: level=%q err=%v", Selected, Err)
	}
	if Allowed, Err := Repo.Can(Ctx, Viewer, ActionEdit, DatabaseID); Err != nil || Allowed {
		T.Fatalf("membership removal did not reduce edit access: allowed=%v err=%v", Allowed, Err)
	}
	if _, Err := Repo.Can(Ctx, Viewer, Action("delete"), DatabaseID); !Errors.Is(Err, ErrInvalidAction) {
		T.Fatalf("invalid action was not rejected: %v", Err)
	}
}
