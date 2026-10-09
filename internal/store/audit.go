package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// AuditEntry is a new audit row. Zero ids are stored as NULL. Detail holds
// names and counts, never values.
type AuditEntry struct {
	UserID        int64
	Action        string
	RepoID        int64
	FileID        int64
	KeyID         int64
	EnvironmentID int64
	Detail        map[string]any
	// at overrides the time the row is stamped with (timeFormat); empty means now.
	at string
}

// AuditRecord is a stored audit row.
type AuditRecord struct {
	ID            int64           `json:"id"`
	Action        string          `json:"action"`
	User          *UserRef        `json:"user"`
	RepoID        *int64          `json:"repoId"`
	FileID        *int64          `json:"fileId"`
	KeyID         *int64          `json:"keyId"`
	EnvironmentID *int64          `json:"environmentId"`
	Detail        json.RawMessage `json:"detail"`
	CreatedAt     string          `json:"createdAt"`
}

// Audit writes one audit row.
func (s *Store) Audit(ctx context.Context, e AuditEntry) error {
	return s.audit(ctx, s.db, e)
}

func (s *Store) audit(ctx context.Context, q querier, e AuditEntry) error {
	if s.skipAudit(e.Action) {
		return nil
	}
	detail := []byte("{}")
	if len(e.Detail) > 0 {
		var err error
		if detail, err = json.Marshal(e.Detail); err != nil {
			return err
		}
	}
	at := e.at
	if at == "" {
		at = s.now()
	}
	_, err := q.ExecContext(ctx, `INSERT INTO audit_log (user_id, action, repo_id, file_id, key_id, environment_id, detail, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		nullID(e.UserID), e.Action, nullID(e.RepoID), nullID(e.FileID), nullID(e.KeyID), nullID(e.EnvironmentID), string(detail), at)
	return err
}

// AuditFilter selects audit rows. Zero values mean "any".
type AuditFilter struct {
	RepoID int64
	FileID int64
	EnvID  int64
	KeyID  int64
	// Since and Until limit rows to [Since, Until); zero times mean no limit.
	Since, Until time.Time
	UserID       int64
	Action       string
	Before       int64 // cursor: only rows with a smaller id
	Limit        int
	// MemberID, when set, limits rows to those a member may see: anything
	// tied to a repo, plus the member's own account events.
	MemberID int64
}

// AuditLog returns matching rows, newest first.
func (s *Store) AuditLog(ctx context.Context, f AuditFilter) ([]AuditRecord, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT a.id, a.action, a.repo_id, a.file_id, a.key_id, a.environment_id, a.detail, a.created_at,
		u.id, u.name, u.email
		FROM audit_log a LEFT JOIN users u ON u.id = a.user_id
		WHERE (?1 = 0 OR a.repo_id = ?1)
		  AND (?2 = 0 OR a.user_id = ?2)
		  AND (?3 = '' OR a.action = ?3)
		  AND (?4 = 0 OR a.id < ?4)
		  AND (?5 = 0 OR a.repo_id IS NOT NULL OR a.user_id = ?5)
		  AND (?7 = 0 OR a.file_id = ?7)
		  AND (?8 = 0 OR a.environment_id = ?8)
		  AND (?9 = 0 OR a.key_id = ?9)
		  AND (?10 = '' OR a.created_at >= ?10)
		  AND (?11 = '' OR a.created_at < ?11)
		ORDER BY a.id DESC LIMIT ?6`,
		f.RepoID, f.UserID, f.Action, f.Before, f.MemberID, f.Limit, f.FileID, f.EnvID, f.KeyID, stamp(f.Since), stamp(f.Until))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditRecord{}
	for rows.Next() {
		var (
			r                   AuditRecord
			repo, file, key, en sql.NullInt64
			detail              string
			userID              sql.NullInt64
			userName, userEmail sql.NullString
		)
		if err := rows.Scan(&r.ID, &r.Action, &repo, &file, &key, &en, &detail, &r.CreatedAt, &userID, &userName, &userEmail); err != nil {
			return nil, err
		}
		r.RepoID, r.FileID, r.KeyID, r.EnvironmentID = ptr(repo), ptr(file), ptr(key), ptr(en)
		r.Detail = json.RawMessage(detail)
		if userID.Valid {
			r.User = &UserRef{ID: userID.Int64, Name: userName.String, Email: userEmail.String}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func ptr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	return &n.Int64
}

// stamp formats a time like created_at, or "" for the zero time.
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(timeFormat)
}
