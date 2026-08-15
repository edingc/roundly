-- name: CreateUser :exec
INSERT INTO users (id, email, password_hash, display_name, email_verified, created_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetUserByID :one
SELECT * FROM users WHERE id = ?;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = ?;

-- name: SetUserPasswordHash :exec
UPDATE users SET password_hash = ? WHERE id = ?;

-- name: SetUserEmailVerified :exec
UPDATE users SET email_verified = ? WHERE id = ?;

-- name: UpdateUserDisplayName :exec
UPDATE users SET display_name = ? WHERE id = ?;

-- name: UpdateUserDistanceUnit :exec
UPDATE users SET distance_unit = ? WHERE id = ?;
