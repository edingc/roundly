package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/edingc/roundly/internal/database/sqlc"
	"github.com/edingc/roundly/internal/httpx"
	"github.com/edingc/roundly/internal/mail"
	"github.com/edingc/roundly/internal/timex"
)

// Email verification.
//
// The email_verified column has existed since the first migration and was a
// hardcoded zero the whole time, because nothing could send the message that
// would set it. This is that message.
//
// What verification is actually for here: the address is the login identity and
// the second factor, so an unverified one means an account whose recovery path
// points at a mailbox nobody has proven they can read. It also stops one person
// from claiming another's address and holding it.

// EmailVerificationRequired reports whether this instance makes people confirm
// their address before using the app.
//
// Tied to mail being configured, and to nothing else. An instance that cannot
// send a verification link must not demand one, or nobody could ever sign in —
// which is the failure mode of every "secure by default" flag that does not
// check whether the mechanism behind it exists.
func (s *Service) EmailVerificationRequired() bool { return s.MailEnabled() }

// SendVerificationEmail issues a fresh link and mails it.
//
// Safe to call repeatedly: each call retires the previous link, and the
// per-hour cap in issueChallenge is what stops the resend button from being a
// way to mail somebody a hundred times.
func (s *Service) SendVerificationEmail(ctx context.Context, userID string) error {
	mailer, err := mailerOrError(s)
	if err != nil {
		return err
	}

	row, err := s.db.Queries.GetUserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return httpx.Unauthorized("Your session has expired. Please sign in again.")
		}
		return httpx.Internal(fmt.Errorf("load user: %w", err))
	}
	if row.EmailVerified != 0 {
		return httpx.BadRequest("That address is already confirmed.")
	}

	token, err := newVerifyToken()
	if err != nil {
		return httpx.Internal(err)
	}
	if _, err := s.issueChallenge(ctx, row.ID, row.Email, PurposeVerifyEmail, token, verifyTTL); err != nil {
		return err
	}

	msg := verificationEmail(row.Email, row.DisplayName, s.verificationLink(token))
	if err := mailer.Send(ctx, msg); err != nil {
		// Logged with the address, because an operator debugging "the mail never
		// arrives" needs to know which one it was. The code itself is never
		// logged, here or anywhere.
		slog.Error("send verification email", "email", row.Email, "error", err)
		return httpx.Internal(fmt.Errorf("send verification email: %w", err))
	}
	return nil
}

// verificationLink is where the mailed link points: the SPA, not the API. The
// page there posts the token back, which keeps the whole flow inside the app
// rather than dumping the user on a bare JSON response.
func (s *Service) verificationLink(token string) string {
	base := s.publicURL
	if base == "" {
		base = "http://localhost:5173"
	}
	return base + "/verify-email?token=" + url.QueryEscape(token)
}

// VerifyEmail redeems a token from a verification link.
//
// Unauthenticated on purpose. The link is opened by whatever browser the mail
// client hands it to, which is routinely not the one that signed up, and
// demanding a session there would strand people on a login screen holding a
// single-use token.
func (s *Service) VerifyEmail(ctx context.Context, token string) (*User, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errChallengeFailed()
	}

	row, err := s.db.Queries.GetEmailChallengeByCodeHash(ctx, sqlc.GetEmailChallengeByCodeHashParams{
		CodeHash: hashChallengeCode(token),
		Purpose:  PurposeVerifyEmail,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errChallengeFailed()
		}
		return nil, httpx.Internal(fmt.Errorf("load verification challenge: %w", err))
	}

	if _, err := s.redeemChallengeRow(ctx, row, token, PurposeVerifyEmail); err != nil {
		return nil, err
	}

	user, err := s.db.Queries.GetUserByID(ctx, row.UserID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("load user: %w", err))
	}

	// The address is checked against the snapshot the challenge carries. Someone
	// who changed their email after the link was sent must not confirm the new
	// address with a link mailed to the old one — that is precisely the move
	// this feature exists to block.
	if !strings.EqualFold(user.Email, row.Email) {
		return nil, httpx.BadRequest(
			"That link was sent to a different address. Ask for a new one from your profile.")
	}

	if err := s.db.Queries.SetUserEmailVerified(ctx, sqlc.SetUserEmailVerifiedParams{
		EmailVerified: 1,
		UpdatedAt:     timex.Now(),
		ID:            user.ID,
	}); err != nil {
		return nil, httpx.Internal(fmt.Errorf("mark email verified: %w", err))
	}

	user.EmailVerified = 1
	result, err := s.toUser(ctx, user)
	if err != nil {
		return nil, httpx.Internal(err)
	}
	return result, nil
}

// mailerOrError turns the nil mailer into the refusal every caller would
// otherwise have to write.
//
// A 400 rather than a 500: an instance with no mail configured is not broken,
// it is one where this feature was never switched on, and the message says what
// to set.
func mailerOrError(s *Service) (mail.Mailer, error) {
	if s.mailer == nil {
		return nil, httpx.BadRequest(
			"This server cannot send email. Ask the administrator to configure SMTP_HOST or RESEND_API_KEY.")
	}
	return s.mailer, nil
}
