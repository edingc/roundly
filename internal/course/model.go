package course

import "github.com/edingc/roundly/internal/database/sqlc"

// Course is the list representation: no tees or holes, for the index screen.
type Course struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Address   *string `json:"address"`
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

// Tee is one set of tee boxes on a course.
type Tee struct {
	ID           string   `json:"id"`
	CourseID     string   `json:"course_id"`
	Name         string   `json:"name"`
	Color        string   `json:"color"`
	CourseRating *float64 `json:"course_rating"`
	SlopeRating  *int     `json:"slope_rating"`
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
		CreatedBy: row.CreatedBy,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
		CanEdit:   row.CreatedBy == viewerID,
	}
}

func toTee(row sqlc.Tee, totalYardage int) Tee {
	tee := Tee{
		ID:           row.ID,
		CourseID:     row.CourseID,
		Name:         row.Name,
		Color:        row.Color,
		CourseRating: row.CourseRating,
		TotalYardage: totalYardage,
		DisplayOrder: int(row.DisplayOrder),
	}
	if row.SlopeRating != nil {
		slope := int(*row.SlopeRating)
		tee.SlopeRating = &slope
	}
	return tee
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
