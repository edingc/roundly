-- name: CreateEmailChallenge :exec
INSERT INTO email_challenges (id, user_id, purpose, email, code_hash, expires_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: GetEmailChallenge :one
SELECT * FROM email_challenges WHERE id = ?;

-- The verification link carries the token itself rather than a challenge id, so
-- this is the only lookup that starts from the hash. Two-factor never uses it:
-- six digits are not unique enough to name a row.
-- name: GetEmailChallengeByCodeHash :one
SELECT * FROM email_challenges WHERE code_hash = ? AND purpose = ?;

-- name: ConsumeEmailChallenge :exec
UPDATE email_challenges SET consumed_at = ? WHERE id = ? AND consumed_at IS NULL;

-- name: IncrementEmailChallengeAttempts :exec
UPDATE email_challenges SET attempts = attempts + 1 WHERE id = ?;

-- Counts what has been sent to one user for one purpose since a cut-off. This
-- is the send-side rate limit: without it, "resend the code" is an open relay
-- pointed at somebody else's inbox.
-- name: CountRecentEmailChallenges :one
SELECT COUNT(*) FROM email_challenges
WHERE user_id = ? AND purpose = ? AND created_at > ?;

-- Invalidates whatever is outstanding. Issuing a new code has to retire the old
-- one, or every resend widens the set of codes that would be accepted.
-- name: ConsumeOutstandingEmailChallenges :exec
UPDATE email_challenges
SET consumed_at = ?
WHERE user_id = ? AND purpose = ? AND consumed_at IS NULL;

-- name: DeleteExpiredEmailChallenges :exec
DELETE FROM email_challenges WHERE expires_at < ?;
