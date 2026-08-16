-- name: CreateCourse :exec
INSERT INTO courses (id, name, street, city, region, postal_code, country, phone, website, notes, facility_type, latitude, longitude, pinned, uploaded_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetCourse :one
SELECT * FROM courses WHERE id = ?;

-- The directory's one ordering, shared by the list and the search below.
--
-- The caller's home course comes first, then pinned courses, then the rest by
-- their names. It has to happen here rather than by re-sorting a page in the
-- client: "first" across a paginated directory means first of all of them, and
-- a client sort can only reach the twenty-five rows it was already sent.
--
-- The home course is found by joining users rather than by comparing against a
-- bound id, because sqlc's SQLite parser does not bind parameters inside an
-- ORDER BY at all: it emits the placeholder but leaves it out of the params
-- struct, so the remaining arguments silently shift by one. In a JOIN condition
-- it binds correctly, and the join costs nothing extra: it reads the same
-- home_course_id a separate lookup would have.
--
-- The join matches at most one row and takes no column from it, so the result
-- is still a plain course row. A player with no home course matches nothing,
-- the first ordering term is false throughout, and the order falls through to
-- pinned.
-- name: ListCourses :many
SELECT courses.* FROM courses
LEFT JOIN users ON users.id = sqlc.arg(user_id) AND users.home_course_id = courses.id
ORDER BY (users.id IS NOT NULL) DESC, courses.pinned DESC, courses.name COLLATE NOCASE ASC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);

-- Search uses instr() rather than LIKE so the term is matched literally: a
-- course named "50% Off Golf" is findable, and sqlc's SQLite parser does not
-- support the ESCAPE clause that literal LIKE matching would require.
-- lower() gives ASCII-case-insensitive matching; callers pass a lowered term.
--
-- Every part of the address is searched, not just the street, so that "marne"
-- and "MI" both find the courses there. That is what makes the profile's
-- home-course picker usable: people look for their club by town, not by the
-- street it sits on.
-- name: SearchCourses :many
SELECT courses.* FROM courses
LEFT JOIN users ON users.id = sqlc.arg(user_id) AND users.home_course_id = courses.id
WHERE instr(lower(courses.name), sqlc.arg(query)) > 0
   OR instr(lower(IFNULL(courses.street, '')), sqlc.arg(query)) > 0
   OR instr(lower(IFNULL(courses.city, '')), sqlc.arg(query)) > 0
   OR instr(lower(IFNULL(courses.region, '')), sqlc.arg(query)) > 0
   OR instr(lower(IFNULL(courses.postal_code, '')), sqlc.arg(query)) > 0
   OR instr(lower(IFNULL(courses.country, '')), sqlc.arg(query)) > 0
ORDER BY (users.id IS NOT NULL) DESC, courses.pinned DESC, courses.name COLLATE NOCASE ASC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);

-- name: CountCourses :one
SELECT COUNT(*) FROM courses;

-- name: CountSearchCourses :one
SELECT COUNT(*) FROM courses
WHERE instr(lower(name), sqlc.arg(query)) > 0
   OR instr(lower(IFNULL(street, '')), sqlc.arg(query)) > 0
   OR instr(lower(IFNULL(city, '')), sqlc.arg(query)) > 0
   OR instr(lower(IFNULL(region, '')), sqlc.arg(query)) > 0
   OR instr(lower(IFNULL(postal_code, '')), sqlc.arg(query)) > 0
   OR instr(lower(IFNULL(country, '')), sqlc.arg(query)) > 0;

-- name: UpdateCourse :exec
UPDATE courses SET name = ?, street = ?, city = ?, region = ?, postal_code = ?, country = ?, phone = ?, website = ?, notes = ?, facility_type = ?, latitude = ?, longitude = ?, pinned = ?, updated_at = ? WHERE id = ?;

-- name: TouchCourse :exec
UPDATE courses SET updated_at = ? WHERE id = ?;

-- name: DeleteCourse :exec
DELETE FROM courses WHERE id = ?;

-- Every course this user uploaded, for the account export. Backed by
-- idx_courses_uploaded_by.
-- name: ListCoursesByUploader :many
SELECT * FROM courses
WHERE uploaded_by = ?
ORDER BY name COLLATE NOCASE ASC;
