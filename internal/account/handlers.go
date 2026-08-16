package account

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/edingc/roundly/internal/auth"
	"github.com/edingc/roundly/internal/httpx"
)

// Handler exposes the profile, data, and API key endpoints.
type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// Register attaches the account endpoints to r, which the caller is expected to
// have wrapped in auth middleware. Registered onto the caller's router rather
// than mounted at "/", which would install a catch-all and swallow unmatched
// /api paths — see the same note in internal/club.
//
// Everything under /account is unreachable by an API key, blocked wholesale by
// the guard in internal/apikey. That block is load-bearing: the export endpoint
// is a GET, so a method check alone would let a read-only key download the
// entire account.
func (h *Handler) Register(r chi.Router) {
	// The read-only view of the caller, deliberately outside /account so an
	// API key can reach it without weakening the block on everything else.
	r.Get("/me", h.me)

	r.Route("/account", func(ar chi.Router) {
		ar.Put("/profile", h.updateProfile)
		ar.Put("/email", h.changeEmail)
		ar.Post("/avatar", h.uploadAvatar)
		ar.Delete("/avatar", h.deleteAvatar)
		ar.Get("/export", h.export)
		ar.Post("/import", h.importAccount)
		// Registered inside this block rather than as a separate
		// r.Delete("/account", …): internal/apikey mounts /account/keys on the
		// same router, and the two would collide.
		ar.Delete("/", h.deleteAccount)
	})
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := h.service.reload(ctx, auth.MustUserID(ctx))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, user)
}

type profileRequest struct {
	FirstName       *string `json:"first_name"`
	LastName        *string `json:"last_name"`
	DisplayName     string  `json:"display_name"`
	HomeCourseID    *string `json:"home_course_id"`
	LocationCity    *string `json:"location_city"`
	LocationRegion  *string `json:"location_region"`
	LocationCountry *string `json:"location_country"`
}

func (h *Handler) updateProfile(w http.ResponseWriter, r *http.Request) {
	var req profileRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	v := httpx.NewValidator()

	// Only the display name is required. Someone who wants to be nothing but
	// "Cody" should not be made to supply a surname and a city to save.
	displayName := v.SingleLine("display_name", req.DisplayName)
	if v.Required("display_name", displayName) {
		v.MaxLen("display_name", displayName, maxDisplayLen)
	}

	first := optionalField(v, "first_name", req.FirstName, maxNameLen)
	last := optionalField(v, "last_name", req.LastName, maxNameLen)
	city := optionalField(v, "location_city", req.LocationCity, maxLocationLen)
	region := optionalField(v, "location_region", req.LocationRegion, maxLocationLen)
	country := optionalField(v, "location_country", req.LocationCountry, maxLocationLen)

	homeCourse := trimOptional(req.HomeCourseID)

	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	user, err := h.service.UpdateProfile(ctx, auth.MustUserID(ctx), ProfileInput{
		FirstName:       first,
		LastName:        last,
		DisplayName:     displayName,
		HomeCourseID:    homeCourse,
		LocationCity:    city,
		LocationRegion:  region,
		LocationCountry: country,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, user)
}

type emailRequest struct {
	Email           string `json:"email"`
	CurrentPassword string `json:"current_password"`
}

func (h *Handler) changeEmail(w http.ResponseWriter, r *http.Request) {
	var req emailRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	v := httpx.NewValidator()
	email := v.Email("email", req.Email)
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	principal, _ := auth.PrincipalFrom(ctx)
	session, err := h.service.ChangeEmail(ctx, auth.MustUserID(ctx), email, req.CurrentPassword, principal.IssuedAt)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, session)
}

// optionalField cleans and bounds an optional string, returning nil for a blank
// one so the column is cleared rather than set to "".
func optionalField(v *httpx.Validator, field string, value *string, max int) *string {
	if value == nil {
		return nil
	}
	cleaned := v.SingleLine(field, *value)
	if cleaned == "" {
		return nil
	}
	v.MaxLen(field, cleaned, max)
	return &cleaned
}

func trimOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// maxImportBytes is the cap on a restore payload. The default 1 MiB that
// httpx.Decode applies is sized for a single course and would reject a backup
// holding a few dozen of them, with a message that reads like a bug.
const maxImportBytes = 16 << 20

// export downloads everything the caller owns.
//
// Synchronous on purpose: this is four queries plus a bag read, which is
// milliseconds at any realistic size. A job queue and a "your file is ready"
// notification would be infrastructure standing in for a problem that does not
// exist here.
func (h *Handler) export(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	exp, err := h.service.Export(ctx, auth.MustUserID(ctx))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	stamp := time.Now().UTC().Format("2006-01-02")

	if strings.EqualFold(r.URL.Query().Get("format"), "csv") {
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="roundly-export-%s.zip"`, stamp))
		if err := writeCSVArchive(w, exp); err != nil {
			// The response is already committed by the time a zip write fails,
			// so there is no status left to change — log it and drop the
			// connection rather than appending an error to a corrupt archive.
			slog.Error("write csv export", "error", err)
		}
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="roundly-export-%s.json"`, stamp))
	httpx.JSON(w, http.StatusOK, exp)
}

// importAccount merges a backup into the caller's account.
func (h *Handler) importAccount(w http.ResponseWriter, r *http.Request) {
	var exp AccountExport
	if err := httpx.DecodeLimit(w, r, &exp, maxImportBytes); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	summary, err := h.service.Import(ctx, auth.MustUserID(ctx), &exp)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	// 200, not 201: a merge that skipped everything created nothing.
	httpx.JSON(w, http.StatusOK, summary)
}

type deleteAccountBody struct {
	CurrentPassword string `json:"current_password"`
}

func (h *Handler) deleteAccount(w http.ResponseWriter, r *http.Request) {
	var req deleteAccountBody
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	principal, _ := auth.PrincipalFrom(ctx)
	if err := h.service.DeleteAccount(ctx, auth.MustUserID(ctx), req.CurrentPassword, principal.IssuedAt); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}
