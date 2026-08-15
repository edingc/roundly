// Package apikey implements personal, read-only API keys.
//
// A key authenticates as exactly one user and can do nothing but read. That is
// enforced in three independent layers, all of them in Guard and all of them
// running before chi has routed the request:
//
//  1. an explicit allow-list of paths, so an endpoint added tomorrow is
//     unreachable until somebody deliberately adds it here;
//  2. a GET/HEAD method check;
//  3. an outright block on /api/auth and /api/account.
//
// Any one of those would usually be enough. The point of having all three is
// that no single mistake is. The third is not redundant decoration: the account
// export is a GET, so a method check alone would let a "read-only" key download
// the user's entire account, and the key-management endpoints would let it
// enumerate their other credentials.
package apikey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	// TokenPrefix is what tells a key apart from a JWT without attempting to
	// parse either: a JWT always begins "eyJ".
	TokenPrefix = "rnd_"

	// 32 bytes of CSPRNG output, base64url encoded to 43 characters.
	tokenEntropyBytes = 32

	// PrefixLength is how much of the token is stored in the clear so the UI
	// can tell two keys apart. "rnd_" plus eight characters leaves around 200
	// bits still secret.
	PrefixLength = 12

	// hashDomain separates these digests from every other SHA-256 in the app.
	// Without it a refresh token and an API key with identical bytes would hash
	// identically, so any future code path that consulted the wrong table would
	// silently accept the wrong class of credential. With it, that bug cannot
	// be written.
	hashDomain = "roundly.api_key.v1|"
)

// NewToken mints a key, returning the secret, its storage hash, and the public
// prefix. The secret is shown to the user once and never recoverable after.
func NewToken() (token, hash, prefix string, err error) {
	buf := make([]byte, tokenEntropyBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", "", fmt.Errorf("generate api key: %w", err)
	}
	token = TokenPrefix + base64.RawURLEncoding.EncodeToString(buf)
	return token, HashToken(token), token[:PrefixLength], nil
}

// HashToken derives the value stored for a key.
//
// SHA-256 is correct here and argon2id would be wrong. A password KDF exists to
// make guessing a low-entropy human secret expensive; this token is 256 bits of
// CSPRNG output, so there is nothing to guess however fast the hash runs.
// Running argon2id — 19 MiB and two passes — on every API request would instead
// be a denial of service the server performs on itself.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(hashDomain + token))
	return hex.EncodeToString(sum[:])
}

// LooksLikeToken reports whether s is shaped like a key this server issued.
// Cheap and allocation-free: it runs on every request before any database work.
func LooksLikeToken(s string) bool {
	if !strings.HasPrefix(s, TokenPrefix) {
		return false
	}
	body := s[len(TokenPrefix):]
	if len(body) != base64.RawURLEncoding.EncodedLen(tokenEntropyBytes) {
		return false
	}
	for _, r := range body {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// SafePrefix returns the loggable head of a presented credential, or empty if
// it is not shaped like one of ours. Keeps a garbage or hostile value from
// being echoed into the log verbatim.
func SafePrefix(presented string) string {
	if !LooksLikeToken(presented) {
		return ""
	}
	return presented[:PrefixLength]
}
