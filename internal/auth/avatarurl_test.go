package auth

import (
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testSigner() *AvatarSigner {
	return NewAvatarSigner([]byte("a-test-instance-secret"))
}

// parseSigned pulls the three parts a signed URL is made of back out of it.
func parseSigned(t *testing.T, signed string) (key, exp, sig string) {
	t.Helper()
	parsed, err := url.Parse(signed)
	if err != nil {
		t.Fatalf("parse %q: %v", signed, err)
	}
	key = strings.TrimSuffix(strings.TrimPrefix(parsed.Path, "/api/avatars/"), ".jpg")
	q := parsed.Query()
	return key, q.Get("exp"), q.Get("sig")
}

func TestAvatarURLIsNilWithoutAKey(t *testing.T) {
	signer := testSigner()
	if got := signer.URL(nil); got != nil {
		t.Errorf("URL(nil) = %v, want nil", *got)
	}
	empty := ""
	if got := signer.URL(&empty); got != nil {
		t.Errorf("URL(\"\") = %v, want nil", *got)
	}
}

func TestAvatarURLRoundTrips(t *testing.T) {
	signer := testSigner()
	key := "AbCdEfGhIjKlMnOpQrStUv"

	signed := signer.URL(&key)
	if signed == nil {
		t.Fatal("URL = nil, want a signed path")
	}
	gotKey, exp, sig := parseSigned(t, *signed)
	if gotKey != key {
		t.Errorf("path key = %q, want %q", gotKey, key)
	}

	expiry, err := signer.Verify(gotKey, exp, sig)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !expiry.After(time.Now()) {
		t.Errorf("expiry %v is not in the future", expiry)
	}
}

// The whole point of the signature: a URL from one instance must not open an
// avatar on another, and nobody can mint one without the secret.
func TestAvatarURLRejectsForgedSignatures(t *testing.T) {
	signer := testSigner()
	other := NewAvatarSigner([]byte("a-different-instance-secret"))
	key := "AbCdEfGhIjKlMnOpQrStUv"

	signed := signer.URL(&key)
	gotKey, exp, sig := parseSigned(t, *signed)

	cases := []struct {
		name          string
		key, exp, sig string
	}{
		{"no signature at all", gotKey, exp, ""},
		{"no expiry at all", gotKey, "", sig},
		{"tampered signature", gotKey, exp, strings.Repeat("A", len(sig))},
		{"expiry pushed out", gotKey, strconv.FormatInt(time.Now().Add(999*time.Hour).Unix(), 10), sig},
		// A signature is bound to its key, so one avatar's link cannot be
		// retargeted at another's.
		{"signature lifted onto another key", "ZZZZZZZZZZZZZZZZZZZZZZ", exp, sig},
		{"expiry not a number", gotKey, "not-a-number", sig},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := signer.Verify(tc.key, tc.exp, tc.sig); err == nil {
				t.Error("err = nil, want the URL rejected")
			}
		})
	}

	t.Run("signed by another instance", func(t *testing.T) {
		if _, err := other.Verify(gotKey, exp, sig); err == nil {
			t.Error("err = nil, want a foreign signature rejected")
		}
	})
}

// A leaked link has to stop working, which is the entire reason for the change.
func TestAvatarURLExpires(t *testing.T) {
	signer := testSigner()
	// Shrunk so the test does not have to sit out a real day.
	signer.window = 2 * time.Second
	signer.minLifetime = time.Millisecond

	key := "AbCdEfGhIjKlMnOpQrStUv"
	gotKey, exp, sig := parseSigned(t, *signer.URL(&key))
	if _, err := signer.Verify(gotKey, exp, sig); err != nil {
		t.Fatalf("fresh URL rejected: %v", err)
	}

	seconds, err := strconv.ParseInt(exp, 10, 64)
	if err != nil {
		t.Fatalf("parse exp: %v", err)
	}
	time.Sleep(time.Until(time.Unix(seconds, 0)) + 50*time.Millisecond)

	if _, err := signer.Verify(gotKey, exp, sig); err == nil {
		t.Error("err = nil, want the lapsed URL rejected")
	}
}

// Bucketing the expiry is what keeps the browser cache useful: two URLs minted
// minutes apart have to be byte-identical, or every /me response would bust the
// cache and re-download the image.
func TestAvatarURLIsStableWithinAWindow(t *testing.T) {
	signer := testSigner()
	key := "AbCdEfGhIjKlMnOpQrStUv"

	first, second := signer.URL(&key), signer.URL(&key)
	if *first != *second {
		t.Errorf("URLs differ within one window:\n  %s\n  %s", *first, *second)
	}
}

// Every minted URL has to be worth minting: an expiry a minute away would send
// the client a link that dies before the page finishes loading.
func TestAvatarURLGrantsAMinimumLifetime(t *testing.T) {
	signer := testSigner()

	// Walk a full day in ten-minute steps; every one has to clear the floor.
	start := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	for offset := time.Duration(0); offset < 24*time.Hour; offset += 10 * time.Minute {
		now := start.Add(offset)
		lifetime := signer.expiryFor(now).Sub(now)
		if lifetime < avatarURLMinLifetime {
			t.Fatalf("at %s the URL would live %s, want at least %s", now, lifetime, avatarURLMinLifetime)
		}
	}
}
