package auth

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

// handoffTTL is how long a browser has to redeem a completed OAuth login. It
// only has to cover one redirect plus one API call.
const handoffTTL = 2 * time.Minute

// handoffStore holds sessions produced by the Google callback until the SPA
// redeems them.
//
// The OAuth callback is a browser redirect, so the tokens cannot be returned in
// a response body. Rather than put a long-lived refresh token in a URL — where
// it lands in browser history, logs, and the Referer header — the callback
// redirects with a single-use code that the SPA immediately trades for the real
// session over XHR.
//
// In-memory is the right scope for this: entries live for two minutes, and a
// restart during that window just means the user taps the button again.
type handoffStore struct {
	mu      sync.Mutex
	entries map[string]handoffEntry
}

type handoffEntry struct {
	session   *Session
	expiresAt time.Time
}

func newHandoffStore() *handoffStore {
	return &handoffStore{entries: make(map[string]handoffEntry)}
}

// issue stores a session and returns its single-use redemption code.
func (s *handoffStore) issue(session *Session) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	code := base64.RawURLEncoding.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	s.entries[code] = handoffEntry{session: session, expiresAt: time.Now().Add(handoffTTL)}
	return code, nil
}

// redeem consumes a code, returning nil if it is unknown, expired, or reused.
func (s *handoffStore) redeem(code string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[code]
	delete(s.entries, code)
	if !ok || time.Now().After(entry.expiresAt) {
		return nil
	}
	return entry.session
}

func (s *handoffStore) pruneLocked() {
	now := time.Now()
	for code, entry := range s.entries {
		if now.After(entry.expiresAt) {
			delete(s.entries, code)
		}
	}
}
