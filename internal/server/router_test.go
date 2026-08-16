package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/edingc/roundly/internal/apikey"
	"github.com/edingc/roundly/internal/config"
	"github.com/edingc/roundly/internal/database"
	"github.com/edingc/roundly/internal/database/sqlc"
	"github.com/edingc/roundly/internal/id"
	"github.com/edingc/roundly/internal/timex"
)

// These tests go through the real handler built by New, not through the
// packages in isolation. That is deliberate: the read-only guarantee is a
// property of middleware *order*, so anything that stubs the router out would
// pass while the running server was wide open.

func newTestServer(t *testing.T, rateLimit int) (http.Handler, *database.DB) {
	t.Helper()

	// The guard logs a line per API-key request by design. That is useful in
	// production and pure noise across a few hundred subtests, so it goes to
	// the void here rather than burying a real failure.
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
		APIKeyRateLimit:  rateLimit,
		APIKeyRateWindow: time.Minute,
		APIKeyMaxPerUser: 10,
		// Generous on purpose. These fixtures create accounts freely, and a
		// realistic signup limit would make an unrelated test fail on its sixth
		// fixture with a message about rate limiting. The limit itself is
		// tested deliberately, in throttle_test.go.
		SignupRateLimit:  1000,
		SignupRateWindow: time.Hour,
	}
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	handler, err := New(cfg, db, nil, stop)
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	return handler, db
}

// signUp creates an account through the real endpoint and returns its ID and a
// live access token.
func signUp(t *testing.T, h http.Handler, email string) (userID, accessToken string) {
	t.Helper()

	body := fmt.Sprintf(`{"email":%q,"password":"test-password-123","display_name":"Tester"}`, email)
	rr := do(t, h, http.MethodPost, "/api/auth/signup", "", strings.NewReader(body))
	if rr.Code != http.StatusCreated {
		t.Fatalf("signup %s: status %d, body %s", email, rr.Code, rr.Body.String())
	}

	var session struct {
		AccessToken string `json:"access_token"`
		User        struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	return session.User.ID, session.AccessToken
}

// mintKey inserts a key directly, so tests do not depend on the creation
// endpoint to test the guard.
func mintKey(t *testing.T, db *database.DB, userID string) string {
	t.Helper()

	token, hash, prefix, err := apikey.NewToken()
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	if err := db.Queries.CreateAPIKey(context.Background(), sqlc.CreateAPIKeyParams{
		ID:        id.New(),
		UserID:    userID,
		Name:      "test key",
		KeyHash:   hash,
		KeyPrefix: prefix,
		Scope:     apikey.ScopeRead,
		CreatedAt: timex.Now(),
	}); err != nil {
		t.Fatalf("create api key: %v", err)
	}
	return token
}

func do(t *testing.T, h http.Handler, method, path, credential string, body ...*strings.Reader) *httptest.ResponseRecorder {
	t.Helper()

	var reader *strings.Reader
	if len(body) > 0 {
		reader = body[0]
	} else {
		reader = strings.NewReader("")
	}

	req := httptest.NewRequest(method, path, reader)
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	if method != http.MethodGet && method != http.MethodDelete {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func countRows(t *testing.T, db *database.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// Every write verb on every write route must be refused, and nothing may be
// created as a side effect of trying.
func TestAPIKeyRejectsEveryWriteVerb(t *testing.T) {
	h, db := newTestServer(t, 1000)
	userID, _ := signUp(t, h, "writer@example.test")
	key := mintKey(t, db, userID)

	routes := []struct{ method, path string }{
		{http.MethodPost, "/api/courses"},
		{http.MethodPost, "/api/courses/import"},
		{http.MethodPut, "/api/courses/abc"},
		{http.MethodDelete, "/api/courses/abc"},
		{http.MethodPost, "/api/courses/abc/tees"},
		{http.MethodPost, "/api/courses/abc/holes"},
		{http.MethodPut, "/api/tees/abc"},
		{http.MethodDelete, "/api/tees/abc"},
		{http.MethodPut, "/api/holes/abc"},
		{http.MethodDelete, "/api/holes/abc"},
		{http.MethodPut, "/api/holes/abc/tee-details/def"},
		{http.MethodDelete, "/api/holes/abc/tee-details/def"},
		{http.MethodPost, "/api/clubs"},
		{http.MethodPut, "/api/clubs/abc"},
		{http.MethodPut, "/api/clubs/abc/status"},
		{http.MethodDelete, "/api/clubs/abc"},
		{http.MethodPatch, "/api/clubs/abc"},
	}

	beforeCourses := countRows(t, db, "courses")
	beforeClubs := countRows(t, db, "clubs")

	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			rr := do(t, h, rt.method, rt.path, key, strings.NewReader(`{"name":"x","label":"x","club_type":"iron"}`))
			if rr.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403; body %s", rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), "api_key_read_only") &&
				!strings.Contains(rr.Body.String(), "forbidden") {
				t.Errorf("unexpected error body: %s", rr.Body.String())
			}
		})
	}

	if after := countRows(t, db, "courses"); after != beforeCourses {
		t.Errorf("courses changed from %d to %d — a write got through", beforeCourses, after)
	}
	if after := countRows(t, db, "clubs"); after != beforeClubs {
		t.Errorf("clubs changed from %d to %d — a write got through", beforeClubs, after)
	}
}

// A key must not reach anything under /api/auth, by any spelling of the path.
func TestAPIKeyCannotReachAuthRoutes(t *testing.T) {
	h, db := newTestServer(t, 1000)
	userID, _ := signUp(t, h, "auth-probe@example.test")
	key := mintKey(t, db, userID)

	paths := []string{
		"/api/auth/me",
		"/api/auth/config",
		"/api/auth/google/start",
		"/api/auth/google/callback",
		"/api/auth/link/google",
		// Spellings that a naive prefix check would miss.
		"/api/auth/../auth/me",
		"/api//auth/me",
		"/api/auth/me/",
		"/api/./auth/me",
	}

	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			rr := do(t, h, http.MethodGet, p, key)
			if rr.Code == http.StatusOK {
				t.Fatalf("status 200 — an API key reached %s; body %s", p, rr.Body.String())
			}
			if rr.Code != http.StatusForbidden && rr.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 403 or 404; body %s", rr.Code, rr.Body.String())
			}
		})
	}
}

// The account routes are the ones a method check alone would not save: the
// export is a GET that returns everything the user owns.
func TestAPIKeyCannotReachAccountRoutes(t *testing.T) {
	h, db := newTestServer(t, 1000)
	userID, token := signUp(t, h, "acct-probe@example.test")
	key := mintKey(t, db, userID)

	// Give the account something worth stealing.
	if rr := do(t, h, http.MethodPost, "/api/clubs", token,
		strings.NewReader(`{"club_type":"iron","label":"Secret 7 iron"}`)); rr.Code != http.StatusCreated {
		t.Fatalf("seed club: %d %s", rr.Code, rr.Body.String())
	}

	for _, p := range []string{
		"/api/account/export",
		"/api/account/export?format=csv",
		"/api/account/export?format=json",
		"/api/account/keys",
		"/api/account/profile",
		"/api/account",
	} {
		t.Run(p, func(t *testing.T) {
			rr := do(t, h, http.MethodGet, p, key)
			if rr.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403; body %s", rr.Code, rr.Body.String())
			}
			if strings.Contains(rr.Body.String(), "Secret 7 iron") {
				t.Error("the response leaked account data")
			}
		})
	}
}

func TestAPIKeyReachesOnlyAllowListedReads(t *testing.T) {
	h, db := newTestServer(t, 1000)
	userID, _ := signUp(t, h, "reader@example.test")
	key := mintKey(t, db, userID)

	allowed := []string{"/api/health", "/api/me", "/api/courses", "/api/clubs", "/api/clubs/options"}
	for _, p := range allowed {
		t.Run("allowed "+p, func(t *testing.T) {
			if rr := do(t, h, http.MethodGet, p, key); rr.Code != http.StatusOK {
				t.Errorf("status = %d, want 200; body %s", rr.Code, rr.Body.String())
			}
		})
	}

	// A GET that exists but is not on the list.
	t.Run("denied /api/courses/abc/export", func(t *testing.T) {
		if rr := do(t, h, http.MethodGet, "/api/courses/abc/export", key); rr.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403; body %s", rr.Code, rr.Body.String())
		}
	})
}

func TestAPIKeyCannotReadAnotherUsersData(t *testing.T) {
	h, db := newTestServer(t, 1000)
	userA, tokenA := signUp(t, h, "a@example.test")
	_, tokenB := signUp(t, h, "b@example.test")

	if rr := do(t, h, http.MethodPost, "/api/clubs", tokenA,
		strings.NewReader(`{"club_type":"iron","label":"A 7 iron"}`)); rr.Code != http.StatusCreated {
		t.Fatalf("seed A: %d %s", rr.Code, rr.Body.String())
	}
	rr := do(t, h, http.MethodPost, "/api/clubs", tokenB,
		strings.NewReader(`{"club_type":"wedge","label":"B wedge"}`))
	if rr.Code != http.StatusCreated {
		t.Fatalf("seed B: %d %s", rr.Code, rr.Body.String())
	}
	var bClub struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &bClub); err != nil {
		t.Fatalf("decode B club: %v", err)
	}

	keyA := mintKey(t, db, userA)

	bag := do(t, h, http.MethodGet, "/api/clubs", keyA)
	if bag.Code != http.StatusOK {
		t.Fatalf("bag: %d %s", bag.Code, bag.Body.String())
	}
	if strings.Contains(bag.Body.String(), "B wedge") {
		t.Error("A's key returned B's clubs")
	}
	if !strings.Contains(bag.Body.String(), "A 7 iron") {
		t.Error("A's key did not return A's own clubs")
	}

	// 404 rather than 403, matching the club package's existing rule that a
	// bag is private enough that confirming an ID exists is itself a leak.
	one := do(t, h, http.MethodGet, "/api/clubs/"+bClub.ID, keyA)
	if one.Code != http.StatusNotFound {
		t.Errorf("another user's club: status = %d, want 404; body %s", one.Code, one.Body.String())
	}
}

func TestRevokedExpiredAndUnknownKeysAreRejectedIdentically(t *testing.T) {
	h, db := newTestServer(t, 1000)
	userID, _ := signUp(t, h, "revoke@example.test")
	ctx := context.Background()

	live := mintKey(t, db, userID)
	if rr := do(t, h, http.MethodGet, "/api/clubs", live); rr.Code != http.StatusOK {
		t.Fatalf("a fresh key was refused: %d %s", rr.Code, rr.Body.String())
	}

	// Revoked.
	revoked := mintKey(t, db, userID)
	now := timex.Now()
	rows, err := db.Queries.ListAPIKeysByUser(ctx, userID)
	if err != nil {
		t.Fatalf("list keys: %v", err)
	}
	for _, row := range rows {
		if row.KeyPrefix == revoked[:apikey.PrefixLength] {
			if err := db.Queries.RevokeAPIKey(ctx, sqlc.RevokeAPIKeyParams{
				RevokedAt: &now, ID: row.ID, UserID: userID,
			}); err != nil {
				t.Fatalf("revoke: %v", err)
			}
		}
	}

	// Expired.
	expiredToken, expiredHash, expiredPrefix, _ := apikey.NewToken()
	past := timex.Format(time.Now().Add(-time.Hour))
	if err := db.Queries.CreateAPIKey(ctx, sqlc.CreateAPIKeyParams{
		ID: id.New(), UserID: userID, Name: "expired",
		KeyHash: expiredHash, KeyPrefix: expiredPrefix, Scope: apikey.ScopeRead,
		CreatedAt: timex.Now(), ExpiresAt: &past,
	}); err != nil {
		t.Fatalf("create expired key: %v", err)
	}

	unknown, _, _, _ := apikey.NewToken()

	bodies := make(map[string]string)
	for name, tok := range map[string]string{"revoked": revoked, "expired": expiredToken, "unknown": unknown} {
		rr := do(t, h, http.MethodGet, "/api/clubs", tok)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("%s key: status = %d, want 401; body %s", name, rr.Code, rr.Body.String())
		}
		if strings.Contains(rr.Body.String(), tok) {
			t.Errorf("%s: the response echoed the token", name)
		}
		bodies[name] = rr.Body.String()
	}

	// Identical messages, so a prober cannot tell which case they hit — in
	// particular cannot confirm that a key once existed.
	if bodies["revoked"] != bodies["unknown"] || bodies["expired"] != bodies["unknown"] {
		t.Errorf("rejection messages differ and leak which case was hit:\n revoked=%s\n expired=%s\n unknown=%s",
			bodies["revoked"], bodies["expired"], bodies["unknown"])
	}
}

func TestAPIKeyRateLimit(t *testing.T) {
	h, db := newTestServer(t, 3)
	userID, _ := signUp(t, h, "ratelimit@example.test")
	key := mintKey(t, db, userID)

	for i := 1; i <= 3; i++ {
		rr := do(t, h, http.MethodGet, "/api/clubs", key)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200", i, rr.Code)
		}
		if got := rr.Header().Get("X-RateLimit-Remaining"); got != fmt.Sprint(3-i) {
			t.Errorf("request %d: X-RateLimit-Remaining = %q, want %q", i, got, fmt.Sprint(3-i))
		}
		if rr.Header().Get("X-RateLimit-Limit") != "3" {
			t.Errorf("request %d: missing or wrong X-RateLimit-Limit", i)
		}
	}

	rr := do(t, h, http.MethodGet, "/api/clubs", key)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("fourth request: status = %d, want 429; body %s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Error("429 is missing Retry-After")
	}
	if rr.Header().Get("X-RateLimit-Reset") == "" {
		t.Error("429 is missing X-RateLimit-Reset")
	}
	if !strings.Contains(rr.Body.String(), "rate_limited") {
		t.Errorf("unexpected 429 body: %s", rr.Body.String())
	}
}

// The regression test for the guard itself: a real signed-in session must be
// completely unaffected by any of the above.
func TestJWTSessionIsUnaffectedByTheGuard(t *testing.T) {
	h, _ := newTestServer(t, 3)
	_, token := signUp(t, h, "human@example.test")

	t.Run("writes still work", func(t *testing.T) {
		rr := do(t, h, http.MethodPost, "/api/clubs", token,
			strings.NewReader(`{"club_type":"iron","label":"7 iron"}`))
		if rr.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("auth routes still work", func(t *testing.T) {
		if rr := do(t, h, http.MethodGet, "/api/auth/me", token); rr.Code != http.StatusOK {
			t.Errorf("status = %d, want 200; body %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("account routes still work", func(t *testing.T) {
		if rr := do(t, h, http.MethodGet, "/api/account/export", token); rr.Code != http.StatusOK {
			t.Errorf("status = %d, want 200; body %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("no rate limiting and no key headers", func(t *testing.T) {
		for i := 0; i < 10; i++ {
			rr := do(t, h, http.MethodGet, "/api/clubs", token)
			if rr.Code != http.StatusOK {
				t.Fatalf("request %d: status = %d — a session was rate limited", i, rr.Code)
			}
			if rr.Header().Get("X-RateLimit-Limit") != "" {
				t.Error("a session response carried API-key rate-limit headers")
			}
		}
	})
}

// Proves the ContextWithPrincipal bridge: handlers written before API keys
// existed read identity through auth.MustUserID and must keep working.
func TestAPIKeyPrincipalSatisfiesMustUserID(t *testing.T) {
	h, db := newTestServer(t, 1000)
	userID, token := signUp(t, h, "bridge@example.test")

	if rr := do(t, h, http.MethodPost, "/api/clubs", token,
		strings.NewReader(`{"club_type":"wood","label":"3 wood"}`)); rr.Code != http.StatusCreated {
		t.Fatalf("seed: %d %s", rr.Code, rr.Body.String())
	}

	rr := do(t, h, http.MethodGet, "/api/me", mintKey(t, db, userID))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rr.Code, rr.Body.String())
	}
	var user struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &user); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if user.ID != userID {
		t.Errorf("id = %q, want %q — the key resolved to the wrong user", user.ID, userID)
	}
}

func TestGuardIgnoresNonAPIPaths(t *testing.T) {
	h, db := newTestServer(t, 1000)
	userID, _ := signUp(t, h, "spa@example.test")
	key := mintKey(t, db, userID)

	// No frontend is mounted in these tests, so the router's own 404 is the
	// tell that the guard did not intercept and 403 it.
	req := httptest.NewRequest(http.MethodGet, "/some/spa/route", bytes.NewReader(nil))
	req.Header.Set("X-API-Key", key)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code == http.StatusForbidden {
		t.Error("the guard intercepted a non-API path")
	}
}

func TestAPIKeyViaXAPIKeyHeader(t *testing.T) {
	h, db := newTestServer(t, 1000)
	userID, _ := signUp(t, h, "header@example.test")
	key := mintKey(t, db, userID)

	req := httptest.NewRequest(http.MethodGet, "/api/clubs", bytes.NewReader(nil))
	req.Header.Set("X-API-Key", key)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rr.Code, rr.Body.String())
	}

	// And the same header must still be read-only.
	req = httptest.NewRequest(http.MethodPost, "/api/clubs", strings.NewReader(`{"club_type":"iron","label":"x"}`))
	req.Header.Set("X-API-Key", key)
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("POST with X-API-Key: status = %d, want 403", rr.Code)
	}
}

// newAdminTestServer builds a handler whose configured administrator is the
// given address.
func newAdminTestServer(t *testing.T, adminEmail string) (http.Handler, *database.DB) {
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
		AdminEmail:       adminEmail,
		APIKeyRateLimit:  1000,
		APIKeyRateWindow: time.Minute,
		APIKeyMaxPerUser: 10,
		// Generous on purpose. These fixtures create accounts freely, and a
		// realistic signup limit would make an unrelated test fail on its sixth
		// fixture with a message about rate limiting. The limit itself is
		// tested deliberately, in throttle_test.go.
		SignupRateLimit:  1000,
		SignupRateWindow: time.Hour,
	}
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	handler, err := New(cfg, db, nil, stop)
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	return handler, db
}

// createCourse makes a course through the API and returns its ID.
func createCourse(t *testing.T, h http.Handler, token, name string) string {
	t.Helper()

	rr := do(t, h, http.MethodPost, "/api/courses", token,
		strings.NewReader(fmt.Sprintf(`{"name":%q,"hole_count":9}`, name)))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create course: %d %s", rr.Code, rr.Body.String())
	}
	var course struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &course); err != nil {
		t.Fatalf("decode course: %v", err)
	}
	return course.ID
}

// Deleting a course is the one destructive act on shared data, so it is the one
// course action a normal player cannot take.
func TestOnlyAdminCanDeleteACourse(t *testing.T) {
	h, _ := newAdminTestServer(t, "admin@example.test")
	_, adminToken := signUp(t, h, "admin@example.test")
	_, playerToken := signUp(t, h, "player@example.test")

	courseID := createCourse(t, h, playerToken, "Shared GC")

	// A normal player is refused, and the course is still there afterwards.
	rr := do(t, h, http.MethodDelete, "/api/courses/"+courseID, playerToken)
	if rr.Code != http.StatusForbidden {
		t.Errorf("player delete: status = %d, want 403; body %s", rr.Code, rr.Body.String())
	}
	if check := do(t, h, http.MethodGet, "/api/courses/"+courseID, playerToken); check.Code != http.StatusOK {
		t.Fatalf("the course vanished despite the refusal: %d", check.Code)
	}

	// Even though that same player can edit it freely.
	edit := do(t, h, http.MethodPut, "/api/courses/"+courseID, playerToken,
		strings.NewReader(`{"name":"Shared GC, corrected"}`))
	if edit.Code != http.StatusOK {
		t.Errorf("player edit: status = %d, want 200; body %s", edit.Code, edit.Body.String())
	}

	// The administrator can remove it.
	if rr := do(t, h, http.MethodDelete, "/api/courses/"+courseID, adminToken); rr.Code != http.StatusNoContent {
		t.Errorf("admin delete: status = %d, want 204; body %s", rr.Code, rr.Body.String())
	}
}

func TestOnlyAdminCanReachTheRemovalQueue(t *testing.T) {
	h, _ := newAdminTestServer(t, "admin@example.test")
	_, adminToken := signUp(t, h, "admin@example.test")
	_, playerToken := signUp(t, h, "player@example.test")

	courseID := createCourse(t, h, playerToken, "Queued GC")

	// Anyone may ask.
	ask := do(t, h, http.MethodPost, "/api/courses/"+courseID+"/removal-request", playerToken,
		strings.NewReader(`{"reason":"Duplicate entry"}`))
	if ask.Code != http.StatusCreated {
		t.Fatalf("request removal: status = %d, want 201; body %s", ask.Code, ask.Body.String())
	}

	// Nobody but the administrator may see or settle the queue.
	if rr := do(t, h, http.MethodGet, "/api/admin/removal-requests", playerToken); rr.Code != http.StatusForbidden {
		t.Errorf("player list: status = %d, want 403; body %s", rr.Code, rr.Body.String())
	}
	if rr := do(t, h, http.MethodGet, "/api/admin/removal-requests", adminToken); rr.Code != http.StatusOK {
		t.Errorf("admin list: status = %d, want 200; body %s", rr.Code, rr.Body.String())
	}
}

// With no ADMIN_EMAIL set there is no administrator, so nobody passes the gate
// and requests simply queue.
func TestNoAdminConfiguredMeansNobodyIsAdmin(t *testing.T) {
	h, _ := newAdminTestServer(t, "")
	_, token := signUp(t, h, "someone@example.test")

	courseID := createCourse(t, h, token, "Unadministered GC")

	if rr := do(t, h, http.MethodDelete, "/api/courses/"+courseID, token); rr.Code != http.StatusForbidden {
		t.Errorf("delete: status = %d, want 403; body %s", rr.Code, rr.Body.String())
	}
	if rr := do(t, h, http.MethodGet, "/api/admin/removal-requests", token); rr.Code != http.StatusForbidden {
		t.Errorf("queue: status = %d, want 403; body %s", rr.Code, rr.Body.String())
	}
	// The course is still readable and editable — no administrator does not
	// mean no app.
	if rr := do(t, h, http.MethodGet, "/api/courses/"+courseID, token); rr.Code != http.StatusOK {
		t.Errorf("read: status = %d, want 200", rr.Code)
	}
}

// An API key must not reach the administrator's routes, by any of the three
// controls that should stop it.
func TestAPIKeyCannotReachAdminRoutes(t *testing.T) {
	h, db := newAdminTestServer(t, "admin@example.test")
	adminID, adminToken := signUp(t, h, "admin@example.test")
	courseID := createCourse(t, h, adminToken, "Admin's GC")

	// The key belongs to the administrator, so only the API-key controls can
	// be what refuses it.
	key := mintKey(t, db, adminID)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/admin/removal-requests"},
		{http.MethodPost, "/api/admin/removal-requests/abc/resolve"},
		{http.MethodDelete, "/api/courses/" + courseID},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rr := do(t, h, tc.method, tc.path, key, strings.NewReader(`{"resolution":"removed"}`))
			if rr.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403; body %s", rr.Code, rr.Body.String())
			}
		})
	}

	// And the course survived every attempt.
	if rr := do(t, h, http.MethodGet, "/api/courses/"+courseID, adminToken); rr.Code != http.StatusOK {
		t.Errorf("the course was removed by an API key: %d", rr.Code)
	}
}

// A working key must get the limit it was configured with. The failure limiter
// used to record every key-authenticated request rather than only the failed
// ones, which silently capped any key at that limiter's 20/min however high
// API_KEY_RATE_LIMIT was set.
func TestValidKeyIsNotChargedToTheFailureLimiter(t *testing.T) {
	h, db := newTestServer(t, 1000)
	userID, _ := signUp(t, h, "scripter@example.com")
	key := mintKey(t, db, userID)

	for i := 1; i <= 40; i++ {
		rr := do(t, h, http.MethodGet, "/api/courses", key)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: status %d, want 200; body %s", i, rr.Code, rr.Body.String())
		}
	}
}

// Bad keys still are, and by IP, since they cost a query each.
func TestBadKeysAreRateLimitedByAddress(t *testing.T) {
	h, _ := newTestServer(t, 1000)

	var last int
	for i := 1; i <= 30; i++ {
		rr := do(t, h, http.MethodGet, "/api/courses", "rnd_"+strings.Repeat("a", 40))
		last = rr.Code
		if last == http.StatusTooManyRequests {
			break
		}
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("status = %d after 30 bad keys, want 429", last)
	}
}
