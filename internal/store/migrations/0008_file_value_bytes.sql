-- Plaintext bytes of each file's current values, kept in step by setValue,
-- so a file's total can be limited without summing it on every write.
-- A ciphertext is the plaintext plus a 12-byte nonce and a 16-byte tag.
ALTER TABLE files ADD COLUMN value_bytes INTEGER NOT NULL DEFAULT 0;

UPDATE files SET value_bytes = (
    SELECT COALESCE(SUM(length(v.ciphertext) - 28), 0)
    FROM keys k
    JOIN value_versions v ON v.key_id = k.id
    WHERE k.file_id = files.id
      AND v.ciphertext IS NOT NULL
      AND v.version = (SELECT MAX(m.version) FROM value_versions m
                       WHERE m.key_id = v.key_id AND m.environment_id = v.environment_id)
);
