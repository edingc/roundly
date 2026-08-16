-- name: CreateCourseRemovalRequest :exec
INSERT INTO course_removal_requests (id, course_id, course_name, requested_by, reason, created_at)
VALUES (?, ?, ?, ?, ?, ?);

-- The administrator's queue. Joined out to the requester's display name so the
-- page does not have to resolve each one, and left-joined because a requester
-- may since have deleted their account.
-- name: ListPendingCourseRemovalRequests :many
SELECT r.*, u.display_name AS requested_by_name
FROM course_removal_requests r
LEFT JOIN users u ON u.id = r.requested_by
WHERE r.resolved_at IS NULL
ORDER BY r.created_at ASC;

-- name: GetCourseRemovalRequest :one
SELECT * FROM course_removal_requests WHERE id = ?;

-- name: ResolveCourseRemovalRequest :exec
UPDATE course_removal_requests
SET resolved_at = ?, resolution = ?
WHERE id = ? AND resolved_at IS NULL;

-- Guards against a second request for a course that already has one waiting.
-- name: CountPendingRemovalRequestsForCourse :one
SELECT COUNT(*) FROM course_removal_requests
WHERE course_id = ? AND resolved_at IS NULL;
