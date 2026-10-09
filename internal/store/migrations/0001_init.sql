CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    email         TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    name          TEXT    NOT NULL DEFAULT '',
    password_hash TEXT    NOT NULL,
    role          TEXT    NOT NULL CHECK (role IN ('admin', 'member')),
    created_at    TEXT    NOT NULL,
    disabled      INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1))
);

-- id is the SHA-256 of the cookie token, so a leaked database holds no
-- usable session.
CREATE TABLE sessions (
    id         TEXT    PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at TEXT    NOT NULL
);
CREATE INDEX sessions_user ON sessions (user_id);

CREATE TABLE repos (
    id          INTEGER PRIMARY KEY,
    name        TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    description TEXT    NOT NULL DEFAULT '',
    created_by  INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at  TEXT    NOT NULL
);

CREATE TABLE environments (
    id           INTEGER PRIMARY KEY,
    repo_id      INTEGER NOT NULL REFERENCES repos (id) ON DELETE CASCADE,
    name         TEXT    NOT NULL COLLATE NOCASE,
    position     INTEGER NOT NULL,
    is_protected INTEGER NOT NULL DEFAULT 0 CHECK (is_protected IN (0, 1)),
    UNIQUE (repo_id, name)
);

CREATE TABLE files (
    id         INTEGER PRIMARY KEY,
    repo_id    INTEGER NOT NULL REFERENCES repos (id) ON DELETE CASCADE,
    name       TEXT    NOT NULL,
    format     TEXT    NOT NULL CHECK (format IN ('dotenv', 'json', 'yaml', 'properties', 'raw')),
    created_at TEXT    NOT NULL,
    UNIQUE (repo_id, name)
);

CREATE TABLE keys (
    id          INTEGER PRIMARY KEY,
    file_id     INTEGER NOT NULL REFERENCES files (id) ON DELETE CASCADE,
    name        TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    is_secret   INTEGER NOT NULL DEFAULT 0 CHECK (is_secret IN (0, 1)),
    position    INTEGER NOT NULL,
    UNIQUE (file_id, name)
);

-- The spec's "values" table. VALUES is a reserved word in SQLite.
-- Append-only: the current value is the highest version per (key, env), and
-- a NULL ciphertext records a deletion.
CREATE TABLE value_versions (
    id             INTEGER PRIMARY KEY,
    key_id         INTEGER NOT NULL REFERENCES keys (id) ON DELETE CASCADE,
    environment_id INTEGER NOT NULL REFERENCES environments (id) ON DELETE CASCADE,
    ciphertext     BLOB,
    version        INTEGER NOT NULL,
    created_by     INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at     TEXT    NOT NULL,
    UNIQUE (key_id, environment_id, version)
);
CREATE INDEX value_versions_env ON value_versions (environment_id);

-- No foreign keys: audit rows must outlive what they describe. detail keeps
-- the names at the time of the event.
CREATE TABLE audit_log (
    id             INTEGER PRIMARY KEY,
    user_id        INTEGER,
    action         TEXT    NOT NULL,
    repo_id        INTEGER,
    file_id        INTEGER,
    key_id         INTEGER,
    environment_id INTEGER,
    detail         TEXT    NOT NULL DEFAULT '{}',
    created_at     TEXT    NOT NULL
);
CREATE INDEX audit_log_repo ON audit_log (repo_id, id);

-- key_check holds a known plaintext sealed with the master key so a wrong
-- key is caught at startup instead of on first read.
CREATE TABLE meta (
    key   TEXT PRIMARY KEY,
    value BLOB NOT NULL
);
