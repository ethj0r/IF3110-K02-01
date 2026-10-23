package Access

import (
	Context "context"
	Errors "errors"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Strings "strings"
)

type Action string

const (
	ActionView Action = "view"
	ActionEdit Action = "edit"
)

type Level string

const (
	LevelNone     Level = ""
	LevelViewOnly Level = "view_only"
	LevelViewEdit Level = "view_edit"
)

var (
	ErrForbidden     = Errors.New("database access denied")
	ErrInvalidAction = Errors.New("invalid access action")
	ErrInvalidLevel  = Errors.New("invalid access level")
)

func (Current Level) Valid() bool {
	return Current == LevelNone || Current == LevelViewOnly || Current == LevelViewEdit
}

func (Current Level) Allows(Act Action) bool {
	switch Act {
	case ActionView:
		return Current == LevelViewOnly || Current == LevelViewEdit
	case ActionEdit:
		return Current == LevelViewEdit
	default:
		return false
	}
}

func ResolveEffectiveLevel(Levels ...Level) Level {
	Best := LevelNone
	for _, Item := range Levels {
		switch Item {
		case LevelViewEdit:
			return LevelViewEdit
		case LevelViewOnly:
			Best = LevelViewOnly
		}
	}
	return Best
}

type Evaluator interface {
	Can(Ctx Context.Context, User Auth.Principal, Act Action, DatabaseID string) (bool, error)
	EffectiveLevel(Ctx Context.Context, User Auth.Principal, DatabaseID string) (Level, error)
}

type StaticEvaluator struct {
	DefaultLevel Level
	Grants       map[string]map[string]Level
}

func AllowAll() StaticEvaluator {
	return StaticEvaluator{DefaultLevel: LevelViewEdit}
}

func (Policy StaticEvaluator) EffectiveLevel(_ Context.Context, User Auth.Principal, DatabaseID string) (Level, error) {
	UserID := Strings.TrimSpace(User.ID)
	DatabaseID = Strings.TrimSpace(DatabaseID)
	if UserID == "" || DatabaseID == "" {
		return LevelNone, nil
	}
	if Policy.Grants != nil {
		if ByDB, Ok := Policy.Grants[UserID]; Ok {
			if Assigned, Exists := ByDB[DatabaseID]; Exists {
				return Assigned, nil
			}
		}
	}
	return Policy.DefaultLevel, nil
}

func (Policy StaticEvaluator) Can(Ctx Context.Context, User Auth.Principal, Act Action, DatabaseID string) (bool, error) {
	if Act != ActionView && Act != ActionEdit {
		return false, ErrInvalidAction
	}
	Assigned, Err := Policy.EffectiveLevel(Ctx, User, DatabaseID)
	if Err != nil {
		return false, Err
	}
	return Assigned.Allows(Act), nil
}

func Can(Ctx Context.Context, Policy Evaluator, User Auth.Principal, Act Action, DatabaseID string) (bool, error) {
	if Policy == nil {
		Policy = AllowAll()
	}
	return Policy.Can(Ctx, User, Act, DatabaseID)
}

