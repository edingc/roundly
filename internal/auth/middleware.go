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

// RequireVerifiedEmail turns away a signed-in user who has not confirmed their
// address.
//
// Mounted over the application's endpoints and deliberately not over the auth
// ones: somebody in this state still has to be able to read /auth/me, ask for
// another link, and sign out. Blocking those would leave them holding a session
// with nowhere to take it.
//
// Two things pass straight through:
//
//   - Every request when this instance cannot send mail. Demanding a
//     confirmation nobody can deliver would lock out every account on the
//     instance, including ones that predate the feature.
//
//   - API keys. A key can only be minted from behind this very gate, so its
//     existence is already proof the account was verified. Re-checking would
//     add a query to every scripted request to re-establish something that
//     cannot have changed without the key being revoked.
func (s *Service) RequireVerifiedEmail(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !s.EmailVerificationRequired() || IsAPIKey(ctx) {
			next.ServeHTTP(w, r)
			return
		}

		userID, ok := UserID(ctx)
		if !ok {
			httpx.Error(w, r, httpx.Unauthorized("Sign in to continue."))
			return
		}

		row, err := s.db.Queries.GetUserByID(ctx, userID)
		if err != nil {
			httpx.Error(w, r, httpx.Unauthorized("Your session has expired. Please sign in again."))
			return
		}
		if row.EmailVerified == 0 {
			httpx.Error(w, r, errEmailUnverified())
			return
		}

		next.ServeHTTP(w, r)
	})
}

// errEmailUnverified is a distinct code rather than a bare 403, because the
// client has a specific screen for it and needs to tell it apart from every
// other way of being refused.
func errEmailUnverified() error {
	return &httpx.APIError{
		Status:  http.StatusForbidden,
		Code:    "email_unverified",
		Message: "Confirm your email address to continue. Check your inbox for the link, or ask for a new one.",
	}
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

// RequireAdmin refuses anyone who is not the configured site administrator.
//
// It reads the user's current address from the database rather than trusting
// the email claim in the access token. The claim is a snapshot: a token minted
// before an address change still carries the old one, which would leave a
// 15-minute window in which administrator rights either linger after they
// should have gone or fail to apply after they should have arrived. An admin
// check is rare enough to afford the query.
//
// API keys never reach this — internal/apikey blocks the whole /api/admin
// prefix, and its allow-list denies anything unlisted by default — but the
// principal check here is the guarantee, not that.
func (s *Service) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		if IsAPIKey(ctx) {
			httpx.Error(w, r, httpx.Forbidden("API keys cannot perform administrator actions."))
			return
		}

		userID, ok := UserID(ctx)
		if !ok {
			httpx.Error(w, r, httpx.Unauthorized("Sign in to continue."))
			return
		}

		row, err := s.db.Queries.GetUserByID(ctx, userID)
		if err != nil {
			// A vanished user is not an administrator. Deliberately the same
			// refusal as a live non-admin, so neither case is distinguishable.
			httpx.Error(w, r, errNotAdmin())
			return
		}
		if !s.isAdminEmail(row.Email) {
			httpx.Error(w, r, errNotAdmin())
			return
		}

		next.ServeHTTP(w, r)
	})
}

// errNotAdmin is one message for every way of not being the administrator, so a
// prober cannot use the difference to learn who is.
func errNotAdmin() error {
	return httpx.Forbidden("Only the site administrator can do that.")
}
