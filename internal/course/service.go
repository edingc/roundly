package course

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/edingc/roundly/internal/database"
	"github.com/edingc/roundly/internal/database/sqlc"
	"github.com/edingc/roundly/internal/httpx"
	"github.com/edingc/roundly/internal/id"
	"github.com/edingc/roundly/internal/timex"
)

// DefaultHoleCount is how many holes a new course gets unless told otherwise.
const DefaultHoleCount = 18

// MaxHoleNumber matches the CHECK constraint on holes.hole_number.
const MaxHoleNumber = 18

// Service implements the course directory use cases.
//
// Courses are a shared directory: any signed-in user can read any course, but
// only the user who created it can modify it. Phase 8 (multi-tenant) is where
// this becomes a real permission model.
type Service struct {
	db *database.DB
}

func NewService(db *database.DB) *Service {
	return &Service{db: db}
}

// List returns a page of courses, optionally filtered by a search term matched
// against name and address.
func (s *Service) List(ctx context.Context, viewerID, search string, limit, offset int) (*Page, error) {
	search = strings.TrimSpace(search)

	var (
		rows  []sqlc.Course
		total int64
		err   error
	)

	if search == "" {
		rows, err = s.db.Queries.ListCourses(ctx, sqlc.ListCoursesParams{
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
		item := toCourse(row, viewerID)

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
func (s *Service) Get(ctx context.Context, viewerID, courseID string) (*CourseDetail, error) {
	row, err := s.loadCourse(ctx, courseID)
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, row, viewerID)
}

func (s *Service) detail(ctx context.Context, row sqlc.Course, viewerID string) (*CourseDetail, error) {
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

	return &CourseDetail{
		Course: toCourse(row, viewerID),
		Tees:   tees,
		Holes:  holes,
	}, nil
}

// CreateCourseInput is the payload for creating a course.
type CreateCourseInput struct {
	Name    string
	Address *string
	// HoleCount pre-creates holes 1..HoleCount so the par/yardage grid is
	// immediately usable. Zero means DefaultHoleCount.
	HoleCount int
	Tees      []TeeInput
}

// TeeInput is a tee in a create or update request.
type TeeInput struct {
	Name         string
	Color        string
	CourseRating *float64
	SlopeRating  *int
	DisplayOrder *int
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

	err := s.db.InTx(func(q *sqlc.Queries) error {
		if err := q.CreateCourse(ctx, sqlc.CreateCourseParams{
			ID:        courseID,
			Name:      strings.TrimSpace(in.Name),
			Address:   normalizeOptional(in.Address),
			CreatedBy: creatorID,
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			return fmt.Errorf("insert course: %w", err)
		}

		for n := 1; n <= holeCount; n++ {
			if err := q.CreateHole(ctx, sqlc.CreateHoleParams{
				ID:         id.New(),
				CourseID:   courseID,
				HoleNumber: int64(n),
				// Stroke index defaults to the hole number, which is a sensible
				// placeholder the user can correct on the detail screen.
				HandicapIndex: int64Ptr(int64(n)),
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
	return s.detail(ctx, row, creatorID)
}

// UpdateCourseInput is the payload for updating course-level fields.
type UpdateCourseInput struct {
	Name    string
	Address *string
}

func (s *Service) Update(ctx context.Context, editorID, courseID string, in UpdateCourseInput) (*CourseDetail, error) {
	row, err := s.loadEditableCourse(ctx, editorID, courseID)
	if err != nil {
		return nil, err
	}

	if err := s.db.Queries.UpdateCourse(ctx, sqlc.UpdateCourseParams{
		Name:      strings.TrimSpace(in.Name),
		Address:   normalizeOptional(in.Address),
		UpdatedAt: timex.Now(),
		ID:        row.ID,
	}); err != nil {
		return nil, httpx.Internal(fmt.Errorf("update course: %w", err))
	}

	updated, err := s.loadCourse(ctx, courseID)
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, updated, editorID)
}

// Delete removes a course; tees, holes, and hole details cascade.
func (s *Service) Delete(ctx context.Context, editorID, courseID string) error {
	row, err := s.loadEditableCourse(ctx, editorID, courseID)
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
	if _, err := s.loadEditableCourse(ctx, editorID, courseID); err != nil {
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
	if _, err := s.loadEditableCourse(ctx, editorID, row.CourseID); err != nil {
		return nil, err
	}

	order := int(row.DisplayOrder)
	if in.DisplayOrder != nil {
		order = *in.DisplayOrder
	}

	if err := s.db.Queries.UpdateTee(ctx, sqlc.UpdateTeeParams{
		Name:         strings.TrimSpace(in.Name),
		Color:        in.Color,
		CourseRating: in.CourseRating,
		SlopeRating:  intToInt64Ptr(in.SlopeRating),
		TotalYardage: row.TotalYardage,
		DisplayOrder: int64(order),
		ID:           teeID,
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
	if _, err := s.loadEditableCourse(ctx, editorID, row.CourseID); err != nil {
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
	if _, err := s.loadEditableCourse(ctx, editorID, courseID); err != nil {
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
	if _, err := s.loadEditableCourse(ctx, editorID, row.CourseID); err != nil {
		return nil, err
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
	if _, err := s.loadEditableCourse(ctx, editorID, row.CourseID); err != nil {
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
	if _, err := s.loadEditableCourse(ctx, editorID, hole.CourseID); err != nil {
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
	if _, err := s.loadEditableCourse(ctx, editorID, hole.CourseID); err != nil {
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

// loadEditableCourse loads a course and enforces that the caller created it.
func (s *Service) loadEditableCourse(ctx context.Context, editorID, courseID string) (sqlc.Course, error) {
	row, err := s.loadCourse(ctx, courseID)
	if err != nil {
		return sqlc.Course{}, err
	}
	if row.CreatedBy != editorID {
		return sqlc.Course{}, httpx.Forbidden("Only the person who added this course can change it.")
	}
	return row, nil
}

// touch bumps a course's updated_at after a change to one of its children.
// A failure here is logged rather than surfaced: the user's edit did succeed,
// and a stale timestamp is not worth failing the request over.
func (s *Service) touch(ctx context.Context, courseID string) {
	_ = s.db.Queries.TouchCourse(ctx, sqlc.TouchCourseParams{
		UpdatedAt: timex.Now(),
		ID:        courseID,
	})
}

func teeParams(teeID, courseID string, in TeeInput, order int) sqlc.CreateTeeParams {
	return sqlc.CreateTeeParams{
		ID:           teeID,
		CourseID:     courseID,
		Name:         strings.TrimSpace(in.Name),
		Color:        in.Color,
		CourseRating: in.CourseRating,
		SlopeRating:  intToInt64Ptr(in.SlopeRating),
		// total_yardage is derived from hole_tee_details on read, so the stored
		// column stays null rather than holding a value that could go stale.
		TotalYardage: nil,
		DisplayOrder: int64(order),
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
