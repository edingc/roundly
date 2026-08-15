package course

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/edingc/roundly/internal/auth"
	"github.com/edingc/roundly/internal/httpx"
)

// courseExportFormatVersion is bumped whenever the export shape changes in a
// way that would break importing an older file.
const courseExportFormatVersion = 3

// CourseExport is everything needed to recreate a course elsewhere: tees and
// holes are matched by name and number instead of ID, since IDs are not
// stable across app instances. Export and import share this shape, so a file
// downloaded from one instance can be uploaded to another (or the same one)
// unmodified.
type CourseExport struct {
	FormatVersion int          `json:"format_version"`
	ID            string       `json:"id,omitempty"`
	Name          string       `json:"name"`
	Address       *string      `json:"address"`
	Phone         *string      `json:"phone"`
	Website       *string      `json:"website"`
	FacilityType  *string      `json:"facility_type"`
	Latitude      *float64     `json:"latitude"`
	Longitude     *float64     `json:"longitude"`
	Tees          []teeRequest `json:"tees"`
	Holes         []holeExport `json:"holes"`
}

type holeExport struct {
	HoleNumber    int               `json:"hole_number"`
	HandicapIndex *int              `json:"handicap_index"`
	TeeDetails    []teeDetailExport `json:"tee_details"`
}

type teeDetailExport struct {
	TeeName string `json:"tee_name"`
	Par     int    `json:"par"`
	Yardage int    `json:"yardage"`
}

// export returns a course as a downloadable, human-readable JSON file that
// can be imported back into this or another instance.
func (h *Handler) export(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	detail, err := h.service.Get(ctx, auth.MustUserID(ctx), chi.URLParam(r, "courseID"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	teeNames := make(map[string]string, len(detail.Tees))
	tees := make([]teeRequest, 0, len(detail.Tees))
	for _, t := range detail.Tees {
		teeNames[t.ID] = t.Name
		order := t.DisplayOrder
		tees = append(tees, teeRequest{
			Name:                    t.Name,
			Color:                   t.Color,
			CourseRatingMen:         t.CourseRatingMen,
			SlopeRatingMen:          t.SlopeRatingMen,
			CourseRatingWomen:       t.CourseRatingWomen,
			SlopeRatingWomen:        t.SlopeRatingWomen,
			Front9CourseRatingMen:   t.Front9CourseRatingMen,
			Front9SlopeRatingMen:    t.Front9SlopeRatingMen,
			Back9CourseRatingMen:    t.Back9CourseRatingMen,
			Back9SlopeRatingMen:     t.Back9SlopeRatingMen,
			Front9CourseRatingWomen: t.Front9CourseRatingWomen,
			Front9SlopeRatingWomen:  t.Front9SlopeRatingWomen,
			Back9CourseRatingWomen:  t.Back9CourseRatingWomen,
			Back9SlopeRatingWomen:   t.Back9SlopeRatingWomen,
			DisplayOrder:            &order,
		})
	}

	holes := make([]holeExport, 0, len(detail.Holes))
	for _, hole := range detail.Holes {
		details := make([]teeDetailExport, 0, len(hole.TeeDetails))
		for _, d := range hole.TeeDetails {
			details = append(details, teeDetailExport{
				TeeName: teeNames[d.TeeID],
				Par:     d.Par,
				Yardage: d.Yardage,
			})
		}
		holes = append(holes, holeExport{
			HoleNumber:    hole.HoleNumber,
			HandicapIndex: hole.HandicapIndex,
			TeeDetails:    details,
		})
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.json"`, exportSlug(detail.Name)))
	httpx.JSON(w, http.StatusOK, CourseExport{
		FormatVersion: courseExportFormatVersion,
		ID:            detail.ID,
		Name:          detail.Name,
		Address:       detail.Address,
		Phone:         detail.Phone,
		Website:       detail.Website,
		FacilityType:  detail.FacilityType,
		Latitude:      detail.Latitude,
		Longitude:     detail.Longitude,
		Tees:          tees,
		Holes:         holes,
	})
}

// importCourse recreates or updates a course, its tees, and its holes (with
// par and yardage) from a file produced by export. If the file contains a
// course ID that the importer owns, the existing course is updated in place;
// otherwise a new course is created.
func (h *Handler) importCourse(w http.ResponseWriter, r *http.Request) {
	var req CourseExport
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	v := httpx.NewValidator()
	validateCourseFields(v, req.Name, req.Address, req.Phone, req.Website, nil, req.FacilityType, req.Latitude, req.Longitude)

	tees := make([]TeeInput, 0, len(req.Tees))
	teeNameSeen := make(map[string]bool, len(req.Tees))
	for i, tee := range req.Tees {
		prefix := fieldPath("tees", i)
		tees = append(tees, validateTee(v, prefix, tee))
		name := strings.TrimSpace(tee.Name)
		if name != "" {
			if teeNameSeen[name] {
				v.Add(joinField(prefix, "name"), "Tee names must be unique within a course.")
			}
			teeNameSeen[name] = true
		}
	}

	holes := make([]ImportHoleInput, 0, len(req.Holes))
	holeNumberSeen := make(map[int]bool, len(req.Holes))
	handicapIndexSeen := make(map[int]bool, len(req.Holes))
	for i, hole := range req.Holes {
		prefix := fieldPath("holes", i)
		v.IntBetween(joinField(prefix, "hole_number"), hole.HoleNumber, 1, MaxHoleNumber)
		if holeNumberSeen[hole.HoleNumber] {
			v.Add(joinField(prefix, "hole_number"), "Duplicate hole number.")
		}
		holeNumberSeen[hole.HoleNumber] = true
		if hole.HandicapIndex != nil {
			v.IntBetween(joinField(prefix, "handicap_index"), *hole.HandicapIndex, 1, MaxHoleNumber)
			if handicapIndexSeen[*hole.HandicapIndex] {
				v.Add(joinField(prefix, "handicap_index"), "Duplicate stroke index.")
			}
			handicapIndexSeen[*hole.HandicapIndex] = true
		}

		details := make([]ImportTeeDetailInput, 0, len(hole.TeeDetails))
		for j, d := range hole.TeeDetails {
			dprefix := joinField(prefix, fmt.Sprintf("tee_details.%d", j))
			v.IntBetween(joinField(dprefix, "par"), d.Par, minPar, maxPar)
			v.IntBetween(joinField(dprefix, "yardage"), d.Yardage, minYardage, maxYardage)
			teeName := strings.TrimSpace(d.TeeName)
			if !teeNameSeen[teeName] {
				v.Add(joinField(dprefix, "tee_name"), "This tee name does not match any tee in the file.")
			}
			details = append(details, ImportTeeDetailInput{TeeName: teeName, Par: d.Par, Yardage: d.Yardage})
		}
		holes = append(holes, ImportHoleInput{
			HoleNumber:    hole.HoleNumber,
			HandicapIndex: hole.HandicapIndex,
			TeeDetails:    details,
		})
	}

	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	detail, err := h.service.Import(ctx, auth.MustUserID(ctx), ImportCourseInput{
		ID:           req.ID,
		Name:         req.Name,
		Address:      req.Address,
		Phone:        req.Phone,
		Website:      req.Website,
		FacilityType: req.FacilityType,
		Latitude:     req.Latitude,
		Longitude:    req.Longitude,
		Tees:         tees,
		Holes:        holes,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, detail)
}

// exportSlug turns a course name into a safe filename stem.
func exportSlug(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "course"
	}
	return slug
}
