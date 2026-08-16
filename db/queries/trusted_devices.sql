-- name: CreateTrustedDevice :exec
INSERT INTO trusted_devices (id, user_id, token_hash, label, expires_at, created_at)
VALUES (?, ?, ?, ?, ?, ?);

-- Scoped to the user as well as the hash. The token is unguessable on its own,
-- but a lookup that ignores the user would let one account's device token
-- satisfy another account's challenge if a hash ever collided or leaked.
-- name: GetTrustedDevice :one
SELECT * FROM trusted_devices
WHERE token_hash = ? AND user_id = ? AND expires_at > ?;

-- name: TouchTrustedDevice :exec
UPDATE trusted_devices SET last_used_at = ? WHERE id = ?;

-- name: ListTrustedDevices :many
SELECT * FROM trusted_devices
WHERE user_id = ? AND expires_at > ?
ORDER BY created_at DESC;

-- name: DeleteTrustedDevice :exec
DELETE FROM trusted_devices WHERE id = ? AND user_id = ?;

-- Used whenever the account's credentials change and whenever two-factor is
-- switched off: trust granted under the old password should not survive it.
-- name: DeleteAllTrustedDevices :exec
DELETE FROM trusted_devices WHERE user_id = ?;

-- name: DeleteExpiredTrustedDevices :exec
DELETE FROM trusted_devices WHERE expires_at < ?;
