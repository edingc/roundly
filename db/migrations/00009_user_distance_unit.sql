-- +goose Up
-- +goose StatementBegin
-- Which unit this user reads and enters distances in: hole yardages, tee
-- totals, and club carry and dispersion.
--
-- This is a display preference only. Every distance in the database stays in
-- yards, which is what all existing rows already hold; conversion happens at
-- the input and display boundary. Storing per-value units instead would let one
-- bag hold yards and metres at once, and would force every read site to convert
-- anyway.
ALTER TABLE users ADD COLUMN distance_unit TEXT NOT NULL DEFAULT 'yards';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN distance_unit;
-- +goose StatementEnd
