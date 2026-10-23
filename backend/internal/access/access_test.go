package Access

import (
	Context "context"
	Errors "errors"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Testing "testing"
)

func TestLevelAllowsAndEffectiveResolution(T *Testing.T) {
	if LevelNone.Allows(ActionView) || LevelNone.Allows(ActionEdit) {
		T.Fatal("none level must not allow actions")
	}
	if !LevelViewOnly.Allows(ActionView) || LevelViewOnly.Allows(ActionEdit) {
		T.Fatal("view_only level permissions mismatch")
	}
	if !LevelViewEdit.Allows(ActionView) || !LevelViewEdit.Allows(ActionEdit) {
		T.Fatal("view_edit level permissions mismatch")
	}
	if Level("unknown").Valid() || !LevelViewOnly.Valid() || !LevelViewEdit.Valid() {
		T.Fatal("level validity check failed")
	}
	if ResolveEffectiveLevel(LevelNone, LevelViewOnly) != LevelViewOnly {
		T.Fatal("expected view_only as effective level")
	}
	if ResolveEffectiveLevel(LevelViewOnly, LevelViewEdit, LevelNone) != LevelViewEdit {
		T.Fatal("expected highest level view_edit to win")
	}
}

func TestStaticEvaluatorAndPlatformAdminSeparation(T *Testing.T) {
	Ctx := Context.Background()
	AdminWithoutGrant := Auth.Principal{ID: "admin-1", Username: "admin", IsPlatformAdmin: true}
	Viewer := Auth.Principal{ID: "user-1", Username: "viewer", IsPlatformAdmin: false}
	Editor := Auth.Principal{ID: "user-2", Username: "editor", IsPlatformAdmin: false}

	Policy := StaticEvaluator{
		DefaultLevel: LevelNone,
		Grants: map[string]map[string]Level{
			"user-1": {"db-1": LevelViewOnly},
			"user-2": {"db-1": LevelViewEdit},
		},
	}

	if Allowed, Err := Can(Ctx, Policy, AdminWithoutGrant, ActionView, "db-1"); Err != nil || Allowed {
		T.Fatalf("platform admin must not automatically receive database access: allowed=%v err=%v", Allowed, Err)
	}
	if Allowed, Err := Can(Ctx, Policy, Viewer, ActionView, "db-1"); Err != nil || !Allowed {
		T.Fatalf("viewer must be allowed to view: allowed=%v err=%v", Allowed, Err)
	}
	if Allowed, Err := Can(Ctx, Policy, Viewer, ActionEdit, "db-1"); Err != nil || Allowed {
		T.Fatalf("viewer must not be allowed to edit: allowed=%v err=%v", Allowed, Err)
	}
	if Allowed, Err := Can(Ctx, Policy, Editor, ActionEdit, "db-1"); Err != nil || !Allowed {
		T.Fatalf("editor must be allowed to edit: allowed=%v err=%v", Allowed, Err)
	}
	if _, Err := Can(Ctx, Policy, Editor, Action("drop"), "db-1"); !Errors.Is(Err, ErrInvalidAction) {
		T.Fatalf("expected invalid action error, got %v", Err)
	}
	if Allowed, Err := Can(Ctx, Policy, Auth.Principal{}, ActionView, "db-1"); Err != nil || Allowed {
		T.Fatalf("anonymous principal must be denied: allowed=%v err=%v", Allowed, Err)
	}
	if Allowed, Err := Can(Ctx, nil, Viewer, ActionEdit, "db-1"); Err != nil || !Allowed {
		T.Fatalf("nil policy stub should default to AllowAll for Phase 0: allowed=%v err=%v", Allowed, Err)
	}
}

