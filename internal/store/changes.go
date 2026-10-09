package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Errors for deciding change requests.
var (
	ErrStale      = errors.New("the value changed after this request was made; reject it and ask for a new one")
	ErrNotPending = errors.New("this request has already been decided or cancelled")
)

// Request statuses.
const (
	RequestPending   = "pending"
	RequestApproved  = "approved"
	RequestRejected  = "rejected"
	RequestCancelled = "cancelled"
)

// ChangeRequest is a proposed change to one value, waiting for an admin.
// Plaintext is the proposed value and is never serialised directly.
type ChangeRequest struct {
	ID              int64    `json:"id"`
	RepoID          int64    `json:"repoId"`
	RepoName        string   `json:"repoName"`
	FileID          int64    `json:"fileId"`
	FileName        string   `json:"fileName"`
	KeyID           int64    `json:"keyId"`
	KeyName         string   `json:"keyName"`
	IsSecret        bool     `json:"isSecret"`
	EnvironmentID   int64    `json:"environmentId"`
	EnvironmentName string   `json:"environmentName"`
	Delete          bool     `json:"delete"`
	Plaintext       string   `json:"-"`
	BaseVersion     int      `json:"baseVersion"`
	Reason          string   `json:"reason"`
	Status          string   `json:"status"`
	CreatedBy       *UserRef `json:"createdBy"`
	CreatedAt       string   `json:"createdAt"`
	DecidedBy       *UserRef `json:"decidedBy"`
	DecidedAt       string   `json:"decidedAt,omitempty"`
	DecisionNote    string   `json:"decisionNote"`
}

// proposalAAD differs from valueAAD, so a proposal can never be copied into
// value_versions and decrypt there.
func proposalAAD(keyID, envID int64) []byte {
	return []byte(fmt.Sprintf("envgrid:v1:proposal:key=%d:env=%d", keyID, envID))
}

const requestSelect = `SELECT cr.id, r.id, r.name, f.id, f.name, k.id, k.name, k.is_secret, e.id, e.name,
	cr.ciphertext, cr.base_version, cr.reason, cr.status, cr.created_at, COALESCE(cr.decided_at, ''), cr.decision_note,
	cu.id, cu.name, cu.email, du.id, du.name, du.email
	FROM change_requests cr
	JOIN keys k ON k.id = cr.key_id
	JOIN files f ON f.id = k.file_id
	JOIN repos r ON r.id = f.repo_id
	JOIN environments e ON e.id = cr.environment_id
	LEFT JOIN users cu ON cu.id = cr.created_by
	LEFT JOIN users du ON du.id = cr.decided_by`

func (s *Store) scanRequest(row scanner) (*ChangeRequest, error) {
	var (
		c                    ChangeRequest
		ct                   []byte
		cID, dID             sql.NullInt64
		cName, cEmail, dName sql.NullString
		dEmail               sql.NullString
	)
	err := row.Scan(&c.ID, &c.RepoID, &c.RepoName, &c.FileID, &c.FileName, &c.KeyID, &c.KeyName, &c.IsSecret,
		&c.EnvironmentID, &c.EnvironmentName, &ct, &c.BaseVersion, &c.Reason, &c.Status, &c.CreatedAt, &c.DecidedAt,
		&c.DecisionNote, &cID, &cName, &cEmail, &dID, &dName, &dEmail)
	if err != nil {
		return nil, mapErr(err)
	}
	if cID.Valid {
		c.CreatedBy = &UserRef{ID: cID.Int64, Name: cName.String, Email: cEmail.String}
	}
	if dID.Valid {
		c.DecidedBy = &UserRef{ID: dID.Int64, Name: dName.String, Email: dEmail.String}
	}
	if ct == nil {
		c.Delete = true
		return &c, nil
	}
	pt, err := s.cipher.Open(ct, proposalAAD(c.KeyID, c.EnvironmentID))
	if err != nil {
		return nil, fmt.Errorf("change request %d: %w", c.ID, err)
	}
	c.Plaintext = string(pt)
	return &c, nil
}

// CreateChangeRequest records a proposal to set (value) or delete (nil) a
// value. The key's rules are checked now, so an admin is never asked to
// approve something that cannot be applied.
func (s *Store) CreateChangeRequest(ctx context.Context, keyID, envID int64, value *string, reason string, userID int64) (*ChangeRequest, error) {
	k, err := s.Key(ctx, keyID)
	if err != nil {
		return nil, err
	}
	if problem := k.RuleProblem(value); problem != "" {
		return nil, &RuleError{Key: k.Name, Problem: problem, Pattern: k.Pattern}
	}
	_, _, version, err := s.current(ctx, s.db, keyID, envID)
	if err != nil {
		return nil, err
	}
	var ct []byte
	if value != nil {
		ct = s.cipher.Seal([]byte(*value), proposalAAD(keyID, envID))
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO change_requests (key_id, environment_id, ciphertext, base_version, reason, status, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, 'pending', ?, ?)`, keyID, envID, ct, version, reason, nullID(userID), s.now())
	if err != nil {
		return nil, mapErr(err)
	}
	id, _ := res.LastInsertId()
	return s.ChangeRequest(ctx, id)
}

// ChangeRequest returns one request.
func (s *Store) ChangeRequest(ctx context.Context, id int64) (*ChangeRequest, error) {
	return s.scanRequest(s.db.QueryRowContext(ctx, requestSelect+` WHERE cr.id = ?`, id))
}

// RequestFilter selects requests; zero values mean "any".
type RequestFilter struct {
	Status string
	RepoID int64
	Limit  int
}

// ChangeRequests lists requests, newest first.
func (s *Store) ChangeRequests(ctx context.Context, f RequestFilter) ([]ChangeRequest, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 200
	}
	rows, err := s.db.QueryContext(ctx, requestSelect+`
		WHERE (?1 = '' OR cr.status = ?1) AND (?2 = 0 OR r.id = ?2)
		ORDER BY cr.id DESC LIMIT ?3`, f.Status, f.RepoID, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChangeRequest{}
	for rows.Next() {
		c, err := s.scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// DecideChangeRequest approves (applying the proposal as a new version) or
// rejects a pending request. Approval is refused as stale when the value
// changed after the proposal was made.
func (s *Store) DecideChangeRequest(ctx context.Context, id int64, approve bool, note string, adminID int64) (*ChangeRequest, ValueChange, error) {
	var ch ValueChange
	req, err := s.ChangeRequest(ctx, id)
	if err != nil {
		return nil, ch, err
	}
	err = s.tx(ctx, func(tx *sql.Tx) error {
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM change_requests WHERE id = ?`, id).Scan(&status); err != nil {
			return mapErr(err)
		}
		if status != RequestPending {
			return ErrNotPending
		}
		next := RequestRejected
		if approve {
			next = RequestApproved
			_, _, version, err := s.current(ctx, tx, req.KeyID, req.EnvironmentID)
			if err != nil {
				return err
			}
			if version != req.BaseVersion {
				return ErrStale
			}
			var value *string
			if !req.Delete {
				v := req.Plaintext
				value = &v
			}
			if ch, err = s.setValue(ctx, tx, req.KeyID, req.EnvironmentID, value, adminID); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `UPDATE change_requests SET status = ?, decided_by = ?, decided_at = ?, decision_note = ? WHERE id = ?`,
			next, nullID(adminID), s.now(), note, id)
		return err
	})
	if err != nil {
		return nil, ch, err
	}
	req, err = s.ChangeRequest(ctx, id)
	return req, ch, err
}

// CancelChangeRequest withdraws a pending request.
func (s *Store) CancelChangeRequest(ctx context.Context, id, userID int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE change_requests SET status = 'cancelled', decided_by = ?, decided_at = ?
		WHERE id = ? AND status = 'pending'`, nullID(userID), s.now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotPending
	}
	return nil
}

// PendingRequests counts pending requests per (key, environment) of a file.
func (s *Store) PendingRequests(ctx context.Context, fileID int64) (map[[2]int64]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT cr.key_id, cr.environment_id, COUNT(*) FROM change_requests cr
		JOIN keys k ON k.id = cr.key_id WHERE k.file_id = ? AND cr.status = 'pending' GROUP BY 1, 2`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[[2]int64]int{}
	for rows.Next() {
		var k, e int64
		var n int
		if err := rows.Scan(&k, &e, &n); err != nil {
			return nil, err
		}
		out[[2]int64{k, e}] = n
	}
	return out, rows.Err()
}

// PendingCount is the number of requests waiting for a decision.
func (s *Store) PendingCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM change_requests WHERE status = 'pending'`).Scan(&n)
	return n, err
}
