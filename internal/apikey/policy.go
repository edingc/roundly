package apikey

import (
	"path"
	"strings"
)

// allowed is the complete set of paths an API key may reach.
//
// An allow-list, not a deny-list, and that direction is the whole point: a
// route added to any package tomorrow is invisible to every key until someone
// deliberately adds it here, and adding it here is a diff a reviewer notices.
// A deny-list fails the other way — silently, and in the attacker's favour.
//
// "{}" matches exactly one non-empty path segment.
//
// Deliberately absent, each for a reason worth stating:
//
//   - /api/auth/*: a key must never mint a session, read credentials state, or
//     start an OAuth flow. Also hard-blocked in Guard.
//   - /api/account/*: the export is a GET and would hand over the entire
//     account; the keys endpoint would enumerate the user's other credentials.
//     Also hard-blocked in Guard.
//   - /api/courses/{}/export: excluded by choice. It returns nothing that
//     GET /api/courses/{} does not, and leaving it off keeps bulk scraping of
//     the directory marginally less convenient.
//   - /api/tees/{} and /api/holes/{}: these accept only writes, so the method
//     guard already stops them. Omitting them anyway means two independent
//     controls must fail before a write could land.
var allowed = []string{
	"/api/health",
	"/api/me",
	"/api/courses",
	"/api/courses/{}",
	"/api/clubs",
	"/api/clubs/options",
	"/api/clubs/{}",
}

// blockedPrefixes are unreachable regardless of method or allow-list.
var blockedPrefixes = []string{"/api/auth", "/api/account"}

// CleanPath normalizes a request path for policy decisions.
//
// Returns ok=false for anything containing a traversal segment. chi does not
// clean paths the way http.ServeMux does, and r.URL.Path arrives already
// percent-decoded, so "%2e%2e" reaches here as "..". Refusing outright is safer
// than resolving it and hoping the result is what the router will match.
func CleanPath(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	for _, seg := range strings.Split(raw, "/") {
		if seg == ".." {
			return "", false
		}
	}
	p := path.Clean(raw)
	if p != "/" {
		p = strings.TrimSuffix(p, "/")
	}
	if p == "" {
		return "", false
	}
	return p, true
}

// IsBlocked reports whether a cleaned path is off limits to every key.
func IsBlocked(p string) bool {
	for _, prefix := range blockedPrefixes {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}

// Allowed reports whether a cleaned path is on the read-only allow-list.
func Allowed(p string) bool {
	got := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for _, pattern := range allowed {
		want := strings.Split(strings.TrimPrefix(pattern, "/"), "/")
		if len(want) != len(got) {
			continue
		}
		match := true
		for i := range want {
			if want[i] == "{}" {
				if got[i] == "" { // rejects a doubled slash
					match = false
					break
				}
				continue
			}
			if want[i] != got[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// IsReadMethod reports whether m is a method a read-only key may use.
//
// Written as an allow-list rather than "not a write verb" so that a method
// nobody thought about — PATCH, or something arriving from a future spec — is
// refused by default instead of admitted by omission.
func IsReadMethod(m string) bool {
	return m == "GET" || m == "HEAD"
}
