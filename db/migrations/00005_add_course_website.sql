-- +goose Up
-- +goose StatementBegin
ALTER TABLE courses ADD COLUMN website TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE courses DROP COLUMN website;
-- +goose StatementEnd
