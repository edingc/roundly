-- name: CreateTee :exec
INSERT INTO tees (id, course_id, name, color, course_rating, slope_rating, total_yardage, display_order)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetTee :one
SELECT * FROM tees WHERE id = ?;

-- name: ListTeesByCourse :many
SELECT * FROM tees WHERE course_id = ? ORDER BY display_order ASC, name COLLATE NOCASE ASC;

-- name: UpdateTee :exec
UPDATE tees
SET name = ?, color = ?, course_rating = ?, slope_rating = ?, total_yardage = ?, display_order = ?
WHERE id = ?;

-- name: DeleteTee :exec
DELETE FROM tees WHERE id = ?;

-- name: MaxTeeDisplayOrder :one
SELECT CAST(IFNULL(MAX(display_order), -1) AS INTEGER) AS max_order FROM tees WHERE course_id = ?;
