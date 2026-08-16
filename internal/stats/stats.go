// Package stats aggregates rounds into the numbers the overview screen shows.
//
// Nothing here is stored. Every figure is computed from the rounds on each
// request, for the same reason a round's own summary is: there is then no
// second copy to go stale when a scorecard is corrected. The work is two
// queries and a pass over a few hundred rows, which is nothing beside the cost
// of keeping a cache honest.
//
// Per-round statistics live in internal/round and are reused here rather than
// reimplemented. This package only knows how to combine them and how to turn a
// score into a handicap differential.
package stats

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/edingc/roundly/internal/database"
	"github.com/edingc/roundly/internal/database/sqlc"
	"github.com/edingc/roundly/internal/httpx"
	"github.com/edingc/roundly/internal/round"
)

// Windows the overview offers. Anything else is rejected rather than clamped,
// so a typo in a query string does not silently answer a different question.
var Windows = []int{5, 10, 20, 50, 0} // 0 means every round

// Service computes the overview.
type Service struct {
	db *database.DB
}

func NewService(db *database.DB) *Service { return &Service{db: db} }

// Tally mirrors round.Tally: made out of attempted, with the percentage left
// to the client because a percentage has to invent an answer when nothing was
// attempted.
type Tally struct {
	Made      int `json:"made"`
	Attempted int `json:"attempted"`
}

func (t *Tally) add(other round.Tally) {
	t.Made += other.Made
	t.Attempted += other.Attempted
}

// Point is one round, for the charts. Oldest first, so a line reads left to
// right the way time does.
type Point struct {
	RoundID    string `json:"round_id"`
	PlayedOn   string `json:"played_on"`
	CourseName string `json:"course_name"`
	Holes      int    `json:"holes"`

	Strokes int `json:"strokes"`
	ToPar   int `json:"to_par"`
	Putts   int `json:"putts"`

	Fairways    Tally `json:"fairways"`
	GreensInReg Tally `json:"greens_in_regulation"`
	// Scores partitions the holes by result against par, which is what makes it
	// the one breakdown here that can be stacked honestly.
	Scores round.ScoreCounts `json:"scores"`

	// Differential is nil when the round has no rating and slope to compute it
	// from. A course with no published rating is common enough that this cannot
	// be an error.
	Differential *float64 `json:"differential"`
}

// Overview is the whole screen's worth of numbers.
type Overview struct {
	// Window is the number of rounds asked for; 0 means all of them.
	Window int `json:"window"`
	// RoundsCounted is how many were actually available, which is what every
	// average below divides by.
	RoundsCounted int `json:"rounds_counted"`

	AverageScore     *float64 `json:"average_score"`
	AverageToPar     *float64 `json:"average_to_par"`
	BestToPar        *int     `json:"best_to_par"`
	AveragePutts     *float64 `json:"average_putts"`
	AveragePenalties *float64 `json:"average_penalties"`

	Fairways    Tally `json:"fairways"`
	GreensInReg Tally `json:"greens_in_regulation"`
	Scrambles   Tally `json:"scrambles"`
	SandSaves   Tally `json:"sand_saves"`

	Handicap *Handicap `json:"handicap"`

	Series []Point `json:"series"`
}

// Handicap is the pair of numbers computed from score differentials.
type Handicap struct {
	// Index is the World Handicap System calculation: the average of the best
	// differentials of the most recent twenty rounds, by the table in
	// handicapUsing. Nil until there are three rounds with a rating to compute
	// from.
	Index *float64 `json:"index"`
	// AntiCap is the mirror: the average of the *worst* twelve differentials.
	// Where the index says what a good day looks like, this says what a bad one
	// does, and the gap between them is the honest measure of consistency.
	AntiCap *float64 `json:"anti_cap"`

	// DifferentialsAvailable is how many rounds carried a rating and slope.
	DifferentialsAvailable int `json:"differentials_available"`
	// IndexUsing and AntiCapUsing are how many differentials each figure
	// averaged, so the screen can say "best 3 of 9" rather than implying twenty.
	IndexUsing   int `json:"index_using"`
	AntiCapUsing int `json:"anti_cap_using"`

	// Unofficial is always true, and says so in the payload rather than only in
	// the UI. See round.DifferentialFor.
	Unofficial bool `json:"unofficial"`
}

// Overview computes the screen for one player.
func (s *Service) Overview(ctx context.Context, userID string, window int) (*Overview, error) {
	roundRows, err := s.db.Queries.ListAllRounds(ctx, userID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list rounds: %w", err))
	}
	holeRows, err := s.db.Queries.ListAllRoundHolesByUser(ctx, userID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list round holes: %w", err))
	}

	holesByRound := make(map[string][]round.Hole, len(roundRows))
	for _, h := range holeRows {
		holesByRound[h.RoundID] = append(holesByRound[h.RoundID], round.HoleFromRow(h))
	}

	// Only finished rounds count. An in-progress round would drag every average
	// down by however many holes have not been played yet, and an abandoned one
	// is a round the player decided not to have.
	counted := make([]sqlc.Round, 0, len(roundRows))
	for _, r := range roundRows {
		if r.Status == round.StatusComplete {
			counted = append(counted, r)
		}
	}

	// ListAllRounds is newest first. The window takes from the front, and the
	// series is then reversed so a chart reads left to right.
	if window > 0 && len(counted) > window {
		counted = counted[:window]
	}

	out := &Overview{Window: window, Series: []Point{}}
	var totalStrokes, totalToPar, totalPutts, totalPenalties, scoringRounds int
	var differentials []float64

	for _, r := range counted {
		summary := round.Summarize(holesByRound[r.ID])
		if summary.HolesCompleted == 0 {
			continue
		}
		out.RoundsCounted++

		out.Fairways.add(summary.Fairways)
		out.GreensInReg.add(summary.GreensInReg)
		out.Scrambles.add(summary.Scrambles)
		out.SandSaves.add(summary.SandSaves)
		totalPutts += summary.Putts
		totalPenalties += summary.Penalties

		// Scoring averages count only rounds where every intended hole has a
		// score. A nine-hole card and an eighteen-hole card cannot be averaged
		// together, and a partial round is not a score at all.
		full := summary.HolesCompleted == int(r.HolesIntended)
		if full {
			scoringRounds++
			totalStrokes += summary.Strokes
			totalToPar += summary.ToPar
			if out.BestToPar == nil || summary.ToPar < *out.BestToPar {
				toPar := summary.ToPar
				out.BestToPar = &toPar
			}
		}

		// The round owns this calculation; aggregation only reads it.
		diff := round.DifferentialFor(r.CourseRating, int64PtrToIntPtr(r.SlopeRating), summary, int(r.HolesIntended))
		if diff != nil {
			differentials = append(differentials, *diff)
		}

		out.Series = append(out.Series, Point{
			RoundID:      r.ID,
			PlayedOn:     r.PlayedOn,
			CourseName:   r.CourseName,
			Holes:        int(r.HolesIntended),
			Strokes:      summary.Strokes,
			ToPar:        summary.ToPar,
			Putts:        summary.Putts,
			Fairways:     Tally{summary.Fairways.Made, summary.Fairways.Attempted},
			GreensInReg:  Tally{summary.GreensInReg.Made, summary.GreensInReg.Attempted},
			Scores:       summary.Scores,
			Differential: diff,
		})
	}

	// Oldest first for the charts.
	for i, j := 0, len(out.Series)-1; i < j; i, j = i+1, j-1 {
		out.Series[i], out.Series[j] = out.Series[j], out.Series[i]
	}

	if scoringRounds > 0 {
		out.AverageScore = ptr(float64(totalStrokes) / float64(scoringRounds))
		out.AverageToPar = ptr(float64(totalToPar) / float64(scoringRounds))
	}
	if out.RoundsCounted > 0 {
		out.AveragePutts = ptr(float64(totalPutts) / float64(out.RoundsCounted))
		out.AveragePenalties = ptr(float64(totalPenalties) / float64(out.RoundsCounted))
	}
	out.Handicap = handicapFrom(differentials)
	return out, nil
}

// handicapFrom turns a set of differentials into an index and its mirror.
//
// Newest first on the way in; only the most recent twenty are considered, which
// is what the World Handicap System looks at.
func handicapFrom(differentials []float64) *Handicap {
	if len(differentials) == 0 {
		return nil
	}
	if len(differentials) > 20 {
		differentials = differentials[:20]
	}

	h := &Handicap{DifferentialsAvailable: len(differentials), Unofficial: true}

	sorted := append([]float64(nil), differentials...)
	sort.Float64s(sorted)

	if using, adjustment, ok := handicapUsing(len(sorted)); ok {
		h.IndexUsing = using
		h.Index = ptr(round1(mean(sorted[:using]) + adjustment))
	}

	// The anti-cap: the average of the worst twelve, or of as many as there
	// are. Fewer than three is not a pattern, it is a bad afternoon.
	if len(sorted) >= 3 {
		using := min(12, len(sorted))
		h.AntiCapUsing = using
		h.AntiCap = ptr(round1(mean(sorted[len(sorted)-using:])))
	}
	return h
}

// handicapUsing is the World Handicap System's table for a short scoring
// record: how many of the best differentials to average, and what to add.
//
// Reproduced rather than approximated, because "the best 8 of 20" is only true
// once there are twenty. A player with nine rounds averages their best three,
// and one with three averages their single best minus two strokes.
func handicapUsing(n int) (using int, adjustment float64, ok bool) {
	switch {
	case n < 3:
		return 0, 0, false
	case n == 3:
		return 1, -2.0, true
	case n == 4:
		return 1, -1.0, true
	case n == 5:
		return 1, 0, true
	case n == 6:
		return 2, -1.0, true
	case n <= 8:
		return 2, 0, true
	case n <= 11:
		return 3, 0, true
	case n <= 14:
		return 4, 0, true
	case n <= 16:
		return 5, 0, true
	case n <= 18:
		return 6, 0, true
	case n == 19:
		return 7, 0, true
	default:
		return 8, 0, true
	}
}

func int64PtrToIntPtr(v *int64) *int {
	if v == nil {
		return nil
	}
	converted := int(*v)
	return &converted
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var total float64
	for _, v := range values {
		total += v
	}
	return total / float64(len(values))
}

// round1 keeps a differential to one decimal, which is how handicaps are
// published and precise enough for anything here.
func round1(v float64) float64 { return math.Round(v*10) / 10 }

func ptr[T any](v T) *T { return &v }
