package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/edingc/roundly/internal/database/sqlc"
	"github.com/edingc/roundly/internal/httpx"
	"github.com/edingc/roundly/internal/id"
	"github.com/edingc/roundly/internal/timex"
)

const (
	// trustedDeviceTTL is how long "remember this device" lasts.
	//
	// Thirty days is the same span as a refresh token, which is not a
	// coincidence: a device that has to sign in again anyway is a device whose
	// trust may as well lapse at the same moment, so the two never disagree
	// about whether this browser is still known.
	trustedDeviceTTL = 30 * 24 * time.Hour

	// deviceTokenBytes is the same 256 bits a refresh token carries. This token
	// does not by itself grant access — it only skips the second factor, and
	// still needs the password — but it is a credential and is sized like one.
	deviceTokenBytes = 32

	// maxDeviceLabelLen bounds a string that comes from the client and is only
	// ever shown back to its owner.
	maxDeviceLabelLen = 120
)

// TrustedDevice is one remembered browser, as its owner sees it.
type TrustedDevice struct {
	ID    string  `json:"id"`
	Label *string `json:"label"`
	// Current marks the device making this request, so the list can say "this
	// one" rather than making somebody guess which row to keep.
	Current    bool    `json:"current"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
	ExpiresAt  string  `json:"expires_at"`
}

// hashDeviceToken derives the lookup hash for a device token. It is the same
// SHA-256 a refresh token gets, named separately because these are two
// different credentials that merely happen to want the same treatment.
func hashDeviceToken(token string) string { return HashRefreshToken(token) }

func newDeviceToken() (string, error) {
	buf := make([]byte, deviceTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate device token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// deviceTrusted reports whether this token names a live trusted device for the
// user, and records the use.
//
// A token for the wrong user, an expired one, or a made-up one all come back
// false with no error: this is a question about whether to skip the second
// factor, not an authentication that can fail.
func (s *Service) deviceTrusted(ctx context.Context, userID, token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}

	row, err := s.db.Queries.GetTrustedDevice(ctx, sqlc.GetTrustedDeviceParams{
		TokenHash: hashDeviceToken(token),
		UserID:    userID,
		ExpiresAt: timex.Now(),
	})
	if err != nil {
		return false
	}

	// Best-effort: failing to update the timestamp is not a reason to demand a
	// code from someone whose device is genuinely trusted.
	now := timex.Now()
	if err := s.db.Queries.TouchTrustedDevice(ctx, sqlc.TouchTrustedDeviceParams{
		LastUsedAt: &now,
		ID:         row.ID,
	}); err != nil {
		return true
	}
	return true
}

// rememberDevice mints a device token for the user and returns it. The caller
// hands it to the client, which sends it on the next sign-in.
func (s *Service) rememberDevice(ctx context.Context, userID, label string) (string, error) {
	token, err := newDeviceToken()
	if err != nil {
		return "", httpx.Internal(err)
	}

	var stored *string
	if trimmed := strings.TrimSpace(label); trimmed != "" {
		// Truncated by runes, not bytes. A User-Agent is ASCII in practice, but
		// slicing a byte at a time is how a string acquires a replacement
		// character the first time that assumption is wrong.
		if runes := []rune(trimmed); len(runes) > maxDeviceLabelLen {
			trimmed = string(runes[:maxDeviceLabelLen])
		}
		// Stripped of line breaks: it is displayed, and it came from a header.
		trimmed = strings.NewReplacer("\r", " ", "\n", " ").Replace(trimmed)
		stored = &trimmed
	}

	if err := s.db.Queries.CreateTrustedDevice(ctx, sqlc.CreateTrustedDeviceParams{
		ID:        id.New(),
		UserID:    userID,
		TokenHash: hashDeviceToken(token),
		Label:     stored,
		ExpiresAt: timex.Format(time.Now().UTC().Add(trustedDeviceTTL)),
		CreatedAt: timex.Now(),
	}); err != nil {
		return "", httpx.Internal(fmt.Errorf("remember device: %w", err))
	}
	return token, nil
}

// ListTrustedDevices returns the user's remembered devices, newest first.
func (s *Service) ListTrustedDevices(ctx context.Context, userID, currentToken string) ([]TrustedDevice, error) {
	rows, err := s.db.Queries.ListTrustedDevices(ctx, sqlc.ListTrustedDevicesParams{
		UserID:    userID,
		ExpiresAt: timex.Now(),
	})
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list trusted devices: %w", err))
	}

	currentHash := ""
	if trimmed := strings.TrimSpace(currentToken); trimmed != "" {
		currentHash = hashDeviceToken(trimmed)
	}

	devices := make([]TrustedDevice, 0, len(rows))
	for _, row := range rows {
		devices = append(devices, TrustedDevice{
			ID:         row.ID,
			Label:      row.Label,
			Current:    currentHash != "" && row.TokenHash == currentHash,
			CreatedAt:  row.CreatedAt,
			LastUsedAt: row.LastUsedAt,
			ExpiresAt:  row.ExpiresAt,
		})
	}
	return devices, nil
}

// ForgetDevice drops one remembered device.
func (s *Service) ForgetDevice(ctx context.Context, userID, deviceID string) error {
	if err := s.db.Queries.DeleteTrustedDevice(ctx, sqlc.DeleteTrustedDeviceParams{
		ID:     deviceID,
		UserID: userID,
	}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return httpx.NotFound("That device is not on your list.")
		}
		return httpx.Internal(fmt.Errorf("forget device: %w", err))
	}
	return nil
}

// ForgetAllDevices drops every remembered device for a user.
//
// Called whenever the credentials behind that trust change — a new password, a
// new address, two-factor switched off — because trust granted under the old
// ones should not outlive them.
func (s *Service) ForgetAllDevices(ctx context.Context, userID string) error {
	if err := s.db.Queries.DeleteAllTrustedDevices(ctx, userID); err != nil {
		return httpx.Internal(fmt.Errorf("forget all devices: %w", err))
	}
	return nil
}
