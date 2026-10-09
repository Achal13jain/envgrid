package store

import (
	"context"
	"strings"
)

// SearchHit is one repo, file or key whose name matches a search. Names and
// ids only: a search never touches values.
type SearchHit struct {
	Kind     string `json:"kind"` // repo, file or key
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	RepoID   int64  `json:"repoId"`
	RepoName string `json:"repoName"`
	FileID   int64  `json:"fileId,omitempty"`
	FileName string `json:"fileName,omitempty"`
}

// likeEscape makes a search term literal inside LIKE ... ESCAPE '\'.
var likeEscape = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// Search finds repos, files and keys whose names contain q, case
// insensitively, up to limit of each kind. Names that start with q come first.
func (s *Store) Search(ctx context.Context, q string, limit int) ([]SearchHit, error) {
	q = strings.TrimSpace(q)
	out := []SearchHit{}
	if q == "" {
		return out, nil
	}
	contains := "%" + likeEscape.Replace(q) + "%"
	prefix := likeEscape.Replace(q) + "%"
	queries := []struct{ kind, sql string }{
		{"repo", `SELECT r.id, r.name, r.id, r.name, 0, '' FROM repos r
			WHERE r.name LIKE ?1 ESCAPE '\' ORDER BY r.name NOT LIKE ?2 ESCAPE '\', r.name LIMIT ?3`},
		{"file", `SELECT f.id, f.name, r.id, r.name, f.id, f.name FROM files f JOIN repos r ON r.id = f.repo_id
			WHERE f.name LIKE ?1 ESCAPE '\' ORDER BY f.name NOT LIKE ?2 ESCAPE '\', f.name LIMIT ?3`},
		{"key", `SELECT k.id, k.name, r.id, r.name, f.id, f.name FROM keys k JOIN files f ON f.id = k.file_id JOIN repos r ON r.id = f.repo_id
			WHERE k.name LIKE ?1 ESCAPE '\' AND k.name <> '__raw__' ORDER BY k.name NOT LIKE ?2 ESCAPE '\', k.name LIMIT ?3`},
	}
	for _, qq := range queries {
		hits, err := s.searchOne(ctx, qq.kind, qq.sql, contains, prefix, limit)
		if err != nil {
			return nil, err
		}
		out = append(out, hits...)
	}
	return out, nil
}

func (s *Store) searchOne(ctx context.Context, kind, query, contains, prefix string, limit int) ([]SearchHit, error) {
	rows, err := s.db.QueryContext(ctx, query, contains, prefix, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchHit
	for rows.Next() {
		h := SearchHit{Kind: kind}
		if err := rows.Scan(&h.ID, &h.Name, &h.RepoID, &h.RepoName, &h.FileID, &h.FileName); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
