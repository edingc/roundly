-- name: CreateHole :exec
INSERT INTO holes (id, course_id, hole_number, handicap_index)
VALUES (?, ?, ?, ?);

-- name: GetHole :one
SELECT * FROM holes WHERE id = ?;

-- name: GetHoleByNumber :one
SELECT * FROM holes WHERE course_id = ? AND hole_number = ?;

-- name: ListHolesByCourse :many
SELECT * FROM holes WHERE course_id = ? ORDER BY hole_number ASC;

-- name: UpdateHole :exec
UPDATE holes SET handicap_index = ? WHERE id = ?;

-- name: DeleteHole :exec
DELETE FROM holes WHERE id = ?;

-- name: UpsertHoleTeeDetail :exec
INSERT INTO hole_tee_details (id, hole_id, tee_id, par, yardage)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(hole_id, tee_id) DO UPDATE SET par = excluded.par, yardage = excluded.yardage;

-- name: GetHoleTeeDetail :one
SELECT * FROM hole_tee_details WHERE hole_id = ? AND tee_id = ?;

-- name: ListHoleTeeDetailsByHole :many
SELECT * FROM hole_tee_details WHERE hole_id = ?;

-- name: ListHoleTeeDetailsByCourse :many
SELECT d.* FROM hole_tee_details d
JOIN holes h ON h.id = d.hole_id
WHERE h.course_id = ?
ORDER BY h.hole_number ASC;

-- name: DeleteHoleTeeDetail :exec
DELETE FROM hole_tee_details WHERE hole_id = ? AND tee_id = ?;

-- name: SumTeeYardage :one
SELECT CAST(IFNULL(SUM(yardage), 0) AS INTEGER) AS total FROM hole_tee_details WHERE tee_id = ?;
