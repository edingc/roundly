package auth

import (
	"context"
	"net/http"
	"strings"

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

		ctx := context.WithValue(r.Context(), userIDKey, claims.Subject)
		ctx = context.WithValue(ctx, emailKey, claims.Email)
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
