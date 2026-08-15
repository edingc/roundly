package course

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/edingc/roundly/internal/auth"
	"github.com/edingc/roundly/internal/database/sqlc"
	"github.com/edingc/roundly/internal/httpx"
)

// courseExportFormatVersion is bumped whenever the export shape changes in a
// way that would break importing an older file.
const courseExportFormatVersion = 3

// minCourseExportVersion is the oldest file this server still reads.
//
// Versions 1 through 3 differ only by fields that were added as optional, so
// every one of them still imports correctly. This matters because the version
// was written but never checked until now: files stamped 1 and 2 are in users'
// hands already, and retroactively demanding 3 would break exports this app
// itself produced.
const minCourseExportVersion = 1

// CheckFormatVersion rejects a file this server cannot read.
//
// A version that is present but too new gets a message saying so, rather than
// a confusing validation failure on whichever field changed shape.
func CheckFormatVersion(got, min, max int, what string) error {
	switch {
	case got == 0:
		return httpx.BadRequest(fmt.Sprintf("This file has no format_version, so it is not a Roundly %s file.", what))
	case got > max:
		return httpx.BadRequest(fmt.Sprintf(
			"This %s file was written by a newer version of Roundly (format %d; this server reads up to %d). Update the server and try again.",
			what, got, max))
	case got < min:
		return httpx.BadRequest(fmt.Sprintf(
			"This %s file uses format %d, which this server can no longer read.", what, got))
	}
	return nil
}

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

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.json"`, exportSlug(detail.Name)))
	httpx.JSON(w, http.StatusOK, buildExport(detail))
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

	if err := CheckFormatVersion(req.FormatVersion, minCourseExportVersion, courseExportFormatVersion, "course"); err != nil {
		httpx.Error(w, r, err)
		return
	}

	in, err := ValidateImport(req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	detail, err := h.service.Import(ctx, auth.MustUserID(ctx), in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, detail)
}

// ValidateImport checks a course file and converts it into service input.
//
// Lifted out of the handler so the account-wide restore validates its embedded
// courses through exactly the same rules, rather than growing a second, subtly
// different copy of them.
func ValidateImport(req CourseExport) (ImportCourseInput, error) {
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
		return ImportCourseInput{}, err
	}

	return ImportCourseInput{
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
	}, nil
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

// buildExport reshapes a loaded course into its portable form. Children are
// referenced by tee name and hole number rather than by ID, because IDs are not
// stable across instances.
func buildExport(detail *CourseDetail) CourseExport {
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

	return CourseExport{
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
	}
}

// ExportAll returns every course the given user created, in portable form.
//
// It reads all four tables in one creator-scoped query each and groups them in
// memory, rather than calling Get per course. That matters: Get costs three
// queries a course, and the pool holds a single connection, so a sixty-course
// account export would otherwise be a hundred and eighty sequential round trips
// inside the server's thirty-second request timeout.
func (s *Service) ExportAll(ctx context.Context, creatorID string) ([]CourseExport, error) {
	courseRows, err := s.db.Queries.ListCoursesByCreator(ctx, creatorID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list courses by creator: %w", err))
	}
	if len(courseRows) == 0 {
		return []CourseExport{}, nil
	}

	teeRows, err := s.db.Queries.ListTeesByCreator(ctx, creatorID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list tees by creator: %w", err))
	}
	holeRows, err := s.db.Queries.ListHolesByCreator(ctx, creatorID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list holes by creator: %w", err))
	}
	detailRows, err := s.db.Queries.ListHoleTeeDetailsByCreator(ctx, creatorID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list hole tee details by creator: %w", err))
	}

	teesByCourse := make(map[string][]sqlc.Tee, len(courseRows))
	for _, t := range teeRows {
		teesByCourse[t.CourseID] = append(teesByCourse[t.CourseID], t)
	}
	holesByCourse := make(map[string][]sqlc.Hole, len(courseRows))
	courseIDByHole := make(map[string]string, len(holeRows))
	for _, h := range holeRows {
		holesByCourse[h.CourseID] = append(holesByCourse[h.CourseID], h)
		courseIDByHole[h.ID] = h.CourseID
	}
	detailsByHole := make(map[string][]TeeDetail, len(holeRows))
	yardageByTee := make(map[string]int, len(teeRows))
	for _, d := range detailRows {
		detailsByHole[d.HoleID] = append(detailsByHole[d.HoleID], TeeDetail{
			TeeID:   d.TeeID,
			Par:     int(d.Par),
			Yardage: int(d.Yardage),
		})
		yardageByTee[d.TeeID] += int(d.Yardage)
	}

	out := make([]CourseExport, 0, len(courseRows))
	for _, row := range courseRows {
		tees := make([]Tee, 0, len(teesByCourse[row.ID]))
		for _, t := range teesByCourse[row.ID] {
			tees = append(tees, toTee(t, yardageByTee[t.ID]))
		}

		holes := make([]Hole, 0, len(holesByCourse[row.ID]))
		for _, h := range holesByCourse[row.ID] {
			details := detailsByHole[h.ID]
			if details == nil {
				details = []TeeDetail{}
			}
			holes = append(holes, toHole(h, details))
		}

		course := toCourse(row, creatorID)
		course.HoleCount = len(holes)
		course.TeeCount = len(tees)
		out = append(out, buildExport(&CourseDetail{Course: course, Tees: tees, Holes: holes}))
	}
	return out, nil
}
