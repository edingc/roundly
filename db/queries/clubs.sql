-- name: CreateClub :exec
INSERT INTO clubs (
    id, user_id, club_type, label, brand, model, loft, shaft, flex, notes,
    expected_carry, average_dispersion,
    active, retired_at, display_order, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetClub :one
SELECT * FROM clubs WHERE id = ?;

-- name: ListClubsByUser :many
SELECT * FROM clubs
WHERE user_id = ?
ORDER BY display_order ASC, created_at ASC;

-- name: UpdateClub :exec
UPDATE clubs
SET club_type = ?, label = ?, brand = ?, model = ?, loft = ?, shaft = ?,
    flex = ?, notes = ?, expected_carry = ?, average_dispersion = ?,
    display_order = ?, updated_at = ?
WHERE id = ?;

-- name: SetClubStatus :exec
UPDATE clubs
SET active = ?, retired_at = ?, updated_at = ?
WHERE id = ?;

-- name: DeleteClub :exec
DELETE FROM clubs WHERE id = ?;

-- name: MaxClubDisplayOrder :one
SELECT CAST(IFNULL(MAX(display_order), -1) AS INTEGER) AS max_order
FROM clubs WHERE user_id = ?;

-- name: CountActiveClubs :one
SELECT COUNT(*) FROM clubs WHERE user_id = ? AND active = 1;
