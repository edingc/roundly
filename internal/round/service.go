package round

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/edingc/roundly/internal/course"
	"github.com/edingc/roundly/internal/database"
	"github.com/edingc/roundly/internal/database/sqlc"
	"github.com/edingc/roundly/internal/httpx"
	"github.com/edingc/roundly/internal/id"
	"github.com/edingc/roundly/internal/timex"
)

// Service implements the round use cases. Every method takes the caller's own
// user ID; nothing here reads or writes another player's round.
type Service struct {
	db      *database.DB
	courses *course.Service
}

func NewService(db *database.DB, courses *course.Service) *Service {
	return &Service{db: db, courses: courses}
}

// StartInput is what beginning a round needs.
type StartInput struct {
	// ID is supplied by the client. This is what makes a round that begins out
	// of signal possible: without an id of its own, a round has nothing for its
	// holes to attach to until the server has seen it. Blank means the server
	// mints one, which is what the manual path does.
	ID       string
	CourseID string
	TeeID    string
	PlayedOn string
	// Holes is 9 or 18; Nine is "front" or "back" when playing nine of an
	// eighteen-hole course.
	Holes     int
	Nine      string
	EntryMode string
	Notes     *string
}

// Start creates a round and pre-creates its holes from the course.
//
// The holes are created up front rather than on first write, because that is
// the moment the snapshot has to be taken: it is the only point at which the
// course is known to say what it said when play began. It also means the client
// receives the pars immediately, which is what the live screen and the manual
// grid both render before a single score exists.
func (s *Service) Start(ctx context.Context, userID string, in StartInput) (*Round, error) {
	detail, err := s.courses.Get(ctx, in.CourseID)
	if err != nil {
		return nil, err
	}

	tee, ok := findTee(detail, in.TeeID)
	if !ok {
		return nil, httpx.ValidationError(map[string]string{
			"tee_id": "That tee is not on this course.",
		})
	}

	// Which rating set applies is a property of the player, read at the moment
	// the round starts and then snapshotted like everything else: changing the
	// setting later must not restate a round already played.
	gender := ""
	if user, err := s.db.Queries.GetUserByID(ctx, userID); err == nil {
		gender = derefString(user.Gender)
	}

	roundID := in.ID
	if roundID == "" {
		roundID = id.New()
	}

	// A client-supplied id that already names one of this player's rounds is a
	// replayed request, not a new round: the offline queue retries, and a retry
	// must not produce a second copy of the same afternoon.
	if existing, err := s.db.Queries.GetRound(ctx, sqlc.GetRoundParams{ID: roundID, UserID: userID}); err == nil {
		return s.assemble(ctx, existing)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.Internal(fmt.Errorf("look up round: %w", err))
	}

	now := timex.Now()
	var startedAt *string
	if in.EntryMode == EntryLive {
		startedAt = &now
	}
	var nine *string
	if in.Nine != "" {
		nine = &in.Nine
	}

	params := sqlc.CreateRoundParams{
		ID:            roundID,
		UserID:        userID,
		CourseID:      &detail.ID,
		CourseName:    detail.Name,
		TeeID:         &tee.ID,
		TeeName:       tee.Name,
		TeeColor:      &tee.Color,
		CourseRating:  ratingFor(tee, in.Holes, in.Nine, gender),
		SlopeRating:   intPtrToInt64Ptr(slopeFor(tee, in.Holes, in.Nine, gender)),
		PlayedOn:      in.PlayedOn,
		StartedAt:     startedAt,
		Status:        StatusInProgress,
		EntryMode:     in.EntryMode,
		HolesIntended: int64(in.Holes),
		Nine:          nine,
		Notes:         in.Notes,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	numbers := holeNumbers(in.Holes, in.Nine)
	if err := s.db.InTx(func(q *sqlc.Queries) error {
		if err := q.CreateRound(ctx, params); err != nil {
			return fmt.Errorf("create round: %w", err)
		}
		for _, n := range numbers {
			par, yardage, strokeIndex := snapshotHole(detail, tee.ID, n)
			if err := q.UpsertRoundHole(ctx, sqlc.UpsertRoundHoleParams{
				ID:          id.New(),
				RoundID:     roundID,
				HoleNumber:  int64(n),
				Par:         intPtrToInt64Ptr(par),
				Yardage:     intPtrToInt64Ptr(yardage),
				StrokeIndex: intPtrToInt64Ptr(strokeIndex),
			}); err != nil {
				return fmt.Errorf("create round hole %d: %w", n, err)
			}
		}
		return nil
	}); err != nil {
		return nil, httpx.Internal(err)
	}

	return s.Get(ctx, userID, roundID)
}

// Get returns one round with its holes and summary.
func (s *Service) Get(ctx context.Context, userID, roundID string) (*Round, error) {
	row, err := s.load(ctx, userID, roundID)
	if err != nil {
		return nil, err
	}
	return s.assemble(ctx, row)
}

// List returns a page of the caller's rounds, newest played first.
func (s *Service) List(ctx context.Context, userID string, limit, offset int) (*Page, error) {
	rows, err := s.db.Queries.ListRounds(ctx, sqlc.ListRoundsParams{
		UserID: userID,
		Limit:  int64(limit),
		Offset: int64(offset),
	})
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list rounds: %w", err))
	}
	total, err := s.db.Queries.CountRounds(ctx, userID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("count rounds: %w", err))
	}

	items := make([]Round, 0, len(rows))
	for _, row := range rows {
		full, err := s.assemble(ctx, row)
		if err != nil {
			return nil, err
		}
		items = append(items, *full)
	}
	return &Page{Items: items, Total: int(total), Limit: limit, Offset: offset}, nil
}

// ListByStatus returns every round in one state, for the resume picker.
//
// A list rather than a lookup because several rounds may be open at once: a
// player who abandons one to weather and starts another the next morning should
// not have to tidy up before they can play.
func (s *Service) ListByStatus(ctx context.Context, userID, status string) ([]Round, error) {
	rows, err := s.db.Queries.ListRoundsByStatus(ctx, sqlc.ListRoundsByStatusParams{
		UserID: userID,
		Status: status,
	})
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list rounds by status: %w", err))
	}
	items := make([]Round, 0, len(rows))
	for _, row := range rows {
		full, err := s.assemble(ctx, row)
		if err != nil {
			return nil, err
		}
		items = append(items, *full)
	}
	return items, nil
}

// MetaInput is the round-level detail a player can change after the fact.
type MetaInput struct {
	PlayedOn string
	Notes    *string
}

// UpdateMeta changes the date and notes, and nothing else.
//
// Separate from the hole endpoints for the reason PUT /clubs/{id} is separate
// from PUT /clubs/{id}/status: a stale form must not be able to change
// something the person saving it was not looking at.
func (s *Service) UpdateMeta(ctx context.Context, userID, roundID string, in MetaInput) (*Round, error) {
	if _, err := s.load(ctx, userID, roundID); err != nil {
		return nil, err
	}
	if err := s.db.Queries.UpdateRound(ctx, sqlc.UpdateRoundParams{
		PlayedOn:  in.PlayedOn,
		Notes:     in.Notes,
		UpdatedAt: timex.Now(),
		ID:        roundID,
		UserID:    userID,
	}); err != nil {
		return nil, httpx.Internal(fmt.Errorf("update round: %w", err))
	}
	return s.Get(ctx, userID, roundID)
}

// HoleInput is one hole's worth of scoring.
//
// The scoring fields replace outright, nil included: this arrives by PUT, the
// payload is the whole hole, and clearing a mis-tapped score has to work. The
// snapshot fields are kept unless supplied, so an ordinary save cannot blank
// the par the statistics depend on.
type HoleInput struct {
	HoleNumber      int
	Par             *int
	Strokes         *int
	Putts           *int
	TeeClubID       *string
	TeeAccuracy     *string
	FirstPuttFeet   *int
	FairwayBunker   bool
	GreensideBunker bool
	Penalties       int
	PenaltyType     *string
}

// SaveHole writes one hole. This is the live path, and it is idempotent: the
// same hole sent twice is the same hole, which is what makes a queued write
// safe to replay after a dead zone.
func (s *Service) SaveHole(ctx context.Context, userID, roundID string, in HoleInput) (*Round, error) {
	return s.SaveHoles(ctx, userID, roundID, []HoleInput{in})
}

// SaveHoles writes many holes in one transaction. This is the manual path: one
// save for the whole grid, so a scorecard is never half-entered.
func (s *Service) SaveHoles(ctx context.Context, userID, roundID string, holes []HoleInput) (*Round, error) {
	row, err := s.load(ctx, userID, roundID)
	if err != nil {
		return nil, err
	}

	allowed := make(map[int]bool, 18)
	for _, n := range holeNumbers(int(row.HolesIntended), derefString(row.Nine)) {
		allowed[n] = true
	}

	// A tee club has to be one of this player's own. The column is a plain
	// foreign key, so without this a hole could name any club id in the
	// database - which would both leak that the id exists and put somebody
	// else's driver on your scorecard.
	ownedClubs, err := s.ownedClubIDs(ctx, userID, holes)
	if err != nil {
		return nil, err
	}
	for _, h := range holes {
		if h.TeeClubID != nil && !ownedClubs[*h.TeeClubID] {
			return nil, httpx.ValidationError(map[string]string{
				"tee_club_id": "That club is not in your bag.",
			})
		}
	}

	if err := s.db.InTx(func(q *sqlc.Queries) error {
		for _, h := range holes {
			if !allowed[h.HoleNumber] {
				// A hole outside the round's own nine is a client bug, not a
				// score. Refusing keeps a back-nine round from quietly growing
				// a hole 3.
				return httpx.ValidationError(map[string]string{
					"hole_number": fmt.Sprintf("Hole %d is not part of this round.", h.HoleNumber),
				})
			}
			if err := q.UpsertRoundHole(ctx, sqlc.UpsertRoundHoleParams{
				ID:              id.New(),
				RoundID:         roundID,
				HoleNumber:      int64(h.HoleNumber),
				Par:             intPtrToInt64Ptr(h.Par),
				Strokes:         intPtrToInt64Ptr(h.Strokes),
				Putts:           intPtrToInt64Ptr(h.Putts),
				TeeClubID:       h.TeeClubID,
				TeeAccuracy:     h.TeeAccuracy,
				FirstPuttFeet:   intPtrToInt64Ptr(h.FirstPuttFeet),
				FairwayBunker:   boolToInt64(h.FairwayBunker),
				GreensideBunker: boolToInt64(h.GreensideBunker),
				Penalties:       int64(h.Penalties),
				PenaltyType:     h.PenaltyType,
			}); err != nil {
				return fmt.Errorf("save hole %d: %w", h.HoleNumber, err)
			}
		}
		return q.UpdateRound(ctx, sqlc.UpdateRoundParams{
			PlayedOn:  row.PlayedOn,
			Notes:     row.Notes,
			UpdatedAt: timex.Now(),
			ID:        roundID,
			UserID:    userID,
		})
	}); err != nil {
		var apiErr *httpx.APIError
		if errors.As(err, &apiErr) {
			return nil, apiErr
		}
		return nil, httpx.Internal(err)
	}

	return s.Get(ctx, userID, roundID)
}

// SetStatus completes or abandons a round, or reopens one for editing.
func (s *Service) SetStatus(ctx context.Context, userID, roundID, status string) (*Round, error) {
	if _, err := s.load(ctx, userID, roundID); err != nil {
		return nil, err
	}

	// The schema enforces that a complete round has a completion time and that
	// nothing else does. Deriving it here rather than taking it from the client
	// is what keeps the pair honest.
	var completedAt *string
	if status == StatusComplete {
		now := timex.Now()
		completedAt = &now
	}

	if err := s.db.Queries.SetRoundStatus(ctx, sqlc.SetRoundStatusParams{
		Status:      status,
		CompletedAt: completedAt,
		UpdatedAt:   timex.Now(),
		ID:          roundID,
		UserID:      userID,
	}); err != nil {
		return nil, httpx.Internal(fmt.Errorf("set round status: %w", err))
	}
	return s.Get(ctx, userID, roundID)
}

// Delete removes a round and its holes.
func (s *Service) Delete(ctx context.Context, userID, roundID string) error {
	if _, err := s.load(ctx, userID, roundID); err != nil {
		return err
	}
	if err := s.db.Queries.DeleteRound(ctx, sqlc.DeleteRoundParams{ID: roundID, UserID: userID}); err != nil {
		return httpx.Internal(fmt.Errorf("delete round: %w", err))
	}
	return nil
}

// ClubPlayed reports whether a club appears in any round.
//
// Exported for internal/club, which has to refuse deleting one that does. A
// club that has been played must keep its row, or the rounds played with it
// lose the only record of what was in hand.
func (s *Service) ClubPlayed(ctx context.Context, clubID string) (bool, error) {
	count, err := s.db.Queries.CountRoundHolesByClub(ctx, &clubID)
	if err != nil {
		return false, httpx.Internal(fmt.Errorf("count rounds using club: %w", err))
	}
	return count > 0, nil
}

// ownedClubIDs returns the caller's club ids, but only when a save actually
// names one - most holes do not, and the common case should cost no query.
//
// Retired clubs count as owned: a round played with a club that has since left
// the bag is still a round played with it.
func (s *Service) ownedClubIDs(ctx context.Context, userID string, holes []HoleInput) (map[string]bool, error) {
	wanted := false
	for _, h := range holes {
		if h.TeeClubID != nil {
			wanted = true
			break
		}
	}
	if !wanted {
		return nil, nil
	}

	clubs, err := s.db.Queries.ListClubsByUser(ctx, userID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list clubs: %w", err))
	}
	owned := make(map[string]bool, len(clubs))
	for _, c := range clubs {
		owned[c.ID] = true
	}
	return owned, nil
}

// load fetches a round scoped to its owner.
//
// A round belonging to somebody else is a 404 rather than a 403, matching the
// golf bag: confirming that an id exists would leak more than the refusal is
// worth.
func (s *Service) load(ctx context.Context, userID, roundID string) (sqlc.Round, error) {
	row, err := s.db.Queries.GetRound(ctx, sqlc.GetRoundParams{ID: roundID, UserID: userID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqlc.Round{}, httpx.NotFound("That round does not exist.")
		}
		return sqlc.Round{}, httpx.Internal(fmt.Errorf("load round: %w", err))
	}
	return row, nil
}

// assemble attaches holes, resolves club labels, and computes the summary.
func (s *Service) assemble(ctx context.Context, row sqlc.Round) (*Round, error) {
	holeRows, err := s.db.Queries.ListRoundHoles(ctx, row.ID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list round holes: %w", err))
	}

	out := toRound(row)
	out.Holes = make([]Hole, 0, len(holeRows))
	for _, hr := range holeRows {
		out.Holes = append(out.Holes, toHole(hr))
	}
	if err := s.labelClubs(ctx, row.UserID, out.Holes); err != nil {
		return nil, err
	}
	out.Summary = Summarize(out.Holes)
	out.Differential = DifferentialFor(out.CourseRating, out.SlopeRating, out.Summary, out.HolesIntended)
	return &out, nil
}

// labelClubs fills in the display name for each tee club.
//
// Reads the whole bag once rather than a query per hole, and reads it including
// retired clubs: a round played in 2024 still has to render the three-wood that
// has since been sold.
func (s *Service) labelClubs(ctx context.Context, userID string, holes []Hole) error {
	needed := false
	for _, h := range holes {
		if h.TeeClubID != nil {
			needed = true
			break
		}
	}
	if !needed {
		return nil
	}

	clubs, err := s.db.Queries.ListClubsByUser(ctx, userID)
	if err != nil {
		return httpx.Internal(fmt.Errorf("list clubs: %w", err))
	}
	labels := make(map[string]string, len(clubs))
	for _, c := range clubs {
		labels[c.ID] = c.Label
	}
	for i := range holes {
		if holes[i].TeeClubID == nil {
			continue
		}
		if label, ok := labels[*holes[i].TeeClubID]; ok {
			holes[i].TeeClubLabel = &label
		}
	}
	return nil
}

// holeNumbers is which holes a round covers.
//
// A back nine keeps 10-18 rather than renumbering to 1-9: the card in the
// player's hand says 10, the stroke indexes are the back-nine ones, and
// renumbering would make the round unreadable against the scorecard it was
// played on.
func holeNumbers(holes int, nine string) []int {
	start := 1
	if holes == 9 && nine == NineBack {
		start = 10
	}
	count := holes
	if count != 9 {
		count = 18
		start = 1
	}
	numbers := make([]int, 0, count)
	for i := range count {
		numbers = append(numbers, start+i)
	}
	return numbers
}

// snapshotHole reads par, yardage, and stroke index for one hole off the course
// as it stands right now. Called once, when the round starts.
func snapshotHole(detail *course.CourseDetail, teeID string, holeNumber int) (par, yardage, strokeIndex *int) {
	for _, h := range detail.Holes {
		if h.HoleNumber != holeNumber {
			continue
		}
		strokeIndex = h.HandicapIndex
		for _, d := range h.TeeDetails {
			if d.TeeID == teeID {
				p, y := d.Par, d.Yardage
				return &p, &y, strokeIndex
			}
		}
		return nil, nil, strokeIndex
	}
	return nil, nil, nil
}

func findTee(detail *course.CourseDetail, teeID string) (course.Tee, bool) {
	for _, t := range detail.Tees {
		if t.ID == teeID {
			return t, true
		}
	}
	return course.Tee{}, false
}

// ratingFor picks the course rating that matches what is being played and who
// is playing it.
//
// Three things vary, and every one of them is a genuinely different published
// number rather than a derivation of another:
//
//   - Nine holes rate against a nine-hole rating, not half the eighteen-hole
//     one. They are computed separately.
//   - The back nine has its own rating. Reading the front-nine number for a
//     back-nine round was a bug in the first cut of this function: the two
//     nines are rated independently and routinely differ by a stroke or more.
//   - Men's and women's ratings differ because the same markers rate
//     differently against the two scratch-and-bogey-golfer models. The player's
//     profile chooses; unset means men's, which is what this did before the
//     column existed.
//
// A missing rating comes back nil rather than falling back to another set. A
// round with no rating simply has no handicap input, which is honest; a round
// carrying somebody else's rating would be a wrong number that looks right.
func ratingFor(tee course.Tee, holes int, nine, gender string) *float64 {
	women := gender == GenderWomen
	if holes != 9 {
		if women {
			return tee.CourseRatingWomen
		}
		return tee.CourseRatingMen
	}
	if nine == NineBack {
		if women {
			return tee.Back9CourseRatingWomen
		}
		return tee.Back9CourseRatingMen
	}
	if women {
		return tee.Front9CourseRatingWomen
	}
	return tee.Front9CourseRatingMen
}

// slopeFor is ratingFor's twin, and splits the same three ways.
func slopeFor(tee course.Tee, holes int, nine, gender string) *int {
	women := gender == GenderWomen
	if holes != 9 {
		if women {
			return tee.SlopeRatingWomen
		}
		return tee.SlopeRatingMen
	}
	if nine == NineBack {
		if women {
			return tee.Back9SlopeRatingWomen
		}
		return tee.Back9SlopeRatingMen
	}
	if women {
		return tee.Front9SlopeRatingWomen
	}
	return tee.Front9SlopeRatingMen
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ParseDate checks a played-on date and returns it normalised.
//
// A calendar date, not a timestamp: nobody's round is "on the 3rd in UTC", and
// a round played at dusk must not land on tomorrow because the server is in
// Europe.
func ParseDate(raw string) (string, bool) {
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return "", false
	}
	return t.Format("2006-01-02"), true
}
