package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"strconv"
	"time"
)

// Avatar URL signing.
//
// An avatar is served to an <img> tag, which cannot carry a bearer token, and
// this SPA holds its access token in memory only. The unguessable key in the
// path was the whole of the protection: correct against enumeration, but it
// made every avatar URL a bearer token with no expiry. A link that leaked —
// into a screenshot, a synced browser history, a proxy log, a pasted message —
// worked forever, or at least until the photo was replaced.
//
// Signing bounds that. The URL now carries an expiry and an HMAC over it, so a
// leaked link stops working within a day or so. It is the presigned-URL pattern
// every object store uses, and it keeps the property that made the plain key
// work in the first place: it is still just a URL, so <img> still loads it, no
// cookie has to survive a cross-origin dev setup, and an API-key client can
// still fetch an avatar it was handed.
//
// What this does not do is tie an avatar to a session. Anyone holding a live
// URL can load it. That is the deliberate trade: making avatars session-bound
// would break sharing them, break API-key clients, and require a cookie that
// has to be Secure+None across the Vite dev origin. The expiry converts
// "forever" into "for a while", which is the part that actually mattered.

const (
	// avatarURLWindow is the granularity of the expiry, and the reason a signed
	// URL is stable rather than different on every request. Every expiry lands
	// on a 24h boundary, so the same key signs to the same URL all through a
	// window and the browser cache keeps hitting.
	avatarURLWindow = 24 * time.Hour

	// avatarURLMinLifetime is how much validity a freshly minted URL is
	// guaranteed. Without it, a URL signed at 23:59 would expire in a minute.
	// With it, lifetimes run from 12 to 36 hours, and the bucket flips at noon.
	avatarURLMinLifetime = 12 * time.Hour

	// avatarSigLen is the signature truncated to 16 bytes. A 128-bit MAC is far
	// past forgery, and the full 32 would double the length of a URL whose
	// whole job is to be short enough to sit in an <img src>.
	avatarSigLen = 16
)

var errBadAvatarSignature = errors.New("auth: invalid avatar URL signature")

// AvatarSigner mints and checks the query string on an avatar URL.
type AvatarSigner struct {
	key []byte

	// Both are fields rather than the constants so a test can prove expiry
	// without waiting half a day for it.
	window      time.Duration
	minLifetime time.Duration
}

// NewAvatarSigner derives an avatar-signing key from the instance secret.
//
// Derived rather than used directly: the same secret signs access tokens, and a
// key that signs two different kinds of thing is a key where a confusion
// between them becomes a forgery. The label is the domain separation.
func NewAvatarSigner(secret []byte) *AvatarSigner {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("roundly/avatar-url/v1"))
	return &AvatarSigner{
		key:         mac.Sum(nil),
		window:      avatarURLWindow,
		minLifetime: avatarURLMinLifetime,
	}
}

// URL builds the signed path for an avatar key, or nil when the user has no
// avatar.
//
// The path stays relative so it works through the Vite dev proxy and
// same-origin in production without configuration.
func (s *AvatarSigner) URL(key *string) *string {
	if key == nil || *key == "" {
		return nil
	}
	expiry := s.expiryFor(time.Now())
	query := url.Values{}
	query.Set("exp", strconv.FormatInt(expiry.Unix(), 10))
	query.Set("sig", s.sign(*key, expiry.Unix()))

	signed := "/api/avatars/" + *key + ".jpg?" + query.Encode()
	return &signed
}

// Verify checks a signature and returns when the URL stops being valid, so the
// caller can cap its cache header at exactly that moment.
func (s *AvatarSigner) Verify(key, rawExp, sig string) (time.Time, error) {
	seconds, err := strconv.ParseInt(rawExp, 10, 64)
	if err != nil {
		return time.Time{}, errBadAvatarSignature
	}

	// Compared before the clock, and in constant time: a signature check that
	// short-circuits on the first differing byte is one an attacker can walk.
	if !hmac.Equal([]byte(sig), []byte(s.sign(key, seconds))) {
		return time.Time{}, errBadAvatarSignature
	}

	expiry := time.Unix(seconds, 0)
	if !time.Now().Before(expiry) {
		return time.Time{}, errBadAvatarSignature
	}
	return expiry, nil
}

// expiryFor rounds up to the next window boundary that is at least minLifetime
// away, which is what makes the URL stable within a window.
func (s *AvatarSigner) expiryFor(now time.Time) time.Time {
	return now.Add(s.minLifetime).Truncate(s.window).Add(s.window)
}

func (s *AvatarSigner) sign(key string, expiry int64) string {
	mac := hmac.New(sha256.New, s.key)
	// The separator matters: without it, key "ab" + exp "1" and key "a" + exp
	// "b1" would hash identically. A newline cannot appear in either part.
	mac.Write([]byte(key))
	mac.Write([]byte("\n"))
	mac.Write([]byte(strconv.FormatInt(expiry, 10)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:avatarSigLen])
}
