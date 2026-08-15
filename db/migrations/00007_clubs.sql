-- +goose Up
-- +goose StatementBegin

-- clubs is the player's golf bag: every club they own, whether or not it is
-- currently in play.
--
-- Unlike courses, which are a shared directory, a bag is personal — every query
-- is scoped by user_id and there is no cross-user read.
--
-- A club has three states, which matter because Phase 3 rounds and Phase 4 shot
-- data will reference club ids:
--
--   active = 1, retired_at IS NULL   in the bag right now
--   active = 0, retired_at IS NULL   owned but benched, swappable back in
--   active = 0, retired_at NOT NULL  retired (sold, replaced, broken)
--
-- Retirement is a soft delete: the row and its id survive so historical shots
-- still resolve to the club that hit them. The CHECK enforces that a retired
-- club can never also be active, which is the one combination that would make
-- the active set meaningless.
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
    active INTEGER NOT NULL DEFAULT 1,
    retired_at TEXT,
    display_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    CHECK (retired_at IS NULL OR active = 0)
);

CREATE INDEX idx_clubs_user_id ON clubs(user_id);

-- The bag screen reads the active set on its own, and this is the ordering it
-- reads it in.
CREATE INDEX idx_clubs_user_active ON clubs(user_id, active, display_order);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_clubs_user_active;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_clubs_user_id;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE clubs;
-- +goose StatementEnd
