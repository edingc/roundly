// Package config loads runtime configuration from the environment.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr        string
	DatabaseURL string
	Env         string

	JWTSecret       []byte
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration

	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string

	// PublicURL is where the browser reaches this instance. Used to bounce the
	// user back to the SPA after the Google redirect lands on the API.
	PublicURL string

	CORSOrigins []string
}

// GoogleEnabled reports whether this instance has been given Google OAuth
// credentials. The frontend hides the Google button when it hasn't.
func (c *Config) GoogleEnabled() bool {
	return c.GoogleClientID != "" && c.GoogleClientSecret != "" && c.GoogleRedirectURL != ""
}

func (c *Config) IsProd() bool { return c.Env == "production" }

func Load() (*Config, error) {
	c := &Config{
		Addr:               env("ADDR", ":8080"),
		DatabaseURL:        env("DATABASE_URL", "roundly.db"),
		Env:                env("ENV", "development"),
		GoogleClientID:     env("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret: env("GOOGLE_CLIENT_SECRET", ""),
		GoogleRedirectURL:  env("GOOGLE_REDIRECT_URL", ""),
		PublicURL:          strings.TrimRight(env("PUBLIC_URL", "http://localhost:5173"), "/"),
	}

	accessTTL, err := envDuration("ACCESS_TOKEN_TTL", 15*time.Minute)
	if err != nil {
		return nil, err
	}
	c.AccessTokenTTL = accessTTL

	refreshTTL, err := envDuration("REFRESH_TOKEN_TTL", 30*24*time.Hour)
	if err != nil {
		return nil, err
	}
	c.RefreshTokenTTL = refreshTTL

	secret := env("JWT_SECRET", "")
	if secret == "" {
		if c.IsProd() {
			return nil, fmt.Errorf("JWT_SECRET must be set when ENV=production")
		}
		// Dev convenience: a random secret means restarting invalidates sessions,
		// which is fine locally and avoids shipping a default signing key.
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			return nil, fmt.Errorf("generate dev jwt secret: %w", err)
		}
		secret = hex.EncodeToString(buf)
	}
	if len(secret) < 32 && c.IsProd() {
		return nil, fmt.Errorf("JWT_SECRET must be at least 32 characters")
	}
	c.JWTSecret = []byte(secret)

	origins := env("CORS_ORIGINS", "http://localhost:5173,http://localhost:8080")
	for _, o := range strings.Split(origins, ",") {
		if o = strings.TrimSpace(o); o != "" {
			c.CORSOrigins = append(c.CORSOrigins, strings.TrimRight(o, "/"))
		}
	}

	return c, nil
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def, nil
	}
	if d, err := time.ParseDuration(raw); err == nil {
		return d, nil
	}
	// Also accept a bare number of seconds.
	secs, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid duration %q", key, raw)
	}
	return time.Duration(secs) * time.Second, nil
}
