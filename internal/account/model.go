// Package account implements the user profile: the person behind the golf bag,
// their data in portable form, and the personal API keys that read it.
//
// It deliberately returns auth.User rather than a DTO of its own. The SPA
// hydrates its whole notion of the signed-in person from one shape, and a
// second one here would drift from it.
//
// Everything in this package is scoped to the caller. There is no cross-user
// read, and no endpoint here is reachable by an API key — see internal/apikey,
// which blocks this entire path prefix.
package account

// Field length bounds. These are generous: a name field that rejects a real
// name is worse than one that stores a long one.
const (
	maxNameLen     = 60
	maxDisplayLen  = 80 // matches the signup bound in internal/auth
	maxLocationLen = 80
)

// ProfileInput is a validated profile update. Nil means "clear this field";
// the handler has already trimmed and rejected anything out of bounds.
type ProfileInput struct {
	FirstName       *string
	LastName        *string
	DisplayName     string
	HomeCourseID    *string
	LocationCity    *string
	LocationRegion  *string
	LocationCountry *string
}
