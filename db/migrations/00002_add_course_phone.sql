-- +goose Up
-- +goose StatementBegin
ALTER TABLE courses ADD COLUMN phone TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE courses DROP COLUMN phone;
-- +goose StatementEnd
