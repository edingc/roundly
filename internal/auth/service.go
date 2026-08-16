package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/edingc/roundly/internal/database"
	"github.com/edingc/roundly/internal/database/sqlc"
	"github.com/edingc/roundly/internal/httpx"
	"github.com/edingc/roundly/internal/id"
	"github.com/edingc/roundly/internal/timex"
)

const ProviderGoogle = "google"

// Service implements the authentication use cases: both login paths, session
// rotation, and provider linking.
type Service struct {
	db     *database.DB
	tokens *TokenIssuer
	google *GoogleProvider
	// adminEmail names the site administrator. Configuration rather than a
	// column, because on a self-hosted instance the administrator is whoever
	// runs the process — and a value in the environment cannot drift out of
	// step with the database or be edited through the app. Empty means the
	// instance has no administrator.
	adminEmail string
}

func NewService(db *database.DB, tokens *TokenIssuer, google *GoogleProvider, adminEmail string) *Service {
	return &Service{
		db:         db,
		tokens:     tokens,
		google:     google,
		adminEmail: httpx.NormalizeEmail(adminEmail),
	}
}

// isAdminEmail reports whether an address is the configured administrator.
func (s *Service) isAdminEmail(email string) bool {
	return s.adminEmail != "" && httpx.NormalizeEmail(email) == s.adminEmail
}

// User is the API representation of an account.
//
// This is the only user DTO. The profile endpoints in internal/account return
// it too rather than defining their own: the SPA hydrates its whole notion of
// the signed-in person from here, and a parallel shape would drift from this
// one within a release.
type User struct {
	ID            string   `json:"id"`
	Email         string   `json:"email"`
	DisplayName   string   `json:"display_name"`
	EmailVerified bool     `json:"email_verified"`
	HasPassword   bool     `json:"has_password"`
	Providers     []string `json:"providers"`
	// DistanceUnit is a display preference: every distance in the database is
	// stored in yards, and the client converts on the way in and out.
	DistanceUnit string `json:"distance_unit"`

	// The profile fields below are all optional. A player who wants to be
	// nothing but a display name should never be made to fill one in.
	FirstName *string `json:"first_name"`
	LastName  *string `json:"last_name"`
	// AvatarURL is a relative path so it works through the Vite dev proxy and
	// same-origin in production without configuration. Nil when unset.
	AvatarURL    *string `json:"avatar_url"`
	HomeCourseID *string `json:"home_course_id"`
	// HomeCourseName is resolved for display so the client does not have to
	// fetch the course just to render its name.
	HomeCourseName  *string `json:"home_course_name"`
	LocationCity    *string `json:"location_city"`
	LocationRegion  *string `json:"location_region"`
	LocationCountry *string `json:"location_country"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`

	// IsAdmin is derived from configuration, never stored. It exists so the
	// client can show the administrator's screens; every actual check happens
	// server-side in RequireAdmin.
	IsAdmin bool `json:"is_admin"`
}

// Distance units the app understands.
const (
	UnitYards  = "yards"
	UnitMeters = "meters"
)

// DistanceUnits are the accepted values for User.DistanceUnit.
var DistanceUnits = []string{UnitYards, UnitMeters}

// Session is what a successful login returns.
type Session struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	User         *User  `json:"user"`
}

func (s *Service) toUser(ctx context.Context, row sqlc.User) (*User, error) {
	accounts, err := s.db.Queries.ListOAuthAccountsByUser(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("list oauth accounts: %w", err)
	}
	providers := make([]string, 0, len(accounts))
	for _, a := range accounts {
		providers = append(providers, a.Provider)
	}

	unit := row.DistanceUnit
	if unit == "" {
		// Defensive: the column is NOT NULL with a default, but a row written
		// by an older binary during a rolling restart would read back empty.
		unit = UnitYards
	}

	// Resolved only when a home course is actually set, so the common case
	// stays at the one query this function has always cost.
	var homeCourseName *string
	if row.HomeCourseID != nil && *row.HomeCourseID != "" {
		if courseRow, err := s.db.Queries.GetCourse(ctx, *row.HomeCourseID); err == nil {
			name := courseRow.Name
			homeCourseName = &name
		}
		// A missing course is not an error worth failing a session check over.
		// The FK is ON DELETE SET NULL, so this only happens in the window
		// between a delete and the next read.
	}

	return &User{
		ID:              row.ID,
		Email:           row.Email,
		DisplayName:     row.DisplayName,
		EmailVerified:   row.EmailVerified != 0,
		HasPassword:     row.PasswordHash != nil && *row.PasswordHash != "",
		Providers:       providers,
		DistanceUnit:    unit,
		FirstName:       row.FirstName,
		LastName:        row.LastName,
		AvatarURL:       AvatarURL(row.AvatarKey),
		HomeCourseID:    row.HomeCourseID,
		HomeCourseName:  homeCourseName,
		LocationCity:    row.LocationCity,
		LocationRegion:  row.LocationRegion,
		LocationCountry: row.LocationCountry,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
		IsAdmin:         s.isAdminEmail(row.Email),
	}, nil
}

// AvatarURL builds the public path for an avatar key, or nil when the user has
// no avatar. The key is unguessable and rotates on every upload, which is what
// lets the served image be cached indefinitely.
func AvatarURL(key *string) *string {
	if key == nil || *key == "" {
		return nil
	}
	url := "/api/avatars/" + *key + ".jpg"
	return &url
}

// SetDistanceUnit changes the unit the caller reads and enters distances in.
// Nothing stored is rewritten — the database stays in yards.
func (s *Service) SetDistanceUnit(ctx context.Context, userID, unit string) (*User, error) {
	if !slices.Contains(DistanceUnits, unit) {
		return nil, httpx.ValidationError(map[string]string{
			"distance_unit": "Choose either yards or meters.",
		})
	}

	if err := s.db.Queries.UpdateUserDistanceUnit(ctx, sqlc.UpdateUserDistanceUnitParams{
		DistanceUnit: unit,
		UpdatedAt:    timex.Now(),
		ID:           userID,
	}); err != nil {
		return nil, httpx.Internal(fmt.Errorf("update distance unit: %w", err))
	}

	row, err := s.db.Queries.GetUserByID(ctx, userID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("reload user: %w", err))
	}
	return s.toUser(ctx, row)
}

// SignUp creates an account from an email and password.
func (s *Service) SignUp(ctx context.Context, email, password, displayName string) (*Session, error) {
	email = httpx.NormalizeEmail(email)
	displayName = strings.TrimSpace(displayName)

	if _, err := s.db.Queries.GetUserByEmail(ctx, email); err == nil {
		return nil, httpx.ValidationError(map[string]string{
			"email": "An account with this email already exists.",
		})
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.Internal(fmt.Errorf("lookup user by email: %w", err))
	}

	hash, err := HashPassword(password)
	if err != nil {
		return nil, httpx.Internal(err)
	}

	now := timex.Now()
	row := sqlc.CreateUserParams{
		ID:            id.New(),
		Email:         email,
		PasswordHash:  &hash,
		DisplayName:   displayName,
		EmailVerified: 0,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.db.Queries.CreateUser(ctx, row); err != nil {
		if isUniqueViolation(err) {
			return nil, httpx.ValidationError(map[string]string{
				"email": "An account with this email already exists.",
			})
		}
		return nil, httpx.Internal(fmt.Errorf("create user: %w", err))
	}

	user, err := s.db.Queries.GetUserByID(ctx, row.ID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("reload created user: %w", err))
	}
	return s.issueSession(ctx, user)
}

// LogIn authenticates an email and password.
func (s *Service) LogIn(ctx context.Context, email, password string) (*Session, error) {
	email = httpx.NormalizeEmail(email)

	user, err := s.db.Queries.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Hash anyway so a missing account takes as long as a wrong password.
			_ = VerifyPassword(password, dummyHash)
			return nil, errInvalidCredentials()
		}
		return nil, httpx.Internal(fmt.Errorf("lookup user by email: %w", err))
	}

	if user.PasswordHash == nil || *user.PasswordHash == "" {
		_ = VerifyPassword(password, dummyHash)
		return nil, httpx.Unauthorized(
			"This account signs in with Google. Use \"Continue with Google\", then set a password from account settings if you want one.")
	}

	if err := VerifyPassword(password, *user.PasswordHash); err != nil {
		if errors.Is(err, ErrMismatchedPassword) {
			return nil, errInvalidCredentials()
		}
		return nil, httpx.Internal(fmt.Errorf("verify password for user %s: %w", user.ID, err))
	}

	return s.issueSession(ctx, user)
}

// Refresh rotates a refresh token, returning a new session.
//
// Rotation is single-use: the presented token is revoked as the replacement is
// created, both inside one transaction.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (*Session, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil, httpx.Unauthorized("A refresh token is required.")
	}

	stored, err := s.db.Queries.GetRefreshTokenByHash(ctx, HashRefreshToken(refreshToken))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errInvalidRefreshToken()
		}
		return nil, httpx.Internal(fmt.Errorf("lookup refresh token: %w", err))
	}

	if stored.RevokedAt != nil {
		// A revoked token being replayed means it leaked or the client raced.
		// Dropping every session for the user is the safe response.
		if err := s.revokeAllForUser(ctx, stored.UserID); err != nil {
			return nil, httpx.Internal(err)
		}
		return nil, errInvalidRefreshToken()
	}
	if timex.Expired(stored.ExpiresAt) {
		return nil, errInvalidRefreshToken()
	}

	user, err := s.db.Queries.GetUserByID(ctx, stored.UserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errInvalidRefreshToken()
		}
		return nil, httpx.Internal(fmt.Errorf("load refresh token user: %w", err))
	}

	newToken, newHash, expiresAt, err := s.tokens.NewRefreshToken()
	if err != nil {
		return nil, httpx.Internal(err)
	}

	now := timex.Now()
	err = s.db.InTx(func(q *sqlc.Queries) error {
		if err := q.RevokeRefreshToken(ctx, sqlc.RevokeRefreshTokenParams{
			RevokedAt: &now,
			TokenHash: stored.TokenHash,
		}); err != nil {
			return fmt.Errorf("revoke rotated token: %w", err)
		}
		return q.CreateRefreshToken(ctx, sqlc.CreateRefreshTokenParams{
			ID:        id.New(),
			UserID:    user.ID,
			TokenHash: newHash,
			ExpiresAt: timex.Format(expiresAt),
			CreatedAt: now,
		})
	})
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("rotate refresh token: %w", err))
	}

	accessToken, accessExpiry, err := s.tokens.IssueAccessToken(user.ID, user.Email)
	if err != nil {
		return nil, httpx.Internal(err)
	}
	apiUser, err := s.toUser(ctx, user)
	if err != nil {
		return nil, httpx.Internal(err)
	}

	return &Session{
		AccessToken:  accessToken,
		RefreshToken: newToken,
		ExpiresIn:    int(time.Until(accessExpiry).Seconds()),
		TokenType:    "Bearer",
		User:         apiUser,
	}, nil
}

// LogOut revokes a refresh token. It is idempotent: an unknown or already
// revoked token still reports success, since the caller's intent is satisfied.
func (s *Service) LogOut(ctx context.Context, refreshToken string) error {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil
	}
	now := timex.Now()
	err := s.db.Queries.RevokeRefreshToken(ctx, sqlc.RevokeRefreshTokenParams{
		RevokedAt: &now,
		TokenHash: HashRefreshToken(refreshToken),
	})
	if err != nil {
		return httpx.Internal(fmt.Errorf("revoke refresh token: %w", err))
	}
	return nil
}

// CurrentUser loads the authenticated user.
func (s *Service) CurrentUser(ctx context.Context, userID string) (*User, error) {
	row, err := s.db.Queries.GetUserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, httpx.Unauthorized("Your session is no longer valid. Please sign in again.")
		}
		return nil, httpx.Internal(fmt.Errorf("load current user: %w", err))
	}
	user, err := s.toUser(ctx, row)
	if err != nil {
		return nil, httpx.Internal(err)
	}
	return user, nil
}

// CompleteGoogleLogin turns a verified Google identity into a session, creating
// or linking the account as needed.
//
// Three cases, in priority order:
//  1. The provider subject is already linked, so that user signs in.
//  2. The email matches an existing account, so Google is linked to it. This is
//     what keeps "password signup then Google login" from making two accounts.
//  3. Neither matches, so a new password-less account is created.
func (s *Service) CompleteGoogleLogin(ctx context.Context, identity *GoogleIdentity) (*Session, error) {
	linked, err := s.db.Queries.GetOAuthAccountByProviderSubject(ctx, sqlc.GetOAuthAccountByProviderSubjectParams{
		Provider:        ProviderGoogle,
		ProviderSubject: identity.Subject,
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.Internal(fmt.Errorf("lookup oauth account: %w", err))
	}
	if err == nil {
		user, err := s.db.Queries.GetUserByID(ctx, linked.UserID)
		if err != nil {
			return nil, httpx.Internal(fmt.Errorf("load linked user: %w", err))
		}
		return s.issueSession(ctx, user)
	}

	email := httpx.NormalizeEmail(identity.Email)
	if email == "" {
		return nil, httpx.BadRequest("Google did not share an email address for this account.")
	}

	existing, err := s.db.Queries.GetUserByEmail(ctx, email)
	switch {
	case err == nil:
		// Only auto-link on a provider-verified email. Without that check,
		// anyone able to set an unverified address at the provider could claim
		// an existing account.
		if !identity.EmailVerified {
			return nil, httpx.Conflict(
				"An account already uses this email. Sign in with your password, then link Google from account settings.")
		}
		if err := s.linkProvider(ctx, existing.ID, identity); err != nil {
			return nil, err
		}
		return s.issueSession(ctx, existing)

	case errors.Is(err, sql.ErrNoRows):
		return s.createUserFromGoogle(ctx, email, identity)

	default:
		return nil, httpx.Internal(fmt.Errorf("lookup user by email: %w", err))
	}
}

func (s *Service) createUserFromGoogle(ctx context.Context, email string, identity *GoogleIdentity) (*Session, error) {
	displayName := strings.TrimSpace(identity.Name)
	if displayName == "" {
		displayName = displayNameFromEmail(email)
	}

	newUserID := id.New()
	now := timex.Now()
	verified := int64(0)
	if identity.EmailVerified {
		verified = 1
	}

	err := s.db.InTx(func(q *sqlc.Queries) error {
		if err := q.CreateUser(ctx, sqlc.CreateUserParams{
			ID:            newUserID,
			Email:         email,
			PasswordHash:  nil,
			DisplayName:   displayName,
			EmailVerified: verified,
			CreatedAt:     now,
			UpdatedAt:     now,
		}); err != nil {
			return fmt.Errorf("create user: %w", err)
		}
		return q.CreateOAuthAccount(ctx, sqlc.CreateOAuthAccountParams{
			ID:              id.New(),
			UserID:          newUserID,
			Provider:        ProviderGoogle,
			ProviderSubject: identity.Subject,
			ProviderEmail:   &email,
			CreatedAt:       now,
		})
	})
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("create google user: %w", err))
	}

	user, err := s.db.Queries.GetUserByID(ctx, newUserID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("reload google user: %w", err))
	}
	return s.issueSession(ctx, user)
}

// LinkGoogle attaches a Google identity to an already authenticated account.
func (s *Service) LinkGoogle(ctx context.Context, userID string, identity *GoogleIdentity) (*User, error) {
	existing, err := s.db.Queries.GetOAuthAccountByProviderSubject(ctx, sqlc.GetOAuthAccountByProviderSubjectParams{
		Provider:        ProviderGoogle,
		ProviderSubject: identity.Subject,
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.Internal(fmt.Errorf("lookup oauth account: %w", err))
	}
	if err == nil {
		if existing.UserID != userID {
			return nil, httpx.Conflict("This Google account is already linked to a different Roundly account.")
		}
		return s.CurrentUser(ctx, userID)
	}

	if err := s.linkProvider(ctx, userID, identity); err != nil {
		return nil, err
	}
	return s.CurrentUser(ctx, userID)
}

func (s *Service) linkProvider(ctx context.Context, userID string, identity *GoogleIdentity) error {
	providerEmail := httpx.NormalizeEmail(identity.Email)
	err := s.db.Queries.CreateOAuthAccount(ctx, sqlc.CreateOAuthAccountParams{
		ID:              id.New(),
		UserID:          userID,
		Provider:        ProviderGoogle,
		ProviderSubject: identity.Subject,
		ProviderEmail:   &providerEmail,
		CreatedAt:       timex.Now(),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return httpx.Conflict("This Google account is already linked to a Roundly account.")
		}
		return httpx.Internal(fmt.Errorf("link oauth account: %w", err))
	}
	return nil
}

// SetPassword gives a password to an OAuth-only account, or changes an existing
// one. Changing a password requires the current password.
func (s *Service) SetPassword(ctx context.Context, userID, currentPassword, newPassword string) error {
	row, err := s.db.Queries.GetUserByID(ctx, userID)
	if err != nil {
		return httpx.Internal(fmt.Errorf("load user: %w", err))
	}

	if row.PasswordHash != nil && *row.PasswordHash != "" {
		if currentPassword == "" {
			return httpx.ValidationError(map[string]string{
				"current_password": "Enter your current password.",
			})
		}
		if err := VerifyPassword(currentPassword, *row.PasswordHash); err != nil {
			return httpx.ValidationError(map[string]string{
				"current_password": "That password is incorrect.",
			})
		}
	}

	hash, err := HashPassword(newPassword)
	if err != nil {
		return httpx.Internal(err)
	}
	if err := s.db.Queries.SetUserPasswordHash(ctx, sqlc.SetUserPasswordHashParams{
		PasswordHash: &hash,
		UpdatedAt:    timex.Now(),
		ID:           userID,
	}); err != nil {
		return httpx.Internal(fmt.Errorf("set password hash: %w", err))
	}
	return nil
}

// issueSession mints an access token and a fresh refresh token for a user.
func (s *Service) issueSession(ctx context.Context, row sqlc.User) (*Session, error) {
	accessToken, accessExpiry, err := s.tokens.IssueAccessToken(row.ID, row.Email)
	if err != nil {
		return nil, httpx.Internal(err)
	}

	refreshToken, refreshHash, refreshExpiry, err := s.tokens.NewRefreshToken()
	if err != nil {
		return nil, httpx.Internal(err)
	}

	if err := s.db.Queries.CreateRefreshToken(ctx, sqlc.CreateRefreshTokenParams{
		ID:        id.New(),
		UserID:    row.ID,
		TokenHash: refreshHash,
		ExpiresAt: timex.Format(refreshExpiry),
		CreatedAt: timex.Now(),
	}); err != nil {
		return nil, httpx.Internal(fmt.Errorf("persist refresh token: %w", err))
	}

	user, err := s.toUser(ctx, row)
	if err != nil {
		return nil, httpx.Internal(err)
	}

	return &Session{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int(time.Until(accessExpiry).Seconds()),
		TokenType:    "Bearer",
		User:         user,
	}, nil
}

func (s *Service) revokeAllForUser(ctx context.Context, userID string) error {
	now := timex.Now()
	if err := s.db.Queries.RevokeAllUserRefreshTokens(ctx, sqlc.RevokeAllUserRefreshTokensParams{
		RevokedAt: &now,
		UserID:    userID,
	}); err != nil {
		return fmt.Errorf("revoke all refresh tokens for user %s: %w", userID, err)
	}
	return nil
}

// PurgeExpiredTokens deletes refresh tokens past their expiry.
func (s *Service) PurgeExpiredTokens(ctx context.Context) error {
	return s.db.Queries.DeleteExpiredRefreshTokens(ctx, timex.Now())
}

// RevokeAllSessions signs a user out everywhere. Exported for the account
// package, which has to do this when an email address changes: the address is
// the login identity, and changing it is exactly what someone holding a stolen
// token would do first.
func (s *Service) RevokeAllSessions(ctx context.Context, userID string) error {
	if err := s.revokeAllForUser(ctx, userID); err != nil {
		return httpx.Internal(err)
	}
	return nil
}

// IssueSessionFor mints a fresh session for an already-authenticated user.
//
// Exported so that an endpoint which invalidates the current access token — by
// changing the email in its claims — can hand back a working replacement in the
// same response, rather than leaving the client to discover it is signed out.
func (s *Service) IssueSessionFor(ctx context.Context, userID string) (*Session, error) {
	row, err := s.db.Queries.GetUserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, httpx.Unauthorized("Your session has expired. Please sign in again.")
		}
		return nil, httpx.Internal(fmt.Errorf("load user: %w", err))
	}
	return s.issueSession(ctx, row)
}

// IsUniqueViolation reports whether err is a UNIQUE constraint failure.
//
// Exported for internal/account, which races the same email-uniqueness check
// that SignUp does. Detecting it from the driver's error string is the one
// SQLite-specific behaviour in the codebase; keeping it in this one function is
// what makes the Phase 8 Postgres migration a single edit.
func IsUniqueViolation(err error) bool { return isUniqueViolation(err) }

func errInvalidCredentials() error {
	return httpx.Unauthorized("That email and password combination is not correct.")
}

func errInvalidRefreshToken() error {
	return httpx.Unauthorized("Your session has expired. Please sign in again.")
}

func displayNameFromEmail(email string) string {
	local, _, found := strings.Cut(email, "@")
	if !found || local == "" {
		return "Golfer"
	}
	return local
}

// isUniqueViolation reports whether err is a UNIQUE constraint failure. The
// SQLite driver surfaces this as a message rather than a typed error, so the
// check is textual; it stays confined to this helper for the Postgres move.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint failed") ||
		strings.Contains(msg, "duplicate key value")
}
