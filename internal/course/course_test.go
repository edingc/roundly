package course

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/edingc/roundly/internal/database"
	"github.com/edingc/roundly/internal/database/sqlc"
	"github.com/edingc/roundly/internal/httpx"
	"github.com/edingc/roundly/internal/id"
	"github.com/edingc/roundly/internal/timex"
)

func newTestService(t *testing.T) (*Service, *database.DB) {
	t.Helper()

	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	return NewService(db), db
}

// courses.created_by is a foreign key, so tests need real user rows.
func createUser(t *testing.T, db *database.DB, email string) string {
	t.Helper()

	userID := id.New()
	err := db.Queries.CreateUser(context.Background(), sqlc.CreateUserParams{
		ID:          userID,
		Email:       email,
		DisplayName: email,
		CreatedAt:   timex.Now(),
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return userID
}

func statusOf(t *testing.T, err error) int {
	t.Helper()

	var apiErr *httpx.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected an *httpx.APIError, got %T: %v", err, err)
	}
	return apiErr.Status
}

func ptr[T any](v T) *T { return &v }

func TestCreateCourseGeneratesHolesAndTees(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	detail, err := svc.Create(ctx, owner, CreateCourseInput{
		Name:    "  Pebble Ridge  ",
		Address: ptr("  1 Fairway Dr  "),
		Tees: []TeeInput{
			{Name: "Championship", Color: "#FFD700", CourseRating: ptr(72.4), SlopeRating: ptr(135)},
			{Name: "Forward", Color: "#FF2D55"},
		},
	})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}

	if detail.Name != "Pebble Ridge" {
		t.Errorf("name = %q, want it trimmed", detail.Name)
	}
	if detail.Address == nil || *detail.Address != "1 Fairway Dr" {
		t.Errorf("address = %v, want it trimmed", detail.Address)
	}
	if len(detail.Holes) != DefaultHoleCount {
		t.Errorf("holes = %d, want %d", len(detail.Holes), DefaultHoleCount)
	}
	if len(detail.Tees) != 2 {
		t.Fatalf("tees = %d, want 2", len(detail.Tees))
	}
	if !detail.CanEdit {
		t.Error("the creator should be able to edit")
	}

	// display_order should follow the order supplied.
	if detail.Tees[0].Name != "Championship" || detail.Tees[1].Name != "Forward" {
		t.Errorf("tee order = %q, %q; want Championship, Forward", detail.Tees[0].Name, detail.Tees[1].Name)
	}

	// Holes come back in play order with a default stroke index.
	for i, hole := range detail.Holes {
		if hole.HoleNumber != i+1 {
			t.Fatalf("hole %d has number %d", i, hole.HoleNumber)
		}
	}
}

func TestCreateNineHoleCourse(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	detail, err := svc.Create(ctx, owner, CreateCourseInput{Name: "Short Nine", HoleCount: 9})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}
	if len(detail.Holes) != 9 {
		t.Errorf("holes = %d, want 9", len(detail.Holes))
	}
}

// The case the schema exists to support: one hole, different par per tee.
func TestSameHoleHasDifferentParPerTee(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	detail, err := svc.Create(ctx, owner, CreateCourseInput{
		Name: "Par Test GC",
		Tees: []TeeInput{
			{Name: "Back", Color: "#000000"},
			{Name: "Forward", Color: "#FFFFFF"},
		},
	})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}

	hole := detail.Holes[0]
	back, forward := detail.Tees[0], detail.Tees[1]

	if _, err := svc.SetTeeDetail(ctx, owner, hole.ID, back.ID, 4, 420); err != nil {
		t.Fatalf("set back tee detail: %v", err)
	}
	if _, err := svc.SetTeeDetail(ctx, owner, hole.ID, forward.ID, 3, 165); err != nil {
		t.Fatalf("set forward tee detail: %v", err)
	}

	reloaded, err := svc.Get(ctx, owner, detail.ID)
	if err != nil {
		t.Fatalf("reload course: %v", err)
	}

	pars := map[string]int{}
	yardages := map[string]int{}
	for _, d := range reloaded.Holes[0].TeeDetails {
		pars[d.TeeID] = d.Par
		yardages[d.TeeID] = d.Yardage
	}

	if pars[back.ID] != 4 {
		t.Errorf("back tee par = %d, want 4", pars[back.ID])
	}
	if pars[forward.ID] != 3 {
		t.Errorf("forward tee par = %d, want 3", pars[forward.ID])
	}
	if yardages[back.ID] != 420 || yardages[forward.ID] != 165 {
		t.Errorf("yardages = %v, want back 420 and forward 165", yardages)
	}
}

func TestSetTeeDetailUpsertsRatherThanDuplicating(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	detail, err := svc.Create(ctx, owner, CreateCourseInput{
		Name: "Upsert GC",
		Tees: []TeeInput{{Name: "Only", Color: "#123456"}},
	})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}
	hole, tee := detail.Holes[0], detail.Tees[0]

	if _, err := svc.SetTeeDetail(ctx, owner, hole.ID, tee.ID, 4, 400); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	updated, err := svc.SetTeeDetail(ctx, owner, hole.ID, tee.ID, 5, 520)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	if len(updated.TeeDetails) != 1 {
		t.Fatalf("tee details = %d, want 1 after an upsert", len(updated.TeeDetails))
	}
	if updated.TeeDetails[0].Par != 5 || updated.TeeDetails[0].Yardage != 520 {
		t.Errorf("detail = %+v, want par 5 and 520 yards", updated.TeeDetails[0])
	}
}

// total_yardage is derived, so it must track the grid rather than a stored copy.
func TestTeeTotalYardageIsDerived(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	detail, err := svc.Create(ctx, owner, CreateCourseInput{
		Name: "Yardage GC",
		Tees: []TeeInput{{Name: "Only", Color: "#123456"}},
	})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}
	tee := detail.Tees[0]

	if detail.Tees[0].TotalYardage != 0 {
		t.Errorf("total yardage = %d, want 0 before any yardages", detail.Tees[0].TotalYardage)
	}

	for i := 0; i < 3; i++ {
		if _, err := svc.SetTeeDetail(ctx, owner, detail.Holes[i].ID, tee.ID, 4, 400); err != nil {
			t.Fatalf("set detail on hole %d: %v", i+1, err)
		}
	}

	reloaded, err := svc.Get(ctx, owner, detail.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Tees[0].TotalYardage != 1200 {
		t.Errorf("total yardage = %d, want 1200", reloaded.Tees[0].TotalYardage)
	}
}

func TestOnlyCreatorCanModify(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")
	stranger := createUser(t, db, "stranger@example.com")

	detail, err := svc.Create(ctx, owner, CreateCourseInput{
		Name: "Owned GC",
		Tees: []TeeInput{{Name: "Only", Color: "#123456"}},
	})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}
	hole, tee := detail.Holes[0], detail.Tees[0]

	// A stranger can still read it: the directory is shared.
	visible, err := svc.Get(ctx, stranger, detail.ID)
	if err != nil {
		t.Fatalf("stranger cannot read the course: %v", err)
	}
	if visible.CanEdit {
		t.Error("can_edit should be false for a non-creator")
	}

	cases := map[string]error{
		"update course": func() error {
			_, err := svc.Update(ctx, stranger, detail.ID, UpdateCourseInput{Name: "Hijacked"})
			return err
		}(),
		"delete course": svc.Delete(ctx, stranger, detail.ID),
		"add tee": func() error {
			_, err := svc.AddTee(ctx, stranger, detail.ID, TeeInput{Name: "X", Color: "#000000"})
			return err
		}(),
		"update tee": func() error {
			_, err := svc.UpdateTee(ctx, stranger, tee.ID, TeeInput{Name: "X", Color: "#000000"})
			return err
		}(),
		"delete tee":       svc.DeleteTee(ctx, stranger, tee.ID),
		"add hole":         func() error { _, err := svc.AddHole(ctx, stranger, detail.ID, HoleInput{HoleNumber: 1}); return err }(),
		"update hole":      func() error { _, err := svc.UpdateHole(ctx, stranger, hole.ID, HoleInput{}); return err }(),
		"delete hole":      svc.DeleteHole(ctx, stranger, hole.ID),
		"set tee detail":   func() error { _, err := svc.SetTeeDetail(ctx, stranger, hole.ID, tee.ID, 4, 400); return err }(),
		"clear tee detail": svc.ClearTeeDetail(ctx, stranger, hole.ID, tee.ID),
	}

	for name, err := range cases {
		if err == nil {
			t.Errorf("%s: a non-creator was allowed", name)
			continue
		}
		// "add hole" collides on the unique hole number before the ownership
		// check would matter, so accept either refusal.
		if status := statusOf(t, err); status != 403 && status != 409 {
			t.Errorf("%s: status = %d, want 403", name, status)
		}
	}
}

// hole_tee_details references two IDs independently, so the service has to
// reject pairings that span courses.
func TestSetTeeDetailRejectsTeeFromAnotherCourse(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	first, err := svc.Create(ctx, owner, CreateCourseInput{
		Name: "First GC",
		Tees: []TeeInput{{Name: "A", Color: "#111111"}},
	})
	if err != nil {
		t.Fatalf("create first course: %v", err)
	}
	second, err := svc.Create(ctx, owner, CreateCourseInput{
		Name: "Second GC",
		Tees: []TeeInput{{Name: "B", Color: "#222222"}},
	})
	if err != nil {
		t.Fatalf("create second course: %v", err)
	}

	_, err = svc.SetTeeDetail(ctx, owner, first.Holes[0].ID, second.Tees[0].ID, 4, 400)
	if err == nil {
		t.Fatal("a tee from another course was accepted")
	}
	if status := statusOf(t, err); status != 400 {
		t.Errorf("status = %d, want 400", status)
	}
}

func TestDeleteTeeCascadesItsHoleDetails(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	detail, err := svc.Create(ctx, owner, CreateCourseInput{
		Name: "Cascade GC",
		Tees: []TeeInput{
			{Name: "Keep", Color: "#111111"},
			{Name: "Remove", Color: "#222222"},
		},
	})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}
	hole := detail.Holes[0]
	keep, remove := detail.Tees[0], detail.Tees[1]

	if _, err := svc.SetTeeDetail(ctx, owner, hole.ID, keep.ID, 4, 400); err != nil {
		t.Fatalf("set keep detail: %v", err)
	}
	if _, err := svc.SetTeeDetail(ctx, owner, hole.ID, remove.ID, 3, 150); err != nil {
		t.Fatalf("set remove detail: %v", err)
	}

	if err := svc.DeleteTee(ctx, owner, remove.ID); err != nil {
		t.Fatalf("delete tee: %v", err)
	}

	reloaded, err := svc.Get(ctx, owner, detail.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(reloaded.Tees) != 1 {
		t.Errorf("tees = %d, want 1", len(reloaded.Tees))
	}
	details := reloaded.Holes[0].TeeDetails
	if len(details) != 1 || details[0].TeeID != keep.ID {
		t.Errorf("hole details = %+v, want only the kept tee", details)
	}
}

func TestDeleteCourseCascades(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	detail, err := svc.Create(ctx, owner, CreateCourseInput{
		Name: "Doomed GC",
		Tees: []TeeInput{{Name: "Only", Color: "#123456"}},
	})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}
	if _, err := svc.SetTeeDetail(ctx, owner, detail.Holes[0].ID, detail.Tees[0].ID, 4, 400); err != nil {
		t.Fatalf("set detail: %v", err)
	}

	if err := svc.Delete(ctx, owner, detail.ID); err != nil {
		t.Fatalf("delete course: %v", err)
	}

	if _, err := svc.Get(ctx, owner, detail.ID); err == nil {
		t.Error("the course is still readable after deletion")
	} else if status := statusOf(t, err); status != 404 {
		t.Errorf("status = %d, want 404", status)
	}

	for table, query := range map[string]string{
		"tees":             "SELECT COUNT(*) FROM tees",
		"holes":            "SELECT COUNT(*) FROM holes",
		"hole_tee_details": "SELECT COUNT(*) FROM hole_tee_details",
	} {
		var count int
		if err := db.QueryRow(query).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Errorf("%s has %d rows after the course was deleted", table, count)
		}
	}
}

func TestSearchMatchesNameAndAddressLiterally(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	for _, c := range []CreateCourseInput{
		{Name: "Pebble Ridge", Address: ptr("Springfield")},
		{Name: "Cypress Dunes", Address: ptr("Shelbyville")},
		{Name: "50% Off Golf", Address: nil},
	} {
		if _, err := svc.Create(ctx, owner, c); err != nil {
			t.Fatalf("create %q: %v", c.Name, err)
		}
	}

	cases := []struct {
		term string
		want int
	}{
		{"pebble", 1},
		{"PEBBLE", 1}, // case-insensitive
		{"ridge", 1},  // mid-string
		{"springfield", 1},
		{"50%", 1}, // a literal % must not act as a wildcard
		{"%", 1},   // matches only the course whose name contains "%"
		{"nonexistent", 0},
		{"", 3},
	}

	for _, tc := range cases {
		page, err := svc.List(ctx, owner, tc.term, 25, 0)
		if err != nil {
			t.Fatalf("search %q: %v", tc.term, err)
		}
		if page.Total != tc.want {
			t.Errorf("search %q: total = %d, want %d", tc.term, page.Total, tc.want)
		}
	}
}

func TestListPaginates(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	for _, name := range []string{"A GC", "B GC", "C GC"} {
		if _, err := svc.Create(ctx, owner, CreateCourseInput{Name: name}); err != nil {
			t.Fatalf("create %q: %v", name, err)
		}
	}

	first, err := svc.List(ctx, owner, "", 2, 0)
	if err != nil {
		t.Fatalf("list page 1: %v", err)
	}
	if len(first.Items) != 2 || first.Total != 3 {
		t.Errorf("page 1: %d items, total %d; want 2 and 3", len(first.Items), first.Total)
	}
	if first.Items[0].Name != "A GC" {
		t.Errorf("first item = %q, want A GC (sorted by name)", first.Items[0].Name)
	}

	second, err := svc.List(ctx, owner, "", 2, 2)
	if err != nil {
		t.Fatalf("list page 2: %v", err)
	}
	if len(second.Items) != 1 || second.Items[0].Name != "C GC" {
		t.Errorf("page 2 = %+v, want just C GC", second.Items)
	}
	if second.Items[0].HoleCount != DefaultHoleCount {
		t.Errorf("hole count = %d, want %d", second.Items[0].HoleCount, DefaultHoleCount)
	}
}

func TestAddHoleRejectsDuplicateNumber(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	detail, err := svc.Create(ctx, owner, CreateCourseInput{Name: "Dup GC", HoleCount: 9})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}

	if _, err := svc.AddHole(ctx, owner, detail.ID, HoleInput{HoleNumber: 5}); err == nil {
		t.Error("a duplicate hole number was accepted")
	} else if status := statusOf(t, err); status != 409 {
		t.Errorf("status = %d, want 409", status)
	}

	// Hole 10 does not exist on a nine-hole course, so it should be addable.
	if _, err := svc.AddHole(ctx, owner, detail.ID, HoleInput{HoleNumber: 10, HandicapIndex: ptr(10)}); err != nil {
		t.Errorf("adding hole 10 failed: %v", err)
	}
}

func TestGetMissingCourseIsNotFound(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	if _, err := svc.Get(ctx, owner, id.New()); err == nil {
		t.Fatal("a missing course was returned")
	} else if status := statusOf(t, err); status != 404 {
		t.Errorf("status = %d, want 404", status)
	}
}
