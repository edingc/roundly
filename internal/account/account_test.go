package account

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/edingc/roundly/internal/auth"
	"github.com/edingc/roundly/internal/course"
	"github.com/edingc/roundly/internal/database"
	"github.com/edingc/roundly/internal/database/sqlc"
	"github.com/edingc/roundly/internal/httpx"
	"github.com/edingc/roundly/internal/id"
	"github.com/edingc/roundly/internal/timex"
)

const testPassword = "test-password-123"

func newTestService(t *testing.T) (*Service, *database.DB) {
	t.Helper()

	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	tokens := auth.NewTokenIssuer([]byte("test-secret-that-is-long-enough-to-sign"), time.Minute, time.Hour)
	authService := auth.NewService(db, tokens, auth.NewGoogleProvider("", "", ""), auth.Options{
		Avatars: auth.NewAvatarSigner([]byte("test-secret-that-is-long-enough-to-sign")),
	})
	return NewService(db, authService, course.NewService(db, nil)), db
}

// createUser makes an account with a password, as a signup would.
func createUser(t *testing.T, db *database.DB, email string) string {
	t.Helper()

	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	userID := id.New()
	now := timex.Now()
	if err := db.Queries.CreateUser(context.Background(), sqlc.CreateUserParams{
		ID:           userID,
		Email:        email,
		PasswordHash: &hash,
		DisplayName:  email,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return userID
}

// createOAuthUser makes a password-less account, as Google sign-in would.
func createOAuthUser(t *testing.T, db *database.DB, email string) string {
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

func statusOf(t *testing.T, err error) int {
	t.Helper()

	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	var apiErr *httpx.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected an *httpx.APIError, got %T: %v", err, err)
	}
	return apiErr.Status
}

func countRows(t *testing.T, db *database.DB, table, where string, args ...any) int {
	t.Helper()

	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE "+where, args...).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// The case that was impossible before courses stopped having an owner: an
// account with courses could not be deleted at all, because courses.created_by
// had no ON DELETE clause.
func TestDeleteAccountWithCourses(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	leaving := createUser(t, db, "leaving@example.test")
	staying := createUser(t, db, "staying@example.test")

	courses := course.NewService(db, nil)
	detail, err := courses.Create(ctx, leaving, course.CreateCourseInput{
		Name: "Legacy Links",
		Tees: []course.TeeInput{{Name: "Back", Color: "#000000"}},
	})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}

	// Another player has it as their home course. Deleting the account must not
	// disturb them.
	if _, err := svc.UpdateProfile(ctx, staying, ProfileInput{
		DisplayName:  "Staying",
		HomeCourseID: &detail.ID,
	}); err != nil {
		t.Fatalf("set home course: %v", err)
	}

	// And the leaving account owns things that should go with it.
	if err := db.Queries.CreateClub(ctx, sqlc.CreateClubParams{
		ID: id.New(), UserID: leaving, ClubType: "iron", Label: "7 iron",
		Active: 1, CreatedAt: timex.Now(), UpdatedAt: timex.Now(),
	}); err != nil {
		t.Fatalf("create club: %v", err)
	}
	if err := db.Queries.CreateAPIKey(ctx, sqlc.CreateAPIKeyParams{
		ID: id.New(), UserID: leaving, Name: "key", KeyHash: "hash", KeyPrefix: "rnd_x",
		Scope: "read", CreatedAt: timex.Now(),
	}); err != nil {
		t.Fatalf("create api key: %v", err)
	}
	if err := db.Queries.UpsertUserAvatar(ctx, sqlc.UpsertUserAvatarParams{
		UserID: leaving, Image: []byte{0xFF, 0xD8}, ContentType: "image/jpeg",
		ByteSize: 2, UpdatedAt: timex.Now(),
	}); err != nil {
		t.Fatalf("create avatar: %v", err)
	}

	if err := svc.DeleteAccount(ctx, leaving, testPassword, time.Now()); err != nil {
		t.Fatalf("delete account: %v", err)
	}

	// The account and everything personal to it are gone.
	if n := countRows(t, db, "users", "id = ?", leaving); n != 0 {
		t.Errorf("users = %d, want 0", n)
	}
	for _, table := range []string{"clubs", "api_keys", "user_avatars", "refresh_tokens", "oauth_accounts"} {
		if n := countRows(t, db, table, "user_id = ?", leaving); n != 0 {
			t.Errorf("%s = %d, want 0 — it should have cascaded", table, n)
		}
	}

	// The course stays, unattributed.
	reloaded, err := courses.Get(ctx, detail.ID)
	if err != nil {
		t.Fatalf("the course went with the account: %v", err)
	}
	if reloaded.UploadedBy != nil {
		t.Errorf("uploaded_by = %v, want nil", reloaded.UploadedBy)
	}
	if len(reloaded.Tees) != 1 || len(reloaded.Holes) != course.DefaultHoleCount {
		t.Errorf("tees = %d, holes = %d; the course lost children", len(reloaded.Tees), len(reloaded.Holes))
	}

	// And the other player's home course is untouched. This is the cross-user
	// damage the whole design exists to avoid.
	other, err := svc.reload(ctx, staying)
	if err != nil {
		t.Fatalf("reload the staying user: %v", err)
	}
	if other.HomeCourseID == nil || *other.HomeCourseID != detail.ID {
		t.Errorf("home_course_id = %v, want it unchanged at %q", other.HomeCourseID, detail.ID)
	}
}

func TestDeleteAccountRequiresTheCorrectPassword(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	userID := createUser(t, db, "careful@example.test")

	for name, password := range map[string]string{
		"empty": "",
		"blank": "   ",
		"wrong": "not-the-password",
	} {
		t.Run(name, func(t *testing.T) {
			err := svc.DeleteAccount(ctx, userID, password, time.Now())
			if status := statusOf(t, err); status != 422 {
				t.Errorf("status = %d, want 422", status)
			}
			if n := countRows(t, db, "users", "id = ?", userID); n != 1 {
				t.Error("the account was deleted despite the refusal")
			}
		})
	}
}

// A Google-only account has no password to demand, so it has to have proved
// itself recently instead.
func TestDeleteAccountWithoutPasswordNeedsARecentSession(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	userID := createOAuthUser(t, db, "google@example.test")

	stale := time.Now().Add(-reauthWindow - time.Minute)
	err := svc.DeleteAccount(ctx, userID, "", stale)
	if status := statusOf(t, err); status != 403 {
		t.Errorf("status = %d, want 403", status)
	}
	if n := countRows(t, db, "users", "id = ?", userID); n != 1 {
		t.Fatal("a stale session deleted the account")
	}

	if err := svc.DeleteAccount(ctx, userID, "", time.Now()); err != nil {
		t.Fatalf("a fresh session was refused: %v", err)
	}
	if n := countRows(t, db, "users", "id = ?", userID); n != 0 {
		t.Error("the account survived a valid deletion")
	}
}

// The address becomes available again, which is what "deleted" has to mean for
// someone who wants to start over.
func TestDeletedEmailCanBeReused(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	userID := createUser(t, db, "returning@example.test")

	if err := svc.DeleteAccount(ctx, userID, testPassword, time.Now()); err != nil {
		t.Fatalf("delete account: %v", err)
	}

	if _, err := svc.auth.SignUp(ctx, "returning@example.test", testPassword, "Back Again"); err != nil {
		t.Errorf("the freed address was refused for a new signup: %v", err)
	}
}

// A removal request outlives the person who raised it.
func TestDeleteAccountKeepsTheirRemovalRequests(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	requester := createUser(t, db, "requester@example.test")
	other := createUser(t, db, "other@example.test")

	courses := course.NewService(db, nil)
	detail, err := courses.Create(ctx, other, course.CreateCourseInput{Name: "Disputed GC"})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}
	if _, err := courses.RequestRemoval(ctx, requester, detail.ID, "Permanently closed"); err != nil {
		t.Fatalf("request removal: %v", err)
	}

	if err := svc.DeleteAccount(ctx, requester, testPassword, time.Now()); err != nil {
		t.Fatalf("delete account: %v", err)
	}

	pending, err := courses.PendingRemovals(ctx)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want 1 — the request went with its requester", len(pending))
	}
	if pending[0].RequestedBy != nil {
		t.Errorf("requested_by = %v, want nil", pending[0].RequestedBy)
	}
}

// Gender is a preference with its own statement, precisely so that saving a
// name cannot disturb which ratings a round records. Before the split they
// shared one UPDATE, and a profile save that did not carry gender blanked it.
func TestSavingTheProfileLeavesGenderAlone(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	userID := createUser(t, db, "player@example.com")

	if _, err := svc.auth.SetGender(ctx, userID, strPtr(auth.GenderWomen)); err != nil {
		t.Fatalf("set gender: %v", err)
	}

	updated, err := svc.UpdateProfile(ctx, userID, ProfileInput{
		DisplayName:  "Renamed Golfer",
		LocationCity: strPtr("Marne"),
	})
	if err != nil {
		t.Fatalf("update profile: %v", err)
	}
	if updated.DisplayName != "Renamed Golfer" {
		t.Errorf("display name = %q, want it saved", updated.DisplayName)
	}
	if updated.Gender == nil || *updated.Gender != auth.GenderWomen {
		t.Errorf("gender = %v after a profile save, want it untouched", updated.Gender)
	}
}

// Clearing it back to unset is a real choice, not a missing value: unset means
// the men's ratings.
func TestGenderCanBeClearedBackToUnset(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	userID := createUser(t, db, "player@example.com")

	if _, err := svc.auth.SetGender(ctx, userID, strPtr(auth.GenderMen)); err != nil {
		t.Fatalf("set: %v", err)
	}
	cleared, err := svc.auth.SetGender(ctx, userID, nil)
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if cleared.Gender != nil {
		t.Errorf("gender = %v, want nil", cleared.Gender)
	}

	if _, err := svc.auth.SetGender(ctx, userID, strPtr("other")); err == nil {
		t.Error("err = nil for an unknown value, want it refused")
	}
}
