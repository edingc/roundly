-- +goose Up
-- +goose StatementBegin
-- Expected carry in yards: how far the player expects to fly this club. Phase 4
-- compares recorded shots against it rather than replacing it, so it stays a
-- player-set baseline that is useful before any round has been played.
ALTER TABLE clubs ADD COLUMN expected_carry INTEGER;
-- +goose StatementEnd
-- +goose StatementBegin
-- Average dispersion in yards: the typical spread around that carry. Unlike
-- carry it is not something a player can eyeball, which is exactly why it is
-- worth recording.
ALTER TABLE clubs ADD COLUMN average_dispersion INTEGER;
-- +goose StatementEnd

-- Neither applies to a putter. That rule lives in internal/club rather than in a
-- CHECK: SQLite cannot add a table-level constraint through ALTER TABLE, and
-- rebuilding the table for a domain rule is a poor trade against the
-- retired/active CHECK, which guards an actual data-integrity invariant.

-- +goose Down
-- +goose StatementBegin
ALTER TABLE clubs DROP COLUMN average_dispersion;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE clubs DROP COLUMN expected_carry;
-- +goose StatementEnd
