-- +goose Up
-- +goose StatementBegin
ALTER TABLE tees RENAME COLUMN course_rating TO course_rating_men;
ALTER TABLE tees RENAME COLUMN slope_rating TO slope_rating_men;
ALTER TABLE tees ADD COLUMN course_rating_women REAL;
ALTER TABLE tees ADD COLUMN slope_rating_women INTEGER;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE tees DROP COLUMN course_rating_women;
ALTER TABLE tees DROP COLUMN slope_rating_women;
ALTER TABLE tees RENAME COLUMN course_rating_men TO course_rating;
ALTER TABLE tees RENAME COLUMN slope_rating_men TO slope_rating;
-- +goose StatementEnd
