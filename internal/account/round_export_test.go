package account

import (
	"context"
	"testing"

	"github.com/edingc/roundly/internal/database/sqlc"
	"github.com/edingc/roundly/internal/id"
	"github.com/edingc/roundly/internal/round"
	"github.com/edingc/roundly/internal/timex"
)

// seedRound writes a finished round straight to the database, standing in for
// one that was played.
func seedRound(t *testing.T, svc *Service, userID, courseName, playedOn string) {
	t.Helper()
	ctx := context.Background()
	roundID := id.New()
	now := timex.Now()
	par, strokes, putts := int64(4), int64(5), int64(2)

	if err := svc.db.Queries.CreateRound(ctx, sqlc.CreateRoundParams{
		ID: roundID, UserID: userID, CourseName: courseName, TeeName: "White",
		PlayedOn: playedOn, Status: round.StatusInProgress, EntryMode: round.EntryManual,
		HolesIntended: 18, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create round: %v", err)
	}
	if err := svc.db.Queries.UpsertRoundHole(ctx, sqlc.UpsertRoundHoleParams{
		ID: id.New(), RoundID: roundID, HoleNumber: 1,
		Par: &par, Strokes: &strokes, Putts: &putts,
	}); err != nil {
		t.Fatalf("create hole: %v", err)
	}
	if err := svc.db.Queries.SetRoundStatus(ctx, sqlc.SetRoundStatusParams{
		Status: round.StatusComplete, CompletedAt: &now, UpdatedAt: now,
		ID: roundID, UserID: userID,
	}); err != nil {
		t.Fatalf("complete round: %v", err)
	}
}

// A backup that carried the course directory and the bag but not the rounds
// played with them would make "take your data" quietly untrue.
func TestExportCarriesRounds(t *testing.T) {
	svc, db := newTestService(t)
	userID := createUser(t, db, "player@example.com")
	seedRound(t, svc, userID, "Sand Creek", "2026-09-03")

	exported, err := svc.Export(context.Background(), userID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if exported.FormatVersion != 2 {
		t.Errorf("format version = %d, want 2", exported.FormatVersion)
	}
	if len(exported.Rounds) != 1 {
		t.Fatalf("rounds = %d, want 1", len(exported.Rounds))
	}
	r := exported.Rounds[0]
	if r.CourseName != "Sand Creek" || r.PlayedOn != "2026-09-03" {
		t.Errorf("round = %q on %q, want the seeded one", r.CourseName, r.PlayedOn)
	}
	if len(r.HoleScores) != 1 || r.HoleScores[0].Strokes == nil || *r.HoleScores[0].Strokes != 5 {
		t.Errorf("hole scores = %+v, want the seeded hole", r.HoleScores)
	}
	// The snapshot travels, because it is the record of what the course said
	// that day rather than what it says now.
	if r.HoleScores[0].Par == nil || *r.HoleScores[0].Par != 4 {
		t.Errorf("par = %v, want the snapshot", r.HoleScores[0].Par)
	}
}

func TestImportRestoresRounds(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	source := createUser(t, db, "source@example.com")
	seedRound(t, svc, source, "Sand Creek", "2026-09-03")

	exported, err := svc.Export(ctx, source)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	target := createUser(t, db, "target@example.com")
	summary, err := svc.Import(ctx, target, exported)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if summary.Rounds.Imported != 1 {
		t.Fatalf("imported = %d, want 1 (failed %d)", summary.Rounds.Imported, summary.Rounds.Failed)
	}

	restored, err := svc.db.Queries.ListAllRounds(ctx, target)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(restored) != 1 {
		t.Fatalf("restored = %d rounds, want 1", len(restored))
	}
	if restored[0].CourseName != "Sand Creek" {
		t.Errorf("course = %q, want Sand Creek", restored[0].CourseName)
	}
	if restored[0].Status != round.StatusComplete || restored[0].CompletedAt == nil {
		t.Errorf("status = %q with completed_at %v, want a finished round",
			restored[0].Status, restored[0].CompletedAt)
	}

	// Importing the same file twice is a no-op, the same promise the rest of
	// the importer makes.
	second, err := svc.Import(ctx, target, exported)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if second.Rounds.Imported != 0 || second.Rounds.Skipped != 1 {
		t.Errorf("second import = %d imported / %d skipped, want 0/1",
			second.Rounds.Imported, second.Rounds.Skipped)
	}
}
