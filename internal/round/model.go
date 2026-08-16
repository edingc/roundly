// Package round records what happened on a golf course.
//
// The shape of everything here follows from one decision: a round snapshots the
// course rather than pointing at it. Courses are shared reference data that
// anyone may correct and an administrator may remove, so a round that only held
// a foreign key would silently change what you shot against whenever somebody
// fixed a par. Par, yardage, and stroke index are copied onto each hole when the
// round starts; the course name, tee, rating, and slope onto the round.
//
// Rounds are solo. There is no notion of a playing partner and no plan for one.
package round

import (
	"github.com/edingc/roundly/internal/auth"
	"github.com/edingc/roundly/internal/database/sqlc"
)

// Statuses a round can be in.
const (
	StatusInProgress = "in_progress"
	// StatusComplete means the player finished and said so. It is not inferred
	// from every hole having a score: a round can be complete with holes left
	// blank, and a round with all eighteen filled in is not finished until the
	// player says it is.
	StatusComplete = "complete"
	// StatusAbandoned is rain, injury, or nine-and-out. Kept rather than
	// deleted, because an abandoned round still holds real holes, and dropped
	// from the averages that would otherwise be dragged down by it.
	StatusAbandoned = "abandoned"
)

// How a round was entered.
const (
	EntryLive   = "live"
	EntryManual = "manual"
)

// Which set of published course ratings applies to a player. Defined in
// internal/auth, where the column lives; aliased here because this is the
// package that reads it.
const (
	GenderMen   = auth.GenderMen
	GenderWomen = auth.GenderWomen
)

// Which nine, when nine holes of an eighteen-hole course were played.
const (
	NineFront = "front"
	NineBack  = "back"
)

// Where a tee shot finished, relative to where it was aimed.
//
// AccuracyHit is named for the outcome rather than the place because a par 3
// has no fairway: it means the intended target was found, which is the fairway
// on a par 4 or 5 and the green on a par 3.
const (
	AccuracyHit      = "hit"
	AccuracyLeft     = "left"
	AccuracyFarLeft  = "far_left"
	AccuracyRight    = "right"
	AccuracyFarRight = "far_right"
	AccuracyLong     = "long"
	AccuracyShort    = "short"
	AccuracyMishit   = "mishit"
)

// Accuracies is the accepted set, for validation and for building the picker.
var Accuracies = []string{
	AccuracyHit, AccuracyLeft, AccuracyFarLeft, AccuracyRight,
	AccuracyFarRight, AccuracyLong, AccuracyShort, AccuracyMishit,
}

// Why a stroke was added.
const (
	PenaltyOBLost      = "ob_lost"
	PenaltyPenaltyArea = "penalty_area"
	PenaltyUnplayable  = "unplayable"
	PenaltyOther       = "other"
)

// PenaltyTypes is the accepted set.
var PenaltyTypes = []string{
	PenaltyOBLost, PenaltyPenaltyArea, PenaltyUnplayable, PenaltyOther,
}

// Hole is one hole of one round: what the course said, and what happened.
type Hole struct {
	HoleNumber int `json:"hole_number"`

	// The snapshot. Par is nullable because a course may have an incomplete
	// scorecard; a hole with no par simply drops out of the statistics that
	// need one instead of blocking the round.
	Par         *int `json:"par"`
	Yardage     *int `json:"yardage"`
	StrokeIndex *int `json:"stroke_index"`

	// Strokes is nil for a hole that was not completed - picked up, conceded,
	// ran out of light. Distinct from zero, which nobody shoots.
	Strokes *int `json:"strokes"`
	Putts   *int `json:"putts"`

	TeeClubID *string `json:"tee_club_id"`
	// TeeClubLabel is resolved for display so the client need not hold the whole
	// bag to render a scorecard, and so a retired club still reads as itself.
	TeeClubLabel *string `json:"tee_club_label"`

	TeeAccuracy     *string `json:"tee_accuracy"`
	FirstPuttFeet   *int    `json:"first_putt_feet"`
	FairwayBunker   bool    `json:"fairway_bunker"`
	GreensideBunker bool    `json:"greenside_bunker"`
	Penalties       int     `json:"penalties"`
	PenaltyType     *string `json:"penalty_type"`
}

// Completed reports whether this hole has a score.
func (h Hole) Completed() bool { return h.Strokes != nil }

// Round is a round of golf as the API returns it.
type Round struct {
	ID string `json:"id"`

	CourseID   *string `json:"course_id"`
	CourseName string  `json:"course_name"`
	TeeID      *string `json:"tee_id"`
	TeeName    string  `json:"tee_name"`
	TeeColor   *string `json:"tee_color"`
	// Rating and slope as they stood on the day. Unused for now; they are the
	// inputs a handicap differential will need, and cannot be recovered later.
	CourseRating *float64 `json:"course_rating"`
	SlopeRating  *int     `json:"slope_rating"`

	// PlayedOn is a local calendar date, not a timestamp.
	PlayedOn    string  `json:"played_on"`
	StartedAt   *string `json:"started_at"`
	CompletedAt *string `json:"completed_at"`

	Status        string  `json:"status"`
	EntryMode     string  `json:"entry_mode"`
	HolesIntended int     `json:"holes_intended"`
	Nine          *string `json:"nine"`
	Notes         *string `json:"notes"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`

	Holes   []Hole  `json:"holes"`
	Summary Summary `json:"summary"`
	// Differential is what this round contributes to a handicap. Nil until every
	// intended hole has a score, and nil on a course with no published rating.
	// Unofficial for the reason DifferentialFor explains.
	Differential *float64 `json:"differential"`
}

// Page is the paginated list envelope, matching the course directory's.
type Page struct {
	Items  []Round `json:"items"`
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
}

func toRound(row sqlc.Round) Round {
	return Round{
		ID:            row.ID,
		CourseID:      row.CourseID,
		CourseName:    row.CourseName,
		TeeID:         row.TeeID,
		TeeName:       row.TeeName,
		TeeColor:      row.TeeColor,
		CourseRating:  row.CourseRating,
		SlopeRating:   int64PtrToIntPtr(row.SlopeRating),
		PlayedOn:      row.PlayedOn,
		StartedAt:     row.StartedAt,
		CompletedAt:   row.CompletedAt,
		Status:        row.Status,
		EntryMode:     row.EntryMode,
		HolesIntended: int(row.HolesIntended),
		Nine:          row.Nine,
		Notes:         row.Notes,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}

// HoleFromRow converts a stored hole into the domain shape.
//
// Exported for internal/stats, which reads holes in bulk across every round and
// would otherwise need its own copy of this mapping - and a second copy is how
// two packages end up disagreeing about what a null score means.
func HoleFromRow(row sqlc.RoundHole) Hole { return toHole(row) }

func toHole(row sqlc.RoundHole) Hole {
	return Hole{
		HoleNumber:      int(row.HoleNumber),
		Par:             int64PtrToIntPtr(row.Par),
		Yardage:         int64PtrToIntPtr(row.Yardage),
		StrokeIndex:     int64PtrToIntPtr(row.StrokeIndex),
		Strokes:         int64PtrToIntPtr(row.Strokes),
		Putts:           int64PtrToIntPtr(row.Putts),
		TeeClubID:       row.TeeClubID,
		TeeAccuracy:     row.TeeAccuracy,
		FirstPuttFeet:   int64PtrToIntPtr(row.FirstPuttFeet),
		FairwayBunker:   row.FairwayBunker != 0,
		GreensideBunker: row.GreensideBunker != 0,
		Penalties:       int(row.Penalties),
		PenaltyType:     row.PenaltyType,
	}
}

func int64PtrToIntPtr(v *int64) *int {
	if v == nil {
		return nil
	}
	converted := int(*v)
	return &converted
}

func intPtrToInt64Ptr(v *int) *int64 {
	if v == nil {
		return nil
	}
	converted := int64(*v)
	return &converted
}

func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
