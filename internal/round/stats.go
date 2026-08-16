package round

import "math"

// Derived statistics.
//
// None of this is stored. Every number below is computed from the holes on
// every read, which is what stops a round from disagreeing with itself after an
// edit: correct a putt count and the greens-in-regulation figure moves with it,
// because there was never a second copy to go stale.
//
// Each rate is reported as a made-over-attempted pair rather than a percentage.
// A percentage has to invent an answer when the denominator is zero, and
// "0% of 0 fairways" reads like a bad round rather than a round with no par 4s
// recorded yet. The client formats.

// Tally is a made-out-of-attempted count.
type Tally struct {
	Made      int `json:"made"`
	Attempted int `json:"attempted"`
}

func (t *Tally) add(made bool) {
	t.Attempted++
	if made {
		t.Made++
	}
}

// ScoreCounts is holes grouped by result against par.
//
// Sums to the number of completed holes with a par recorded, which is what
// makes it the one breakdown in this app that can honestly be stacked: the
// parts are a partition of a whole rather than unrelated measures that happen
// to share an axis.
type ScoreCounts struct {
	EagleOrBetter int `json:"eagle_or_better"`
	Birdies       int `json:"birdies"`
	Pars          int `json:"pars"`
	Bogeys        int `json:"bogeys"`
	DoubleOrWorse int `json:"double_or_worse"`
}

// Total is how many holes the breakdown accounts for.
func (c ScoreCounts) Total() int {
	return c.EagleOrBetter + c.Birdies + c.Pars + c.Bogeys + c.DoubleOrWorse
}

// Summary is everything derivable from a round's holes.
type Summary struct {
	// HolesRecorded counts holes with any data at all; HolesCompleted counts
	// those with a score. The two differ for a round in progress, and for a
	// round where a hole was picked up.
	HolesRecorded  int `json:"holes_recorded"`
	HolesCompleted int `json:"holes_completed"`

	Strokes int `json:"strokes"`
	// Par totals only the completed holes, so ToPar compares like with like.
	// Totalling every hole's par would show a player mid-round as catastrophically
	// under it.
	Par   int `json:"par"`
	ToPar int `json:"to_par"`
	Putts int `json:"putts"`

	// Out and In are the front and back nine totals. Both are zero for a round
	// that did not visit that nine.
	OutStrokes int `json:"out_strokes"`
	InStrokes  int `json:"in_strokes"`

	Fairways        Tally `json:"fairways"`
	GreensInReg     Tally `json:"greens_in_regulation"`
	Scrambles       Tally `json:"scrambles"`
	SandSaves       Tally `json:"sand_saves"`
	Penalties       int   `json:"penalties"`
	FairwayBunkers  int   `json:"fairway_bunkers"`
	GreensideBunker int   `json:"greenside_bunkers"`

	// Scores groups the holes by result against par.
	Scores ScoreCounts `json:"scores"`

	// PuttsOnGreensHit is putts taken on holes where the green was hit in
	// regulation, the denominator being GreensInReg.Made. It is the putting
	// number worth watching: total putts flatter a player who misses greens and
	// chips close.
	PuttsOnGreensHit int `json:"putts_on_greens_hit"`
}

// Summarize computes a round's statistics from its holes.
func Summarize(holes []Hole) Summary {
	var s Summary

	for _, h := range holes {
		if isBlank(h) {
			continue
		}
		s.HolesRecorded++
		s.Penalties += h.Penalties
		if h.FairwayBunker {
			s.FairwayBunkers++
		}
		if h.GreensideBunker {
			s.GreensideBunker++
		}

		// Driving accuracy counts par 4s and 5s only. A par 3 has no fairway,
		// and including them would understate accuracy by roughly a fifth
		// against any published figure.
		if h.Par != nil && *h.Par >= 4 && h.TeeAccuracy != nil {
			s.Fairways.add(*h.TeeAccuracy == AccuracyHit)
		}

		if h.Putts != nil {
			s.Putts += *h.Putts
		}
		if h.Strokes == nil {
			continue
		}

		s.HolesCompleted++
		s.Strokes += *h.Strokes
		if h.HoleNumber <= 9 {
			s.OutStrokes += *h.Strokes
		} else {
			s.InStrokes += *h.Strokes
		}
		if h.Par != nil {
			s.Par += *h.Par
			s.Scores.add(*h.Strokes - *h.Par)
		}

		// Everything below needs par, a score, and a putt count together.
		if h.Par == nil || h.Putts == nil {
			continue
		}
		hitGreen := greenInRegulation(*h.Strokes, *h.Putts, *h.Par)
		s.GreensInReg.add(hitGreen)

		if hitGreen {
			s.PuttsOnGreensHit += *h.Putts
		} else {
			// Scrambling asks a question only of the holes where the green was
			// missed: did you still make par?
			s.Scrambles.add(*h.Strokes <= *h.Par)
		}
		if h.GreensideBunker {
			s.SandSaves.add(*h.Strokes <= *h.Par)
		}
	}

	s.ToPar = s.Strokes - s.Par
	return s
}

// add files one hole into the breakdown by how it finished against par.
//
// Everything at double bogey and worse lands in one bucket. Splitting further
// would make a chart with more colours than a reader can hold, and the
// difference between a double and a triple is not a difference in kind - both
// are a hole that got away.
func (c *ScoreCounts) add(toPar int) {
	switch {
	case toPar <= -2:
		c.EagleOrBetter++
	case toPar == -1:
		c.Birdies++
	case toPar == 0:
		c.Pars++
	case toPar == 1:
		c.Bogeys++
	default:
		c.DoubleOrWorse++
	}
}

// DifferentialFor computes the score differential a round contributes to a
// handicap: (113 / slope) * (score - rating).
//
// It lives here rather than in internal/stats because every input is a property
// of the round - the rating and slope snapshotted when it started, and the
// strokes its holes add up to. Aggregation reads it; it does not own it.
//
// Nil unless every intended hole has a score, because a differential from a
// half-finished card is not a smaller differential, it is a meaningless one.
// Nil too when the course had no published rating, which is common enough that
// it cannot be an error.
//
// One deliberate simplification, and it is why callers label the result
// unofficial: the World Handicap System uses an *adjusted* gross score, capping
// each hole at net double bogey. Computing that cap needs a course handicap,
// which needs an index, which is what this is trying to produce - the real
// system resolves the circularity iteratively across a whole scoring record.
// This uses the gross score, so a blow-up hole counts in full and the number
// reads slightly high.
func DifferentialFor(courseRating *float64, slopeRating *int, summary Summary, holesIntended int) *float64 {
	if courseRating == nil || slopeRating == nil || *slopeRating == 0 {
		return nil
	}
	if holesIntended == 0 || summary.HolesCompleted != holesIntended {
		return nil
	}
	diff := (113.0 / float64(*slopeRating)) * (float64(summary.Strokes) - *courseRating)
	// One decimal, which is how handicaps are published.
	return ptrFloat(math.Round(diff*10) / 10)
}

func ptrFloat(v float64) *float64 { return &v }

// greenInRegulation reports whether the green was reached in par - 2 strokes.
//
// Derived from the score and the putt count, which treats "strokes that were
// not putts" as "strokes taken to reach the green". That is exact whenever the
// ball finished on the green, and generous in one case: holing out from off it
// counts as a green hit, because a wedge from the fringe is not a putt and the
// arithmetic cannot tell. Recovering the truth would need a per-hole "did you
// hit the green" tap on all eighteen, for an event that happens a handful of
// times a season. See docs/phase-3-plan.md.
func greenInRegulation(strokes, putts, par int) bool {
	return strokes-putts <= par-2
}

// isBlank reports whether a hole holds nothing worth counting. A round starts
// with all its holes pre-created from the course, so most of them are empty for
// most of a round, and an empty hole must not register as one that was played.
func isBlank(h Hole) bool {
	return h.Strokes == nil &&
		h.Putts == nil &&
		h.TeeClubID == nil &&
		h.TeeAccuracy == nil &&
		h.FirstPuttFeet == nil &&
		!h.FairwayBunker &&
		!h.GreensideBunker &&
		h.Penalties == 0
}
