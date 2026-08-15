package club

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

// Service implements the golf bag use cases.
//
// Every method takes the viewer's user ID and scopes to it. A club belonging to
// another user is reported as not found rather than forbidden: a bag is private,
// so confirming that someone else's club ID exists would leak more than the
// refusal is worth.
type Service struct {
	db *database.DB
}

func NewService(db *database.DB) *Service {
	return &Service{db: db}
}

// Bag returns every club the user owns, split into the three groups.
func (s *Service) Bag(ctx context.Context, userID string) (*Bag, error) {
	rows, err := s.db.Queries.ListClubsByUser(ctx, userID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list clubs: %w", err))
	}

	bag := &Bag{
		Active:    []Club{},
		Benched:   []Club{},
		Retired:   []Club{},
		ClubLimit: ClubLimit,
	}
	for _, row := range rows {
		c := toClub(row)
		switch c.Status {
		case StatusActive:
			bag.Active = append(bag.Active, c)
		case StatusBenched:
			bag.Benched = append(bag.Benched, c)
		case StatusRetired:
			bag.Retired = append(bag.Retired, c)
		}
	}
	bag.ActiveCount = len(bag.Active)
	bag.OverLimit = bag.ActiveCount > ClubLimit
	return bag, nil
}

// Get returns one club the user owns.
func (s *Service) Get(ctx context.Context, userID, clubID string) (*Club, error) {
	row, err := s.load(ctx, userID, clubID)
	if err != nil {
		return nil, err
	}
	c := toClub(row)
	return &c, nil
}

// ClubInput is the payload for creating or updating a club.
type ClubInput struct {
	Type  string
	Label string
	Brand *string
	Model *string
	Loft  *float64
	Shaft *string
	Flex  *string
	Notes *string
	// DisplayOrder is optional; a new club without one is appended to the bag.
	DisplayOrder *int
	// Status defaults to StatusActive on create, and is ignored on update —
	// SetStatus is the one way to move a club between groups.
	Status Status
}

// Create adds a club to the bag.
func (s *Service) Create(ctx context.Context, userID string, in ClubInput) (*Club, error) {
	order := 0
	if in.DisplayOrder != nil {
		order = *in.DisplayOrder
	} else {
		maxOrder, err := s.db.Queries.MaxClubDisplayOrder(ctx, userID)
		if err != nil {
			return nil, httpx.Internal(fmt.Errorf("max club display order: %w", err))
		}
		order = int(maxOrder) + 1
	}

	status := in.Status
	if status == "" {
		status = StatusActive
	}
	clubID := id.New()
	now := timex.Now()
	active, retiredAt := statusColumns(status, now)
	err := s.db.Queries.CreateClub(ctx, sqlc.CreateClubParams{
		ID:           clubID,
		UserID:       userID,
		ClubType:     in.Type,
		Label:        strings.TrimSpace(in.Label),
		Brand:        normalizeOptional(in.Brand),
		Model:        normalizeOptional(in.Model),
		Loft:         in.Loft,
		Shaft:        normalizeOptional(in.Shaft),
		Flex:         normalizeOptional(in.Flex),
		Notes:        normalizeOptional(in.Notes),
		Active:       active,
		RetiredAt:    retiredAt,
		DisplayOrder: int64(order),
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("create club: %w", err))
	}

	row, err := s.db.Queries.GetClub(ctx, clubID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("reload club: %w", err))
	}
	c := toClub(row)
	return &c, nil
}

// Update edits a club's descriptive fields. It deliberately does not change
// status: moving between active, benched, and retired goes through SetStatus,
// so an edit form saving stale state cannot silently pull a club back into the
// bag.
func (s *Service) Update(ctx context.Context, userID, clubID string, in ClubInput) (*Club, error) {
	row, err := s.load(ctx, userID, clubID)
	if err != nil {
		return nil, err
	}

	order := int(row.DisplayOrder)
	if in.DisplayOrder != nil {
		order = *in.DisplayOrder
	}

	err = s.db.Queries.UpdateClub(ctx, sqlc.UpdateClubParams{
		ClubType:     in.Type,
		Label:        strings.TrimSpace(in.Label),
		Brand:        normalizeOptional(in.Brand),
		Model:        normalizeOptional(in.Model),
		Loft:         in.Loft,
		Shaft:        normalizeOptional(in.Shaft),
		Flex:         normalizeOptional(in.Flex),
		Notes:        normalizeOptional(in.Notes),
		DisplayOrder: int64(order),
		UpdatedAt:    timex.Now(),
		ID:           clubID,
	})
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("update club: %w", err))
	}

	updated, err := s.db.Queries.GetClub(ctx, clubID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("reload club: %w", err))
	}
	c := toClub(updated)
	return &c, nil
}

// SetStatus moves a club between the active set, the bench, and retirement.
//
// Every transition is allowed, including un-retiring: a club that comes back
// from the shed lands on the bench rather than straight into the bag, so the
// active set only ever changes when the player says so explicitly.
func (s *Service) SetStatus(ctx context.Context, userID, clubID string, status Status) (*Club, error) {
	row, err := s.load(ctx, userID, clubID)
	if err != nil {
		return nil, err
	}

	now := timex.Now()
	active, retiredAt := statusColumns(status, now)

	// Retiring an already-retired club keeps its original retirement time; the
	// timestamp records when it left the bag, not when the row was last saved.
	if status == StatusRetired && row.RetiredAt != nil {
		retiredAt = row.RetiredAt
	}

	err = s.db.Queries.SetClubStatus(ctx, sqlc.SetClubStatusParams{
		Active:    active,
		RetiredAt: retiredAt,
		UpdatedAt: now,
		ID:        clubID,
	})
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("set club status: %w", err))
	}

	updated, err := s.db.Queries.GetClub(ctx, clubID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("reload club: %w", err))
	}
	c := toClub(updated)
	return &c, nil
}

// Delete removes a club permanently.
//
// Retiring is the right move once a club has been played, since Phase 3 rounds
// and Phase 4 shots reference club IDs and a hard delete would orphan them.
// This exists for correcting a club that was entered by mistake, and the UI
// steers toward Retire.
func (s *Service) Delete(ctx context.Context, userID, clubID string) error {
	if _, err := s.load(ctx, userID, clubID); err != nil {
		return err
	}
	if err := s.db.Queries.DeleteClub(ctx, clubID); err != nil {
		return httpx.Internal(fmt.Errorf("delete club: %w", err))
	}
	return nil
}

// statusColumns maps a status onto the two stored columns, which is the one
// place the mapping lives so active and retired_at cannot drift apart.
func statusColumns(status Status, now string) (active int64, retiredAt *string) {
	switch status {
	case StatusActive:
		return 1, nil
	case StatusRetired:
		return 0, &now
	default: // StatusBenched
		return 0, nil
	}
}

// load fetches a club and enforces that the caller owns it.
func (s *Service) load(ctx context.Context, userID, clubID string) (sqlc.Club, error) {
	row, err := s.db.Queries.GetClub(ctx, clubID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqlc.Club{}, httpx.NotFound("That club is not in your bag.")
		}
		return sqlc.Club{}, httpx.Internal(fmt.Errorf("load club: %w", err))
	}
	if row.UserID != userID {
		// Deliberately the same response as a missing club — see the note on
		// Service.
		return sqlc.Club{}, httpx.NotFound("That club is not in your bag.")
	}
	return row, nil
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
