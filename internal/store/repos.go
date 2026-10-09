package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/Achal13jain/envgrid/internal/formats"
)

// ErrBadOrder is returned when a reorder list is not exactly the current ids.
var ErrBadOrder = errors.New("ids must list every item exactly once")

// Repo is a project holding environments and files.
type Repo struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	SourceURL    string `json:"sourceUrl"` // optional link to the code repository
	CreatedBy    int64  `json:"createdBy"`
	CreatedAt    string `json:"createdAt"`
	FileCount    int    `json:"fileCount"`
	EnvCount     int    `json:"envCount"`
	KeyCount     int    `json:"keyCount"`
	LastChangeAt string `json:"lastChangeAt"`
	// Coverage has one entry per environment, in display order: how many of
	// the repo's keys have a value there.
	Coverage []EnvCoverage `json:"coverage"`
}

// EnvCoverage counts the keys that have a value in one environment.
type EnvCoverage struct {
	EnvironmentID int64  `json:"environmentId"`
	Name          string `json:"name"`
	IsProtected   bool   `json:"isProtected"`
	Present       int    `json:"present"`
}

// readActions do not count as changes for "last change".
const repoSelect = `SELECT r.id, r.name, r.description, r.source_url, COALESCE(r.created_by, 0), r.created_at,
	(SELECT COUNT(*) FROM files f WHERE f.repo_id = r.id),
	(SELECT COUNT(*) FROM environments e WHERE e.repo_id = r.id),
	(SELECT COUNT(*) FROM keys k JOIN files f ON f.id = k.file_id WHERE f.repo_id = r.id),
	COALESCE((SELECT MAX(a.created_at) FROM audit_log a WHERE a.repo_id = r.id
		AND a.action NOT IN ('reveal_secret', 'export_file', 'copy_value')), r.created_at)
	FROM repos r`

// coverageQuery counts present values per environment for every repo (or
// one, with ?1 set) in one statement: the latest version of each (key, env)
// found through the unique version index, deletions excluded. CROSS JOIN
// pins the order files, keys, versions, so SQLite seeks one version per cell
// instead of walking an environment's whole history.
const coverageQuery = `
SELECT e.repo_id, e.id, e.name, e.is_protected,
       (SELECT COUNT(*) FROM files f
        CROSS JOIN keys k ON k.file_id = f.id
        CROSS JOIN value_versions v ON v.key_id = k.id AND v.environment_id = e.id
        WHERE f.repo_id = e.repo_id AND v.ciphertext IS NOT NULL
          AND v.version = (SELECT MAX(m.version) FROM value_versions m WHERE m.key_id = k.id AND m.environment_id = e.id))
FROM environments e
WHERE ?1 = 0 OR e.repo_id = ?1
ORDER BY e.repo_id, e.position, e.id`

func scanRepo(row scanner) (*Repo, error) {
	r := Repo{Coverage: []EnvCoverage{}}
	err := row.Scan(&r.ID, &r.Name, &r.Description, &r.SourceURL, &r.CreatedBy, &r.CreatedAt, &r.FileCount, &r.EnvCount, &r.KeyCount, &r.LastChangeAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &r, nil
}

// addCoverage fills Coverage for repos (all of them when repoID is 0).
func (s *Store) addCoverage(ctx context.Context, repoID int64, repos []Repo) error {
	byID := make(map[int64]*Repo, len(repos))
	for i := range repos {
		byID[repos[i].ID] = &repos[i]
	}
	rows, err := s.db.QueryContext(ctx, coverageQuery, repoID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var rid int64
		var c EnvCoverage
		if err := rows.Scan(&rid, &c.EnvironmentID, &c.Name, &c.IsProtected, &c.Present); err != nil {
			return err
		}
		if r := byID[rid]; r != nil {
			r.Coverage = append(r.Coverage, c)
		}
	}
	return rows.Err()
}

// Repos lists every repo with its counts and coverage.
func (s *Store) Repos(ctx context.Context) ([]Repo, error) {
	rows, err := s.db.QueryContext(ctx, repoSelect+` ORDER BY r.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	repos := []Repo{}
	for rows.Next() {
		r, err := scanRepo(rows)
		if err != nil {
			return nil, err
		}
		repos = append(repos, *r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return repos, s.addCoverage(ctx, 0, repos)
}

// Repo returns one repo with its counts and coverage.
func (s *Store) Repo(ctx context.Context, id int64) (*Repo, error) {
	r, err := scanRepo(s.db.QueryRowContext(ctx, repoSelect+` WHERE r.id = ?`, id))
	if err != nil {
		return nil, err
	}
	list := []Repo{*r}
	if err := s.addCoverage(ctx, id, list); err != nil {
		return nil, err
	}
	return &list[0], nil
}

// DefaultEnvironments are created with every repo; prod is protected.
var DefaultEnvironments = []struct {
	Name      string
	Protected bool
}{{"test", false}, {"uat", false}, {"prod", true}}

// CreateRepo inserts a repo with the default environments.
func (s *Store) CreateRepo(ctx context.Context, name, description, sourceURL string, userID int64) (*Repo, error) {
	var id int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO repos (name, description, source_url, created_by, created_at) VALUES (?, ?, ?, ?, ?)`,
			name, description, sourceURL, nullID(userID), s.now())
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		for i, e := range DefaultEnvironments {
			if _, err := tx.ExecContext(ctx, `INSERT INTO environments (repo_id, name, position, is_protected) VALUES (?, ?, ?, ?)`,
				id, e.Name, i, e.Protected); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return s.Repo(ctx, id)
}

// UpdateRepo changes any of the name, description and source URL.
func (s *Store) UpdateRepo(ctx context.Context, id int64, name, description, sourceURL *string) (*Repo, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE repos SET name = COALESCE(?, name), description = COALESCE(?, description),
		source_url = COALESCE(?, source_url) WHERE id = ?`, name, description, sourceURL, id)
	if err := affected(res, err); err != nil {
		return nil, err
	}
	return s.Repo(ctx, id)
}

// DeleteRepo removes a repo and everything in it.
func (s *Store) DeleteRepo(ctx context.Context, id int64) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM repos WHERE id = ?`, id))
}

func affected(res sql.Result, err error) error {
	if err != nil {
		return mapErr(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Environment is a column of the grid (test, uat, prod...).
type Environment struct {
	ID          int64  `json:"id"`
	RepoID      int64  `json:"repoId"`
	Name        string `json:"name"`
	Position    int    `json:"position"`
	IsProtected bool   `json:"isProtected"`
}

const envCols = `id, repo_id, name, position, is_protected`

func scanEnv(row scanner) (*Environment, error) {
	var e Environment
	if err := row.Scan(&e.ID, &e.RepoID, &e.Name, &e.Position, &e.IsProtected); err != nil {
		return nil, mapErr(err)
	}
	return &e, nil
}

// Environments lists a repo's environments in display order.
func (s *Store) Environments(ctx context.Context, repoID int64) ([]Environment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+envCols+` FROM environments WHERE repo_id = ? ORDER BY position, id`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	envs := []Environment{}
	for rows.Next() {
		e, err := scanEnv(rows)
		if err != nil {
			return nil, err
		}
		envs = append(envs, *e)
	}
	return envs, rows.Err()
}

// Environment returns one environment.
func (s *Store) Environment(ctx context.Context, id int64) (*Environment, error) {
	return scanEnv(s.db.QueryRowContext(ctx, `SELECT `+envCols+` FROM environments WHERE id = ?`, id))
}

// CreateEnvironment appends an environment to a repo.
func (s *Store) CreateEnvironment(ctx context.Context, repoID int64, name string, protected bool) (*Environment, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM environments WHERE repo_id = ?`, repoID).Scan(&n); err != nil {
		return nil, err
	}
	if n >= MaxEnvironments {
		return nil, fmt.Errorf("%w: a repo holds at most %d environments", ErrTooMany, MaxEnvironments)
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO environments (repo_id, name, position, is_protected)
		VALUES (?, ?, (SELECT COALESCE(MAX(position) + 1, 0) FROM environments WHERE repo_id = ?), ?)`,
		repoID, name, repoID, protected)
	if err != nil {
		return nil, mapErr(err)
	}
	id, _ := res.LastInsertId()
	return s.Environment(ctx, id)
}

// UpdateEnvironment renames and/or (un)protects an environment.
func (s *Store) UpdateEnvironment(ctx context.Context, id int64, name *string, protected *bool) (*Environment, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE environments SET name = COALESCE(?, name), is_protected = COALESCE(?, is_protected) WHERE id = ?`,
		name, protected, id)
	if err := affected(res, err); err != nil {
		return nil, err
	}
	return s.Environment(ctx, id)
}

// DeleteEnvironment removes an environment and its values.
func (s *Store) DeleteEnvironment(ctx context.Context, id int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var repoID int64
		if err := tx.QueryRowContext(ctx, `DELETE FROM environments WHERE id = ? RETURNING repo_id`, id).Scan(&repoID); err != nil {
			return mapErr(err)
		}
		_, err := tx.ExecContext(ctx, recountBytes+`WHERE repo_id = ?`, repoID)
		return err
	})
}

// recountBytes recomputes files.value_bytes after a delete removed values
// through a cascade, which setValue never sees.
const recountBytes = `UPDATE files SET value_bytes = (
	SELECT COALESCE(SUM(length(v.ciphertext) - 28), 0)
	FROM keys k JOIN value_versions v ON v.key_id = k.id
	WHERE k.file_id = files.id AND v.ciphertext IS NOT NULL
	  AND v.version = (SELECT MAX(m.version) FROM value_versions m
	                   WHERE m.key_id = v.key_id AND m.environment_id = v.environment_id))
`

// ReorderEnvironments sets positions from the order of ids.
func (s *Store) ReorderEnvironments(ctx context.Context, repoID int64, ids []int64) error {
	return s.reorder(ctx, `SELECT id FROM environments WHERE repo_id = ?`, `UPDATE environments SET position = ? WHERE id = ?`, repoID, ids)
}

// reorder is shared by environments and keys: list selects the current ids
// under parentID, update sets one position.
func (s *Store) reorder(ctx context.Context, list, update string, parentID int64, ids []int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, list, parentID)
		if err != nil {
			return err
		}
		defer rows.Close()
		current := map[int64]bool{}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
			current[id] = true
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(ids) != len(current) {
			return ErrBadOrder
		}
		for i, id := range ids {
			if !current[id] {
				return ErrBadOrder
			}
			delete(current, id) // catches duplicates
			if _, err := tx.ExecContext(ctx, update, i, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// File is one config file within a repo.
type File struct {
	ID        int64  `json:"id"`
	RepoID    int64  `json:"repoId"`
	Name      string `json:"name"`
	Format    string `json:"format"`
	CreatedAt string `json:"createdAt"`
	KeyCount  int    `json:"keyCount"`
}

const fileSelect = `SELECT f.id, f.repo_id, f.name, f.format, f.created_at,
	(SELECT COUNT(*) FROM keys k WHERE k.file_id = f.id) FROM files f`

func scanFile(row scanner) (*File, error) {
	var f File
	if err := row.Scan(&f.ID, &f.RepoID, &f.Name, &f.Format, &f.CreatedAt, &f.KeyCount); err != nil {
		return nil, mapErr(err)
	}
	return &f, nil
}

// Files lists a repo's files by name.
func (s *Store) Files(ctx context.Context, repoID int64) ([]File, error) {
	rows, err := s.db.QueryContext(ctx, fileSelect+` WHERE f.repo_id = ? ORDER BY f.name`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := []File{}
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, *f)
	}
	return files, rows.Err()
}

// File returns one file.
func (s *Store) File(ctx context.Context, id int64) (*File, error) {
	return scanFile(s.db.QueryRowContext(ctx, fileSelect+` WHERE f.id = ?`, id))
}

// CreateFile inserts a file. Raw files get their single implicit key, which
// starts out secret because whole config bodies usually hold credentials.
func (s *Store) CreateFile(ctx context.Context, repoID int64, name, format string) (*File, error) {
	var id int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO files (repo_id, name, format, created_at) VALUES (?, ?, ?, ?)`,
			repoID, name, format, s.now())
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		if format == formats.Raw {
			_, err = tx.ExecContext(ctx, `INSERT INTO keys (file_id, name, description, is_secret, position) VALUES (?, ?, 'Whole file body', 1, 0)`,
				id, formats.RawKey)
		}
		return err
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return s.File(ctx, id)
}

// RenameFile changes a file's name. The format is fixed at creation.
func (s *Store) RenameFile(ctx context.Context, id int64, name string) (*File, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE files SET name = ? WHERE id = ?`, name, id)
	if err := affected(res, err); err != nil {
		return nil, err
	}
	return s.File(ctx, id)
}

// DeleteFile removes a file, its keys and their values.
func (s *Store) DeleteFile(ctx context.Context, id int64) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM files WHERE id = ?`, id))
}

// Key is a row of the grid.
type Key struct {
	ID          int64  `json:"id"`
	FileID      int64  `json:"fileId"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsSecret    bool   `json:"isSecret"`
	Position    int    `json:"position"`
	// Required keys must have a value in every environment and cannot have
	// one deleted. Pattern is a regular expression every value must contain
	// a match for (anchor it with ^ and $ to match the whole value).
	Required bool     `json:"required"`
	Pattern  string   `json:"pattern"`
	Tags     []string `json:"tags"`
}

const keyCols = `k.id, k.file_id, k.name, k.description, k.is_secret, k.position, k.required, k.pattern, k.tags`

func scanKey(row scanner) (*Key, error) {
	var k Key
	var tags string
	if err := row.Scan(&k.ID, &k.FileID, &k.Name, &k.Description, &k.IsSecret, &k.Position, &k.Required, &k.Pattern, &tags); err != nil {
		return nil, mapErr(err)
	}
	k.Tags = splitTags(tags)
	return &k, nil
}

// Tags are stored as one comma-separated column; they are short labels.
func splitTags(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, ",")
}

// Key returns one key.
func (s *Store) Key(ctx context.Context, id int64) (*Key, error) {
	return scanKey(s.db.QueryRowContext(ctx, `SELECT `+keyCols+` FROM keys k WHERE k.id = ?`, id))
}

// CreateKey appends a key to a file.
func (s *Store) CreateKey(ctx context.Context, fileID int64, name, description string, secret bool) (*Key, error) {
	return s.createKey(ctx, s.db, fileID, name, description, secret)
}

// MaxKeysPerFile and MaxEnvironments bound what one grid load, export or
// rollback has to read: a grid is environments times keys.
const (
	MaxKeysPerFile  = 2000
	MaxEnvironments = 50
)

func (s *Store) createKey(ctx context.Context, q querier, fileID int64, name, description string, secret bool) (*Key, error) {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM keys WHERE file_id = ?`, fileID).Scan(&n); err != nil {
		return nil, err
	}
	if n >= MaxKeysPerFile {
		return nil, fmt.Errorf("%w: a file holds at most %d keys", ErrTooMany, MaxKeysPerFile)
	}
	res, err := q.ExecContext(ctx, `INSERT INTO keys (file_id, name, description, is_secret, position)
		VALUES (?, ?, ?, ?, (SELECT COALESCE(MAX(position) + 1, 0) FROM keys WHERE file_id = ?))`,
		fileID, name, description, secret, fileID)
	if err != nil {
		return nil, mapErr(err)
	}
	id, _ := res.LastInsertId()
	return scanKey(q.QueryRowContext(ctx, `SELECT `+keyCols+` FROM keys k WHERE k.id = ?`, id))
}

// KeyPatch lists optional changes; nil fields are left alone.
type KeyPatch struct {
	Name        *string
	Description *string
	IsSecret    *bool
	Required    *bool
	Pattern     *string
	Tags        *[]string
}

// UpdateKey applies a patch.
func (s *Store) UpdateKey(ctx context.Context, id int64, p KeyPatch) (*Key, error) {
	var tags *string
	if p.Tags != nil {
		joined := strings.Join(*p.Tags, ",")
		tags = &joined
	}
	res, err := s.db.ExecContext(ctx, `UPDATE keys SET name = COALESCE(?, name), description = COALESCE(?, description),
		is_secret = COALESCE(?, is_secret), required = COALESCE(?, required), pattern = COALESCE(?, pattern),
		tags = COALESCE(?, tags) WHERE id = ?`, p.Name, p.Description, p.IsSecret, p.Required, p.Pattern, tags, id)
	if err := affected(res, err); err != nil {
		return nil, err
	}
	return s.Key(ctx, id)
}

// DeleteKey removes a key and all its versions.
func (s *Store) DeleteKey(ctx context.Context, id int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var fileID int64
		if err := tx.QueryRowContext(ctx, `DELETE FROM keys WHERE id = ? RETURNING file_id`, id).Scan(&fileID); err != nil {
			return mapErr(err)
		}
		_, err := tx.ExecContext(ctx, recountBytes+`WHERE id = ?`, fileID)
		return err
	})
}

// ReorderKeys sets positions from the order of ids.
func (s *Store) ReorderKeys(ctx context.Context, fileID int64, ids []int64) error {
	return s.reorder(ctx, `SELECT id FROM keys WHERE file_id = ?`, `UPDATE keys SET position = ? WHERE id = ?`, fileID, ids)
}

// KeyHasProtectedValues reports whether the key has any history or a pending
// change request in a protected environment. Renaming or deleting such a key
// changes that environment, or what an admin approves there, so it needs the
// same rights as writing a value there.
func (s *Store) KeyHasProtectedValues(ctx context.Context, keyID int64) (bool, error) {
	return s.hasProtectedState(ctx, `k.id = ?`, keyID)
}

// FileHasProtectedValues and RepoHasProtectedValues apply the same rule to
// renaming a file or repo, which moves the name its protected values are
// exported under.
func (s *Store) FileHasProtectedValues(ctx context.Context, fileID int64) (bool, error) {
	return s.hasProtectedState(ctx, `k.file_id = ?`, fileID)
}

func (s *Store) RepoHasProtectedValues(ctx context.Context, repoID int64) (bool, error) {
	return s.hasProtectedState(ctx, `k.file_id IN (SELECT id FROM files WHERE repo_id = ?)`, repoID)
}

// where is one of the constant conditions above, never user input.
func (s *Store) hasProtectedState(ctx context.Context, where string, id int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM keys k JOIN value_versions v ON v.key_id = k.id
			JOIN environments e ON e.id = v.environment_id AND e.is_protected = 1 WHERE `+where+`)
		+ (SELECT COUNT(*) FROM keys k JOIN change_requests cr ON cr.key_id = k.id AND cr.status = 'pending'
			JOIN environments e ON e.id = cr.environment_id AND e.is_protected = 1 WHERE `+where+`)`, id, id).Scan(&n)
	return n > 0, err
}
