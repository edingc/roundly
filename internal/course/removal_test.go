package course

import (
	"context"
	"strings"
	"testing"
)

func TestRequestRemovalRecordsAndDeduplicates(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	uploader := createUser(t, db, "uploader@example.com")
	stranger := createUser(t, db, "stranger@example.com")

	detail, err := svc.Create(ctx, uploader, CreateCourseInput{Name: "Doomed GC"})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}

	// Anyone may ask, not only whoever uploaded it.
	request, err := svc.RequestRemoval(ctx, stranger, detail.ID, "Duplicate of Pine Valley")
	if err != nil {
		t.Fatalf("request removal: %v", err)
	}
	if request.CourseName != "Doomed GC" {
		t.Errorf("course_name = %q, want a snapshot of the name", request.CourseName)
	}

	// Asking is not doing: the course is untouched.
	if _, err := svc.Get(ctx, detail.ID); err != nil {
		t.Fatalf("the course was removed by the request itself: %v", err)
	}

	// A second request for the same course is refused rather than queued twice.
	if _, err := svc.RequestRemoval(ctx, uploader, detail.ID, "Also think so"); err == nil {
		t.Error("a duplicate request was accepted")
	} else if status := statusOf(t, err); status != 409 {
		t.Errorf("status = %d, want 409", status)
	}

	pending, err := svc.PendingRemovals(ctx)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(pending))
	}
	if pending[0].RequestedByName == nil || *pending[0].RequestedByName != "stranger@example.com" {
		t.Errorf("requested_by_name = %v, want the requester's display name", pending[0].RequestedByName)
	}
}

func TestResolveRemovalRemovesTheCourseAndKeepsTheRecord(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	uploader := createUser(t, db, "uploader@example.com")

	detail, err := svc.Create(ctx, uploader, CreateCourseInput{
		Name: "Removable GC",
		Tees: []TeeInput{{Name: "Back", Color: "#000000"}},
	})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}
	request, err := svc.RequestRemoval(ctx, uploader, detail.ID, "Closed down")
	if err != nil {
		t.Fatalf("request removal: %v", err)
	}

	if err := svc.ResolveRemoval(ctx, request.ID, ResolutionRemoved); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if _, err := svc.Get(ctx, detail.ID); err == nil {
		t.Error("the course survived a removal")
	} else if status := statusOf(t, err); status != 404 {
		t.Errorf("status = %d, want 404", status)
	}

	// The record outlives the course it was about — cascading it away would
	// destroy the audit trail exactly when it became worth having.
	row, err := db.Queries.GetCourseRemovalRequest(ctx, request.ID)
	if err != nil {
		t.Fatalf("the request was deleted along with the course: %v", err)
	}
	if row.CourseID != nil {
		t.Errorf("course_id = %v, want nil once the course is gone", row.CourseID)
	}
	if row.CourseName != "Removable GC" {
		t.Errorf("course_name = %q, want the snapshot to survive", row.CourseName)
	}
	if row.Resolution == nil || *row.Resolution != ResolutionRemoved {
		t.Errorf("resolution = %v, want %q", row.Resolution, ResolutionRemoved)
	}
}

func TestResolveRemovalDeclineKeepsTheCourse(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	uploader := createUser(t, db, "uploader@example.com")

	detail, err := svc.Create(ctx, uploader, CreateCourseInput{Name: "Keeper GC"})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}
	request, err := svc.RequestRemoval(ctx, uploader, detail.ID, "Mistake")
	if err != nil {
		t.Fatalf("request removal: %v", err)
	}

	if err := svc.ResolveRemoval(ctx, request.ID, ResolutionDeclined); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, err := svc.Get(ctx, detail.ID); err != nil {
		t.Errorf("a declined request removed the course anyway: %v", err)
	}

	// Settled once and once only.
	if err := svc.ResolveRemoval(ctx, request.ID, ResolutionRemoved); err == nil {
		t.Error("an already-resolved request was resolved again")
	} else if status := statusOf(t, err); status != 409 {
		t.Errorf("status = %d, want 409", status)
	}
	if _, err := svc.Get(ctx, detail.ID); err != nil {
		t.Errorf("the second resolution removed the course: %v", err)
	}

	// And a fresh request can now be raised, since nothing is pending.
	if _, err := svc.RequestRemoval(ctx, uploader, detail.ID, "Actually yes"); err != nil {
		t.Errorf("a new request after resolution was refused: %v", err)
	}
}

func TestResolveRemovalRejectsUnknownResolution(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	uploader := createUser(t, db, "uploader@example.com")

	detail, err := svc.Create(ctx, uploader, CreateCourseInput{Name: "Strict GC"})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}
	request, err := svc.RequestRemoval(ctx, uploader, detail.ID, "Reason")
	if err != nil {
		t.Fatalf("request removal: %v", err)
	}

	for _, bad := range []string{"", "deleted", "REMOVED", "yes"} {
		if err := svc.ResolveRemoval(ctx, request.ID, bad); err == nil {
			t.Errorf("resolution %q was accepted", bad)
		} else if status := statusOf(t, err); status != 422 {
			t.Errorf("resolution %q: status = %d, want 422", bad, status)
		}
	}
	if _, err := svc.Get(ctx, detail.ID); err != nil {
		t.Errorf("a rejected resolution removed the course: %v", err)
	}
}

// A request survives the account that raised it, which is what the nullable
// requested_by is for. courses.created_by taught this lesson the hard way.
func TestRemovalRequestSurvivesItsRequester(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	uploader := createUser(t, db, "uploader@example.com")
	requester := createUser(t, db, "leaving@example.com")

	detail, err := svc.Create(ctx, uploader, CreateCourseInput{Name: "Outliving GC"})
	if err != nil {
		t.Fatalf("create course: %v", err)
	}
	request, err := svc.RequestRemoval(ctx, requester, detail.ID, "Wrong yardages throughout")
	if err != nil {
		t.Fatalf("request removal: %v", err)
	}

	if _, err := db.Exec("DELETE FROM users WHERE id = ?", requester); err != nil {
		t.Fatalf("delete the requester: %v", err)
	}

	pending, err := svc.PendingRemovals(ctx)
	if err != nil {
		t.Fatalf("list pending after the requester left: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(pending))
	}
	if pending[0].ID != request.ID {
		t.Errorf("id = %q, want %q", pending[0].ID, request.ID)
	}
	if pending[0].RequestedBy != nil {
		t.Errorf("requested_by = %v, want nil once the requester is gone", pending[0].RequestedBy)
	}
	if !strings.Contains(pending[0].Reason, "Wrong yardages") {
		t.Errorf("reason = %q, want it preserved", pending[0].Reason)
	}
}

func TestRequestRemovalValidates(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	uploader := createUser(t, db, "uploader@example.com")

	if _, err := svc.RequestRemoval(ctx, uploader, "no-such-course", "Reason"); err == nil {
		t.Error("a request was accepted for a course that does not exist")
	} else if status := statusOf(t, err); status != 404 {
		t.Errorf("status = %d, want 404", status)
	}
}
