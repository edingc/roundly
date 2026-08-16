package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/edingc/roundly/internal/config"
	"github.com/edingc/roundly/internal/database"
)

// newVerifyingServer builds a handler on an instance that has mail configured,
// which is what switches the verification gate on.
//
// The SMTP host points at a closed port on purpose. Delivery failing is not
// what is under test — the gate is — and a signup whose mail did not send is
// exactly the state that has to be reachable: an account that exists, holds a
// session, and has not confirmed anything.
func newVerifyingServer(t *testing.T) http.Handler {
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
		MailFrom:         "Roundly <no-reply@example.test>",
		SMTPHost:         "127.0.0.1",
		SMTPPort:         closedPort(t),
	}

	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	handler, err := New(cfg, db, nil, stop)
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	return handler
}

// closedPort returns a port with nothing listening on it.
func closedPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

func TestConfigAnnouncesEmailSupport(t *testing.T) {
	h := newVerifyingServer(t)

	rr := do(t, h, http.MethodGet, "/api/auth/config", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	var cfg struct {
		EmailEnabled              bool `json:"email_enabled"`
		EmailVerificationRequired bool `json:"email_verification_required"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if !cfg.EmailEnabled || !cfg.EmailVerificationRequired {
		t.Errorf("config = %+v, want both true on an instance with mail", cfg)
	}
}

// The gate: a signed-in but unconfirmed account gets a session that opens
// nothing, and gets told why in a code the client can act on.
func TestUnverifiedAccountIsBlockedFromTheApp(t *testing.T) {
	h := newVerifyingServer(t)
	_, token := signUp(t, h, "unconfirmed@example.com")

	for _, path := range []string{
		"/api/courses",
		"/api/clubs",
		"/api/account/export",
		"/api/account/keys",
	} {
		t.Run(path, func(t *testing.T) {
			rr := do(t, h, http.MethodGet, path, token)
			if rr.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403; body %s", rr.Code, rr.Body.String())
			}
			var body struct {
				// The wire name for APIError.Code is "error".
				Code string `json:"error"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode error: %v", err)
			}
			// A distinct code, because the client renders a whole screen for it
			// and must not confuse it with an ordinary refusal.
			if body.Code != "email_unverified" {
				t.Errorf("code = %q, want email_unverified", body.Code)
			}
		})
	}
}

// Being blocked from the app must not mean being blocked from getting out of
// that state.
func TestUnverifiedAccountCanStillSeeItselfAndAskAgain(t *testing.T) {
	h := newVerifyingServer(t)
	_, token := signUp(t, h, "unconfirmed@example.com")

	rr := do(t, h, http.MethodGet, "/api/auth/me", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("/api/auth/me status = %d, want 200; body %s", rr.Code, rr.Body.String())
	}
	var user struct {
		EmailVerified bool `json:"email_verified"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &user); err != nil {
		t.Fatalf("decode user: %v", err)
	}
	if user.EmailVerified {
		t.Error("email_verified = true, want false for an account that never opened the link")
	}

	// The resend endpoint has to be reachable. Delivery fails here because the
	// port is closed, which is a 500 — what matters is that it was not the
	// verification gate that answered.
	rr = do(t, h, http.MethodPost, "/api/auth/verify-email/resend", token, strings.NewReader("{}"))
	if rr.Code == http.StatusForbidden {
		t.Errorf("resend was blocked by the gate it exists to escape: %s", rr.Body.String())
	}
}

func TestVerifyEmailRejectsAJunkToken(t *testing.T) {
	h := newVerifyingServer(t)

	rr := do(t, h, http.MethodPost, "/api/auth/verify-email", "",
		strings.NewReader(`{"token":"nonsense"}`))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401; body %s", rr.Code, rr.Body.String())
	}
}
