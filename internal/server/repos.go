package server

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/Achal13jain/envgrid/internal/formats"
	"github.com/Achal13jain/envgrid/internal/store"
)

// Permission model (see README): any signed-in user may read everything and
// write unprotected environments. Protected environments, deleting repos,
// files and environments, and changing protection are admin-only.

func canWrite(u *store.User, env *store.Environment) bool {
	return u.IsAdmin() || !env.IsProtected
}

func requireWrite(r *http.Request, env *store.Environment) error {
	if !canWrite(currentUser(r), env) {
		return &apiError{http.StatusForbidden, "protected_environment", env.Name + " is protected: only admins can change it"}
	}
	return nil
}

func adminOnly(r *http.Request, what string) error {
	if !currentUser(r).IsAdmin() {
		return forbidden("only admins can %s", what)
	}
	return nil
}

// repos

func (s *Server) loadRepo(r *http.Request) (*store.Repo, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	return s.store.Repo(r.Context(), id)
}

func (s *Server) listRepos(w http.ResponseWriter, r *http.Request) error {
	repos, err := s.store.Repos(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, repos)
}

func (s *Server) getRepo(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.loadRepo(r)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, repo)
}

// checkSourceURL accepts an empty value or an http(s) address, adding
// https:// when the scheme is left out. Anything else (javascript:, file:)
// is refused, because the UI renders the value as a link.
func checkSourceURL(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	if !strings.Contains(v, "://") {
		v = "https://" + v
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || len(v) > 500 {
		return "", badRequest("the repository URL must be a web address, such as https://github.com/acme/checkout")
	}
	return u.String(), nil
}

func (s *Server) createRepo(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		SourceURL   string `json:"sourceUrl"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	name, err := cleanName("name", in.Name, 100)
	if err != nil {
		return err
	}
	source, err := checkSourceURL(in.SourceURL)
	if err != nil {
		return err
	}
	repo, err := s.store.CreateRepo(r.Context(), name, strings.TrimSpace(in.Description), source, currentUser(r).ID)
	if err != nil {
		return err
	}
	if err := s.audit(r, store.AuditEntry{Action: "create_repo", RepoID: repo.ID, Detail: map[string]any{"repo": repo.Name}}); err != nil {
		return err
	}
	return writeJSON(w, http.StatusCreated, repo)
}

func (s *Server) updateRepo(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.loadRepo(r)
	if err != nil {
		return err
	}
	var in struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		SourceURL   *string `json:"sourceUrl"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if in.Name != nil {
		name, err := cleanName("name", *in.Name, 100)
		if err != nil {
			return err
		}
		if name != repo.Name {
			if err := s.requireRenameRight(r, s.store.RepoHasProtectedValues, repo.ID, "repo"); err != nil {
				return err
			}
		}
		in.Name = &name
	}
	if in.SourceURL != nil {
		source, err := checkSourceURL(*in.SourceURL)
		if err != nil {
			return err
		}
		in.SourceURL = &source
	}
	updated, err := s.store.UpdateRepo(r.Context(), repo.ID, in.Name, in.Description, in.SourceURL)
	if err != nil {
		return err
	}
	if err := s.audit(r, store.AuditEntry{Action: "update_repo", RepoID: repo.ID, Detail: map[string]any{"repo": updated.Name, "previousName": repo.Name}}); err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteRepo(w http.ResponseWriter, r *http.Request) error {
	if err := adminOnly(r, "delete repos"); err != nil {
		return err
	}
	repo, err := s.loadRepo(r)
	if err != nil {
		return err
	}
	if err := s.store.DeleteRepo(r.Context(), repo.ID); err != nil {
		return err
	}
	if err := s.audit(r, store.AuditEntry{Action: "delete_repo", RepoID: repo.ID, Detail: map[string]any{"repo": repo.Name}}); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// environments

// Environment names may look like branches (feature/login, qa 2); export
// file names replace anything unusual with '-'.
var envName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _./-]{0,62}$`)

func checkEnvName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !envName.MatchString(name) {
		return "", badRequest("environment names are 1 to 63 characters: letters, digits, spaces, '.', '_', '-' or '/', starting with a letter or digit")
	}
	return name, nil
}

func (s *Server) listEnvironments(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.loadRepo(r)
	if err != nil {
		return err
	}
	envs, err := s.store.Environments(r.Context(), repo.ID)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, envs)
}

func (s *Server) createEnvironment(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.loadRepo(r)
	if err != nil {
		return err
	}
	var in struct {
		Name        string `json:"name"`
		IsProtected bool   `json:"isProtected"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	name, err := checkEnvName(in.Name)
	if err != nil {
		return err
	}
	if in.IsProtected {
		if err := adminOnly(r, "create protected environments"); err != nil {
			return err
		}
	}
	env, err := s.store.CreateEnvironment(r.Context(), repo.ID, name, in.IsProtected)
	if err != nil {
		return err
	}
	if err := s.audit(r, store.AuditEntry{Action: "create_environment", RepoID: repo.ID, EnvironmentID: env.ID,
		Detail: map[string]any{"repo": repo.Name, "env": env.Name, "protected": env.IsProtected}}); err != nil {
		return err
	}
	return writeJSON(w, http.StatusCreated, env)
}

func (s *Server) loadEnvironment(r *http.Request, param string) (*store.Environment, error) {
	id, err := pathID(r, param)
	if err != nil {
		return nil, err
	}
	return s.store.Environment(r.Context(), id)
}

func (s *Server) updateEnvironment(w http.ResponseWriter, r *http.Request) error {
	env, err := s.loadEnvironment(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Name        *string `json:"name"`
		IsProtected *bool   `json:"isProtected"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if in.IsProtected != nil && *in.IsProtected != env.IsProtected {
		if err := adminOnly(r, "change environment protection"); err != nil {
			return err
		}
	}
	if in.Name != nil {
		name, err := checkEnvName(*in.Name)
		if err != nil {
			return err
		}
		if err := requireWrite(r, env); err != nil {
			return err
		}
		in.Name = &name
	}
	updated, err := s.store.UpdateEnvironment(r.Context(), env.ID, in.Name, in.IsProtected)
	if err != nil {
		return err
	}
	if err := s.audit(r, store.AuditEntry{Action: "update_environment", RepoID: env.RepoID, EnvironmentID: env.ID,
		Detail: map[string]any{"env": updated.Name, "previousName": env.Name, "protected": updated.IsProtected}}); err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteEnvironment(w http.ResponseWriter, r *http.Request) error {
	if err := adminOnly(r, "delete environments"); err != nil {
		return err
	}
	env, err := s.loadEnvironment(r, "id")
	if err != nil {
		return err
	}
	envs, err := s.store.Environments(r.Context(), env.RepoID)
	if err != nil {
		return err
	}
	if len(envs) <= 1 {
		return badRequest("a repo needs at least one environment")
	}
	if err := s.store.DeleteEnvironment(r.Context(), env.ID); err != nil {
		return err
	}
	if err := s.audit(r, store.AuditEntry{Action: "delete_environment", RepoID: env.RepoID, EnvironmentID: env.ID,
		Detail: map[string]any{"env": env.Name}}); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

type reorderInput struct {
	IDs []int64 `json:"ids"`
}

func (s *Server) reorderEnvironments(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.loadRepo(r)
	if err != nil {
		return err
	}
	var in reorderInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if err := s.store.ReorderEnvironments(r.Context(), repo.ID, in.IDs); err != nil {
		return err
	}
	if err := s.audit(r, store.AuditEntry{Action: "reorder_environments", RepoID: repo.ID, Detail: map[string]any{"repo": repo.Name}}); err != nil {
		return err
	}
	envs, err := s.store.Environments(r.Context(), repo.ID)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, envs)
}

// files

func checkFileName(name string) (string, error) {
	name, err := cleanName("file name", name, 200)
	if err != nil {
		return "", err
	}
	if strings.ContainsAny(name, `/\"`) {
		return "", badRequest("file names must not contain '/', '\\' or '\"'")
	}
	return name, nil
}

func (s *Server) loadFile(r *http.Request) (*store.File, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	return s.store.File(r.Context(), id)
}

func (s *Server) listFiles(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.loadRepo(r)
	if err != nil {
		return err
	}
	files, err := s.store.Files(r.Context(), repo.ID)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, files)
}

func (s *Server) getFile(w http.ResponseWriter, r *http.Request) error {
	f, err := s.loadFile(r)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, f)
}

func (s *Server) createFile(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.loadRepo(r)
	if err != nil {
		return err
	}
	var in struct {
		Name   string `json:"name"`
		Format string `json:"format"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	name, err := checkFileName(in.Name)
	if err != nil {
		return err
	}
	if !formats.Valid(in.Format) {
		return badRequest("format must be one of dotenv, json, yaml, properties, ini, php, raw")
	}
	f, err := s.store.CreateFile(r.Context(), repo.ID, name, in.Format)
	if err != nil {
		return err
	}
	if err := s.audit(r, store.AuditEntry{Action: "create_file", RepoID: repo.ID, FileID: f.ID,
		Detail: map[string]any{"repo": repo.Name, "file": f.Name, "format": f.Format}}); err != nil {
		return err
	}
	return writeJSON(w, http.StatusCreated, f)
}

func (s *Server) updateFile(w http.ResponseWriter, r *http.Request) error {
	f, err := s.loadFile(r)
	if err != nil {
		return err
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	name, err := checkFileName(in.Name)
	if err != nil {
		return err
	}
	if name != f.Name {
		if err := s.requireRenameRight(r, s.store.FileHasProtectedValues, f.ID, "file"); err != nil {
			return err
		}
	}
	updated, err := s.store.RenameFile(r.Context(), f.ID, name)
	if err != nil {
		return err
	}
	if err := s.audit(r, store.AuditEntry{Action: "update_file", RepoID: f.RepoID, FileID: f.ID,
		Detail: map[string]any{"file": updated.Name, "previousName": f.Name}}); err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteFile(w http.ResponseWriter, r *http.Request) error {
	if err := adminOnly(r, "delete files"); err != nil {
		return err
	}
	f, err := s.loadFile(r)
	if err != nil {
		return err
	}
	if err := s.store.DeleteFile(r.Context(), f.ID); err != nil {
		return err
	}
	if err := s.audit(r, store.AuditEntry{Action: "delete_file", RepoID: f.RepoID, FileID: f.ID, Detail: map[string]any{"file": f.Name}}); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// keys

// keyScope is a key with the file it belongs to.
type keyScope struct {
	key  *store.Key
	file *store.File
}

func (s *Server) loadKey(r *http.Request) (*keyScope, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	k, err := s.store.Key(r.Context(), id)
	if err != nil {
		return nil, err
	}
	f, err := s.store.File(r.Context(), k.FileID)
	if err != nil {
		return nil, err
	}
	return &keyScope{key: k, file: f}, nil
}

// requireStructuralWrite guards renaming or deleting a key: that changes
// every environment holding a value for it, protected ones included.
func (s *Server) requireStructuralWrite(r *http.Request, k *store.Key) error {
	if currentUser(r).IsAdmin() {
		return nil
	}
	has, err := s.store.KeyHasProtectedValues(r.Context(), k.ID)
	if err != nil {
		return err
	}
	if has {
		return &apiError{http.StatusForbidden, "protected_environment", "this key has values in a protected environment: only admins can rename or delete it"}
	}
	return nil
}

func (s *Server) createKey(w http.ResponseWriter, r *http.Request) error {
	f, err := s.loadFile(r)
	if err != nil {
		return err
	}
	var in struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		IsSecret    bool     `json:"isSecret"`
		Tags        []string `json:"tags"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if err := formats.ValidateKey(f.Format, in.Name); err != nil {
		return badRequest("%s", err.Error())
	}
	tags, err := store.NormalizeTags(in.Tags)
	if err != nil {
		return badRequest("%s", err.Error())
	}
	k, err := s.store.CreateKey(r.Context(), f.ID, in.Name, strings.TrimSpace(in.Description), in.IsSecret)
	if err != nil {
		return err
	}
	if len(tags) > 0 {
		if k, err = s.store.UpdateKey(r.Context(), k.ID, store.KeyPatch{Tags: &tags}); err != nil {
			return err
		}
	}
	if err := s.audit(r, store.AuditEntry{Action: "create_key", RepoID: f.RepoID, FileID: f.ID, KeyID: k.ID,
		Detail: map[string]any{"file": f.Name, "key": k.Name, "secret": k.IsSecret}}); err != nil {
		return err
	}
	return writeJSON(w, http.StatusCreated, k)
}

func (s *Server) updateKey(w http.ResponseWriter, r *http.Request) error {
	ks, err := s.loadKey(r)
	if err != nil {
		return err
	}
	var in struct {
		Name        *string   `json:"name"`
		Description *string   `json:"description"`
		IsSecret    *bool     `json:"isSecret"`
		Required    *bool     `json:"required"`
		Pattern     *string   `json:"pattern"`
		Tags        *[]string `json:"tags"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	var changed []string
	// Validation rules govern every environment, protected ones included,
	// so only admins set them. Existing values that break a new rule are
	// flagged in the grid rather than refused here.
	if (in.Required != nil && *in.Required != ks.key.Required) || (in.Pattern != nil && *in.Pattern != ks.key.Pattern) {
		if err := adminOnly(r, "change validation rules"); err != nil {
			return err
		}
		changed = append(changed, "rules")
	}
	if in.Pattern != nil {
		p := strings.TrimSpace(*in.Pattern)
		if err := store.ValidatePattern(p); err != nil {
			return badRequest("%s", err.Error())
		}
		in.Pattern = &p
	}
	if in.Tags != nil {
		tags, err := store.NormalizeTags(*in.Tags)
		if err != nil {
			return badRequest("%s", err.Error())
		}
		in.Tags = &tags
		changed = append(changed, "tags")
	}
	if in.Name != nil && *in.Name != ks.key.Name {
		if err := formats.ValidateKey(ks.file.Format, *in.Name); err != nil {
			return badRequest("%s", err.Error())
		}
		if err := s.requireStructuralWrite(r, ks.key); err != nil {
			return err
		}
		changed = append(changed, "name")
	}
	if in.Description != nil {
		d := strings.TrimSpace(*in.Description)
		in.Description = &d
		changed = append(changed, "description")
	}
	if in.IsSecret != nil && *in.IsSecret != ks.key.IsSecret {
		changed = append(changed, "secret")
	}
	k, err := s.store.UpdateKey(r.Context(), ks.key.ID, store.KeyPatch{
		Name: in.Name, Description: in.Description, IsSecret: in.IsSecret, Required: in.Required, Pattern: in.Pattern, Tags: in.Tags,
	})
	if err != nil {
		return err
	}
	if err := s.audit(r, store.AuditEntry{Action: "update_key", RepoID: ks.file.RepoID, FileID: ks.file.ID, KeyID: k.ID,
		Detail: map[string]any{
			"file": ks.file.Name, "key": k.Name, "previousName": ks.key.Name, "changed": changed, "secret": k.IsSecret,
			"required": k.Required, "pattern": k.Pattern,
		}}); err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, k)
}

func (s *Server) deleteKey(w http.ResponseWriter, r *http.Request) error {
	ks, err := s.loadKey(r)
	if err != nil {
		return err
	}
	if ks.file.Format == formats.Raw {
		return badRequest("the body of a raw file cannot be deleted; delete the file instead")
	}
	if err := s.requireStructuralWrite(r, ks.key); err != nil {
		return err
	}
	if err := s.store.DeleteKey(r.Context(), ks.key.ID); err != nil {
		return err
	}
	if err := s.audit(r, store.AuditEntry{Action: "delete_key", RepoID: ks.file.RepoID, FileID: ks.file.ID, KeyID: ks.key.ID,
		Detail: map[string]any{"file": ks.file.Name, "key": ks.key.Name}}); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) reorderKeys(w http.ResponseWriter, r *http.Request) error {
	f, err := s.loadFile(r)
	if err != nil {
		return err
	}
	var in reorderInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	// Reordering a large file is a bulk write too, so it shares the slot.
	if !s.importing.TryLock() {
		w.Header().Set("Retry-After", "2")
		return &apiError{http.StatusTooManyRequests, "busy", "another bulk change is running, try again in a moment"}
	}
	defer s.importing.Unlock()
	if err := s.store.ReorderKeys(r.Context(), f.ID, in.IDs); err != nil {
		return err
	}
	if err := s.audit(r, store.AuditEntry{Action: "reorder_keys", RepoID: f.RepoID, FileID: f.ID, Detail: map[string]any{"file": f.Name}}); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// requireRenameRight keeps members from renaming a file or repo that holds
// protected values: the CLI finds files by name, so a rename changes what a
// protected environment delivers, like renaming a key does.
func (s *Server) requireRenameRight(r *http.Request, has func(context.Context, int64) (bool, error), id int64, what string) error {
	if currentUser(r).IsAdmin() {
		return nil
	}
	yes, err := has(r.Context(), id)
	if err != nil {
		return err
	}
	if yes {
		return &apiError{http.StatusForbidden, "protected_environment", "this " + what + " has values in a protected environment: only admins can rename it"}
	}
	return nil
}
