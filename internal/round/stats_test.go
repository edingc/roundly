package round

import "testing"

func hole(number, par, strokes, putts int) Hole {
	return Hole{HoleNumber: number, Par: &par, Strokes: &strokes, Putts: &putts}
}

// The table from docs/phase-3-plan.md, turned into assertions. Deriving GIR
// from a score and a putt count is exact whenever the ball finished on the
// green and generous when the hole was finished from off it; both halves of
// that are pinned here so a later "simplification" cannot quietly change what
// the statistic means.
func TestGreenInRegulation(t *testing.T) {
	cases := []struct {
		name                string
		strokes, putts, par int
		want                bool
	}{
		{"green in two, two putts", 4, 2, 4, true},
		{"green in two, one putt", 3, 1, 4, true},
		{"green in three, two putts", 5, 2, 4, false},
		{"missed, chipped, one putt", 4, 1, 4, false},
		{"par 3, one putt", 2, 1, 3, true},
		{"par 3, missed and scrambled", 3, 1, 3, false},
		{"par 5, on in three", 5, 2, 5, true},
		// The known false positive: holing out from off the green counts as a
		// green hit, because a wedge from the fringe is not a putt and the
		// arithmetic cannot tell the difference.
		{"chip-in for eagle", 2, 0, 4, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := greenInRegulation(tc.strokes, tc.putts, tc.par); got != tc.want {
				t.Errorf("greenInRegulation(%d, %d, %d) = %v, want %v",
					tc.strokes, tc.putts, tc.par, got, tc.want)
			}
		})
	}
}

// Driving accuracy counts par 4s and 5s. Including par 3s would understate it
// against every published figure.
func TestFairwaysExcludeParThrees(t *testing.T) {
	hit, miss := AccuracyHit, AccuracyLeft
	par3, par4, par5 := 3, 4, 5

	s := Summarize([]Hole{
		{HoleNumber: 1, Par: &par4, TeeAccuracy: &hit, Strokes: intp(4), Putts: intp(2)},
		{HoleNumber: 2, Par: &par3, TeeAccuracy: &hit, Strokes: intp(3), Putts: intp(2)},
		{HoleNumber: 3, Par: &par5, TeeAccuracy: &miss, Strokes: intp(6), Putts: intp(2)},
		{HoleNumber: 4, Par: &par3, TeeAccuracy: &miss, Strokes: intp(4), Putts: intp(2)},
	})

	if s.Fairways.Attempted != 2 {
		t.Errorf("attempted = %d, want 2 - only the par 4 and the par 5", s.Fairways.Attempted)
	}
	if s.Fairways.Made != 1 {
		t.Errorf("made = %d, want 1", s.Fairways.Made)
	}
}

func TestSummarizeCountsAFullRound(t *testing.T) {
	holes := []Hole{
		hole(1, 4, 4, 2), // GIR
		hole(2, 3, 4, 2), // missed, no scramble
		hole(3, 5, 5, 2), // GIR
		hole(4, 4, 5, 1), // missed green, scrambled? 5 > 4, no
		hole(5, 4, 4, 1), // missed green (4-1=3 > 2), made par: a scramble
	}
	s := Summarize(holes)

	if s.HolesCompleted != 5 {
		t.Errorf("completed = %d, want 5", s.HolesCompleted)
	}
	if s.Strokes != 22 {
		t.Errorf("strokes = %d, want 22", s.Strokes)
	}
	if s.Par != 20 {
		t.Errorf("par = %d, want 20", s.Par)
	}
	if s.ToPar != 2 {
		t.Errorf("to par = %d, want +2", s.ToPar)
	}
	if s.Putts != 8 {
		t.Errorf("putts = %d, want 8", s.Putts)
	}
	if s.GreensInReg.Made != 2 || s.GreensInReg.Attempted != 5 {
		t.Errorf("GIR = %d/%d, want 2/5", s.GreensInReg.Made, s.GreensInReg.Attempted)
	}
	// Three greens missed; one of them still made par.
	if s.Scrambles.Attempted != 3 || s.Scrambles.Made != 1 {
		t.Errorf("scrambles = %d/%d, want 1/3", s.Scrambles.Made, s.Scrambles.Attempted)
	}
	// Putts on greens hit: holes 1 and 3, two each.
	if s.PuttsOnGreensHit != 4 {
		t.Errorf("putts on greens hit = %d, want 4", s.PuttsOnGreensHit)
	}
}

// A round in progress has eighteen pre-created holes, most of them empty. An
// empty hole must not register as one that was played, or every round would
// start at eighteen holes and a hundred under par.
func TestSummarizeIgnoresUntouchedHoles(t *testing.T) {
	par := 4
	holes := []Hole{
		hole(1, 4, 5, 2),
		{HoleNumber: 2, Par: &par},
		{HoleNumber: 3, Par: &par},
	}
	s := Summarize(holes)

	if s.HolesRecorded != 1 {
		t.Errorf("recorded = %d, want 1", s.HolesRecorded)
	}
	if s.Par != 4 {
		t.Errorf("par = %d, want 4 - only the hole that was played", s.Par)
	}
	if s.ToPar != 1 {
		t.Errorf("to par = %d, want +1", s.ToPar)
	}
}

// A picked-up hole has data but no score: it counts for putting and accuracy,
// and not for the scoring total.
func TestSummarizeHandlesAPickedUpHole(t *testing.T) {
	par := 4
	hit := AccuracyHit
	holes := []Hole{
		hole(1, 4, 5, 2),
		{HoleNumber: 2, Par: &par, TeeAccuracy: &hit, Penalties: 2},
	}
	s := Summarize(holes)

	if s.HolesRecorded != 2 {
		t.Errorf("recorded = %d, want 2", s.HolesRecorded)
	}
	if s.HolesCompleted != 1 {
		t.Errorf("completed = %d, want 1", s.HolesCompleted)
	}
	if s.Strokes != 5 {
		t.Errorf("strokes = %d, want 5 - the picked-up hole has no score", s.Strokes)
	}
	// Only hole 2 recorded a tee shot, and it counts even though the hole was
	// never finished. A fairway you did not record is not a fairway you missed.
	if s.Fairways.Attempted != 1 || s.Fairways.Made != 1 {
		t.Errorf("fairways = %d/%d, want 1/1 - the picked-up hole still counts",
			s.Fairways.Made, s.Fairways.Attempted)
	}
	if s.Penalties != 2 {
		t.Errorf("penalties = %d, want 2", s.Penalties)
	}
}

func TestSummarizeSandSaves(t *testing.T) {
	par := 4
	holes := []Hole{
		// In a greenside bunker and still made par: a sand save.
		{HoleNumber: 1, Par: &par, Strokes: intp(4), Putts: intp(1), GreensideBunker: true},
		// In one and did not.
		{HoleNumber: 2, Par: &par, Strokes: intp(6), Putts: intp(2), GreensideBunker: true},
		// A fairway bunker is not a sand save opportunity.
		{HoleNumber: 3, Par: &par, Strokes: intp(4), Putts: intp(2), FairwayBunker: true},
	}
	s := Summarize(holes)

	if s.SandSaves.Attempted != 2 || s.SandSaves.Made != 1 {
		t.Errorf("sand saves = %d/%d, want 1/2", s.SandSaves.Made, s.SandSaves.Attempted)
	}
	if s.FairwayBunkers != 1 {
		t.Errorf("fairway bunkers = %d, want 1", s.FairwayBunkers)
	}
	if s.GreensideBunker != 2 {
		t.Errorf("greenside bunkers = %d, want 2", s.GreensideBunker)
	}
}

// A hole with no par drops out of the statistics that need one, rather than
// blocking the round or being counted as par 0.
func TestSummarizeToleratesAMissingPar(t *testing.T) {
	s := Summarize([]Hole{
		hole(1, 4, 4, 2),
		{HoleNumber: 2, Strokes: intp(5), Putts: intp(2)},
	})

	if s.HolesCompleted != 2 {
		t.Errorf("completed = %d, want 2", s.HolesCompleted)
	}
	if s.Strokes != 9 {
		t.Errorf("strokes = %d, want 9", s.Strokes)
	}
	if s.Par != 4 {
		t.Errorf("par = %d, want 4 - the hole with no par contributes none", s.Par)
	}
	if s.GreensInReg.Attempted != 1 {
		t.Errorf("GIR attempted = %d, want 1", s.GreensInReg.Attempted)
	}
}

// The out and in totals are what a scorecard prints, and a back-nine round
// keeps its real hole numbers.
func TestSummarizeSplitsTheNines(t *testing.T) {
	s := Summarize([]Hole{
		hole(9, 4, 5, 2),
		hole(10, 4, 4, 2),
		hole(18, 5, 6, 2),
	})
	if s.OutStrokes != 5 {
		t.Errorf("out = %d, want 5", s.OutStrokes)
	}
	if s.InStrokes != 10 {
		t.Errorf("in = %d, want 10", s.InStrokes)
	}
}

func intp(v int) *int { return &v }

// The score breakdown is the one thing in this app that can honestly be
// stacked, so it has to actually partition the holes it counts.
func TestScoreCounts(t *testing.T) {
	s := Summarize([]Hole{
		hole(1, 4, 2, 1), // eagle
		hole(2, 5, 2, 1), // albatross, still the eagle-or-better bucket
		hole(3, 4, 3, 1), // birdie
		hole(4, 4, 4, 2), // par
		hole(5, 3, 4, 2), // bogey
		hole(6, 4, 6, 2), // double
		hole(7, 4, 9, 3), // worse than double, same bucket
	})

	want := ScoreCounts{EagleOrBetter: 2, Birdies: 1, Pars: 1, Bogeys: 1, DoubleOrWorse: 2}
	if s.Scores != want {
		t.Errorf("scores = %+v, want %+v", s.Scores, want)
	}
	// The whole point of stacking: the parts add up to the holes counted.
	if s.Scores.Total() != 7 {
		t.Errorf("total = %d, want 7", s.Scores.Total())
	}
	if s.Scores.Total() != s.HolesCompleted {
		t.Errorf("total = %d but %d holes completed; the stack would not reach the bar top",
			s.Scores.Total(), s.HolesCompleted)
	}
}

// A hole with no par cannot be classified, so it stays out of the breakdown
// rather than being counted as a par.
func TestScoreCountsSkipHolesWithNoPar(t *testing.T) {
	s := Summarize([]Hole{
		hole(1, 4, 4, 2),
		{HoleNumber: 2, Strokes: intp(5), Putts: intp(2)},
	})
	if s.Scores.Total() != 1 {
		t.Errorf("total = %d, want 1 - the hole with no par cannot be classified", s.Scores.Total())
	}
	if s.HolesCompleted != 2 {
		t.Errorf("completed = %d, want 2", s.HolesCompleted)
	}
}
