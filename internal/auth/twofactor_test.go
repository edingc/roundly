package auth

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/edingc/roundly/internal/database"
	"github.com/edingc/roundly/internal/mail"
)

// fakeMailer keeps what was sent so a test can read the code out of it, the
// same way a person reads it out of their inbox.
type fakeMailer struct {
	mu   sync.Mutex
	sent []mail.Message
	// fail makes every send report a failure, for the paths that have to cope
	// with a mail server being down.
	fail error
}

func (m *fakeMailer) Send(_ context.Context, msg mail.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	m.sent = append(m.sent, msg)
	return nil
}

func (m *fakeMailer) Describe() string { return "fake" }

func (m *fakeMailer) last(t *testing.T) mail.Message {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sent) == 0 {
		t.Fatal("no mail was sent")
	}
	return m.sent[len(m.sent)-1]
}

func (m *fakeMailer) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sent)
}

var (
	codePattern  = regexp.MustCompile(`\b(\d{6})\b`)
	tokenPattern = regexp.MustCompile(`/verify-email\?token=([A-Za-z0-9_\-%]+)`)
)

// codeFrom pulls the six digits out of a sign-in email.
func codeFrom(t *testing.T, msg mail.Message) string {
	t.Helper()
	match := codePattern.FindStringSubmatch(msg.Text)
	if match == nil {
		t.Fatalf("no six-digit code in:\n%s", msg.Text)
	}
	return match[1]
}

// tokenFrom pulls the token out of a verification link.
func tokenFrom(t *testing.T, msg mail.Message) string {
	t.Helper()
	match := tokenPattern.FindStringSubmatch(msg.Text)
	if match == nil {
		t.Fatalf("no verification link in:\n%s", msg.Text)
	}
	return match[1]
}

// newMailedService builds a service that can send, which is what switches on
// both verification and two-factor.
func newMailedService(t *testing.T) (*Service, *fakeMailer) {
	t.Helper()

	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	mailer := &fakeMailer{}
	tokens := NewTokenIssuer([]byte("test-secret-key-of-sufficient-length"), 15*time.Minute, 24*time.Hour)
	svc := NewService(db, tokens, NewGoogleProvider("", "", ""), Options{
		PublicURL: "https://roundly.test",
		Mailer:    mailer,
	})
	return svc, mailer
}

// signUpVerified creates an account and takes it through the confirmation link,
// which is the state most of these tests start from.
func signUpVerified(t *testing.T, svc *Service, mailer *fakeMailer, email string) string {
	t.Helper()
	ctx := context.Background()

	session, err := svc.SignUp(ctx, email, testAccountPassword, "Test Golfer")
	if err != nil {
		t.Fatalf("sign up: %v", err)
	}
	if _, err := svc.VerifyEmail(ctx, tokenFrom(t, mailer.last(t))); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	return session.User.ID
}

const testAccountPassword = "a-good-enough-password"

// --- verification -------------------------------------------------------

func TestSignUpSendsAVerificationLinkAndStartsUnverified(t *testing.T) {
	svc, mailer := newMailedService(t)
	ctx := context.Background()

	session, err := svc.SignUp(ctx, "golfer@example.com", testAccountPassword, "Test Golfer")
	if err != nil {
		t.Fatalf("sign up: %v", err)
	}
	if session.User.EmailVerified {
		t.Error("a brand-new account is verified, want unverified until the link is opened")
	}
	if mailer.count() != 1 {
		t.Fatalf("mail sent = %d, want 1", mailer.count())
	}

	msg := mailer.last(t)
	if msg.To != "golfer@example.com" {
		t.Errorf("sent to %q, want the address that signed up", msg.To)
	}
	// The public URL is what the operator configured, not a relative path: a
	// link in an email has nowhere to be relative to.
	if !strings.Contains(msg.Text, "https://roundly.test/verify-email?token=") {
		t.Errorf("no absolute verification link in:\n%s", msg.Text)
	}
}

func TestVerifyEmailAcceptsTheLinkOnceAndOnlyOnce(t *testing.T) {
	svc, mailer := newMailedService(t)
	ctx := context.Background()

	if _, err := svc.SignUp(ctx, "golfer@example.com", testAccountPassword, "Test Golfer"); err != nil {
		t.Fatalf("sign up: %v", err)
	}
	token := tokenFrom(t, mailer.last(t))

	user, err := svc.VerifyEmail(ctx, token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !user.EmailVerified {
		t.Error("email_verified is still false after the link was opened")
	}

	// Single use: a link that keeps working is a link worth stealing.
	if _, err := svc.VerifyEmail(ctx, token); err == nil {
		t.Error("err = nil on replay, want the used link rejected")
	}
}

func TestVerifyEmailRejectsAnUnknownToken(t *testing.T) {
	svc, _ := newMailedService(t)
	for name, token := range map[string]string{
		"empty":     "",
		"made up":   "not-a-real-token",
		"plausible": strings.Repeat("A", 43),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.VerifyEmail(context.Background(), token); err == nil {
				t.Error("err = nil, want the token rejected")
			}
		})
	}
}

// A signup whose mail could not be sent still has to produce an account and a
// session, or a mail hiccup becomes a failed registration with no way back.
func TestSignUpSurvivesAMailFailure(t *testing.T) {
	svc, mailer := newMailedService(t)
	mailer.fail = context.DeadlineExceeded

	session, err := svc.SignUp(context.Background(), "golfer@example.com", testAccountPassword, "Test Golfer")
	if err != nil {
		t.Fatalf("sign up: %v", err)
	}
	if session.AccessToken == "" {
		t.Error("no access token, want a usable session despite the mail failure")
	}
}

// --- two-factor ---------------------------------------------------------

func TestLogInWithoutTwoFactorReturnsASession(t *testing.T) {
	svc, mailer := newMailedService(t)
	signUpVerified(t, svc, mailer, "golfer@example.com")

	result, err := svc.LogIn(context.Background(), "golfer@example.com", testAccountPassword, "")
	if err != nil {
		t.Fatalf("log in: %v", err)
	}
	if result.Session == nil || result.Challenge != nil {
		t.Fatalf("result = %+v, want a session and no challenge", result)
	}
}

func TestTwoFactorCannotBeEnabledWithoutAConfirmedAddress(t *testing.T) {
	svc, _ := newMailedService(t)
	ctx := context.Background()

	session, err := svc.SignUp(ctx, "golfer@example.com", testAccountPassword, "Test Golfer")
	if err != nil {
		t.Fatalf("sign up: %v", err)
	}

	// Arming a second factor aimed at an unproven mailbox is a lockout waiting
	// to happen, so it is refused rather than allowed with a warning.
	if _, err := svc.SetTwoFactor(ctx, session.User.ID, testAccountPassword, true); err == nil {
		t.Error("err = nil, want two-factor refused while the address is unconfirmed")
	}
}

func TestTwoFactorRequiresTheCurrentPassword(t *testing.T) {
	svc, mailer := newMailedService(t)
	userID := signUpVerified(t, svc, mailer, "golfer@example.com")

	if _, err := svc.SetTwoFactor(context.Background(), userID, "not-the-password", true); err == nil {
		t.Error("err = nil, want the wrong password refused")
	}
}

func TestTwoFactorLoginNeedsTheMailedCode(t *testing.T) {
	svc, mailer := newMailedService(t)
	ctx := context.Background()
	userID := signUpVerified(t, svc, mailer, "golfer@example.com")

	setup, err := svc.SetTwoFactor(ctx, userID, testAccountPassword, true)
	if err != nil {
		t.Fatalf("enable two-factor: %v", err)
	}
	if !setup.User.TwoFactorEmail {
		t.Fatal("two_factor_email is false after enabling")
	}

	result, err := svc.LogIn(ctx, "golfer@example.com", testAccountPassword, "")
	if err != nil {
		t.Fatalf("log in: %v", err)
	}
	if result.Session != nil {
		t.Fatal("a session was issued, want the login held for a code")
	}
	if result.Challenge == nil || result.Challenge.ChallengeID == "" {
		t.Fatalf("challenge = %+v, want one with an id", result.Challenge)
	}

	code := codeFrom(t, mailer.last(t))
	session, err := svc.CompleteTwoFactor(ctx, result.Challenge.ChallengeID, code, false, "Test Browser")
	if err != nil {
		t.Fatalf("complete two-factor: %v", err)
	}
	if session.AccessToken == "" {
		t.Error("no access token after the code was accepted")
	}
	// Not asked for, so not issued.
	if session.DeviceToken != nil {
		t.Error("a device token was returned without remember_device")
	}
}

func TestTwoFactorRejectsTheWrongCodeAndGivesUpAfterFiveTries(t *testing.T) {
	svc, mailer := newMailedService(t)
	ctx := context.Background()
	userID := signUpVerified(t, svc, mailer, "golfer@example.com")
	if _, err := svc.SetTwoFactor(ctx, userID, testAccountPassword, true); err != nil {
		t.Fatalf("enable two-factor: %v", err)
	}

	result, err := svc.LogIn(ctx, "golfer@example.com", testAccountPassword, "")
	if err != nil {
		t.Fatalf("log in: %v", err)
	}
	challengeID := result.Challenge.ChallengeID
	realCode := codeFrom(t, mailer.last(t))

	// A wrong guess costs an attempt whether or not it was close.
	wrong := "000000"
	if wrong == realCode {
		wrong = "111111"
	}
	for i := range maxCodeAttempts {
		if _, err := svc.CompleteTwoFactor(ctx, challengeID, wrong, false, ""); err == nil {
			t.Fatalf("guess %d was accepted, want it refused", i+1)
		}
	}

	// Out of attempts, so even the real code is dead. A challenge that could be
	// rescued by finally typing it right would make the counter decorative.
	if _, err := svc.CompleteTwoFactor(ctx, challengeID, realCode, false, ""); err == nil {
		t.Error("the correct code was accepted after the attempts ran out")
	}
}

func TestRememberedDeviceSkipsTheCodeNextTime(t *testing.T) {
	svc, mailer := newMailedService(t)
	ctx := context.Background()
	userID := signUpVerified(t, svc, mailer, "golfer@example.com")
	if _, err := svc.SetTwoFactor(ctx, userID, testAccountPassword, true); err != nil {
		t.Fatalf("enable two-factor: %v", err)
	}

	first, err := svc.LogIn(ctx, "golfer@example.com", testAccountPassword, "")
	if err != nil {
		t.Fatalf("log in: %v", err)
	}
	session, err := svc.CompleteTwoFactor(
		ctx, first.Challenge.ChallengeID, codeFrom(t, mailer.last(t)), true, "Test Browser")
	if err != nil {
		t.Fatalf("complete two-factor: %v", err)
	}
	if session.DeviceToken == nil {
		t.Fatal("device_token = nil, want one when remember_device was asked for")
	}
	deviceToken := *session.DeviceToken

	// The whole point: this browser is not asked again.
	second, err := svc.LogIn(ctx, "golfer@example.com", testAccountPassword, deviceToken)
	if err != nil {
		t.Fatalf("second log in: %v", err)
	}
	if second.Session == nil {
		t.Error("the remembered device was challenged again")
	}

	// And a different browser still is.
	third, err := svc.LogIn(ctx, "golfer@example.com", testAccountPassword, "")
	if err != nil {
		t.Fatalf("third log in: %v", err)
	}
	if third.Challenge == nil {
		t.Error("an unknown device was let straight in")
	}
}

// A device token from one account must not open another, even though it is a
// perfectly valid token.
func TestDeviceTokenIsBoundToItsAccount(t *testing.T) {
	svc, mailer := newMailedService(t)
	ctx := context.Background()

	victimID := signUpVerified(t, svc, mailer, "victim@example.com")
	if _, err := svc.SetTwoFactor(ctx, victimID, testAccountPassword, true); err != nil {
		t.Fatalf("enable two-factor: %v", err)
	}
	attackerID := signUpVerified(t, svc, mailer, "attacker@example.com")
	if _, err := svc.SetTwoFactor(ctx, attackerID, testAccountPassword, true); err != nil {
		t.Fatalf("enable two-factor: %v", err)
	}

	// The attacker legitimately remembers their own browser.
	start, err := svc.LogIn(ctx, "attacker@example.com", testAccountPassword, "")
	if err != nil {
		t.Fatalf("attacker log in: %v", err)
	}
	session, err := svc.CompleteTwoFactor(
		ctx, start.Challenge.ChallengeID, codeFrom(t, mailer.last(t)), true, "Attacker Browser")
	if err != nil {
		t.Fatalf("attacker two-factor: %v", err)
	}

	// Then tries it against the victim, whose password they are assumed to have.
	result, err := svc.LogIn(ctx, "victim@example.com", testAccountPassword, *session.DeviceToken)
	if err != nil {
		t.Fatalf("victim log in: %v", err)
	}
	if result.Challenge == nil {
		t.Error("another account's device token skipped the code")
	}
}

func TestChangingThePasswordForgetsEveryDevice(t *testing.T) {
	svc, mailer := newMailedService(t)
	ctx := context.Background()
	userID := signUpVerified(t, svc, mailer, "golfer@example.com")
	if _, err := svc.SetTwoFactor(ctx, userID, testAccountPassword, true); err != nil {
		t.Fatalf("enable two-factor: %v", err)
	}

	start, err := svc.LogIn(ctx, "golfer@example.com", testAccountPassword, "")
	if err != nil {
		t.Fatalf("log in: %v", err)
	}
	session, err := svc.CompleteTwoFactor(
		ctx, start.Challenge.ChallengeID, codeFrom(t, mailer.last(t)), true, "Test Browser")
	if err != nil {
		t.Fatalf("complete two-factor: %v", err)
	}
	deviceToken := *session.DeviceToken

	const newPassword = "an-entirely-new-password"
	if err := svc.SetPassword(ctx, userID, testAccountPassword, newPassword); err != nil {
		t.Fatalf("set password: %v", err)
	}

	// The usual reason to change a password is that somebody else has it, so
	// trust granted under the old one has to go with it.
	result, err := svc.LogIn(ctx, "golfer@example.com", newPassword, deviceToken)
	if err != nil {
		t.Fatalf("log in after password change: %v", err)
	}
	if result.Challenge == nil {
		t.Error("a device remembered under the old password was still trusted")
	}
}

func TestTurningTwoFactorOffStopsTheCodes(t *testing.T) {
	svc, mailer := newMailedService(t)
	ctx := context.Background()
	userID := signUpVerified(t, svc, mailer, "golfer@example.com")
	if _, err := svc.SetTwoFactor(ctx, userID, testAccountPassword, true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, err := svc.SetTwoFactor(ctx, userID, testAccountPassword, false); err != nil {
		t.Fatalf("disable: %v", err)
	}

	result, err := svc.LogIn(ctx, "golfer@example.com", testAccountPassword, "")
	if err != nil {
		t.Fatalf("log in: %v", err)
	}
	if result.Session == nil {
		t.Error("still challenged after two-factor was turned off")
	}
}

// With no mailer, nothing about the old behaviour changes: signup completes,
// login works, and two-factor cannot be armed.
func TestWithoutAMailerNothingIsDemanded(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	if svc.EmailVerificationRequired() {
		t.Error("verification is required on an instance that cannot send mail")
	}

	session, err := svc.SignUp(ctx, "golfer@example.com", testAccountPassword, "Test Golfer")
	if err != nil {
		t.Fatalf("sign up: %v", err)
	}
	if _, err := svc.SetTwoFactor(ctx, session.User.ID, testAccountPassword, true); err == nil {
		t.Error("err = nil, want two-factor refused with no way to send the code")
	}

	result, err := svc.LogIn(ctx, "golfer@example.com", testAccountPassword, "")
	if err != nil {
		t.Fatalf("log in: %v", err)
	}
	if result.Session == nil {
		t.Error("login was challenged on an instance with no mailer")
	}
}

// The resend button must not become a way to have this server mail somebody
// else fifty times.
func TestVerificationResendIsRateLimited(t *testing.T) {
	svc, mailer := newMailedService(t)
	ctx := context.Background()

	session, err := svc.SignUp(ctx, "golfer@example.com", testAccountPassword, "Test Golfer")
	if err != nil {
		t.Fatalf("sign up: %v", err)
	}

	// Signup already sent one, so the cap is reached partway through this loop.
	var lastErr error
	for range maxChallengesPerWindow + 2 {
		lastErr = svc.SendVerificationEmail(ctx, session.User.ID)
	}
	if lastErr == nil {
		t.Error("err = nil after exceeding the send cap, want it refused")
	}
	if mailer.count() > maxChallengesPerWindow {
		t.Errorf("sent %d messages, want no more than %d", mailer.count(), maxChallengesPerWindow)
	}
}
