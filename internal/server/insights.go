package server

import (
	"crypto/sha256"
	"net/http"
	"strconv"
	"time"

	"github.com/Achal13jain/envgrid/internal/store"
)

// insightPlace locates one value.
type insightPlace struct {
	RepoID        int64          `json:"repoId"`
	RepoName      string         `json:"repoName"`
	FileID        int64          `json:"fileId"`
	FileName      string         `json:"fileName"`
	KeyID         int64          `json:"keyId"`
	Key           string         `json:"key"`
	EnvironmentID int64          `json:"environmentId"`
	Environment   string         `json:"environment"`
	UpdatedAt     string         `json:"updatedAt,omitempty"`
	UpdatedBy     *store.UserRef `json:"updatedBy,omitempty"`
	Problem       string         `json:"problem,omitempty"`
}

// insights reports secrets that have not changed for a while, secret values
// used in more than one place, and values that break their key's rules. It
// returns locations only, never values.
// ponytail: decrypts every value of the repos in scope per request; cache
// or precompute when deployments grow to many thousands of values.
func (s *Server) insights(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	days := 90
	if v := q.Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 3650 {
			return badRequest("days must be between 1 and 3650")
		}
		days = n
	}
	var repos []store.Repo
	if q.Get("repo") != "" {
		repoID, err := queryID(r, "repo")
		if err != nil {
			return err
		}
		repo, err := s.store.Repo(r.Context(), repoID)
		if err != nil {
			return err
		}
		repos = []store.Repo{*repo}
	} else {
		all, err := s.store.Repos(r.Context())
		if err != nil {
			return err
		}
		repos = all
	}

	cutoff := s.store.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Format("2006-01-02T15:04:05.000Z")
	stale := []insightPlace{}
	problems := []insightPlace{}
	byValue := map[[32]byte][]insightPlace{} // by hash, so plaintexts are not all held at once
	var order [][32]byte
	for _, repo := range repos {
		files, err := s.store.Files(r.Context(), repo.ID)
		if err != nil {
			return err
		}
		for _, f := range files {
			g, err := s.store.Grid(r.Context(), f.ID)
			if err != nil {
				return err
			}
			for _, row := range g.Rows {
				for i, c := range row.Cells {
					env := g.Environments[i]
					place := insightPlace{RepoID: repo.ID, RepoName: repo.Name, FileID: f.ID, FileName: f.Name, KeyID: row.Key.ID,
						Key: row.Key.Name, EnvironmentID: env.ID, Environment: env.Name, UpdatedAt: c.UpdatedAt, UpdatedBy: c.UpdatedBy}
					var problem string
					if c.Present {
						v := c.Plaintext
						problem = row.Key.RuleProblem(&v)
					} else {
						problem = row.Key.RuleProblem(nil)
					}
					if problem != "" {
						p := place
						p.Problem = problem
						problems = append(problems, p)
					}
					if !row.Key.IsSecret || !c.Present || c.Plaintext == "" {
						continue
					}
					if c.UpdatedAt < cutoff {
						stale = append(stale, place)
					}
					h := sha256.Sum256([]byte(c.Plaintext))
					if _, seen := byValue[h]; !seen {
						order = append(order, h)
					}
					byValue[h] = append(byValue[h], place)
				}
			}
		}
	}
	reused := [][]insightPlace{}
	for _, v := range order {
		if places := byValue[v]; len(places) > 1 {
			reused = append(reused, places)
		}
	}
	pending, err := s.store.PendingCount(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, map[string]any{
		"days": days, "stale": stale, "reused": reused, "problems": problems, "pendingRequests": pending,
	})
}

// search finds repos, files and keys by name for the command palette.
func (s *Server) search(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query().Get("q")
	if len(q) > 200 {
		return badRequest("the search is limited to 200 characters")
	}
	hits, err := s.store.Search(r.Context(), q, 8)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, hits)
}

// auditSettings lists the kinds of activity and whether each is recorded.
func (s *Server) auditSettings(w http.ResponseWriter, r *http.Request) error {
	if err := adminOnly(r, "see which activity is recorded"); err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, s.store.AuditCategories())
}

// setAuditSettings chooses the kinds of activity that are not recorded.
func (s *Server) setAuditSettings(w http.ResponseWriter, r *http.Request) error {
	if err := adminOnly(r, "choose which activity is recorded"); err != nil {
		return err
	}
	var in struct {
		Off []string `json:"off"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	// Unknown or locked categories are the caller's mistake.
	if err := s.store.SetAuditOff(r.Context(), in.Off); err != nil {
		return badRequest("%s", err.Error())
	}
	if err := s.audit(r, store.AuditEntry{Action: "update_audit_settings", Detail: map[string]any{"notRecorded": in.Off}}); err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, s.store.AuditCategories())
}
