-- An optional link to the code repository (GitHub, GitLab, Bitbucket...).
ALTER TABLE repos ADD COLUMN source_url TEXT NOT NULL DEFAULT '';
