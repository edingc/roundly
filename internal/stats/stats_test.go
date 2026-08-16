package stats

import (
	"math"
	"testing"
)

func closeTo(t *testing.T, label string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s = nil, want %.1f", label, want)
	}
	if math.Abs(*got-want) > 0.05 {
		t.Errorf("%s = %.2f, want %.1f", label, *got, want)
	}
}

// The World Handicap System's table for a short scoring record. "Best 8 of 20"
// is only true once there are twenty, and getting the short cases wrong would
// hand a new player a number that is wildly out.
func TestHandicapUsingFollowsTheWHSTable(t *testing.T) {
	cases := []struct {
		rounds     int
		using      int
		adjustment float64
		ok         bool
	}{
		{0, 0, 0, false},
		{2, 0, 0, false},
		{3, 1, -2.0, true},
		{4, 1, -1.0, true},
		{5, 1, 0, true},
		{6, 2, -1.0, true},
		{8, 2, 0, true},
		{9, 3, 0, true},
		{11, 3, 0, true},
		{12, 4, 0, true},
		{15, 5, 0, true},
		{17, 6, 0, true},
		{19, 7, 0, true},
		{20, 8, 0, true},
	}
	for _, tc := range cases {
		using, adjustment, ok := handicapUsing(tc.rounds)
		if ok != tc.ok || using != tc.using || adjustment != tc.adjustment {
			t.Errorf("handicapUsing(%d) = %d, %.1f, %v; want %d, %.1f, %v",
				tc.rounds, using, adjustment, ok, tc.using, tc.adjustment, tc.ok)
		}
	}
}

// The index averages the best; the anti-cap averages the worst. The gap between
// them is the whole point of showing both.
func TestHandicapAndAntiCapReadFromOppositeEnds(t *testing.T) {
	// Twenty differentials, 10.0 through 29.0.
	differentials := make([]float64, 0, 20)
	for i := range 20 {
		differentials = append(differentials, float64(10+i))
	}

	h := handicapFrom(differentials)
	if h == nil {
		t.Fatal("handicap = nil")
	}
	if h.DifferentialsAvailable != 20 {
		t.Errorf("available = %d, want 20", h.DifferentialsAvailable)
	}
	// Best 8 are 10..17, mean 13.5.
	if h.IndexUsing != 8 {
		t.Errorf("index using = %d, want 8", h.IndexUsing)
	}
	closeTo(t, "index", h.Index, 13.5)
	// Worst 12 are 18..29, mean 23.5.
	if h.AntiCapUsing != 12 {
		t.Errorf("anti-cap using = %d, want 12", h.AntiCapUsing)
	}
	closeTo(t, "anti-cap", h.AntiCap, 23.5)

	if !h.Unofficial {
		t.Error("unofficial = false; the figure is computed from gross scores and must say so")
	}
}

// Only the most recent twenty count, so an improving player is not held back by
// a bad first season.
func TestHandicapConsidersOnlyTheLastTwenty(t *testing.T) {
	// Newest first: twenty good rounds, then ten terrible ones.
	differentials := make([]float64, 0, 30)
	for range 20 {
		differentials = append(differentials, 5.0)
	}
	for range 10 {
		differentials = append(differentials, 40.0)
	}

	h := handicapFrom(differentials)
	closeTo(t, "index", h.Index, 5.0)
	closeTo(t, "anti-cap", h.AntiCap, 5.0)
	if h.DifferentialsAvailable != 20 {
		t.Errorf("available = %d, want 20 - the older rounds are out of scope", h.DifferentialsAvailable)
	}
}

// A short record still produces an index, by the table, and an anti-cap once
// there is enough to call a pattern.
func TestHandicapWithAShortRecord(t *testing.T) {
	h := handicapFrom([]float64{12.0, 18.0, 21.0})
	if h.IndexUsing != 1 {
		t.Errorf("using = %d, want 1", h.IndexUsing)
	}
	// Best of three, minus two.
	closeTo(t, "index", h.Index, 10.0)
	// All three are the "worst three".
	if h.AntiCapUsing != 3 {
		t.Errorf("anti-cap using = %d, want 3", h.AntiCapUsing)
	}
	closeTo(t, "anti-cap", h.AntiCap, 17.0)

	// Two rounds is not a handicap and not a pattern.
	short := handicapFrom([]float64{12.0, 18.0})
	if short.Index != nil {
		t.Errorf("index = %v with two rounds, want nil", short.Index)
	}
	if short.AntiCap != nil {
		t.Errorf("anti-cap = %v with two rounds, want nil", short.AntiCap)
	}

	if handicapFrom(nil) != nil {
		t.Error("handicap should be nil with no differentials at all")
	}
}
