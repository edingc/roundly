-- name: CreateRecoveryCode :exec
INSERT INTO recovery_codes (id, user_id, code_hash, created_at)
VALUES (?, ?, ?, ?);

-- Returns the hashes to check a submitted code against. There is no lookup by
-- hash: argon2id salts every hash, so the same code stored twice hashes
-- differently and only a scan can match it.
-- name: ListUnusedRecoveryCodes :many
SELECT * FROM recovery_codes
WHERE user_id = ? AND consumed_at IS NULL
ORDER BY created_at;

-- name: CountUnusedRecoveryCodes :one
SELECT COUNT(*) FROM recovery_codes WHERE user_id = ? AND consumed_at IS NULL;

-- Guarded on consumed_at IS NULL so two requests racing the same code cannot
-- both come away having spent it.
-- name: ConsumeRecoveryCode :exec
UPDATE recovery_codes SET consumed_at = ? WHERE id = ? AND consumed_at IS NULL;

-- Regenerating replaces the whole set rather than topping it up: a sheet that
-- is partly old and partly new is one nobody can reason about.
-- name: DeleteRecoveryCodes :exec
DELETE FROM recovery_codes WHERE user_id = ?;
