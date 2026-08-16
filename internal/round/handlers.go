package round

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/edingc/roundly/internal/auth"
	"github.com/edingc/roundly/internal/httpx"
)

const (
	defaultPageSize = 25
	maxPageSize     = 100
	maxNotesLen     = 2000

	// A hole payload larger than a full round is a client bug. Bounded for the
	// same reason MaxTeesPerCourse is: this is the only place a single request
	// writes an unbounded number of rows.
	maxHolesPerSave = 18
)

// Handler exposes the round endpoints.
type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// Register attaches the round endpoints to an already-authenticated router.
func (h *Handler) Register(r chi.Router) {
	r.Route("/rounds", func(rr chi.Router) {
		rr.Get("/", h.list)
		rr.Post("/", h.start)
		rr.Get("/{roundID}", h.get)
		rr.Put("/{roundID}", h.updateMeta)
		rr.Delete("/{roundID}", h.delete)

		// Holes are written through their own paths so a stale round form
		// cannot change a score, and a score save cannot change the date.
		rr.Put("/{roundID}/holes", h.saveHoles)
		rr.Put("/{roundID}/holes/{holeNumber}", h.saveHole)

		rr.Post("/{roundID}/complete", h.complete)
		rr.Post("/{roundID}/abandon", h.abandon)
		// Abandoning by accident has to be undoable, and a finished round is
		// still editable - this is a personal logbook, not a competition record.
		rr.Post("/{roundID}/reopen", h.reopen)
	})
}

type startRequest struct {
	// ID is optional and client-supplied. See StartInput.
	ID        string  `json:"id"`
	CourseID  string  `json:"course_id"`
	TeeID     string  `json:"tee_id"`
	PlayedOn  string  `json:"played_on"`
	Holes     int     `json:"holes"`
	Nine      string  `json:"nine"`
	EntryMode string  `json:"entry_mode"`
	Notes     *string `json:"notes"`
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	var req startRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	v := httpx.NewValidator()
	v.Required("course_id", req.CourseID)
	v.Required("tee_id", req.TeeID)

	playedOn, ok := ParseDate(strings.TrimSpace(req.PlayedOn))
	if !ok {
		v.Add("played_on", "Enter a date as YYYY-MM-DD.")
	}

	if req.Holes != 9 && req.Holes != 18 {
		v.Add("holes", "Choose either 9 or 18 holes.")
	}
	nine := strings.ToLower(strings.TrimSpace(req.Nine))
	switch {
	case req.Holes == 9 && nine != NineFront && nine != NineBack:
		v.Add("nine", "Choose the front or the back nine.")
	case req.Holes == 18 && nine != "":
		v.Add("nine", "A full round does not have a nine.")
	}

	mode := strings.ToLower(strings.TrimSpace(req.EntryMode))
	if mode != EntryLive && mode != EntryManual {
		v.Add("entry_mode", "Entry mode must be live or manual.")
	}

	// A client-supplied id has to be a UUID this server would itself have
	// minted. Checking the shape keeps anything else out of a primary key.
	roundID := strings.TrimSpace(req.ID)
	if roundID != "" {
		if _, err := uuid.Parse(roundID); err != nil {
			v.Add("id", "That is not a valid round id.")
		}
	}

	if req.Notes != nil {
		v.MaxLen("notes", strings.TrimSpace(*req.Notes), maxNotesLen)
	}

	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	created, err := h.service.Start(ctx, auth.MustUserID(ctx), StartInput{
		ID:        roundID,
		CourseID:  req.CourseID,
		TeeID:     req.TeeID,
		PlayedOn:  playedOn,
		Holes:     req.Holes,
		Nine:      nine,
		EntryMode: mode,
		Notes:     normalizeNotes(req.Notes),
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, created)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := auth.MustUserID(ctx)

	// ?status=in_progress is how the resume picker finds rounds to continue.
	// Several may be open at once, so it returns all of them.
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		if !slices.Contains([]string{StatusInProgress, StatusComplete, StatusAbandoned}, status) {
			httpx.Error(w, r, httpx.BadRequest("Unknown round status."))
			return
		}
		items, err := h.service.ListByStatus(ctx, userID, status)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, Page{Items: items, Total: len(items), Limit: len(items)})
		return
	}

	limit := httpx.QueryInt(r, "limit", defaultPageSize, 1, maxPageSize)
	offset := httpx.QueryInt(r, "offset", 0, 0, 1_000_000)
	page, err := h.service.List(ctx, userID, limit, offset)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, page)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	found, err := h.service.Get(ctx, auth.MustUserID(ctx), chi.URLParam(r, "roundID"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, found)
}

type metaRequest struct {
	PlayedOn string  `json:"played_on"`
	Notes    *string `json:"notes"`
}

func (h *Handler) updateMeta(w http.ResponseWriter, r *http.Request) {
	var req metaRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	v := httpx.NewValidator()
	playedOn, ok := ParseDate(strings.TrimSpace(req.PlayedOn))
	if !ok {
		v.Add("played_on", "Enter a date as YYYY-MM-DD.")
	}
	if req.Notes != nil {
		v.MaxLen("notes", strings.TrimSpace(*req.Notes), maxNotesLen)
	}
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	updated, err := h.service.UpdateMeta(ctx, auth.MustUserID(ctx), chi.URLParam(r, "roundID"), MetaInput{
		PlayedOn: playedOn,
		Notes:    normalizeNotes(req.Notes),
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, updated)
}

// holeRequest is one hole. Absent scoring fields mean absent, not unchanged:
// this arrives by PUT and the payload is the whole hole.
type holeRequest struct {
	HoleNumber      int     `json:"hole_number"`
	Par             *int    `json:"par"`
	Strokes         *int    `json:"strokes"`
	Putts           *int    `json:"putts"`
	TeeClubID       *string `json:"tee_club_id"`
	TeeAccuracy     *string `json:"tee_accuracy"`
	FirstPuttFeet   *int    `json:"first_putt_feet"`
	FairwayBunker   bool    `json:"fairway_bunker"`
	GreensideBunker bool    `json:"greenside_bunker"`
	Penalties       int     `json:"penalties"`
	PenaltyType     *string `json:"penalty_type"`
}

func (h *Handler) saveHole(w http.ResponseWriter, r *http.Request) {
	number, err := strconv.Atoi(chi.URLParam(r, "holeNumber"))
	if err != nil {
		httpx.Error(w, r, httpx.BadRequest("That is not a hole number."))
		return
	}

	var req holeRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	// The path wins over the body, so the two cannot disagree.
	req.HoleNumber = number

	v := httpx.NewValidator()
	in := validateHole(v, "", req)
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	updated, err := h.service.SaveHole(ctx, auth.MustUserID(ctx), chi.URLParam(r, "roundID"), in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, updated)
}

type saveHolesRequest struct {
	Holes []holeRequest `json:"holes"`
}

func (h *Handler) saveHoles(w http.ResponseWriter, r *http.Request) {
	var req saveHolesRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	v := httpx.NewValidator()
	if len(req.Holes) > maxHolesPerSave {
		v.Add("holes", "A round has at most 18 holes.")
	}
	in := make([]HoleInput, 0, len(req.Holes))
	seen := make(map[int]bool, len(req.Holes))
	for i, hole := range req.Holes {
		prefix := "holes." + strconv.Itoa(i)
		if seen[hole.HoleNumber] {
			v.Add(prefix+".hole_number", "Duplicate hole number.")
		}
		seen[hole.HoleNumber] = true
		in = append(in, validateHole(v, prefix, hole))
	}
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	updated, err := h.service.SaveHoles(ctx, auth.MustUserID(ctx), chi.URLParam(r, "roundID"), in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, updated)
}

func (h *Handler) complete(w http.ResponseWriter, r *http.Request) { h.setStatus(w, r, StatusComplete) }
func (h *Handler) abandon(w http.ResponseWriter, r *http.Request)  { h.setStatus(w, r, StatusAbandoned) }
func (h *Handler) reopen(w http.ResponseWriter, r *http.Request)   { h.setStatus(w, r, StatusInProgress) }

func (h *Handler) setStatus(w http.ResponseWriter, r *http.Request, status string) {
	ctx := r.Context()
	updated, err := h.service.SetStatus(ctx, auth.MustUserID(ctx), chi.URLParam(r, "roundID"), status)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, updated)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := h.service.Delete(ctx, auth.MustUserID(ctx), chi.URLParam(r, "roundID")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

// validateHole checks one hole and returns it in service form.
//
// The database carries the same constraints as CHECKs. These exist so a mistake
// comes back as a message against the field rather than as a 500 from a
// constraint violation.
func validateHole(v *httpx.Validator, prefix string, req holeRequest) HoleInput {
	field := func(name string) string {
		if prefix == "" {
			return name
		}
		return prefix + "." + name
	}

	if req.HoleNumber < 1 || req.HoleNumber > 18 {
		v.Add(field("hole_number"), "Hole number must be between 1 and 18.")
	}
	if req.Par != nil {
		v.IntBetween(field("par"), *req.Par, 3, 6)
	}
	if req.Strokes != nil {
		v.IntBetween(field("strokes"), *req.Strokes, 1, 20)
	}
	if req.Putts != nil {
		v.IntBetween(field("putts"), *req.Putts, 0, 10)
	}
	// You cannot putt more times than you hit the ball.
	if req.Strokes != nil && req.Putts != nil && *req.Putts > *req.Strokes {
		v.Add(field("putts"), "There cannot be more putts than strokes.")
	}
	if req.FirstPuttFeet != nil {
		v.IntBetween(field("first_putt_feet"), *req.FirstPuttFeet, 0, 200)
	}
	v.IntBetween(field("penalties"), req.Penalties, 0, 10)

	accuracy := normalizeChoice(req.TeeAccuracy)
	if accuracy != nil && !slices.Contains(Accuracies, *accuracy) {
		v.Add(field("tee_accuracy"), "That is not a recognised tee shot result.")
	}
	penaltyType := normalizeChoice(req.PenaltyType)
	if penaltyType != nil && !slices.Contains(PenaltyTypes, *penaltyType) {
		v.Add(field("penalty_type"), "That is not a recognised penalty.")
	}
	// A reason with no penalty is a contradiction, and the count is what feeds
	// the statistics.
	if penaltyType != nil && req.Penalties == 0 {
		v.Add(field("penalty_type"), "Record at least one penalty stroke to give a reason.")
	}

	return HoleInput{
		HoleNumber:      req.HoleNumber,
		Par:             req.Par,
		Strokes:         req.Strokes,
		Putts:           req.Putts,
		TeeClubID:       normalizeChoice(req.TeeClubID),
		TeeAccuracy:     accuracy,
		FirstPuttFeet:   req.FirstPuttFeet,
		FairwayBunker:   req.FairwayBunker,
		GreensideBunker: req.GreensideBunker,
		Penalties:       req.Penalties,
		PenaltyType:     penaltyType,
	}
}

// normalizeChoice trims an optional string and collapses a blank one to nil, so
// that clearing a picker stores NULL rather than an empty string.
func normalizeChoice(s *string) *string {
	if s == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizeNotes(s *string) *string {
	if s == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
