package apikey

import (
	"testing"

	"github.com/edingc/roundly/internal/auth"
)

func TestTokenShape(t *testing.T) {
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		token, hash, prefix, err := NewToken()
		if err != nil {
			t.Fatalf("NewToken: %v", err)
		}
		if !LooksLikeToken(token) {
			t.Fatalf("NewToken produced %q, which LooksLikeToken rejects", token)
		}
		if prefix != token[:PrefixLength] {
			t.Errorf("prefix %q is not the head of the token", prefix)
		}
		if hash == token {
			t.Fatal("the stored hash is the token itself")
		}
		if seen[token] {
			t.Fatalf("NewToken repeated a token")
		}
		seen[token] = true
	}
}

func TestLooksLikeToken(t *testing.T) {
	valid, _, _, _ := NewToken()
	tests := []struct {
		in   string
		want bool
	}{
		{valid, true},
		{"", false},
		{"rnd_", false},
		{"rnd_tooshort", false},
		// A JWT must never be mistaken for a key, which is the entire reason
		// for the prefix.
		{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxIn0.abc", false},
		{"Bearer rnd_xxx", false},
		{valid + "x", false},
		{"rnd_" + "!@#$%^&*()_+!@#$%^&*()_+!@#$%^&*()_+!@#$%^&", false},
	}
	for _, tc := range tests {
		if got := LooksLikeToken(tc.in); got != tc.want {
			t.Errorf("LooksLikeToken(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// The domain separator means an api_keys row and a refresh_tokens row can never
// be interchanged, even if a future code path consulted the wrong table.
func TestHashIsDomainSeparatedFromRefreshTokens(t *testing.T) {
	const sample = "rnd_the-same-bytes-in-both-places"
	if HashToken(sample) == auth.HashRefreshToken(sample) {
		t.Error("an API key and a refresh token hash identically; the domain separator is missing")
	}
	if HashToken(sample) != HashToken(sample) {
		t.Error("HashToken is not deterministic")
	}
}

func TestSafePrefix(t *testing.T) {
	valid, _, prefix, _ := NewToken()
	if got := SafePrefix(valid); got != prefix {
		t.Errorf("SafePrefix = %q, want %q", got, prefix)
	}
	// A value that is not one of ours is never echoed, so a hostile credential
	// cannot be written into the log verbatim.
	for _, bad := range []string{"", "not-a-key", "eyJhbGciOiJIUzI1NiJ9.aaa.bbb", "rnd_short"} {
		if got := SafePrefix(bad); got != "" {
			t.Errorf("SafePrefix(%q) = %q, want empty", bad, got)
		}
	}
}

func TestBearerToken(t *testing.T) {
	tests := []struct {
		auth, header, want string
	}{
		{"Bearer rnd_abc", "", "rnd_abc"},
		{"bearer rnd_abc", "", "rnd_abc"},
		{"", "rnd_abc", "rnd_abc"},
		// The dedicated header wins, so a page holding both is unambiguous.
		{"Bearer eyJhbGci", "rnd_abc", "rnd_abc"},
		{"", "", ""},
		{"rnd_abc", "", ""},
		{"Basic dXNlcjpwYXNz", "", ""},
	}
	for _, tc := range tests {
		if got := BearerToken(tc.auth, tc.header); got != tc.want {
			t.Errorf("BearerToken(%q, %q) = %q, want %q", tc.auth, tc.header, got, tc.want)
		}
	}
}
