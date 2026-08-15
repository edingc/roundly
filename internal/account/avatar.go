package account

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/edingc/roundly/internal/auth"
	"github.com/edingc/roundly/internal/database/sqlc"
	"github.com/edingc/roundly/internal/httpx"
	"github.com/edingc/roundly/internal/timex"
)

// avatarKeyLen is the encoded length of 16 random bytes in base64url.
const avatarKeyLen = 22

// newAvatarKey mints the unguessable stem an avatar is served under.
//
// Rotating this on every upload is what makes the immutable cache header
// correct: the bytes behind a given URL genuinely never change, so a browser,
// a CDN, or the service worker can hold it forever, and replacing the image
// invalidates every cache by changing the URL rather than by asking nicely.
// It also means a link shared from an old avatar stops resolving.
func newAvatarKey() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate avatar key: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// validAvatarKey reports whether s is shaped like a key this server minted.
//
// Checked before the key reaches a query, so a path traversal attempt, a
// wildcard, or an injection payload is rejected on its shape alone.
func validAvatarKey(s string) bool {
	if len(s) != avatarKeyLen {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// SetAvatar stores a processed image and rotates the user's avatar key.
func (s *Service) SetAvatar(ctx context.Context, userID string, raw []byte) (*auth.User, error) {
	processed, err := processAvatar(raw)
	if err != nil {
		var bad errBadImage
		if errors.As(err, &bad) {
			return nil, httpx.ValidationError(map[string]string{"avatar": bad.msg})
		}
		return nil, httpx.Internal(err)
	}

	key, err := newAvatarKey()
	if err != nil {
		return nil, httpx.Internal(err)
	}
	now := timex.Now()

	// One transaction: with the bytes in the database there is no file that
	// could end up out of step with the row that points at it.
	if err := s.db.InTx(func(q *sqlc.Queries) error {
		if err := q.UpsertUserAvatar(ctx, sqlc.UpsertUserAvatarParams{
			UserID:      userID,
			Image:       processed,
			ContentType: avatarContentType,
			ByteSize:    int64(len(processed)),
			UpdatedAt:   now,
		}); err != nil {
			return fmt.Errorf("store avatar: %w", err)
		}
		return q.SetUserAvatarKey(ctx, sqlc.SetUserAvatarKeyParams{
			AvatarKey: &key,
			UpdatedAt: now,
			ID:        userID,
		})
	}); err != nil {
		return nil, httpx.Internal(err)
	}

	return s.reload(ctx, userID)
}

// ClearAvatar removes the image and the key that served it.
func (s *Service) ClearAvatar(ctx context.Context, userID string) (*auth.User, error) {
	if err := s.db.InTx(func(q *sqlc.Queries) error {
		if err := q.DeleteUserAvatar(ctx, userID); err != nil {
			return fmt.Errorf("delete avatar: %w", err)
		}
		return q.SetUserAvatarKey(ctx, sqlc.SetUserAvatarKeyParams{
			AvatarKey: nil,
			UpdatedAt: timex.Now(),
			ID:        userID,
		})
	}); err != nil {
		return nil, httpx.Internal(err)
	}
	return s.reload(ctx, userID)
}

func (h *Handler) uploadAvatar(w http.ResponseWriter, r *http.Request) {
	// Required: httpx.Decode's body cap is on the JSON path and does nothing
	// for a multipart upload.
	r.Body = http.MaxBytesReader(w, r.Body, MaxAvatarUploadBytes)

	if err := r.ParseMultipartForm(MaxAvatarUploadBytes); err != nil {
		httpx.Error(w, r, avatarUploadError(err))
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	file, _, err := r.FormFile("avatar")
	if err != nil {
		httpx.Error(w, r, httpx.BadRequest("Attach an image in the \"avatar\" field."))
		return
	}
	defer func() { _ = file.Close() }()

	raw, err := io.ReadAll(file)
	if err != nil {
		httpx.Error(w, r, avatarUploadError(err))
		return
	}

	ctx := r.Context()
	user, err := h.service.SetAvatar(ctx, auth.MustUserID(ctx), raw)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, user)
}

func (h *Handler) deleteAvatar(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := h.service.ClearAvatar(ctx, auth.MustUserID(ctx))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, user)
}

func avatarUploadError(err error) error {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return httpx.ValidationError(map[string]string{
			"avatar": fmt.Sprintf("That image is larger than %d MB.", MaxAvatarUploadBytes>>20),
		})
	}
	return httpx.BadRequest("That upload could not be read.")
}

// ServeAvatar returns an avatar image by its key.
//
// Registered outside the authenticated group on purpose: an <img> tag cannot
// send an Authorization header, and this SPA keeps its access token in memory
// only. The unguessable, rotating key in the path is what stands in for
// authentication — the same trade-off Gravatar and every CDN-hosted avatar
// makes. Anyone holding the URL can view that image until it is replaced.
func (h *Handler) ServeAvatar(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	key, ok := strings.CutSuffix(name, ".jpg")
	if !ok || !validAvatarKey(key) {
		httpx.Error(w, r, httpx.NotFound("That image does not exist."))
		return
	}

	row, err := h.service.db.Queries.GetAvatarByKey(r.Context(), &key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, r, httpx.NotFound("That image does not exist."))
			return
		}
		httpx.Error(w, r, httpx.Internal(fmt.Errorf("load avatar: %w", err)))
		return
	}

	modTime, _ := timex.Parse(row.UpdatedAt)
	w.Header().Set("Content-Type", row.ContentType)
	// Safe to cache forever precisely because the key rotates on replacement.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, modTime, bytes.NewReader(row.Image))
}
