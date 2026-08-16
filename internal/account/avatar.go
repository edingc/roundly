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
	"time"

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
// correct: the bytes behind a given key genuinely never change, so a browser
// can hold it for as long as the link lives, and replacing the image
// invalidates every cache by changing the URL rather than by asking nicely.
// It also means a link shared from an old avatar stops resolving immediately,
// rather than waiting out the signature that auth.AvatarSigner puts on it.
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

// ServeAvatar returns an avatar image by its key, if the URL's signature is
// still good.
//
// Registered outside the authenticated group on purpose: an <img> tag cannot
// send an Authorization header, and this SPA keeps its access token in memory
// only. What stands in for authentication is the signed, expiring URL the
// server hands out with the user — see auth.AvatarSigner for why that shape and
// not a session cookie. Two things have to hold: the key has to name a real
// avatar, and the query string has to be one this instance signed and has not
// yet let lapse.
func (h *Handler) ServeAvatar(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	key, ok := strings.CutSuffix(name, ".jpg")
	if !ok || !validAvatarKey(key) {
		httpx.Error(w, r, httpx.NotFound("That image does not exist."))
		return
	}

	// Checked before the database is touched, so an unsigned request costs a
	// hash rather than a query on the one write connection this server has.
	query := r.URL.Query()
	expiry, err := h.service.auth.Avatars().Verify(key, query.Get("exp"), query.Get("sig"))
	if err != nil {
		// Not 403: a caller who cannot present a valid link has no business
		// learning whether the key behind it names anything. This is the same
		// answer a made-up key gets.
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
	writeAvatarPrivacyHeaders(w, expiry)
	http.ServeContent(w, r, name, modTime, bytes.NewReader(row.Image))
}

// writeAvatarPrivacyHeaders keeps a personal photo out of the places it would
// otherwise drift into.
//
// Each line is a different leak. `private` stops a shared proxy or a CDN from
// holding a copy that outlives the signature — the old `public` was wrong the
// moment these stopped being world-readable. The max-age stops at the exact
// second the URL does, so no cache can serve a picture whose link has lapsed.
// X-Robots-Tag keeps it out of image search if a link ever reaches a crawler,
// and no-referrer means following one of these leaks nothing about where it
// was found. The image itself is `immutable` in the only sense that matters:
// the key rotates on every upload, so the bytes behind a given key never do.
func writeAvatarPrivacyHeaders(w http.ResponseWriter, expiry time.Time) {
	maxAge := int(time.Until(expiry).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	w.Header().Set("Cache-Control", fmt.Sprintf("private, max-age=%d, immutable", maxAge))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noimageindex")
	w.Header().Set("Referrer-Policy", "no-referrer")
	// Navigating straight to the image should render a picture and nothing
	// else — no script, no frame, no subresource of any kind.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; sandbox")
}
