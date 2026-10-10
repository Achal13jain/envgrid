package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Achal13jain/envgrid/internal/store"
)

// Regression tests for the fixes from the October 2026 security review.

func tokenFor(t *testing.T, c *client) string {
	t.Helper()
	return c.expect(http.StatusCreated, "POST", "/api/v1/auth/tokens", map[string]any{"name": "ci", "expiresInDays": 30})["token"].(string)
}

func withToken(e *env, tok, method, path, body string) int {
	resp, _ := e.anon().raw(method, path, strings.NewReader(body), map[string]string{"Authorization": "Bearer " + tok, "Content-Type": "application/json"})
	return resp.StatusCode
}

func TestRenamesOfProtectedFilesAndReposNeedAdmin(t *testing.T) {
	e := newEnv(t)
	x := e.seed()
	m := e.member("m@example.com")
	m.expect(http.StatusOK, "PATCH", fmt.Sprintf("/api/v1/files/%d", x.file), map[string]string{"name": "still-free.env"})
	e.admin.expect(http.StatusOK, "PUT", valuePath(x.plainKey, x.prod), map[string]string{"value": "8080"})
	m.expect(http.StatusForbidden, "PATCH", fmt.Sprintf("/api/v1/files/%d", x.file), map[string]string{"name": "other.env"})
	m.expect(http.StatusForbidden, "PATCH", fmt.Sprintf("/api/v1/repos/%d", x.repo), map[string]string{"name": "other"})
	e.admin.expect(http.StatusOK, "PATCH", fmt.Sprintf("/api/v1/repos/%d", x.repo), map[string]string{"name": "renamed"})
}

func TestKeyWithPendingProtectedRequestNeedsAdmin(t *testing.T) {
	e := newEnv(t)
	x := e.seed()
	m := e.member("m@example.com")
	m.expect(http.StatusCreated, "POST", valuePath(x.plainKey, x.prod)+"/requests", map[string]string{"value": "9090", "reason": "new port"})
	m.expect(http.StatusForbidden, "PATCH", fmt.Sprintf("/api/v1/keys/%d", x.plainKey), map[string]string{"name": "OTHER"})
	m.expect(http.StatusForbidden, "DELETE", fmt.Sprintf("/api/v1/keys/%d", x.plainKey), nil)
}

func TestPasswordResetEndsTokens(t *testing.T) {
	e := newEnv(t)
	m := e.member("m@example.com")
	tok := tokenFor(t, m)
	me := m.expect(http.StatusOK, "GET", "/api/v1/auth/me", nil)
	e.admin.expect(http.StatusOK, "PATCH", fmt.Sprintf("/api/v1/users/%d", id(me["id"])), map[string]string{"password": "reset-password-1"})
	if got := withToken(e, tok, "GET", "/api/v1/auth/me", ""); got != http.StatusUnauthorized {
		t.Fatalf("token after reset: %d", got)
	}
}

func TestTokensCannotSetPasswordsOrAddPeople(t *testing.T) {
	e := newEnv(t)
	tok := tokenFor(t, e.admin)
	if got := withToken(e, tok, "PATCH", "/api/v1/users/1", `{"password":"another-password"}`); got != http.StatusForbidden {
		t.Fatalf("token password reset: %d", got)
	}
	for _, body := range []string{`{"role":"member"}`, `{"disabled":false}`, `{"expiresAt":""}`} {
		if got := withToken(e, tok, "PATCH", "/api/v1/users/1", body); got != http.StatusForbidden {
			t.Fatalf("token account change %s: %d", body, got)
		}
	}
	if got := withToken(e, tok, "POST", "/api/v1/users", `{"email":"x@example.com","password":"long-enough-1","role":"admin"}`); got != http.StatusForbidden {
		t.Fatalf("token user creation: %d", got)
	}
	if got := withToken(e, tok, "PATCH", "/api/v1/auth/me", `{"currentPassword":"admin-password","newPassword":"another-password"}`); got != http.StatusForbidden {
		t.Fatalf("token own password change: %d", got)
	}
	// An admin changes their own password in Settings, which asks for the current one.
	e.admin.expect(http.StatusBadRequest, "PATCH", "/api/v1/users/1", map[string]string{"password": "another-password"})
}

func TestOwnPasswordChecksAreLimited(t *testing.T) {
	e := newEnv(t)
	status := 0
	for i := 0; i < 6; i++ {
		resp, _ := e.admin.do("PATCH", "/api/v1/auth/me", map[string]string{"currentPassword": "wrong-password", "newPassword": "another-password"})
		status = resp.StatusCode
	}
	if status != http.StatusTooManyRequests {
		t.Fatalf("sixth attempt: %d", status)
	}
}

func TestProtectedExportReadsOnlyProtectedEnvironments(t *testing.T) {
	e := newEnv(t)
	x := e.seed()
	host := id(e.admin.expect(http.StatusCreated, "POST", fmt.Sprintf("/api/v1/files/%d/keys", x.file), map[string]any{"name": "db.host"})["id"])
	url := id(e.admin.expect(http.StatusCreated, "POST", fmt.Sprintf("/api/v1/files/%d/keys", x.file), map[string]any{"name": "DB_URL"})["id"])
	e.admin.expect(http.StatusOK, "PUT", valuePath(host, x.prod), map[string]string{"value": "db.internal"})
	e.admin.expect(http.StatusOK, "PUT", valuePath(url, x.prod), map[string]string{"value": "pg://${db.host}/app ${uat.PORT}"})
	e.admin.expect(http.StatusOK, "PUT", valuePath(x.plainKey, x.uat), map[string]string{"value": "1234"})
	m := e.member("m@example.com")
	db := id(m.expect(http.StatusCreated, "POST", fmt.Sprintf("/api/v1/repos/%d/environments", x.repo), map[string]string{"name": "db"})["id"])
	other := id(m.expect(http.StatusCreated, "POST", fmt.Sprintf("/api/v1/files/%d/keys", x.file), map[string]any{"name": "host"})["id"])
	m.expect(http.StatusOK, "PUT", valuePath(other, db), map[string]string{"value": "elsewhere"})
	_, body := e.admin.do("GET", fmt.Sprintf("/api/v1/files/%d/export?env=%d", x.file, x.prod), nil)
	if !strings.Contains(string(body), "pg://db.internal/app ${uat.PORT}") {
		t.Fatalf("prod export: %s", body)
	}
}

func TestReferenceBudgetCountsEmptyExpansions(t *testing.T) {
	env := store.Environment{ID: 1, Name: "dev"}
	g := &store.Grid{Environments: []store.Environment{env}}
	for i := 0; i < 13; i++ {
		v := strings.Repeat(fmt.Sprintf("${K%d}", i+1), 8)
		if i == 12 {
			v = ""
		}
		g.Rows = append(g.Rows, store.GridRow{Key: store.Key{Name: fmt.Sprintf("K%d", i)}, Cells: []store.Cell{{EnvironmentID: 1, Present: true, Plaintext: v}}})
	}
	if _, err := resolveReferences(g, 1); err == nil || !strings.Contains(err.Error(), "MiB") {
		t.Fatalf("8^12 empty expansions: %v", err)
	}
}

func TestFailedPasswordsAreAudited(t *testing.T) {
	e := newEnv(t)
	e.anon().raw("POST", "/api/v1/auth/login", strings.NewReader(`{"email":"admin@example.com","password":"wrong-password"}`), map[string]string{"X-Requested-With": "envgrid", "Content-Type": "application/json"})
	e.admin.do("PATCH", "/api/v1/auth/me", map[string]string{"currentPassword": "wrong-password", "newPassword": "another-password"})
	_, body := e.admin.do("GET", "/api/v1/audit", nil)
	for _, action := range []string{`"login_failed"`, `"password_check_failed"`} {
		if !strings.Contains(string(body), action) {
			t.Fatalf("audit log lacks %s: %s", action, body)
		}
	}
}

func TestBulkChangesAreLimitedPerPerson(t *testing.T) {
	e := newEnv(t)
	x := e.seed()
	path := fmt.Sprintf("/api/v1/files/%d/import?env=%d&dryRun=true", x.file, x.test)
	status := 0
	for i := 0; i < 31; i++ {
		resp, _ := e.admin.raw("POST", path, strings.NewReader("PORT=1\n"), map[string]string{"X-Requested-With": "envgrid"})
		status = resp.StatusCode
	}
	if status != http.StatusTooManyRequests {
		t.Fatalf("31st bulk change in a minute: %d", status)
	}
}

func TestLookAlikeRepoNamesAreRefused(t *testing.T) {
	e := newEnv(t)
	e.admin.expect(http.StatusCreated, "POST", "/api/v1/repos", map[string]string{"name": "acme-shop"})
	e.admin.expect(http.StatusConflict, "POST", "/api/v1/repos", map[string]string{"name": "acme-\u017fhop"})
	other := id(e.admin.expect(http.StatusCreated, "POST", "/api/v1/repos", map[string]string{"name": "other"})["id"])
	e.admin.expect(http.StatusConflict, "PATCH", fmt.Sprintf("/api/v1/repos/%d", other), map[string]string{"name": "acme-\u017fhop"})
}

func TestAdminRevokesSomeoneElsesToken(t *testing.T) {
	e := newEnv(t)
	m := e.member("m@example.com")
	tok := tokenFor(t, m)
	me := m.expect(http.StatusOK, "GET", "/api/v1/auth/me", nil)
	path := fmt.Sprintf("/api/v1/users/%d/tokens", id(me["id"]))
	list := e.admin.expect(http.StatusOK, "GET", path, nil)["list"].([]any)
	if len(list) != 1 {
		t.Fatalf("tokens listed: %d", len(list))
	}
	m.expect(http.StatusForbidden, "GET", path, nil)
	e.admin.expect(http.StatusNoContent, "DELETE", fmt.Sprintf("%s/%d", path, id(list[0].(map[string]any)["id"])), nil)
	if got := withToken(e, tok, "GET", "/api/v1/auth/me", ""); got != http.StatusUnauthorized {
		t.Fatalf("revoked token still works: %d", got)
	}
}

func TestSameNameFoldsASCIIOnly(t *testing.T) {
	if !sameName("Shop", "sHOP") || sameName("\u017fhop", "shop") || sameName("ab", "abc") {
		t.Fatal("sameName must fold ASCII letters only")
	}
}

func TestLimiterKeyGroupsIPv6(t *testing.T) {
	if limiterKey("2001:db8:0:1::1") != limiterKey("2001:db8:0:1::7") || limiterKey("2001:db8:0:1::1") == limiterKey("2001:db8:0:2::1") {
		t.Fatal("IPv6 addresses must share a key per /64")
	}
	if limiterKey("192.0.2.1") == limiterKey("192.0.2.2") {
		t.Fatal("IPv4 addresses must keep separate keys")
	}
}
