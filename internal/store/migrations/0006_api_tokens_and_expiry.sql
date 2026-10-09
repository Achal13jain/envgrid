-- Personal API tokens for scripts and the CLI. Only a SHA-256 of the token
-- is stored; prefix is a few leading characters shown to tell tokens apart.
CREATE TABLE api_tokens (
    id           INTEGER PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         TEXT    NOT NULL,
    token_hash   TEXT    NOT NULL UNIQUE,
    prefix       TEXT    NOT NULL,
    created_at   TEXT    NOT NULL,
    expires_at   TEXT,
    last_used_at TEXT
);
CREATE INDEX api_tokens_user ON api_tokens (user_id);

-- Temporary access: after expires_at the account can no longer sign in or
-- use its tokens. NULL means no end date.
ALTER TABLE users ADD COLUMN expires_at TEXT;
