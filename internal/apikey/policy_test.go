package apikey

import "testing"

func TestAllowed(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		// On the list.
		{"/api/health", true},
		{"/api/me", true},
		{"/api/courses", true},
		{"/api/courses/01a00613-f435-7185-97f2-64f0cb59f9da", true},
		{"/api/clubs", true},
		{"/api/clubs/options", true},
		{"/api/clubs/abc123", true},

		// Off it. Each of these is a route that exists and that a key must not
		// reach; if one ever starts returning true, something has been widened.
		{"/api/courses/abc/export", false},
		{"/api/courses/abc/tees", false},
		{"/api/courses/abc/holes", false},
		{"/api/courses/import", true}, // matches /api/courses/{}; the method guard is what stops this POST
		{"/api/tees/abc", false},
		{"/api/holes/abc", false},
		{"/api/holes/abc/tee-details/def", false},
		{"/api/clubs/abc/status", false},
		{"/api/auth/me", false},
		{"/api/account/export", false},
		{"/api/account/keys", false},
		{"/api/avatars/abc.jpg", false},

		// Shapes that must not match a single-segment wildcard.
		{"/api/courses/", false},
		{"/api/courses//abc", false},
		{"/api", false},
		{"/", false},
		{"", false},
		{"/api/COURSES", false},
	}

	for _, tc := range tests {
		if got := Allowed(tc.path); got != tc.want {
			t.Errorf("Allowed(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestIsBlocked(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/api/auth", true},
		{"/api/auth/me", true},
		{"/api/auth/login", true},
		{"/api/auth/google/callback", true},
		{"/api/account", true},
		{"/api/account/export", true},
		{"/api/account/keys", true},
		{"/api/account/avatar", true},

		// Must not over-reach into paths that merely share a prefix string.
		{"/api/accounts", false},
		{"/api/authorized", false},
		{"/api/clubs", false},
		{"/api/me", false},
	}

	for _, tc := range tests {
		if got := IsBlocked(tc.path); got != tc.want {
			t.Errorf("IsBlocked(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestCleanPath(t *testing.T) {
	tests := []struct {
		raw     string
		want    string
		wantOK  bool
		comment string
	}{
		{"/api/clubs", "/api/clubs", true, ""},
		{"/api/clubs/", "/api/clubs", true, "trailing slash normalized"},
		{"/api/clubs//", "/api/clubs", true, ""},
		{"/", "/", true, ""},

		// Traversal is refused rather than resolved. r.URL.Path arrives already
		// percent-decoded, so %2e%2e reaches us as "..".
		{"/api/auth/../clubs", "", false, ""},
		{"/api/../api/auth/me", "", false, ""},
		{"/api/clubs/..", "", false, ""},
		{"..", "", false, ""},
		{"", "", false, ""},
	}

	for _, tc := range tests {
		got, ok := CleanPath(tc.raw)
		if ok != tc.wantOK || (ok && got != tc.want) {
			t.Errorf("CleanPath(%q) = (%q, %v), want (%q, %v) %s",
				tc.raw, got, ok, tc.want, tc.wantOK, tc.comment)
		}
	}
}

func TestIsReadMethod(t *testing.T) {
	for _, m := range []string{"GET", "HEAD"} {
		if !IsReadMethod(m) {
			t.Errorf("IsReadMethod(%q) = false, want true", m)
		}
	}
	// An allow-list, so anything unanticipated is refused by default.
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS", "TRACE", "CONNECT", "get", ""} {
		if IsReadMethod(m) {
			t.Errorf("IsReadMethod(%q) = true, want false", m)
		}
	}
}

// Every path a key is allowed to reach must also survive the block check, or
// the two lists disagree and the allow-list is lying.
func TestAllowListAndBlockListAgree(t *testing.T) {
	for _, p := range allowed {
		concrete := p
		if len(p) > 2 && p[len(p)-2:] == "{}" {
			concrete = p[:len(p)-2] + "abc"
		}
		if IsBlocked(concrete) {
			t.Errorf("%q is on the allow-list but IsBlocked says otherwise", concrete)
		}
	}
}
