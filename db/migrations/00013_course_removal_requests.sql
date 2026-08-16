-- +goose Up
-- +goose StatementBegin

-- Removing a course is the one irreversible act on shared data: it cascades
-- away every tee, hole, par, and yardage, and there is no history to restore
-- from. Now that nobody owns a course, nobody should be able to do that on
-- their own — so a player asks, and an administrator decides.
--
-- Both foreign keys are nullable rather than NOT NULL, which is the lesson from
-- courses.created_by: a reference that cannot be released is a reference that
-- blocks deletion later.
--
-- course_id goes null rather than cascading, and course_name holds a snapshot,
-- so that resolving a request by actually removing the course leaves the record
-- behind saying what was removed. Cascading here would delete the audit trail
-- at exactly the moment it became worth having.
CREATE TABLE course_removal_requests (
    id TEXT PRIMARY KEY,
    course_id TEXT REFERENCES courses(id) ON DELETE SET NULL,
    course_name TEXT NOT NULL,
    requested_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    reason TEXT NOT NULL,
    created_at TEXT NOT NULL,
    resolved_at TEXT,
    resolution TEXT,
    CHECK (resolution IS NULL OR resolution IN ('removed', 'declined')),
    -- A resolution and its timestamp arrive together or not at all.
    CHECK ((resolved_at IS NULL) = (resolution IS NULL))
);

-- The administrator's queue is "everything unresolved, oldest first".
CREATE INDEX idx_course_removal_requests_pending
    ON course_removal_requests(resolved_at, created_at);

CREATE INDEX idx_course_removal_requests_course_id
    ON course_removal_requests(course_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_course_removal_requests_course_id;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_course_removal_requests_pending;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE course_removal_requests;
-- +goose StatementEnd
