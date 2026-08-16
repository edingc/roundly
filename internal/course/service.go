package course

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/edingc/roundly/internal/database"
	"github.com/edingc/roundly/internal/database/sqlc"
	"github.com/edingc/roundly/internal/geocode"
	"github.com/edingc/roundly/internal/httpx"
	"github.com/edingc/roundly/internal/id"
	"github.com/edingc/roundly/internal/timex"
)

// DefaultHoleCount is how many holes a new course gets unless told otherwise.
const DefaultHoleCount = 18

// MaxHoleNumber matches the CHECK constraint on holes.hole_number.
const MaxHoleNumber = 18

// MaxTeesPerCourse bounds how many tee sets one course may carry.
//
// Holes are self-limiting — the hole number is checked against 1..18 and
// duplicates are rejected — but tees had no cap at all, so a single crafted
// payload could ask for as many rows as fit inside the 1 MiB body limit. That
// is the only unbounded write in the app, and it is inconsistent with the
// explicit caps the account import already applies to courses and clubs.
//
// Twenty-four is well past generous: a course with separate men's, women's,
// senior, and junior markers plus a handful of combo tees is nowhere near it.
const MaxTeesPerCourse = 24

// Service implements the course directory use cases.
//
// Courses are shared reference data, and nobody owns one. Any signed-in user can
// read any course and correct any course: a golf course's yardages are objective
// facts about the world, not one player's property, and tying edit rights to
// whoever typed them in first left wrong numbers uncorrectable by everyone else.
//
// uploaded_by records who entered a course and grants nothing. Removing a course
// is the one destructive act, so it is not a user action at all — see the
// removal-request flow and RequireAdmin.
type Service struct {
	db *database.DB
	// geocoder fills latitude and longitude from the address. Nil on an
	// instance that has not configured one, which is the default: this is the
	// only outbound call the course directory makes, and an operator should opt
	// into it rather than discover it in their egress logs.
	geocoder geocode.Geocoder
}

func NewService(db *database.DB, geocoder geocode.Geocoder) *Service {
	return &Service{db: db, geocoder: geocoder}
}

// resolveCoordinates returns the coordinates to store for a course.
//
// A point that was supplied wins untouched: this fills gaps, it never corrects
// anybody. Clearing both fields and saving is therefore also how you ask for a
// course to be re-placed after its address changes.
//
// Every failure - no geocoder, too little address to place, a lookup error, a
// place OSM has never heard of - returns no coordinates and no error. None of
// them is a reason to refuse to save a golf course.
func (s *Service) resolveCoordinates(ctx context.Context, loc Location, lat, lng *float64) (*float64, *float64) {
	if lat != nil || lng != nil {
		return lat, lng
	}
	if s.geocoder == nil {
		return nil, nil
	}
	address := loc.postalLine()
	if address == "" {
		return nil, nil
	}

	result, err := s.geocoder.Lookup(ctx, address)
	if err != nil {
		slog.Warn("geocode failed", "address", address, "error", err)
		return nil, nil
	}
	if result == nil {
		return nil, nil
	}
	return &result.Latitude, &result.Longitude
}

// List returns a page of courses, optionally filtered by a search term matched
// against the name and every part of the address.
//
// viewerID is whose home course sorts to the top. It orders the page rather
// than filtering it: the directory is shared, and everybody still sees every
// course.
func (s *Service) List(ctx context.Context, viewerID, search string, limit, offset int) (*Page, error) {
	search = strings.TrimSpace(search)

	var (
		rows  []sqlc.Course
		total int64
		err   error
	)

	if search == "" {
		rows, err = s.db.Queries.ListCourses(ctx, sqlc.ListCoursesParams{
			UserID: viewerID,
			Limit:  int64(limit),
			Offset: int64(offset),
		})
		if err != nil {
			return nil, httpx.Internal(fmt.Errorf("list courses: %w", err))
		}
		total, err = s.db.Queries.CountCourses(ctx)
		if err != nil {
			return nil, httpx.Internal(fmt.Errorf("count courses: %w", err))
		}
	} else {
		// The query lowercases the columns, so the term must be lowered to match.
		term := strings.ToLower(search)
		rows, err = s.db.Queries.SearchCourses(ctx, sqlc.SearchCoursesParams{
			UserID: viewerID,
			Query:  term,
			Limit:  int64(limit),
			Offset: int64(offset),
		})
		if err != nil {
			return nil, httpx.Internal(fmt.Errorf("search courses: %w", err))
		}
		total, err = s.db.Queries.CountSearchCourses(ctx, term)
		if err != nil {
			return nil, httpx.Internal(fmt.Errorf("count search courses: %w", err))
		}
	}

	items := make([]Course, 0, len(rows))
	for _, row := range rows {
		item := toCourse(row)

		// The list cards show how complete each course is. These are small
		// per-row reads; if the directory grows large enough for this to matter,
		// they become a single aggregate query.
		tees, err := s.db.Queries.ListTeesByCourse(ctx, row.ID)
		if err != nil {
			return nil, httpx.Internal(fmt.Errorf("list tees for course %s: %w", row.ID, err))
		}
		holes, err := s.db.Queries.ListHolesByCourse(ctx, row.ID)
		if err != nil {
			return nil, httpx.Internal(fmt.Errorf("list holes for course %s: %w", row.ID, err))
		}
		item.TeeCount = len(tees)
		item.HoleCount = len(holes)

		items = append(items, item)
	}

	return &Page{Items: items, Total: int(total), Limit: limit, Offset: offset}, nil
}

// Get returns a course with its tees, holes, and per-tee hole details.
func (s *Service) Get(ctx context.Context, courseID string) (*CourseDetail, error) {
	row, err := s.loadCourse(ctx, courseID)
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, row)
}

func (s *Service) detail(ctx context.Context, row sqlc.Course) (*CourseDetail, error) {
	teeRows, err := s.db.Queries.ListTeesByCourse(ctx, row.ID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list tees: %w", err))
	}
	holeRows, err := s.db.Queries.ListHolesByCourse(ctx, row.ID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list holes: %w", err))
	}
	detailRows, err := s.db.Queries.ListHoleTeeDetailsByCourse(ctx, row.ID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list hole tee details: %w", err))
	}

	// Group details by hole, and total yardage by tee, in one pass.
	byHole := make(map[string][]TeeDetail, len(holeRows))
	yardageByTee := make(map[string]int, len(teeRows))
	for _, d := range detailRows {
		byHole[d.HoleID] = append(byHole[d.HoleID], TeeDetail{
			TeeID:   d.TeeID,
			Par:     int(d.Par),
			Yardage: int(d.Yardage),
		})
		yardageByTee[d.TeeID] += int(d.Yardage)
	}

	tees := make([]Tee, 0, len(teeRows))
	for _, t := range teeRows {
		tees = append(tees, toTee(t, yardageByTee[t.ID]))
	}

	holes := make([]Hole, 0, len(holeRows))
	for _, h := range holeRows {
		details := byHole[h.ID]
		if details == nil {
			details = []TeeDetail{}
		}
		holes = append(holes, toHole(h, details))
	}

	course := toCourse(row)
	course.HoleCount = len(holes)
	course.TeeCount = len(tees)

	return &CourseDetail{
		Course: course,
		Tees:   tees,
		Holes:  holes,
	}, nil
}

// CreateCourseInput is the payload for creating a course.
type CreateCourseInput struct {
	Name string
	Location
	Phone        *string
	Website      *string
	Notes        *string
	FacilityType *string
	Latitude     *float64
	Longitude    *float64
	Pinned       bool
	// HoleCount pre-creates holes 1..HoleCount so the par/yardage grid is
	// immediately usable. Zero means DefaultHoleCount.
	HoleCount int
	Tees      []TeeInput
}

// TeeInput is a tee in a create or update request.
type TeeInput struct {
	Name                    string
	Color                   string
	CourseRatingMen         *float64
	SlopeRatingMen          *int
	CourseRatingWomen       *float64
	SlopeRatingWomen        *int
	Front9CourseRatingMen   *float64
	Front9SlopeRatingMen    *int
	Back9CourseRatingMen    *float64
	Back9SlopeRatingMen     *int
	Front9CourseRatingWomen *float64
	Front9SlopeRatingWomen  *int
	Back9CourseRatingWomen  *float64
	Back9SlopeRatingWomen   *int
	DisplayOrder            *int
}

// Create makes a course, its holes, and any tees supplied up front, in one
// transaction so a partially built course is never visible.
func (s *Service) Create(ctx context.Context, creatorID string, in CreateCourseInput) (*CourseDetail, error) {
	holeCount := in.HoleCount
	if holeCount == 0 {
		holeCount = DefaultHoleCount
	}

	courseID := id.New()
	now := timex.Now()
	loc := in.normalized()
	latitude, longitude := s.resolveCoordinates(ctx, loc, in.Latitude, in.Longitude)

	err := s.db.InTx(func(q *sqlc.Queries) error {
		var pinned int64
		if in.Pinned {
			pinned = 1
		}
		if err := q.CreateCourse(ctx, sqlc.CreateCourseParams{
			ID:           courseID,
			Name:         strings.TrimSpace(in.Name),
			Street:       loc.Street,
			City:         loc.City,
			Region:       loc.Region,
			PostalCode:   loc.PostalCode,
			Country:      loc.Country,
			Phone:        normalizeOptional(in.Phone),
			Website:      normalizeOptional(in.Website),
			Notes:        normalizeOptional(in.Notes),
			FacilityType: normalizeOptional(in.FacilityType),
			Latitude:     latitude,
			Longitude:    longitude,
			Pinned:       pinned,
			UploadedBy:   &creatorID,
			CreatedAt:    now,
			UpdatedAt:    now,
		}); err != nil {
			return fmt.Errorf("insert course: %w", err)
		}

		for n := 1; n <= holeCount; n++ {
			if err := q.CreateHole(ctx, sqlc.CreateHoleParams{
				ID:            id.New(),
				CourseID:      courseID,
				HoleNumber:    int64(n),
				HandicapIndex: nil,
			}); err != nil {
				return fmt.Errorf("insert hole %d: %w", n, err)
			}
		}

		for i, tee := range in.Tees {
			order := i
			if tee.DisplayOrder != nil {
				order = *tee.DisplayOrder
			}
			if err := q.CreateTee(ctx, teeParams(id.New(), courseID, tee, order)); err != nil {
				return fmt.Errorf("insert tee %q: %w", tee.Name, err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("create course: %w", err))
	}

	row, err := s.loadCourse(ctx, courseID)
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, row)
}

// ImportTeeDetailInput is one hole's par and yardage for one named tee, from
// an imported course file.
type ImportTeeDetailInput struct {
	TeeName string
	Par     int
	Yardage int
}

// ImportHoleInput is one hole, with its per-tee par and yardage, from an
// imported course file. Tees are referenced by name rather than ID, since IDs
// are not stable across app instances.
type ImportHoleInput struct {
	HoleNumber    int
	HandicapIndex *int
	TeeDetails    []ImportTeeDetailInput
}

// ImportCourseInput is the payload for recreating a course, its tees, and its
// holes from data produced by Export. If ID is set and the importer owns a
// course with that ID, the existing course is updated in place.
type ImportCourseInput struct {
	ID   string
	Name string
	Location
	Phone        *string
	Website      *string
	FacilityType *string
	Latitude     *float64
	Longitude    *float64
	Tees         []TeeInput
	Holes        []ImportHoleInput
}

// Import recreates or updates a course, its tees, and its holes (with par
// and yardage) from data produced by Export, in one transaction so a partial
// import is never visible. If the input contains a course ID that the
// importer owns, the existing course is updated; otherwise a new course is
// created. Hole tee details reference tees by name rather than ID, since IDs
// are not stable across app instances.
func (s *Service) Import(ctx context.Context, creatorID string, in ImportCourseInput) (*CourseDetail, error) {
	// Decide whether to update an existing course or create a new one.
	//
	// This used to require that the importer had uploaded the course. Nobody
	// owns a course any more, and anyone may edit one, so an import that names
	// an existing course updates it — which is exactly what editing it by hand
	// would do. A course the file does not name still gets a fresh ID.
	courseID := id.New()
	update := false
	if in.ID != "" {
		if existing, err := s.db.Queries.GetCourse(ctx, in.ID); err == nil {
			courseID = existing.ID
			update = true
		}
	}

	now := timex.Now()
	loc := in.normalized()

	// Import deliberately does not geocode. A restore can carry sixty courses,
	// and sixty lookups at one a second is both a minute of held-open request
	// and precisely the bulk geocoding the OSM usage policy forbids. An export
	// file carries its own coordinates; one that does not can be re-placed a
	// course at a time by saving it.

	err := s.db.InTx(func(q *sqlc.Queries) error {
		if update {
			// Import preserves the existing notes and pinned values.
			existing, _ := q.GetCourse(ctx, courseID)
			if err := q.UpdateCourse(ctx, sqlc.UpdateCourseParams{
				Name:         strings.TrimSpace(in.Name),
				Street:       loc.Street,
				City:         loc.City,
				Region:       loc.Region,
				PostalCode:   loc.PostalCode,
				Country:      loc.Country,
				Phone:        normalizeOptional(in.Phone),
				Website:      normalizeOptional(in.Website),
				Notes:        existing.Notes,
				FacilityType: normalizeOptional(in.FacilityType),
				Latitude:     in.Latitude,
				Longitude:    in.Longitude,
				Pinned:       existing.Pinned,
				UpdatedAt:    now,
				ID:           courseID,
			}); err != nil {
				return fmt.Errorf("update course: %w", err)
			}
		} else {
			if err := q.CreateCourse(ctx, sqlc.CreateCourseParams{
				ID:           courseID,
				Name:         strings.TrimSpace(in.Name),
				Street:       loc.Street,
				City:         loc.City,
				Region:       loc.Region,
				PostalCode:   loc.PostalCode,
				Country:      loc.Country,
				Phone:        normalizeOptional(in.Phone),
				Website:      normalizeOptional(in.Website),
				Notes:        nil,
				FacilityType: normalizeOptional(in.FacilityType),
				Latitude:     in.Latitude,
				Longitude:    in.Longitude,
				Pinned:       0,
				UploadedBy:   &creatorID,
				CreatedAt:    now,
				UpdatedAt:    now,
			}); err != nil {
				return fmt.Errorf("insert course: %w", err)
			}
		}

		// Upsert tees: match by name on update, create fresh on insert.
		teeIDByName := make(map[string]string, len(in.Tees))
		inputTeeNames := make(map[string]bool, len(in.Tees))
		for i, tee := range in.Tees {
			name := strings.TrimSpace(tee.Name)
			inputTeeNames[name] = true
			order := i
			if tee.DisplayOrder != nil {
				order = *tee.DisplayOrder
			}

			if update {
				existing, err := q.GetTeeByName(ctx, sqlc.GetTeeByNameParams{
					CourseID: courseID,
					Name:     name,
				})
				if err == nil {
					// Update existing tee.
					if err := q.UpdateTee(ctx, sqlc.UpdateTeeParams{
						Name:                    name,
						Color:                   tee.Color,
						CourseRatingMen:         tee.CourseRatingMen,
						SlopeRatingMen:          intToInt64Ptr(tee.SlopeRatingMen),
						CourseRatingWomen:       tee.CourseRatingWomen,
						SlopeRatingWomen:        intToInt64Ptr(tee.SlopeRatingWomen),
						Front9CourseRatingMen:   tee.Front9CourseRatingMen,
						Front9SlopeRatingMen:    intToInt64Ptr(tee.Front9SlopeRatingMen),
						Back9CourseRatingMen:    tee.Back9CourseRatingMen,
						Back9SlopeRatingMen:     intToInt64Ptr(tee.Back9SlopeRatingMen),
						Front9CourseRatingWomen: tee.Front9CourseRatingWomen,
						Front9SlopeRatingWomen:  intToInt64Ptr(tee.Front9SlopeRatingWomen),
						Back9CourseRatingWomen:  tee.Back9CourseRatingWomen,
						Back9SlopeRatingWomen:   intToInt64Ptr(tee.Back9SlopeRatingWomen),
						DisplayOrder:            int64(order),
						ID:                      existing.ID,
					}); err != nil {
						return fmt.Errorf("update tee %q: %w", name, err)
					}
					teeIDByName[name] = existing.ID
					continue
				}
			}

			// Create new tee.
			teeID := id.New()
			if err := q.CreateTee(ctx, teeParams(teeID, courseID, tee, order)); err != nil {
				return fmt.Errorf("insert tee %q: %w", tee.Name, err)
			}
			teeIDByName[name] = teeID
		}

		// Delete tees not in the import (update path only).
		if update {
			existingTees, err := q.ListTeesByCourse(ctx, courseID)
			if err != nil {
				return fmt.Errorf("list tees for cleanup: %w", err)
			}
			for _, t := range existingTees {
				if !inputTeeNames[t.Name] {
					if err := q.DeleteTee(ctx, t.ID); err != nil {
						return fmt.Errorf("delete extra tee %q: %w", t.Name, err)
					}
				}
			}
		}

		// Upsert holes: match by hole_number on update, create fresh on insert.
		holeIDByNumber := make(map[int]string, len(in.Holes))
		inputHoleNumbers := make(map[int]bool, len(in.Holes))
		for _, hole := range in.Holes {
			inputHoleNumbers[hole.HoleNumber] = true

			if update {
				existing, err := q.GetHoleByNumber(ctx, sqlc.GetHoleByNumberParams{
					CourseID:   courseID,
					HoleNumber: int64(hole.HoleNumber),
				})
				if err == nil {
					// Update existing hole.
					if err := q.UpdateHole(ctx, sqlc.UpdateHoleParams{
						HandicapIndex: intToInt64Ptr(hole.HandicapIndex),
						ID:            existing.ID,
					}); err != nil {
						return fmt.Errorf("update hole %d: %w", hole.HoleNumber, err)
					}
					holeIDByNumber[hole.HoleNumber] = existing.ID
					continue
				}
			}

			// Create new hole.
			holeID := id.New()
			if err := q.CreateHole(ctx, sqlc.CreateHoleParams{
				ID:            holeID,
				CourseID:      courseID,
				HoleNumber:    int64(hole.HoleNumber),
				HandicapIndex: intToInt64Ptr(hole.HandicapIndex),
			}); err != nil {
				return fmt.Errorf("insert hole %d: %w", hole.HoleNumber, err)
			}
			holeIDByNumber[hole.HoleNumber] = holeID
		}

		// Delete holes not in the import (update path only).
		if update {
			existingHoles, err := q.ListHolesByCourse(ctx, courseID)
			if err != nil {
				return fmt.Errorf("list holes for cleanup: %w", err)
			}
			for _, h := range existingHoles {
				if !inputHoleNumbers[int(h.HoleNumber)] {
					if err := q.DeleteHole(ctx, h.ID); err != nil {
						return fmt.Errorf("delete extra hole %d: %w", h.HoleNumber, err)
					}
				}
			}
		}

		// Upsert hole tee details.
		for _, hole := range in.Holes {
			holeID := holeIDByNumber[hole.HoleNumber]
			for _, d := range hole.TeeDetails {
				teeID, ok := teeIDByName[d.TeeName]
				if !ok {
					continue
				}
				if err := q.UpsertHoleTeeDetail(ctx, sqlc.UpsertHoleTeeDetailParams{
					ID:      id.New(),
					HoleID:  holeID,
					TeeID:   teeID,
					Par:     int64(d.Par),
					Yardage: int64(d.Yardage),
				}); err != nil {
					return fmt.Errorf("insert hole %d tee detail: %w", hole.HoleNumber, err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("import course: %w", err))
	}

	row, err := s.loadCourse(ctx, courseID)
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, row)
}

// UpdateCourseInput is the payload for updating course-level fields.
type UpdateCourseInput struct {
	Name string
	Location
	Phone        *string
	Website      *string
	Notes        *string
	FacilityType *string
	Latitude     *float64
	Longitude    *float64
	Pinned       bool
}

func (s *Service) Update(ctx context.Context, editorID, courseID string, in UpdateCourseInput) (*CourseDetail, error) {
	row, err := s.loadCourse(ctx, courseID)
	if err != nil {
		return nil, err
	}

	var pinned int64
	if in.Pinned {
		pinned = 1
	}
	loc := in.normalized()
	latitude, longitude := s.resolveCoordinates(ctx, loc, in.Latitude, in.Longitude)
	if err := s.db.Queries.UpdateCourse(ctx, sqlc.UpdateCourseParams{
		Name:         strings.TrimSpace(in.Name),
		Street:       loc.Street,
		City:         loc.City,
		Region:       loc.Region,
		PostalCode:   loc.PostalCode,
		Country:      loc.Country,
		Phone:        normalizeOptional(in.Phone),
		Website:      normalizeOptional(in.Website),
		Notes:        normalizeOptional(in.Notes),
		FacilityType: normalizeOptional(in.FacilityType),
		Latitude:     latitude,
		Longitude:    longitude,
		Pinned:       pinned,
		UpdatedAt:    timex.Now(),
		ID:           row.ID,
	}); err != nil {
		return nil, httpx.Internal(fmt.Errorf("update course: %w", err))
	}

	updated, err := s.loadCourse(ctx, courseID)
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, updated)
}

// Delete removes a course; tees, holes, and hole details cascade.
func (s *Service) Delete(ctx context.Context, editorID, courseID string) error {
	row, err := s.loadCourse(ctx, courseID)
	if err != nil {
		return err
	}
	if err := s.db.Queries.DeleteCourse(ctx, row.ID); err != nil {
		return httpx.Internal(fmt.Errorf("delete course: %w", err))
	}
	return nil
}

// AddTee appends a tee to a course.
func (s *Service) AddTee(ctx context.Context, editorID, courseID string, in TeeInput) (*Tee, error) {
	if _, err := s.loadCourse(ctx, courseID); err != nil {
		return nil, err
	}

	order := 0
	if in.DisplayOrder != nil {
		order = *in.DisplayOrder
	} else {
		maxOrder, err := s.db.Queries.MaxTeeDisplayOrder(ctx, courseID)
		if err != nil {
			return nil, httpx.Internal(fmt.Errorf("max tee display order: %w", err))
		}
		order = int(maxOrder) + 1
	}

	teeID := id.New()
	if err := s.db.Queries.CreateTee(ctx, teeParams(teeID, courseID, in, order)); err != nil {
		return nil, httpx.Internal(fmt.Errorf("create tee: %w", err))
	}
	s.touch(ctx, courseID)

	row, err := s.db.Queries.GetTee(ctx, teeID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("reload tee: %w", err))
	}
	tee := toTee(row, 0)
	return &tee, nil
}

// UpdateTee edits a tee's name, color, rating, or ordering.
func (s *Service) UpdateTee(ctx context.Context, editorID, teeID string, in TeeInput) (*Tee, error) {
	row, err := s.db.Queries.GetTee(ctx, teeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, httpx.NotFound("That tee does not exist.")
		}
		return nil, httpx.Internal(fmt.Errorf("load tee: %w", err))
	}
	if _, err := s.loadCourse(ctx, row.CourseID); err != nil {
		return nil, err
	}

	order := int(row.DisplayOrder)
	if in.DisplayOrder != nil {
		order = *in.DisplayOrder
	}

	if err := s.db.Queries.UpdateTee(ctx, sqlc.UpdateTeeParams{
		Name:                    strings.TrimSpace(in.Name),
		Color:                   in.Color,
		CourseRatingMen:         in.CourseRatingMen,
		SlopeRatingMen:          intToInt64Ptr(in.SlopeRatingMen),
		CourseRatingWomen:       in.CourseRatingWomen,
		SlopeRatingWomen:        intToInt64Ptr(in.SlopeRatingWomen),
		Front9CourseRatingMen:   in.Front9CourseRatingMen,
		Front9SlopeRatingMen:    intToInt64Ptr(in.Front9SlopeRatingMen),
		Back9CourseRatingMen:    in.Back9CourseRatingMen,
		Back9SlopeRatingMen:     intToInt64Ptr(in.Back9SlopeRatingMen),
		Front9CourseRatingWomen: in.Front9CourseRatingWomen,
		Front9SlopeRatingWomen:  intToInt64Ptr(in.Front9SlopeRatingWomen),
		Back9CourseRatingWomen:  in.Back9CourseRatingWomen,
		Back9SlopeRatingWomen:   intToInt64Ptr(in.Back9SlopeRatingWomen),
		DisplayOrder:            int64(order),
		ID:                      teeID,
	}); err != nil {
		return nil, httpx.Internal(fmt.Errorf("update tee: %w", err))
	}
	s.touch(ctx, row.CourseID)

	updated, err := s.db.Queries.GetTee(ctx, teeID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("reload tee: %w", err))
	}
	yardage, err := s.db.Queries.SumTeeYardage(ctx, teeID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("sum tee yardage: %w", err))
	}
	tee := toTee(updated, int(yardage))
	return &tee, nil
}

// DeleteTee removes a tee and, by cascade, its per-hole par/yardage rows.
func (s *Service) DeleteTee(ctx context.Context, editorID, teeID string) error {
	row, err := s.db.Queries.GetTee(ctx, teeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return httpx.NotFound("That tee does not exist.")
		}
		return httpx.Internal(fmt.Errorf("load tee: %w", err))
	}
	if _, err := s.loadCourse(ctx, row.CourseID); err != nil {
		return err
	}
	if err := s.db.Queries.DeleteTee(ctx, teeID); err != nil {
		return httpx.Internal(fmt.Errorf("delete tee: %w", err))
	}
	s.touch(ctx, row.CourseID)
	return nil
}

// HoleInput is the payload for adding or updating a hole.
type HoleInput struct {
	HoleNumber    int
	HandicapIndex *int
}

// AddHole creates a single hole on a course.
func (s *Service) AddHole(ctx context.Context, editorID, courseID string, in HoleInput) (*Hole, error) {
	if _, err := s.loadCourse(ctx, courseID); err != nil {
		return nil, err
	}

	if _, err := s.db.Queries.GetHoleByNumber(ctx, sqlc.GetHoleByNumberParams{
		CourseID:   courseID,
		HoleNumber: int64(in.HoleNumber),
	}); err == nil {
		return nil, httpx.Conflict(fmt.Sprintf("Hole %d already exists on this course.", in.HoleNumber))
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.Internal(fmt.Errorf("lookup hole by number: %w", err))
	}

	if in.HandicapIndex != nil {
		if err := s.checkHandicapIndexUnique(ctx, courseID, "", *in.HandicapIndex); err != nil {
			return nil, err
		}
	}

	holeID := id.New()
	if err := s.db.Queries.CreateHole(ctx, sqlc.CreateHoleParams{
		ID:            holeID,
		CourseID:      courseID,
		HoleNumber:    int64(in.HoleNumber),
		HandicapIndex: intToInt64Ptr(in.HandicapIndex),
	}); err != nil {
		return nil, httpx.Internal(fmt.Errorf("create hole: %w", err))
	}
	s.touch(ctx, courseID)

	row, err := s.db.Queries.GetHole(ctx, holeID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("reload hole: %w", err))
	}
	hole := toHole(row, []TeeDetail{})
	return &hole, nil
}

// UpdateHole edits a hole's stroke index.
func (s *Service) UpdateHole(ctx context.Context, editorID, holeID string, in HoleInput) (*Hole, error) {
	row, err := s.db.Queries.GetHole(ctx, holeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, httpx.NotFound("That hole does not exist.")
		}
		return nil, httpx.Internal(fmt.Errorf("load hole: %w", err))
	}
	if _, err := s.loadCourse(ctx, row.CourseID); err != nil {
		return nil, err
	}

	if in.HandicapIndex != nil {
		if err := s.checkHandicapIndexUnique(ctx, row.CourseID, holeID, *in.HandicapIndex); err != nil {
			return nil, err
		}
	}

	if err := s.db.Queries.UpdateHole(ctx, sqlc.UpdateHoleParams{
		HandicapIndex: intToInt64Ptr(in.HandicapIndex),
		ID:            holeID,
	}); err != nil {
		return nil, httpx.Internal(fmt.Errorf("update hole: %w", err))
	}
	s.touch(ctx, row.CourseID)

	updated, err := s.db.Queries.GetHole(ctx, holeID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("reload hole: %w", err))
	}
	details, err := s.teeDetailsForHole(ctx, holeID)
	if err != nil {
		return nil, err
	}
	hole := toHole(updated, details)
	return &hole, nil
}

// DeleteHole removes a hole and its per-tee detail rows.
func (s *Service) DeleteHole(ctx context.Context, editorID, holeID string) error {
	row, err := s.db.Queries.GetHole(ctx, holeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return httpx.NotFound("That hole does not exist.")
		}
		return httpx.Internal(fmt.Errorf("load hole: %w", err))
	}
	if _, err := s.loadCourse(ctx, row.CourseID); err != nil {
		return err
	}
	if err := s.db.Queries.DeleteHole(ctx, holeID); err != nil {
		return httpx.Internal(fmt.Errorf("delete hole: %w", err))
	}
	s.touch(ctx, row.CourseID)
	return nil
}

// SetTeeDetail upserts the par and yardage for one hole from one tee. This is
// the endpoint the editable grid calls per cell.
func (s *Service) SetTeeDetail(ctx context.Context, editorID, holeID, teeID string, par, yardage int) (*Hole, error) {
	hole, err := s.db.Queries.GetHole(ctx, holeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, httpx.NotFound("That hole does not exist.")
		}
		return nil, httpx.Internal(fmt.Errorf("load hole: %w", err))
	}
	if _, err := s.loadCourse(ctx, hole.CourseID); err != nil {
		return nil, err
	}

	tee, err := s.db.Queries.GetTee(ctx, teeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, httpx.NotFound("That tee does not exist.")
		}
		return nil, httpx.Internal(fmt.Errorf("load tee: %w", err))
	}
	// Without this check a caller could attach a hole from one course to a tee
	// from another, since the join table only references the two row IDs.
	if tee.CourseID != hole.CourseID {
		return nil, httpx.BadRequest("That tee belongs to a different course than that hole.")
	}

	if err := s.db.Queries.UpsertHoleTeeDetail(ctx, sqlc.UpsertHoleTeeDetailParams{
		ID:      id.New(),
		HoleID:  holeID,
		TeeID:   teeID,
		Par:     int64(par),
		Yardage: int64(yardage),
	}); err != nil {
		return nil, httpx.Internal(fmt.Errorf("upsert hole tee detail: %w", err))
	}
	s.touch(ctx, hole.CourseID)

	details, err := s.teeDetailsForHole(ctx, holeID)
	if err != nil {
		return nil, err
	}
	result := toHole(hole, details)
	return &result, nil
}

// ClearTeeDetail removes the par/yardage for one hole+tee pair.
func (s *Service) ClearTeeDetail(ctx context.Context, editorID, holeID, teeID string) error {
	hole, err := s.db.Queries.GetHole(ctx, holeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return httpx.NotFound("That hole does not exist.")
		}
		return httpx.Internal(fmt.Errorf("load hole: %w", err))
	}
	if _, err := s.loadCourse(ctx, hole.CourseID); err != nil {
		return err
	}
	if err := s.db.Queries.DeleteHoleTeeDetail(ctx, sqlc.DeleteHoleTeeDetailParams{
		HoleID: holeID,
		TeeID:  teeID,
	}); err != nil {
		return httpx.Internal(fmt.Errorf("delete hole tee detail: %w", err))
	}
	s.touch(ctx, hole.CourseID)
	return nil
}

// checkHandicapIndexUnique reports a conflict if another hole on the course
// already uses index. excludeHoleID is skipped, so updating a hole to the
// stroke index it already has does not trip over itself.
func (s *Service) checkHandicapIndexUnique(ctx context.Context, courseID, excludeHoleID string, index int) error {
	holes, err := s.db.Queries.ListHolesByCourse(ctx, courseID)
	if err != nil {
		return httpx.Internal(fmt.Errorf("list holes for course %s: %w", courseID, err))
	}
	for _, h := range holes {
		if h.ID == excludeHoleID || h.HandicapIndex == nil || int(*h.HandicapIndex) != index {
			continue
		}
		return httpx.Conflict(fmt.Sprintf("Stroke index %d is already used by hole %d.", index, h.HoleNumber))
	}
	return nil
}

func (s *Service) teeDetailsForHole(ctx context.Context, holeID string) ([]TeeDetail, error) {
	rows, err := s.db.Queries.ListHoleTeeDetailsByHole(ctx, holeID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list hole tee details: %w", err))
	}
	details := make([]TeeDetail, 0, len(rows))
	for _, r := range rows {
		details = append(details, TeeDetail{TeeID: r.TeeID, Par: int(r.Par), Yardage: int(r.Yardage)})
	}
	return details, nil
}

func (s *Service) loadCourse(ctx context.Context, courseID string) (sqlc.Course, error) {
	row, err := s.db.Queries.GetCourse(ctx, courseID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqlc.Course{}, httpx.NotFound("That course does not exist.")
		}
		return sqlc.Course{}, httpx.Internal(fmt.Errorf("load course: %w", err))
	}
	return row, nil
}

// touch bumps a course's updated_at after a change to one of its children.
//
// A failure is logged rather than surfaced: the user's edit did succeed, and a
// stale timestamp is not worth failing the request over. It is logged rather
// than dropped because the only ways this fails are a vanished course or a
// database that has stopped accepting writes, and both are worth knowing.
func (s *Service) touch(ctx context.Context, courseID string) {
	if err := s.db.Queries.TouchCourse(ctx, sqlc.TouchCourseParams{
		UpdatedAt: timex.Now(),
		ID:        courseID,
	}); err != nil {
		slog.Warn("touch course", "course_id", courseID, "error", err)
	}
}

func teeParams(teeID, courseID string, in TeeInput, order int) sqlc.CreateTeeParams {
	return sqlc.CreateTeeParams{
		ID:                      teeID,
		CourseID:                courseID,
		Name:                    strings.TrimSpace(in.Name),
		Color:                   in.Color,
		CourseRatingMen:         in.CourseRatingMen,
		SlopeRatingMen:          intToInt64Ptr(in.SlopeRatingMen),
		CourseRatingWomen:       in.CourseRatingWomen,
		SlopeRatingWomen:        intToInt64Ptr(in.SlopeRatingWomen),
		Front9CourseRatingMen:   in.Front9CourseRatingMen,
		Front9SlopeRatingMen:    intToInt64Ptr(in.Front9SlopeRatingMen),
		Back9CourseRatingMen:    in.Back9CourseRatingMen,
		Back9SlopeRatingMen:     intToInt64Ptr(in.Back9SlopeRatingMen),
		Front9CourseRatingWomen: in.Front9CourseRatingWomen,
		Front9SlopeRatingWomen:  intToInt64Ptr(in.Front9SlopeRatingWomen),
		Back9CourseRatingWomen:  in.Back9CourseRatingWomen,
		Back9SlopeRatingWomen:   intToInt64Ptr(in.Back9SlopeRatingWomen),
		DisplayOrder:            int64(order),
	}
}

func normalizeOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func int64Ptr(v int64) *int64 { return &v }

func intToInt64Ptr(v *int) *int64 {
	if v == nil {
		return nil
	}
	converted := int64(*v)
	return &converted
}
