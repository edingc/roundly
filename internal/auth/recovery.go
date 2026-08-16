package auth

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"

	"github.com/edingc/roundly/internal/database/sqlc"
	"github.com/edingc/roundly/internal/httpx"
	"github.com/edingc/roundly/internal/id"
	"github.com/edingc/roundly/internal/timex"
)

const (
	// recoveryCodeCount is a sheet's worth: enough that using one or two in an
	// emergency does not immediately mean generating a new set, few enough that
	// somebody will actually write them down.
	recoveryCodeCount = 10

	// recoveryCodeChars is Crockford base32: the digits and letters minus I, L,
	// O, and U. The first three because they are unreadable next to 1 and 0 on
	// a printout, and U because excluding it is what keeps the alphabet from
	// spelling things.
	recoveryCodeChars = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

	// recoveryCodeLength is 10 characters, so 50 bits per code. Paired with
	// argon2id that is far past guessing, and it is short enough to read off
	// paper without losing your place.
	recoveryCodeLength = 10

	// recoveryCodeGroup is where the hyphen goes. Two groups of five, because
	// unbroken strings of ten characters are where transcription errors live.
	recoveryCodeGroup = 5
)

// newRecoveryCode returns one code, formatted for reading aloud.
func newRecoveryCode() (string, error) {
	limit := big.NewInt(int64(len(recoveryCodeChars)))
	buf := make([]byte, 0, recoveryCodeLength+1)
	for i := range recoveryCodeLength {
		if i > 0 && i%recoveryCodeGroup == 0 {
			buf = append(buf, '-')
		}
		// crypto/rand and a rejection-free draw from a power-of-two alphabet:
		// a biased code is one an attacker can predict rather than guess.
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("generate recovery code: %w", err)
		}
		buf = append(buf, recoveryCodeChars[n.Int64()])
	}
	return string(buf), nil
}

// normalizeRecoveryCode undoes what a person does to a code between the paper
// and the box.
//
// Case is folded, hyphens and spaces are dropped, and the Crockford
// substitutions are applied: O reads as 0, and I and L both read as 1. Somebody
// copying by hand will make exactly these mistakes, and refusing a correct code
// because it was written in lower case with the wrong kind of one would be a
// lockout caused by pedantry.
func normalizeRecoveryCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(code)) {
		switch r {
		case '-', ' ', '\t':
			continue
		case 'O':
			b.WriteRune('0')
		case 'I', 'L':
			b.WriteRune('1')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// storedRecoveryCode strips the formatting before hashing, so that the stored
// value matches whatever a user types back regardless of how they type it.
func storedRecoveryCode(code string) string { return normalizeRecoveryCode(code) }

// GenerateRecoveryCodes replaces the caller's codes with a fresh set and
// returns them in the clear.
//
// This is the only moment the plaintext exists. It is not stored, cannot be
// recovered, and is never mailed — mailing them would put the codes in the same
// mailbox they exist to survive the loss of.
//
// Requires the current password, because a live session is not the same as the
// person: someone holding a stolen session could otherwise mint themselves a
// permanent way back in that survives every password change.
func (s *Service) GenerateRecoveryCodes(ctx context.Context, userID, password string) ([]string, error) {
	row, err := s.db.Queries.GetUserByID(ctx, userID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("load user: %w", err))
	}
	if err := s.requirePassword(row, password); err != nil {
		return nil, err
	}
	return s.regenerateRecoveryCodes(ctx, userID)
}

// regenerateRecoveryCodes does the work without the password check, for the
// callers that have already made one — enabling two-factor, in particular.
func (s *Service) regenerateRecoveryCodes(ctx context.Context, userID string) ([]string, error) {
	codes := make([]string, 0, recoveryCodeCount)
	hashes := make([]string, 0, recoveryCodeCount)
	for range recoveryCodeCount {
		code, err := newRecoveryCode()
		if err != nil {
			return nil, httpx.Internal(err)
		}
		hash, err := HashPassword(storedRecoveryCode(code))
		if err != nil {
			return nil, httpx.Internal(err)
		}
		codes = append(codes, code)
		hashes = append(hashes, hash)
	}

	now := timex.Now()
	// One transaction: a half-replaced sheet would leave the user holding codes
	// that no longer work alongside ones that were never shown to them.
	if err := s.db.InTx(func(q *sqlc.Queries) error {
		if err := q.DeleteRecoveryCodes(ctx, userID); err != nil {
			return fmt.Errorf("clear recovery codes: %w", err)
		}
		for _, hash := range hashes {
			if err := q.CreateRecoveryCode(ctx, sqlc.CreateRecoveryCodeParams{
				ID:        id.New(),
				UserID:    userID,
				CodeHash:  hash,
				CreatedAt: now,
			}); err != nil {
				return fmt.Errorf("store recovery code: %w", err)
			}
		}
		return nil
	}); err != nil {
		return nil, httpx.Internal(err)
	}
	return codes, nil
}

// CountRecoveryCodes reports how many of the caller's codes are still unused,
// so the profile screen can prompt for a new sheet before they run out.
func (s *Service) CountRecoveryCodes(ctx context.Context, userID string) (int, error) {
	count, err := s.db.Queries.CountUnusedRecoveryCodes(ctx, userID)
	if err != nil {
		return 0, httpx.Internal(fmt.Errorf("count recovery codes: %w", err))
	}
	return int(count), nil
}

// ClearRecoveryCodes drops the whole set. Called when two-factor is switched
// off, since a recovery code with no second factor to recover from is a
// standing credential nobody remembers issuing.
func (s *Service) ClearRecoveryCodes(ctx context.Context, userID string) error {
	if err := s.db.Queries.DeleteRecoveryCodes(ctx, userID); err != nil {
		return httpx.Internal(fmt.Errorf("clear recovery codes: %w", err))
	}
	return nil
}

// RedeemRecoveryCode completes a two-factor login with a recovery code instead
// of a mailed one.
//
// Deliberately does not offer to remember the device. Somebody reaching for a
// recovery code has lost access to their email, which is a bad moment to also
// hand out a thirty-day pass — and the first thing they should do afterwards is
// fix the address, which drops remembered devices anyway.
func (s *Service) RedeemRecoveryCode(ctx context.Context, challengeID, code string) (*Session, error) {
	challenge, err := s.db.Queries.GetEmailChallenge(ctx, challengeID)
	if err != nil {
		return nil, errChallengeFailed()
	}

	// The challenge is validated and its attempt counted through the same path
	// a mailed code takes, so a recovery code cannot be used to get more than
	// five guesses, and cannot be tried against an expired or spent challenge.
	if err := s.chargeChallengeAttempt(ctx, challenge, PurposeLogin); err != nil {
		return nil, err
	}

	rows, err := s.db.Queries.ListUnusedRecoveryCodes(ctx, challenge.UserID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list recovery codes: %w", err))
	}

	normalized := normalizeRecoveryCode(code)
	matched := ""
	for _, candidate := range rows {
		// argon2id salts each hash, so there is nothing to look up by: every
		// unused code has to be tried. Ten at most, on an operation performed
		// once in the life of an account.
		if err := VerifyPassword(normalized, candidate.CodeHash); err == nil {
			matched = candidate.ID
			break
		}
	}
	if matched == "" {
		return nil, errChallengeFailed()
	}

	now := timex.Now()
	if err := s.db.Queries.ConsumeRecoveryCode(ctx, sqlc.ConsumeRecoveryCodeParams{
		ConsumedAt: &now,
		ID:         matched,
	}); err != nil {
		return nil, httpx.Internal(fmt.Errorf("consume recovery code: %w", err))
	}
	// The challenge goes with it: one code, one sign-in.
	if err := s.db.Queries.ConsumeEmailChallenge(ctx, sqlc.ConsumeEmailChallengeParams{
		ConsumedAt: &now,
		ID:         challenge.ID,
	}); err != nil {
		return nil, httpx.Internal(fmt.Errorf("consume challenge: %w", err))
	}

	user, err := s.db.Queries.GetUserByID(ctx, challenge.UserID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("load user: %w", err))
	}
	return s.issueSession(ctx, user)
}

// requirePassword checks a password the way every credential-changing endpoint
// in this package does, with the same two refusals.
func (s *Service) requirePassword(row sqlc.User, password string) error {
	if row.PasswordHash == nil || *row.PasswordHash == "" {
		return httpx.BadRequest("This account has no password to confirm with.")
	}
	if err := VerifyPassword(password, *row.PasswordHash); err != nil {
		return httpx.ValidationError(map[string]string{
			"current_password": "That password is not correct.",
		})
	}
	return nil
}
