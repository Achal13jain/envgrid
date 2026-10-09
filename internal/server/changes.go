package server

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/Achal13jain/envgrid/internal/formats"
	"github.com/Achal13jain/envgrid/internal/store"
)

// Change requests let anyone propose a change that an admin approves. They
// are how members change protected environments, which they cannot write.

type requestJSON struct {
	store.ChangeRequest
	// Proposed is the proposed value; nil for a deletion or a masked secret.
	Proposed *string  `json:"proposed"`
	Masked   bool     `json:"masked"`
	Current  cellJSON `json:"current"`
}

func (s *Server) requestView(r *http.Request, c *store.ChangeRequest) (requestJSON, error) {
	out := requestJSON{ChangeRequest: *c}
	if !c.Delete {
		if c.IsSecret {
			out.Masked = true
		} else {
			v := c.Plaintext
			out.Proposed = &v
		}
	}
	value, present, err := s.store.CurrentValue(r.Context(), c.KeyID, c.EnvironmentID)
	if err != nil {
		return out, err
	}
	out.Current = toCell(store.Cell{EnvironmentID: c.EnvironmentID, Present: present, Plaintext: value}, !c.IsSecret)
	return out, nil
}

func (s *Server) loadRequest(r *http.Request) (*store.ChangeRequest, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	return s.store.ChangeRequest(r.Context(), id)
}

func requestDetail(c *store.ChangeRequest, extra map[string]any) map[string]any {
	d := map[string]any{"file": c.FileName, "key": c.KeyName, "env": c.EnvironmentName, "request": c.ID}
	for k, v := range extra {
		d[k] = v
	}
	return d
}

func requestEntry(action string, c *store.ChangeRequest, extra map[string]any) store.AuditEntry {
	return store.AuditEntry{Action: action, RepoID: c.RepoID, FileID: c.FileID, KeyID: c.KeyID, EnvironmentID: c.EnvironmentID, Detail: requestDetail(c, extra)}
}

// createRequest proposes setting (value) or deleting (delete) one value.
func (s *Server) createRequest(w http.ResponseWriter, r *http.Request) error {
	vs, err := s.loadValueScope(r)
	if err != nil {
		return err
	}
	var in struct {
		Value  *string `json:"value"`
		Delete bool    `json:"delete"`
		Reason string  `json:"reason"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if (in.Value == nil) == !in.Delete {
		return badRequest("propose either a value or a deletion")
	}
	reason := strings.TrimSpace(in.Reason)
	if utf8.RuneCountInString(reason) > 1000 {
		return badRequest("the reason is limited to 1000 characters")
	}
	if in.Value != nil {
		if len(*in.Value) > maxValue {
			return &apiError{http.StatusRequestEntityTooLarge, "too_large", "values are limited to 1 MiB"}
		}
		if vs.file.Format == formats.PHP {
			v := strings.TrimSpace(*in.Value)
			if err := formats.ValidateValue(formats.PHP, v); err != nil {
				return &apiError{http.StatusBadRequest, "invalid_value", "not a valid PHP value: " + err.Error() + ". Quote text, for example 'hello'"}
			}
			in.Value = &v
		}
	}
	c, err := s.store.CreateChangeRequest(r.Context(), vs.key.ID, vs.env.ID, in.Value, reason, currentUser(r).ID)
	if err != nil {
		return err
	}
	if err := s.audit(r, requestEntry("request_change", c, map[string]any{"delete": c.Delete})); err != nil {
		return err
	}
	view, err := s.requestView(r, c)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusCreated, view)
}

func (s *Server) listRequests(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	status := q.Get("status")
	switch status {
	case "":
		status = store.RequestPending
	case "all":
		status = ""
	case store.RequestPending, store.RequestApproved, store.RequestRejected, store.RequestCancelled:
	default:
		return badRequest("status must be pending, approved, rejected, cancelled or all")
	}
	var repoID int64
	if q.Get("repo") != "" {
		id, err := queryID(r, "repo")
		if err != nil {
			return err
		}
		repoID = id
	}
	list, err := s.store.ChangeRequests(r.Context(), store.RequestFilter{Status: status, RepoID: repoID})
	if err != nil {
		return err
	}
	out := make([]requestJSON, 0, len(list))
	for i := range list {
		v, err := s.requestView(r, &list[i])
		if err != nil {
			return err
		}
		out = append(out, v)
	}
	return writeJSON(w, http.StatusOK, out)
}

func (s *Server) countRequests(w http.ResponseWriter, r *http.Request) error {
	n, err := s.store.PendingCount(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, map[string]int{"pending": n})
}

// revealRequest returns a proposed secret, audited like any reveal.
func (s *Server) revealRequest(w http.ResponseWriter, r *http.Request) error {
	c, err := s.loadRequest(r)
	if err != nil {
		return err
	}
	if c.Delete {
		return &apiError{http.StatusNotFound, "missing", "this request proposes deleting the value"}
	}
	if err := s.audit(r, requestEntry("reveal_secret", c, map[string]any{"via": "change_request"})); err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, map[string]string{"value": c.Plaintext})
}

func (s *Server) decideRequest(approve bool) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		if err := adminOnly(r, "approve or reject change requests"); err != nil {
			return err
		}
		c, err := s.loadRequest(r)
		if err != nil {
			return err
		}
		var in struct {
			Note string `json:"note"`
		}
		if err := decode(w, r, &in); err != nil {
			return err
		}
		note := strings.TrimSpace(in.Note)
		if utf8.RuneCountInString(note) > 1000 {
			return badRequest("the note is limited to 1000 characters")
		}
		decided, ch, err := s.store.DecideChangeRequest(r.Context(), c.ID, approve, note, currentUser(r).ID)
		if err != nil {
			return err
		}
		action := "reject_change"
		if approve {
			action = "approve_change"
		}
		if err := s.audit(r, requestEntry(action, decided, map[string]any{"by": personLabel(decided.CreatedBy)})); err != nil {
			return err
		}
		if approve && ch.Changed {
			valueAction := "update_value"
			switch {
			case decided.Delete:
				valueAction = "delete_value"
			case !ch.WasPresent:
				valueAction = "create_value"
			}
			if err := s.audit(r, requestEntry(valueAction, decided, map[string]any{"via": "change_request", "version": ch.Version})); err != nil {
				return err
			}
		}
		view, err := s.requestView(r, decided)
		if err != nil {
			return err
		}
		return writeJSON(w, http.StatusOK, view)
	}
}

func (s *Server) cancelRequest(w http.ResponseWriter, r *http.Request) error {
	c, err := s.loadRequest(r)
	if err != nil {
		return err
	}
	u := currentUser(r)
	if !u.IsAdmin() && (c.CreatedBy == nil || c.CreatedBy.ID != u.ID) {
		return forbidden("only the person who made the request, or an admin, can cancel it")
	}
	if err := s.store.CancelChangeRequest(r.Context(), c.ID, u.ID); err != nil {
		return err
	}
	if err := s.audit(r, requestEntry("cancel_change", c, nil)); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func personLabel(u *store.UserRef) string {
	if u == nil {
		return ""
	}
	return u.Email
}

// requestErrors maps the change request sentinels for fail().
func requestError(err error) *apiError {
	switch {
	case errors.Is(err, store.ErrStale):
		return &apiError{http.StatusConflict, "stale", err.Error()}
	case errors.Is(err, store.ErrNotPending):
		return &apiError{http.StatusConflict, "not_pending", err.Error()}
	}
	return nil
}
