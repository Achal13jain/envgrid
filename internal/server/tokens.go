package server

import (
	"net/http"
	"time"

	"github.com/Achal13jain/envgrid/internal/store"
)

// Personal API tokens for scripts and the CLI. A token acts as its owner,
// with the same role and the same audit trail.

func (s *Server) listTokens(w http.ResponseWriter, r *http.Request) error {
	tokens, err := s.store.APITokens(r.Context(), currentUser(r).ID)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, tokens)
}

func (s *Server) createToken(w http.ResponseWriter, r *http.Request) error {
	// A leaked token must not be able to mint permanent new ones.
	if viaToken(r) {
		return forbidden("sign in to the web interface to create API tokens")
	}
	var in struct {
		Name          string `json:"name"`
		ExpiresInDays int    `json:"expiresInDays"` // 0 means no end date
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	name, err := cleanName("token name", in.Name, 100)
	if err != nil {
		return err
	}
	if in.ExpiresInDays < 0 || in.ExpiresInDays > 3650 {
		return badRequest("expiresInDays must be between 0 (no end date) and 3650")
	}
	var expires *time.Time
	if in.ExpiresInDays > 0 {
		t := s.store.Now().Add(time.Duration(in.ExpiresInDays) * 24 * time.Hour)
		expires = &t
	}
	token, t, err := s.store.CreateAPIToken(r.Context(), currentUser(r).ID, name, expires)
	if err != nil {
		return err
	}
	if err := s.audit(r, store.AuditEntry{Action: "create_token", Detail: map[string]any{"token": t.Name, "prefix": t.Prefix}}); err != nil {
		return err
	}
	// The token is returned once; only its hash is kept.
	return writeJSON(w, http.StatusCreated, map[string]any{"token": token, "info": t})
}

func (s *Server) deleteToken(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.store.DeleteAPIToken(r.Context(), id, currentUser(r).ID); err != nil {
		return err
	}
	if err := s.audit(r, store.AuditEntry{Action: "delete_token", Detail: map[string]any{"token": id}}); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// listUserTokens and deleteUserToken let an admin see and revoke one
// person's tokens, for example one found in a leaked script, without
// resetting that person's password.
func (s *Server) listUserTokens(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	tokens, err := s.store.APITokens(r.Context(), id)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, tokens)
}

func (s *Server) deleteUserToken(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	tokenID, err := pathID(r, "tokenId")
	if err != nil {
		return err
	}
	if err := s.store.DeleteAPIToken(r.Context(), tokenID, id); err != nil {
		return err
	}
	if err := s.audit(r, store.AuditEntry{Action: "delete_token", Detail: map[string]any{"token": tokenID, "owner": id}}); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// resolveNames turns repo, file and environment names into ids, so scripts
// can say "acme-shop backend.env prod" instead of numbers.
func (s *Server) resolveNames(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	repoName, fileName, envName := q.Get("repo"), q.Get("file"), q.Get("env")
	repos, err := s.store.Repos(r.Context())
	if err != nil {
		return err
	}
	out := map[string]int64{}
	var repoID int64
	for _, rp := range repos {
		if sameName(rp.Name, repoName) {
			repoID = rp.ID
			break
		}
	}
	if repoID == 0 {
		return &apiError{http.StatusNotFound, "not_found", "no repo named " + repoName}
	}
	out["repo"] = repoID
	if fileName != "" {
		files, err := s.store.Files(r.Context(), repoID)
		if err != nil {
			return err
		}
		for _, f := range files {
			if f.Name == fileName {
				out["file"] = f.ID
			}
		}
		if out["file"] == 0 {
			return &apiError{http.StatusNotFound, "not_found", "no file named " + fileName + " in " + repoName}
		}
	}
	if envName != "" {
		envs, err := s.store.Environments(r.Context(), repoID)
		if err != nil {
			return err
		}
		for _, e := range envs {
			if sameName(e.Name, envName) {
				out["env"] = e.ID
				break
			}
		}
		if out["env"] == 0 {
			return &apiError{http.StatusNotFound, "not_found", "no environment named " + envName + " in " + repoName}
		}
	}
	return writeJSON(w, http.StatusOK, out)
}

// sameName compares names the way the unique indexes do (SQLite NOCASE folds
// ASCII letters only). strings.EqualFold also folds look-alikes such as the
// long s, which would let a second repo answer for the same name.
func sameName(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}
