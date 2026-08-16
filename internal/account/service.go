package account

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/edingc/roundly/internal/auth"
	"github.com/edingc/roundly/internal/course"
	"github.com/edingc/roundly/internal/database"
	"github.com/edingc/roundly/internal/database/sqlc"
	"github.com/edingc/roundly/internal/httpx"
	"github.com/edingc/roundly/internal/timex"
)

// reauthWindow is how recently a password-less account must have signed in
// before it may change its email address or delete itself.
//
// An account with a password proves itself by typing that password. One created
// through Google has nothing to type, so without this an access token alone
// would be enough to move the account to an attacker's address — or erase it.
// Requiring a fresh token means the SPA sends the user back through Google,
// which is the same proof.
const reauthWindow = 5 * time.Minute

// Service implements the account use cases. Every method takes the caller's own
// user ID; nothing here reads or writes another user's row.
type Service struct {
	db      *database.DB
	auth    *auth.Service
	courses *course.Service
}

func NewService(db *database.DB, authService *auth.Service, courseService *course.Service) *Service {
	return &Service{db: db, auth: authService, courses: courseService}
}

// UpdateProfile writes the caller's profile fields and returns the whole user.
func (s *Service) UpdateProfile(ctx context.Context, userID string, in ProfileInput) (*auth.User, error) {
	// A home course must exist. It may be any course in the shared directory,
	// not only one the caller created — the directory is public-read, so
	// restricting it here would be a rule that exists nowhere else.
	if in.HomeCourseID != nil {
		if _, err := s.db.Queries.GetCourse(ctx, *in.HomeCourseID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, httpx.ValidationError(map[string]string{
					"home_course_id": "That course is not in the directory.",
				})
			}
			return nil, httpx.Internal(fmt.Errorf("load home course: %w", err))
		}
	}

	if err := s.db.Queries.UpdateUserProfile(ctx, sqlc.UpdateUserProfileParams{
		FirstName:       in.FirstName,
		LastName:        in.LastName,
		DisplayName:     in.DisplayName,
		HomeCourseID:    in.HomeCourseID,
		LocationCity:    in.LocationCity,
		LocationRegion:  in.LocationRegion,
		LocationCountry: in.LocationCountry,
		UpdatedAt:       timex.Now(),
		ID:              userID,
	}); err != nil {
		return nil, httpx.Internal(fmt.Errorf("update profile: %w", err))
	}

	return s.reload(ctx, userID)
}

// ChangeEmail moves the account to a new address.
//
// It returns a whole new Session rather than a User, for two reasons that both
// matter. The access token carries the old address in its claims, so it is
// stale the instant this succeeds; and every other device is signed out,
// because changing the login address is the first thing someone holding a
// stolen token would do.
func (s *Service) ChangeEmail(ctx context.Context, userID, email, currentPassword string, issuedAt time.Time) (*auth.Session, error) {
	row, err := s.db.Queries.GetUserByID(ctx, userID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("load user: %w", err))
	}

	// Saving the form without touching the address must not un-verify anyone
	// or sign their other devices out.
	if row.Email == email {
		return s.auth.IssueSessionFor(ctx, userID)
	}

	hasPassword := row.PasswordHash != nil && *row.PasswordHash != ""
	if hasPassword {
		if strings.TrimSpace(currentPassword) == "" {
			return nil, httpx.ValidationError(map[string]string{
				"current_password": "Enter your current password.",
			})
		}
		if err := auth.VerifyPassword(currentPassword, *row.PasswordHash); err != nil {
			return nil, httpx.ValidationError(map[string]string{
				"current_password": "That password is incorrect.",
			})
		}
	} else if time.Since(issuedAt) > reauthWindow {
		// No password to demand, so demand a recently proven session instead.
		return nil, &httpx.APIError{
			Status:  403,
			Code:    "reauthentication_required",
			Message: "Sign in again before changing your email address.",
		}
	}

	if existing, err := s.db.Queries.GetUserByEmail(ctx, email); err == nil && existing.ID != userID {
		return nil, httpx.ValidationError(map[string]string{
			"email": "An account with this email already exists.",
		})
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.Internal(fmt.Errorf("lookup user by email: %w", err))
	}

	if err := s.db.Queries.UpdateUserEmail(ctx, sqlc.UpdateUserEmailParams{
		Email:     email,
		UpdatedAt: timex.Now(),
		ID:        userID,
	}); err != nil {
		// Lost the race against a concurrent signup on the same address.
		if auth.IsUniqueViolation(err) {
			return nil, httpx.ValidationError(map[string]string{
				"email": "An account with this email already exists.",
			})
		}
		return nil, httpx.Internal(fmt.Errorf("update email: %w", err))
	}

	if err := s.auth.RevokeAllSessions(ctx, userID); err != nil {
		return nil, err
	}
	return s.auth.IssueSessionFor(ctx, userID)
}

// reload re-reads the user so callers return the stored truth rather than what
// they hoped they wrote.
func (s *Service) reload(ctx context.Context, userID string) (*auth.User, error) {
	user, err := s.auth.CurrentUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return user, nil
}

// DeleteAccount erases the caller's account.
//
// The reauthentication rules are exactly ChangeEmail's, and for the same
// reason: this is irreversible, and an unattended session should not be enough
// to trigger it. An account with a password types it; one created through
// Google has nothing to type, so it must instead have signed in recently.
//
// The deletion itself is a single statement. Clubs, API keys, OAuth links,
// refresh tokens, and the avatar all cascade, and the courses this person
// uploaded keep their rows with the attribution nulled — they are shared
// reference data that other players depend on, and nobody owned them anyway.
func (s *Service) DeleteAccount(ctx context.Context, userID, currentPassword string, issuedAt time.Time) error {
	row, err := s.db.Queries.GetUserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return httpx.Unauthorized("Your session is no longer valid.")
		}
		return httpx.Internal(fmt.Errorf("load user: %w", err))
	}

	hasPassword := row.PasswordHash != nil && *row.PasswordHash != ""
	if hasPassword {
		if strings.TrimSpace(currentPassword) == "" {
			return httpx.ValidationError(map[string]string{
				"current_password": "Enter your current password.",
			})
		}
		if err := auth.VerifyPassword(currentPassword, *row.PasswordHash); err != nil {
			return httpx.ValidationError(map[string]string{
				"current_password": "That password is incorrect.",
			})
		}
	} else if time.Since(issuedAt) > reauthWindow {
		return &httpx.APIError{
			Status:  http.StatusForbidden,
			Code:    "reauthentication_required",
			Message: "Sign in again before deleting your account.",
		}
	}

	if err := s.db.Queries.DeleteUser(ctx, userID); err != nil {
		return httpx.Internal(fmt.Errorf("delete user: %w", err))
	}

	slog.Info("account deleted", "user_id", userID)
	return nil
}
