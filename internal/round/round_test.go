package round

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/edingc/roundly/internal/course"
	"github.com/edingc/roundly/internal/database"
	"github.com/edingc/roundly/internal/database/sqlc"
	"github.com/edingc/roundly/internal/id"
	"github.com/edingc/roundly/internal/timex"
)

func newTestService(t *testing.T) (*Service, *course.Service, *database.DB) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	courses := course.NewService(db, nil)
	return NewService(db, courses), courses, db
}

func createUser(t *testing.T, db *database.DB, email string) string {
	t.Helper()
	userID := id.New()
	now := timex.Now()
	if err := db.Queries.CreateUser(context.Background(), sqlc.CreateUserParams{
		ID:          userID,
		Email:       email,
		DisplayName: email,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return userID
}

// createCourse builds an 18-hole course with one tee, par 4 everywhere except
// holes 3 and 12 (par 3) and 5 and 14 (par 5).
func createCourse(t *testing.T, courses *course.Service, userID string) *course.CourseDetail {
	t.Helper()

	white := "White"
	detail, err := courses.Create(context.Background(), userID, course.CreateCourseInput{
		Name:      "Test Links",
		HoleCount: 18,
		// Every rating deliberately distinct, so a test can tell which one was
		// read rather than which one happened to be right.
		Tees: []course.TeeInput{{
			Name:                    white,
			Color:                   "#FFFFFF",
			CourseRatingMen:         float64p(71.2),
			SlopeRatingMen:          intp(128),
			CourseRatingWomen:       float64p(74.8),
			SlopeRatingWomen:        intp(140),
			Front9CourseRatingMen:   float64p(35.6),
			Front9SlopeRatingMen:    intp(126),
			Back9CourseRatingMen:    float64p(35.9),
			Back9SlopeRatingMen:     intp(130),
			Front9CourseRatingWomen: float64p(37.2),
			Front9SlopeRatingWomen:  intp(138),
			Back9CourseRatingWomen:  float64p(37.6),
			Back9SlopeRatingWomen:   intp(142),
		}},
	})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}

	teeID := detail.Tees[0].ID
	for _, h := range detail.Holes {
		par, yardage := 4, 380
		switch h.HoleNumber {
		case 3, 12:
			par, yardage = 3, 165
		case 5, 14:
			par, yardage = 5, 520
		}
		if _, err := courses.SetTeeDetail(context.Background(), userID, h.ID, teeID, par, yardage); err != nil {
			t.Fatalf("set tee detail for hole %d: %v", h.HoleNumber, err)
		}
	}

	reloaded, err := courses.Get(context.Background(), detail.ID)
	if err != nil {
		t.Fatalf("reload course: %v", err)
	}
	return reloaded
}

func startRound(t *testing.T, svc *Service, userID string, c *course.CourseDetail, holes int, nine string) *Round {
	t.Helper()
	r, err := svc.Start(context.Background(), userID, StartInput{
		CourseID:  c.ID,
		TeeID:     c.Tees[0].ID,
		PlayedOn:  "2026-09-03",
		Holes:     holes,
		Nine:      nine,
		EntryMode: EntryLive,
	})
	if err != nil {
		t.Fatalf("start round: %v", err)
	}
	return r
}

// The decision the whole feature rests on: a round copies the course rather
// than pointing at it, so a later correction cannot restate what was shot.
func TestStartSnapshotsTheCourse(t *testing.T) {
	svc, courses, db := newTestService(t)
	userID := createUser(t, db, "player@example.com")
	c := createCourse(t, courses, userID)

	r := startRound(t, svc, userID, c, 18, "")
	if len(r.Holes) != 18 {
		t.Fatalf("holes = %d, want 18 pre-created", len(r.Holes))
	}
	if r.CourseName != "Test Links" || r.TeeName != "White" {
		t.Errorf("course/tee = %q/%q, want the names snapshotted", r.CourseName, r.TeeName)
	}
	if r.CourseRating == nil || *r.CourseRating != 71.2 {
		t.Errorf("course rating = %v, want 71.2 captured for a later handicap", r.CourseRating)
	}
	if r.SlopeRating == nil || *r.SlopeRating != 128 {
		t.Errorf("slope = %v, want 128", r.SlopeRating)
	}
	byNumber := holesByNumber(r)
	if byNumber[3].Par == nil || *byNumber[3].Par != 3 {
		t.Errorf("hole 3 par = %v, want 3", byNumber[3].Par)
	}
	if byNumber[5].Yardage == nil || *byNumber[5].Yardage != 520 {
		t.Errorf("hole 5 yardage = %v, want 520", byNumber[5].Yardage)
	}

	// Now somebody corrects the course. The round must not move.
	if _, err := courses.SetTeeDetail(context.Background(), userID, c.Holes[2].ID, c.Tees[0].ID, 5, 600); err != nil {
		t.Fatalf("edit course: %v", err)
	}
	after, err := svc.Get(context.Background(), userID, r.ID)
	if err != nil {
		t.Fatalf("reload round: %v", err)
	}
	if got := holesByNumber(after)[3]; got.Par == nil || *got.Par != 3 {
		t.Errorf("hole 3 par = %v after the course changed, want the snapshot 3", got.Par)
	}
}

// A nine-hole round rates against the nine-hole rating, which is computed
// separately and is not half the eighteen-hole one.
func TestStartNineHoleUsesTheNineHoleRating(t *testing.T) {
	svc, courses, db := newTestService(t)
	userID := createUser(t, db, "player@example.com")
	c := createCourse(t, courses, userID)

	r := startRound(t, svc, userID, c, 9, NineFront)
	if r.CourseRating == nil || *r.CourseRating != 35.6 {
		t.Errorf("rating = %v, want the front-nine 35.6", r.CourseRating)
	}
}

// A back nine keeps hole numbers 10-18 so the round reads against the scorecard
// it was played on.
func TestBackNineKeepsItsHoleNumbers(t *testing.T) {
	svc, courses, db := newTestService(t)
	userID := createUser(t, db, "player@example.com")
	c := createCourse(t, courses, userID)

	r := startRound(t, svc, userID, c, 9, NineBack)
	if len(r.Holes) != 9 {
		t.Fatalf("holes = %d, want 9", len(r.Holes))
	}
	if r.Holes[0].HoleNumber != 10 || r.Holes[8].HoleNumber != 18 {
		t.Errorf("holes run %d-%d, want 10-18", r.Holes[0].HoleNumber, r.Holes[8].HoleNumber)
	}
	if got := holesByNumber(r)[12]; got.Par == nil || *got.Par != 3 {
		t.Errorf("hole 12 par = %v, want the par 3 from the course", got.Par)
	}

	// And a front-nine hole is not part of it.
	_, err := svc.SaveHole(context.Background(), userID, r.ID, HoleInput{HoleNumber: 3, Strokes: intp(4)})
	if err == nil {
		t.Error("err = nil saving hole 3 on a back-nine round, want it refused")
	}
}

// The offline queue retries. A retry must not produce a second copy of the same
// afternoon, which is what the client-supplied id is for.
func TestStartIsIdempotentForAClientSuppliedID(t *testing.T) {
	svc, courses, db := newTestService(t)
	userID := createUser(t, db, "player@example.com")
	c := createCourse(t, courses, userID)

	roundID := id.New()
	in := StartInput{
		ID: roundID, CourseID: c.ID, TeeID: c.Tees[0].ID,
		PlayedOn: "2026-09-03", Holes: 18, EntryMode: EntryLive,
	}
	first, err := svc.Start(context.Background(), userID, in)
	if err != nil {
		t.Fatalf("first start: %v", err)
	}
	second, err := svc.Start(context.Background(), userID, in)
	if err != nil {
		t.Fatalf("replayed start: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("ids differ: %s vs %s", first.ID, second.ID)
	}

	page, err := svc.List(context.Background(), userID, 25, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Total != 1 {
		t.Errorf("total = %d, want 1 - a replay is not a second round", page.Total)
	}
}

// Saving a score must not blank the par the statistics depend on. This is what
// the COALESCE in the upsert is for.
func TestSavingAHoleKeepsItsSnapshot(t *testing.T) {
	svc, courses, db := newTestService(t)
	userID := createUser(t, db, "player@example.com")
	c := createCourse(t, courses, userID)
	r := startRound(t, svc, userID, c, 18, "")

	updated, err := svc.SaveHole(context.Background(), userID, r.ID, HoleInput{
		HoleNumber: 5, Strokes: intp(6), Putts: intp(2),
	})
	if err != nil {
		t.Fatalf("save hole: %v", err)
	}

	got := holesByNumber(updated)[5]
	if got.Par == nil || *got.Par != 5 {
		t.Errorf("par = %v after saving a score, want the snapshot 5", got.Par)
	}
	if got.Yardage == nil || *got.Yardage != 520 {
		t.Errorf("yardage = %v, want 520 kept", got.Yardage)
	}
	if updated.Summary.Strokes != 6 {
		t.Errorf("summary strokes = %d, want 6", updated.Summary.Strokes)
	}
}

// A mis-tapped score has to be clearable, which means the scoring fields
// replace outright rather than merging.
func TestAScoreCanBeCleared(t *testing.T) {
	svc, courses, db := newTestService(t)
	userID := createUser(t, db, "player@example.com")
	c := createCourse(t, courses, userID)
	r := startRound(t, svc, userID, c, 18, "")
	ctx := context.Background()

	if _, err := svc.SaveHole(ctx, userID, r.ID, HoleInput{HoleNumber: 1, Strokes: intp(9), Putts: intp(4)}); err != nil {
		t.Fatalf("save: %v", err)
	}
	cleared, err := svc.SaveHole(ctx, userID, r.ID, HoleInput{HoleNumber: 1})
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := holesByNumber(cleared)[1]; got.Strokes != nil || got.Putts != nil {
		t.Errorf("hole 1 = %v/%v, want both cleared", got.Strokes, got.Putts)
	}
	if cleared.Summary.HolesCompleted != 0 {
		t.Errorf("completed = %d, want 0", cleared.Summary.HolesCompleted)
	}
}

// A hole must not be able to name a club out of somebody else's bag.
func TestATeeClubMustBeYourOwn(t *testing.T) {
	svc, courses, db := newTestService(t)
	ctx := context.Background()
	userID := createUser(t, db, "player@example.com")
	strangerID := createUser(t, db, "stranger@example.com")
	c := createCourse(t, courses, userID)
	r := startRound(t, svc, userID, c, 18, "")

	strangerClub := id.New()
	now := timex.Now()
	if err := db.Queries.CreateClub(ctx, sqlc.CreateClubParams{
		ID: strangerClub, UserID: strangerID, ClubType: "driver", Label: "Driver",
		Active: 1, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create club: %v", err)
	}

	_, err := svc.SaveHole(ctx, userID, r.ID, HoleInput{
		HoleNumber: 1, Strokes: intp(4), TeeClubID: &strangerClub,
	})
	if err == nil {
		t.Error("err = nil, want another player's club refused")
	}
}

// Another player's round is a 404, not a 403: confirming an id exists leaks
// more than the refusal is worth.
func TestAnotherPlayersRoundIsNotFound(t *testing.T) {
	svc, courses, db := newTestService(t)
	userID := createUser(t, db, "player@example.com")
	strangerID := createUser(t, db, "stranger@example.com")
	c := createCourse(t, courses, userID)
	r := startRound(t, svc, userID, c, 18, "")

	if _, err := svc.Get(context.Background(), strangerID, r.ID); err == nil {
		t.Error("err = nil, want another player's round hidden")
	}
}

// Several rounds may be open at once, and the picker has to find them all.
func TestSeveralRoundsCanBeInProgress(t *testing.T) {
	svc, courses, db := newTestService(t)
	ctx := context.Background()
	userID := createUser(t, db, "player@example.com")
	c := createCourse(t, courses, userID)

	first := startRound(t, svc, userID, c, 18, "")
	second := startRound(t, svc, userID, c, 9, NineFront)

	open, err := svc.ListByStatus(ctx, userID, StatusInProgress)
	if err != nil {
		t.Fatalf("list in progress: %v", err)
	}
	if len(open) != 2 {
		t.Fatalf("in progress = %d, want 2", len(open))
	}

	// Abandoning one leaves the other alone.
	if _, err := svc.SetStatus(ctx, userID, first.ID, StatusAbandoned); err != nil {
		t.Fatalf("abandon: %v", err)
	}
	open, err = svc.ListByStatus(ctx, userID, StatusInProgress)
	if err != nil {
		t.Fatalf("list again: %v", err)
	}
	if len(open) != 1 || open[0].ID != second.ID {
		t.Errorf("in progress = %d, want just the second round", len(open))
	}

	// And an in-progress round can be deleted outright.
	if err := svc.Delete(ctx, userID, second.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	open, _ = svc.ListByStatus(ctx, userID, StatusInProgress)
	if len(open) != 0 {
		t.Errorf("in progress = %d after deleting, want 0", len(open))
	}
}

// Completing stamps the time; the schema refuses the pair drifting apart.
func TestCompleteAndReopen(t *testing.T) {
	svc, courses, db := newTestService(t)
	ctx := context.Background()
	userID := createUser(t, db, "player@example.com")
	c := createCourse(t, courses, userID)
	r := startRound(t, svc, userID, c, 18, "")

	done, err := svc.SetStatus(ctx, userID, r.ID, StatusComplete)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if done.Status != StatusComplete || done.CompletedAt == nil {
		t.Errorf("status/completed = %q/%v, want complete with a timestamp", done.Status, done.CompletedAt)
	}

	// A finished round is still editable - this is a logbook, not a card.
	if _, err := svc.SaveHole(ctx, userID, r.ID, HoleInput{HoleNumber: 1, Strokes: intp(4), Putts: intp(2)}); err != nil {
		t.Errorf("editing a completed round: %v", err)
	}

	reopened, err := svc.SetStatus(ctx, userID, r.ID, StatusInProgress)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if reopened.CompletedAt != nil {
		t.Errorf("completed_at = %v after reopening, want it cleared", reopened.CompletedAt)
	}
}

// A club that has been played keeps its row, so the rounds played with it stay
// readable. The label resolves even once the club is retired.
func TestClubPlayedAndLabelled(t *testing.T) {
	svc, courses, db := newTestService(t)
	ctx := context.Background()
	userID := createUser(t, db, "player@example.com")
	c := createCourse(t, courses, userID)
	r := startRound(t, svc, userID, c, 18, "")

	clubID := id.New()
	now := timex.Now()
	if err := db.Queries.CreateClub(ctx, sqlc.CreateClubParams{
		ID: clubID, UserID: userID, ClubType: "driver", Label: "Big Bertha",
		Active: 1, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create club: %v", err)
	}

	if played, err := svc.ClubPlayed(ctx, clubID); err != nil || played {
		t.Fatalf("ClubPlayed before use = %v, %v; want false", played, err)
	}

	updated, err := svc.SaveHole(ctx, userID, r.ID, HoleInput{
		HoleNumber: 1, Strokes: intp(4), Putts: intp(2), TeeClubID: &clubID,
	})
	if err != nil {
		t.Fatalf("save hole: %v", err)
	}
	got := holesByNumber(updated)[1]
	if got.TeeClubLabel == nil || *got.TeeClubLabel != "Big Bertha" {
		t.Errorf("club label = %v, want it resolved for display", got.TeeClubLabel)
	}

	if played, err := svc.ClubPlayed(ctx, clubID); err != nil || !played {
		t.Errorf("ClubPlayed after use = %v, %v; want true", played, err)
	}
}

func TestParseDate(t *testing.T) {
	for raw, want := range map[string]bool{
		"2026-09-03":           true,
		"2026-9-3":             false,
		"03/09/2026":           false,
		"":                     false,
		"2026-09-03T10:00:00Z": false,
	} {
		if _, ok := ParseDate(raw); ok != want {
			t.Errorf("ParseDate(%q) ok = %v, want %v", raw, ok, want)
		}
	}
}

func holesByNumber(r *Round) map[int]Hole {
	out := make(map[int]Hole, len(r.Holes))
	for _, h := range r.Holes {
		out[h.HoleNumber] = h
	}
	return out
}

func float64p(v float64) *float64 { return &v }

// setGender writes the profile field that chooses a rating set.
func setGender(t *testing.T, db *database.DB, userID, gender string) {
	t.Helper()
	var value *string
	if gender != "" {
		value = &gender
	}
	if err := db.Queries.SetUserGender(context.Background(), sqlc.SetUserGenderParams{
		Gender:    value,
		UpdatedAt: timex.Now(),
		ID:        userID,
	}); err != nil {
		t.Fatalf("set gender: %v", err)
	}
}

// The two nines are rated independently and routinely differ. Reading the
// front-nine number for a back-nine round was a bug in the first cut.
func TestNineHoleRoundsUseTheRightNinesRating(t *testing.T) {
	svc, courses, db := newTestService(t)
	userID := createUser(t, db, "player@example.com")
	c := createCourse(t, courses, userID)

	front := startRound(t, svc, userID, c, 9, NineFront)
	if front.CourseRating == nil || *front.CourseRating != 35.6 {
		t.Errorf("front nine rating = %v, want 35.6", front.CourseRating)
	}
	if front.SlopeRating == nil || *front.SlopeRating != 126 {
		t.Errorf("front nine slope = %v, want 126", front.SlopeRating)
	}

	back := startRound(t, svc, userID, c, 9, NineBack)
	if back.CourseRating == nil || *back.CourseRating != 35.9 {
		t.Errorf("back nine rating = %v, want 35.9 (the back nine's own)", back.CourseRating)
	}
	if back.SlopeRating == nil || *back.SlopeRating != 130 {
		t.Errorf("back nine slope = %v, want 130", back.SlopeRating)
	}
}

// Men's and women's ratings are separate published numbers for the same tee.
func TestRatingsFollowTheProfilesGender(t *testing.T) {
	svc, courses, db := newTestService(t)
	userID := createUser(t, db, "player@example.com")
	c := createCourse(t, courses, userID)

	cases := []struct {
		gender      string
		holes       int
		nine        string
		wantRating  float64
		wantSlope   int
		description string
	}{
		{"", 18, "", 71.2, 128, "unset falls back to the men's ratings"},
		{GenderMen, 18, "", 71.2, 128, "men, eighteen"},
		{GenderWomen, 18, "", 74.8, 140, "women, eighteen"},
		{GenderMen, 9, NineFront, 35.6, 126, "men, front nine"},
		{GenderWomen, 9, NineFront, 37.2, 138, "women, front nine"},
		{GenderMen, 9, NineBack, 35.9, 130, "men, back nine"},
		{GenderWomen, 9, NineBack, 37.6, 142, "women, back nine"},
	}

	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			setGender(t, db, userID, tc.gender)
			r := startRound(t, svc, userID, c, tc.holes, tc.nine)
			if r.CourseRating == nil || *r.CourseRating != tc.wantRating {
				t.Errorf("rating = %v, want %v", r.CourseRating, tc.wantRating)
			}
			if r.SlopeRating == nil || *r.SlopeRating != tc.wantSlope {
				t.Errorf("slope = %v, want %v", r.SlopeRating, tc.wantSlope)
			}
		})
	}
}

// Changing the setting later must not restate a round already played - the
// rating is snapshotted like everything else the course said that day.
func TestChangingGenderDoesNotMoveAnExistingRound(t *testing.T) {
	svc, courses, db := newTestService(t)
	userID := createUser(t, db, "player@example.com")
	c := createCourse(t, courses, userID)

	setGender(t, db, userID, GenderWomen)
	r := startRound(t, svc, userID, c, 18, "")
	if r.CourseRating == nil || *r.CourseRating != 74.8 {
		t.Fatalf("rating = %v, want the women's 74.8", r.CourseRating)
	}

	setGender(t, db, userID, GenderMen)
	after, err := svc.Get(context.Background(), userID, r.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.CourseRating == nil || *after.CourseRating != 74.8 {
		t.Errorf("rating = %v after the setting changed, want the snapshot 74.8", after.CourseRating)
	}
}

// A tee with no women's rating gives no rating rather than the men's: a round
// carrying somebody else's number is a wrong number that looks right.
func TestAMissingRatingIsNotSubstituted(t *testing.T) {
	svc, courses, db := newTestService(t)
	ctx := context.Background()
	userID := createUser(t, db, "player@example.com")

	detail, err := courses.Create(ctx, userID, course.CreateCourseInput{
		Name:      "Men's Ratings Only",
		HoleCount: 18,
		Tees: []course.TeeInput{{
			Name: "Blue", Color: "#0000FF",
			CourseRatingMen: float64p(70.0), SlopeRatingMen: intp(125),
		}},
	})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}

	setGender(t, db, userID, GenderWomen)
	r, err := svc.Start(ctx, userID, StartInput{
		CourseID: detail.ID, TeeID: detail.Tees[0].ID,
		PlayedOn: "2026-09-03", Holes: 18, EntryMode: EntryManual,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if r.CourseRating != nil {
		t.Errorf("rating = %v, want nil rather than the men's number", r.CourseRating)
	}
}

// The differential is what the rounds list and the overview both read, so it
// belongs to the round rather than to whichever screen asks.
func TestRoundCarriesItsDifferential(t *testing.T) {
	svc, courses, db := newTestService(t)
	ctx := context.Background()
	userID := createUser(t, db, "player@example.com")

	// Slope 113 makes the differential exactly (strokes - rating), which is
	// what makes this checkable by hand.
	detail, err := courses.Create(ctx, userID, course.CreateCourseInput{
		Name:      "Checkable GC",
		HoleCount: 18,
		Tees: []course.TeeInput{{
			Name: "White", Color: "#FFFFFF",
			CourseRatingMen: float64p(72.0), SlopeRatingMen: intp(113),
		}},
	})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}
	for _, h := range detail.Holes {
		if _, err := courses.SetTeeDetail(ctx, userID, h.ID, detail.Tees[0].ID, 4, 380); err != nil {
			t.Fatalf("set tee detail: %v", err)
		}
	}

	r, err := svc.Start(ctx, userID, StartInput{
		CourseID: detail.ID, TeeID: detail.Tees[0].ID,
		PlayedOn: "2026-09-03", Holes: 18, EntryMode: EntryManual,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// Half a card: no differential, because a partial one is meaningless rather
	// than merely smaller.
	partial := make([]HoleInput, 0, 9)
	for i := 1; i <= 9; i++ {
		partial = append(partial, HoleInput{HoleNumber: i, Strokes: intp(5), Putts: intp(2)})
	}
	half, err := svc.SaveHoles(ctx, userID, r.ID, partial)
	if err != nil {
		t.Fatalf("save half: %v", err)
	}
	if half.Differential != nil {
		t.Errorf("differential = %v on a half-finished card, want nil", half.Differential)
	}

	// The whole card at 5 a hole: 90 strokes, so 90 - 72 = 18.0.
	all := make([]HoleInput, 0, 18)
	for i := 1; i <= 18; i++ {
		all = append(all, HoleInput{HoleNumber: i, Strokes: intp(5), Putts: intp(2)})
	}
	full, err := svc.SaveHoles(ctx, userID, r.ID, all)
	if err != nil {
		t.Fatalf("save all: %v", err)
	}
	if full.Differential == nil || *full.Differential != 18.0 {
		t.Errorf("differential = %v, want 18.0", full.Differential)
	}
}

// A course with no published rating is common. It gives no differential rather
// than a wrong one.
func TestNoRatingMeansNoDifferential(t *testing.T) {
	svc, courses, db := newTestService(t)
	ctx := context.Background()
	userID := createUser(t, db, "player@example.com")

	detail, err := courses.Create(ctx, userID, course.CreateCourseInput{
		Name: "Unrated Muni", HoleCount: 18,
		Tees: []course.TeeInput{{Name: "Only", Color: "#FFFFFF"}},
	})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}
	for _, h := range detail.Holes {
		if _, err := courses.SetTeeDetail(ctx, userID, h.ID, detail.Tees[0].ID, 4, 380); err != nil {
			t.Fatalf("set tee detail: %v", err)
		}
	}

	r, err := svc.Start(ctx, userID, StartInput{
		CourseID: detail.ID, TeeID: detail.Tees[0].ID,
		PlayedOn: "2026-09-03", Holes: 18, EntryMode: EntryManual,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	holes := make([]HoleInput, 0, 18)
	for i := 1; i <= 18; i++ {
		holes = append(holes, HoleInput{HoleNumber: i, Strokes: intp(4), Putts: intp(2)})
	}
	full, err := svc.SaveHoles(ctx, userID, r.ID, holes)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if full.Differential != nil {
		t.Errorf("differential = %v with no rating, want nil", full.Differential)
	}
	// The round is otherwise perfectly normal.
	if full.Summary.Strokes != 72 {
		t.Errorf("strokes = %d, want 72", full.Summary.Strokes)
	}
}
