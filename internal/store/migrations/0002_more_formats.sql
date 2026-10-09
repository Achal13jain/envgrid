-- Allow the ini and php formats. SQLite cannot change a CHECK constraint in
-- place, so the table is rebuilt (create, copy, drop, rename). The migration
-- runner turns foreign keys off around migrations, so dropping the old table
-- does not cascade into keys and values.
CREATE TABLE files_new (
    id         INTEGER PRIMARY KEY,
    repo_id    INTEGER NOT NULL REFERENCES repos (id) ON DELETE CASCADE,
    name       TEXT    NOT NULL,
    format     TEXT    NOT NULL CHECK (format IN ('dotenv', 'json', 'yaml', 'properties', 'ini', 'php', 'raw')),
    created_at TEXT    NOT NULL,
    UNIQUE (repo_id, name)
);

INSERT INTO files_new (id, repo_id, name, format, created_at)
SELECT id, repo_id, name, format, created_at FROM files;

DROP TABLE files;

ALTER TABLE files_new RENAME TO files;
