package Auth

import (
	Context "context"
	Errors "errors"
	Regexp "regexp"
	Time "time"
)

var UsernamePattern = Regexp.MustCompile(`^[A-Za-z0-9_.-]{3,64}$`)

var (
	ErrCredentials  = Errors.New("invalid username or password")
	ErrUnauthorized = Errors.New("authentication required")
	ErrCSRF         = Errors.New("request verification failed")
	ErrUnavailable  = Errors.New("authentication service unavailable")
	ErrAdminExists  = Errors.New("an administrator already exists")
	ErrBusy         = Errors.New("too many authentication requests")
)

type Principal struct {
	ID              string `json:"id"`
	Username        string `json:"username"`
	IsPlatformAdmin bool   `json:"is_platform_admin"`
}

type User struct {
	Principal
	PasswordHash string `json:"-"`
	IsActive     bool   `json:"-"`
	AuthVersion  int64  `json:"-"`
}

type Session struct {
	ID                  string     `json:"-"`
	UserID              string     `json:"-"`
	SecretCiphertext    []byte     `json:"-"`
	UserAuthVersion     int64      `json:"-"`
	UserIsPlatformAdmin bool       `json:"-"`
	CreatedAt           Time.Time  `json:"-"`
	LastSeenAt          Time.Time  `json:"-"`
	RotatedAt           Time.Time  `json:"-"`
	AbsoluteExpiresAt   Time.Time  `json:"-"`
	RevokedAt           *Time.Time `json:"-"`
	IdentifierExpiresAt *Time.Time `json:"-"`
}

type SessionUpdate struct {
	SeenAt              Time.Time
	Rotate              bool
	Revoke              bool
	SecretCiphertext    []byte
	IdentifierHash      []byte
	GraceUntil          Time.Time
	UserAuthVersion     int64
	UserIsPlatformAdmin bool
}

type Authenticated struct {
	Principal         Principal
	SessionID         string    `json:"-"`
	Identifier        string    `json:"-"`
	CSRFToken         string    `json:"-"`
	AbsoluteExpiresAt Time.Time `json:"-"`
	SetCookie         bool      `json:"-"`
}

type ContextKey uint8

const SessionContext ContextKey = 0

func PrincipalFromContext(Ctx Context.Context) (Principal, bool) {
	State, Ok := Ctx.Value(SessionContext).(Authenticated)
	return State.Principal, Ok
}
