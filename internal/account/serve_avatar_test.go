package account

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// serveAvatars mounts the one unauthenticated route this package exposes, the
// way the router does.
func serveAvatars(svc *Service) http.Handler {
	r := chi.NewRouter()
	r.Get("/api/avatars/{name}", NewHandler(svc).ServeAvatar)
	return r
}

// uploadTestAvatar stores an image for a user and returns the signed URL the
// API would have handed the client.
func uploadTestAvatar(t *testing.T, svc *Service, userID string) string {
	t.Helper()
	user, err := svc.SetAvatar(context.Background(), userID, solidJPEG(t, 400, 400, false))
	if err != nil {
		t.Fatalf("set avatar: %v", err)
	}
	if user.AvatarURL == nil {
		t.Fatal("avatar_url = nil after upload")
	}
	return *user.AvatarURL
}

func TestServeAvatarReturnsTheImageForASignedURL(t *testing.T) {
	svc, db := newTestService(t)
	userID := createUser(t, db, "player@example.com")
	signed := uploadTestAvatar(t, svc, userID)

	rec := httptest.NewRecorder()
	serveAvatars(svc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, signed, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != avatarContentType {
		t.Errorf("Content-Type = %q, want %q", got, avatarContentType)
	}
	if rec.Body.Len() == 0 {
		t.Error("body is empty, want image bytes")
	}
}

// A personal photo must not sit in a shared cache, get indexed, or leak the
// page it was loaded from.
func TestServeAvatarSetsPrivacyHeaders(t *testing.T) {
	svc, db := newTestService(t)
	userID := createUser(t, db, "player@example.com")
	signed := uploadTestAvatar(t, svc, userID)

	rec := httptest.NewRecorder()
	serveAvatars(svc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, signed, nil))

	cacheControl := rec.Header().Get("Cache-Control")
	if !strings.HasPrefix(cacheControl, "private,") {
		t.Errorf("Cache-Control = %q, want it to start private", cacheControl)
	}
	if strings.Contains(cacheControl, "public") {
		t.Errorf("Cache-Control = %q, want no public caching", cacheControl)
	}
	if got := rec.Header().Get("X-Robots-Tag"); !strings.Contains(got, "noindex") {
		t.Errorf("X-Robots-Tag = %q, want noindex", got)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q, want no-referrer", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

// The signature is the access control. Without it the key alone must not open
// the image, or nothing has changed.
func TestServeAvatarRejectsUnsignedAndTamperedURLs(t *testing.T) {
	svc, db := newTestService(t)
	userID := createUser(t, db, "player@example.com")
	signed := uploadTestAvatar(t, svc, userID)

	parsed, err := url.Parse(signed)
	if err != nil {
		t.Fatalf("parse signed URL: %v", err)
	}
	query := parsed.Query()

	withQuery := func(mutate func(url.Values)) string {
		q := url.Values{}
		for k, v := range query {
			q[k] = append([]string(nil), v...)
		}
		mutate(q)
		if len(q) == 0 {
			return parsed.Path
		}
		return parsed.Path + "?" + q.Encode()
	}

	cases := []struct {
		name string
		path string
	}{
		// This is exactly the URL that used to work forever.
		{"bare key, no query at all", parsed.Path},
		{"signature removed", withQuery(func(q url.Values) { q.Del("sig") })},
		{"expiry removed", withQuery(func(q url.Values) { q.Del("exp") })},
		{"signature altered", withQuery(func(q url.Values) { q.Set("sig", strings.Repeat("A", 22)) })},
		{"expiry extended", withQuery(func(q url.Values) { q.Set("exp", "99999999999") })},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			serveAvatars(svc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404", rec.Code)
			}
		})
	}
}

// Replacing a photo has to break every link to the old one, which is what the
// rotating key has always been for.
func TestServeAvatarStopsServingAReplacedImage(t *testing.T) {
	svc, db := newTestService(t)
	userID := createUser(t, db, "player@example.com")

	old := uploadTestAvatar(t, svc, userID)
	replacement := uploadTestAvatar(t, svc, userID)
	if old == replacement {
		t.Fatal("the URL did not change when the image was replaced")
	}

	rec := httptest.NewRecorder()
	serveAvatars(svc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, old, nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("old URL status = %d, want 404", rec.Code)
	}

	rec = httptest.NewRecorder()
	serveAvatars(svc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, replacement, nil))
	if rec.Code != http.StatusOK {
		t.Errorf("new URL status = %d, want 200", rec.Code)
	}
}
