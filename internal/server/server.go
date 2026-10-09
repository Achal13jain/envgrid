// Package server is envgrid's HTTP layer: the JSON API under /api/v1 and the
// embedded single-page app at "/".
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/Achal13jain/envgrid/internal/store"
)

// Config holds the HTTP-level settings.
type Config struct {
	SecureCookies bool
	// TrustProxy takes the client IP from the last X-Forwarded-For entry,
	// the one appended by the reverse proxy in front of envgrid. Only enable
	// it behind exactly one proxy that sets that header.
	TrustProxy bool
	// Static is the built frontend. Without index.html the API still works
	// and "/" explains how to build the UI.
	Static  fs.FS
	Version string
}

// Server serves the API and the UI.
type Server struct {
	store *store.Store
	log   *slog.Logger
	cfg   Config
	login *limiter
	// stepUp limits current-password checks per account, like sign-in.
	stepUp *limiter
	// importing lets one bulk write (an import or a rollback) run at a time:
	// each holds SQLite's single write lock, so stacking them would make
	// other writes wait.
	importing sync.Mutex
}

// New builds a Server.
func New(st *store.Store, log *slog.Logger, cfg Config) *Server {
	return &Server{store: st, log: log, cfg: cfg, login: newLimiter(5, time.Minute), stepUp: newLimiter(5, time.Minute)}
}

const cookieName = "envgrid_session"

// sessionCookie is the cookie's name. With Secure cookies it carries the
// __Host- prefix, so a sibling subdomain cannot set or shadow it; browsers
// accept that prefix only on Secure cookies, so plain-HTTP trials keep the
// plain name.
func (s *Server) sessionCookie() string {
	if s.cfg.SecureCookies {
		return "__Host-" + cookieName
	}
	return cookieName
}

// Handler returns the root handler.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(s.logRequests, s.recoverPanics, securityHeaders)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(noStore, requireCustomHeader)
		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			s.fail(w, r, &apiError{http.StatusNotFound, "not_found", "no such endpoint"})
		})
		r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
			s.fail(w, r, &apiError{http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed"})
		})

		r.Get("/health", s.h(s.health))
		r.With(s.rateLimitLogin).Post("/auth/login", s.h(s.handleLogin))

		r.Group(func(r chi.Router) {
			r.Use(s.authenticate)
			r.Post("/auth/logout", s.h(s.handleLogout))
			r.Get("/auth/me", s.h(s.handleMe))
			r.Patch("/auth/me", s.h(s.handleUpdateMe))
			r.Get("/auth/tokens", s.h(s.listTokens))
			r.Post("/auth/tokens", s.h(s.createToken))
			r.Delete("/auth/tokens/{id}", s.h(s.deleteToken))
			r.Get("/resolve", s.h(s.resolveNames))

			r.Group(func(r chi.Router) {
				r.Use(requireAdmin)
				r.Get("/users", s.h(s.listUsers))
				r.Post("/users", s.h(s.createUser))
				r.Patch("/users/{id}", s.h(s.updateUser))
			})

			r.Get("/repos", s.h(s.listRepos))
			r.Post("/repos", s.h(s.createRepo))
			r.Get("/repos/{id}", s.h(s.getRepo))
			r.Patch("/repos/{id}", s.h(s.updateRepo))
			r.Delete("/repos/{id}", s.h(s.deleteRepo))

			r.Get("/repos/{id}/environments", s.h(s.listEnvironments))
			r.Post("/repos/{id}/environments", s.h(s.createEnvironment))
			r.Post("/repos/{id}/environments/reorder", s.h(s.reorderEnvironments))
			r.Patch("/environments/{id}", s.h(s.updateEnvironment))
			r.Delete("/environments/{id}", s.h(s.deleteEnvironment))

			r.Get("/repos/{id}/files", s.h(s.listFiles))
			r.Post("/repos/{id}/files", s.h(s.createFile))
			r.Get("/files/{id}", s.h(s.getFile))
			r.Patch("/files/{id}", s.h(s.updateFile))
			r.Delete("/files/{id}", s.h(s.deleteFile))
			r.Get("/files/{id}/grid", s.h(s.grid))
			r.Get("/files/{id}/compare", s.h(s.compare))
			r.Get("/files/{id}/export", s.h(s.export))
			r.Post("/files/{id}/import", s.h(s.importFile))
			r.Get("/files/{id}/rollback", s.h(s.rollbackPreview))
			r.Post("/files/{id}/rollback", s.h(s.rollback))
			r.Post("/formats/detect", s.h(s.detectFormat))

			r.Post("/files/{id}/keys", s.h(s.createKey))
			r.Post("/files/{id}/keys/reorder", s.h(s.reorderKeys))
			r.Patch("/keys/{id}", s.h(s.updateKey))
			r.Delete("/keys/{id}", s.h(s.deleteKey))

			r.Put("/keys/{id}/values/{envId}", s.h(s.setValue))
			r.Delete("/keys/{id}/values/{envId}", s.h(s.deleteValue))
			r.Get("/keys/{id}/values/{envId}/reveal", s.h(s.reveal))
			r.Get("/keys/{id}/values/{envId}/history", s.h(s.history))
			r.Post("/keys/{id}/values/{envId}/restore", s.h(s.restoreValue))
			r.Post("/keys/{id}/values/{envId}/copy", s.h(s.copyValue))
			r.Post("/keys/{id}/values/{envId}/requests", s.h(s.createRequest))

			r.Get("/change-requests", s.h(s.listRequests))
			r.Get("/change-requests/count", s.h(s.countRequests))
			r.Get("/change-requests/{id}/reveal", s.h(s.revealRequest))
			r.Post("/change-requests/{id}/approve", s.h(s.decideRequest(true)))
			r.Post("/change-requests/{id}/reject", s.h(s.decideRequest(false)))
			r.Post("/change-requests/{id}/cancel", s.h(s.cancelRequest))

			r.Get("/audit", s.h(s.listAudit))
			r.Get("/insights", s.h(s.insights))
			r.Get("/search", s.h(s.search))
			r.Get("/settings/audit", s.h(s.auditSettings))
			r.Put("/settings/audit", s.h(s.setAuditSettings))
		})
	})

	r.Handle("/*", s.spa())
	return r
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) error {
	if err := s.store.Ping(r.Context()); err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.cfg.Version})
}

// errors and JSON

type apiError struct {
	Status  int
	Code    string
	Message string
}

func (e *apiError) Error() string { return e.Message }

func badRequest(format string, args ...any) *apiError {
	return &apiError{http.StatusBadRequest, "bad_request", fmt.Sprintf(format, args...)}
}

func forbidden(format string, args ...any) *apiError {
	return &apiError{http.StatusForbidden, "forbidden", fmt.Sprintf(format, args...)}
}

func notFound() *apiError { return &apiError{http.StatusNotFound, "not_found", "not found"} }

// h adapts handlers that return errors. Every error goes through fail, which
// decides what the client may see.
func (s *Server) h(fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			s.fail(w, r, err)
		}
	}
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apiError
	switch {
	case errors.As(err, &ae):
	case requestError(err) != nil:
		ae = requestError(err)
	case errors.Is(err, store.ErrNotFound):
		ae = notFound()
	case errors.Is(err, store.ErrTooMany):
		ae = badRequest("%s", err.Error())
	case errors.Is(err, store.ErrConflict):
		ae = &apiError{http.StatusConflict, "conflict", "a record with that name already exists"}
	case errors.Is(err, store.ErrBadOrder):
		ae = badRequest("%s", store.ErrBadOrder.Error())
	case errors.Is(err, store.ErrLastAdmin):
		ae = &apiError{http.StatusConflict, "last_admin", store.ErrLastAdmin.Error()}
	case errors.Is(err, store.ErrUnknownEnvironment):
		ae = badRequest("%s", store.ErrUnknownEnvironment.Error())
	case errors.As(err, new(*store.RuleError)) || errors.As(err, new(*store.RuleErrors)):
		ae = &apiError{http.StatusBadRequest, "rule_failed", err.Error()}
	default:
		// Store errors carry ids and SQL, never values.
		s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err.Error())
		ae = &apiError{http.StatusInternalServerError, "internal", "internal error"}
	}
	_ = writeJSON(w, ae.Status, map[string]any{"error": map[string]string{"code": ae.Code, "message": ae.Message}})
}

func writeJSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	// The JSON content type and nosniff keep this out of HTML, so < > & need
	// no escaping, which would otherwise make such values six times larger.
	// Control characters are still escaped, as JSON requires.
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

const maxBody = 2 << 20

// decode reads a JSON body. The error never echoes the input.
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return &apiError{http.StatusRequestEntityTooLarge, "too_large", "request body too large"}
		}
		return badRequest("invalid JSON body")
	}
	return nil
}

func pathID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil || id <= 0 {
		return 0, notFound()
	}
	return id, nil
}

func queryID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.URL.Query().Get(name), 10, 64)
	if err != nil || id <= 0 {
		return 0, badRequest("query parameter %q must be an id", name)
	}
	return id, nil
}

// cleanName trims and checks a user-supplied name.
func cleanName(field, v string, maxLen int) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" || utf8.RuneCountInString(v) > maxLen {
		return "", badRequest("%s must be 1 to %d characters", field, maxLen)
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return "", badRequest("%s must not contain control characters", field)
		}
	}
	return v, nil
}

// middleware

type ctxKey int

const (
	userKey ctxKey = iota
	logKey
	viaTokenKey
)

// viaToken reports whether the request was authenticated by an API token.
func viaToken(r *http.Request) bool {
	v, _ := r.Context().Value(viaTokenKey).(bool)
	return v
}

// reqInfo lets inner middleware report the user to the outer request log.
type reqInfo struct{ userID int64 }

func currentUser(r *http.Request) *store.User {
	u, _ := r.Context().Value(userKey).(*store.User)
	return u
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// logRequests logs method, path, status and timing. It never logs query
// strings, headers or bodies.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		info := &reqInfo{}
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r.WithContext(context.WithValue(r.Context(), logKey, info)))
		s.log.Info("request", "method", r.Method, "path", r.URL.Path, "status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(), "user_id", info.userID, "ip", s.clientIP(r))
	})
}

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.log.Error("panic", "method", r.Method, "path", r.URL.Path, "panic", fmt.Sprintf("%T", v))
				s.fail(w, r, &apiError{http.StatusInternalServerError, "internal", "internal error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

const csp = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; " +
	"font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		// A self-hosted server is private: keep every page out of search engines.
		h.Set("X-Robots-Tag", "noindex, nofollow")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		next.ServeHTTP(w, r)
	})
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// requireCustomHeader is the CSRF defence on top of SameSite=Lax: a
// cross-site form or image cannot set custom headers, and a cross-site
// fetch that does would need a CORS preflight that this server never grants.
func requireCustomHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Every method needs the header, GET included: reveal, export and
		// compare are GETs that write audit rows or release plaintext, and a
		// cross-site link must not trigger them. Only the health check is open.
		if r.URL.Path != "/api/v1/health" {
			if r.Header.Get("X-Requested-With") == "" && !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":{"code":"csrf","message":"missing X-Requested-With header"}}` + "\n"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
			u, err := s.store.TokenUser(r.Context(), strings.TrimSpace(bearer))
			if err != nil {
				if !errors.Is(err, store.ErrNotFound) {
					s.fail(w, r, err)
					return
				}
				s.fail(w, r, &apiError{http.StatusUnauthorized, "unauthorized", "the API token is invalid, expired or revoked"})
				return
			}
			if info, ok := r.Context().Value(logKey).(*reqInfo); ok {
				info.userID = u.ID
			}
			ctx := context.WithValue(context.WithValue(r.Context(), userKey, u), viaTokenKey, true)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		c, err := r.Cookie(s.sessionCookie())
		if err != nil || c.Value == "" {
			s.fail(w, r, &apiError{http.StatusUnauthorized, "unauthorized", "sign in required"})
			return
		}
		u, err := s.store.SessionUser(r.Context(), c.Value)
		if err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				s.fail(w, r, err)
				return
			}
			s.fail(w, r, &apiError{http.StatusUnauthorized, "unauthorized", "session expired, sign in again"})
			return
		}
		if info, ok := r.Context().Value(logKey).(*reqInfo); ok {
			info.userID = u.ID
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	})
}

func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !currentUser(r).IsAdmin() {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"forbidden","message":"admin role required"}}` + "\n"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP is the peer address, or with TrustProxy the rightmost
// X-Forwarded-For entry. The rightmost entry is the one our proxy added, so
// a client cannot spoof it by sending its own header.
func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxy {
		if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
			parts := strings.Split(strings.Join(xff, ","), ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func (s *Server) rateLimitLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.login.allow(limiterKey(s.clientIP(r))) {
			w.Header().Set("Retry-After", "60")
			s.fail(w, r, &apiError{http.StatusTooManyRequests, "rate_limited", "too many sign-in attempts, wait a minute"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// limiter is a per-key sliding window kept in memory.
// ponytail: per-process state; a multi-instance deployment would need a
// shared store, but envgrid is one binary on one SQLite file.
type limiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	limit  int
	window time.Duration
	now    func() time.Time
}

func newLimiter(limit int, window time.Duration) *limiter {
	return &limiter{hits: map[string][]time.Time{}, limit: limit, window: window, now: time.Now}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	cutoff := now.Add(-l.window)
	h := l.hits[key]
	for len(h) > 0 && !h[0].After(cutoff) {
		h = h[1:]
	}
	if len(h) >= l.limit {
		l.hits[key] = h
		return false
	}
	l.hits[key] = append(h, now)
	if len(l.hits) > 10000 {
		for k, v := range l.hits {
			if !v[len(v)-1].After(cutoff) {
				delete(l.hits, k)
			}
		}
	}
	return true
}

// static UI

func (s *Server) spa() http.Handler {
	var index []byte
	if s.cfg.Static != nil {
		index, _ = fs.ReadFile(s.cfg.Static, "index.html")
	}
	if index == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("envgrid is running, but this binary was built without the web UI.\n" +
				"Build it with: (cd frontend && npm ci && npm run build) && go build .\n"))
		})
	}
	files := http.FileServerFS(s.cfg.Static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p != "" && p != "index.html" {
			if st, err := fs.Stat(s.cfg.Static, p); err == nil && !st.IsDir() {
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		// Client-side routes all get the app shell.
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
}

// limiterKey groups IPv6 clients by their /64, since one host usually holds a
// whole /64 and could otherwise get a fresh budget from every address.
func limiterKey(ip string) string {
	a, err := netip.ParseAddr(ip)
	if err != nil || !a.Is6() || a.Is4In6() {
		return ip
	}
	p, _ := a.Prefix(64)
	return p.String()
}
