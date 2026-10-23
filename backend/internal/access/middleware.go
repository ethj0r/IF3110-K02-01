package Access

import (
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Httpx "github.com/ethj0r/IF3110-K02-01/backend/internal/httpx"
	Chi "github.com/go-chi/chi/v5"
	Http "net/http"
)

func RequireAdmin(Next Http.Handler) Http.Handler {
	return Http.HandlerFunc(func(W Http.ResponseWriter, R *Http.Request) {
		User, Ok := Auth.PrincipalFromContext(R.Context())
		if !Ok {
			Httpx.Error(W, Http.StatusUnauthorized, "unauthenticated", "Authentication required")
			return
		}
		if !User.IsPlatformAdmin {
			Httpx.Error(W, Http.StatusForbidden, "forbidden", "Platform administrator access required")
			return
		}
		Next.ServeHTTP(W, R)
	})
}

func RequireDatabase(Policy Evaluator, Act Action, Parameter string) func(Http.Handler) Http.Handler {
	return func(Next Http.Handler) Http.Handler {
		return Http.HandlerFunc(func(W Http.ResponseWriter, R *Http.Request) {
			User, Ok := Auth.PrincipalFromContext(R.Context())
			if !Ok {
				Httpx.Error(W, Http.StatusUnauthorized, "unauthenticated", "Authentication required")
				return
			}
			Allowed, Err := Can(R.Context(), Policy, User, Act, Chi.URLParam(R, Parameter))
			if Err != nil {
				Httpx.Error(W, Http.StatusInternalServerError, "internal_error", "An unexpected error occurred")
				return
			}
			if !Allowed {
				Httpx.Error(W, Http.StatusForbidden, "forbidden", "Database access denied")
				return
			}
			Next.ServeHTTP(W, R)
		})
	}
}
