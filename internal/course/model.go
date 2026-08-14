package course

import "github.com/edingc/roundly/internal/database/sqlc"

// Course is the list representation: no tees or holes, for the index screen.
type Course struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Address   *string `json:"address"`
	Phone     *string `json:"phone"`
	CreatedBy string  `json:"created_by"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
	// CanEdit tells the frontend whether to show edit controls, so it does not
	// have to duplicate the ownership rule.
	CanEdit bool `json:"can_edit"`
	// HoleCount and TeeCount populate the list cards.
	HoleCount int `json:"hole_count"`
	TeeCount  int `json:"tee_count"`
}

// Tee is one set of tee boxes on a course. Course and slope rating are tracked
// separately per gender because that is how they are computed: the same
// physical markers rate differently against the men's and women's scratch and
// bogey golfer models, so a scorecard lists both for one tee. Front9 and Back9
// ratings are separate from the 18-hole rating (not simply half of it) because
// that is how USGA course and slope ratings work, and a 9-hole round posts to
// a handicap using the matching 9-hole rating rather than the 18-hole one.
type Tee struct {
	ID                      string   `json:"id"`
	CourseID                string   `json:"course_id"`
	Name                    string   `json:"name"`
	Color                   string   `json:"color"`
	CourseRatingMen         *float64 `json:"course_rating_men"`
	SlopeRatingMen          *int     `json:"slope_rating_men"`
	CourseRatingWomen       *float64 `json:"course_rating_women"`
	SlopeRatingWomen        *int     `json:"slope_rating_women"`
	Front9CourseRatingMen   *float64 `json:"front9_course_rating_men"`
	Front9SlopeRatingMen    *int     `json:"front9_slope_rating_men"`
	Back9CourseRatingMen    *float64 `json:"back9_course_rating_men"`
	Back9SlopeRatingMen     *int     `json:"back9_slope_rating_men"`
	Front9CourseRatingWomen *float64 `json:"front9_course_rating_women"`
	Front9SlopeRatingWomen  *int     `json:"front9_slope_rating_women"`
	Back9CourseRatingWomen  *float64 `json:"back9_course_rating_women"`
	Back9SlopeRatingWomen   *int     `json:"back9_slope_rating_women"`
	// TotalYardage is derived from the per-hole yardages rather than stored
	// independently, so it cannot drift from the grid.
	TotalYardage int `json:"total_yardage"`
	DisplayOrder int `json:"display_order"`
}

// TeeDetail is the par and yardage for one hole from one tee. This is the row
// that makes a hole playable as a par 3 from one tee and a par 4 from another.
type TeeDetail struct {
	TeeID   string `json:"tee_id"`
	Par     int    `json:"par"`
	Yardage int    `json:"yardage"`
}

// Hole is course-level: the number and stroke index, with per-tee detail attached.
type Hole struct {
	ID            string      `json:"id"`
	CourseID      string      `json:"course_id"`
	HoleNumber    int         `json:"hole_number"`
	HandicapIndex *int        `json:"handicap_index"`
	TeeDetails    []TeeDetail `json:"tee_details"`
}

// CourseDetail is the full course: everything the detail/edit grid needs in one
// request.
type CourseDetail struct {
	Course
	Tees  []Tee  `json:"tees"`
	Holes []Hole `json:"holes"`
}

// Page is the paginated list envelope.
type Page struct {
	Items  []Course `json:"items"`
	Total  int      `json:"total"`
	Limit  int      `json:"limit"`
	Offset int      `json:"offset"`
}

func toCourse(row sqlc.Course, viewerID string) Course {
	return Course{
		ID:        row.ID,
		Name:      row.Name,
		Address:   row.Address,
		Phone:     row.Phone,
		CreatedBy: row.CreatedBy,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
		CanEdit:   row.CreatedBy == viewerID,
	}
}

func toTee(row sqlc.Tee, totalYardage int) Tee {
	return Tee{
		ID:                      row.ID,
		CourseID:                row.CourseID,
		Name:                    row.Name,
		Color:                   row.Color,
		CourseRatingMen:         row.CourseRatingMen,
		SlopeRatingMen:          int64PtrToIntPtr(row.SlopeRatingMen),
		CourseRatingWomen:       row.CourseRatingWomen,
		SlopeRatingWomen:        int64PtrToIntPtr(row.SlopeRatingWomen),
		Front9CourseRatingMen:   row.Front9CourseRatingMen,
		Front9SlopeRatingMen:    int64PtrToIntPtr(row.Front9SlopeRatingMen),
		Back9CourseRatingMen:    row.Back9CourseRatingMen,
		Back9SlopeRatingMen:     int64PtrToIntPtr(row.Back9SlopeRatingMen),
		Front9CourseRatingWomen: row.Front9CourseRatingWomen,
		Front9SlopeRatingWomen:  int64PtrToIntPtr(row.Front9SlopeRatingWomen),
		Back9CourseRatingWomen:  row.Back9CourseRatingWomen,
		Back9SlopeRatingWomen:   int64PtrToIntPtr(row.Back9SlopeRatingWomen),
		TotalYardage:            totalYardage,
		DisplayOrder:            int(row.DisplayOrder),
	}
}

func int64PtrToIntPtr(v *int64) *int {
	if v == nil {
		return nil
	}
	converted := int(*v)
	return &converted
}

func toHole(row sqlc.Hole, details []TeeDetail) Hole {
	hole := Hole{
		ID:         row.ID,
		CourseID:   row.CourseID,
		HoleNumber: int(row.HoleNumber),
		TeeDetails: details,
	}
	if row.HandicapIndex != nil {
		idx := int(*row.HandicapIndex)
		hole.HandicapIndex = &idx
	}
	return hole
}
