package stats

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/edingc/roundly/internal/course"
	"github.com/edingc/roundly/internal/database"
	"github.com/edingc/roundly/internal/database/sqlc"
	"github.com/edingc/roundly/internal/id"
	"github.com/edingc/roundly/internal/round"
	"github.com/edingc/roundly/internal/timex"
)

type fixture struct {
	stats   *Service
	rounds  *round.Service
	courses *course.Service
	userID  string
	course  *course.CourseDetail
}

// A player and an eighteen-hole course whose only tee is rated 72.0/113. A
// slope of 113 makes the differential exactly (score - rating), so a test can
// state the number it expects rather than derive it.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	courses := course.NewService(db, nil)
	userID := id.New()
	now := timex.Now()
	if err := db.Queries.CreateUser(context.Background(), sqlc.CreateUserParams{
		ID:          userID,
		Email:       "player@example.com",
		DisplayName: "player",
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	rating, slope := 72.0, 113
	detail, err := courses.Create(context.Background(), userID, course.CreateCourseInput{
		Name:      "Rated Links",
		HoleCount: 18,
		Tees: []course.TeeInput{{
			Name:            "White",
			Color:           "#FFFFFF",
			CourseRatingMen: &rating,
			SlopeRatingMen:  &slope,
		}},
	})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}

	f := &fixture{
		stats:   NewService(db),
		rounds:  round.NewService(db, courses),
		courses: courses,
		userID:  userID,
	}
	f.setPars(t, detail.ID, detail.Tees[0].ID)
	f.course = f.reload(t, detail.ID)
	return f
}

// addUnratedTee returns a second tee with no rating, which is the common case
// this app has to survive: a course somebody entered without looking up its
// rating produces rounds that cannot feed a handicap.
func (f *fixture) addUnratedTee(t *testing.T) string {
	t.Helper()
	tee, err := f.courses.AddTee(context.Background(), f.userID, f.course.ID, course.TeeInput{
		Name:  "Unrated",
		Color: "#000000",
	})
	if err != nil {
		t.Fatalf("add unrated tee: %v", err)
	}
	f.setPars(t, f.course.ID, tee.ID)
	f.course = f.reload(t, f.course.ID)
	return tee.ID
}

// Par 4 on every hole, so eighteen holes is a par 72.
func (f *fixture) setPars(t *testing.T, courseID, teeID string) {
	t.Helper()
	detail := f.reload(t, courseID)
	for _, h := range detail.Holes {
		if _, err := f.courses.SetTeeDetail(context.Background(), f.userID, h.ID, teeID, 4, 380); err != nil {
			t.Fatalf("set tee detail for hole %d: %v", h.HoleNumber, err)
		}
	}
}

func (f *fixture) reload(t *testing.T, courseID string) *course.CourseDetail {
	t.Helper()
	detail, err := f.courses.Get(context.Background(), courseID)
	if err != nil {
		t.Fatalf("reload course: %v", err)
	}
	return detail
}

// play records one finished round of `strokes` shots over eighteen holes. On the
// rated tee its differential is strokes - 72.
func (f *fixture) play(t *testing.T, playedOn string, strokes int, teeID string) {
	t.Helper()
	ctx := context.Background()

	r, err := f.rounds.Start(ctx, f.userID, round.StartInput{
		CourseID:  f.course.ID,
		TeeID:     teeID,
		PlayedOn:  playedOn,
		Holes:     18,
		EntryMode: round.EntryManual,
	})
	if err != nil {
		t.Fatalf("start round on %s: %v", playedOn, err)
	}

	// Spread the total: the holes over par 72 take a fifth shot.
	holes := make([]round.HoleInput, 0, 18)
	for i := 1; i <= 18; i++ {
		shots := 4
		if i <= strokes-72 {
			shots = 5
		}
		putts := 2
		holes = append(holes, round.HoleInput{HoleNumber: i, Strokes: &shots, Putts: &putts})
	}
	if _, err := f.rounds.SaveHoles(ctx, f.userID, r.ID, holes); err != nil {
		t.Fatalf("save holes on %s: %v", playedOn, err)
	}
	if _, err := f.rounds.SetStatus(ctx, f.userID, r.ID, round.StatusComplete); err != nil {
		t.Fatalf("complete round on %s: %v", playedOn, err)
	}
}

// The window selector changes the question the averages answer. It does not
// change the handicap, which is defined over the last twenty rated rounds
// rather than over the span somebody happens to be looking at.
func TestHandicapIgnoresTheWindow(t *testing.T) {
	f := newFixture(t)
	rated := f.course.Tees[0].ID

	// Twenty-five rounds, oldest first: five 100s, then twenty 90s. The newest
	// twenty are all 90s, so the 100s are out of the record entirely.
	for i := range 5 {
		f.play(t, fmt.Sprintf("2026-01-%02d", i+1), 100, rated)
	}
	for i := range 20 {
		f.play(t, fmt.Sprintf("2026-02-%02d", i+1), 90, rated)
	}

	// Best 8 of twenty differentials of 18.0 is 18.0, at every window.
	for _, window := range []int{5, 10, 20, 50, 0} {
		out, err := f.stats.Overview(context.Background(), f.userID, window)
		if err != nil {
			t.Fatalf("overview(window=%d): %v", window, err)
		}
		if out.Handicap == nil {
			t.Fatalf("overview(window=%d): handicap = nil", window)
		}
		closeTo(t, fmt.Sprintf("index at window %d", window), out.Handicap.Index, 18.0)
		if out.Handicap.IndexUsing != 8 || out.Handicap.DifferentialsAvailable != 20 {
			t.Errorf("overview(window=%d): best %d of %d, want best 8 of 20",
				window, out.Handicap.IndexUsing, out.Handicap.DifferentialsAvailable)
		}
		if out.Handicap.AntiCapUsing != 12 {
			t.Errorf("overview(window=%d): anti-cap used %d, want the worst 12", window, out.Handicap.AntiCapUsing)
		}
	}

	// The averages, meanwhile, are supposed to move with the window: five rounds
	// of 100 at the front, twenty-five rounds all told.
	narrow, err := f.stats.Overview(context.Background(), f.userID, 5)
	if err != nil {
		t.Fatalf("overview(window=5): %v", err)
	}
	if narrow.RoundsCounted != 5 {
		t.Errorf("rounds counted at window 5 = %d, want 5", narrow.RoundsCounted)
	}
	all, err := f.stats.Overview(context.Background(), f.userID, 0)
	if err != nil {
		t.Fatalf("overview(window=0): %v", err)
	}
	if all.RoundsCounted != 25 {
		t.Errorf("rounds counted at window 0 = %d, want 25", all.RoundsCounted)
	}
}

// A rated round is what feeds a handicap, so a stretch of rounds on an unrated
// course pushes the record further back rather than shrinking it.
func TestHandicapReachesPastUnratedRounds(t *testing.T) {
	f := newFixture(t)
	rated := f.course.Tees[0].ID
	unrated := f.addUnratedTee(t)

	// Three rated rounds, then twenty unrated ones on top of them - enough to
	// push the rated ones past the twentieth round played, which is where the
	// record used to stop looking.
	for i := range 3 {
		f.play(t, fmt.Sprintf("2026-03-%02d", i+1), 90, rated)
	}
	for i := range 20 {
		f.play(t, fmt.Sprintf("2026-04-%02d", i+1), 95, unrated)
	}

	out, err := f.stats.Overview(context.Background(), f.userID, 20)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if out.RoundsCounted != 20 {
		t.Errorf("rounds counted = %d, want the 20 the window asked for", out.RoundsCounted)
	}
	if out.Handicap == nil {
		t.Fatal("handicap = nil, want the three rated rounds found behind the unrated ones")
	}
	if out.Handicap.DifferentialsAvailable != 3 {
		t.Errorf("differentials available = %d, want 3", out.Handicap.DifferentialsAvailable)
	}
	// Best 1 of 3, minus 2.
	closeTo(t, "index", out.Handicap.Index, 16.0)
}
