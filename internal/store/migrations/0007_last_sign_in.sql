-- When each person last signed in, shown on the Users page. Filled from the
-- audit log for existing accounts.
ALTER TABLE users ADD COLUMN last_login_at TEXT;

UPDATE users SET last_login_at = (
	SELECT MAX(a.created_at) FROM audit_log a WHERE a.user_id = users.id AND a.action = 'login'
);
