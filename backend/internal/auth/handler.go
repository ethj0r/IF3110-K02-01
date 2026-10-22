package Auth

import (
	Context "context"
	Subtle "crypto/subtle"
	Json "encoding/json"
	Errors "errors"
	Httpx "github.com/ethj0r/IF3110-K02-01/backend/internal/httpx"
	Chi "github.com/go-chi/chi/v5"
	Io "io"
	Mime "mime"
	Http "net/http"
	Time "time"
)

type Handler struct{ Service *Service }

func (Routes *Handler) Register(Api Chi.Router) {
	Api.Route("/auth", func(R Chi.Router) {
		R.Use(NoStore)
		R.Post("/login", Routes.Login)
		R.Group(func(Protected Chi.Router) {
			Protected.Use(Routes.RequireSession)
			Protected.Get("/me", Routes.Me)
			Protected.Get("/csrf", Routes.CSRF)
			Protected.Post("/logout", Routes.Logout)
		})
	})
}

func NoStore(Next Http.Handler) Http.Handler {
	return Http.HandlerFunc(func(W Http.ResponseWriter, R *Http.Request) {
		W.Header().Set("Cache-Control", "no-store")
		W.Header().Set("Pragma", "no-cache")
		Next.ServeHTTP(W, R)
	})
}

func (Routes *Handler) ValidOrigin(R *Http.Request) bool {
	Values := R.Header.Values("Origin")
	return len(Values) == 1 && Values[0] == Routes.Service.Settings.Origin && R.Header.Get("Sec-Fetch-Site") != "cross-site"
}

func (Routes *Handler) Login(W Http.ResponseWriter, R *Http.Request) {
	if !Routes.ValidOrigin(R) {
		WriteAuthError(W, ErrCSRF)
		return
	}
	MediaType, _, Err := Mime.ParseMediaType(R.Header.Get("Content-Type"))
	if Err != nil || MediaType != "application/json" {
		Httpx.Error(W, 415, "unsupported_media_type", "Use application/json")
		return
	}
	var Input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	Decoder := Json.NewDecoder(Http.MaxBytesReader(W, R.Body, 8192))
	Decoder.DisallowUnknownFields()
	if Err := Decoder.Decode(&Input); Err != nil {
		Httpx.Error(W, 400, "invalid_request", "Invalid login request")
		return
	}
	if Err := Decoder.Decode(new(any)); Err != Io.EOF {
		Httpx.Error(W, 400, "invalid_request", "Invalid login request")
		return
	}
	Previous, HasPrevious := SessionCookie(R)
	if !HasPrevious {
		Previous = ""
	}
	State, Err := Routes.Service.Login(R.Context(), Input.Username, Input.Password, Previous)
	if Err != nil {
		WriteAuthError(W, Err)
		return
	}
	Routes.SetCookie(W, State)
	Httpx.JSON(W, 200, map[string]any{"user": State.Principal, "csrf_token": State.CSRFToken, "expires_at": State.AbsoluteExpiresAt})
}

func SessionCookie(R *Http.Request) (string, bool) {
	Value := ""
	Count := 0
	for _, Cookie := range R.Cookies() {
		if Cookie.Name == CookieName {
			Value = Cookie.Value
			Count++
		}
	}
	return Value, Count == 1 && ValidToken(Value)
}

func (Routes *Handler) RequireSession(Next Http.Handler) Http.Handler {
	return NoStore(Http.HandlerFunc(func(W Http.ResponseWriter, R *Http.Request) {
		Unsafe := R.Method != Http.MethodGet && R.Method != Http.MethodHead && R.Method != Http.MethodOptions
		if Unsafe && !Routes.ValidOrigin(R) {
			WriteAuthError(W, ErrCSRF)
			return
		}
		Identifier, Ok := SessionCookie(R)
		if !Ok {
			Routes.ClearCookie(W)
			WriteAuthError(W, ErrUnauthorized)
			return
		}
		State, Err := Routes.Service.Authenticate(R.Context(), Identifier)
		if Err != nil {
			if Errors.Is(Err, ErrUnauthorized) {
				Routes.ClearCookie(W)
			}
			WriteAuthError(W, Err)
			return
		}
		if Unsafe {
			Tokens := R.Header.Values("X-CSRF-Token")
			if len(Tokens) != 1 || !ValidToken(Tokens[0]) || Subtle.ConstantTimeCompare([]byte(Tokens[0]), []byte(State.CSRFToken)) != 1 {
				WriteAuthError(W, ErrCSRF)
				return
			}
		}
		if State.SetCookie {
			Routes.SetCookie(W, State)
		}
		Next.ServeHTTP(W, R.WithContext(Context.WithValue(R.Context(), SessionContext, State)))
	}))
}

func (Routes *Handler) Me(W Http.ResponseWriter, R *Http.Request) {
	State, Ok := R.Context().Value(SessionContext).(Authenticated)
	if !Ok {
		WriteAuthError(W, ErrUnauthorized)
		return
	}
	Httpx.JSON(W, 200, map[string]any{"user": State.Principal, "csrf_token": State.CSRFToken, "expires_at": State.AbsoluteExpiresAt})
}

func (Routes *Handler) CSRF(W Http.ResponseWriter, R *Http.Request) {
	State, Ok := R.Context().Value(SessionContext).(Authenticated)
	if !Ok {
		WriteAuthError(W, ErrUnauthorized)
		return
	}
	Httpx.JSON(W, 200, map[string]string{"csrf_token": State.CSRFToken})
}

func (Routes *Handler) Logout(W Http.ResponseWriter, R *Http.Request) {
	State, Ok := R.Context().Value(SessionContext).(Authenticated)
	if !Ok {
		WriteAuthError(W, ErrUnauthorized)
		return
	}
	if Err := Routes.Service.Repo.RevokeSession(R.Context(), State.SessionID, Routes.Service.Now().UTC()); Err != nil {
		WriteAuthError(W, Err)
		return
	}
	Routes.ClearCookie(W)
	W.WriteHeader(Http.StatusNoContent)
}

func (Routes *Handler) SetCookie(W Http.ResponseWriter, State Authenticated) {
	Age := int(State.AbsoluteExpiresAt.Sub(Routes.Service.Now()).Seconds())
	if Age < 1 {
		Age = 1
	}
	Http.SetCookie(W, &Http.Cookie{Name: CookieName, Value: State.Identifier, Path: "/", HttpOnly: true, Secure: Routes.Service.Settings.CookieSecure, SameSite: Http.SameSiteLaxMode, Expires: State.AbsoluteExpiresAt, MaxAge: Age})
}

func (Routes *Handler) ClearCookie(W Http.ResponseWriter) {
	Http.SetCookie(W, &Http.Cookie{Name: CookieName, Value: "", Path: "/", HttpOnly: true, Secure: Routes.Service.Settings.CookieSecure, SameSite: Http.SameSiteLaxMode, Expires: Time.Unix(1, 0), MaxAge: -1})
}

func WriteAuthError(W Http.ResponseWriter, Err error) {
	switch {
	case Errors.Is(Err, ErrCredentials):
		Httpx.Error(W, 401, "invalid_credentials", "Invalid username or password")
	case Errors.Is(Err, ErrUnauthorized):
		Httpx.Error(W, 401, "unauthenticated", "Authentication required")
	case Errors.Is(Err, ErrCSRF):
		Httpx.Error(W, 403, "csrf_rejected", "Request verification failed")
	case Errors.Is(Err, ErrBusy):
		Httpx.Error(W, 429, "authentication_busy", "Try again shortly")
	case Errors.Is(Err, ErrUnavailable):
		Httpx.Error(W, 503, "authentication_unavailable", "Authentication service unavailable")
	default:
		Httpx.Error(W, 500, "internal_error", "An unexpected error occurred")
	}
}
