-- name: CreateCourse :exec
INSERT INTO courses (id, name, address, phone, website, created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetCourse :one
SELECT * FROM courses WHERE id = ?;

-- name: ListCourses :many
SELECT * FROM courses
ORDER BY name COLLATE NOCASE ASC
LIMIT ? OFFSET ?;

-- Search uses instr() rather than LIKE so the term is matched literally: a
-- course named "50% Off Golf" is findable, and sqlc's SQLite parser does not
-- support the ESCAPE clause that literal LIKE matching would require.
-- lower() gives ASCII-case-insensitive matching; callers pass a lowered term.
-- name: SearchCourses :many
SELECT * FROM courses
WHERE instr(lower(name), sqlc.arg(query)) > 0
   OR instr(lower(IFNULL(address, '')), sqlc.arg(query)) > 0
ORDER BY name COLLATE NOCASE ASC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);

-- name: CountCourses :one
SELECT COUNT(*) FROM courses;

-- name: CountSearchCourses :one
SELECT COUNT(*) FROM courses
WHERE instr(lower(name), sqlc.arg(query)) > 0
   OR instr(lower(IFNULL(address, '')), sqlc.arg(query)) > 0;

-- name: UpdateCourse :exec
UPDATE courses SET name = ?, address = ?, phone = ?, website = ?, updated_at = ? WHERE id = ?;

-- name: TouchCourse :exec
UPDATE courses SET updated_at = ? WHERE id = ?;

-- name: DeleteCourse :exec
DELETE FROM courses WHERE id = ?;
