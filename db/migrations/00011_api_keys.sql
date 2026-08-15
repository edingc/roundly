-- +goose Up
-- +goose StatementBegin

-- api_keys are long-lived, read-only credentials for scripts and integrations.
--
-- Only the SHA-256 of the token is stored, exactly as for refresh_tokens. A
-- password KDF would be wrong here: the token is 256 bits of CSPRNG output, so
-- there is nothing to guess regardless of how fast the hash is, and running
-- argon2id on every API request would be a denial of service the server
-- inflicts on itself. The hash is domain-separated in Go (see apikey.HashToken)
-- so an api_keys row and a refresh_tokens row can never be interchanged.
--
-- key_prefix is the first 12 characters of the token, stored in the clear so
-- the UI can show "rnd_a1b2c3d4..." and the user can tell two keys apart. The
-- remaining ~200 bits stay secret.
--
-- Revocation is a soft delete rather than a DELETE, so that "this key was used
-- last Tuesday" survives revoking it — which is exactly the question a user
-- asks after revoking a key they think leaked.
CREATE TABLE api_keys (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    key_hash TEXT NOT NULL UNIQUE,
    key_prefix TEXT NOT NULL,
    -- Only 'read' exists. The column and its CHECK are here so that adding a
    -- write scope later is a deliberate migration plus a code audit, rather
    -- than something a stray UPDATE could do quietly.
    scope TEXT NOT NULL DEFAULT 'read',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    -- Approximate by design: written at most once per key per flush window, so
    -- that a read-only request never has to take the single writer connection.
    last_used_at TEXT,
    expires_at TEXT,
    revoked_at TEXT,
    CHECK (scope IN ('read'))
);

-- No index on key_hash: the UNIQUE constraint already creates one, and that is
-- the index the authentication lookup uses.
CREATE INDEX idx_api_keys_user_id ON api_keys(user_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_api_keys_user_id;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE api_keys;
-- +goose StatementEnd
