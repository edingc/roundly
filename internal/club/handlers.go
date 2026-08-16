package club

import (
	"fmt"
	"slices"
	"strings"

	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/edingc/roundly/internal/auth"
	"github.com/edingc/roundly/internal/httpx"
)

// Loft bounds. A putter sits near 3 degrees and a lob wedge near 64, so this
// range is wide enough for anything real while still catching a yardage typed
// into the loft box.
const (
	minLoft = 0.0
	maxLoft = 75.0

	// Carry bounds in yards. The ceiling clears the longest realistic drive
	// while still catching a total distance typed in metres or a stray loft.
	minCarry = 1
	maxCarry = 400

	// Dispersion bounds in yards. Zero is allowed — a player who has not
	// measured a spread yet may legitimately enter it as unknown-but-tight —
	// and the ceiling catches a carry typed into the wrong box.
	minDispersion = 0
	maxDispersion = 150
)

// Handler exposes the golf bag endpoints.
type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// Register attaches the club endpoints to r, which the caller is expected to
// have wrapped in auth middleware. As with the course handler, these are
// registered onto the caller's router rather than mounted at "/", which would
// install a catch-all and swallow unmatched /api paths.
func (h *Handler) Register(r chi.Router) {
	r.Route("/clubs", func(cr chi.Router) {
		cr.Get("/", h.bag)
		cr.Post("/", h.create)
		cr.Get("/options", h.options)

		cr.Route("/{clubID}", func(dr chi.Router) {
			dr.Get("/", h.get)
			dr.Put("/", h.update)
			dr.Put("/status", h.setStatus)
			dr.Delete("/", h.delete)
		})
	})
}

func (h *Handler) bag(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bag, err := h.service.Bag(ctx, auth.MustUserID(ctx))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, bag)
}

// options lets the frontend build its selects from the server's own lists
// rather than a hand-copied duplicate that can drift.
func (h *Handler) options(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, Options{Types: Types, Flexes: Flexes, Limit: ClubLimit})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, err := h.service.Get(ctx, auth.MustUserID(ctx), chi.URLParam(r, "clubID"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, c)
}

type clubRequest struct {
	Type              string   `json:"club_type"`
	Label             string   `json:"label"`
	Brand             *string  `json:"brand"`
	Model             *string  `json:"model"`
	Loft              *float64 `json:"loft"`
	Shaft             *string  `json:"shaft"`
	Flex              *string  `json:"flex"`
	Notes             *string  `json:"notes"`
	ExpectedCarry     *int     `json:"expected_carry"`
	AverageDispersion *int     `json:"average_dispersion"`
	DisplayOrder      *int     `json:"display_order"`
	// Status is honored on create only, so a club can be added straight to the
	// bench. Update ignores it in favor of the status endpoint.
	Status string `json:"status"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req clubRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	v := httpx.NewValidator()
	input := validateClub(v, req)
	if req.Status != "" {
		input.Status = validateStatus(v, "status", req.Status)
	}
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	c, err := h.service.Create(ctx, auth.MustUserID(ctx), input)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, c)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req clubRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	v := httpx.NewValidator()
	input := validateClub(v, req)
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	c, err := h.service.Update(ctx, auth.MustUserID(ctx), chi.URLParam(r, "clubID"), input)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, c)
}

type statusRequest struct {
	Status string `json:"status"`
}

// setStatus is the one way a club moves between the bag, the bench, and
// retirement.
func (h *Handler) setStatus(w http.ResponseWriter, r *http.Request) {
	var req statusRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	v := httpx.NewValidator()
	status := validateStatus(v, "status", req.Status)
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	c, err := h.service.SetStatus(ctx, auth.MustUserID(ctx), chi.URLParam(r, "clubID"), status)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, c)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := h.service.Delete(ctx, auth.MustUserID(ctx), chi.URLParam(r, "clubID")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func validateClub(v *httpx.Validator, req clubRequest) ClubInput {
	v.Required("label", req.Label)
	v.MaxLen("label", strings.TrimSpace(req.Label), 60)

	// Required reports a blank type on its own; the list of valid values is
	// only worth adding when something was actually supplied.
	clubType := strings.ToLower(strings.TrimSpace(req.Type))
	if v.Required("club_type", clubType) && !slices.Contains(Types, clubType) {
		v.Add("club_type", fmt.Sprintf("Club type must be one of: %s.", strings.Join(Types, ", ")))
	}

	if req.Loft != nil && (*req.Loft < minLoft || *req.Loft > maxLoft) {
		v.Add("loft", fmt.Sprintf("Loft is usually between %g and %g degrees.", minLoft, maxLoft))
	}

	flex := req.Flex
	if flex != nil {
		normalized := strings.ToLower(strings.TrimSpace(*flex))
		if normalized == "" {
			flex = nil
		} else if !slices.Contains(Flexes, normalized) {
			v.Add("flex", fmt.Sprintf("Flex must be one of: %s.", strings.Join(Flexes, ", ")))
		} else {
			flex = &normalized
		}
	}

	if req.Brand != nil {
		v.MaxLen("brand", strings.TrimSpace(*req.Brand), 60)
	}
	if req.Model != nil {
		v.MaxLen("model", strings.TrimSpace(*req.Model), 60)
	}
	if req.Shaft != nil {
		v.MaxLen("shaft", strings.TrimSpace(*req.Shaft), 120)
	}
	if req.Notes != nil {
		v.MaxLen("notes", strings.TrimSpace(*req.Notes), 2000)
	}

	validateDistances(v, clubType, req.ExpectedCarry, req.AverageDispersion)

	return ClubInput{
		Type:              clubType,
		Label:             req.Label,
		Brand:             req.Brand,
		Model:             req.Model,
		Loft:              req.Loft,
		Shaft:             req.Shaft,
		Flex:              flex,
		Notes:             req.Notes,
		ExpectedCarry:     req.ExpectedCarry,
		AverageDispersion: req.AverageDispersion,
		DisplayOrder:      req.DisplayOrder,
	}
}

// validateDistances bounds carry and dispersion, and refuses either on a
// putter. Refusing rather than quietly nulling them means a client that
// re-types a club as a putter without clearing the boxes is told so, instead of
// silently losing numbers the player entered.
func validateDistances(v *httpx.Validator, clubType string, carry, dispersion *int) {
	if clubType == NoDistanceType {
		if carry != nil {
			v.Add("expected_carry", "A putter does not carry, so leave this empty.")
		}
		if dispersion != nil {
			v.Add("average_dispersion", "A putter has no shot dispersion, so leave this empty.")
		}
		return
	}
	if carry != nil {
		v.IntBetween("expected_carry", *carry, minCarry, maxCarry)
	}
	if dispersion != nil {
		v.IntBetween("average_dispersion", *dispersion, minDispersion, maxDispersion)
	}
}

func validateStatus(v *httpx.Validator, field, raw string) Status {
	status := Status(strings.ToLower(strings.TrimSpace(raw)))
	switch status {
	case StatusActive, StatusBenched, StatusRetired:
		return status
	default:
		v.Add(field, "Status must be active, benched, or retired.")
		return ""
	}
}

// ImportClub is one club as a backup file describes it.
//
// A separate type from clubRequest because the two are different contracts that
// happen to overlap: this one comes from a file rather than a form, and gaining
// a field here should not silently change what the API accepts.
type ImportClub struct {
	Type              string
	Label             string
	Brand             *string
	Model             *string
	Loft              *float64
	Shaft             *string
	Flex              *string
	Notes             *string
	ExpectedCarry     *int
	AverageDispersion *int
}

// ValidateImport checks a club from a backup against the same rules the API
// enforces, mirroring course.ValidateImport.
//
// It exists because the account importer wrote clubs straight to the database.
// A hand-edited file could therefore store what the API would refuse — a club
// type outside the list, a 900-degree loft, a flex nobody recognises, a note
// bounded only by the request body — and the app would then render values it
// believes cannot exist. Routing both paths through validateClub is what stops
// the two from drifting apart, which is the same argument the course importer
// already makes in its own comment.
func ValidateImport(in ImportClub) (ClubInput, error) {
	v := httpx.NewValidator()
	out := validateClub(v, clubRequest{
		Type:              in.Type,
		Label:             in.Label,
		Brand:             in.Brand,
		Model:             in.Model,
		Loft:              in.Loft,
		Shaft:             in.Shaft,
		Flex:              in.Flex,
		Notes:             in.Notes,
		ExpectedCarry:     in.ExpectedCarry,
		AverageDispersion: in.AverageDispersion,
	})
	if err := v.Err(); err != nil {
		return ClubInput{}, err
	}
	return out, nil
}
