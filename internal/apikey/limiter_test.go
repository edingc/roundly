package apikey

import (
	"sync"
	"testing"
	"time"

	"github.com/edingc/roundly/internal/auth"
)

func TestLimiterAllowsUpToLimit(t *testing.T) {
	l := NewLimiter(3, time.Minute)
	now := time.Now()

	for i := 1; i <= 3; i++ {
		ok, remaining, _ := l.Allow("k", now)
		if !ok {
			t.Fatalf("request %d was rejected inside the limit", i)
		}
		if want := 3 - i; remaining != want {
			t.Errorf("request %d: remaining = %d, want %d", i, remaining, want)
		}
	}

	ok, remaining, resetAt := l.Allow("k", now)
	if ok {
		t.Error("the fourth request was allowed past a limit of 3")
	}
	if remaining != 0 {
		t.Errorf("remaining = %d, want 0", remaining)
	}
	if !resetAt.After(now) {
		t.Error("resetAt should be in the future")
	}
}

func TestLimiterWindowRollover(t *testing.T) {
	l := NewLimiter(2, time.Minute)
	now := time.Now()

	l.Allow("k", now)
	l.Allow("k", now)
	if ok, _, _ := l.Allow("k", now); ok {
		t.Fatal("expected the third request in the window to be refused")
	}

	// A clock parameter rather than a sleep: the test states the passage of
	// time instead of waiting for it.
	later := now.Add(time.Minute + time.Second)
	if ok, remaining, _ := l.Allow("k", later); !ok || remaining != 1 {
		t.Errorf("after the window rolled over: ok=%v remaining=%d, want true and 1", ok, remaining)
	}
}

func TestLimiterKeysAreIndependent(t *testing.T) {
	l := NewLimiter(1, time.Minute)
	now := time.Now()

	if ok, _, _ := l.Allow("a", now); !ok {
		t.Fatal("first request for a was refused")
	}
	if ok, _, _ := l.Allow("a", now); ok {
		t.Fatal("second request for a should have been refused")
	}
	if ok, _, _ := l.Allow("b", now); !ok {
		t.Error("b was refused because a had exhausted its own bucket")
	}
}

func TestLimiterSweepDropsElapsedBuckets(t *testing.T) {
	l := NewLimiter(5, time.Minute)
	now := time.Now()
	l.Allow("old", now)
	l.Allow("new", now.Add(30*time.Second))

	// 70s after "old" started (elapsed) but only 40s after "new" did (live).
	l.Sweep(now.Add(70 * time.Second))

	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.buckets["old"]; ok {
		t.Error("an elapsed bucket survived the sweep")
	}
	if _, ok := l.buckets["new"]; !ok {
		t.Error("a live bucket was swept away")
	}
}

func TestLimiterIsConcurrencySafe(t *testing.T) {
	l := NewLimiter(1000, time.Minute)
	now := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				l.Allow("shared", now)
			}
		}()
	}
	wg.Wait()

	l.mu.Lock()
	defer l.mu.Unlock()
	if got := l.buckets["shared"].count; got != 1000 {
		t.Errorf("count = %d, want 1000 — a lost update means the lock is wrong", got)
	}
}

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
