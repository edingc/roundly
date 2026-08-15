package club

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
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

// clubs.user_id is a foreign key, so tests need real user rows.
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

func statusOfErr(t *testing.T, err error) int {
	t.Helper()

	var apiErr *httpx.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected an *httpx.APIError, got %T: %v", err, err)
	}
	return apiErr.Status
}

func ptr[T any](v T) *T { return &v }

// addClub creates a club and fails the test if it cannot.
func addClub(t *testing.T, svc *Service, userID string, in ClubInput) *Club {
	t.Helper()

	c, err := svc.Create(context.Background(), userID, in)
	if err != nil {
		t.Fatalf("create club %q: %v", in.Label, err)
	}
	return c
}

func TestCreateClubDefaultsToActiveAndTrims(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	c := addClub(t, svc, owner, ClubInput{
		Type:  "wedge",
		Label: "  56° Sand Wedge  ",
		Brand: ptr("  Vokey  "),
		Model: ptr("   "),
		Loft:  ptr(56.0),
	})

	if c.Status != StatusActive {
		t.Errorf("status = %q, want %q", c.Status, StatusActive)
	}
	if c.RetiredAt != nil {
		t.Errorf("retired_at = %v, want nil", *c.RetiredAt)
	}
	if c.Label != "56° Sand Wedge" {
		t.Errorf("label = %q, want it trimmed", c.Label)
	}
	if c.Brand == nil || *c.Brand != "Vokey" {
		t.Errorf("brand = %v, want trimmed %q", c.Brand, "Vokey")
	}
	// A whitespace-only optional field clears rather than storing blanks.
	if c.Model != nil {
		t.Errorf("model = %q, want nil for a blank value", *c.Model)
	}

	bag, err := svc.Bag(ctx, owner)
	if err != nil {
		t.Fatalf("bag: %v", err)
	}
	if len(bag.Active) != 1 || len(bag.Benched) != 0 || len(bag.Retired) != 0 {
		t.Fatalf("bag groups = %d/%d/%d, want 1/0/0",
			len(bag.Active), len(bag.Benched), len(bag.Retired))
	}
}

// Type and label are the only required fields. A player who wants nothing more
// than "4 Iron" in their bag should not be made to fill anything else in.
func TestTypeAndLabelAreTheOnlyRequiredFields(t *testing.T) {
	svc, db := newTestService(t)
	owner := createUser(t, db, "owner@example.com")

	c := addClub(t, svc, owner, ClubInput{Type: "iron", Label: "4 Iron"})

	if c.Label != "4 Iron" || c.Type != "iron" {
		t.Fatalf("club = %q/%q, want %q/%q", c.Label, c.Type, "4 Iron", "iron")
	}
	// Everything else comes back empty rather than defaulted to something.
	for name, value := range map[string]any{
		"brand":              c.Brand,
		"model":              c.Model,
		"loft":               c.Loft,
		"shaft":              c.Shaft,
		"flex":               c.Flex,
		"notes":              c.Notes,
		"expected_carry":     c.ExpectedCarry,
		"average_dispersion": c.AverageDispersion,
	} {
		if !reflect.ValueOf(value).IsNil() {
			t.Errorf("%s = %v, want nil on a bare club", name, value)
		}
	}

	// And the request-level validator agrees: nothing but type and label.
	v := httpx.NewValidator()
	validateClub(v, clubRequest{Type: "iron", Label: "4 Iron"})
	if err := v.Err(); err != nil {
		t.Errorf("a type-and-label-only request was rejected: %v", err)
	}
}

func TestNewClubsAppendToTheBag(t *testing.T) {
	svc, db := newTestService(t)
	owner := createUser(t, db, "owner@example.com")

	first := addClub(t, svc, owner, ClubInput{Type: "driver", Label: "Driver"})
	second := addClub(t, svc, owner, ClubInput{Type: "putter", Label: "Putter"})

	if second.DisplayOrder <= first.DisplayOrder {
		t.Errorf("display order %d did not append after %d",
			second.DisplayOrder, first.DisplayOrder)
	}
}

func TestStatusTransitions(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	c := addClub(t, svc, owner, ClubInput{Type: "iron", Label: "2 iron"})

	benched, err := svc.SetStatus(ctx, owner, c.ID, StatusBenched)
	if err != nil {
		t.Fatalf("bench: %v", err)
	}
	if benched.Status != StatusBenched || benched.RetiredAt != nil {
		t.Errorf("benched club = %+v, want benched with no retired_at", benched)
	}

	retired, err := svc.SetStatus(ctx, owner, c.ID, StatusRetired)
	if err != nil {
		t.Fatalf("retire: %v", err)
	}
	if retired.Status != StatusRetired {
		t.Errorf("status = %q, want %q", retired.Status, StatusRetired)
	}
	if retired.RetiredAt == nil {
		t.Fatal("retired club has no retired_at")
	}

	// Coming back from retirement lands on the bench, not straight into the
	// bag: the active set only changes when the player says so.
	revived, err := svc.SetStatus(ctx, owner, c.ID, StatusBenched)
	if err != nil {
		t.Fatalf("unretire: %v", err)
	}
	if revived.Status != StatusBenched {
		t.Errorf("status = %q, want %q", revived.Status, StatusBenched)
	}
	if revived.RetiredAt != nil {
		t.Errorf("retired_at = %q, want cleared", *revived.RetiredAt)
	}
}

func TestRetireIsIdempotentOnTheTimestamp(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	c := addClub(t, svc, owner, ClubInput{Type: "wood", Label: "3 wood"})

	first, err := svc.SetStatus(ctx, owner, c.ID, StatusRetired)
	if err != nil {
		t.Fatalf("retire: %v", err)
	}
	again, err := svc.SetStatus(ctx, owner, c.ID, StatusRetired)
	if err != nil {
		t.Fatalf("retire again: %v", err)
	}

	// retired_at records when the club left the bag, so re-retiring must not
	// move it forward.
	if *again.RetiredAt != *first.RetiredAt {
		t.Errorf("retired_at moved from %q to %q", *first.RetiredAt, *again.RetiredAt)
	}
}

// The ID is what Phase 3 rounds and Phase 4 shots will reference, so it has to
// survive every status change and every edit.
func TestIDIsStableAcrossStatusChangesAndEdits(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	c := addClub(t, svc, owner, ClubInput{Type: "iron", Label: "7 iron"})
	original := c.ID

	for _, status := range []Status{StatusBenched, StatusRetired, StatusBenched, StatusActive} {
		updated, err := svc.SetStatus(ctx, owner, c.ID, status)
		if err != nil {
			t.Fatalf("set status %q: %v", status, err)
		}
		if updated.ID != original {
			t.Fatalf("ID changed to %q on transition to %q", updated.ID, status)
		}
	}

	edited, err := svc.Update(ctx, owner, c.ID, ClubInput{Type: "hybrid", Label: "4 hybrid"})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if edited.ID != original {
		t.Errorf("ID changed to %q on edit", edited.ID)
	}
}

// Update edits description only; status moves go through SetStatus, so a stale
// edit form cannot pull a retired club back into the bag.
func TestUpdateLeavesStatusAlone(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	c := addClub(t, svc, owner, ClubInput{Type: "wedge", Label: "60° LW"})
	if _, err := svc.SetStatus(ctx, owner, c.ID, StatusRetired); err != nil {
		t.Fatalf("retire: %v", err)
	}

	updated, err := svc.Update(ctx, owner, c.ID, ClubInput{
		Type:  "wedge",
		Label: "60° Lob Wedge",
		Loft:  ptr(60.0),
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Status != StatusRetired {
		t.Errorf("status = %q, want it left at %q", updated.Status, StatusRetired)
	}
	if updated.Label != "60° Lob Wedge" {
		t.Errorf("label = %q, want the edit applied", updated.Label)
	}
}

func TestBagReportsTheFourteenClubLimit(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	for i := 0; i < ClubLimit; i++ {
		addClub(t, svc, owner, ClubInput{Type: "iron", Label: "Club"})
	}

	bag, err := svc.Bag(ctx, owner)
	if err != nil {
		t.Fatalf("bag: %v", err)
	}
	if bag.ActiveCount != ClubLimit || bag.OverLimit {
		t.Errorf("at the limit: count=%d over=%v, want %d and false",
			bag.ActiveCount, bag.OverLimit, ClubLimit)
	}

	// The 15th is accepted — the rule is reported, not enforced, because a bag
	// mid-edit legitimately passes through invalid states.
	addClub(t, svc, owner, ClubInput{Type: "wedge", Label: "One too many"})

	bag, err = svc.Bag(ctx, owner)
	if err != nil {
		t.Fatalf("bag: %v", err)
	}
	if bag.ActiveCount != ClubLimit+1 || !bag.OverLimit {
		t.Errorf("over the limit: count=%d over=%v, want %d and true",
			bag.ActiveCount, bag.OverLimit, ClubLimit+1)
	}

	// Benched and retired clubs do not count against the limit.
	if _, err := svc.SetStatus(ctx, owner, bag.Active[0].ID, StatusBenched); err != nil {
		t.Fatalf("bench: %v", err)
	}
	bag, err = svc.Bag(ctx, owner)
	if err != nil {
		t.Fatalf("bag: %v", err)
	}
	if bag.ActiveCount != ClubLimit || bag.OverLimit {
		t.Errorf("after benching one: count=%d over=%v, want %d and false",
			bag.ActiveCount, bag.OverLimit, ClubLimit)
	}
}

// A bag is private, unlike the shared course directory.
func TestBagsAreScopedToTheirOwner(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")
	other := createUser(t, db, "other@example.com")

	c := addClub(t, svc, owner, ClubInput{Type: "driver", Label: "Driver"})

	bag, err := svc.Bag(ctx, other)
	if err != nil {
		t.Fatalf("bag: %v", err)
	}
	if len(bag.Active)+len(bag.Benched)+len(bag.Retired) != 0 {
		t.Errorf("another user's bag returned %d clubs, want 0",
			len(bag.Active)+len(bag.Benched)+len(bag.Retired))
	}

	// Not found rather than forbidden: confirming the ID exists would leak more
	// than the refusal is worth.
	for name, err := range map[string]error{
		"get":    mustErr(svc.Get(ctx, other, c.ID)),
		"update": mustErr(svc.Update(ctx, other, c.ID, ClubInput{Type: "iron", Label: "Mine now"})),
		"status": mustErr(svc.SetStatus(ctx, other, c.ID, StatusRetired)),
		"delete": svc.Delete(ctx, other, c.ID),
	} {
		if err == nil {
			t.Fatalf("%s: another user's club was reachable", name)
		}
		if got := statusOfErr(t, err); got != http.StatusNotFound {
			t.Errorf("%s: status = %d, want %d", name, got, http.StatusNotFound)
		}
	}

	// And the club is untouched.
	still, err := svc.Get(ctx, owner, c.ID)
	if err != nil {
		t.Fatalf("owner get: %v", err)
	}
	if still.Label != "Driver" || still.Status != StatusActive {
		t.Errorf("club was modified by another user: %+v", still)
	}
}

func TestDeleteRemovesTheClub(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	c := addClub(t, svc, owner, ClubInput{Type: "putter", Label: "Putter"})
	if err := svc.Delete(ctx, owner, c.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := svc.Get(ctx, owner, c.ID); statusOfErr(t, err) != http.StatusNotFound {
		t.Errorf("deleted club is still readable")
	}
}

func TestGetMissingClubIsNotFound(t *testing.T) {
	svc, db := newTestService(t)
	owner := createUser(t, db, "owner@example.com")

	_, err := svc.Get(context.Background(), owner, id.New())
	if err == nil {
		t.Fatal("expected an error for a missing club")
	}
	if got := statusOfErr(t, err); got != http.StatusNotFound {
		t.Errorf("status = %d, want %d", got, http.StatusNotFound)
	}
}

func TestValidateClubGuardsTheEnums(t *testing.T) {
	tests := map[string]struct {
		req       clubRequest
		wantField string
	}{
		"valid": {
			req: clubRequest{Type: "wedge", Label: "56° SW", Loft: ptr(56.0), Flex: ptr("stiff")},
		},
		"type is normalized": {
			req: clubRequest{Type: "  Wedge  ", Label: "56° SW"},
		},
		"unknown type": {
			req: clubRequest{Type: "chipper", Label: "Chipper"}, wantField: "club_type",
		},
		"missing label": {
			req: clubRequest{Type: "iron", Label: "   "}, wantField: "label",
		},
		"loft is a yardage typo": {
			req: clubRequest{Type: "iron", Label: "7 iron", Loft: ptr(155.0)}, wantField: "loft",
		},
		"unknown flex": {
			req: clubRequest{Type: "iron", Label: "7 iron", Flex: ptr("bendy")}, wantField: "flex",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			v := httpx.NewValidator()
			in := validateClub(v, tc.req)

			if tc.wantField == "" {
				if err := v.Err(); err != nil {
					t.Fatalf("expected no validation error, got %v", err)
				}
				if in.Type != strings.ToLower(strings.TrimSpace(tc.req.Type)) {
					t.Errorf("type = %q, want it lowercased and trimmed", in.Type)
				}
				return
			}

			err := v.Err()
			if err == nil {
				t.Fatalf("expected a validation error on %q", tc.wantField)
			}
			var apiErr *httpx.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected an *httpx.APIError, got %T", err)
			}
			if _, ok := apiErr.Fields[tc.wantField]; !ok {
				t.Errorf("fields = %v, want a message on %q", apiErr.Fields, tc.wantField)
			}
		})
	}
}

// A blank flex clears the field rather than failing validation, matching how
// the other optional strings behave.
func TestBlankFlexClears(t *testing.T) {
	v := httpx.NewValidator()
	in := validateClub(v, clubRequest{Type: "iron", Label: "7 iron", Flex: ptr("   ")})
	if err := v.Err(); err != nil {
		t.Fatalf("expected no validation error, got %v", err)
	}
	if in.Flex != nil {
		t.Errorf("flex = %q, want nil", *in.Flex)
	}
}

func TestCarryAndDispersionRoundTrip(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, db, "owner@example.com")

	c := addClub(t, svc, owner, ClubInput{
		Type:              "iron",
		Label:             "7 iron",
		ExpectedCarry:     ptr(158),
		AverageDispersion: ptr(12),
	})
	if c.ExpectedCarry == nil || *c.ExpectedCarry != 158 {
		t.Errorf("expected_carry = %v, want 158", c.ExpectedCarry)
	}
	if c.AverageDispersion == nil || *c.AverageDispersion != 12 {
		t.Errorf("average_dispersion = %v, want 12", c.AverageDispersion)
	}

	// Clearing them is how a player says "I no longer know this".
	cleared, err := svc.Update(ctx, owner, c.ID, ClubInput{Type: "iron", Label: "7 iron"})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if cleared.ExpectedCarry != nil || cleared.AverageDispersion != nil {
		t.Errorf("distances = %v/%v, want both cleared",
			cleared.ExpectedCarry, cleared.AverageDispersion)
	}
}

func TestPutterRejectsCarryAndDispersion(t *testing.T) {
	tests := map[string]clubRequest{
		"carry": {Type: "putter", Label: "Putter", ExpectedCarry: ptr(30)},
		"dispersion": {
			Type: "putter", Label: "Putter", AverageDispersion: ptr(3),
		},
	}

	for name, req := range tests {
		t.Run(name, func(t *testing.T) {
			v := httpx.NewValidator()
			validateClub(v, req)

			err := v.Err()
			if err == nil {
				t.Fatalf("a putter with a %s was accepted", name)
			}
			var apiErr *httpx.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected an *httpx.APIError, got %T", err)
			}
			field := "expected_carry"
			if name == "dispersion" {
				field = "average_dispersion"
			}
			if _, ok := apiErr.Fields[field]; !ok {
				t.Errorf("fields = %v, want a message on %q", apiErr.Fields, field)
			}
		})
	}

	// A putter with neither is exactly the normal case.
	v := httpx.NewValidator()
	validateClub(v, clubRequest{Type: "putter", Label: "Putter", Loft: ptr(3.5)})
	if err := v.Err(); err != nil {
		t.Errorf("a plain putter was rejected: %v", err)
	}
}

func TestDistanceBounds(t *testing.T) {
	tests := map[string]struct {
		req       clubRequest
		wantField string
	}{
		"carry at the ceiling":   {req: clubRequest{Type: "driver", Label: "Driver", ExpectedCarry: ptr(maxCarry)}},
		"carry over the ceiling": {req: clubRequest{Type: "driver", Label: "Driver", ExpectedCarry: ptr(maxCarry + 1)}, wantField: "expected_carry"},
		"carry of zero":          {req: clubRequest{Type: "driver", Label: "Driver", ExpectedCarry: ptr(0)}, wantField: "expected_carry"},
		"dispersion of zero":     {req: clubRequest{Type: "iron", Label: "7 iron", AverageDispersion: ptr(0)}},
		"dispersion over":        {req: clubRequest{Type: "iron", Label: "7 iron", AverageDispersion: ptr(maxDispersion + 1)}, wantField: "average_dispersion"},
		"negative dispersion":    {req: clubRequest{Type: "iron", Label: "7 iron", AverageDispersion: ptr(-1)}, wantField: "average_dispersion"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			v := httpx.NewValidator()
			validateClub(v, tc.req)
			err := v.Err()

			if tc.wantField == "" {
				if err != nil {
					t.Fatalf("expected no validation error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected a validation error on %q", tc.wantField)
			}
			var apiErr *httpx.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected an *httpx.APIError, got %T", err)
			}
			if _, ok := apiErr.Fields[tc.wantField]; !ok {
				t.Errorf("fields = %v, want a message on %q", apiErr.Fields, tc.wantField)
			}
		})
	}
}

// Wedge flex is a real shaft designation, so it has to be accepted alongside
// the ladies-to-x-stiff scale.
func TestWedgeFlexIsAccepted(t *testing.T) {
	v := httpx.NewValidator()
	in := validateClub(v, clubRequest{Type: "wedge", Label: "56° SW", Flex: ptr("Wedge")})
	if err := v.Err(); err != nil {
		t.Fatalf("wedge flex was rejected: %v", err)
	}
	if in.Flex == nil || *in.Flex != "wedge" {
		t.Errorf("flex = %v, want normalized to %q", in.Flex, "wedge")
	}
	if !slices.Contains(Flexes, "wedge") {
		t.Error("wedge is missing from the advertised Flexes list")
	}
}

func TestValidateStatusRejectsUnknown(t *testing.T) {
	v := httpx.NewValidator()
	if got := validateStatus(v, "status", "  RETIRED "); got != StatusRetired {
		t.Errorf("status = %q, want %q", got, StatusRetired)
	}
	if err := v.Err(); err != nil {
		t.Fatalf("expected no error for a normalized status, got %v", err)
	}

	v = httpx.NewValidator()
	validateStatus(v, "status", "sold")
	if v.Err() == nil {
		t.Error("expected an error for an unknown status")
	}
}

// mustErr discards a value so table-driven tests can hold mixed-arity calls.
func mustErr[T any](_ T, err error) error { return err }
