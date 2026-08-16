package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/edingc/roundly/internal/database/sqlc"
	"github.com/edingc/roundly/internal/httpx"
	"github.com/edingc/roundly/internal/timex"
)

// Email two-factor authentication.
//
// The shape of it, and why:
//
//   - Opt-in, not mandatory. Turning it on makes this account's sign-in depend
//     on somebody else's mail server staying up. That is a real cost and it is
//     the account holder's to accept, not this file's to impose. An operator who
//     wants it for everyone has the stricter lever already: they are the one who
//     decides whether mail is configured at all.
//
//   - Password path only. A Google sign-in has been through Google's own second
//     factor before it ever reaches this server. Mailing a code after that adds
//     no security and a great deal of friction, and dressing it up as thorough
//     would be theatre.
//
//   - Remembered devices. A code on every sign-in is the version people switch
//     off, and a feature switched off protects nobody. Trust is per-browser,
//     expires in thirty days, and is dropped the moment the credentials behind
//     it change.
//
// What it protects against is precisely one thing, and it is the common thing:
// somebody else has the password. It does not help if they have the mailbox,
// because the mailbox is also the recovery channel — email two-factor raises
// the floor, it does not change what the account ultimately rests on.

// TwoFactorChallenge is what a login returns instead of a session when a code
// is needed.
type TwoFactorChallenge struct {
	// TwoFactorRequired is always true and exists so the client can tell the two
	// possible shapes of a login response apart by looking at one field.
	TwoFactorRequired bool   `json:"two_factor_required"`
	ChallengeID       string `json:"challenge_id"`
	ExpiresIn         int    `json:"expires_in"`
}

// LoginResult is either a finished session or a challenge standing between the
// caller and one. Exactly one field is set.
type LoginResult struct {
	Session   *Session
	Challenge *TwoFactorChallenge
}

// TwoFactorEnabled reports whether this account demands a mailed code.
//
// Reads the row rather than a cached flag, and returns false whenever mail is
// unavailable: a column left set on an instance that has since lost its mail
// configuration must not lock its owner out.
func (s *Service) twoFactorEnabled(row sqlc.User) bool {
	return s.MailEnabled() && row.TwoFactorEmail != 0
}

// startTwoFactor issues a code, mails it, and returns the challenge.
func (s *Service) startTwoFactor(ctx context.Context, row sqlc.User) (*TwoFactorChallenge, error) {
	mailer, err := mailerOrError(s)
	if err != nil {
		return nil, err
	}

	code, err := newLoginCode()
	if err != nil {
		return nil, httpx.Internal(err)
	}
	challengeID, err := s.issueChallenge(ctx, row.ID, row.Email, PurposeLogin, code, loginCodeTTL)
	if err != nil {
		return nil, err
	}

	if err := mailer.Send(ctx, loginCodeEmail(row.Email, row.DisplayName, code)); err != nil {
		slog.Error("send sign-in code", "email", row.Email, "error", err)
		// Reported rather than swallowed. A login that silently stops at a code
		// which was never sent is a login the user cannot complete and cannot
		// diagnose.
		return nil, httpx.Internal(fmt.Errorf("send sign-in code: %w", err))
	}

	return &TwoFactorChallenge{
		TwoFactorRequired: true,
		ChallengeID:       challengeID,
		ExpiresIn:         int(loginCodeTTL.Seconds()),
	}, nil
}

// CompleteTwoFactor exchanges a challenge and its code for a session.
//
// remember mints a device token the client sends on later sign-ins to skip the
// code; label is a description of the browser, shown only back to its owner.
func (s *Service) CompleteTwoFactor(ctx context.Context, challengeID, code string, remember bool, label string) (*Session, error) {
	challenge, err := s.redeemChallenge(ctx, challengeID, normalizeCode(code), PurposeLogin)
	if err != nil {
		return nil, err
	}

	row, err := s.db.Queries.GetUserByID(ctx, challenge.UserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errChallengeFailed()
		}
		return nil, httpx.Internal(fmt.Errorf("load user: %w", err))
	}

	// The address is re-checked against the challenge for the same reason
	// verification does it: a code mailed to the old address must not open an
	// account that now answers to a new one.
	if !equalEmail(row.Email, challenge.Email) {
		return nil, errChallengeFailed()
	}

	session, err := s.issueSession(ctx, row)
	if err != nil {
		return nil, err
	}

	if remember {
		token, err := s.rememberDevice(ctx, row.ID, label)
		if err != nil {
			// The session is already good. Failing the whole login because the
			// convenience half did not work would be the wrong trade.
			slog.Warn("remember device", "user_id", row.ID, "error", err)
		} else {
			session.DeviceToken = &token
		}
	}
	return session, nil
}

// TwoFactorSetup is what turning two-factor on returns.
//
// RecoveryCodes are present exactly once, here, and only when enabling. They
// are not stored in the clear and cannot be shown again — the client has to put
// them in front of the user at this moment or not at all.
type TwoFactorSetup struct {
	User          *User    `json:"user"`
	RecoveryCodes []string `json:"recovery_codes,omitempty"`
}

// SetTwoFactor turns email two-factor on or off for the caller.
//
// Both directions demand the current password. Turning it on is a change to how
// this account is entered, and turning it off removes a protection — the second
// is the one an attacker holding a live session would want, so it is guarded at
// least as tightly as the first.
func (s *Service) SetTwoFactor(ctx context.Context, userID, password string, enabled bool) (*TwoFactorSetup, error) {
	row, err := s.db.Queries.GetUserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, httpx.Unauthorized("Your session has expired. Please sign in again.")
		}
		return nil, httpx.Internal(fmt.Errorf("load user: %w", err))
	}

	if row.PasswordHash == nil || *row.PasswordHash == "" {
		// A Google-only account has no password login for this to guard, so
		// there is nothing here to switch on.
		return nil, httpx.BadRequest(
			"This account signs in with Google, which already has its own second factor. Set a password first if you want email codes as well.")
	}
	if err := s.requirePassword(row, password); err != nil {
		return nil, err
	}

	if enabled {
		if !s.MailEnabled() {
			return nil, httpx.BadRequest(
				"This server cannot send email, so sign-in codes cannot be turned on.")
		}
		// Requiring a confirmed address first is what stops somebody arming a
		// second factor that fires at a mailbox they cannot read. That is not a
		// hypothetical: it is a self-inflicted permanent lockout.
		if row.EmailVerified == 0 {
			return nil, httpx.BadRequest(
				"Confirm your email address first. Sign-in codes would otherwise go somewhere you have not proven you can read.")
		}
	}

	flag := int64(0)
	if enabled {
		flag = 1
	}
	if err := s.db.Queries.SetUserTwoFactorEmail(ctx, sqlc.SetUserTwoFactorEmailParams{
		TwoFactorEmail: flag,
		UpdatedAt:      timex.Now(),
		ID:             userID,
	}); err != nil {
		return nil, httpx.Internal(fmt.Errorf("set two factor: %w", err))
	}

	// Either direction clears remembered devices. Switching off makes them
	// meaningless; switching on must not inherit trust granted while there was
	// no second factor to skip.
	if err := s.ForgetAllDevices(ctx, userID); err != nil {
		return nil, err
	}

	// Recovery codes exist only alongside the thing they recover from. Turning
	// two-factor on mints a sheet in the same breath, because an account that
	// is protected for a week before anybody thinks about recovery is an
	// account that spends that week one mailbox outage from being lost.
	var codes []string
	if enabled {
		codes, err = s.regenerateRecoveryCodes(ctx, userID)
		if err != nil {
			return nil, err
		}
	} else if err := s.ClearRecoveryCodes(ctx, userID); err != nil {
		return nil, err
	}

	user, err := s.CurrentUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &TwoFactorSetup{User: user, RecoveryCodes: codes}, nil
}

// PurgeExpired deletes the credentials and challenges that have aged out.
//
// One sweep for all four, run on a timer by the server. None of it is required
// for correctness — every check tests the expiry itself — but a table that only
// grows is a table that eventually matters, and a spent sign-in code has no
// business outliving the ten minutes it was good for.
func (s *Service) PurgeExpired(ctx context.Context) error {
	now := timex.Now()
	if err := s.db.Queries.DeleteExpiredRefreshTokens(ctx, now); err != nil {
		return fmt.Errorf("purge refresh tokens: %w", err)
	}
	if err := s.db.Queries.DeleteExpiredEmailChallenges(ctx, now); err != nil {
		return fmt.Errorf("purge email challenges: %w", err)
	}
	if err := s.db.Queries.DeleteExpiredTrustedDevices(ctx, now); err != nil {
		return fmt.Errorf("purge trusted devices: %w", err)
	}
	return nil
}

// StartJanitor sweeps expired rows on a timer until stop is closed.
//
// Modelled on the API-key flusher next door, and started the same way, so the
// server has one story about background work rather than two.
func (s *Service) StartJanitor(stop <-chan struct{}) {
	const interval = time.Hour

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if err := s.PurgeExpired(context.Background()); err != nil {
					slog.Warn("purge expired auth rows", "error", err)
				}
			}
		}
	}()
}
