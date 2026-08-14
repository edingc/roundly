package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	tokenIssuer   = "roundly"
	tokenAudience = "roundly-api"
)

var (
	// ErrInvalidToken covers any access token that fails signature, claim, or
	// expiry checks. The reason is deliberately not distinguished to the caller.
	ErrInvalidToken = errors.New("auth: invalid or expired token")
)

// Claims is the access token payload. Subject holds the user ID.
type Claims struct {
	jwt.RegisteredClaims
	Email string `json:"email"`
}

// TokenIssuer mints and verifies access tokens and generates refresh tokens.
type TokenIssuer struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewTokenIssuer(secret []byte, accessTTL, refreshTTL time.Duration) *TokenIssuer {
	return &TokenIssuer{secret: secret, accessTTL: accessTTL, refreshTTL: refreshTTL}
}

func (t *TokenIssuer) AccessTTL() time.Duration  { return t.accessTTL }
func (t *TokenIssuer) RefreshTTL() time.Duration { return t.refreshTTL }

// IssueAccessToken returns a signed HS256 JWT for the user.
func (t *TokenIssuer) IssueAccessToken(userID, email string) (string, time.Time, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(t.accessTTL)

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    tokenIssuer,
			Audience:  jwt.ClaimStrings{tokenAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			ID:        uuid.NewString(),
		},
		Email: email,
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return signed, expiresAt, nil
}

// ParseAccessToken verifies a token and returns its claims. The signing method
// is pinned to HS256 so a token claiming "alg":"none" cannot be accepted.
func (t *TokenIssuer) ParseAccessToken(raw string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		return t.secret, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(tokenIssuer),
		jwt.WithAudience(tokenAudience),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if claims.Subject == "" {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// NewRefreshToken returns an opaque high-entropy token plus its storage hash and
// expiry. Only the hash is persisted, so a database leak does not yield usable
// sessions. A plain SHA-256 is appropriate here (unlike for passwords) because
// the token is 256 bits of random data and not guessable.
func (t *TokenIssuer) NewRefreshToken() (token string, hash string, expiresAt time.Time, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", time.Time{}, fmt.Errorf("generate refresh token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(buf)
	return token, HashRefreshToken(token), time.Now().UTC().Add(t.refreshTTL), nil
}

// HashRefreshToken derives the lookup hash for an opaque refresh token.
func HashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
