package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/edingc/roundly/internal/database/sqlc"
	"github.com/edingc/roundly/internal/httpx"
	"github.com/edingc/roundly/internal/id"
	"github.com/edingc/roundly/internal/timex"
)

// The two things this app mails, and the only values the purpose column takes.
const (
	// PurposeVerifyEmail is the link sent once, when an address is first claimed
	// or changed.
	PurposeVerifyEmail = "verify_email"
	// PurposeLogin is the six-digit code sent when two-factor is on and the
	// sign-in is from a device this account has not seen.
	PurposeLogin = "login"
)

const (
	// verifyTokenBytes makes a link nobody can guess. It travels in a URL, so
	// there is no cost to making it long.
	verifyTokenBytes = 32

	// verifyTTL is generous on purpose: someone signs up on a phone, gets
	// interrupted, and opens the mail that evening. A day covers that. The link
	// is single-use, and a new one can always be asked for.
	verifyTTL = 24 * time.Hour

	// loginCodeTTL is short because a sign-in code is used within a minute or
	// abandoned. Ten minutes covers a slow mail server and a slow typist.
	loginCodeTTL = 10 * time.Minute

	// maxCodeAttempts is what actually protects six digits.
	//
	// A million possibilities sounds like a lot and is not: a script clears it
	// in minutes. Five guesses against a code that lives ten minutes puts the
	// odds of a blind hit at one in two hundred thousand per challenge, and a
	// sixth guess retires the challenge entirely rather than merely failing.
	maxCodeAttempts = 5

	// The send-side rate limit. Every one of these messages goes to an address
	// the requester has only claimed, so an unmetered "send it again" button is
	// a way to have this server deliver mail somebody else did not ask for.
	challengeSendWindow    = time.Hour
	maxChallengesPerWindow = 5
)

// errChallengeFailed is one message for every way of getting a code wrong:
// mistyped, expired, already used, out of attempts, or never issued.
//
// Distinguishing them would tell someone holding a stolen password which of
// their guesses was close, and tell a prober whether an account has two-factor
// on at all. The user who genuinely mistyped is told to ask for a new one,
// which is the correct advice in every one of those cases anyway.
func errChallengeFailed() error {
	return httpx.Unauthorized("That code is not valid or has expired. Request a new one.")
}

// hashChallengeCode stores what was sent without storing the secret itself.
//
// SHA-256 rather than argon2id, unlike passwords: these codes live minutes,
// are single-use, are rate-limited on both sides, and are not reused anywhere
// else. What a slow hash buys — resistance to offline cracking of a stolen
// database — is worth nothing against a secret that has already expired by the
// time the attacker has it, and it would cost a hash on every login.
func hashChallengeCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// newLoginCode returns six uniformly random digits.
//
// crypto/rand and a modulo-free draw: a code generated with math/rand, or with
// a biased reduction, is one an attacker can predict rather than guess.
func newLoginCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("generate login code: %w", err)
	}
	// Zero-padded: 000042 is a valid code, and dropping the leading zeros would
	// quietly shrink the space.
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func newVerifyToken() (string, error) {
	buf := make([]byte, verifyTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate verification token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// issueChallenge retires whatever was outstanding, records the new code, and
// returns the row id.
//
// Retiring first is the part that matters. Without it every "send it again"
// would widen the set of codes the server accepts, so a user who pressed the
// button four times would have four live codes and four times the chance of a
// blind guess landing.
func (s *Service) issueChallenge(ctx context.Context, userID, email, purpose, code string, ttl time.Duration) (string, error) {
	now := timex.Now()

	since := timex.Format(time.Now().UTC().Add(-challengeSendWindow))
	sent, err := s.db.Queries.CountRecentEmailChallenges(ctx, sqlc.CountRecentEmailChallengesParams{
		UserID:    userID,
		Purpose:   purpose,
		CreatedAt: since,
	})
	if err != nil {
		return "", httpx.Internal(fmt.Errorf("count recent challenges: %w", err))
	}
	if sent >= maxChallengesPerWindow {
		return "", httpx.TooManyRequests(
			"Too many messages have been sent to this address. Wait an hour and try again.")
	}

	challengeID := id.New()
	if err := s.db.InTx(func(q *sqlc.Queries) error {
		if err := q.ConsumeOutstandingEmailChallenges(ctx, sqlc.ConsumeOutstandingEmailChallengesParams{
			ConsumedAt: &now,
			UserID:     userID,
			Purpose:    purpose,
		}); err != nil {
			return fmt.Errorf("retire outstanding challenges: %w", err)
		}
		return q.CreateEmailChallenge(ctx, sqlc.CreateEmailChallengeParams{
			ID:        challengeID,
			UserID:    userID,
			Purpose:   purpose,
			Email:     email,
			CodeHash:  hashChallengeCode(code),
			ExpiresAt: timex.Format(time.Now().UTC().Add(ttl)),
			CreatedAt: now,
		})
	}); err != nil {
		return "", httpx.Internal(err)
	}
	return challengeID, nil
}

// redeemChallenge checks a code against a challenge and marks it used.
//
// Every failure returns the same error. The attempt counter is incremented
// before the comparison, so a caller that hangs up mid-request still pays for
// the guess.
func (s *Service) redeemChallenge(ctx context.Context, challengeID, code, purpose string) (*sqlc.EmailChallenge, error) {
	row, err := s.db.Queries.GetEmailChallenge(ctx, challengeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errChallengeFailed()
		}
		return nil, httpx.Internal(fmt.Errorf("load challenge: %w", err))
	}
	return s.redeemChallengeRow(ctx, row, code, purpose)
}

// chargeChallengeAttempt validates a challenge and spends one of its attempts.
//
// Split out from redeemChallengeRow so that a recovery code goes through
// exactly the same gate a mailed code does. Without that, "sign in with a
// recovery code instead" would be a way to get an unlimited number of guesses
// against a challenge that only allows five.
func (s *Service) chargeChallengeAttempt(ctx context.Context, row sqlc.EmailChallenge, purpose string) error {
	if row.Purpose != purpose || row.ConsumedAt != nil || row.Attempts >= maxCodeAttempts {
		return errChallengeFailed()
	}
	if expiry, err := timex.Parse(row.ExpiresAt); err != nil || !time.Now().UTC().Before(expiry) {
		return errChallengeFailed()
	}

	// Incremented before the answer is checked, so a caller that hangs up
	// mid-request still pays for the guess.
	if err := s.db.Queries.IncrementEmailChallengeAttempts(ctx, row.ID); err != nil {
		return httpx.Internal(fmt.Errorf("record challenge attempt: %w", err))
	}
	return nil
}

func (s *Service) redeemChallengeRow(ctx context.Context, row sqlc.EmailChallenge, code, purpose string) (*sqlc.EmailChallenge, error) {
	if err := s.chargeChallengeAttempt(ctx, row, purpose); err != nil {
		return nil, err
	}

	// Constant time: a six-digit comparison that returns early leaks how many
	// leading digits were right, which turns a million guesses into sixty.
	if subtle.ConstantTimeCompare([]byte(hashChallengeCode(code)), []byte(row.CodeHash)) != 1 {
		return nil, errChallengeFailed()
	}

	// Guarded on consumed_at IS NULL, so two requests racing the same code
	// cannot both come away thinking they redeemed it.
	now := timex.Now()
	if err := s.db.Queries.ConsumeEmailChallenge(ctx, sqlc.ConsumeEmailChallengeParams{
		ConsumedAt: &now,
		ID:         row.ID,
	}); err != nil {
		return nil, httpx.Internal(fmt.Errorf("consume challenge: %w", err))
	}
	return &row, nil
}

// normalizeCode strips what a person pastes around a code: spaces from a mail
// client's line wrapping, and the dash somebody types out of habit.
func normalizeCode(code string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, code)
}
