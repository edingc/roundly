-- +goose NO TRANSACTION

-- +goose Up
-- +goose StatementBegin
-- Courses stop having an owner.
--
-- created_by was doing two jobs: recording who typed the course in, and
-- deciding who was allowed to change it. That second job was wrong. A golf
-- course is objective real-world data — its yardages are not anyone's property
-- — and tying edit rights to whoever entered it first meant a wrong number
-- could only be corrected by one person, who may never come back. It also made
-- an account undeletable, because this is the only reference to users(id)
-- without an ON DELETE clause.
--
-- So the column is renamed to uploaded_by, made nullable, and given
-- ON DELETE SET NULL. It is now attribution and nothing else: authorization
-- lives nowhere, because nobody owns a course.
--
-- This is the first table rebuild in the project. SQLite cannot alter a
-- constraint at all — not the NOT NULL, not the missing ON DELETE — so the
-- documented 12-step procedure is the only way, and it needs foreign_keys off
-- for the duration. goose cannot toggle that pragma inside a transaction,
-- which is why this file is NO TRANSACTION.
PRAGMA foreign_keys=OFF;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE courses_new (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    address TEXT,
    -- Nullable, because an uploader can leave. A course with no attribution is
    -- a normal course, not a broken one.
    uploaded_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    phone TEXT,
    website TEXT,
    notes TEXT,
    facility_type TEXT,
    latitude REAL,
    longitude REAL,
    pinned INTEGER NOT NULL DEFAULT 0
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO courses_new (id, name, address, uploaded_by, created_at, updated_at,
                         phone, website, notes, facility_type, latitude, longitude, pinned)
SELECT id, name, address, created_by, created_at, updated_at,
       phone, website, notes, facility_type, latitude, longitude, pinned
FROM courses;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE courses;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE courses_new RENAME TO courses;
-- +goose StatementEnd

-- +goose StatementBegin
-- Dropping the old table took idx_courses_created_by with it.
CREATE INDEX idx_courses_uploaded_by ON courses(uploaded_by);
-- +goose StatementEnd

-- +goose StatementBegin
-- The child tables reference courses(id) and were pointed at the table that was
-- just dropped and recreated. SQLite reattaches them by name, but nothing has
-- verified that until this runs.
PRAGMA foreign_key_check;
-- +goose StatementEnd

-- +goose StatementBegin
PRAGMA foreign_keys=ON;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
PRAGMA foreign_keys=OFF;
-- +goose StatementEnd

-- +goose StatementBegin
-- Reversing this cannot be lossless: rows whose uploader has since been deleted
-- have no owner to restore, and the old column was NOT NULL. They are given the
-- oldest surviving account, which is the least-wrong answer available.
CREATE TABLE courses_old (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    address TEXT,
    created_by TEXT NOT NULL REFERENCES users(id),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    phone TEXT,
    website TEXT,
    notes TEXT,
    facility_type TEXT,
    latitude REAL,
    longitude REAL,
    pinned INTEGER NOT NULL DEFAULT 0
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO courses_old (id, name, address, created_by, created_at, updated_at,
                         phone, website, notes, facility_type, latitude, longitude, pinned)
SELECT id, name, address,
       COALESCE(uploaded_by, (SELECT id FROM users ORDER BY created_at ASC LIMIT 1)),
       created_at, updated_at,
       phone, website, notes, facility_type, latitude, longitude, pinned
FROM courses;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE courses;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE courses_old RENAME TO courses;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_courses_created_by ON courses(created_by);
-- +goose StatementEnd

-- +goose StatementBegin
PRAGMA foreign_keys=ON;
-- +goose StatementEnd
