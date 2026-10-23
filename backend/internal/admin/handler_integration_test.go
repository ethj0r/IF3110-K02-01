//go:build integration

package Admin

import (
	Bytes "bytes"
	Context "context"
	Json "encoding/json"
	Errors "errors"
	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Testdb "github.com/ethj0r/IF3110-K02-01/backend/internal/testdb"
	Testutil "github.com/ethj0r/IF3110-K02-01/backend/internal/testutil"
	Chi "github.com/go-chi/chi/v5"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
	Http "net/http"
	Httptest "net/http/httptest"
	Strings "strings"
	Testing "testing"
)

const AdminTestPassword = "correct horse battery staple"
const TargetTestPassword = "target account password"

type AdminUser = User

type Fixture struct {
	Pool    *Pgxpool.Pool
	Service *Service
	Handler *Handler
	Auth    *Auth.Service
	AuthAPI *Auth.Repository
	Admin   Auth.Principal
}

func NewFixture(T *Testing.T) *Fixture {
	T.Helper()
	Pool := Testdb.PlatformDatabase(T)
	Repo := &Repository{Pool: Pool}
	Settings := Auth.DefaultOptions()
	Settings.Origin = "https://app.test"
	Settings.CookieSecure = true
	Settings.EncryptionKey = Bytes.Repeat([]byte{7}, 32)
	AuthAPI := &Auth.Repository{Pool: Pool}
	AuthService, Err := Auth.NewService(AuthAPI, Settings)
	if Err != nil {
		T.Fatal(Err)
	}
	Admin, Err := Auth.BootstrapAdmin(Context.Background(), AuthAPI, "admin", AdminTestPassword)
	if Err != nil {
		T.Fatal(Err)
	}
	Service := NewService(Repo)
	return &Fixture{Pool: Pool, Service: Service, Handler: &Handler{Service: Service}, Auth: AuthService, AuthAPI: AuthAPI, Admin: Admin}
}

type HandlerFunc func(Http.ResponseWriter, *Http.Request)

func Request(T *Testing.T, Target HandlerFunc, Method, Body string, Params map[string]string, Principal Auth.Principal) *Httptest.ResponseRecorder {
	T.Helper()
	Request := Httptest.NewRequest(Method, "/admin", Strings.NewReader(Body))
	if Body != "" {
		Request.Header.Set("Content-Type", "application/json")
	}
	Ctx := Context.WithValue(Request.Context(), Auth.SessionContext, Auth.Authenticated{Principal: Principal})
	if len(Params) > 0 {
		RouteContext := Chi.NewRouteContext()
		for Key, Value := range Params {
			RouteContext.URLParams.Add(Key, Value)
		}
		Ctx = Context.WithValue(Ctx, Chi.RouteCtxKey, RouteContext)
	}
	Response := Httptest.NewRecorder()
	Target(Response, Request.WithContext(Ctx))
	return Response
}

func Decode[Result any](T *Testing.T, Response *Httptest.ResponseRecorder) Result {
	T.Helper()
	var ResultValue Result
	if Err := Json.Unmarshal(Response.Body.Bytes(), &ResultValue); Err != nil {
		T.Fatalf("invalid JSON response: %s", Response.Body.String())
	}
	return ResultValue
}

func ExpectStatus(T *Testing.T, Response *Httptest.ResponseRecorder, Status int) {
	T.Helper()
	if Response.Code != Status {
		T.Fatalf("status = %d, want %d; body = %s", Response.Code, Status, Response.Body.String())
	}
}

func TestAdminUserGroupAndAccessLifecycle(T *Testing.T) {
	App := NewFixture(T)
	Admin := App.Admin
	Create := Request(T, App.Handler.CreateUser, Http.MethodPost, `{"username":"reporter","password":"`+TargetTestPassword+`"}`, nil, Admin)
	ExpectStatus(T, Create, Http.StatusCreated)
	User := Decode[AdminUser](T, Create)
	if User.Username != "reporter" || User.IsPlatformAdmin || !User.IsActive {
		T.Fatalf("unexpected created user: %+v", User)
	}
	Duplicate := Request(T, App.Handler.CreateUser, Http.MethodPost, `{"username":"REPORTER","password":"`+TargetTestPassword+`"}`, nil, Admin)
	ExpectStatus(T, Duplicate, Http.StatusConflict)

	GroupResponse := Request(T, App.Handler.CreateGroup, Http.MethodPost, `{"name":"Reporting","description":"Reporting readers"}`, nil, Admin)
	ExpectStatus(T, GroupResponse, Http.StatusCreated)
	Group := Decode[Group](T, GroupResponse)
	if Group.Name != "Reporting" || Group.MemberCount != 0 {
		T.Fatalf("unexpected created group: %+v", Group)
	}
	DuplicateGroup := Request(T, App.Handler.CreateGroup, Http.MethodPost, `{"name":"reporting"}`, nil, Admin)
	ExpectStatus(T, DuplicateGroup, Http.StatusConflict)

	Added := Request(T, App.Handler.AddMember, Http.MethodPut, "", map[string]string{"id": Group.ID, "userId": User.ID}, Admin)
	ExpectStatus(T, Added, Http.StatusNoContent)
	AddedAgain := Request(T, App.Handler.AddMember, Http.MethodPut, "", map[string]string{"id": Group.ID, "userId": User.ID}, Admin)
	ExpectStatus(T, AddedAgain, Http.StatusNoContent)
	Members := Decode[[]Member](T, Request(T, App.Handler.ListMembers, Http.MethodGet, "", map[string]string{"id": Group.ID}, Admin))
	if len(Members) != 1 || Members[0].ID != User.ID {
		T.Fatalf("unexpected members: %+v", Members)
	}
	MissingGroup := "00000000-0000-0000-0000-000000000000"
	ExpectStatus(T, Request(T, App.Handler.ListMembers, Http.MethodGet, "", map[string]string{"id": MissingGroup}, Admin), Http.StatusNotFound)

	ConnectionID := Testutil.CreateConnection(T, App.Pool, "reporting-target")
	Set := Request(T, App.Handler.SetAccess, Http.MethodPut, `{"level":"view_only"}`, map[string]string{"id": Group.ID, "databaseId": ConnectionID}, Admin)
	ExpectStatus(T, Set, Http.StatusNoContent)
	InvalidLevel := Request(T, App.Handler.SetAccess, Http.MethodPut, `{"level":"owner"}`, map[string]string{"id": Group.ID, "databaseId": ConnectionID}, Admin)
	ExpectStatus(T, InvalidLevel, Http.StatusBadRequest)
	Accesses := Decode[[]GroupAccess](T, Request(T, App.Handler.ListGroupAccess, Http.MethodGet, "", map[string]string{"id": Group.ID}, Admin))
	if len(Accesses) != 1 || Accesses[0].Level != Access.LevelViewOnly {
		T.Fatalf("unexpected group access: %+v", Accesses)
	}

	Matrix := Decode[AccessMatrix](T, Request(T, App.Handler.Matrix, Http.MethodGet, "", nil, Admin))
	if len(Matrix.Groups) != 1 || len(Matrix.Databases) != 1 || len(Matrix.Grants) != 1 || Matrix.Grants[0].Level != Access.LevelViewOnly {
		T.Fatalf("unexpected access matrix: %+v", Matrix)
	}

	Fetched := Decode[AdminUser](T, Request(T, App.Handler.GetUser, Http.MethodGet, "", map[string]string{"id": User.ID}, Admin))
	if len(Fetched.Groups) != 1 || Fetched.Groups[0].ID != Group.ID {
		T.Fatalf("user group membership was not listed: %+v", Fetched)
	}

	Deactivated := Request(T, App.Handler.UpdateUser, Http.MethodPatch, `{"is_active":false}`, map[string]string{"id": User.ID}, Admin)
	ExpectStatus(T, Deactivated, Http.StatusOK)
	if Decode[AdminUser](T, Deactivated).IsActive {
		T.Fatal("user was not deactivated")
	}

	NewPassword := "replacement password 42"
	Reset := Request(T, App.Handler.ResetPassword, Http.MethodPut, `{"password":"`+NewPassword+`"}`, map[string]string{"id": User.ID}, Admin)
	ExpectStatus(T, Reset, Http.StatusOK)
	var StoredHash string
	if Err := App.Pool.QueryRow(Context.Background(), "SELECT password_hash FROM users WHERE id::text = $1", User.ID).Scan(&StoredHash); Err != nil {
		T.Fatal(Err)
	}
	if !Auth.VerifyPassword(NewPassword, StoredHash) {
		T.Fatal("reset password was not stored as a verifiable hash")
	}

	ExpectStatus(T, Request(T, App.Handler.DeleteUser, Http.MethodDelete, "", map[string]string{"id": User.ID}, Admin), Http.StatusNoContent)
	ExpectStatus(T, Request(T, App.Handler.GetUser, Http.MethodGet, "", map[string]string{"id": User.ID}, Admin), Http.StatusNotFound)
	ExpectStatus(T, Request(T, App.Handler.DeleteGroup, Http.MethodDelete, "", map[string]string{"id": Group.ID}, Admin), Http.StatusNoContent)
	Matrix = Decode[AccessMatrix](T, Request(T, App.Handler.Matrix, Http.MethodGet, "", nil, Admin))
	if len(Matrix.Groups) != 0 || len(Matrix.Grants) != 0 {
		T.Fatalf("group deletion did not remove grants: %+v", Matrix)
	}
}

func TestAdminProtectsLastActiveAdministrator(T *Testing.T) {
	App := NewFixture(T)
	Context := Context.Background()
	if _, Err := App.Service.UpdateUserActive(Context, App.Admin.ID, false); !Errors.Is(Err, ErrLastAdmin) {
		T.Fatalf("last administrator was deactivated: %v", Err)
	}
	if Err := App.Service.DeleteUser(Context, App.Admin.ID); !Errors.Is(Err, ErrLastAdmin) {
		T.Fatalf("last administrator was deleted: %v", Err)
	}
	SecondID := Testutil.CreateUser(T, App.Pool, "second-admin", true)
	if _, Err := App.Service.UpdateUserActive(Context, App.Admin.ID, false); Err != nil {
		T.Fatalf("administrator deactivation with a peer failed: %v", Err)
	}
	if _, Err := App.Service.UpdateUserActive(Context, SecondID, false); !Errors.Is(Err, ErrLastAdmin) {
		T.Fatalf("remaining administrator was deactivated: %v", Err)
	}
	if Err := App.Service.DeleteUser(Context, SecondID); !Errors.Is(Err, ErrLastAdmin) {
		T.Fatalf("remaining administrator was deleted: %v", Err)
	}
}

func TestAdminAccessChangesRotateAndRevokeSessions(T *Testing.T) {
	App := NewFixture(T)
	Context := Context.Background()
	User, Err := App.Service.CreateUser(Context, CreateUserInput{Username: "session-user", Password: TargetTestPassword})
	if Err != nil {
		T.Fatal(Err)
	}
	State, Err := App.Auth.Login(Context, User.Username, TargetTestPassword, "")
	if Err != nil {
		T.Fatal(Err)
	}
	if State.Principal.ID != User.ID {
		T.Fatal("test session belongs to an unexpected user")
	}
	if Version := AuthVersion(T, App.Pool, User.ID); Version != 1 {
		T.Fatalf("initial auth version = %d", Version)
	}
	Group, Err := App.Service.CreateGroup(Context, CreateGroupInput{Name: "Session Group"})
	if Err != nil {
		T.Fatal(Err)
	}
	ConnectionID := Testutil.CreateConnection(T, App.Pool, "session-target")

	if Err := App.Service.AddMember(Context, Group.ID, User.ID); Err != nil {
		T.Fatal(Err)
	}
	if Version := AuthVersion(T, App.Pool, User.ID); Version != 2 {
		T.Fatalf("membership increase did not rotate privileges: version=%d", Version)
	}
	if LatestSessionRevoked(T, App.Pool, User.ID) {
		T.Fatal("membership increase revoked the session")
	}

	if Err := App.Service.SetAccess(Context, Group.ID, ConnectionID, "view_only"); Err != nil {
		T.Fatal(Err)
	}
	if Version := AuthVersion(T, App.Pool, User.ID); Version != 3 {
		T.Fatalf("access increase did not rotate privileges: version=%d", Version)
	}
	if Err := App.Service.SetAccess(Context, Group.ID, ConnectionID, "view_edit"); Err != nil {
		T.Fatal(Err)
	}
	if Version := AuthVersion(T, App.Pool, User.ID); Version != 4 {
		T.Fatalf("access increase did not rotate privileges: version=%d", Version)
	}
	if LatestSessionRevoked(T, App.Pool, User.ID) {
		T.Fatal("access increase revoked the session")
	}

	if Err := App.Service.SetAccess(Context, Group.ID, ConnectionID, "view_only"); Err != nil {
		T.Fatal(Err)
	}
	if Version := AuthVersion(T, App.Pool, User.ID); Version != 5 {
		T.Fatalf("access decrease was not recorded: version=%d", Version)
	}
	if !LatestSessionRevoked(T, App.Pool, User.ID) {
		T.Fatal("access decrease did not revoke the session")
	}

	State, Err = App.Auth.Login(Context, User.Username, TargetTestPassword, "")
	if Err != nil {
		T.Fatal(Err)
	}
	if LatestSessionRevoked(T, App.Pool, User.ID) {
		T.Fatal("new session was unexpectedly revoked")
	}
	if Err := App.Service.RemoveMember(Context, Group.ID, User.ID); Err != nil {
		T.Fatal(Err)
	}
	if Version := AuthVersion(T, App.Pool, User.ID); Version != 6 {
		T.Fatalf("membership removal was not recorded: version=%d", Version)
	}
	if !LatestSessionRevoked(T, App.Pool, User.ID) {
		T.Fatal("membership removal did not revoke sessions")
	}

	if Err := App.Service.AddMember(Context, Group.ID, User.ID); Err != nil {
		T.Fatal(Err)
	}
	if Version := AuthVersion(T, App.Pool, User.ID); Version != 7 {
		T.Fatalf("membership re-add was not recorded: version=%d", Version)
	}
	if !LatestSessionRevoked(T, App.Pool, User.ID) {
		T.Fatal("privilege rotation must not revive a revoked session")
	}

	if _, Err := App.Auth.Login(Context, User.Username, TargetTestPassword, ""); Err != nil {
		T.Fatal(Err)
	}
	if Err := App.Service.DeleteGroup(Context, Group.ID); Err != nil {
		T.Fatal(Err)
	}
	if Version := AuthVersion(T, App.Pool, User.ID); Version != 8 {
		T.Fatalf("group deletion was not recorded: version=%d", Version)
	}
	if !LatestSessionRevoked(T, App.Pool, User.ID) {
		T.Fatal("group deletion did not revoke member sessions")
	}
}

func AuthVersion(T *Testing.T, Pool *Pgxpool.Pool, UserID string) int64 {
	T.Helper()
	var Version int64
	if Err := Pool.QueryRow(Context.Background(), "SELECT auth_version FROM users WHERE id::text = $1", UserID).Scan(&Version); Err != nil {
		T.Fatal(Err)
	}
	return Version
}

func LatestSessionRevoked(T *Testing.T, Pool *Pgxpool.Pool, UserID string) bool {
	T.Helper()
	var Revoked bool
	if Err := Pool.QueryRow(Context.Background(), "SELECT revoked_at IS NOT NULL FROM sessions WHERE user_id::text = $1 ORDER BY created_at DESC, id DESC LIMIT 1", UserID).Scan(&Revoked); Err != nil {
		T.Fatal(Err)
	}
	return Revoked
}
