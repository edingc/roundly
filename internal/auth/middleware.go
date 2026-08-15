package auth

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/edingc/roundly/internal/httpx"
)

type contextKey string

const (
	userIDKey contextKey = "auth.user_id"
	emailKey  contextKey = "auth.email"
)

// Middleware rejects requests without a valid access token and puts the caller's
// identity on the request context.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// An API key has already been authenticated and policy-checked by the
		// global guard, which is the only thing that can put a principal here.
		// Without this, every key-authenticated request would fail the JWT
		// parse below and a read-only key could never reach anything.
		if _, ok := PrincipalFrom(r.Context()); ok {
			next.ServeHTTP(w, r)
			return
		}

		raw, err := bearerToken(r)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}

		claims, err := s.tokens.ParseAccessToken(raw)
		if err != nil {
			httpx.Error(w, r, httpx.Unauthorized("Your session has expired. Please sign in again."))
			return
		}

		var issuedAt time.Time
		if claims.IssuedAt != nil {
			issuedAt = claims.IssuedAt.Time
		}
		ctx := ContextWithPrincipal(r.Context(), Principal{
			Kind:     PrincipalUser,
			UserID:   claims.Subject,
			Email:    claims.Email,
			IssuedAt: issuedAt,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func bearerToken(r *http.Request) (string, error) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return "", httpx.Unauthorized("Sign in to continue.")
	}
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return "", httpx.Unauthorized("Authorization header must be in the form \"Bearer <token>\".")
	}
	return strings.TrimSpace(token), nil
}

// UserID returns the authenticated user's ID. The bool is false when the request
// did not pass through Middleware.
func UserID(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(userIDKey).(string)
	return userID, ok && userID != ""
}

// MustUserID returns the authenticated user's ID, panicking if absent. It is
// safe in any handler mounted behind Middleware, where a missing ID is a routing
// bug rather than a runtime condition.
func MustUserID(ctx context.Context) string {
	userID, ok := UserID(ctx)
	if !ok {
		panic("auth: no user on context; handler is not behind auth.Middleware")
	}
	return userID
}

// Email returns the authenticated user's email from the access token claims.
func Email(ctx context.Context) (string, bool) {
	email, ok := ctx.Value(emailKey).(string)
	return email, ok && email != ""
}
