package auth

import (
	"context"
	"time"
)

// PrincipalKind is how a request proved who it is.
type PrincipalKind string

const (
	// PrincipalUser is a signed-in person holding a JWT access token.
	PrincipalUser PrincipalKind = "user"
	// PrincipalAPIKey is a script holding a personal API key. Read-only, and
	// restricted to an explicit allow-list of paths — see internal/apikey.
	PrincipalAPIKey PrincipalKind = "api_key"
)

// Principal is who is making a request and by what means.
//
// It is the single place that distinguishes a browser session from a script's
// API key. Handlers should keep using MustUserID and stay unaware of the
// difference; the few that must care — anything that changes credentials — ask
// with IsAPIKey rather than inspecting the context themselves.
type Principal struct {
	Kind   PrincipalKind
	UserID string
	Email  string

	// KeyID and KeyPrefix are set only for PrincipalAPIKey. KeyPrefix is the
	// public first segment of the token and is safe to log; the secret itself
	// never reaches this struct.
	KeyID     string
	KeyPrefix string
	// Scope is "read" for API keys and empty for people, whose permissions come
	// from ownership rather than from the credential.
	Scope string

	// IssuedAt is when the credential was minted. For a JWT this is the `iat`
	// claim, which is what lets an endpoint demand a recently proven session.
	IssuedAt time.Time
}

const principalKey contextKey = "auth.principal"

// ContextWithPrincipal records p on the context.
//
// It also populates the older user-ID and email keys, which is the point: every
// handler written before API keys existed — every auth.MustUserID call in
// internal/course and internal/club — keeps working with no change at all,
// because the identity it reads arrives by the same route it always did.
func ContextWithPrincipal(ctx context.Context, p Principal) context.Context {
	ctx = context.WithValue(ctx, principalKey, p)
	ctx = context.WithValue(ctx, userIDKey, p.UserID)
	return context.WithValue(ctx, emailKey, p.Email)
}

// PrincipalFrom returns the principal recorded by an authenticating middleware.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey).(Principal)
	return p, ok && p.UserID != ""
}

// IsAPIKey reports whether this request was authenticated by an API key rather
// than by a signed-in session.
func IsAPIKey(ctx context.Context) bool {
	p, ok := PrincipalFrom(ctx)
	return ok && p.Kind == PrincipalAPIKey
}
