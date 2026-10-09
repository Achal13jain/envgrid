package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"
)

// ValueAt is a key with its value in one environment at a point in time.
type ValueAt struct {
	Key       Key
	Present   bool
	Plaintext string `json:"-"`
	Version   int    // 0 when the key had no version yet
}

// valuesAtQuery picks, per key of the file, the latest version in the
// environment created at or before ?3.
const valuesAtQuery = `
SELECT ` + keyCols + `, v.ciphertext, COALESCE(v.version, 0)
FROM keys k
LEFT JOIN value_versions v ON v.key_id = k.id AND v.environment_id = ?2
	AND v.version = (SELECT MAX(m.version) FROM value_versions m
	                 WHERE m.key_id = k.id AND m.environment_id = ?2 AND m.created_at <= ?3)
WHERE k.file_id = ?1
ORDER BY k.position, k.id`

func (s *Store) valuesAt(ctx context.Context, q querier, fileID, envID int64, at time.Time) ([]ValueAt, error) {
	rows, err := q.QueryContext(ctx, valuesAtQuery, fileID, envID, at.UTC().Format(timeFormat))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ValueAt{}
	for rows.Next() {
		var (
			v    ValueAt
			tags string
			ct   []byte
		)
		k := &v.Key
		if err := rows.Scan(&k.ID, &k.FileID, &k.Name, &k.Description, &k.IsSecret, &k.Position, &k.Required, &k.Pattern, &tags, &ct, &v.Version); err != nil {
			return nil, err
		}
		k.Tags = splitTags(tags)
		if ct != nil {
			if v.Plaintext, err = s.open(ct, k.ID, envID); err != nil {
				return nil, err
			}
			v.Present = true
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// RollbackChange is one key a rollback would change.
type RollbackChange struct {
	Then    ValueAt
	Current Cell
}

// growth is how many plaintext bytes applying the change adds.
func (c RollbackChange) growth() int {
	n := 0
	if c.Then.Present {
		n += len(c.Then.Plaintext)
	}
	if c.Current.Present {
		n -= len(c.Current.Plaintext)
	}
	return n
}

// RollbackPreview lists the keys whose value in the environment differs
// from what it was at time at. Keys created after at had no value then, so
// restoring removes their value.
func (s *Store) RollbackPreview(ctx context.Context, fileID, envID int64, at time.Time) ([]RollbackChange, error) {
	return s.rollbackChanges(ctx, s.db, fileID, envID, at)
}

func (s *Store) rollbackChanges(ctx context.Context, q querier, fileID, envID int64, at time.Time) ([]RollbackChange, error) {
	then, err := s.valuesAt(ctx, q, fileID, envID, at)
	if err != nil {
		return nil, err
	}
	out := []RollbackChange{}
	for _, t := range then {
		cur, present, version, err := s.current(ctx, q, t.Key.ID, envID)
		if err != nil {
			return nil, err
		}
		if present == t.Present && cur == t.Plaintext {
			continue
		}
		out = append(out, RollbackChange{Then: t, Current: Cell{EnvironmentID: envID, Present: present, Plaintext: cur, Version: version}})
	}
	return out, nil
}

// Rollback writes the values of time at back as new versions, in one
// transaction, auditing each change. Rule failures abort the whole restore.
func (s *Store) Rollback(ctx context.Context, fileID, envID int64, at time.Time, base AuditEntry) ([]string, error) {
	var changed []string
	err := s.tx(ctx, func(tx *sql.Tx) error {
		changes, err := s.rollbackChanges(ctx, tx, fileID, envID, at)
		if err != nil {
			return err
		}
		// Shrinking values go first, so a restore that fits the file's byte
		// budget is not refused halfway through.
		sort.SliceStable(changes, func(i, j int) bool { return changes[i].growth() < changes[j].growth() })
		var broken []*RuleError
		for _, c := range changes {
			var value *string
			if c.Then.Present {
				v := c.Then.Plaintext
				value = &v
			}
			ch, err := s.setValue(ctx, tx, c.Then.Key.ID, envID, value, base.UserID)
			var re *RuleError
			if errors.As(err, &re) {
				broken = append(broken, re)
				continue
			}
			if err != nil {
				return err
			}
			if !ch.Changed {
				continue
			}
			entry := base
			entry.KeyID = c.Then.Key.ID
			entry.Detail = cloneDetail(base.Detail)
			entry.Detail["key"] = c.Then.Key.Name
			entry.Detail["via"] = "rollback"
			entry.Detail["version"] = ch.Version
			switch {
			case value == nil:
				entry.Action = "delete_value"
			case ch.WasPresent:
				entry.Action = "update_value"
			default:
				entry.Action = "create_value"
			}
			if err := s.audit(ctx, tx, entry); err != nil {
				return err
			}
			changed = append(changed, c.Then.Key.Name)
		}
		if len(broken) > 0 {
			return &RuleErrors{Items: broken}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if changed == nil {
		changed = []string{}
	}
	return changed, nil
}
