-- name: CreateAPIKey :exec
INSERT INTO api_keys (id, user_id, name, key_hash, key_prefix, scope, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- The authentication lookup. Equality on an indexed hash, not on the secret:
-- there is nothing here to compare in constant time, because the value in the
-- WHERE clause is already a digest of the attacker's own input.
-- name: GetAPIKeyByHash :one
SELECT * FROM api_keys WHERE key_hash = ?;

-- name: ListAPIKeysByUser :many
SELECT * FROM api_keys
WHERE user_id = ? AND revoked_at IS NULL
ORDER BY created_at DESC;

-- name: CountActiveAPIKeys :one
SELECT COUNT(*) FROM api_keys WHERE user_id = ? AND revoked_at IS NULL;

-- The user_id predicate is the ownership check: another user's key id matches
-- zero rows rather than revoking their key.
-- name: RevokeAPIKey :exec
UPDATE api_keys
SET revoked_at = ?
WHERE id = ? AND user_id = ? AND revoked_at IS NULL;

-- name: GetAPIKeyForUser :one
SELECT * FROM api_keys WHERE id = ? AND user_id = ?;

-- name: TouchAPIKeyLastUsed :exec
UPDATE api_keys SET last_used_at = ? WHERE id = ?;
