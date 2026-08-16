package account

import (
	"context"
	"strings"
	"testing"
)

// A backup is a file, and a file can be edited. It must not be able to put
// values in the database that the API itself would refuse.
func TestImportRejectsClubsTheAPIWouldNotAccept(t *testing.T) {
	svc, db := newTestService(t)
	userID := createUser(t, db, "player@example.com")

	tooLong := strings.Repeat("x", 4000)
	badLoft := 900.0
	huge := 100000

	cases := []struct {
		name string
		club ClubExport
	}{
		{"unknown club type", ClubExport{ClubType: "trebuchet", Label: "Siege Engine"}},
		{"loft off the scale", ClubExport{ClubType: "wedge", Label: "Moon Wedge", Loft: &badLoft}},
		{"unknown flex", ClubExport{ClubType: "iron", Label: "7 Iron", Flex: strPtr("spaghetti")}},
		{"notes past the cap", ClubExport{ClubType: "iron", Label: "8 Iron", Notes: &tooLong}},
		{"label past the cap", ClubExport{ClubType: "iron", Label: tooLong}},
		{"absurd carry", ClubExport{ClubType: "driver", Label: "Big Stick", ExpectedCarry: &huge}},
		{"no label", ClubExport{ClubType: "iron", Label: "   "}},
		{"no type", ClubExport{ClubType: "", Label: "Mystery"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			summary := &ImportSummary{}
			if err := svc.importClubs(context.Background(), userID, []ClubExport{tc.club}, summary); err != nil {
				t.Fatalf("import: %v", err)
			}
			if summary.Clubs.Imported != 0 {
				t.Errorf("imported %d, want 0 — the API would have refused this club", summary.Clubs.Imported)
			}
			if summary.Clubs.Failed != 1 {
				t.Errorf("failed = %d, want 1", summary.Clubs.Failed)
			}
		})
	}
}

// The valid case still works, and normalisation is applied on the way in.
func TestImportAcceptsAValidClub(t *testing.T) {
	svc, db := newTestService(t)
	userID := createUser(t, db, "player@example.com")

	summary := &ImportSummary{}
	err := svc.importClubs(context.Background(), userID, []ClubExport{{
		ClubType: "  IRON  ",
		Label:    " 7 Iron ",
		Flex:     strPtr("  Regular  "),
	}}, summary)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if summary.Clubs.Imported != 1 {
		t.Fatalf("imported = %d, want 1 (failed %d)", summary.Clubs.Imported, summary.Clubs.Failed)
	}

	rows, err := db.Queries.ListClubsByUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("list clubs: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("stored %d clubs, want 1", len(rows))
	}
	if rows[0].ClubType != "iron" {
		t.Errorf("club_type = %q, want it lowercased to %q", rows[0].ClubType, "iron")
	}
	if rows[0].Label != "7 Iron" {
		t.Errorf("label = %q, want it trimmed", rows[0].Label)
	}
	if rows[0].Flex == nil || *rows[0].Flex != "regular" {
		t.Errorf("flex = %v, want normalised to %q", rows[0].Flex, "regular")
	}
}

func strPtr(s string) *string { return &s }
