package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/edingc/roundly/internal/config"
	"github.com/edingc/roundly/internal/database"
)

// login posts credentials and returns the status and decoded body.
func login(t *testing.T, h http.Handler, email, password string) (int, map[string]any) {
	t.Helper()
	body := fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)
	rr := do(t, h, http.MethodPost, "/api/auth/login", "", strings.NewReader(body))

	var decoded map[string]any
	if rr.Body.Len() > 0 {
		_ = json.Unmarshal(rr.Body.Bytes(), &decoded)
	}
	return rr.Code, decoded
}

// The gap this closes: /api/auth/login used to take guesses as fast as they
// arrived, which is minutes for an eight-character password.
func TestLoginStopsAcceptingGuesses(t *testing.T) {
	h, _ := newTestServer(t, 1000)
	signUp(t, h, "player@example.com")

	// The default allowance is 10 failures per account per window.
	var lastStatus int
	var lastBody map[string]any
	for range 12 {
		lastStatus, lastBody = login(t, h, "player@example.com", "definitely-not-the-password")
		if lastStatus == http.StatusTooManyRequests {
			break
		}
	}

	if lastStatus != http.StatusTooManyRequests {
		t.Fatalf("status = %d after twelve wrong passwords, want 429", lastStatus)
	}
	if lastBody["error"] != "too_many_attempts" {
		t.Errorf("error = %v, want too_many_attempts", lastBody["error"])
	}
	// `fields` is reserved for per-field validation detail. Anything else there
	// makes the client render the error as a form failure and swallow the
	// message, so a throttle response must not carry any.
	if lastBody["fields"] != nil {
		t.Errorf("fields = %v, want none — the client reads its presence as validation",
			lastBody["fields"])
	}

	// The correct password is refused too, which is the point: an attacker who
	// happens to guess right on attempt eleven still does not get in.
	status, _ := login(t, h, "player@example.com", "test-password-123")
	if status != http.StatusTooManyRequests {
		t.Errorf("status = %d for the right password while blocked, want 429", status)
	}
}

// A limiter that only refuses real accounts is an enumeration oracle, and a
// particularly good one: it needs no valid password to consult.
func TestLoginThrottleSaysTheSameThingForAnAccountThatDoesNotExist(t *testing.T) {
	h, _ := newTestServer(t, 1000)
	signUp(t, h, "real@example.com")

	exhaust := func(email string) (int, map[string]any) {
		var status int
		var body map[string]any
		for range 12 {
			status, body = login(t, h, email, "definitely-not-the-password")
			if status == http.StatusTooManyRequests {
				break
			}
		}
		return status, body
	}

	realStatus, realBody := exhaust("real@example.com")
	fakeStatus, fakeBody := exhaust("nobody@example.com")

	if realStatus != http.StatusTooManyRequests || fakeStatus != http.StatusTooManyRequests {
		t.Fatalf("statuses = %d and %d, want both 429", realStatus, fakeStatus)
	}
	if realBody["message"] != fakeBody["message"] {
		t.Errorf("the two refusals differ:\n  %v\n  %v", realBody["message"], fakeBody["message"])
	}
}

// Somebody who knows their password must never be locked out by their own
// traffic — that is the failure mode that gets rate limits removed.
func TestSuccessfulLoginsAreNotCounted(t *testing.T) {
	h, _ := newTestServer(t, 1000)
	signUp(t, h, "player@example.com")

	for i := range 30 {
		status, body := login(t, h, "player@example.com", "test-password-123")
		if status != http.StatusOK {
			t.Fatalf("sign-in %d: status = %d, want 200; body %v", i+1, status, body)
		}
	}
}

// One person's typos must not lock out an unrelated account.
func TestOneAccountsFailuresDoNotBlockAnother(t *testing.T) {
	h, _ := newTestServer(t, 1000)
	signUp(t, h, "unlucky@example.com")
	signUp(t, h, "bystander@example.com")

	for range 12 {
		login(t, h, "unlucky@example.com", "definitely-not-the-password")
	}

	// Both requests come from the same test address, so this also proves the
	// per-IP allowance is looser than the per-account one — an office or a
	// household behind one address should not lock each other out.
	status, body := login(t, h, "bystander@example.com", "test-password-123")
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200; body %v", status, body)
	}
}

// newSignupLimitedServer builds a handler with a realistic signup allowance,
// rather than the deliberately generous one the shared fixtures use.
func newSignupLimitedServer(t *testing.T, limit int) http.Handler {
	t.Helper()

	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	cfg := &config.Config{
		Env:              "test",
		JWTSecret:        []byte("test-secret-that-is-long-enough-to-sign"),
		AccessTokenTTL:   15 * time.Minute,
		RefreshTokenTTL:  24 * time.Hour,
		PublicURL:        "http://localhost",
		APIKeyRateLimit:  1000,
		APIKeyRateWindow: time.Minute,
		APIKeyMaxPerUser: 10,
		SignupRateLimit:  limit,
		SignupRateWindow: time.Hour,
	}

	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	handler, err := New(cfg, db, nil, stop)
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	return handler
}

// trySignUp posts a signup without asserting anything about the outcome.
func trySignUp(t *testing.T, h http.Handler, email string) (int, map[string]any) {
	t.Helper()
	body := fmt.Sprintf(`{"email":%q,"password":"test-password-123","display_name":"Tester"}`, email)
	rr := do(t, h, http.MethodPost, "/api/auth/signup", "", strings.NewReader(body))

	var decoded map[string]any
	if rr.Body.Len() > 0 {
		_ = json.Unmarshal(rr.Body.Bytes(), &decoded)
	}
	return rr.Code, decoded
}

// Unlike sign-in, a *successful* signup is the abuse: filling an instance with
// junk accounts needs no failed attempts at all.
func TestSignupStopsAfterItsAllowance(t *testing.T) {
	h := newSignupLimitedServer(t, 3)

	for i := range 3 {
		status, body := trySignUp(t, h, fmt.Sprintf("real-%d@example.com", i))
		if status != http.StatusCreated {
			t.Fatalf("signup %d: status = %d, want 201; body %v", i+1, status, body)
		}
	}

	status, body := trySignUp(t, h, "one-too-many@example.com")
	if status != http.StatusTooManyRequests {
		t.Fatalf("status = %d on the fourth signup, want 429; body %v", status, body)
	}
	if body["error"] != "too_many_signups" {
		t.Errorf("error = %v, want too_many_signups", body["error"])
	}
	if body["fields"] != nil {
		t.Errorf("fields = %v, want none", body["fields"])
	}

	// And the account really was not created.
	status, _ = login(t, h, "one-too-many@example.com", "test-password-123")
	if status == http.StatusOK {
		t.Error("the refused signup created an account anyway")
	}
}

// A script probing which addresses are already registered never needs to send a
// valid password. Charging only well-formed attempts would leave that free.
func TestSignupCountsMalformedAttemptsToo(t *testing.T) {
	h := newSignupLimitedServer(t, 3)

	for range 3 {
		// No password at all: a 422, and still an attempt.
		status, _ := trySignUp(t, h, "not-a-valid-email")
		if status == http.StatusCreated {
			t.Fatal("an invalid signup was accepted")
		}
	}

	status, body := trySignUp(t, h, "genuine@example.com")
	if status != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429 — invalid attempts should have used the allowance; body %v",
			status, body)
	}
}

// Being cut off from signing up must not affect signing in to an account that
// already exists.
func TestSignupLimitDoesNotBlockSigningIn(t *testing.T) {
	h := newSignupLimitedServer(t, 2)

	if status, body := trySignUp(t, h, "established@example.com"); status != http.StatusCreated {
		t.Fatalf("first signup: status = %d, body %v", status, body)
	}
	for range 5 {
		trySignUp(t, h, "flood@example.com")
	}

	status, body := login(t, h, "established@example.com", "test-password-123")
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200 — the signup limit leaked into sign-in; body %v", status, body)
	}
}

// The delay belongs in Retry-After, where a client or a proxy looks for it —
// not in the validation-fields map, where it used to hide the message.
func TestThrottleRepliesWithRetryAfter(t *testing.T) {
	h, _ := newTestServer(t, 1000)
	signUp(t, h, "player@example.com")

	body := fmt.Sprintf(`{"email":%q,"password":%q}`, "player@example.com", "wrong")
	var rr *httptest.ResponseRecorder
	for range 12 {
		rr = do(t, h, http.MethodPost, "/api/auth/login", "", strings.NewReader(body))
		if rr.Code == http.StatusTooManyRequests {
			break
		}
	}
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rr.Code)
	}

	retry := rr.Header().Get("Retry-After")
	if retry == "" {
		t.Fatal("no Retry-After header on a 429")
	}
	seconds, err := strconv.Atoi(retry)
	if err != nil || seconds <= 0 {
		t.Errorf("Retry-After = %q, want a positive number of seconds", retry)
	}

	// The wait is also stated in the message, because that is what a person
	// actually reads.
	var decoded map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &decoded)
	message, _ := decoded["message"].(string)
	if !strings.Contains(message, "minute") {
		t.Errorf("message = %q, want it to say how long to wait", message)
	}
}
