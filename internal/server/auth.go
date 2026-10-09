package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Achal13jain/envgrid/internal/store"
)

// MinPasswordLength applies to every password set through envgrid.
const MinPasswordLength = 8

func checkPassword(p string) error {
	if utf8.RuneCountInString(p) < MinPasswordLength {
		return badRequest("password must be at least %d characters", MinPasswordLength)
	}
	if len(p) > 1024 {
		return badRequest("password is too long")
	}
	return nil
}

func checkEmail(e string) (string, error) {
	e = strings.TrimSpace(e)
	if len(e) > 254 || strings.Count(e, "@") != 1 || strings.HasPrefix(e, "@") || strings.HasSuffix(e, "@") || strings.ContainsAny(e, " \t\r\n") {
		return "", badRequest("a valid email address is required")
	}
	return e, nil
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	// Secure defaults to true; ENVGRID_SECURE_COOKIES=false exists for plain-HTTP local trials.
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure is configurable on purpose
		Name: s.sessionCookie(), Value: token, Path: "/", Expires: expires, MaxAge: int(time.Until(expires).Seconds()),
		HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID int64) error {
	token, expires, err := s.store.CreateSession(r.Context(), userID)
	if err != nil {
		return err
	}
	s.setSessionCookie(w, token, expires)
	return nil
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	u, err := s.store.Authenticate(r.Context(), strings.TrimSpace(in.Email), in.Password)
	if errors.Is(err, store.ErrInvalidCredentials) {
		// Attempts past the rate limit never reach here, so these rows stay bounded.
		email := strings.ToLower(strings.TrimSpace(in.Email))
		if len(email) > 254 {
			email = email[:254]
		}
		if err := s.store.Audit(r.Context(), store.AuditEntry{Action: "login_failed", Detail: map[string]any{"email": email, "ip": s.clientIP(r)}}); err != nil {
			return err
		}
		return &apiError{http.StatusUnauthorized, "invalid_credentials", "invalid email or password"}
	}
	if errors.Is(err, store.ErrAccessExpired) {
		return &apiError{http.StatusUnauthorized, "access_expired", "your access to envgrid has ended; ask an admin to extend it"}
	}
	if err != nil {
		return err
	}
	if err := s.startSession(w, r, u.ID); err != nil {
		return err
	}
	if info, ok := r.Context().Value(logKey).(*reqInfo); ok {
		info.userID = u.ID
	}
	if err := s.store.Audit(r.Context(), store.AuditEntry{UserID: u.ID, Action: "login", Detail: map[string]any{"ip": s.clientIP(r)}}); err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(s.sessionCookie()); err == nil {
		if err := s.store.DeleteSession(r.Context(), c.Value); err != nil {
			return err
		}
	}
	http.SetCookie(w, &http.Cookie{Name: s.sessionCookie(), Value: "", Path: "/", MaxAge: -1, HttpOnly: true, //nolint:gosec // G124: see setSessionCookie
		Secure: s.cfg.SecureCookies, SameSite: http.SameSiteLaxMode})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) error {
	return writeJSON(w, http.StatusOK, currentUser(r))
}

// handleUpdateMe changes the caller's own name or password. A password change
// ends every session, so a fresh one is issued for this browser.
func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Name            *string `json:"name"`
		CurrentPassword string  `json:"currentPassword"`
		NewPassword     *string `json:"newPassword"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	u := currentUser(r)
	patch := store.UserPatch{}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if utf8.RuneCountInString(name) > 100 {
			return badRequest("name must be at most 100 characters")
		}
		patch.Name = &name
	}
	if in.NewPassword != nil {
		// A token cannot turn itself into a password, a session and new tokens.
		if viaToken(r) {
			return forbidden("sign in to the web interface to change your password")
		}
		if err := checkPassword(*in.NewPassword); err != nil {
			return err
		}
		if !s.stepUp.allow(strconv.FormatInt(u.ID, 10)) {
			w.Header().Set("Retry-After", "60")
			return &apiError{http.StatusTooManyRequests, "rate_limited", "too many password attempts, wait a minute"}
		}
		if !s.store.CheckPassword(r.Context(), u.ID, in.CurrentPassword) {
			if err := s.audit(r, store.AuditEntry{Action: "password_check_failed", Detail: map[string]any{"email": u.Email}}); err != nil {
				return err
			}
			return &apiError{http.StatusBadRequest, "wrong_password", "current password is incorrect"}
		}
		patch.Password = in.NewPassword
	}
	updated, err := s.store.UpdateUser(r.Context(), u.ID, patch)
	if err != nil {
		return err
	}
	if patch.Password != nil {
		if err := s.startSession(w, r, u.ID); err != nil {
			return err
		}
		if err := s.audit(r, store.AuditEntry{Action: "update_user", Detail: map[string]any{"email": u.Email, "changed": []string{"password"}, "self": true}}); err != nil {
			return err
		}
	}
	return writeJSON(w, http.StatusOK, updated)
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) error {
	users, err := s.store.Users(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, users)
}

func validRole(role string) bool { return role == store.RoleAdmin || role == store.RoleMember }

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) error {
	// Like token creation, adding a person with a password needs a signed-in
	// admin, so a leaked token cannot mint itself a lasting account.
	if viaToken(r) {
		return forbidden("sign in to the web interface to add people")
	}
	var in struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	email, err := checkEmail(in.Email)
	if err != nil {
		return err
	}
	if err := checkPassword(in.Password); err != nil {
		return err
	}
	if in.Role == "" {
		in.Role = store.RoleMember
	}
	if !validRole(in.Role) {
		return badRequest("role must be admin or member")
	}
	u, err := s.store.CreateUser(r.Context(), email, strings.TrimSpace(in.Name), in.Password, in.Role)
	if err != nil {
		return err
	}
	if err := s.audit(r, store.AuditEntry{Action: "user_created", Detail: map[string]any{"email": u.Email, "role": u.Role}}); err != nil {
		return err
	}
	return writeJSON(w, http.StatusCreated, u)
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) error {
	// A token cannot re-enable, promote or extend an account either, since
	// any of those can turn a leaked token into a lasting sign-in.
	if viaToken(r) {
		return forbidden("sign in to the web interface to change people")
	}
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Name      *string `json:"name"`
		Role      *string `json:"role"`
		Disabled  *bool   `json:"disabled"`
		Password  *string `json:"password"`
		ExpiresAt *string `json:"expiresAt"` // RFC 3339, or "" for no end date
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	var changed []string
	if in.Name != nil {
		changed = append(changed, "name")
	}
	if in.Role != nil {
		if !validRole(*in.Role) {
			return badRequest("role must be admin or member")
		}
		changed = append(changed, "role")
	}
	if in.Disabled != nil {
		changed = append(changed, "disabled")
	}
	if in.Password != nil {
		if id == currentUser(r).ID {
			return badRequest("change your own password in Settings, which asks for the current one")
		}
		if err := checkPassword(*in.Password); err != nil {
			return err
		}
		changed = append(changed, "password")
	}
	if in.ExpiresAt != nil {
		if *in.ExpiresAt != "" {
			t, err := parseAt(*in.ExpiresAt)
			if err != nil {
				return badRequest("expiresAt must be a date and time such as 2026-12-31T23:59:00Z, or empty for no end date")
			}
			v := t.Format("2006-01-02T15:04:05.000Z")
			in.ExpiresAt = &v
		}
		changed = append(changed, "access end date")
	}
	u, err := s.store.UpdateUser(r.Context(), id, store.UserPatch{Name: in.Name, Role: in.Role, Disabled: in.Disabled, Password: in.Password, ExpiresAt: in.ExpiresAt})
	if err != nil {
		return err
	}
	if err := s.audit(r, store.AuditEntry{Action: "update_user", Detail: map[string]any{
		"email": u.Email, "changed": changed, "role": u.Role, "disabled": u.Disabled,
	}}); err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, u)
}

// audit records an event by the current user.
func (s *Server) audit(r *http.Request, e store.AuditEntry) error {
	e.UserID = currentUser(r).ID
	return s.store.Audit(r.Context(), e)
}
