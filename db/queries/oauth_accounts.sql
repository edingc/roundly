-- name: CreateOAuthAccount :exec
INSERT INTO oauth_accounts (id, user_id, provider, provider_subject, provider_email, created_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetOAuthAccountByProviderSubject :one
SELECT * FROM oauth_accounts WHERE provider = ? AND provider_subject = ?;

-- name: ListOAuthAccountsByUser :many
SELECT * FROM oauth_accounts WHERE user_id = ? ORDER BY created_at;

-- name: DeleteOAuthAccount :exec
DELETE FROM oauth_accounts WHERE user_id = ? AND provider = ?;
