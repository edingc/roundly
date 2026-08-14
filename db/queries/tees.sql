-- name: CreateTee :exec
INSERT INTO tees (
    id, course_id, name, color,
    course_rating_men, slope_rating_men, course_rating_women, slope_rating_women,
    front9_course_rating_men, front9_slope_rating_men, back9_course_rating_men, back9_slope_rating_men,
    front9_course_rating_women, front9_slope_rating_women, back9_course_rating_women, back9_slope_rating_women,
    total_yardage, display_order
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetTee :one
SELECT * FROM tees WHERE id = ?;

-- name: ListTeesByCourse :many
SELECT * FROM tees WHERE course_id = ? ORDER BY display_order ASC, name COLLATE NOCASE ASC;

-- name: UpdateTee :exec
UPDATE tees
SET name = ?, color = ?,
    course_rating_men = ?, slope_rating_men = ?, course_rating_women = ?, slope_rating_women = ?,
    front9_course_rating_men = ?, front9_slope_rating_men = ?, back9_course_rating_men = ?, back9_slope_rating_men = ?,
    front9_course_rating_women = ?, front9_slope_rating_women = ?, back9_course_rating_women = ?, back9_slope_rating_women = ?,
    total_yardage = ?, display_order = ?
WHERE id = ?;

-- name: DeleteTee :exec
DELETE FROM tees WHERE id = ?;

-- name: GetTeeByName :one
SELECT * FROM tees WHERE course_id = ? AND name = ?;

-- name: MaxTeeDisplayOrder :one
SELECT CAST(IFNULL(MAX(display_order), -1) AS INTEGER) AS max_order FROM tees WHERE course_id = ?;
