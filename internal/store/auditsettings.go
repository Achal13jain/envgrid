package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// AuditCategory groups audit actions that an admin can stop recording
// together. Locked categories are always recorded: who has access and what
// was deleted is the trail that matters after an incident.
type AuditCategory struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Locked      bool   `json:"locked"`
	Enabled     bool   `json:"enabled"`
	actions     []string
}

var auditCategories = []AuditCategory{
	{ID: "secrets", Label: "Reveals, copies and exports",
		Description: "Who saw a secret, copied a value or downloaded a file.",
		actions:     []string{"reveal_secret", "copy_value", "export_file"}},
	{ID: "access", Label: "People, tokens and these settings", Locked: true,
		Description: "Users added or changed, API tokens created or revoked, wrong current passwords, and changes to what is recorded.",
		actions:     []string{"user_created", "update_user", "create_token", "delete_token", "update_audit_settings", "password_check_failed"}},
	{ID: "deletions", Label: "Deletions", Locked: true,
		Description: "Repos, files, environments and keys that were deleted.",
		actions:     []string{"delete_repo", "delete_file", "delete_environment", "delete_key"}},
	{ID: "values", Label: "Value changes",
		Description: "Values set, changed, deleted or restored. Each value's history keeps who and when either way.",
		actions:     []string{"create_value", "update_value", "delete_value", "restore_environment", "import_file"}},
	{ID: "structure", Label: "Repos, files, keys and environments",
		Description: "Created, renamed, edited or reordered.",
		actions: []string{"create_repo", "update_repo", "create_file", "update_file", "create_key", "update_key", "reorder_keys",
			"create_environment", "update_environment", "reorder_environments"}},
	{ID: "requests", Label: "Change requests",
		Description: "Requests made, approved, rejected or cancelled.",
		actions:     []string{"request_change", "approve_change", "reject_change", "cancel_change"}},
	{ID: "logins", Label: "Sign-ins",
		Description: "Each time someone signs in, or tries with a wrong password.",
		actions:     []string{"login", "login_failed"}},
}

// ErrLockedAuditCategory refuses to stop recording a locked category.
var ErrLockedAuditCategory = errors.New("this kind of activity is always recorded")

// AuditCategories lists the categories and whether each is recorded.
func (s *Store) AuditCategories() []AuditCategory {
	off := s.auditOff()
	out := make([]AuditCategory, len(auditCategories))
	for i, c := range auditCategories {
		c.Enabled = !off[c.ID]
		out[i] = c
	}
	return out
}

// SetAuditOff stores the categories that are not recorded.
func (s *Store) SetAuditOff(ctx context.Context, ids []string) error {
	off := map[string]bool{}
	for _, id := range ids {
		c := findCategory(id)
		if c == nil {
			return fmt.Errorf("unknown activity category %q", id)
		}
		if c.Locked {
			return ErrLockedAuditCategory
		}
		off[id] = true
	}
	list := make([]string, 0, len(off))
	for _, c := range auditCategories {
		if off[c.ID] {
			list = append(list, c.ID)
		}
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES ('audit_off', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, strings.Join(list, ",")); err != nil {
		return err
	}
	s.off.Store(&off)
	return nil
}

// loadAuditOff reads the stored setting at start-up.
func (s *Store) loadAuditOff(ctx context.Context) error {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT CAST(value AS TEXT) FROM meta WHERE key = 'audit_off'`).Scan(&v)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	off := map[string]bool{}
	for _, id := range strings.Split(v, ",") {
		if c := findCategory(id); c != nil && !c.Locked {
			off[id] = true
		}
	}
	s.off.Store(&off)
	return nil
}

func (s *Store) auditOff() map[string]bool {
	if m := s.off.Load(); m != nil {
		return *m
	}
	return nil
}

// skipAudit reports whether an action belongs to a category that is off.
func (s *Store) skipAudit(action string) bool {
	off := s.auditOff()
	if len(off) == 0 {
		return false
	}
	for _, c := range auditCategories {
		if off[c.ID] {
			for _, a := range c.actions {
				if a == action {
					return true
				}
			}
		}
	}
	return false
}

func findCategory(id string) *AuditCategory {
	for i := range auditCategories {
		if auditCategories[i].ID == id {
			return &auditCategories[i]
		}
	}
	return nil
}
