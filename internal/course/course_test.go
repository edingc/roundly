package course

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/edingc/roundly/internal/database"
	"github.com/edingc/roundly/internal/database/sqlc"
	"github.com/edingc/roundly/internal/geocode"
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

	return NewService(db, nil), db
}

// courses.uploaded_by is a foreign key, so tests need real user rows.
func createUser(t *testing.T, db *database.DB, email string) string {
	t.Helper()

	userID := id.New()
	now := timex.Now()
	err := db.Queries.CreateUser(context.Background(), sqlc.CreateUserParams{
		ID:          userID,
		Email:       email,
		DisplayName: email,
		CreatedAt:   now,
		UpdatedAt:   now,
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
		Name:     "  Pebble Ridge  ",
		Location: Location{Street: ptr("  1 Fairway Dr  "), City: ptr("  Marne  "), Region: ptr(" MI "), PostalCode: ptr(" 49435 "), Country: ptr(" USA ")},
		Phone:    ptr("  (555) 123-4567  "),
		Tees: []TeeInput{
			{
				Name: "Championship", Color: "#FFD700",
				CourseRatingMen: ptr(72.4), SlopeRatingMen: ptr(135),
				CourseRatingWomen: ptr(74.8), SlopeRatingWomen: ptr(140),
				Front9CourseRatingMen: ptr(35.8), Front9SlopeRatingMen: ptr(133),
				Back9CourseRatingMen: ptr(36.6), Back9SlopeRatingMen: ptr(137),
				Front9CourseRatingWomen: ptr(37.0), Front9SlopeRatingWomen: ptr(138),
				Back9CourseRatingWomen: ptr(37.8), Back9SlopeRatingWomen: ptr(142),
			},
			{Name: "Forward", Color: "#FF2D55"},
		},
	})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}

	if detail.Name != "Pebble Ridge" {
		t.Errorf("name = %q, want it trimmed", detail.Name)
	}
	for _, part := range []struct {
		field string
		got   *string
		want  string
	}{
		{"street", detail.Street, "1 Fairway Dr"},
		{"city", detail.City, "Marne"},
		{"region", detail.Region, "MI"},
		{"postal_code", detail.PostalCode, "49435"},
		{"country", detail.Country, "USA"},
	} {
		if part.got == nil || *part.got != part.want {
			t.Errorf("%s = %v, want it trimmed to %q", part.field, part.got, part.want)
		}
	}
	if detail.Phone == nil || *detail.Phone != "(555) 123-4567" {
		t.Errorf("phone = %v, want it trimmed", detail.Phone)
	}
	if len(detail.Holes) != DefaultHoleCount {
		t.Errorf("holes = %d, want %d", len(detail.Holes), DefaultHoleCount)
	}
	if len(detail.Tees) != 2 {
		t.Fatalf("tees = %d, want 2", len(detail.Tees))
	}
	// display_order should follow the order supplied.
	if detail.Tees[0].Name != "Championship" || detail.Tees[1].Name != "Forward" {
		t.Errorf("tee order = %q, %q; want Championship, Forward", detail.Tees[0].Name, detail.Tees[1].Name)
	}

	// Course and slope rating are tracked separately per gender.
	championship := detail.Tees[0]
	if championship.CourseRatingMen == nil || *championship.CourseRatingMen != 72.4 {
		t.Errorf("course_rating_men = %v, want 72.4", championship.CourseRatingMen)
	}
	if championship.SlopeRatingMen == nil || *championship.SlopeRatingMen != 135 {
		t.Errorf("slope_rating_men = %v, want 135", championship.SlopeRatingMen)
	}
	if championship.CourseRatingWomen == nil || *championship.CourseRatingWomen != 74.8 {
		t.Errorf("course_rating_women = %v, want 74.8", championship.CourseRatingWomen)
	}
	if championship.SlopeRatingWomen == nil || *championship.SlopeRatingWomen != 140 {
		t.Errorf("slope_rating_women = %v, want 140", championship.SlopeRatingWomen)
	}

	// Front9/back9 ratings round-trip independently of the 18-hole rating.
	if championship.Front9CourseRatingMen == nil || *championship.Front9CourseRatingMen != 35.8 {
		t.Errorf("front9_course_rating_men = %v, want 35.8", championship.Front9CourseRatingMen)
	}
	if championship.Front9SlopeRatingMen == nil || *championship.Front9SlopeRatingMen != 133 {
		t.Errorf("front9_slope_rating_men = %v, want 133", championship.Front9SlopeRatingMen)
	}
	if championship.Back9CourseRatingMen == nil || *championship.Back9CourseRatingMen != 36.6 {
		t.Errorf("back9_course_rating_men = %v, want 36.6", championship.Back9CourseRatingMen)
	}
	if championship.Back9SlopeRatingMen == nil || *championship.Back9SlopeRatingMen != 137 {
		t.Errorf("back9_slope_rating_men = %v, want 137", championship.Back9SlopeRatingMen)
	}
	if championship.Front9CourseRatingWomen == nil || *championship.Front9CourseRatingWomen != 37.0 {
		t.Errorf("front9_course_rating_women = %v, want 37.0", championship.Front9CourseRatingWomen)
	}
	if championship.Front9SlopeRatingWomen == nil || *championship.Front9SlopeRatingWomen != 138 {
		t.Errorf("front9_slope_rating_women = %v, want 138", championship.Front9SlopeRatingWomen)
	}
	if championship.Back9CourseRatingWomen == nil || *championship.Back9CourseRatingWomen != 37.8 {
		t.Errorf("back9_course_rating_women = %v, want 37.8", championship.Back9CourseRatingWomen)
	}
	if championship.Back9SlopeRatingWomen == nil || *championship.Back9SlopeRatingWomen != 142 {
		t.Errorf("back9_slope_rating_women = %v, want 142", championship.Back9SlopeRatingWomen)
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
func TestUpdateCourseSetsAndClearsPhone(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	detail, err := svc.Create(ctx, owner, CreateCourseInput{Name: "Phone GC"})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}
	if detail.Phone != nil {
		t.Errorf("phone = %v, want nil when not supplied", detail.Phone)
	}

	updated, err := svc.Update(ctx, owner, detail.ID, UpdateCourseInput{
		Name:  detail.Name,
		Phone: ptr("555-867-5309"),
	})
	if err != nil {
		t.Fatalf("update course: %v", err)
	}
	if updated.Phone == nil || *updated.Phone != "555-867-5309" {
		t.Errorf("phone = %v, want 555-867-5309", updated.Phone)
	}

	cleared, err := svc.Update(ctx, owner, detail.ID, UpdateCourseInput{Name: detail.Name})
	if err != nil {
		t.Fatalf("clear phone: %v", err)
	}
	if cleared.Phone != nil {
		t.Errorf("phone = %v, want nil after clearing", cleared.Phone)
	}
}

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

	reloaded, err := svc.Get(ctx, detail.ID)
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

	reloaded, err := svc.Get(ctx, detail.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Tees[0].TotalYardage != 1200 {
		t.Errorf("total yardage = %d, want 1200", reloaded.Tees[0].TotalYardage)
	}
}

// Nobody owns a course. This test previously asserted the opposite — that only
// the creator could modify one — and it is inverted rather than deleted so the
// change in behaviour is visible in the diff.
func TestAnyoneCanModifyACourse(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	uploader := createUser(t, db, "uploader@example.com")
	stranger := createUser(t, db, "stranger@example.com")

	detail, err := svc.Create(ctx, uploader, CreateCourseInput{
		Name: "Shared GC",
		Tees: []TeeInput{{Name: "Only", Color: "#123456"}},
	})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}
	hole, tee := detail.Holes[0], detail.Tees[0]

	if detail.UploadedBy == nil || *detail.UploadedBy != uploader {
		t.Errorf("uploaded_by = %v, want the creating user", detail.UploadedBy)
	}

	// A stranger can read it, as they always could.
	if _, err := svc.Get(ctx, detail.ID); err != nil {
		t.Fatalf("stranger cannot read the course: %v", err)
	}

	// And can now correct it, which is the point: a wrong yardage should not be
	// uncorrectable just because someone else typed it in.
	if _, err := svc.Update(ctx, stranger, detail.ID, UpdateCourseInput{Name: "Corrected GC"}); err != nil {
		t.Errorf("update course: a stranger was refused: %v", err)
	}
	if _, err := svc.AddTee(ctx, stranger, detail.ID, TeeInput{Name: "Back", Color: "#000000"}); err != nil {
		t.Errorf("add tee: a stranger was refused: %v", err)
	}
	if _, err := svc.UpdateTee(ctx, stranger, tee.ID, TeeInput{Name: "Renamed", Color: "#000000"}); err != nil {
		t.Errorf("update tee: a stranger was refused: %v", err)
	}
	if _, err := svc.UpdateHole(ctx, stranger, hole.ID, HoleInput{}); err != nil {
		t.Errorf("update hole: a stranger was refused: %v", err)
	}
	if _, err := svc.SetTeeDetail(ctx, stranger, hole.ID, tee.ID, 4, 400); err != nil {
		t.Errorf("set tee detail: a stranger was refused: %v", err)
	}
	if err := svc.ClearTeeDetail(ctx, stranger, hole.ID, tee.ID); err != nil {
		t.Errorf("clear tee detail: a stranger was refused: %v", err)
	}
	if err := svc.DeleteTee(ctx, stranger, tee.ID); err != nil {
		t.Errorf("delete tee: a stranger was refused: %v", err)
	}

	// The uploader keeps no special standing either — attribution grants nothing.
	if _, err := svc.Update(ctx, uploader, detail.ID, UpdateCourseInput{Name: "Shared GC"}); err != nil {
		t.Errorf("the uploader was refused: %v", err)
	}
}

// A course outlives the account that uploaded it: the attribution goes null and
// the course stays readable and editable. This is what makes account deletion
// possible at all.
func TestCourseSurvivesItsUploader(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	uploader := createUser(t, db, "leaving@example.com")
	other := createUser(t, db, "staying@example.com")

	detail, err := svc.Create(ctx, uploader, CreateCourseInput{
		Name: "Orphan Links",
		Tees: []TeeInput{{Name: "Only", Color: "#123456"}},
	})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}

	if _, err := db.Exec("DELETE FROM users WHERE id = ?", uploader); err != nil {
		t.Fatalf("delete the uploader: %v", err)
	}

	// The whole list is the risk here, not just this row: uploaded_by is scanned
	// into a *string, and getting that wrong fails every row in the page.
	page, err := svc.List(ctx, "", "", 25, 0)
	if err != nil {
		t.Fatalf("list courses after the uploader left: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("courses = %d, want 1", len(page.Items))
	}
	if page.Items[0].UploadedBy != nil {
		t.Errorf("uploaded_by = %v, want nil once the uploader is gone", page.Items[0].UploadedBy)
	}

	reloaded, err := svc.Get(ctx, detail.ID)
	if err != nil {
		t.Fatalf("get an unattributed course: %v", err)
	}
	if len(reloaded.Tees) != 1 || len(reloaded.Holes) != DefaultHoleCount {
		t.Errorf("tees = %d, holes = %d; the course lost children", len(reloaded.Tees), len(reloaded.Holes))
	}

	// And it is still editable, rather than frozen forever.
	if _, err := svc.Update(ctx, other, detail.ID, UpdateCourseInput{Name: "Adopted Links"}); err != nil {
		t.Errorf("an unattributed course should still be editable: %v", err)
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

	reloaded, err := svc.Get(ctx, detail.ID)
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

	if _, err := svc.Get(ctx, detail.ID); err == nil {
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

func TestSearchMatchesNameAndLocationLiterally(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	for _, c := range []CreateCourseInput{
		{Name: "Pebble Ridge", Location: Location{Street: ptr("1 Fairway Dr"), City: ptr("Springfield"), Region: ptr("MO"), PostalCode: ptr("65801"), Country: ptr("USA")}},
		{Name: "Cypress Dunes", Location: Location{City: ptr("Shelbyville"), Region: ptr("KY")}},
		{Name: "50% Off Golf"},
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
		{"fairway", 1},
		// Every part of the address is searchable, which is what lets the home
		// course picker find a club by the town it is in.
		{"springfield", 1},
		{"shelbyville", 1},
		{"mo", 1},
		{"65801", 1},
		{"usa", 1},
		{"50%", 1}, // a literal % must not act as a wildcard
		{"%", 1},   // matches only the course whose name contains "%"
		{"nonexistent", 0},
		{"", 3},
	}

	for _, tc := range cases {
		page, err := svc.List(ctx, "", tc.term, 25, 0)
		if err != nil {
			t.Fatalf("search %q: %v", tc.term, err)
		}
		if page.Total != tc.want {
			t.Errorf("search %q: total = %d, want %d", tc.term, page.Total, tc.want)
		}
	}
}

// The directory's ordering is per viewer: your home course first, then the
// pinned ones, then everything by name. It has to be the query's job rather
// than the client's, because "first" has to mean first of the whole directory
// and not first of whichever page happened to be fetched.
func TestListPutsTheViewersHomeCourseFirst(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")
	stranger := createUser(t, db, "stranger@example.com")

	var homeCourseID string
	for _, c := range []CreateCourseInput{
		{Name: "Alpha GC"},
		{Name: "Zulu GC", Pinned: true},
		{Name: "Marne GC"},
	} {
		detail, err := svc.Create(ctx, owner, c)
		if err != nil {
			t.Fatalf("create %q: %v", c.Name, err)
		}
		if c.Name == "Marne GC" {
			homeCourseID = detail.ID
		}
	}
	if _, err := db.Exec("UPDATE users SET home_course_id = ? WHERE id = ?", homeCourseID, owner); err != nil {
		t.Fatalf("set home course: %v", err)
	}

	names := func(viewerID string) []string {
		t.Helper()
		page, err := svc.List(ctx, viewerID, "", 25, 0)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		out := make([]string, 0, len(page.Items))
		for _, item := range page.Items {
			out = append(out, item.Name)
		}
		return out
	}

	// Home first, then the pinned course, then the rest by name.
	got := names(owner)
	want := []string{"Marne GC", "Zulu GC", "Alpha GC"}
	if !slices.Equal(got, want) {
		t.Errorf("owner's order = %v, want %v", got, want)
	}

	// Nobody else's list is reordered by it: the home course drops back to its
	// name position behind the pinned one.
	got = names(stranger)
	want = []string{"Zulu GC", "Alpha GC", "Marne GC"}
	if !slices.Equal(got, want) {
		t.Errorf("stranger's order = %v, want %v", got, want)
	}

	// A search is ordered the same way.
	page, err := svc.List(ctx, owner, "gc", 25, 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(page.Items) != 3 || page.Items[0].Name != "Marne GC" {
		t.Errorf("search order = %v, want the home course first", page.Items)
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

	first, err := svc.List(ctx, "", "", 2, 0)
	if err != nil {
		t.Fatalf("list page 1: %v", err)
	}
	if len(first.Items) != 2 || first.Total != 3 {
		t.Errorf("page 1: %d items, total %d; want 2 and 3", len(first.Items), first.Total)
	}
	if first.Items[0].Name != "A GC" {
		t.Errorf("first item = %q, want A GC (sorted by name)", first.Items[0].Name)
	}

	second, err := svc.List(ctx, "", "", 2, 2)
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

// Stroke index ranks holes by difficulty across the whole course, so two
// holes sharing one is a data error even though each index is individually
// in range.
func TestAddHoleRejectsDuplicateHandicapIndex(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	// A nine-hole course gets holes 1-9 with no stroke index by default.
	detail, err := svc.Create(ctx, owner, CreateCourseInput{Name: "SI GC", HoleCount: 9})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}

	// Assign stroke index 5 to hole 5 so we can test for duplicates.
	if _, err := svc.UpdateHole(ctx, owner, detail.Holes[4].ID, HoleInput{HandicapIndex: ptr(5)}); err != nil {
		t.Fatalf("set hole 5 stroke index: %v", err)
	}

	if _, err := svc.AddHole(ctx, owner, detail.ID, HoleInput{HoleNumber: 10, HandicapIndex: ptr(5)}); err == nil {
		t.Error("a duplicate stroke index was accepted")
	} else if status := statusOf(t, err); status != 409 {
		t.Errorf("status = %d, want 409", status)
	}

	if _, err := svc.AddHole(ctx, owner, detail.ID, HoleInput{HoleNumber: 10, HandicapIndex: ptr(10)}); err != nil {
		t.Errorf("adding hole 10 with a fresh stroke index failed: %v", err)
	}
}

func TestUpdateHoleRejectsDuplicateHandicapIndex(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	detail, err := svc.Create(ctx, owner, CreateCourseInput{Name: "SI Update GC", HoleCount: 9})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}
	hole1, hole2 := detail.Holes[0], detail.Holes[1]

	// Assign stroke indexes so we can test duplicate detection.
	if _, err := svc.UpdateHole(ctx, owner, hole1.ID, HoleInput{HandicapIndex: ptr(1)}); err != nil {
		t.Fatalf("set hole 1 stroke index: %v", err)
	}
	if _, err := svc.UpdateHole(ctx, owner, hole2.ID, HoleInput{HandicapIndex: ptr(2)}); err != nil {
		t.Fatalf("set hole 2 stroke index: %v", err)
	}

	if _, err := svc.UpdateHole(ctx, owner, hole1.ID, HoleInput{HandicapIndex: ptr(2)}); err == nil {
		t.Error("updating a hole to another hole's stroke index was accepted")
	} else if status := statusOf(t, err); status != 409 {
		t.Errorf("status = %d, want 409", status)
	}

	// Setting a hole's stroke index to the value it already has must not trip
	// over itself as a false positive.
	if _, err := svc.UpdateHole(ctx, owner, hole1.ID, HoleInput{HandicapIndex: ptr(1)}); err != nil {
		t.Errorf("re-setting hole 1's own stroke index failed: %v", err)
	}

	// Clearing hole 2's stroke index frees it up for reuse.
	if _, err := svc.UpdateHole(ctx, owner, hole2.ID, HoleInput{}); err != nil {
		t.Fatalf("clear hole 2's stroke index: %v", err)
	}
	if _, err := svc.UpdateHole(ctx, owner, hole1.ID, HoleInput{HandicapIndex: ptr(2)}); err != nil {
		t.Errorf("reusing the now-free stroke index failed: %v", err)
	}
}

func TestGetMissingCourseIsNotFound(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	if _, err := svc.Get(ctx, id.New()); err == nil {
		t.Fatal("a missing course was returned")
	} else if status := statusOf(t, err); status != 404 {
		t.Errorf("status = %d, want 404", status)
	}
}

// Import recreates a course from the same shape Export produces, matching
// tees to hole details by name rather than ID, since IDs are not stable
// across app instances.
func TestImportRecreatesTeesAndHoleDetails(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	imported, err := svc.Import(ctx, owner, ImportCourseInput{
		Name:     "  Imported GC  ",
		Location: Location{Street: ptr("1 Fairway Dr")},
		Tees: []TeeInput{
			{Name: "Back", Color: "#000000", CourseRatingMen: ptr(72.4), SlopeRatingMen: ptr(135)},
			{Name: "Forward", Color: "#FFFFFF"},
		},
		Holes: []ImportHoleInput{
			{
				HoleNumber:    1,
				HandicapIndex: ptr(7),
				TeeDetails: []ImportTeeDetailInput{
					{TeeName: "Back", Par: 4, Yardage: 420},
					{TeeName: "Forward", Par: 3, Yardage: 165},
				},
			},
			{HoleNumber: 2, HandicapIndex: ptr(3)},
		},
	})
	if err != nil {
		t.Fatalf("import course: %v", err)
	}

	if imported.Name != "Imported GC" {
		t.Errorf("name = %q, want it trimmed", imported.Name)
	}
	if len(imported.Tees) != 2 || len(imported.Holes) != 2 {
		t.Fatalf("tees = %d, holes = %d; want 2 and 2", len(imported.Tees), len(imported.Holes))
	}

	var backID, forwardID string
	for _, tee := range imported.Tees {
		switch tee.Name {
		case "Back":
			backID = tee.ID
			if tee.CourseRatingMen == nil || *tee.CourseRatingMen != 72.4 {
				t.Errorf("back course_rating_men = %v, want 72.4", tee.CourseRatingMen)
			}
		case "Forward":
			forwardID = tee.ID
		default:
			t.Errorf("unexpected tee %q", tee.Name)
		}
	}

	hole1 := imported.Holes[0]
	if hole1.HoleNumber != 1 || hole1.HandicapIndex == nil || *hole1.HandicapIndex != 7 {
		t.Fatalf("hole 1 = %+v, want number 1 and handicap 7", hole1)
	}
	pars := map[string]int{}
	for _, d := range hole1.TeeDetails {
		pars[d.TeeID] = d.Par
	}
	if pars[backID] != 4 {
		t.Errorf("back tee par on hole 1 = %d, want 4", pars[backID])
	}
	if pars[forwardID] != 3 {
		t.Errorf("forward tee par on hole 1 = %d, want 3", pars[forwardID])
	}
}

// validateLocation checks shape, not whether the place exists: it should catch
// obvious junk without rejecting a short-but-real address. Every part is
// independently optional, so a course known only by its town still validates.
func TestValidateLocationRejectsJunk(t *testing.T) {
	cases := []struct {
		name    string
		loc     Location
		wantErr bool
	}{
		{"nothing provided", Location{}, false},
		{"blank clears the field", Location{Street: ptr(""), City: ptr("")}, false},
		{"whitespace-only clears the field", Location{Street: ptr("   ")}, false},
		{"a real address passes", Location{Street: ptr("1600 Pennsylvania Ave")}, false},
		{"a short real street passes", Location{Street: ptr("221B Baker St")}, false},
		{"street too short", Location{Street: ptr("12")}, true},
		{"street of digits only, no name", Location{Street: ptr("12345")}, true},
		{"city alone is a complete location", Location{City: ptr("Marne")}, false},
		{"a one-letter city passes", Location{City: ptr("Å")}, false},
		{"punctuation is not a city", Location{City: ptr("--")}, true},
		{"punctuation is not a region", Location{Region: ptr("-")}, true},
		{"punctuation is not a country", Location{Country: ptr(".")}, true},
		{"a postal code may be all digits", Location{PostalCode: ptr("49435")}, false},
		{"a postal code may have a space", Location{PostalCode: ptr("N1A 2B3")}, false},
		{"an over-long postal code fails", Location{PostalCode: ptr("012345678901234567890")}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := httpx.NewValidator()
			validateLocation(v, tc.loc)
			if got := !v.Valid(); got != tc.wantErr {
				t.Errorf("validateLocation(%+v): rejected = %v, want %v", tc.loc, got, tc.wantErr)
			}
		})
	}
}

// Files written before format 4 carry one `address` line instead of the five
// location fields. Those files are in users' hands, so importing one has to
// keep the address rather than drop it on the floor: it lands in `street`,
// which is where an unsplittable address line already lives.
func TestImportReadsPreV4AddressIntoStreet(t *testing.T) {
	in, err := ValidateImport(CourseExport{
		FormatVersion: 3,
		Name:          "Legacy GC",
		Address:       ptr("1 Fairway Dr, Marne, MI 49435"),
	})
	if err != nil {
		t.Fatalf("validate import: %v", err)
	}
	if in.Street == nil || *in.Street != "1 Fairway Dr, Marne, MI 49435" {
		t.Errorf("street = %v, want the old address line", in.Street)
	}

	// A current file names street directly, and its own value wins.
	in, err = ValidateImport(CourseExport{
		FormatVersion: courseExportFormatVersion,
		Name:          "Current GC",
		Location:      Location{Street: ptr("2 Fairway Dr"), City: ptr("Marne")},
		Address:       ptr("should be ignored"),
	})
	if err != nil {
		t.Fatalf("validate import: %v", err)
	}
	if in.Street == nil || *in.Street != "2 Fairway Dr" {
		t.Errorf("street = %v, want 2 Fairway Dr", in.Street)
	}
	if in.City == nil || *in.City != "Marne" {
		t.Errorf("city = %v, want Marne", in.City)
	}
}

// The export a course produces must not carry the legacy `address` key at all,
// so a file round-tripped through this server stops re-seeding it.
func TestExportOmitsLegacyAddress(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	detail, err := svc.Create(ctx, owner, CreateCourseInput{
		Name:     "Pebble Ridge",
		Location: Location{Street: ptr("1 Fairway Dr"), City: ptr("Marne")},
	})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}

	encoded, err := json.Marshal(buildExport(detail))
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal export: %v", err)
	}
	if _, ok := decoded["address"]; ok {
		t.Errorf("export still carries an `address` key: %s", encoded)
	}
	if decoded["street"] != "1 Fairway Dr" {
		t.Errorf("street = %v, want 1 Fairway Dr", decoded["street"])
	}
	if decoded["format_version"] != float64(courseExportFormatVersion) {
		t.Errorf("format_version = %v, want %d", decoded["format_version"], courseExportFormatVersion)
	}
}

// TestImportUpdatesExistingCourse verifies that re-importing a v2 export
// file owned by the same user updates the course in place rather than
// creating a duplicate.
func TestImportUpdatesExistingCourse(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	// Create a course via import (simulating a first import).
	original, err := svc.Import(ctx, owner, ImportCourseInput{
		Name:     "Original Name",
		Location: Location{Street: ptr("100 Main St")},
		Phone:    ptr("555-0000"),
		Tees: []TeeInput{
			{Name: "Back", Color: "#000000", CourseRatingMen: ptr(72.0), SlopeRatingMen: ptr(130)},
			{Name: "Forward", Color: "#FFFFFF"},
			{Name: "ToRemove", Color: "#FF0000"},
		},
		Holes: []ImportHoleInput{
			{
				HoleNumber:    1,
				HandicapIndex: ptr(1),
				TeeDetails: []ImportTeeDetailInput{
					{TeeName: "Back", Par: 4, Yardage: 400},
					{TeeName: "Forward", Par: 3, Yardage: 250},
					{TeeName: "ToRemove", Par: 4, Yardage: 350},
				},
			},
			{
				HoleNumber:    2,
				HandicapIndex: ptr(2),
				TeeDetails: []ImportTeeDetailInput{
					{TeeName: "Back", Par: 5, Yardage: 500},
				},
			},
			{
				HoleNumber: 3,
			},
		},
	})
	if err != nil {
		t.Fatalf("initial import: %v", err)
	}

	// Re-import with the same ID but changed values: rename, change par,
	// remove a tee, remove a hole, add a new hole.
	updated, err := svc.Import(ctx, owner, ImportCourseInput{
		ID:       original.ID,
		Name:     "Updated Name",
		Location: Location{Street: ptr("200 New Ave"), City: ptr("Marne")},
		Phone:    ptr("555-1111"),
		Tees: []TeeInput{
			{Name: "Back", Color: "#000000", CourseRatingMen: ptr(73.5), SlopeRatingMen: ptr(135)},
			{Name: "Forward", Color: "#FFFFFF"},
			// "ToRemove" tee is gone.
		},
		Holes: []ImportHoleInput{
			{
				HoleNumber:    1,
				HandicapIndex: ptr(5),
				TeeDetails: []ImportTeeDetailInput{
					{TeeName: "Back", Par: 5, Yardage: 450}, // changed par and yardage
					{TeeName: "Forward", Par: 4, Yardage: 300},
				},
			},
			{
				HoleNumber:    2,
				HandicapIndex: ptr(2),
				TeeDetails: []ImportTeeDetailInput{
					{TeeName: "Back", Par: 5, Yardage: 510},
				},
			},
			// Hole 3 removed, hole 4 added.
			{
				HoleNumber:    4,
				HandicapIndex: ptr(3),
				TeeDetails: []ImportTeeDetailInput{
					{TeeName: "Back", Par: 3, Yardage: 180},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}

	// Same course ID — not a duplicate.
	if updated.ID != original.ID {
		t.Errorf("course ID changed: %s -> %s", original.ID, updated.ID)
	}

	// Course-level fields updated.
	if updated.Name != "Updated Name" {
		t.Errorf("name = %q, want Updated Name", updated.Name)
	}
	if updated.Street == nil || *updated.Street != "200 New Ave" {
		t.Errorf("street = %v, want 200 New Ave", updated.Street)
	}
	if updated.City == nil || *updated.City != "Marne" {
		t.Errorf("city = %v, want Marne", updated.City)
	}

	// Tees: "ToRemove" gone, "Back" rating updated.
	if len(updated.Tees) != 2 {
		t.Fatalf("tees = %d, want 2", len(updated.Tees))
	}
	for _, tee := range updated.Tees {
		if tee.Name == "ToRemove" {
			t.Error("deleted tee ToRemove still present")
		}
		if tee.Name == "Back" {
			if tee.CourseRatingMen == nil || *tee.CourseRatingMen != 73.5 {
				t.Errorf("back course_rating_men = %v, want 73.5", tee.CourseRatingMen)
			}
		}
	}

	// Holes: 3 holes (1, 2, 4); hole 3 removed.
	if len(updated.Holes) != 3 {
		t.Fatalf("holes = %d, want 3", len(updated.Holes))
	}
	holeNumbers := map[int]bool{}
	for _, h := range updated.Holes {
		holeNumbers[h.HoleNumber] = true
	}
	if holeNumbers[3] {
		t.Error("deleted hole 3 still present")
	}
	if !holeNumbers[4] {
		t.Error("new hole 4 missing")
	}

	// Hole 1 should have updated par.
	hole1 := updated.Holes[0]
	if hole1.HoleNumber != 1 {
		t.Fatalf("first hole = %d, want 1", hole1.HoleNumber)
	}
	if hole1.HandicapIndex == nil || *hole1.HandicapIndex != 5 {
		t.Errorf("hole 1 handicap_index = %v, want 5", hole1.HandicapIndex)
	}
	var backTeeID string
	for _, tee := range updated.Tees {
		if tee.Name == "Back" {
			backTeeID = tee.ID
		}
	}
	for _, d := range hole1.TeeDetails {
		if d.TeeID == backTeeID && d.Par != 5 {
			t.Errorf("hole 1 back tee par = %d, want 5", d.Par)
		}
	}

	// Verify only one course exists.
	page, err := svc.List(ctx, "", "", 100, 0)
	if err != nil {
		t.Fatalf("list courses: %v", err)
	}
	if page.Total != 1 {
		t.Errorf("total courses = %d, want 1 (no duplicate)", page.Total)
	}
}

// Importing a file that names an existing course updates that course, whoever
// uploaded it. This inverts an earlier test which asserted the import forked a
// copy instead — that fork only existed to avoid touching someone else's
// property, and courses are nobody's property now.
//
// The sharp edge, worth knowing: import is a destructive sync, so a stale file
// removes tees and holes added since it was exported. That was already true for
// the uploader; it now applies to everyone, exactly as hand-editing does.
func TestImportUpdatesAnExistingCourseWhoeverUploadedIt(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	uploader := createUser(t, db, "uploader@example.com")
	other := createUser(t, db, "other@example.com")

	original, err := svc.Import(ctx, uploader, ImportCourseInput{
		Name: "Shared Course",
		Tees: []TeeInput{{Name: "Back", Color: "#000000"}},
		Holes: []ImportHoleInput{
			{HoleNumber: 1, TeeDetails: []ImportTeeDetailInput{{TeeName: "Back", Par: 4, Yardage: 400}}},
		},
	})
	if err != nil {
		t.Fatalf("create original: %v", err)
	}

	imported, err := svc.Import(ctx, other, ImportCourseInput{
		ID:   original.ID,
		Name: "Shared Course, corrected",
		Tees: []TeeInput{{Name: "Back", Color: "#000000"}},
		Holes: []ImportHoleInput{
			{HoleNumber: 1, TeeDetails: []ImportTeeDetailInput{{TeeName: "Back", Par: 4, Yardage: 415}}},
		},
	})
	if err != nil {
		t.Fatalf("import by another user: %v", err)
	}

	if imported.ID != original.ID {
		t.Errorf("import created a new course %q; want it to update %q", imported.ID, original.ID)
	}
	if imported.Name != "Shared Course, corrected" {
		t.Errorf("name = %q, want the imported correction", imported.Name)
	}
	if imported.Holes[0].TeeDetails[0].Yardage != 415 {
		t.Errorf("yardage = %d, want the corrected 415", imported.Holes[0].TeeDetails[0].Yardage)
	}

	// Attribution stays with whoever uploaded it first; an edit is not a claim.
	if imported.UploadedBy == nil || *imported.UploadedBy != uploader {
		t.Errorf("uploaded_by = %v, want it unchanged at the original uploader", imported.UploadedBy)
	}

	page, err := svc.List(ctx, "", "", 100, 0)
	if err != nil {
		t.Fatalf("list courses: %v", err)
	}
	if page.Total != 1 {
		t.Errorf("total courses = %d, want 1 (updated, not forked)", page.Total)
	}
}

// fakeGeocoder records what it was asked and answers with whatever it was told
// to, so the service's rules can be tested without a network.
type fakeGeocoder struct {
	result  *geocode.Result
	err     error
	queries []string
}

func (f *fakeGeocoder) Lookup(_ context.Context, address string) (*geocode.Result, error) {
	f.queries = append(f.queries, address)
	return f.result, f.err
}

// Coordinates fill themselves from the address, so nobody types a latitude by
// hand. The query is the postal line, which is the form a geocoder parses best.
func TestCreateGeocodesTheAddress(t *testing.T) {
	_, db := newTestService(t)
	geo := &fakeGeocoder{result: &geocode.Result{Latitude: 43.0331, Longitude: -85.8225}}
	svc := NewService(db, geo)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	detail, err := svc.Create(ctx, owner, CreateCourseInput{
		Name: "Sand Creek",
		Location: Location{
			Street: ptr("1831 Johnson St."), City: ptr("Marne"),
			Region: ptr("MI"), PostalCode: ptr("49435"), Country: ptr("USA"),
		},
	})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}

	if detail.Latitude == nil || *detail.Latitude != 43.0331 {
		t.Errorf("latitude = %v, want 43.0331", detail.Latitude)
	}
	if detail.Longitude == nil || *detail.Longitude != -85.8225 {
		t.Errorf("longitude = %v, want -85.8225", detail.Longitude)
	}
	want := []string{"1831 Johnson St., Marne, MI 49435, USA"}
	if !slices.Equal(geo.queries, want) {
		t.Errorf("queries = %v, want %v", geo.queries, want)
	}
}

// Geocoding fills gaps; it never corrects anybody. A course saved with a point
// keeps that point and costs no lookup.
func TestCreateKeepsSuppliedCoordinates(t *testing.T) {
	_, db := newTestService(t)
	geo := &fakeGeocoder{result: &geocode.Result{Latitude: 1, Longitude: 2}}
	svc := NewService(db, geo)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	detail, err := svc.Create(ctx, owner, CreateCourseInput{
		Name:      "Hand Placed",
		Location:  Location{City: ptr("Marne")},
		Latitude:  ptr(40.0),
		Longitude: ptr(-80.0),
	})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}
	if detail.Latitude == nil || *detail.Latitude != 40.0 {
		t.Errorf("latitude = %v, want the supplied 40", detail.Latitude)
	}
	if len(geo.queries) != 0 {
		t.Errorf("queries = %v, want none when a point was supplied", geo.queries)
	}
}

// Clearing both coordinates and saving is how a course gets re-placed after its
// address changes. It is the only re-trigger, and it has to work.
func TestUpdateReGeocodesWhenCoordinatesAreCleared(t *testing.T) {
	_, db := newTestService(t)
	geo := &fakeGeocoder{result: &geocode.Result{Latitude: 43.0331, Longitude: -85.8225}}
	svc := NewService(db, geo)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	detail, err := svc.Create(ctx, owner, CreateCourseInput{
		Name: "Moved GC", Location: Location{City: ptr("Marne")},
		Latitude: ptr(1.0), Longitude: ptr(2.0),
	})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}

	// Saving the coordinates back unchanged must not re-geocode.
	if _, err := svc.Update(ctx, owner, detail.ID, UpdateCourseInput{
		Name: detail.Name, Location: Location{City: ptr("Marne")},
		Latitude: detail.Latitude, Longitude: detail.Longitude,
	}); err != nil {
		t.Fatalf("update course: %v", err)
	}
	if len(geo.queries) != 0 {
		t.Fatalf("queries = %v, want none while a point is set", geo.queries)
	}

	updated, err := svc.Update(ctx, owner, detail.ID, UpdateCourseInput{
		Name:     detail.Name,
		Location: Location{Street: ptr("2475 Johnson St"), City: ptr("Marne")},
	})
	if err != nil {
		t.Fatalf("update course: %v", err)
	}
	if updated.Latitude == nil || *updated.Latitude != 43.0331 {
		t.Errorf("latitude = %v, want it re-resolved to 43.0331", updated.Latitude)
	}
	if len(geo.queries) != 1 {
		t.Errorf("queries = %v, want exactly the one after clearing", geo.queries)
	}
}

// None of a geocoder's failures is a reason to refuse to save a golf course.
func TestGeocodeFailuresNeverBlockASave(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name string
		geo  geocode.Geocoder
		loc  Location
		want int // expected lookups
	}{
		{"lookup errors", &fakeGeocoder{err: errors.New("nominatim is down")}, Location{City: ptr("Marne")}, 1},
		{"address is unknown", &fakeGeocoder{}, Location{City: ptr("Marne")}, 1},
		{"no geocoder configured", nil, Location{City: ptr("Marne")}, 0},
		// A country on its own would geocode to the middle of the country, so
		// it is not asked at all: a pin in Kansas is worse than no pin.
		{"too little address to place", &fakeGeocoder{}, Location{Country: ptr("USA")}, 0},
		{"no address at all", &fakeGeocoder{}, Location{}, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, db := newTestService(t)
			svc := NewService(db, tc.geo)
			owner := createUser(t, db, "owner@example.com")

			detail, err := svc.Create(ctx, owner, CreateCourseInput{Name: "Resilient GC", Location: tc.loc})
			if err != nil {
				t.Fatalf("create course: %v", err)
			}
			if detail.Latitude != nil || detail.Longitude != nil {
				t.Errorf("point = %v, %v; want none", detail.Latitude, detail.Longitude)
			}
			if fake, ok := tc.geo.(*fakeGeocoder); ok && len(fake.queries) != tc.want {
				t.Errorf("lookups = %d, want %d", len(fake.queries), tc.want)
			}
		})
	}
}

// A restore can carry sixty courses, and sixty lookups at one a second is both
// a minute of held-open request and the bulk geocoding the OSM policy forbids.
func TestImportNeverGeocodes(t *testing.T) {
	_, db := newTestService(t)
	geo := &fakeGeocoder{result: &geocode.Result{Latitude: 43.0331, Longitude: -85.8225}}
	svc := NewService(db, geo)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	if _, err := svc.Import(ctx, owner, ImportCourseInput{
		Name:     "Imported GC",
		Location: Location{Street: ptr("1831 Johnson St."), City: ptr("Marne")},
	}); err != nil {
		t.Fatalf("import course: %v", err)
	}
	if len(geo.queries) != 0 {
		t.Errorf("queries = %v, want none: import must not geocode in bulk", geo.queries)
	}
}

// Holes are self-limiting (1..18, no duplicates); tees had no cap at all, so a
// single request could ask for as many rows as fit in the body limit.
func TestTeeCountIsCapped(t *testing.T) {
	exported := CourseExport{
		FormatVersion: courseExportFormatVersion,
		Name:          "Too Many Tees",
	}
	for i := range MaxTeesPerCourse + 1 {
		exported.Tees = append(exported.Tees, teeRequest{
			Name:  "Tee " + strconv.Itoa(i),
			Color: "#FFFFFF",
		})
	}

	if _, err := ValidateImport(exported); err == nil {
		t.Errorf("err = nil for %d tees, want the request refused", len(exported.Tees))
	}

	// One under the cap is still fine.
	exported.Tees = exported.Tees[:MaxTeesPerCourse]
	if _, err := ValidateImport(exported); err != nil {
		t.Errorf("%d tees was refused: %v", MaxTeesPerCourse, err)
	}
}
