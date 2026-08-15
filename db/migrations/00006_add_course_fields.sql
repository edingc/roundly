-- +goose Up
-- +goose StatementBegin
ALTER TABLE courses ADD COLUMN notes TEXT;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE courses ADD COLUMN facility_type TEXT;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE courses ADD COLUMN latitude REAL;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE courses ADD COLUMN longitude REAL;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE courses ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE courses DROP COLUMN pinned;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE courses DROP COLUMN longitude;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE courses DROP COLUMN latitude;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE courses DROP COLUMN facility_type;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE courses DROP COLUMN notes;
-- +goose StatementEnd
