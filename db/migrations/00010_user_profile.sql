-- +goose Up
-- +goose StatementBegin
-- The player's real name, kept separate from display_name. Both optional: a
-- user who only ever wants to be "Cody" should not have to fill in a surname.
ALTER TABLE users ADD COLUMN first_name TEXT;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE users ADD COLUMN last_name TEXT;
-- +goose StatementEnd

-- +goose StatementBegin
-- The unguessable stem the avatar is served under: /api/avatars/<key>.jpg.
--
-- It lives on users rather than on user_avatars so that toUser, which already
-- reads this row on nearly every authenticated request, can build the URL
-- without a second query. The image bytes deliberately do not (see below).
--
-- Rotating the key on every upload is what lets the served URL be cached
-- forever: the bytes at a given URL genuinely never change, and a replaced
-- avatar is unreachable from a previously shared link.
ALTER TABLE users ADD COLUMN avatar_key TEXT;
-- +goose StatementEnd

-- +goose StatementBegin
-- ON DELETE SET NULL, not CASCADE: deleting a course must not delete the
-- players who called it home. SQLite allows a REFERENCES clause on ADD COLUMN
-- only when the default is NULL, which it is.
ALTER TABLE users ADD COLUMN home_course_id TEXT REFERENCES courses(id) ON DELETE SET NULL;
-- +goose StatementEnd

-- +goose StatementBegin
-- Free text rather than a normalized place table. A golf app has no reason to
-- know that Springfield is in Missouri, and every structured alternative either
-- ships a country list that goes stale or refuses an address someone lives at.
ALTER TABLE users ADD COLUMN location_city TEXT;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE users ADD COLUMN location_region TEXT;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE users ADD COLUMN location_country TEXT;
-- +goose StatementEnd

-- +goose StatementBegin
-- users is the last table without an updated_at, and a profile page is what
-- makes one worth having.
--
-- This cannot default to strftime() the way clubs.updated_at does in 00007:
-- SQLite rejects a non-constant DEFAULT on ADD COLUMN, allowing it only inside
-- CREATE TABLE. So it lands with a constant default and is backfilled below,
-- and every application write sets it explicitly from here on.
ALTER TABLE users ADD COLUMN updated_at TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE users SET updated_at = created_at WHERE updated_at = '';
-- +goose StatementEnd

-- +goose StatementBegin
-- The avatar image, in its own table so that SELECT * FROM users — which is
-- what every user query in this codebase does — never drags ~25KB of JPEG
-- through a session check, a course list, or a bag load.
--
-- The bytes live in SQLite rather than on disk because at this size that is
-- both faster and simpler: a 256x256 JPEG is well under the ~100KB threshold
-- where the filesystem starts to win, and keeping it here means the database
-- file remains a complete backup and the server keeps its single-file
-- deployment story. BLOB maps to BYTEA in the Phase 8 Postgres migration.
CREATE TABLE user_avatars (
    user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    image BLOB NOT NULL,
    content_type TEXT NOT NULL,
    byte_size INTEGER NOT NULL,
    updated_at TEXT NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
-- NULLs are distinct in a SQLite unique index, so this constrains only the
-- users who actually have an avatar. It also backs the serve path's lookup.
CREATE UNIQUE INDEX idx_users_avatar_key ON users(avatar_key);
-- +goose StatementEnd
-- +goose StatementBegin
-- Without this, every course delete full-scans users to apply SET NULL.
CREATE INDEX idx_users_home_course_id ON users(home_course_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_users_home_course_id;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_users_avatar_key;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE user_avatars;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN updated_at;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN location_country;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN location_region;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN location_city;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN home_course_id;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN avatar_key;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN last_name;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN first_name;
-- +goose StatementEnd
