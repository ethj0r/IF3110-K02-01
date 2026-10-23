package Admin

import (
	Context "context"
	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Pgx "github.com/jackc/pgx/v5"
	Strings "strings"
	Time "time"
)

type Service struct {
	Repo *Repository
	Now  func() Time.Time
}

func NewService(Repo *Repository) *Service {
	return &Service{Repo: Repo, Now: Time.Now}
}

func (Server *Service) clock() Time.Time {
	if Server.Now == nil {
		return Time.Now().UTC()
	}
	return Server.Now().UTC()
}

func (Server *Service) ListUsers(Ctx Context.Context) ([]User, error) {
	return Server.Repo.ListUsers(Ctx)
}

func (Server *Service) GetUser(Ctx Context.Context, ID string) (User, error) {
	if Strings.TrimSpace(ID) == "" {
		return User{}, ErrNotFound
	}
	return Server.Repo.GetUser(Ctx, ID)
}

func (Server *Service) CreateUser(Ctx Context.Context, Input CreateUserInput) (User, error) {
	Input = Input.Normalized()
	if Err := Input.Validate(); Err != nil {
		return User{}, Err
	}
	Hash, Err := Auth.HashPassword(Input.Password)
	if Err != nil {
		return User{}, ErrUnavailable
	}
	Tx, Err := Server.Repo.Pool.Begin(Ctx)
	if Err != nil {
		return User{}, ErrUnavailable
	}
	defer Tx.Rollback(Ctx)
	Result, Err := Server.Repo.CreateUser(Ctx, Tx, Input.Username, Hash)
	if Err != nil {
		return User{}, Err
	}
	if Err := Tx.Commit(Ctx); Err != nil {
		return User{}, ErrUnavailable
	}
	return Result, nil
}

func (Server *Service) UpdateUserActive(Ctx Context.Context, ID string, Active bool) (User, error) {
	if Strings.TrimSpace(ID) == "" {
		return User{}, ErrNotFound
	}
	Tx, Err := Server.Repo.Pool.Begin(Ctx)
	if Err != nil {
		return User{}, ErrUnavailable
	}
	defer Tx.Rollback(Ctx)
	if !Active {
		if Err := Server.GuardAdmin(Ctx, Tx, ID); Err != nil {
			return User{}, Err
		}
	}
	Result, Err := Server.Repo.UpdateUserActive(Ctx, Tx, ID, Active)
	if Err != nil {
		return User{}, Err
	}
	if Err := Tx.Commit(Ctx); Err != nil {
		return User{}, ErrUnavailable
	}
	return Result, nil
}

func (Server *Service) ResetPassword(Ctx Context.Context, ID string, Input PasswordInput) (User, error) {
	if Strings.TrimSpace(ID) == "" {
		return User{}, ErrNotFound
	}
	if Err := Input.Validate(); Err != nil {
		return User{}, Err
	}
	Hash, Err := Auth.HashPassword(Input.Password)
	if Err != nil {
		return User{}, ErrUnavailable
	}
	Tx, Err := Server.Repo.Pool.Begin(Ctx)
	if Err != nil {
		return User{}, ErrUnavailable
	}
	defer Tx.Rollback(Ctx)
	Result, Err := Server.Repo.ResetPassword(Ctx, Tx, ID, Hash)
	if Err != nil {
		return User{}, Err
	}
	if Err := Tx.Commit(Ctx); Err != nil {
		return User{}, ErrUnavailable
	}
	return Result, nil
}

func (Server *Service) DeleteUser(Ctx Context.Context, ID string) error {
	if Strings.TrimSpace(ID) == "" {
		return ErrNotFound
	}
	Tx, Err := Server.Repo.Pool.Begin(Ctx)
	if Err != nil {
		return ErrUnavailable
	}
	defer Tx.Rollback(Ctx)
	if Err := Server.GuardAdmin(Ctx, Tx, ID); Err != nil {
		return Err
	}
	if Err := Server.Repo.DeleteUser(Ctx, Tx, ID); Err != nil {
		return Err
	}
	if Err := Tx.Commit(Ctx); Err != nil {
		return ErrUnavailable
	}
	return nil
}

func (Server *Service) GuardAdmin(Ctx Context.Context, Tx Pgx.Tx, ID string) error {
	if _, Err := Tx.Exec(Ctx, "SELECT pg_advisory_xact_lock(hashtext('platform.admin_accounts'))"); Err != nil {
		return ErrUnavailable
	}
	Target, Err := Server.Repo.GetUserForUpdate(Ctx, Tx, ID)
	if Err != nil {
		return Err
	}
	if !Target.IsPlatformAdmin || !Target.IsActive {
		return nil
	}
	Others, Err := Server.Repo.CountActiveAdminsExcluding(Ctx, Tx, ID)
	if Err != nil {
		return Err
	}
	if Others == 0 {
		return ErrLastAdmin
	}
	return nil
}

func (Server *Service) ListGroups(Ctx Context.Context) ([]Group, error) {
	return Server.Repo.ListGroups(Ctx)
}

func (Server *Service) GetGroup(Ctx Context.Context, ID string) (Group, error) {
	if Strings.TrimSpace(ID) == "" {
		return Group{}, ErrNotFound
	}
	return Server.Repo.GetGroup(Ctx, ID)
}

func (Server *Service) CreateGroup(Ctx Context.Context, Input CreateGroupInput) (Group, error) {
	Input = Input.Normalized()
	if Err := Input.Validate(); Err != nil {
		return Group{}, Err
	}
	Tx, Err := Server.Repo.Pool.Begin(Ctx)
	if Err != nil {
		return Group{}, ErrUnavailable
	}
	defer Tx.Rollback(Ctx)
	Result, Err := Server.Repo.CreateGroup(Ctx, Tx, Input.Name, Input.Description)
	if Err != nil {
		return Group{}, Err
	}
	if Err := Tx.Commit(Ctx); Err != nil {
		return Group{}, ErrUnavailable
	}
	return Result, nil
}

func (Server *Service) UpdateGroup(Ctx Context.Context, ID string, Input UpdateGroupInput) (Group, error) {
	if Strings.TrimSpace(ID) == "" {
		return Group{}, ErrNotFound
	}
	if Err := Input.Validate(); Err != nil {
		return Group{}, Err
	}
	Current, Err := Server.Repo.GetGroup(Ctx, ID)
	if Err != nil {
		return Group{}, Err
	}
	Name := Current.Name
	if Input.Name != nil {
		Name = Strings.TrimSpace(*Input.Name)
	}
	Description := Current.Description
	if Input.Description != nil {
		Description = Strings.TrimSpace(*Input.Description)
	}
	Tx, Err := Server.Repo.Pool.Begin(Ctx)
	if Err != nil {
		return Group{}, ErrUnavailable
	}
	defer Tx.Rollback(Ctx)
	Result, Err := Server.Repo.UpdateGroup(Ctx, Tx, ID, Name, Description)
	if Err != nil {
		return Group{}, Err
	}
	if Err := Tx.Commit(Ctx); Err != nil {
		return Group{}, ErrUnavailable
	}
	return Result, nil
}

func (Server *Service) DeleteGroup(Ctx Context.Context, ID string) error {
	if Strings.TrimSpace(ID) == "" {
		return ErrNotFound
	}
	Tx, Err := Server.Repo.Pool.Begin(Ctx)
	if Err != nil {
		return ErrUnavailable
	}
	defer Tx.Rollback(Ctx)
	Members, Err := Server.Repo.GroupMemberIDs(Ctx, Tx, ID)
	if Err != nil {
		return Err
	}
	if Err := Server.Repo.DeleteGroup(Ctx, Tx, ID); Err != nil {
		return Err
	}
	if Err := Server.InvalidateMembers(Ctx, Tx, Members); Err != nil {
		return Err
	}
	if Err := Tx.Commit(Ctx); Err != nil {
		return ErrUnavailable
	}
	return nil
}

func (Server *Service) ListMembers(Ctx Context.Context, GroupID string) ([]Member, error) {
	GroupID = Strings.TrimSpace(GroupID)
	if GroupID == "" {
		return nil, ErrNotFound
	}
	if _, Err := Server.Repo.GetGroup(Ctx, GroupID); Err != nil {
		return nil, Err
	}
	return Server.Repo.ListMembers(Ctx, GroupID)
}

func (Server *Service) AddMember(Ctx Context.Context, GroupID, UserID string) error {
	GroupID, UserID = Strings.TrimSpace(GroupID), Strings.TrimSpace(UserID)
	if GroupID == "" || UserID == "" {
		return ErrNotFound
	}
	Tx, Err := Server.Repo.Pool.Begin(Ctx)
	if Err != nil {
		return ErrUnavailable
	}
	defer Tx.Rollback(Ctx)
	if Found, Err := Server.Repo.GroupExists(Ctx, Tx, GroupID); Err != nil || !Found {
		if Err != nil {
			return Err
		}
		return ErrNotFound
	}
	if Found, Err := Server.Repo.UserExists(Ctx, Tx, UserID); Err != nil || !Found {
		if Err != nil {
			return Err
		}
		return ErrNotFound
	}
	Inserted, Err := Server.Repo.AddMember(Ctx, Tx, GroupID, UserID)
	if Err != nil {
		return Err
	}
	if Inserted {
		if Err := Auth.MarkPrivilegeChange(Ctx, Tx, UserID); Err != nil {
			return ErrUnavailable
		}
	}
	if Err := Tx.Commit(Ctx); Err != nil {
		return ErrUnavailable
	}
	return nil
}

func (Server *Service) RemoveMember(Ctx Context.Context, GroupID, UserID string) error {
	GroupID, UserID = Strings.TrimSpace(GroupID), Strings.TrimSpace(UserID)
	if GroupID == "" || UserID == "" {
		return ErrNotFound
	}
	Tx, Err := Server.Repo.Pool.Begin(Ctx)
	if Err != nil {
		return ErrUnavailable
	}
	defer Tx.Rollback(Ctx)
	Removed, Err := Server.Repo.RemoveMember(Ctx, Tx, GroupID, UserID)
	if Err != nil {
		return Err
	}
	if !Removed {
		return ErrNotFound
	}
	if Err := Auth.InvalidateUserSessions(Ctx, Tx, UserID, Server.clock()); Err != nil {
		return ErrUnavailable
	}
	if Err := Tx.Commit(Ctx); Err != nil {
		return ErrUnavailable
	}
	return nil
}

func (Server *Service) ListGroupAccess(Ctx Context.Context, GroupID string) ([]GroupAccess, error) {
	GroupID = Strings.TrimSpace(GroupID)
	if GroupID == "" {
		return nil, ErrNotFound
	}
	if _, Err := Server.Repo.GetGroup(Ctx, GroupID); Err != nil {
		return nil, Err
	}
	return Server.Repo.ListGroupAccess(Ctx, GroupID)
}

func (Server *Service) SetAccess(Ctx Context.Context, GroupID, DatabaseID, RawLevel string) error {
	GroupID, DatabaseID = Strings.TrimSpace(GroupID), Strings.TrimSpace(DatabaseID)
	Selected, Err := Access.ParseLevel(RawLevel)
	if Err != nil {
		return ErrInvalidInput
	}
	if GroupID == "" || DatabaseID == "" {
		return ErrNotFound
	}
	Tx, Err := Server.Repo.Pool.Begin(Ctx)
	if Err != nil {
		return ErrUnavailable
	}
	defer Tx.Rollback(Ctx)
	if Found, Err := Server.Repo.GroupExists(Ctx, Tx, GroupID); Err != nil || !Found {
		if Err != nil {
			return Err
		}
		return ErrNotFound
	}
	if Found, Err := Server.Repo.ConnectionExists(Ctx, Tx, DatabaseID); Err != nil || !Found {
		if Err != nil {
			return Err
		}
		return ErrNotFound
	}
	Previous, Err := Server.Repo.AccessLevel(Ctx, Tx, GroupID, DatabaseID)
	if Err != nil {
		return Err
	}
	if Err := Server.Repo.SetAccess(Ctx, Tx, GroupID, DatabaseID, Selected); Err != nil {
		return Err
	}
	Members, Err := Server.Repo.GroupMemberIDs(Ctx, Tx, GroupID)
	if Err != nil {
		return Err
	}
	if Selected.Rank() > Previous.Rank() {
		if Err := Server.MarkMembers(Ctx, Tx, Members); Err != nil {
			return Err
		}
	}
	if Selected.Rank() < Previous.Rank() {
		if Err := Server.InvalidateMembers(Ctx, Tx, Members); Err != nil {
			return Err
		}
	}
	if Err := Tx.Commit(Ctx); Err != nil {
		return ErrUnavailable
	}
	return nil
}

func (Server *Service) RemoveAccess(Ctx Context.Context, GroupID, DatabaseID string) error {
	GroupID, DatabaseID = Strings.TrimSpace(GroupID), Strings.TrimSpace(DatabaseID)
	if GroupID == "" || DatabaseID == "" {
		return ErrNotFound
	}
	Tx, Err := Server.Repo.Pool.Begin(Ctx)
	if Err != nil {
		return ErrUnavailable
	}
	defer Tx.Rollback(Ctx)
	Removed, Err := Server.Repo.DeleteAccess(Ctx, Tx, GroupID, DatabaseID)
	if Err != nil {
		return Err
	}
	if !Removed {
		return ErrNotFound
	}
	Members, Err := Server.Repo.GroupMemberIDs(Ctx, Tx, GroupID)
	if Err != nil {
		return Err
	}
	if Err := Server.InvalidateMembers(Ctx, Tx, Members); Err != nil {
		return Err
	}
	if Err := Tx.Commit(Ctx); Err != nil {
		return ErrUnavailable
	}
	return nil
}

func (Server *Service) MarkMembers(Ctx Context.Context, Tx Pgx.Tx, Members []string) error {
	for _, ID := range Members {
		if Err := Auth.MarkPrivilegeChange(Ctx, Tx, ID); Err != nil {
			return ErrUnavailable
		}
	}
	return nil
}

func (Server *Service) InvalidateMembers(Ctx Context.Context, Tx Pgx.Tx, Members []string) error {
	Now := Server.clock()
	for _, ID := range Members {
		if Err := Auth.InvalidateUserSessions(Ctx, Tx, ID, Now); Err != nil {
			return ErrUnavailable
		}
	}
	return nil
}

func (Server *Service) Matrix(Ctx Context.Context) (AccessMatrix, error) {
	return Server.Repo.Matrix(Ctx)
}

func (Server *Service) ListDatabases(Ctx Context.Context) ([]DatabaseRef, error) {
	return Server.Repo.ListDatabases(Ctx)
}
