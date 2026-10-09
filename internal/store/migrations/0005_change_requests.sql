-- Proposed changes to values, reviewed by an admin. Members use them for
-- protected environments, which they cannot write directly.
-- ciphertext is the proposed value, sealed with its own associated data so
-- it can never be mistaken for a stored value; NULL proposes a deletion.
-- base_version is the version the proposal was made against: approving a
-- proposal whose cell has changed since is refused as stale.
CREATE TABLE change_requests (
    id             INTEGER PRIMARY KEY,
    key_id         INTEGER NOT NULL REFERENCES keys (id) ON DELETE CASCADE,
    environment_id INTEGER NOT NULL REFERENCES environments (id) ON DELETE CASCADE,
    ciphertext     BLOB,
    base_version   INTEGER NOT NULL,
    reason         TEXT    NOT NULL DEFAULT '',
    status         TEXT    NOT NULL CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled')),
    created_by     INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at     TEXT    NOT NULL,
    decided_by     INTEGER REFERENCES users (id) ON DELETE SET NULL,
    decided_at     TEXT,
    decision_note  TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX change_requests_status ON change_requests (status, id);
CREATE INDEX change_requests_cell ON change_requests (key_id, environment_id);
