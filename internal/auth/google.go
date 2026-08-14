package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// userInfoEndpoint is the OIDC UserInfo endpoint. Reading the identity from here
// over TLS, using the access token we just exchanged, avoids having to verify
// id_token signatures locally and the JWKS-caching dependency that would need.
const userInfoEndpoint = "https://openidconnect.googleapis.com/v1/userinfo"

// ErrGoogleNotConfigured is returned when the instance has no Google credentials.
var ErrGoogleNotConfigured = errors.New("auth: google oauth is not configured on this instance")

// GoogleIdentity is the subset of the Google profile this app stores.
type GoogleIdentity struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Picture       string
}

// GoogleProvider wraps the OAuth2 client for one self-hosted instance's own
// Google client credentials.
type GoogleProvider struct {
	config  *oauth2.Config
	client  *http.Client
	enabled bool
}

// NewGoogleProvider builds a provider. Missing credentials yield a disabled
// provider rather than an error, so an instance can run password-only.
func NewGoogleProvider(clientID, clientSecret, redirectURL string) *GoogleProvider {
	if clientID == "" || clientSecret == "" || redirectURL == "" {
		return &GoogleProvider{enabled: false}
	}
	return &GoogleProvider{
		enabled: true,
		client:  &http.Client{Timeout: 10 * time.Second},
		config: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Endpoint:     google.Endpoint,
			Scopes:       []string{"openid", "email", "profile"},
		},
	}
}

func (g *GoogleProvider) Enabled() bool { return g.enabled }

// NewVerifier returns a PKCE code verifier to be held in a cookie for the
// duration of the redirect round trip.
func (g *GoogleProvider) NewVerifier() string { return oauth2.GenerateVerifier() }

// AuthCodeURL builds the Google consent URL for a given state and PKCE verifier.
func (g *GoogleProvider) AuthCodeURL(state, verifier string) (string, error) {
	if !g.enabled {
		return "", ErrGoogleNotConfigured
	}
	return g.config.AuthCodeURL(state,
		oauth2.AccessTypeOnline,
		oauth2.S256ChallengeOption(verifier),
	), nil
}

// Exchange trades an authorization code for the caller's Google identity.
func (g *GoogleProvider) Exchange(ctx context.Context, code, verifier string) (*GoogleIdentity, error) {
	if !g.enabled {
		return nil, ErrGoogleNotConfigured
	}

	token, err := g.config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("exchange google code: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userInfoEndpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build userinfo request: %w", err)
	}
	token.SetAuthHeader(req)

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch google userinfo: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read google userinfo: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google userinfo returned %s: %s", resp.Status, string(body))
	}

	var payload struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode google userinfo: %w", err)
	}
	if payload.Sub == "" {
		return nil, errors.New("google userinfo did not include a subject")
	}

	return &GoogleIdentity{
		Subject:       payload.Sub,
		Email:         payload.Email,
		EmailVerified: payload.EmailVerified,
		Name:          payload.Name,
		Picture:       payload.Picture,
	}, nil
}
