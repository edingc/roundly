package auth

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/edingc/roundly/internal/database"
)

func newTestService(t *testing.T) *Service {
	t.Helper()

	// A file in t.TempDir() rather than :memory:, because the pool is capped at
	// one connection and a file keeps behavior identical to production.
	dsn := filepath.Join(t.TempDir(), "test.db")
	db, err := database.Open(dsn)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	tokens := NewTokenIssuer([]byte("test-secret-key-of-sufficient-length"), 15*time.Minute, 24*time.Hour)
	return NewService(db, tokens, NewGoogleProvider("", "", ""))
}

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	if err := VerifyPassword("correct horse battery staple", hash); err != nil {
		t.Errorf("correct password rejected: %v", err)
	}

	if err := VerifyPassword("wrong password", hash); !errors.Is(err, ErrMismatchedPassword) {
		t.Errorf("wrong password: got %v, want ErrMismatchedPassword", err)
	}

	// A distinct salt per hash means the same password never encodes identically.
	other, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash password again: %v", err)
	}
	if other == hash {
		t.Error("two hashes of the same password are identical, so the salt is not random")
	}
}

func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	for name, hash := range map[string]string{
		"empty":          "",
		"not argon":      "$2a$10$abcdefghijklmnopqrstuv",
		"missing parts":  "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA",
		"bad base64":     "$argon2id$v=19$m=19456,t=2,p=1$!!!!$!!!!",
		"wrong version":  "$argon2id$v=13$m=19456,t=2,p=1$c2FsdHNhbHQ$aGFzaGhhc2g",
		"wrong function": "$argon2i$v=19$m=19456,t=2,p=1$c2FsdHNhbHQ$aGFzaGhhc2g",
	} {
		if err := VerifyPassword("anything", hash); !errors.Is(err, ErrInvalidHash) {
			t.Errorf("%s: got %v, want ErrInvalidHash", name, err)
		}
	}
}

func TestAccessTokenRoundTrip(t *testing.T) {
	issuer := NewTokenIssuer([]byte("test-secret-key-of-sufficient-length"), time.Minute, time.Hour)

	token, expiresAt, err := issuer.IssueAccessToken("user-123", "golfer@example.com")
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if !expiresAt.After(time.Now()) {
		t.Error("token expiry is not in the future")
	}

	claims, err := issuer.ParseAccessToken(token)
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	if claims.Subject != "user-123" {
		t.Errorf("subject = %q, want user-123", claims.Subject)
	}
	if claims.Email != "golfer@example.com" {
		t.Errorf("email = %q, want golfer@example.com", claims.Email)
	}
}

func TestAccessTokenRejectsTamperedAndForeignTokens(t *testing.T) {
	issuer := NewTokenIssuer([]byte("test-secret-key-of-sufficient-length"), time.Minute, time.Hour)
	token, _, err := issuer.IssueAccessToken("user-123", "golfer@example.com")
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	if _, err := issuer.ParseAccessToken(token + "x"); err == nil {
		t.Error("a token with a corrupted signature was accepted")
	}

	// A token signed with a different secret must not validate here. This is what
	// keeps one self-hosted instance's tokens from working on another.
	other := NewTokenIssuer([]byte("a-completely-different-secret-key-01"), time.Minute, time.Hour)
	if _, err := other.ParseAccessToken(token); err == nil {
		t.Error("a token signed with another key was accepted")
	}

	// The "alg":"none" family is rejected because the method is pinned to HS256.
	const unsigned = "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0." +
		"eyJzdWIiOiJhdHRhY2tlciIsImlzcyI6InJvdW5kbHkiLCJhdWQiOlsicm91bmRseS1hcGkiXSwiZXhwIjo5OTk5OTk5OTk5fQ."
	if _, err := issuer.ParseAccessToken(unsigned); err == nil {
		t.Error("an unsigned token was accepted")
	}
}

func TestExpiredAccessTokenIsRejected(t *testing.T) {
	issuer := NewTokenIssuer([]byte("test-secret-key-of-sufficient-length"), -time.Minute, time.Hour)
	token, _, err := issuer.IssueAccessToken("user-123", "golfer@example.com")
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if _, err := issuer.ParseAccessToken(token); err == nil {
		t.Error("an expired token was accepted")
	}
}

func TestSignUpNormalizesEmailAndRejectsDuplicates(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	session, err := svc.SignUp(ctx, "  Golfer@Example.COM ", "supersecret123", " Cody ")
	if err != nil {
		t.Fatalf("sign up: %v", err)
	}
	if session.User.Email != "golfer@example.com" {
		t.Errorf("email = %q, want it lowercased and trimmed", session.User.Email)
	}
	if session.User.DisplayName != "Cody" {
		t.Errorf("display name = %q, want it trimmed", session.User.DisplayName)
	}
	if !session.User.HasPassword {
		t.Error("a password signup should report has_password")
	}

	// Signing up again with different casing must collide, not create a second row.
	if _, err := svc.SignUp(ctx, "GOLFER@example.com", "anotherpassword", "Imposter"); err == nil {
		t.Error("duplicate signup was allowed")
	}
}

func TestLogInWrongPasswordAndUnknownEmail(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	if _, err := svc.SignUp(ctx, "golfer@example.com", "supersecret123", "Cody"); err != nil {
		t.Fatalf("sign up: %v", err)
	}

	if _, err := svc.LogIn(ctx, "golfer@example.com", "supersecret123"); err != nil {
		t.Errorf("valid login failed: %v", err)
	}
	if _, err := svc.LogIn(ctx, "golfer@example.com", "wrong-password"); err == nil {
		t.Error("login with the wrong password succeeded")
	}
	if _, err := svc.LogIn(ctx, "nobody@example.com", "supersecret123"); err == nil {
		t.Error("login with an unknown email succeeded")
	}
}

// A returning Google user must resolve to the account they already have.
func TestGoogleLoginIsIdempotentForReturningUser(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	identity := &GoogleIdentity{
		Subject:       "google-subject-1",
		Email:         "newgolfer@example.com",
		EmailVerified: true,
		Name:          "New Golfer",
	}

	first, err := svc.CompleteGoogleLogin(ctx, identity)
	if err != nil {
		t.Fatalf("first google login: %v", err)
	}
	if first.User.HasPassword {
		t.Error("a Google-only account should have no password")
	}
	if !first.User.EmailVerified {
		t.Error("a verified Google email should mark the account verified")
	}

	second, err := svc.CompleteGoogleLogin(ctx, identity)
	if err != nil {
		t.Fatalf("second google login: %v", err)
	}
	if first.User.ID != second.User.ID {
		t.Errorf("returning Google user got a new account: %s then %s", first.User.ID, second.User.ID)
	}
}

// Password signup followed by Google login on the same verified email must land
// on one account, not two.
func TestGoogleLoginLinksToExistingPasswordAccount(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	password, err := svc.SignUp(ctx, "golfer@example.com", "supersecret123", "Cody")
	if err != nil {
		t.Fatalf("sign up: %v", err)
	}

	google, err := svc.CompleteGoogleLogin(ctx, &GoogleIdentity{
		Subject:       "google-subject-2",
		Email:         "Golfer@Example.com", // different casing on purpose
		EmailVerified: true,
		Name:          "Cody G",
	})
	if err != nil {
		t.Fatalf("google login: %v", err)
	}

	if google.User.ID != password.User.ID {
		t.Fatalf("google login created a second account: %s vs %s", google.User.ID, password.User.ID)
	}
	if !google.User.HasPassword {
		t.Error("linking Google should not remove the existing password")
	}
	if len(google.User.Providers) != 1 || google.User.Providers[0] != ProviderGoogle {
		t.Errorf("providers = %v, want [google]", google.User.Providers)
	}

	// And the password still works afterwards.
	if _, err := svc.LogIn(ctx, "golfer@example.com", "supersecret123"); err != nil {
		t.Errorf("password login broke after linking Google: %v", err)
	}
}

// An unverified provider email must not silently claim an existing account.
func TestGoogleLoginRefusesUnverifiedEmailTakeover(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	if _, err := svc.SignUp(ctx, "victim@example.com", "supersecret123", "Victim"); err != nil {
		t.Fatalf("sign up: %v", err)
	}

	_, err := svc.CompleteGoogleLogin(ctx, &GoogleIdentity{
		Subject:       "attacker-subject",
		Email:         "victim@example.com",
		EmailVerified: false,
		Name:          "Attacker",
	})
	if err == nil {
		t.Fatal("an unverified Google email was allowed to claim an existing account")
	}
}

func TestLinkGoogleToSignedInAccount(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	session, err := svc.SignUp(ctx, "golfer@example.com", "supersecret123", "Cody")
	if err != nil {
		t.Fatalf("sign up: %v", err)
	}

	identity := &GoogleIdentity{
		Subject:       "google-subject-3",
		Email:         "golfer@example.com",
		EmailVerified: true,
	}

	user, err := svc.LinkGoogle(ctx, session.User.ID, identity)
	if err != nil {
		t.Fatalf("link google: %v", err)
	}
	if len(user.Providers) != 1 || user.Providers[0] != ProviderGoogle {
		t.Errorf("providers = %v, want [google]", user.Providers)
	}

	// Linking the same identity again is a no-op rather than an error.
	if _, err := svc.LinkGoogle(ctx, session.User.ID, identity); err != nil {
		t.Errorf("re-linking the same Google account failed: %v", err)
	}

	// The same Google identity cannot be attached to a second account.
	other, err := svc.SignUp(ctx, "other@example.com", "supersecret123", "Other")
	if err != nil {
		t.Fatalf("sign up other: %v", err)
	}
	if _, err := svc.LinkGoogle(ctx, other.User.ID, identity); err == nil {
		t.Error("one Google account was linked to two Roundly accounts")
	}
}

func TestGoogleOnlyUserCanSetPasswordThenLogIn(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	session, err := svc.CompleteGoogleLogin(ctx, &GoogleIdentity{
		Subject:       "google-subject-4",
		Email:         "oauthonly@example.com",
		EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("google login: %v", err)
	}

	// Password login must not work before one is set.
	if _, err := svc.LogIn(ctx, "oauthonly@example.com", "brandnewpassword"); err == nil {
		t.Error("password login worked on an account with no password")
	}

	// No current password is required, because there is none to confirm.
	if err := svc.SetPassword(ctx, session.User.ID, "", "brandnewpassword"); err != nil {
		t.Fatalf("set password: %v", err)
	}
	if _, err := svc.LogIn(ctx, "oauthonly@example.com", "brandnewpassword"); err != nil {
		t.Errorf("password login failed after setting one: %v", err)
	}
}

func TestSetPasswordRequiresCurrentPasswordWhenOneExists(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	session, err := svc.SignUp(ctx, "golfer@example.com", "supersecret123", "Cody")
	if err != nil {
		t.Fatalf("sign up: %v", err)
	}

	if err := svc.SetPassword(ctx, session.User.ID, "", "replacementpassword"); err == nil {
		t.Error("password was changed without the current one")
	}
	if err := svc.SetPassword(ctx, session.User.ID, "not-the-password", "replacementpassword"); err == nil {
		t.Error("password was changed with an incorrect current password")
	}
	if err := svc.SetPassword(ctx, session.User.ID, "supersecret123", "replacementpassword"); err != nil {
		t.Fatalf("legitimate password change failed: %v", err)
	}
	if _, err := svc.LogIn(ctx, "golfer@example.com", "replacementpassword"); err != nil {
		t.Errorf("login with the new password failed: %v", err)
	}
}

func TestRefreshRotatesAndDetectsReplay(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	session, err := svc.SignUp(ctx, "golfer@example.com", "supersecret123", "Cody")
	if err != nil {
		t.Fatalf("sign up: %v", err)
	}

	rotated, err := svc.Refresh(ctx, session.RefreshToken)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if rotated.RefreshToken == session.RefreshToken {
		t.Error("refresh returned the same token, so it is not rotating")
	}

	// Replaying the consumed token must fail and invalidate the whole family,
	// since a reused refresh token means it leaked.
	if _, err := svc.Refresh(ctx, session.RefreshToken); err == nil {
		t.Error("a consumed refresh token was accepted again")
	}
	if _, err := svc.Refresh(ctx, rotated.RefreshToken); err == nil {
		t.Error("the replacement token still worked after a replay was detected")
	}
}

func TestLogOutRevokesRefreshToken(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	session, err := svc.SignUp(ctx, "golfer@example.com", "supersecret123", "Cody")
	if err != nil {
		t.Fatalf("sign up: %v", err)
	}

	if err := svc.LogOut(ctx, session.RefreshToken); err != nil {
		t.Fatalf("log out: %v", err)
	}
	if _, err := svc.Refresh(ctx, session.RefreshToken); err == nil {
		t.Error("a revoked refresh token was still accepted")
	}

	// Logging out twice, or with nonsense, is not an error.
	if err := svc.LogOut(ctx, session.RefreshToken); err != nil {
		t.Errorf("second log out returned an error: %v", err)
	}
	if err := svc.LogOut(ctx, "not-a-real-token"); err != nil {
		t.Errorf("log out with an unknown token returned an error: %v", err)
	}
}

func TestGoogleProviderDisabledWithoutCredentials(t *testing.T) {
	provider := NewGoogleProvider("", "", "")
	if provider.Enabled() {
		t.Error("provider reports enabled with no credentials")
	}
	if _, err := provider.AuthCodeURL("state", "verifier"); !errors.Is(err, ErrGoogleNotConfigured) {
		t.Errorf("AuthCodeURL error = %v, want ErrGoogleNotConfigured", err)
	}

	configured := NewGoogleProvider("client-id", "client-secret", "http://localhost:8080/api/auth/google/callback")
	if !configured.Enabled() {
		t.Fatal("provider reports disabled with full credentials")
	}
	url, err := configured.AuthCodeURL("state-value", configured.NewVerifier())
	if err != nil {
		t.Fatalf("AuthCodeURL: %v", err)
	}
	for _, want := range []string{
		"accounts.google.com",
		"client_id=client-id",
		"state=state-value",
		"code_challenge=",
		"code_challenge_method=S256",
	} {
		if !strings.Contains(url, want) {
			t.Errorf("authorization URL is missing %q:\n%s", want, url)
		}
	}
}
