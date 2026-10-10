package server

import (
	"net/http"
	"time"

	"github.com/Achal13jain/envgrid/internal/store"
)

// Restoring a file's environment to an earlier time: a preview of what would
// change, then a single transaction that writes the old values back as new
// versions, so history is never rewritten.

func parseAt(v string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, badRequest("at must be a date and time such as 2026-10-01T09:30:00Z")
	}
	return t.UTC(), nil
}

// rollbackScope loads the file and checks the environment belongs to it.
func (s *Server) rollbackScope(r *http.Request, envID int64) (*store.File, *store.Environment, error) {
	f, err := s.loadFile(r)
	if err != nil {
		return nil, nil, err
	}
	env, err := s.store.Environment(r.Context(), envID)
	if err != nil || env.RepoID != f.RepoID {
		return nil, nil, badRequest("env must be an environment of this file's repo")
	}
	return f, env, nil
}

func (s *Server) rollbackPreview(w http.ResponseWriter, r *http.Request) error {
	envID, err := queryID(r, "env")
	if err != nil {
		return err
	}
	at, err := parseAt(r.URL.Query().Get("at"))
	if err != nil {
		return err
	}
	f, env, err := s.rollbackScope(r, envID)
	if err != nil {
		return err
	}
	changes, err := s.store.RollbackPreview(r.Context(), f.ID, env.ID, at)
	if err != nil {
		return err
	}
	type changeJSON struct {
		Key     store.Key `json:"key"`
		Current cellJSON  `json:"current"`
		Then    cellJSON  `json:"then"`
	}
	out := make([]changeJSON, 0, len(changes))
	for _, c := range changes {
		show := !c.Then.Key.IsSecret
		out = append(out, changeJSON{
			Key:     c.Then.Key,
			Current: toCell(c.Current, show),
			Then:    toCell(store.Cell{EnvironmentID: env.ID, Present: c.Then.Present, Plaintext: c.Then.Plaintext, Version: c.Then.Version}, show),
		})
	}
	return writeJSON(w, http.StatusOK, map[string]any{"at": at.Format(time.RFC3339Nano), "environment": env, "changes": out})
}

func (s *Server) rollback(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Env int64  `json:"env"`
		At  string `json:"at"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	at, err := parseAt(in.At)
	if err != nil {
		return err
	}
	f, env, err := s.rollbackScope(r, in.Env)
	if err != nil {
		return err
	}
	if err := requireWrite(r, env); err != nil {
		return err
	}
	stamp := at.Format(time.RFC3339)
	release, err := s.startBulk(w, r)
	if err != nil {
		return err
	}
	defer release()
	changed, err := s.store.Rollback(r.Context(), f.ID, env.ID, at, store.AuditEntry{
		UserID: currentUser(r).ID, RepoID: f.RepoID, FileID: f.ID, EnvironmentID: env.ID,
		Detail: map[string]any{"file": f.Name, "env": env.Name, "restoredTo": stamp},
	})
	if err != nil {
		return err
	}
	if err := s.audit(r, store.AuditEntry{Action: "restore_environment", RepoID: f.RepoID, FileID: f.ID, EnvironmentID: env.ID,
		Detail: map[string]any{"file": f.Name, "env": env.Name, "restoredTo": stamp, "keys": len(changed)}}); err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, map[string]any{"changed": changed})
}
