-- Validation rules and tags on keys.
-- required: every environment must have a value, and it cannot be deleted.
-- pattern:  a regular expression every value must contain a match for.
-- tags:     comma-separated, lowercase labels used for filtering.
ALTER TABLE keys ADD COLUMN required INTEGER NOT NULL DEFAULT 0 CHECK (required IN (0, 1));
ALTER TABLE keys ADD COLUMN pattern TEXT NOT NULL DEFAULT '';
ALTER TABLE keys ADD COLUMN tags TEXT NOT NULL DEFAULT '';
