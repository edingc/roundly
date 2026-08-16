package course

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/edingc/roundly/internal/auth"
	"github.com/edingc/roundly/internal/database/sqlc"
	"github.com/edingc/roundly/internal/httpx"
	"github.com/edingc/roundly/internal/id"
	"github.com/edingc/roundly/internal/timex"
)

// Resolutions an administrator can record.
const (
	ResolutionRemoved  = "removed"
	ResolutionDeclined = "declined"
)

const maxRemovalReasonLen = 500

// RemovalRequest is a player's request to take a course out of the directory.
type RemovalRequest struct {
	ID string `json:"id"`
	// CourseID is null once the course has actually been removed. CourseName is
	// a snapshot taken at request time, so the record still says what it was
	// about after the course is gone.
	CourseID        *string `json:"course_id"`
	CourseName      string  `json:"course_name"`
	RequestedBy     *string `json:"requested_by"`
	RequestedByName *string `json:"requested_by_name"`
	Reason          string  `json:"reason"`
	CreatedAt       string  `json:"created_at"`
	ResolvedAt      *string `json:"resolved_at"`
	Resolution      *string `json:"resolution"`
}

// RequestRemoval records a request to remove a course.
//
// Any signed-in player may ask; nobody may act on it themselves. Deleting a
// course cascades away its tees, holes, and every par and yardage, with no
// history to restore from, so it is the one thing about shared course data that
// is not self-service.
func (s *Service) RequestRemoval(ctx context.Context, requesterID, courseID, reason string) (*RemovalRequest, error) {
	row, err := s.loadCourse(ctx, courseID)
	if err != nil {
		return nil, err
	}

	pending, err := s.db.Queries.CountPendingRemovalRequestsForCourse(ctx, &courseID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("count pending removal requests: %w", err))
	}
	if pending > 0 {
		return nil, httpx.Conflict("Someone has already asked for this course to be removed.")
	}

	requestID := id.New()
	now := timex.Now()
	if err := s.db.Queries.CreateCourseRemovalRequest(ctx, sqlc.CreateCourseRemovalRequestParams{
		ID:          requestID,
		CourseID:    &courseID,
		CourseName:  row.Name,
		RequestedBy: &requesterID,
		Reason:      reason,
		CreatedAt:   now,
	}); err != nil {
		return nil, httpx.Internal(fmt.Errorf("create removal request: %w", err))
	}

	// Logged as well as queued: on a self-hosted instance the person reading
	// the server's output is the administrator.
	slog.Warn("course removal requested",
		"request_id", requestID,
		"course_id", courseID,
		"course_name", row.Name,
		"requested_by", requesterID)

	return &RemovalRequest{
		ID:          requestID,
		CourseID:    &courseID,
		CourseName:  row.Name,
		RequestedBy: &requesterID,
		Reason:      reason,
		CreatedAt:   now,
	}, nil
}

// PendingRemovals lists everything waiting on the administrator.
func (s *Service) PendingRemovals(ctx context.Context) ([]RemovalRequest, error) {
	rows, err := s.db.Queries.ListPendingCourseRemovalRequests(ctx)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list removal requests: %w", err))
	}
	out := make([]RemovalRequest, 0, len(rows))
	for _, r := range rows {
		out = append(out, RemovalRequest{
			ID:              r.ID,
			CourseID:        r.CourseID,
			CourseName:      r.CourseName,
			RequestedBy:     r.RequestedBy,
			RequestedByName: r.RequestedByName,
			Reason:          r.Reason,
			CreatedAt:       r.CreatedAt,
			ResolvedAt:      r.ResolvedAt,
			Resolution:      r.Resolution,
		})
	}
	return out, nil
}

// ResolveRemoval settles a request, deleting the course if that is the answer.
//
// The two writes are one transaction so a course is never removed without the
// record saying so. Deleting the course nulls the request's course_id rather
// than cascading the request away, which is what leaves an audit trail behind.
func (s *Service) ResolveRemoval(ctx context.Context, requestID, resolution string) error {
	if resolution != ResolutionRemoved && resolution != ResolutionDeclined {
		return httpx.ValidationError(map[string]string{
			"resolution": "Choose either removed or declined.",
		})
	}

	row, err := s.db.Queries.GetCourseRemovalRequest(ctx, requestID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return httpx.NotFound("That request does not exist.")
		}
		return httpx.Internal(fmt.Errorf("load removal request: %w", err))
	}
	if row.ResolvedAt != nil {
		return httpx.Conflict("That request has already been resolved.")
	}

	now := timex.Now()
	if err := s.db.InTx(func(q *sqlc.Queries) error {
		if err := q.ResolveCourseRemovalRequest(ctx, sqlc.ResolveCourseRemovalRequestParams{
			ResolvedAt: &now,
			Resolution: &resolution,
			ID:         requestID,
		}); err != nil {
			return fmt.Errorf("resolve removal request: %w", err)
		}
		if resolution == ResolutionRemoved && row.CourseID != nil {
			if err := q.DeleteCourse(ctx, *row.CourseID); err != nil {
				return fmt.Errorf("delete course: %w", err)
			}
		}
		return nil
	}); err != nil {
		return httpx.Internal(err)
	}

	slog.Warn("course removal resolved",
		"request_id", requestID,
		"course_name", row.CourseName,
		"resolution", resolution)
	return nil
}

// ---- handlers ----

type removalRequestBody struct {
	Reason string `json:"reason"`
}

func (h *Handler) requestRemoval(w http.ResponseWriter, r *http.Request) {
	var req removalRequestBody
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	v := httpx.NewValidator()
	reason := v.SingleLine("reason", req.Reason)
	if v.Required("reason", reason) {
		v.MaxLen("reason", reason, maxRemovalReasonLen)
	}
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	request, err := h.service.RequestRemoval(ctx, auth.MustUserID(ctx), chi.URLParam(r, "courseID"), reason)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, request)
}

func (h *Handler) listRemovalRequests(w http.ResponseWriter, r *http.Request) {
	requests, err := h.service.PendingRemovals(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"requests": requests})
}

type resolveBody struct {
	Resolution string `json:"resolution"`
}

func (h *Handler) resolveRemovalRequest(w http.ResponseWriter, r *http.Request) {
	var req resolveBody
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	err := h.service.ResolveRemoval(r.Context(), chi.URLParam(r, "requestID"), strings.ToLower(strings.TrimSpace(req.Resolution)))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}
