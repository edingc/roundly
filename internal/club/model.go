// Package club implements the golf bag: the clubs a player owns, which of them
// are currently in the bag, and which have been retired.
//
// A bag is personal. Unlike the course directory, which is shared and readable
// by any signed-in user, every read and write here is scoped to the owner.
package club

import "github.com/edingc/roundly/internal/database/sqlc"

// Status is a club's place in the bag. It is derived from the active flag and
// retired_at rather than stored separately, so the two can never disagree.
type Status string

const (
	// StatusActive is in the bag right now.
	StatusActive Status = "active"
	// StatusBenched is owned but out of the bag, and can be swapped back in.
	StatusBenched Status = "benched"
	// StatusRetired is sold, replaced, or broken. The row survives so that
	// historical rounds and shots still resolve to the club that hit them.
	StatusRetired Status = "retired"
)

// ClubLimit is the number of clubs the Rules of Golf allow in a bag during a
// round. It is reported to the client rather than enforced: a player editing a
// bag passes through invalid states, and practice bags are legitimately larger.
const ClubLimit = 14

// Types are the club categories, in the order a bag is normally laid out.
// Exposed through the API so the frontend does not keep its own copy that can
// drift from what the server will accept.
var Types = []string{"driver", "wood", "hybrid", "iron", "wedge", "putter"}

// Flexes are the recognized shaft flex values, soft to stiff.
var Flexes = []string{"ladies", "senior", "regular", "stiff", "x-stiff"}

// Club is one club a player owns.
type Club struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`
	// Type is one of Types.
	Type  string  `json:"club_type"`
	Label string  `json:"label"`
	Brand *string `json:"brand"`
	Model *string `json:"model"`
	// Loft is in degrees, and fractional because wedges are sold in half
	// degrees.
	Loft   *float64 `json:"loft"`
	Shaft  *string  `json:"shaft"`
	Flex   *string  `json:"flex"`
	Notes  *string  `json:"notes"`
	Status Status   `json:"status"`
	// RetiredAt is set only for retired clubs.
	RetiredAt    *string `json:"retired_at"`
	DisplayOrder int     `json:"display_order"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

// Bag is the whole equipment screen in one response: the three groups plus the
// club-count rule, so the client does not reimplement either the grouping or
// the 14-club check.
type Bag struct {
	Active  []Club `json:"active"`
	Benched []Club `json:"benched"`
	Retired []Club `json:"retired"`
	// ActiveCount is len(Active), sent explicitly because it is what the
	// counter in the UI displays.
	ActiveCount int `json:"active_count"`
	ClubLimit   int `json:"club_limit"`
	// OverLimit is a warning, not an error: the server accepts the state, and
	// the client is expected to say so rather than block the edit.
	OverLimit bool `json:"over_limit"`
}

// Options tells the client which club types and flexes the server accepts.
type Options struct {
	Types  []string `json:"club_types"`
	Flexes []string `json:"flexes"`
	Limit  int      `json:"club_limit"`
}

func statusOf(row sqlc.Club) Status {
	switch {
	case row.RetiredAt != nil:
		return StatusRetired
	case row.Active != 0:
		return StatusActive
	default:
		return StatusBenched
	}
}

func toClub(row sqlc.Club) Club {
	return Club{
		ID:           row.ID,
		UserID:       row.UserID,
		Type:         row.ClubType,
		Label:        row.Label,
		Brand:        row.Brand,
		Model:        row.Model,
		Loft:         row.Loft,
		Shaft:        row.Shaft,
		Flex:         row.Flex,
		Notes:        row.Notes,
		Status:       statusOf(row),
		RetiredAt:    row.RetiredAt,
		DisplayOrder: int(row.DisplayOrder),
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}
