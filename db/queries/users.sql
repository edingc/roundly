-- name: CreateUser :exec
INSERT INTO users (id, email, password_hash, display_name, email_verified, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: GetUserByID :one
SELECT * FROM users WHERE id = ?;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = ?;

-- name: SetUserPasswordHash :exec
UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?;

-- name: SetUserEmailVerified :exec
UPDATE users SET email_verified = ?, updated_at = ? WHERE id = ?;

-- name: UpdateUserDisplayName :exec
UPDATE users SET display_name = ?, updated_at = ? WHERE id = ?;

-- name: UpdateUserDistanceUnit :exec
UPDATE users SET distance_unit = ?, updated_at = ? WHERE id = ?;

-- name: UpdateUserProfile :exec
UPDATE users
SET first_name = ?,
    last_name = ?,
    display_name = ?,
    home_course_id = ?,
    location_city = ?,
    location_region = ?,
    location_country = ?,
    updated_at = ?
WHERE id = ?;

-- Gender is a preference, not a profile field, so it has its own statement:
-- saving a name must not be able to disturb which ratings a round records.
-- name: SetUserGender :exec
UPDATE users SET gender = ?, updated_at = ? WHERE id = ?;

-- Changing an address always drops verification: the new one has not been
-- proven, and carrying the old flag over would mark an unproven address
-- verified.
-- Clearing email_verified is part of the same statement rather than a separate
-- call, so there is no window in which the row names a new address and still
-- claims the old one was confirmed. Callers only have to send the new link.
-- name: UpdateUserEmail :exec
UPDATE users SET email = ?, email_verified = 0, updated_at = ? WHERE id = ?;

-- name: SetUserAvatarKey :exec
UPDATE users SET avatar_key = ?, updated_at = ? WHERE id = ?;

-- name: UpsertUserAvatar :exec
INSERT INTO user_avatars (user_id, image, content_type, byte_size, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(user_id) DO UPDATE SET
    image = excluded.image,
    content_type = excluded.content_type,
    byte_size = excluded.byte_size,
    updated_at = excluded.updated_at;

-- name: DeleteUserAvatar :exec
DELETE FROM user_avatars WHERE user_id = ?;

-- The whole avatar serve path, in one indexed lookup. Joined rather than keyed
-- directly on the image row so that the unguessable key stays in one place.
-- name: GetAvatarByKey :one
SELECT a.image, a.content_type, a.updated_at
FROM user_avatars a
JOIN users u ON u.id = a.user_id
WHERE u.avatar_key = ?;

-- name: GetAvatarByUser :one
SELECT * FROM user_avatars WHERE user_id = ?;

-- Deleting a user relies entirely on the schema: clubs, API keys, OAuth links,
-- refresh tokens, and the avatar cascade away, and course attribution nulls
-- itself. courses.created_by was the one reference that made this impossible,
-- and migration 00012 released it.
-- name: DeleteUser :exec
DELETE FROM users WHERE id = ?;

-- name: SetUserTwoFactorEmail :exec
UPDATE users SET two_factor_email = ?, updated_at = ? WHERE id = ?;
