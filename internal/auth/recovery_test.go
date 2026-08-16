package auth

import (
	"context"
	"strings"
	"testing"
)

// armTwoFactor turns two-factor on and returns the recovery sheet it minted.
func armTwoFactor(t *testing.T, svc *Service, userID string) []string {
	t.Helper()
	setup, err := svc.SetTwoFactor(context.Background(), userID, testAccountPassword, true)
	if err != nil {
		t.Fatalf("enable two-factor: %v", err)
	}
	return setup.RecoveryCodes
}

// A sheet has to arrive with the feature. An account protected for a week
// before anybody thinks about recovery spends that week one mailbox outage from
// being lost.
func TestEnablingTwoFactorMintsRecoveryCodes(t *testing.T) {
	svc, mailer := newMailedService(t)
	userID := signUpVerified(t, svc, mailer, "golfer@example.com")

	codes := armTwoFactor(t, svc, userID)
	if len(codes) != recoveryCodeCount {
		t.Fatalf("got %d codes, want %d", len(codes), recoveryCodeCount)
	}

	seen := make(map[string]bool, len(codes))
	for _, code := range codes {
		if seen[code] {
			t.Errorf("duplicate code %q in one sheet", code)
		}
		seen[code] = true
		if !strings.Contains(code, "-") {
			t.Errorf("code %q is not grouped for reading", code)
		}
		if len(normalizeRecoveryCode(code)) != recoveryCodeLength {
			t.Errorf("code %q normalises to the wrong length", code)
		}
	}

	user, err := svc.CurrentUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("load user: %v", err)
	}
	if user.RecoveryCodesRemaining != recoveryCodeCount {
		t.Errorf("remaining = %d, want %d", user.RecoveryCodesRemaining, recoveryCodeCount)
	}
}

// The point of the whole feature: the mailbox is gone and the account is not.
func TestRecoveryCodeSignsInWhenTheMailboxIsGone(t *testing.T) {
	svc, mailer := newMailedService(t)
	ctx := context.Background()
	userID := signUpVerified(t, svc, mailer, "golfer@example.com")
	codes := armTwoFactor(t, svc, userID)

	// The mail server is now unreachable, so no sign-in code can arrive.
	// The challenge is still issued, which is what the recovery code answers.
	result, err := svc.LogIn(ctx, "golfer@example.com", testAccountPassword, "")
	if err != nil {
		t.Fatalf("log in: %v", err)
	}
	if result.Challenge == nil {
		t.Fatal("expected a challenge")
	}

	session, err := svc.RedeemRecoveryCode(ctx, result.Challenge.ChallengeID, codes[0])
	if err != nil {
		t.Fatalf("redeem recovery code: %v", err)
	}
	if session.AccessToken == "" {
		t.Error("no access token after a valid recovery code")
	}
	// Never offered a device token: somebody reaching for a recovery code has
	// just lost access to their email, which is a bad moment to hand out a
	// thirty-day pass.
	if session.DeviceToken != nil {
		t.Error("a recovery sign-in handed out a trusted-device token")
	}
}

func TestRecoveryCodeIsSingleUse(t *testing.T) {
	svc, mailer := newMailedService(t)
	ctx := context.Background()
	userID := signUpVerified(t, svc, mailer, "golfer@example.com")
	codes := armTwoFactor(t, svc, userID)

	first, err := svc.LogIn(ctx, "golfer@example.com", testAccountPassword, "")
	if err != nil {
		t.Fatalf("log in: %v", err)
	}
	if _, err := svc.RedeemRecoveryCode(ctx, first.Challenge.ChallengeID, codes[0]); err != nil {
		t.Fatalf("first redemption: %v", err)
	}

	second, err := svc.LogIn(ctx, "golfer@example.com", testAccountPassword, "")
	if err != nil {
		t.Fatalf("second log in: %v", err)
	}
	if _, err := svc.RedeemRecoveryCode(ctx, second.Challenge.ChallengeID, codes[0]); err == nil {
		t.Error("err = nil, want the spent code refused")
	}

	// A different code from the same sheet still works.
	third, err := svc.LogIn(ctx, "golfer@example.com", testAccountPassword, "")
	if err != nil {
		t.Fatalf("third log in: %v", err)
	}
	if _, err := svc.RedeemRecoveryCode(ctx, third.Challenge.ChallengeID, codes[1]); err != nil {
		t.Errorf("a second unused code was refused: %v", err)
	}

	user, err := svc.CurrentUser(ctx, userID)
	if err != nil {
		t.Fatalf("load user: %v", err)
	}
	if user.RecoveryCodesRemaining != recoveryCodeCount-2 {
		t.Errorf("remaining = %d, want %d", user.RecoveryCodesRemaining, recoveryCodeCount-2)
	}
}

// Somebody copying ten characters off paper will get the case wrong, drop the
// hyphen, and write O for zero. Refusing a correct code over any of that would
// be a lockout caused by pedantry.
func TestRecoveryCodeToleratesHowPeopleTypeIt(t *testing.T) {
	svc, mailer := newMailedService(t)
	ctx := context.Background()
	userID := signUpVerified(t, svc, mailer, "golfer@example.com")
	codes := armTwoFactor(t, svc, userID)

	// Crockford substitutions, applied backwards: what the user might type
	// instead of what was printed.
	mangled := strings.ToLower(strings.ReplaceAll(codes[0], "-", " "))
	mangled = strings.ReplaceAll(mangled, "0", "o")
	mangled = strings.ReplaceAll(mangled, "1", "l")

	result, err := svc.LogIn(ctx, "golfer@example.com", testAccountPassword, "")
	if err != nil {
		t.Fatalf("log in: %v", err)
	}
	if _, err := svc.RedeemRecoveryCode(ctx, result.Challenge.ChallengeID, mangled); err != nil {
		t.Errorf("a correctly-transcribed code was refused as %q: %v", mangled, err)
	}
}

func TestRecoveryCodeRejectsAWrongOne(t *testing.T) {
	svc, mailer := newMailedService(t)
	ctx := context.Background()
	userID := signUpVerified(t, svc, mailer, "golfer@example.com")
	armTwoFactor(t, svc, userID)

	result, err := svc.LogIn(ctx, "golfer@example.com", testAccountPassword, "")
	if err != nil {
		t.Fatalf("log in: %v", err)
	}
	for name, code := range map[string]string{
		"empty":         "",
		"right shape":   "ABCDE-FGHJK",
		"a mailed code": "123456",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.RedeemRecoveryCode(ctx, result.Challenge.ChallengeID, code); err == nil {
				t.Error("err = nil, want the code refused")
			}
		})
	}
}

// "Sign in with a recovery code instead" must not be a way around the
// challenge's five-attempt cap.
func TestRecoveryCodeSharesTheChallengeAttemptCap(t *testing.T) {
	svc, mailer := newMailedService(t)
	ctx := context.Background()
	userID := signUpVerified(t, svc, mailer, "golfer@example.com")
	codes := armTwoFactor(t, svc, userID)

	result, err := svc.LogIn(ctx, "golfer@example.com", testAccountPassword, "")
	if err != nil {
		t.Fatalf("log in: %v", err)
	}
	challengeID := result.Challenge.ChallengeID

	for i := range maxCodeAttempts {
		if _, err := svc.RedeemRecoveryCode(ctx, challengeID, "ZZZZZ-ZZZZZ"); err == nil {
			t.Fatalf("guess %d was accepted", i+1)
		}
	}
	if _, err := svc.RedeemRecoveryCode(ctx, challengeID, codes[0]); err == nil {
		t.Error("a real code was accepted after the attempts ran out")
	}
}

func TestRegeneratingReplacesTheWholeSheet(t *testing.T) {
	svc, mailer := newMailedService(t)
	ctx := context.Background()
	userID := signUpVerified(t, svc, mailer, "golfer@example.com")
	original := armTwoFactor(t, svc, userID)

	if _, err := svc.GenerateRecoveryCodes(ctx, userID, "not-the-password"); err == nil {
		t.Error("err = nil, want the wrong password refused")
	}

	replacement, err := svc.GenerateRecoveryCodes(ctx, userID, testAccountPassword)
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if len(replacement) != recoveryCodeCount {
		t.Fatalf("got %d codes, want %d", len(replacement), recoveryCodeCount)
	}

	// A sheet that is partly old and partly new is one nobody can reason about,
	// so the old ones have to be dead.
	result, err := svc.LogIn(ctx, "golfer@example.com", testAccountPassword, "")
	if err != nil {
		t.Fatalf("log in: %v", err)
	}
	if _, err := svc.RedeemRecoveryCode(ctx, result.Challenge.ChallengeID, original[0]); err == nil {
		t.Error("a code from the replaced sheet still worked")
	}

	next, err := svc.LogIn(ctx, "golfer@example.com", testAccountPassword, "")
	if err != nil {
		t.Fatalf("log in again: %v", err)
	}
	if _, err := svc.RedeemRecoveryCode(ctx, next.Challenge.ChallengeID, replacement[0]); err != nil {
		t.Errorf("a code from the new sheet was refused: %v", err)
	}
}

// A recovery code with no second factor to recover from is a standing
// credential nobody remembers issuing.
func TestTurningTwoFactorOffDiscardsTheCodes(t *testing.T) {
	svc, mailer := newMailedService(t)
	ctx := context.Background()
	userID := signUpVerified(t, svc, mailer, "golfer@example.com")
	codes := armTwoFactor(t, svc, userID)

	if _, err := svc.SetTwoFactor(ctx, userID, testAccountPassword, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	remaining, err := svc.CountRecoveryCodes(ctx, userID)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if remaining != 0 {
		t.Errorf("remaining = %d, want 0 once two-factor is off", remaining)
	}

	// And turning it back on issues a fresh sheet rather than reviving the old.
	revived := armTwoFactor(t, svc, userID)
	for _, old := range codes {
		for _, current := range revived {
			if old == current {
				t.Fatal("a code from before survived being switched off and on")
			}
		}
	}
}

// The codes are stored the way passwords are, not the way sign-in codes are:
// they live until spent, which may be years, in a database that may leak.
func TestRecoveryCodesAreStoredHashed(t *testing.T) {
	svc, mailer := newMailedService(t)
	ctx := context.Background()
	userID := signUpVerified(t, svc, mailer, "golfer@example.com")
	codes := armTwoFactor(t, svc, userID)

	rows, err := svc.db.Queries.ListUnusedRecoveryCodes(ctx, userID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, row := range rows {
		if !strings.HasPrefix(row.CodeHash, "$argon2id$") {
			t.Errorf("code_hash = %q, want an argon2id hash", row.CodeHash)
		}
		for _, code := range codes {
			if strings.Contains(row.CodeHash, normalizeRecoveryCode(code)) {
				t.Error("a code appears in the clear inside its own hash")
			}
		}
	}
}
