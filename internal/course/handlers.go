package course

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/edingc/roundly/internal/auth"
	"github.com/edingc/roundly/internal/httpx"
)

// Validation bounds. Par and yardage ranges are wide enough for real courses
// (including par 6s and long par 5s) while still catching typos.
const (
	minPar     = 3
	maxPar     = 6
	minYardage = 1
	maxYardage = 1000

	minCourseRating = 50.0
	maxCourseRating = 90.0
	minSlopeRating  = 55
	maxSlopeRating  = 155
)

// Handler exposes the course directory endpoints.
type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// Register attaches the course, tee, and hole endpoints to r, which the caller
// is expected to have wrapped in auth middleware.
//
// These are registered onto the caller's router rather than mounted as a
// subrouter at "/": a mount at the root installs a catch-all, which would
// swallow every unmatched /api path and answer it with this group's 401 instead
// of the API's own 404 and 405 handlers.
func (h *Handler) Register(r chi.Router) {
	r.Route("/courses", func(cr chi.Router) {
		cr.Get("/", h.list)
		cr.Post("/", h.create)

		cr.Route("/{courseID}", func(dr chi.Router) {
			dr.Get("/", h.get)
			dr.Put("/", h.update)
			dr.Delete("/", h.delete)
			dr.Post("/tees", h.addTee)
			dr.Post("/holes", h.addHole)
		})
	})

	r.Route("/tees/{teeID}", func(tr chi.Router) {
		tr.Put("/", h.updateTee)
		tr.Delete("/", h.deleteTee)
	})

	r.Route("/holes/{holeID}", func(hr chi.Router) {
		hr.Put("/", h.updateHole)
		hr.Delete("/", h.deleteHole)
		hr.Put("/tee-details/{teeID}", h.setTeeDetail)
		hr.Delete("/tee-details/{teeID}", h.clearTeeDetail)
	})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	limit := httpx.QueryInt(r, "limit", 25, 1, 100)
	offset := httpx.QueryInt(r, "offset", 0, 0, 1_000_000)
	search := r.URL.Query().Get("q")

	page, err := h.service.List(ctx, auth.MustUserID(ctx), search, limit, offset)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, page)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	detail, err := h.service.Get(ctx, auth.MustUserID(ctx), chi.URLParam(r, "courseID"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, detail)
}

type teeRequest struct {
	Name         string   `json:"name"`
	Color        string   `json:"color"`
	CourseRating *float64 `json:"course_rating"`
	SlopeRating  *int     `json:"slope_rating"`
	DisplayOrder *int     `json:"display_order"`
}

type createCourseRequest struct {
	Name      string       `json:"name"`
	Address   *string      `json:"address"`
	HoleCount int          `json:"hole_count"`
	Tees      []teeRequest `json:"tees"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createCourseRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	v := httpx.NewValidator()
	validateCourseFields(v, req.Name, req.Address)

	// Nine- and eighteen-hole courses are the realistic cases, and the holes
	// table CHECK constraint caps hole numbers at 18 either way.
	if req.HoleCount != 0 && req.HoleCount != 9 && req.HoleCount != 18 {
		v.Add("hole_count", "Choose either 9 or 18 holes.")
	}

	tees := make([]TeeInput, 0, len(req.Tees))
	for i, tee := range req.Tees {
		tees = append(tees, validateTee(v, fieldPath("tees", i), tee))
	}
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	detail, err := h.service.Create(ctx, auth.MustUserID(ctx), CreateCourseInput{
		Name:      req.Name,
		Address:   req.Address,
		HoleCount: req.HoleCount,
		Tees:      tees,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, detail)
}

type updateCourseRequest struct {
	Name    string  `json:"name"`
	Address *string `json:"address"`
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req updateCourseRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	v := httpx.NewValidator()
	validateCourseFields(v, req.Name, req.Address)
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	detail, err := h.service.Update(ctx, auth.MustUserID(ctx), chi.URLParam(r, "courseID"), UpdateCourseInput{
		Name:    req.Name,
		Address: req.Address,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, detail)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := h.service.Delete(ctx, auth.MustUserID(ctx), chi.URLParam(r, "courseID")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) addTee(w http.ResponseWriter, r *http.Request) {
	var req teeRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	v := httpx.NewValidator()
	input := validateTee(v, "", req)
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	tee, err := h.service.AddTee(ctx, auth.MustUserID(ctx), chi.URLParam(r, "courseID"), input)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, tee)
}

func (h *Handler) updateTee(w http.ResponseWriter, r *http.Request) {
	var req teeRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	v := httpx.NewValidator()
	input := validateTee(v, "", req)
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	tee, err := h.service.UpdateTee(ctx, auth.MustUserID(ctx), chi.URLParam(r, "teeID"), input)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, tee)
}

func (h *Handler) deleteTee(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := h.service.DeleteTee(ctx, auth.MustUserID(ctx), chi.URLParam(r, "teeID")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

type holeRequest struct {
	HoleNumber    int  `json:"hole_number"`
	HandicapIndex *int `json:"handicap_index"`
}

func (h *Handler) addHole(w http.ResponseWriter, r *http.Request) {
	var req holeRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	v := httpx.NewValidator()
	v.IntBetween("hole_number", req.HoleNumber, 1, MaxHoleNumber)
	validateHandicapIndex(v, req.HandicapIndex)
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	hole, err := h.service.AddHole(ctx, auth.MustUserID(ctx), chi.URLParam(r, "courseID"), HoleInput{
		HoleNumber:    req.HoleNumber,
		HandicapIndex: req.HandicapIndex,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, hole)
}

type updateHoleRequest struct {
	HandicapIndex *int `json:"handicap_index"`
}

func (h *Handler) updateHole(w http.ResponseWriter, r *http.Request) {
	var req updateHoleRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	v := httpx.NewValidator()
	validateHandicapIndex(v, req.HandicapIndex)
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	hole, err := h.service.UpdateHole(ctx, auth.MustUserID(ctx), chi.URLParam(r, "holeID"), HoleInput{
		HandicapIndex: req.HandicapIndex,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, hole)
}

func (h *Handler) deleteHole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := h.service.DeleteHole(ctx, auth.MustUserID(ctx), chi.URLParam(r, "holeID")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

type teeDetailRequest struct {
	Par     int `json:"par"`
	Yardage int `json:"yardage"`
}

// setTeeDetail is the per-cell upsert the editable grid calls.
func (h *Handler) setTeeDetail(w http.ResponseWriter, r *http.Request) {
	var req teeDetailRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	v := httpx.NewValidator()
	v.IntBetween("par", req.Par, minPar, maxPar)
	v.IntBetween("yardage", req.Yardage, minYardage, maxYardage)
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	hole, err := h.service.SetTeeDetail(
		ctx,
		auth.MustUserID(ctx),
		chi.URLParam(r, "holeID"),
		chi.URLParam(r, "teeID"),
		req.Par,
		req.Yardage,
	)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, hole)
}

func (h *Handler) clearTeeDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	err := h.service.ClearTeeDetail(ctx, auth.MustUserID(ctx), chi.URLParam(r, "holeID"), chi.URLParam(r, "teeID"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func validateCourseFields(v *httpx.Validator, name string, address *string) {
	v.Required("name", name)
	v.MaxLen("name", strings.TrimSpace(name), 120)
	if address != nil {
		v.MaxLen("address", strings.TrimSpace(*address), 240)
	}
}

func validateTee(v *httpx.Validator, prefix string, req teeRequest) TeeInput {
	nameField := joinField(prefix, "name")
	colorField := joinField(prefix, "color")

	v.Required(nameField, req.Name)
	v.MaxLen(nameField, strings.TrimSpace(req.Name), 60)
	color := v.HexColor(colorField, req.Color)

	if req.CourseRating != nil {
		rating := *req.CourseRating
		if rating < minCourseRating || rating > maxCourseRating {
			v.Add(joinField(prefix, "course_rating"), "Course rating is usually between 50 and 90.")
		}
	}
	if req.SlopeRating != nil {
		v.IntBetween(joinField(prefix, "slope_rating"), *req.SlopeRating, minSlopeRating, maxSlopeRating)
	}

	return TeeInput{
		Name:         req.Name,
		Color:        color,
		CourseRating: req.CourseRating,
		SlopeRating:  req.SlopeRating,
		DisplayOrder: req.DisplayOrder,
	}
}

func validateHandicapIndex(v *httpx.Validator, index *int) {
	if index != nil {
		v.IntBetween("handicap_index", *index, 1, MaxHoleNumber)
	}
}

// fieldPath names an element of a request array so validation errors point at
// the right row, e.g. "tees.0".
func fieldPath(base string, index int) string {
	return base + "." + strconv.Itoa(index)
}

func joinField(prefix, field string) string {
	if prefix == "" {
		return field
	}
	return prefix + "." + field
}
