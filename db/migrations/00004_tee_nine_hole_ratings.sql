-- +goose Up
-- +goose StatementBegin
ALTER TABLE tees ADD COLUMN front9_course_rating_men REAL;
ALTER TABLE tees ADD COLUMN front9_slope_rating_men INTEGER;
ALTER TABLE tees ADD COLUMN back9_course_rating_men REAL;
ALTER TABLE tees ADD COLUMN back9_slope_rating_men INTEGER;
ALTER TABLE tees ADD COLUMN front9_course_rating_women REAL;
ALTER TABLE tees ADD COLUMN front9_slope_rating_women INTEGER;
ALTER TABLE tees ADD COLUMN back9_course_rating_women REAL;
ALTER TABLE tees ADD COLUMN back9_slope_rating_women INTEGER;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE tees DROP COLUMN front9_course_rating_men;
ALTER TABLE tees DROP COLUMN front9_slope_rating_men;
ALTER TABLE tees DROP COLUMN back9_course_rating_men;
ALTER TABLE tees DROP COLUMN back9_slope_rating_men;
ALTER TABLE tees DROP COLUMN front9_course_rating_women;
ALTER TABLE tees DROP COLUMN front9_slope_rating_women;
ALTER TABLE tees DROP COLUMN back9_course_rating_women;
ALTER TABLE tees DROP COLUMN back9_slope_rating_women;
-- +goose StatementEnd
