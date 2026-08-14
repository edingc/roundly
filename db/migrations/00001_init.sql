-- +goose Up
-- +goose StatementBegin

-- users
-- password_hash is nullable: OAuth-only users have no local password.
CREATE TABLE users (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT,
    display_name TEXT NOT NULL,
    email_verified INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- oauth_accounts links a user to one or more external identity providers.
-- Separate table so a user can hold both a password and a Google login, and so
-- adding GitHub/Microsoft later needs no schema change.
CREATE TABLE oauth_accounts (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    provider_subject TEXT NOT NULL,
    provider_email TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE(provider, provider_subject)
);

CREATE TABLE refresh_tokens (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    revoked_at TEXT
);

CREATE TABLE courses (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    address TEXT,
    created_by TEXT NOT NULL REFERENCES users(id),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE tees (
    id TEXT PRIMARY KEY,
    course_id TEXT NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    color TEXT NOT NULL,
    course_rating REAL,
    slope_rating INTEGER,
    total_yardage INTEGER,
    display_order INTEGER NOT NULL DEFAULT 0
);

-- holes are course-level: hole number plus stroke index, not tee-specific.
CREATE TABLE holes (
    id TEXT PRIMARY KEY,
    course_id TEXT NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    hole_number INTEGER NOT NULL CHECK (hole_number BETWEEN 1 AND 18),
    handicap_index INTEGER,
    UNIQUE(course_id, hole_number)
);

-- par and yardage vary by tee, which is what solves the par-3/par-4 case.
CREATE TABLE hole_tee_details (
    id TEXT PRIMARY KEY,
    hole_id TEXT NOT NULL REFERENCES holes(id) ON DELETE CASCADE,
    tee_id TEXT NOT NULL REFERENCES tees(id) ON DELETE CASCADE,
    par INTEGER NOT NULL,
    yardage INTEGER NOT NULL,
    UNIQUE(hole_id, tee_id)
);

CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens(user_id);
CREATE INDEX idx_refresh_tokens_token_hash ON refresh_tokens(token_hash);
CREATE INDEX idx_oauth_accounts_user_id ON oauth_accounts(user_id);
CREATE INDEX idx_courses_created_by ON courses(created_by);
CREATE INDEX idx_tees_course_id ON tees(course_id);
CREATE INDEX idx_holes_course_id ON holes(course_id);
CREATE INDEX idx_hole_tee_details_hole_id ON hole_tee_details(hole_id);
CREATE INDEX idx_hole_tee_details_tee_id ON hole_tee_details(tee_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE hole_tee_details;
DROP TABLE holes;
DROP TABLE tees;
DROP TABLE courses;
DROP TABLE refresh_tokens;
DROP TABLE oauth_accounts;
DROP TABLE users;
-- +goose StatementEnd
