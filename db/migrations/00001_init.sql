-- +goose Up
-- +goose StatementBegin

-- The whole schema, as one migration.
--
-- This replaces sixteen incremental migrations that were squashed before the
-- app ever ran anywhere but a laptop. Nothing was deployed, so there was no
-- history worth preserving — only sixteen files describing a schema that could
-- be stated once. Two things were fixed in the squash that the incremental
-- chain could not express cleanly:
--
--   - Columns added by ALTER TABLE sat at the end of their table in arrival
--     order rather than in any order that made sense to read. They are grouped
--     properly here.
--   - `users.updated_at` had `DEFAULT ''`, because SQLite requires a constant
--     default when adding a NOT NULL column. An empty string is not a
--     timestamp, and anything that parsed one would have failed on it.
--
-- After this, incremental migrations resume above the old chain's high-water
-- mark rather than at 00002. A database that ran the original sixteen still
-- records their version numbers, so a migration numbered 2 would be taken for
-- one of them and skipped without a word. See 00017_rounds.sql.
--
-- Conventions throughout:
--
--   - Every id is a TEXT UUIDv7 minted by internal/id, not an autoincrement.
--     Sortable by creation time, safe to generate before the insert, and safe
--     to expose.
--   - Every timestamp is TEXT in RFC 3339 UTC with milliseconds, written by
--     internal/timex. SQLite has no date type, and a single spelling is what
--     makes string comparison a valid ordering.
--   - Booleans are INTEGER 0/1. SQLite has no boolean either.
--   - Money, ratings, and distances that can be fractional are REAL; everything
--     countable is INTEGER.

-- ---------------------------------------------------------------- identity --

CREATE TABLE users (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    -- Null for an account that has only ever signed in with Google. Such an
    -- account can set one later, which is what adds email sign-in as a backup.
    password_hash TEXT,
    display_name TEXT NOT NULL,
    -- Set only by opening a mailed confirmation link. Never backfilled: marking
    -- an account verified without checking would be recording something nobody
    -- checked.
    email_verified INTEGER NOT NULL DEFAULT 0,
    -- Opt-in email two-factor, guarding the password path only. See
    -- internal/auth/twofactor.go for why Google sign-in is exempt.
    two_factor_email INTEGER NOT NULL DEFAULT 0,

    -- Profile. All optional: somebody who wants to be nothing but a display
    -- name should never be made to fill one in.
    first_name TEXT,
    last_name TEXT,
    -- The unguessable stem an avatar is served under, rotated on every upload.
    -- Null when there is no avatar. See internal/auth/avatarurl.go for the
    -- signature that now sits alongside it in the URL.
    avatar_key TEXT,
    home_course_id TEXT REFERENCES courses(id) ON DELETE SET NULL,
    location_city TEXT,
    location_region TEXT,
    location_country TEXT,

    -- A display preference only. Every distance in this database is stored in
    -- yards and converted at the edges.
    distance_unit TEXT NOT NULL DEFAULT 'yards',

    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Unique so two accounts cannot share an avatar URL. Nullable, and SQLite
-- allows any number of NULLs in a unique index, so accounts without a photo do
-- not collide.
CREATE UNIQUE INDEX idx_users_avatar_key ON users(avatar_key);
CREATE INDEX idx_users_home_course_id ON users(home_course_id);

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

CREATE INDEX idx_oauth_accounts_user_id ON oauth_accounts(user_id);

-- Avatars live in the database rather than on disk so that a backup is one
-- file and there is no way for the bytes to drift out of step with the row
-- that points at them.
CREATE TABLE user_avatars (
    user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    image BLOB NOT NULL,
    content_type TEXT NOT NULL,
    byte_size INTEGER NOT NULL,
    updated_at TEXT NOT NULL
);

-- ---------------------------------------------------------------- sessions --

CREATE TABLE refresh_tokens (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- The hash, never the token. Rotated single-use on every refresh; a
    -- presented token that is already revoked is treated as a leak and drops
    -- every session for the user.
    token_hash TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    revoked_at TEXT
);

CREATE INDEX idx_refresh_tokens_token_hash ON refresh_tokens(token_hash);
CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens(user_id);

-- email_challenges is every "we sent you something, send it back" in one table.
--
-- One table rather than two because the row is the same shape and the lifecycle
-- is identical — issue, expire, consume once — and two tables would mean two
-- copies of the expiry sweep, the replay check, and the rate limit. The purpose
-- column is what differs, and it is checked rather than free text so a typo
-- cannot quietly create a third kind of challenge nobody sweeps.
--
-- code_hash, never the code. Verification uses a long random token delivered as
-- a link; two-factor uses six digits typed by hand. Both hash the same way, and
-- only the length differs.
CREATE TABLE email_challenges (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose TEXT NOT NULL,
    -- The address the challenge was sent to, snapshotted. A user who changes
    -- their email mid-flight must not be able to complete a challenge that was
    -- mailed somewhere else, and the users row no longer remembers where that
    -- was.
    email TEXT NOT NULL,
    code_hash TEXT NOT NULL,
    -- Counts wrong guesses. Six digits is a million possibilities, which is a
    -- lot for a human and nothing for a script, so the attempt cap is the real
    -- protection here rather than the entropy.
    attempts INTEGER NOT NULL DEFAULT 0,
    expires_at TEXT NOT NULL,
    -- Set when redeemed. The row is kept rather than deleted so that a replayed
    -- code is refused as used rather than silently treated as never-issued.
    consumed_at TEXT,
    created_at TEXT NOT NULL,
    CHECK (purpose IN ('verify_email', 'login'))
);

CREATE INDEX idx_email_challenges_user ON email_challenges(user_id, purpose, created_at);
CREATE INDEX idx_email_challenges_code_hash ON email_challenges(code_hash);
CREATE INDEX idx_email_challenges_expires_at ON email_challenges(expires_at);

-- trusted_devices is what keeps two-factor from asking on every sign-in.
--
-- Without it the feature is unusable enough that people switch it off, which
-- makes the stricter design the less secure one in practice. Deliberately not a
-- device fingerprint: fingerprinting is unreliable enough that it would demand
-- a code after a browser update, and reliable enough to be worth nobody's
-- privacy.
CREATE TABLE trusted_devices (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    -- Free text for the person reading their own device list later, e.g. the
    -- browser and platform. Never trusted for anything.
    label TEXT,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    last_used_at TEXT
);

CREATE INDEX idx_trusted_devices_user ON trusted_devices(user_id, created_at);
CREATE INDEX idx_trusted_devices_expires_at ON trusted_devices(expires_at);

-- Recovery codes: the way back in when the mailbox is gone.
--
-- Email two-factor has a failure mode nothing else here does. Every other
-- credential is recovered through the address on the account; the second factor
-- *is* that address, so losing it locks the account with no self-service way
-- out and no administrator to appeal to on a self-hosted instance.
--
-- Hashed with argon2id, unlike the sign-in codes above, and the difference is
-- lifetime. A six-digit code lives ten minutes, so a slow hash buys nothing. A
-- recovery code lives until used, which may be years, in a database that may
-- one day leak.
CREATE TABLE recovery_codes (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    created_at TEXT NOT NULL,
    -- Set when spent. The row stays rather than being deleted so that the
    -- profile screen can say how many of the ten are gone, which is what
    -- prompts somebody to generate a fresh set before they run out.
    consumed_at TEXT
);

CREATE INDEX idx_recovery_codes_user ON recovery_codes(user_id, consumed_at);

CREATE TABLE api_keys (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    key_hash TEXT NOT NULL UNIQUE,
    key_prefix TEXT NOT NULL,
    -- Only 'read' exists. The column and its CHECK are here so that adding a
    -- write scope later is a deliberate migration plus a code audit, rather
    -- than something a stray UPDATE could do quietly.
    scope TEXT NOT NULL DEFAULT 'read',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    -- Approximate by design: written at most once per key per flush window, so
    -- that a read-only request never has to take the single writer connection.
    last_used_at TEXT,
    expires_at TEXT,
    revoked_at TEXT,
    CHECK (scope IN ('read'))
);

CREATE INDEX idx_api_keys_user_id ON api_keys(user_id);

-- --------------------------------------------------------- course directory --

-- Courses are shared reference data. Anybody signed in may correct one, and
-- nobody owns one — uploaded_by is attribution and grants no rights.
CREATE TABLE courses (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,

    -- Location in parts rather than one free-text address, because the parts
    -- are what callers ask for: the profile's home-course picker shows
    -- "City, Region, Country" beside a name, and no amount of comma-splitting
    -- makes that reliable from one string. Street stays alongside them because
    -- it is what puts a map pin on the clubhouse instead of the town.
    street TEXT,
    city TEXT,
    region TEXT,
    postal_code TEXT,
    country TEXT,
    -- Filled from the address by internal/geocode when NOMINATIM_URL is set,
    -- and left exactly as typed when it is not. Gaps are filled, never
    -- overwritten.
    latitude REAL,
    longitude REAL,

    phone TEXT,
    website TEXT,
    notes TEXT,
    facility_type TEXT,
    -- Sorts a course to the top of the directory for everybody.
    pinned INTEGER NOT NULL DEFAULT 0,

    -- Nullable, because an uploader can leave. A course with no attribution is
    -- a normal course, not a broken one.
    uploaded_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE INDEX idx_courses_uploaded_by ON courses(uploaded_by);

-- One set of tee boxes on a course.
--
-- Course and slope ratings are tracked separately per gender because that is
-- how they are computed: the same physical markers rate differently against the
-- men's and women's scratch and bogey golfer models, so a scorecard lists both
-- for one tee. The nine-hole ratings are separate from the eighteen-hole one
-- rather than half of it, because that is how USGA ratings work and a nine-hole
-- round posts against the matching nine-hole rating.
--
-- There is deliberately no total_yardage column. The squash dropped one: it was
-- written on every insert and never read, because the total the API returns is
-- summed from the per-hole yardages so that it cannot drift from the grid.
CREATE TABLE tees (
    id TEXT PRIMARY KEY,
    course_id TEXT NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    color TEXT NOT NULL,

    course_rating_men REAL,
    slope_rating_men INTEGER,
    course_rating_women REAL,
    slope_rating_women INTEGER,

    front9_course_rating_men REAL,
    front9_slope_rating_men INTEGER,
    back9_course_rating_men REAL,
    back9_slope_rating_men INTEGER,
    front9_course_rating_women REAL,
    front9_slope_rating_women INTEGER,
    back9_course_rating_women REAL,
    back9_slope_rating_women INTEGER,

    display_order INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_tees_course_id ON tees(course_id);

-- A hole is course-level: its number and stroke index. Par and yardage are
-- per-tee, in hole_tee_details, which is what makes a hole a par 3 from one tee
-- and a par 4 from another.
CREATE TABLE holes (
    id TEXT PRIMARY KEY,
    course_id TEXT NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    hole_number INTEGER NOT NULL CHECK (hole_number BETWEEN 1 AND 18),
    handicap_index INTEGER,
    UNIQUE(course_id, hole_number)
);

CREATE INDEX idx_holes_course_id ON holes(course_id);

CREATE TABLE hole_tee_details (
    id TEXT PRIMARY KEY,
    hole_id TEXT NOT NULL REFERENCES holes(id) ON DELETE CASCADE,
    tee_id TEXT NOT NULL REFERENCES tees(id) ON DELETE CASCADE,
    par INTEGER NOT NULL,
    yardage INTEGER NOT NULL,
    UNIQUE(hole_id, tee_id)
);

CREATE INDEX idx_hole_tee_details_hole_id ON hole_tee_details(hole_id);
CREATE INDEX idx_hole_tee_details_tee_id ON hole_tee_details(tee_id);

-- Removing a course is the one irreversible act on shared data: it cascades
-- away every tee, hole, par, and yardage, and there is no history to restore
-- from. Nobody owns a course, so nobody should be able to do that alone — a
-- player asks, and an administrator decides.
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

CREATE INDEX idx_course_removal_requests_pending
    ON course_removal_requests(resolved_at, created_at);
CREATE INDEX idx_course_removal_requests_course_id
    ON course_removal_requests(course_id);

-- -------------------------------------------------------------------- bag --

CREATE TABLE clubs (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    club_type TEXT NOT NULL,
    label TEXT NOT NULL,
    brand TEXT,
    model TEXT,
    -- Loft in degrees. REAL rather than INTEGER because wedges are sold in half
    -- degrees (e.g. 56.5).
    loft REAL,
    shaft TEXT,
    flex TEXT,
    notes TEXT,

    -- Yards, like everything else stored here.
    expected_carry INTEGER,
    average_dispersion INTEGER,

    -- Where the club sits: in the bag, on the bench, or retired. Retiring
    -- rather than deleting is the right move for a club that has been played,
    -- since rounds and shots will reference club IDs.
    active INTEGER NOT NULL DEFAULT 1,
    retired_at TEXT,
    display_order INTEGER NOT NULL DEFAULT 0,

    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    CHECK (retired_at IS NULL OR active = 0)
);

CREATE INDEX idx_clubs_user_id ON clubs(user_id);
CREATE INDEX idx_clubs_user_active ON clubs(user_id, active, display_order);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Dropped in reverse dependency order. The two mutually-referencing tables,
-- users and courses, are last: SQLite resolves foreign keys at runtime, so the
-- order only has to satisfy the rows that exist.
DROP TABLE IF EXISTS clubs;
DROP TABLE IF EXISTS course_removal_requests;
DROP TABLE IF EXISTS hole_tee_details;
DROP TABLE IF EXISTS holes;
DROP TABLE IF EXISTS tees;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS recovery_codes;
DROP TABLE IF EXISTS trusted_devices;
DROP TABLE IF EXISTS email_challenges;
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS user_avatars;
DROP TABLE IF EXISTS oauth_accounts;
DROP TABLE IF EXISTS courses;
DROP TABLE IF EXISTS users;

-- +goose StatementEnd
