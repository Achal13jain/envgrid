package server

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Achal13jain/envgrid/internal/formats"
	"github.com/Achal13jain/envgrid/internal/store"
)

// maxValue bounds a single value (a raw file body included).
const maxValue = 1 << 20

// cellJSON is the only shape a cell leaves the server in. Value is nil when
// the cell is missing or masked; plaintext of secrets is only sent by the
// audited reveal, export and compare-with-reveal endpoints.
type cellJSON struct {
	EnvironmentID int64          `json:"environmentId"`
	Present       bool           `json:"present"`
	Masked        bool           `json:"masked"`
	Value         *string        `json:"value"`
	Version       int            `json:"version"`
	UpdatedAt     string         `json:"updatedAt,omitempty"`
	UpdatedBy     *store.UserRef `json:"updatedBy"`
	// Group numbers the distinct values of a key across environments in the
	// grid (0 for the first value seen, left to right), so the UI can mark
	// which environments differ without seeing a secret. It reveals equality
	// only, as the compare view already does. Nil when missing.
	Group *int `json:"group,omitempty"`
	// Problem names a broken validation rule: "required" on a missing value
	// of a required key, "pattern" on a value that does not match.
	Problem string `json:"problem,omitempty"`
	// Pending counts change requests waiting for this cell (grid only).
	Pending int `json:"pending,omitempty"`
}

func toCell(c store.Cell, reveal bool) cellJSON {
	out := cellJSON{EnvironmentID: c.EnvironmentID, Present: c.Present, Version: c.Version, UpdatedAt: c.UpdatedAt, UpdatedBy: c.UpdatedBy}
	if c.Present {
		if reveal {
			v := c.Plaintext
			out.Value = &v
		} else {
			out.Masked = true
		}
	}
	return out
}

func (s *Server) grid(w http.ResponseWriter, r *http.Request) error {
	f, err := s.loadFile(r)
	if err != nil {
		return err
	}
	g, err := s.store.Grid(r.Context(), f.ID)
	if err != nil {
		return err
	}
	pending, err := s.store.PendingRequests(r.Context(), f.ID)
	if err != nil {
		return err
	}
	type rowJSON struct {
		Key   store.Key  `json:"key"`
		Cells []cellJSON `json:"cells"`
	}
	rows := make([]rowJSON, 0, len(g.Rows))
	for _, row := range g.Rows {
		cells := make([]cellJSON, len(row.Cells))
		groups := map[string]int{}
		for i, c := range row.Cells {
			cells[i] = toCell(c, !row.Key.IsSecret)
			cells[i].Pending = pending[[2]int64{row.Key.ID, c.EnvironmentID}]
			if c.Present {
				g, ok := groups[c.Plaintext]
				if !ok {
					g = len(groups)
					groups[c.Plaintext] = g
				}
				cells[i].Group = &g
				v := c.Plaintext
				cells[i].Problem = row.Key.RuleProblem(&v)
			} else {
				cells[i].Problem = row.Key.RuleProblem(nil)
			}
		}
		rows = append(rows, rowJSON{Key: row.Key, Cells: cells})
	}
	return writeJSON(w, http.StatusOK, map[string]any{"file": f, "environments": g.Environments, "rows": rows})
}

// valueScope is a key, its file and one environment of the file's repo.
type valueScope struct {
	keyScope
	env *store.Environment
}

func (s *Server) loadValueScope(r *http.Request) (*valueScope, error) {
	ks, err := s.loadKey(r)
	if err != nil {
		return nil, err
	}
	env, err := s.loadEnvironment(r, "envId")
	if err != nil {
		return nil, err
	}
	if env.RepoID != ks.file.RepoID {
		return nil, notFound()
	}
	return &valueScope{keyScope: *ks, env: env}, nil
}

// entry is an audit entry naming the key, file and environment.
func (vs *valueScope) entry(action string, extra map[string]any) store.AuditEntry {
	d := map[string]any{"file": vs.file.Name, "key": vs.key.Name, "env": vs.env.Name}
	for k, v := range extra {
		d[k] = v
	}
	return store.AuditEntry{Action: action, RepoID: vs.file.RepoID, FileID: vs.file.ID, KeyID: vs.key.ID, EnvironmentID: vs.env.ID, Detail: d}
}

// write stores value (nil deletes) and audits the change.
func (s *Server) write(w http.ResponseWriter, r *http.Request, vs *valueScope, value *string, extra map[string]any) error {
	if err := requireWrite(r, vs.env); err != nil {
		return err
	}
	if value != nil && len(*value) > maxValue {
		return &apiError{http.StatusRequestEntityTooLarge, "too_large", "values are limited to 1 MiB"}
	}
	ch, err := s.store.SetValue(r.Context(), vs.key.ID, vs.env.ID, value, currentUser(r).ID)
	if err != nil {
		return err
	}
	if ch.Changed {
		action := "update_value"
		switch {
		case value == nil:
			action = "delete_value"
		case !ch.WasPresent:
			action = "create_value"
		}
		if extra == nil {
			extra = map[string]any{}
		}
		extra["version"] = ch.Version
		if err := s.audit(r, vs.entry(action, extra)); err != nil {
			return err
		}
	}
	return writeJSON(w, http.StatusOK, map[string]any{"changed": ch.Changed, "version": ch.Version})
}

func (s *Server) setValue(w http.ResponseWriter, r *http.Request) error {
	vs, err := s.loadValueScope(r)
	if err != nil {
		return err
	}
	var in struct {
		Value *string `json:"value"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if in.Value == nil {
		return badRequest("value is required; use DELETE to remove a value")
	}
	if vs.file.Format == formats.PHP {
		// PHP values are expressions; a broken one would break the export.
		v := strings.TrimSpace(*in.Value)
		if err := formats.ValidateValue(formats.PHP, v); err != nil {
			return &apiError{http.StatusBadRequest, "invalid_value", "not a valid PHP value: " + err.Error() + ". Quote text, for example 'hello'"}
		}
		in.Value = &v
	}
	return s.write(w, r, vs, in.Value, nil)
}

func (s *Server) deleteValue(w http.ResponseWriter, r *http.Request) error {
	vs, err := s.loadValueScope(r)
	if err != nil {
		return err
	}
	return s.write(w, r, vs, nil, nil)
}

// reveal returns plaintext and is always audited, before the value is sent.
// purpose=copy records a clipboard copy (copy_value) instead of a reveal.
func (s *Server) reveal(w http.ResponseWriter, r *http.Request) error {
	vs, err := s.loadValueScope(r)
	if err != nil {
		return err
	}
	var (
		value   string
		present bool
		detail  = map[string]any{}
	)
	if v := r.URL.Query().Get("version"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return badRequest("version must be a positive number")
		}
		value, present, err = s.store.ValueAt(r.Context(), vs.key.ID, vs.env.ID, n)
		if err != nil {
			return err
		}
		detail["version"] = n
	} else if value, present, err = s.store.CurrentValue(r.Context(), vs.key.ID, vs.env.ID); err != nil {
		return err
	}
	if !present {
		return &apiError{http.StatusNotFound, "missing", "no value in this environment"}
	}
	action := "reveal_secret"
	if r.URL.Query().Get("purpose") == "copy" {
		action = "copy_value"
		detail["target"] = "clipboard"
	}
	if err := s.audit(r, vs.entry(action, detail)); err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, map[string]string{"value": value})
}

func (s *Server) history(w http.ResponseWriter, r *http.Request) error {
	vs, err := s.loadValueScope(r)
	if err != nil {
		return err
	}
	versions, err := s.store.History(r.Context(), vs.key.ID, vs.env.ID)
	if err != nil {
		return err
	}
	type versionJSON struct {
		Version   int            `json:"version"`
		Present   bool           `json:"present"`
		Masked    bool           `json:"masked"`
		Value     *string        `json:"value"`
		CreatedAt string         `json:"createdAt"`
		CreatedBy *store.UserRef `json:"createdBy"`
	}
	out := make([]versionJSON, len(versions))
	for i, v := range versions {
		c := toCell(store.Cell{Present: v.Present, Plaintext: v.Plaintext}, !vs.key.IsSecret)
		out[i] = versionJSON{Version: v.Version, Present: v.Present, Masked: c.Masked, Value: c.Value, CreatedAt: v.CreatedAt, CreatedBy: v.CreatedBy}
	}
	return writeJSON(w, http.StatusOK, map[string]any{"key": vs.key, "environment": vs.env, "versions": out})
}

// restoreValue writes an old version again as a new version, without the
// plaintext passing through the client.
func (s *Server) restoreValue(w http.ResponseWriter, r *http.Request) error {
	vs, err := s.loadValueScope(r)
	if err != nil {
		return err
	}
	var in struct {
		Version int `json:"version"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	value, present, err := s.store.ValueAt(r.Context(), vs.key.ID, vs.env.ID, in.Version)
	if err != nil {
		return err
	}
	var v *string
	if present {
		v = &value
	}
	return s.write(w, r, vs, v, map[string]any{"restoredFrom": in.Version})
}

// copyValue copies the current value of another environment into this one
// on the server, so the compare view never needs the plaintext.
func (s *Server) copyValue(w http.ResponseWriter, r *http.Request) error {
	vs, err := s.loadValueScope(r)
	if err != nil {
		return err
	}
	var in struct {
		FromEnvironmentID int64 `json:"fromEnvironmentId"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	src, err := s.store.Environment(r.Context(), in.FromEnvironmentID)
	if err != nil || src.RepoID != vs.file.RepoID {
		return badRequest("fromEnvironmentId must be an environment of the same repo")
	}
	value, present, err := s.store.CurrentValue(r.Context(), vs.key.ID, src.ID)
	if err != nil {
		return err
	}
	if !present {
		return badRequest("%s has no value for this key", src.Name)
	}
	return s.write(w, r, vs, &value, map[string]any{"copiedFrom": src.Name})
}

func (s *Server) compare(w http.ResponseWriter, r *http.Request) error {
	f, err := s.loadFile(r)
	if err != nil {
		return err
	}
	a, err := queryID(r, "a")
	if err != nil {
		return err
	}
	b, err := queryID(r, "b")
	if err != nil {
		return err
	}
	reveal := r.URL.Query().Get("reveal") == "true" || r.URL.Query().Get("reveal") == "1"
	g, err := s.store.Grid(r.Context(), f.ID)
	if err != nil {
		return err
	}
	c, err := store.Compare(g, a, b)
	if err != nil {
		return err
	}
	var envA, envB store.Environment
	for _, e := range g.Environments {
		if e.ID == a {
			envA = e
		}
		if e.ID == b {
			envB = e
		}
	}

	type itemJSON struct {
		Key store.Key `json:"key"`
		A   cellJSON  `json:"a"`
		B   cellJSON  `json:"b"`
	}
	var revealed []string
	convert := func(items []store.CompareItem) []itemJSON {
		out := make([]itemJSON, len(items))
		for i, it := range items {
			show := !it.Key.IsSecret || reveal
			if it.Key.IsSecret && reveal && (it.A.Present || it.B.Present) {
				revealed = append(revealed, it.Key.Name)
			}
			out[i] = itemJSON{Key: it.Key, A: toCell(it.A, show), B: toCell(it.B, show)}
		}
		return out
	}
	body := map[string]any{
		"a": envA, "b": envB,
		"missingInA": convert(c.MissingInA), "missingInB": convert(c.MissingInB),
		"different": convert(c.Different), "same": convert(c.Same),
	}
	if len(revealed) > 0 {
		if err := s.audit(r, store.AuditEntry{Action: "reveal_secret", RepoID: f.RepoID, FileID: f.ID, Detail: map[string]any{
			"file": f.Name, "via": "compare", "envs": []string{envA.Name, envB.Name}, "keys": revealed,
		}}); err != nil {
			return err
		}
	}
	return writeJSON(w, http.StatusOK, body)
}

// exportFormat resolves the requested format. Text formats convert into each
// other; raw and PHP files only export as themselves. For any file "raw"
// means "as stored".
func exportFormat(f *store.File, requested string) (string, error) {
	switch {
	case requested == "" || requested == formats.Raw || requested == f.Format:
		return f.Format, nil
	case !formats.ExportValid(requested):
		return "", badRequest("format must be one of dotenv, json, yaml, properties, ini, csv, php, raw")
	case !formats.Convertible(f.Format, requested):
		return "", badRequest("%s files can only be exported as %s", formats.Label(f.Format), formats.Label(f.Format))
	}
	return requested, nil
}

// unsafeFileChars are replaced in the environment part of export names, so
// branch-style names such as feature/login stay one file name.
var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func exportFileName(f *store.File, env, format string) string {
	env = unsafeFileChars.ReplaceAllString(env, "-")
	ext := filepath.Ext(f.Name)
	stem := strings.TrimSuffix(f.Name, ext)
	if stem == "" { // dotfiles: ".env" becomes ".env.prod"
		stem, ext = f.Name+"."+env, ""
	} else {
		stem += "." + env
	}
	if format != f.Format {
		ext = formats.Ext(format)
	}
	return stem + ext
}

func (s *Server) export(w http.ResponseWriter, r *http.Request) error {
	f, err := s.loadFile(r)
	if err != nil {
		return err
	}
	envID, err := queryID(r, "env")
	if err != nil {
		return err
	}
	format, err := exportFormat(f, r.URL.Query().Get("format"))
	if err != nil {
		return err
	}
	g, err := s.store.Grid(r.Context(), f.ID)
	if err != nil {
		return err
	}
	kvs, ok := g.Column(envID)
	if !ok {
		return badRequest("env must be an environment of this file's repo")
	}
	// ${...} references are filled in for text formats unless resolve=false.
	// PHP and plain text keep them: there ${...} already means something.
	if formats.Convertible(f.Format, f.Format) && r.URL.Query().Get("resolve") != "false" {
		if kvs, err = resolveReferences(g, envID); err != nil {
			return badRequest("cannot export: %s", err.Error())
		}
	}
	body, err := formats.Export(format, kvs)
	if err != nil {
		return badRequest("cannot export as %s: %s", format, err.Error())
	}
	env := g.Environments[0]
	for _, e := range g.Environments {
		if e.ID == envID {
			env = e
		}
	}
	if err := s.audit(r, store.AuditEntry{Action: "export_file", RepoID: f.RepoID, FileID: f.ID, EnvironmentID: env.ID,
		Detail: map[string]any{"file": f.Name, "env": env.Name, "format": format, "keys": len(kvs)}}); err != nil {
		return err
	}
	w.Header().Set("Content-Type", formats.ContentType(format))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": exportFileName(f, env.Name, format)}))
	// Served as a download with a non-HTML type and nosniff, so not rendered.
	_, err = w.Write(body) //nolint:gosec // G705: see above
	return err
}

// importFile parses a pasted file in the file's own format and upserts it
// into one environment.
func (s *Server) importFile(w http.ResponseWriter, r *http.Request) error {
	f, err := s.loadFile(r)
	if err != nil {
		return err
	}
	envID, err := queryID(r, "env")
	if err != nil {
		return err
	}
	env, err := s.store.Environment(r.Context(), envID)
	if err != nil || env.RepoID != f.RepoID {
		return badRequest("env must be an environment of this file's repo")
	}
	if err := requireWrite(r, env); err != nil {
		return err
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxValue))
	if err != nil {
		return &apiError{http.StatusRequestEntityTooLarge, "too_large", "imports are limited to 1 MiB"}
	}
	kvs, readAs, err := parseForFile(f, body)
	if err != nil {
		return err
	}
	if f.Format != formats.Raw {
		for _, kv := range kvs {
			if err := formats.ValidateKey(f.Format, kv.Key); err != nil {
				return badRequest("key %q: %s", kv.Key, err.Error())
			}
		}
	}
	// dryRun=true answers what an import would do and stores nothing.
	dryRun := r.URL.Query().Get("dryRun") == "true"
	release, err := s.startBulk(w, r)
	if err != nil {
		return err
	}
	defer release()
	res, err := s.store.Import(r.Context(), f, env.ID, kvs, store.AuditEntry{
		UserID: currentUser(r).ID, RepoID: f.RepoID, FileID: f.ID, EnvironmentID: env.ID,
		Detail: map[string]any{"file": f.Name, "env": env.Name},
	}, dryRun)
	if err != nil {
		return err
	}
	res.ReadAs = readAs
	return writeJSON(w, http.StatusOK, res)
}

// parseForFile reads an import in the file's own format. If that fails and
// the content is another text format (JSON pasted into a .env file, say), it
// is read that way instead and readAs names the format used. Otherwise the
// error says what the content looks like and what to do. Errors never quote
// the content.
func parseForFile(f *store.File, body []byte) ([]formats.KV, string, error) {
	kvs, err := formats.Parse(f.Format, body)
	if err == nil {
		return kvs, "", nil
	}
	var pe *formats.ParseError
	if !errors.As(err, &pe) {
		return nil, "", err
	}
	detected := formats.Detect("", body)
	// .env and .properties are both "key=value" lines and .properties reads
	// almost anything, so a broken .env must not slip through as .properties.
	lineFormats := func(f string) bool { return f == formats.Dotenv || f == formats.Properties }
	sameFamily := lineFormats(detected) && lineFormats(f.Format)
	if detected != f.Format && !sameFamily && formats.Convertible(detected, f.Format) {
		if alt, err := formats.Parse(detected, body); err == nil && len(alt) > 0 {
			return alt, detected, nil
		}
	}
	msg := pe.Error()
	switch {
	case sameFamily:
	case detected == formats.Raw:
		msg += fmt.Sprintf(". This looks like plain text rather than %s. To keep a text file whole, add it as a plain text file instead", formats.Label(f.Format))
	case detected != f.Format:
		msg += fmt.Sprintf(". This looks like %s, but %s is a %s file. Import it as a new %s file instead", formats.Label(detected), f.Name, formats.Label(f.Format), formats.Label(detected))
	}
	return nil, "", &apiError{http.StatusBadRequest, "parse_error", msg}
}

// detectFormat guesses the format of an upload or paste and counts its keys,
// so the UI can show what an import would do. It returns key names only,
// never values, and stores nothing.
// maxDetectedKeys caps the key names a detection returns.
const maxDetectedKeys = 1000

func (s *Server) detectFormat(w http.ResponseWriter, r *http.Request) error {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxValue))
	if err != nil {
		return &apiError{http.StatusRequestEntityTooLarge, "too_large", "files are limited to 1 MiB"}
	}
	format := formats.Detect(r.URL.Query().Get("name"), body)
	kvs, err := formats.Parse(format, body)
	out := map[string]any{"format": format, "label": formats.Label(format), "keys": len(kvs), "sample": []string{}}
	if err != nil {
		out["error"] = err.Error()
		out["keys"] = 0
	} else if format != formats.Raw {
		sample := make([]string, 0, min(len(kvs), maxDetectedKeys))
		for _, kv := range kvs {
			if len(sample) == cap(sample) {
				break
			}
			sample = append(sample, kv.Key)
		}
		out["sample"] = sample
	}
	return writeJSON(w, http.StatusOK, out)
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	num := func(name string) (int64, error) {
		if q.Get(name) == "" {
			return 0, nil
		}
		n, err := strconv.ParseInt(q.Get(name), 10, 64)
		if err != nil || n < 0 {
			return 0, badRequest("%s must be a number", name)
		}
		return n, nil
	}
	f := store.AuditFilter{Action: q.Get("action")}
	var err error
	if f.RepoID, err = num("repo"); err != nil {
		return err
	}
	if f.UserID, err = num("user"); err != nil {
		return err
	}
	if f.FileID, err = num("file"); err != nil {
		return err
	}
	if f.EnvID, err = num("env"); err != nil {
		return err
	}
	if f.KeyID, err = num("key"); err != nil {
		return err
	}
	for name, dst := range map[string]*time.Time{"since": &f.Since, "until": &f.Until} {
		if v := q.Get(name); v != "" {
			if *dst, err = time.Parse(time.RFC3339, v); err != nil {
				return badRequest("%s must be a date and time such as 2026-10-01T00:00:00Z", name)
			}
		}
	}
	if f.Before, err = num("cursor"); err != nil {
		return err
	}
	limit, err := num("limit")
	if err != nil {
		return err
	}
	f.Limit = int(limit)
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	if u := currentUser(r); !u.IsAdmin() {
		f.MemberID = u.ID
	}
	items, err := s.store.AuditLog(r.Context(), f)
	if err != nil {
		return err
	}
	var next *int64
	if len(items) == f.Limit {
		next = &items[len(items)-1].ID
	}
	return writeJSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}
