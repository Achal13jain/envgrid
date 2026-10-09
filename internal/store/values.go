package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"

	"github.com/Achal13jain/envgrid/internal/formats"
)

// Cell is the current state of one key in one environment. Plaintext is
// tagged json:"-" so a cell can never be serialised with its value by
// accident; the server decides what to expose.
type Cell struct {
	EnvironmentID int64
	Present       bool
	Plaintext     string `json:"-"`
	Version       int    // 0 when never set
	UpdatedAt     string
	UpdatedBy     *UserRef
}

// GridRow is one key and its cells, aligned with Grid.Environments.
type GridRow struct {
	Key   Key
	Cells []Cell
}

// Grid is the keys x environments matrix of a file.
type Grid struct {
	Environments []Environment
	Rows         []GridRow
}

// gridQuery builds the whole matrix in one statement: every environment of
// the file's repo crossed with every key, joined to the latest version of
// each (key, environment). With no keys it still
// returns one row per environment (key columns NULL).
// The latest version of each cell is found through the unique
// (key_id, environment_id, version) index, so a grid reads one version per
// cell however long the history is.
const gridQuery = `
SELECT e.id, e.repo_id, e.name, e.position, e.is_protected,
       k.id, k.name, k.description, k.is_secret, k.position, k.required, k.pattern, k.tags,
       v.ciphertext, v.version, v.created_at, u.id, u.name, u.email
FROM files f
JOIN environments e ON e.repo_id = f.repo_id
LEFT JOIN keys k ON k.file_id = f.id
LEFT JOIN value_versions v ON v.key_id = k.id AND v.environment_id = e.id
	AND v.version = (SELECT MAX(m.version) FROM value_versions m WHERE m.key_id = k.id AND m.environment_id = e.id)
LEFT JOIN users u ON u.id = v.created_by
WHERE f.id = ?1
ORDER BY k.position, k.id, e.position, e.id`

// Grid loads a file's matrix and decrypts every present value.
func (s *Store) Grid(ctx context.Context, fileID int64) (*Grid, error) {
	rows, err := s.db.QueryContext(ctx, gridQuery, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	g := &Grid{Environments: []Environment{}, Rows: []GridRow{}}
	firstKey := int64(-1)
	for rows.Next() {
		var (
			e                   Environment
			keyID, keyPos       sql.NullInt64
			keyName, keyDesc    sql.NullString
			keySecret, keyReq   sql.NullBool
			keyPattern, keyTags sql.NullString
			ct                  []byte
			version             sql.NullInt64
			updatedAt           sql.NullString
			userID              sql.NullInt64
			userName, userEmail sql.NullString
		)
		if err := rows.Scan(&e.ID, &e.RepoID, &e.Name, &e.Position, &e.IsProtected,
			&keyID, &keyName, &keyDesc, &keySecret, &keyPos, &keyReq, &keyPattern, &keyTags,
			&ct, &version, &updatedAt, &userID, &userName, &userEmail); err != nil {
			return nil, err
		}
		// Environments come from the first key's rows (or the key-less rows).
		if firstKey == -1 {
			firstKey = keyID.Int64
		}
		if keyID.Int64 == firstKey {
			g.Environments = append(g.Environments, e)
		}
		if !keyID.Valid {
			continue
		}
		if n := len(g.Rows); n == 0 || g.Rows[n-1].Key.ID != keyID.Int64 {
			g.Rows = append(g.Rows, GridRow{Key: Key{
				ID: keyID.Int64, FileID: fileID, Name: keyName.String, Description: keyDesc.String,
				IsSecret: keySecret.Bool, Position: int(keyPos.Int64),
				Required: keyReq.Bool, Pattern: keyPattern.String, Tags: splitTags(keyTags.String),
			}})
		}
		cell := Cell{EnvironmentID: e.ID, Version: int(version.Int64), UpdatedAt: updatedAt.String}
		if userID.Valid {
			cell.UpdatedBy = &UserRef{ID: userID.Int64, Name: userName.String, Email: userEmail.String}
		}
		if ct != nil {
			if cell.Plaintext, err = s.open(ct, keyID.Int64, e.ID); err != nil {
				return nil, err
			}
			cell.Present = true
		}
		row := &g.Rows[len(g.Rows)-1]
		row.Cells = append(row.Cells, cell)
	}
	return g, rows.Err()
}

// Column returns the cells of one environment keyed by key name, in key
// order. Used by export and compare.
func (g *Grid) Column(envID int64) ([]formats.KV, bool) {
	idx := g.envIndex(envID)
	if idx < 0 {
		return nil, false
	}
	kvs := []formats.KV{}
	for _, r := range g.Rows {
		if c := r.Cells[idx]; c.Present {
			kvs = append(kvs, formats.KV{Key: r.Key.Name, Value: c.Plaintext})
		}
	}
	return kvs, true
}

func (g *Grid) envIndex(envID int64) int {
	for i, e := range g.Environments {
		if e.ID == envID {
			return i
		}
	}
	return -1
}

// CompareItem is one key's cells in the two compared environments.
type CompareItem struct {
	Key  Key
	A, B Cell
}

// Comparison groups keys by how environments A and B differ. Keys missing in
// both environments are left out: there is nothing to reconcile.
type Comparison struct {
	MissingInA []CompareItem
	MissingInB []CompareItem
	Different  []CompareItem
	Same       []CompareItem
}

// ErrUnknownEnvironment means a compared environment is not in the grid.
var ErrUnknownEnvironment = errors.New("environment does not belong to this file's repo")

// Compare classifies every key of the grid. Values are compared as plaintext.
func Compare(g *Grid, envA, envB int64) (*Comparison, error) {
	ia, ib := g.envIndex(envA), g.envIndex(envB)
	if ia < 0 || ib < 0 {
		return nil, ErrUnknownEnvironment
	}
	c := &Comparison{MissingInA: []CompareItem{}, MissingInB: []CompareItem{}, Different: []CompareItem{}, Same: []CompareItem{}}
	for _, r := range g.Rows {
		item := CompareItem{Key: r.Key, A: r.Cells[ia], B: r.Cells[ib]}
		switch {
		case !item.A.Present && !item.B.Present:
		case !item.A.Present:
			c.MissingInA = append(c.MissingInA, item)
		case !item.B.Present:
			c.MissingInB = append(c.MissingInB, item)
		case item.A.Plaintext != item.B.Plaintext:
			c.Different = append(c.Different, item)
		default:
			c.Same = append(c.Same, item)
		}
	}
	return c, nil
}

// ValueChange describes the outcome of a write.
type ValueChange struct {
	Changed    bool // false when the value was already as requested
	WasPresent bool
	Version    int
}

// SetValue appends a version for (key, env). value == nil records a
// deletion. Writing the current value again is a no-op.
func (s *Store) SetValue(ctx context.Context, keyID, envID int64, value *string, userID int64) (ValueChange, error) {
	var ch ValueChange
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		ch, err = s.setValue(ctx, tx, keyID, envID, value, userID)
		return err
	})
	return ch, err
}

func (s *Store) setValue(ctx context.Context, q querier, keyID, envID int64, value *string, userID int64) (ValueChange, error) {
	var ch ValueChange
	k, err := scanKey(q.QueryRowContext(ctx, `SELECT `+keyCols+` FROM keys k WHERE k.id = ?`, keyID))
	if err != nil {
		return ch, err
	}
	if problem := k.RuleProblem(value); problem != "" {
		return ch, &RuleError{Key: k.Name, Problem: problem, Pattern: k.Pattern}
	}
	cur, present, version, err := s.current(ctx, q, keyID, envID)
	if err != nil {
		return ch, err
	}
	ch.WasPresent, ch.Version = present, version
	if (value == nil && !present) || (value != nil && present && *value == cur) {
		return ch, nil
	}
	// A file holds a bounded number of plaintext bytes across its
	// environments, so a grid, export or restore reads a bounded amount.
	var delta int64
	if value != nil {
		delta += int64(len(*value))
	}
	if present {
		delta -= int64(len(cur))
	}
	if delta != 0 {
		var total int64
		if err := q.QueryRowContext(ctx, `SELECT value_bytes FROM files WHERE id = ?`, k.FileID).Scan(&total); err != nil {
			return ch, err
		}
		if delta > 0 && total+delta > MaxFileBytes {
			return ch, fmt.Errorf("%w: a file holds at most %d MiB of values", ErrTooMany, MaxFileBytes>>20)
		}
		if _, err := q.ExecContext(ctx, `UPDATE files SET value_bytes = value_bytes + ? WHERE id = ?`, delta, k.FileID); err != nil {
			return ch, err
		}
	}
	var ct []byte
	if value != nil {
		ct = s.cipher.Seal([]byte(*value), valueAAD(keyID, envID))
	}
	ch.Changed, ch.Version = true, version+1
	_, err = q.ExecContext(ctx, `INSERT INTO value_versions (key_id, environment_id, ciphertext, version, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, keyID, envID, ct, ch.Version, nullID(userID), s.now())
	return ch, mapErr(err)
}

func (s *Store) current(ctx context.Context, q querier, keyID, envID int64) (value string, present bool, version int, err error) {
	var ct []byte
	err = q.QueryRowContext(ctx, `SELECT ciphertext, version FROM value_versions
		WHERE key_id = ? AND environment_id = ? ORDER BY version DESC LIMIT 1`, keyID, envID).Scan(&ct, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, 0, nil
	}
	if err != nil || ct == nil {
		return "", false, version, err
	}
	value, err = s.open(ct, keyID, envID)
	return value, err == nil, version, err
}

// CurrentValue returns the current plaintext, if any.
func (s *Store) CurrentValue(ctx context.Context, keyID, envID int64) (string, bool, error) {
	v, ok, _, err := s.current(ctx, s.db, keyID, envID)
	return v, ok, err
}

// ValueAt returns the plaintext of a specific version. present is false for
// a deletion marker; ErrNotFound if the version does not exist.
func (s *Store) ValueAt(ctx context.Context, keyID, envID int64, version int) (string, bool, error) {
	var ct []byte
	err := s.db.QueryRowContext(ctx, `SELECT ciphertext FROM value_versions WHERE key_id = ? AND environment_id = ? AND version = ?`,
		keyID, envID, version).Scan(&ct)
	if err != nil {
		return "", false, mapErr(err)
	}
	if ct == nil {
		return "", false, nil
	}
	v, err := s.open(ct, keyID, envID)
	return v, err == nil, err
}

// Version is one entry of a value's history.
type Version struct {
	Version   int
	Present   bool
	Plaintext string `json:"-"`
	CreatedAt string
	CreatedBy *UserRef
}

// HistoryLimit is how many versions History returns. Every version is
// decrypted, so an unbounded list would grow with each write.
const HistoryLimit = 100

// History lists the newest HistoryLimit versions of (key, env), newest first.
func (s *Store) History(ctx context.Context, keyID, envID int64) ([]Version, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT v.version, v.ciphertext, v.created_at, u.id, u.name, u.email
		FROM value_versions v LEFT JOIN users u ON u.id = v.created_by
		WHERE v.key_id = ? AND v.environment_id = ? ORDER BY v.version DESC LIMIT ?`, keyID, envID, HistoryLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Version{}
	for rows.Next() {
		var (
			v                   Version
			ct                  []byte
			userID              sql.NullInt64
			userName, userEmail sql.NullString
		)
		if err := rows.Scan(&v.Version, &ct, &v.CreatedAt, &userID, &userName, &userEmail); err != nil {
			return nil, err
		}
		if userID.Valid {
			v.CreatedBy = &UserRef{ID: userID.Int64, Name: userName.String, Email: userEmail.String}
		}
		if ct != nil {
			if v.Plaintext, err = s.open(ct, keyID, envID); err != nil {
				return nil, err
			}
			v.Present = true
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ImportResult summarises an import by key name.
type ImportResult struct {
	Added       []string `json:"added"`
	Updated     []string `json:"updated"`
	Unchanged   []string `json:"unchanged"`
	KeysCreated []string `json:"keysCreated"`
	// ReadAs is set when the content was read in another format than the
	// file's own (for example JSON pasted into a .env file).
	ReadAs string `json:"readAs,omitempty"`
}

// secretName guesses which new keys should start masked. False positives
// only mask a value, so the list leans wide.
var secretName = regexp.MustCompile(`(?i)(secret|passw|pwd|token|private|credential|auth|dsn|cert|salt|jwt|access[_.-]?code|(api|access|signing|encryption|master|field|session|license)[_.-]?key)`)

// Import upserts parsed key/values into one environment in a single
// transaction. Keys absent from the input are left untouched. The whole
// import is one audit row (import_file) listing what it changed, written in
// the same transaction; base carries the user, repo, file and environment.
// errDryRun rolls back an import that only previews its result.
var errDryRun = errors.New("dry run")

// Import writes kvs into one environment of a file. With dryRun it computes
// the same result and rolls everything back, audit rows included.
func (s *Store) Import(ctx context.Context, file *File, envID int64, kvs []formats.KV, base AuditEntry, dryRun bool) (*ImportResult, error) {
	for _, kv := range kvs {
		if file.Format == formats.Raw {
			break
		}
		if err := formats.ValidateKey(file.Format, kv.Key); err != nil {
			return nil, fmt.Errorf("key %q: %w", kv.Key, err)
		}
	}
	res := &ImportResult{Added: []string{}, Updated: []string{}, Unchanged: []string{}, KeysCreated: []string{}}
	// The audit row carries the start time, so "just before this import" in
	// the restore view comes before its first value, however long it ran.
	started := s.now()
	err := s.tx(ctx, func(tx *sql.Tx) error {
		existing := map[string]int64{}
		rows, err := tx.QueryContext(ctx, `SELECT id, name FROM keys WHERE file_id = ?`, file.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				return err
			}
			existing[name] = id
		}
		if err := rows.Err(); err != nil {
			return err
		}

		var broken []*RuleError
		for _, kv := range kvs {
			keyID, ok := existing[kv.Key]
			if !ok {
				k, err := s.createKey(ctx, tx, file.ID, kv.Key, "", secretName.MatchString(kv.Key))
				if err != nil {
					return err
				}
				keyID = k.ID
				existing[kv.Key] = keyID
				res.KeysCreated = append(res.KeysCreated, kv.Key)
			}
			v := kv.Value
			ch, err := s.setValue(ctx, tx, keyID, envID, &v, base.UserID)
			var re *RuleError
			if errors.As(err, &re) {
				broken = append(broken, re)
				continue
			}
			if err != nil {
				return err
			}
			switch {
			case !ch.Changed:
				res.Unchanged = append(res.Unchanged, kv.Key)
			case ch.WasPresent:
				res.Updated = append(res.Updated, kv.Key)
			default:
				res.Added = append(res.Added, kv.Key)
			}
		}
		if len(broken) > 0 {
			return &RuleErrors{Items: broken}
		}
		if len(res.Added)+len(res.Updated)+len(res.KeysCreated) > 0 {
			entry := base
			entry.Action, entry.at = "import_file", started
			entry.Detail = cloneDetail(base.Detail)
			entry.Detail["added"], entry.Detail["updated"], entry.Detail["keysCreated"] = res.Added, res.Updated, res.KeysCreated
			if err := s.audit(ctx, tx, entry); err != nil {
				return err
			}
		}
		if dryRun {
			return errDryRun
		}
		return nil
	})
	if errors.Is(err, errDryRun) {
		return res, nil
	}
	if err != nil {
		return nil, mapErr(err)
	}
	return res, nil
}

func cloneDetail(d map[string]any) map[string]any {
	out := make(map[string]any, len(d)+3)
	for k, v := range d {
		out[k] = v
	}
	return out
}

// MaxFileBytes caps the plaintext of one file's current values across all of
// its environments.
const MaxFileBytes = 16 << 20
