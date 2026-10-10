package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Achal13jain/envgrid/internal/crypto"
	"github.com/Achal13jain/envgrid/internal/store"
)

func TestMain(m *testing.M) {
	crypto.PasswordParams = crypto.Argon2Params{Time: 1, Memory: 1024, Threads: 1}
	os.Exit(m.Run())
}

type env struct {
	t     *testing.T
	st    *store.Store
	app   *Server
	srv   *httptest.Server
	logs  *bytes.Buffer
	admin *client
}

// newEnv starts a server on a private in-memory database with an admin
// signed in. Every log line at debug level and above lands in env.logs.
func newEnv(t *testing.T) *env {
	t.Helper()
	key, _ := crypto.ParseKey(crypto.GenerateKey())
	c, _ := crypto.NewCipher(key)
	st, err := store.Open(context.Background(), ":memory:", c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	logs := &bytes.Buffer{}
	log := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	static := fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>envgrid</title>")}, "assets/app.js": {Data: []byte("console.log(1)")}}
	app := New(st, log, Config{Static: static, Version: "test"})
	srv := httptest.NewServer(app.Handler())
	t.Cleanup(srv.Close)
	e := &env{t: t, st: st, app: app, srv: srv, logs: logs}
	if _, err := st.CreateUser(context.Background(), "admin@example.com", "Admin", "admin-password", store.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	e.admin = e.login("admin@example.com", "admin-password")
	return e
}

type client struct {
	t  *testing.T
	e  *env
	hc *http.Client
}

func (e *env) anon() *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: e.t, e: e, hc: &http.Client{Jar: jar}}
}

func (e *env) login(email, password string) *client {
	e.t.Helper()
	c := e.anon()
	c.expect(http.StatusOK, "POST", "/api/v1/auth/login", map[string]string{"email": email, "password": password})
	return c
}

// member creates a member account and signs it in.
func (e *env) member(email string) *client {
	e.t.Helper()
	e.admin.expect(http.StatusCreated, "POST", "/api/v1/users", map[string]string{"email": email, "password": "member-password", "role": "member"})
	return e.login(email, "member-password")
}

func (c *client) raw(method, path string, body io.Reader, headers map[string]string) (*http.Response, []byte) {
	c.t.Helper()
	req, _ := http.NewRequest(method, c.e.srv.URL+path, body)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

func (c *client) do(method, path string, body any) (*http.Response, []byte) {
	c.t.Helper()
	var r io.Reader
	headers := map[string]string{"X-Requested-With": "envgrid"}
	switch b := body.(type) {
	case nil:
	case string:
		r = strings.NewReader(b)
		headers["Content-Type"] = "text/plain"
	default:
		data, _ := json.Marshal(b)
		r = bytes.NewReader(data)
		headers["Content-Type"] = "application/json"
	}
	return c.raw(method, path, r, headers)
}

// expect performs a request, checks the status and decodes JSON into a map.
func (c *client) expect(status int, method, path string, body any) map[string]any {
	c.t.Helper()
	resp, data := c.do(method, path, body)
	if resp.StatusCode != status {
		c.t.Fatalf("%s %s: status %d, want %d: %s", method, path, resp.StatusCode, status, data)
	}
	out := map[string]any{}
	if len(data) > 0 && strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		var v any
		if err := json.Unmarshal(data, &v); err != nil {
			c.t.Fatalf("%s %s: bad JSON: %v", method, path, err)
		}
		if m, ok := v.(map[string]any); ok {
			out = m
		} else {
			out["list"] = v
		}
	}
	return out
}

func id(v any) int64 { return int64(v.(float64)) }

// seeded builds a repo with one dotenv file and returns ids.
type ids struct {
	repo, file          int64
	test, uat, prod     int64
	plainKey, secretKey int64
}

func (e *env) seed() ids {
	e.t.Helper()
	var x ids
	x.repo = id(e.admin.expect(201, "POST", "/api/v1/repos", map[string]string{"name": "shop"})["id"])
	envs := e.admin.expect(200, "GET", fmt.Sprintf("/api/v1/repos/%d/environments", x.repo), nil)["list"].([]any)
	x.test, x.uat, x.prod = id(envs[0].(map[string]any)["id"]), id(envs[1].(map[string]any)["id"]), id(envs[2].(map[string]any)["id"])
	x.file = id(e.admin.expect(201, "POST", fmt.Sprintf("/api/v1/repos/%d/files", x.repo), map[string]string{"name": "app.env", "format": "dotenv"})["id"])
	x.plainKey = id(e.admin.expect(201, "POST", fmt.Sprintf("/api/v1/files/%d/keys", x.file), map[string]any{"name": "PORT"})["id"])
	x.secretKey = id(e.admin.expect(201, "POST", fmt.Sprintf("/api/v1/files/%d/keys", x.file), map[string]any{"name": "DB_PASSWORD", "isSecret": true})["id"])
	return x
}

func valuePath(key, env int64) string { return fmt.Sprintf("/api/v1/keys/%d/values/%d", key, env) }

func (e *env) auditActions(action string) []map[string]any {
	e.t.Helper()
	items := e.admin.expect(200, "GET", "/api/v1/audit?action="+action, nil)["items"].([]any)
	out := make([]map[string]any, len(items))
	for i, it := range items {
		out[i] = it.(map[string]any)
	}
	return out
}

func TestUnauthenticatedVsForbidden(t *testing.T) {
	e := newEnv(t)
	resp, body := e.anon().do("GET", "/api/v1/repos", nil)
	if resp.StatusCode != 401 || !strings.Contains(string(body), `"code":"unauthorized"`) {
		t.Fatalf("anonymous: %d %s", resp.StatusCode, body)
	}
	m := e.member("m@example.com")
	resp, body = m.do("GET", "/api/v1/users", nil)
	if resp.StatusCode != 403 || !strings.Contains(string(body), `"code":"forbidden"`) {
		t.Fatalf("member on admin route: %d %s", resp.StatusCode, body)
	}
	resp, body = e.anon().do("POST", "/api/v1/auth/login", map[string]string{"email": "admin@example.com", "password": "wrong-password"})
	if resp.StatusCode != 401 || !strings.Contains(string(body), "invalid_credentials") {
		t.Fatalf("bad password: %d %s", resp.StatusCode, body)
	}
	e.anon().expect(200, "GET", "/api/v1/health", nil)
}

func TestErrorShape(t *testing.T) {
	e := newEnv(t)
	resp, body := e.admin.do("GET", "/api/v1/repos/999", nil)
	var v struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if err := json.Unmarshal(body, &v); err != nil || resp.StatusCode != 404 || v.Error.Code != "not_found" || v.Error.Message == "" {
		t.Fatalf("got %d %s", resp.StatusCode, body)
	}
	resp, body = e.admin.do("GET", "/api/v1/nope", nil)
	if resp.StatusCode != 404 || !strings.Contains(string(body), `"code":"not_found"`) {
		t.Fatalf("unknown endpoint: %d %s", resp.StatusCode, body)
	}
	e.admin.expect(201, "POST", "/api/v1/repos", map[string]string{"name": "dup"})
	resp, body = e.admin.do("POST", "/api/v1/repos", map[string]string{"name": "DUP"})
	if resp.StatusCode != 409 || !strings.Contains(string(body), `"code":"conflict"`) {
		t.Fatalf("duplicate repo: %d %s", resp.StatusCode, body)
	}
}

func TestCSRFHeaderRequiredOnMutations(t *testing.T) {
	e := newEnv(t)
	resp, body := e.admin.raw("POST", "/api/v1/repos", strings.NewReader(`{"name":"x"}`), map[string]string{"Content-Type": "application/json"})
	if resp.StatusCode != 403 || !strings.Contains(string(body), `"code":"csrf"`) {
		t.Fatalf("POST without header: %d %s", resp.StatusCode, body)
	}
	resp, _ = e.anon().raw("POST", "/api/v1/auth/login", strings.NewReader(`{"email":"admin@example.com","password":"admin-password"}`), nil)
	if resp.StatusCode != 403 {
		t.Fatalf("login without header: %d", resp.StatusCode)
	}
	// GETs need it too: reveal and export write audit rows or release plaintext.
	resp, _ = e.admin.raw("GET", "/api/v1/repos", nil, nil)
	if resp.StatusCode != 403 {
		t.Fatalf("GET without header: %d", resp.StatusCode)
	}
	resp, _ = e.anon().raw("GET", "/api/v1/health", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("health without header: %d", resp.StatusCode)
	}
}

func TestSecurityHeadersAndCookie(t *testing.T) {
	e := newEnv(t)
	resp, _ := e.anon().do("POST", "/api/v1/auth/login", map[string]string{"email": "admin@example.com", "password": "admin-password"})
	cookie := resp.Header.Get("Set-Cookie")
	for _, want := range []string{"envgrid_session=", "HttpOnly", "SameSite=Lax", "Path=/", "Max-Age="} {
		if !strings.Contains(cookie, want) {
			t.Errorf("cookie %q lacks %s", cookie, want)
		}
	}
	for _, path := range []string{"/api/v1/health", "/", "/repos/1/files/2"} {
		resp, body := e.anon().do("GET", path, nil)
		h := resp.Header
		if csp := h.Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self';") || !strings.Contains(csp, "frame-ancestors 'none'") {
			t.Errorf("%s: CSP %q", path, csp)
		}
		if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s: missing nosniff or referrer policy", path)
		}
		if path != "/api/v1/health" && !strings.Contains(string(body), "<title>envgrid</title>") {
			t.Errorf("%s: SPA fallback not served", path)
		}
	}
	resp, body := e.anon().do("GET", "/assets/app.js", nil)
	if string(body) != "console.log(1)" || !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("asset: %q cache %q", body, resp.Header.Get("Cache-Control"))
	}
}

func TestLoginRateLimit(t *testing.T) {
	e := newEnv(t) // the admin login used one attempt from this IP
	c := e.anon()
	codes := []int{}
	for i := 0; i < 5; i++ {
		resp, _ := c.do("POST", "/api/v1/auth/login", map[string]string{"email": "x@example.com", "password": "nope-nope"})
		codes = append(codes, resp.StatusCode)
	}
	want := []int{401, 401, 401, 401, 429}
	if fmt.Sprint(codes) != fmt.Sprint(want) {
		t.Fatalf("codes %v, want %v", codes, want)
	}
	now := time.Now().Add(61 * time.Second)
	e.app.login.now = func() time.Time { return now }
	resp, _ := c.do("POST", "/api/v1/auth/login", map[string]string{"email": "admin@example.com", "password": "admin-password"})
	if resp.StatusCode != 200 {
		t.Fatalf("after the window: %d", resp.StatusCode)
	}
}

func TestGridMasksSecrets(t *testing.T) {
	e := newEnv(t)
	x := e.seed()
	e.admin.expect(200, "PUT", valuePath(x.plainKey, x.test), map[string]string{"value": "8080"})
	e.admin.expect(200, "PUT", valuePath(x.secretKey, x.test), map[string]string{"value": "hunter2-canary"})
	resp, body := e.admin.do("GET", fmt.Sprintf("/api/v1/files/%d/grid", x.file), nil)
	if resp.StatusCode != 200 {
		t.Fatal(string(body))
	}
	if strings.Contains(string(body), "hunter2-canary") {
		t.Fatal("grid leaked a secret value")
	}
	var g struct {
		Environments []struct{ Name string }
		Rows         []struct {
			Key   struct{ Name string }
			Cells []cellJSON
		}
	}
	_ = json.Unmarshal(body, &g)
	if len(g.Environments) != 3 || len(g.Rows) != 2 {
		t.Fatalf("grid shape: %s", body)
	}
	port, pw := g.Rows[0].Cells, g.Rows[1].Cells
	if !port[0].Present || port[0].Value == nil || *port[0].Value != "8080" || port[0].Masked || port[0].UpdatedBy == nil {
		t.Errorf("non-secret cell: %+v", port[0])
	}
	if !pw[0].Present || !pw[0].Masked || pw[0].Value != nil {
		t.Errorf("secret cell: %+v", pw[0])
	}
	if port[1].Present || port[1].Value != nil {
		t.Errorf("missing cell: %+v", port[1])
	}
}

func TestProtectedEnvironmentAuthorization(t *testing.T) {
	e := newEnv(t)
	x := e.seed()
	m := e.member("m@example.com")

	// Members read everything and write unprotected environments.
	m.expect(200, "PUT", valuePath(x.plainKey, x.test), map[string]string{"value": "1"})
	m.expect(200, "GET", fmt.Sprintf("/api/v1/files/%d/grid", x.file), nil)

	denied := func(method, path string, body any) {
		t.Helper()
		resp, data := m.do(method, path, body)
		if resp.StatusCode != 403 {
			t.Errorf("%s %s: member got %d, want 403: %s", method, path, resp.StatusCode, data)
		}
	}
	denied("PUT", valuePath(x.plainKey, x.prod), map[string]string{"value": "1"})
	denied("DELETE", valuePath(x.plainKey, x.prod), nil)
	denied("POST", fmt.Sprintf("/api/v1/files/%d/import?env=%d", x.file, x.prod), "PORT=1\n")
	denied("POST", valuePath(x.plainKey, x.prod)+"/copy", map[string]any{"fromEnvironmentId": x.test})
	denied("PATCH", fmt.Sprintf("/api/v1/environments/%d", x.prod), map[string]any{"isProtected": false})
	denied("PATCH", fmt.Sprintf("/api/v1/environments/%d", x.prod), map[string]any{"name": "production"})
	denied("PATCH", fmt.Sprintf("/api/v1/environments/%d", x.test), map[string]any{"isProtected": true})
	denied("POST", fmt.Sprintf("/api/v1/repos/%d/environments", x.repo), map[string]any{"name": "staging", "isProtected": true})
	denied("DELETE", fmt.Sprintf("/api/v1/environments/%d", x.uat), nil)
	denied("DELETE", fmt.Sprintf("/api/v1/files/%d", x.file), nil)
	denied("DELETE", fmt.Sprintf("/api/v1/repos/%d", x.repo), nil)

	// Once prod holds a value, members may no longer rename or delete the key.
	e.admin.expect(200, "PUT", valuePath(x.plainKey, x.prod), map[string]string{"value": "443"})
	denied("PATCH", fmt.Sprintf("/api/v1/keys/%d", x.plainKey), map[string]any{"name": "HTTP_PORT"})
	denied("DELETE", fmt.Sprintf("/api/v1/keys/%d", x.plainKey), nil)
	m.expect(200, "PATCH", fmt.Sprintf("/api/v1/keys/%d", x.plainKey), map[string]any{"description": "fine"})

	// Admin can do all of it, and the member's attempts changed nothing.
	e.admin.expect(200, "POST", valuePath(x.plainKey, x.prod)+"/copy", map[string]any{"fromEnvironmentId": x.test})
	e.admin.expect(200, "PATCH", fmt.Sprintf("/api/v1/environments/%d", x.prod), map[string]any{"isProtected": false})
	m.expect(200, "PUT", valuePath(x.plainKey, x.prod), map[string]string{"value": "now allowed"})

	// Environments of another repo are not reachable through this key.
	other := id(e.admin.expect(201, "POST", "/api/v1/repos", map[string]string{"name": "other"})["id"])
	otherEnvs := e.admin.expect(200, "GET", fmt.Sprintf("/api/v1/repos/%d/environments", other), nil)["list"].([]any)
	resp, _ := e.admin.do("PUT", valuePath(x.plainKey, id(otherEnvs[0].(map[string]any)["id"])), map[string]string{"value": "x"})
	if resp.StatusCode != 404 {
		t.Fatalf("cross-repo environment: %d", resp.StatusCode)
	}
}

func TestRevealExportCopyAreAudited(t *testing.T) {
	e := newEnv(t)
	x := e.seed()
	e.admin.expect(200, "PUT", valuePath(x.secretKey, x.test), map[string]string{"value": "s3cret"})
	e.admin.expect(200, "PUT", valuePath(x.secretKey, x.test), map[string]string{"value": "s3cret-v2"})
	m := e.member("m@example.com")

	if v := m.expect(200, "GET", valuePath(x.secretKey, x.test)+"/reveal", nil)["value"]; v != "s3cret-v2" {
		t.Fatalf("reveal returned %v", v)
	}
	if v := m.expect(200, "GET", valuePath(x.secretKey, x.test)+"/reveal?version=1", nil)["value"]; v != "s3cret" {
		t.Fatalf("reveal of version 1 returned %v", v)
	}
	m.expect(200, "GET", valuePath(x.secretKey, x.test)+"/reveal?purpose=copy", nil)
	resp, body := m.do("GET", fmt.Sprintf("/api/v1/files/%d/export?env=%d&format=dotenv", x.file, x.test), nil)
	if resp.StatusCode != 200 || string(body) != "DB_PASSWORD=s3cret-v2\n" {
		t.Fatalf("export: %d %q", resp.StatusCode, body)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != `attachment; filename=app.test.env` {
		t.Fatalf("content-disposition %q", cd)
	}
	e.admin.expect(200, "POST", valuePath(x.secretKey, x.uat)+"/copy", map[string]any{"fromEnvironmentId": x.test})

	reveals := e.auditActions("reveal_secret")
	if len(reveals) != 2 {
		t.Fatalf("reveal_secret entries: %d", len(reveals))
	}
	if u := reveals[0]["user"].(map[string]any); u["email"] != "m@example.com" || id(reveals[0]["keyId"]) != x.secretKey || id(reveals[0]["environmentId"]) != x.test {
		t.Fatalf("reveal entry: %v", reveals[0])
	}
	if copies := e.auditActions("copy_value"); len(copies) != 1 {
		t.Fatalf("copy_value entries: %d", len(copies))
	}
	exports := e.auditActions("export_file")
	if len(exports) != 1 || exports[0]["detail"].(map[string]any)["format"] != "dotenv" {
		t.Fatalf("export_file entries: %v", exports)
	}
	created := e.auditActions("create_value")
	if len(created) != 2 || created[0]["detail"].(map[string]any)["copiedFrom"] != "test" {
		t.Fatalf("copy between environments not audited as a value write: %v", created)
	}
	for _, rows := range [][]map[string]any{reveals, exports, created} {
		for _, r := range rows {
			if strings.Contains(fmt.Sprint(r["detail"]), "s3cret") {
				t.Fatal("audit detail contains a value")
			}
		}
	}

	// Members see repo events but not admin-only account events.
	items := m.expect(200, "GET", "/api/v1/audit", nil)["items"].([]any)
	for _, it := range items {
		if it.(map[string]any)["action"] == "user_created" {
			t.Fatal("member sees account administration events")
		}
	}
}

func TestCompareEndpoint(t *testing.T) {
	e := newEnv(t)
	x := e.seed()
	e.admin.expect(200, "PUT", valuePath(x.plainKey, x.test), map[string]string{"value": "1"})
	e.admin.expect(200, "PUT", valuePath(x.plainKey, x.prod), map[string]string{"value": "2"})
	e.admin.expect(200, "PUT", valuePath(x.secretKey, x.test), map[string]string{"value": "only-in-test"})
	path := fmt.Sprintf("/api/v1/files/%d/compare?a=%d&b=%d", x.file, x.test, x.prod)

	resp, body := e.admin.do("GET", path, nil)
	if resp.StatusCode != 200 || strings.Contains(string(body), "only-in-test") {
		t.Fatalf("masked compare: %d %s", resp.StatusCode, body)
	}
	var c struct {
		MissingInA, MissingInB, Different, Same []struct {
			Key  struct{ Name string }
			A, B cellJSON
		}
	}
	_ = json.Unmarshal(body, &c)
	if len(c.MissingInA) != 0 || len(c.MissingInB) != 1 || len(c.Different) != 1 || len(c.Same) != 0 {
		t.Fatalf("buckets: %s", body)
	}
	if c.Different[0].Key.Name != "PORT" || *c.Different[0].A.Value != "1" || *c.Different[0].B.Value != "2" {
		t.Fatalf("different: %+v", c.Different[0])
	}
	if len(e.auditActions("reveal_secret")) != 0 {
		t.Fatal("masked compare was audited as a reveal")
	}
	_, body = e.admin.do("GET", path+"&reveal=true", nil)
	if !strings.Contains(string(body), "only-in-test") {
		t.Fatal("reveal=true did not reveal")
	}
	if rs := e.auditActions("reveal_secret"); len(rs) != 1 || rs[0]["detail"].(map[string]any)["via"] != "compare" {
		t.Fatalf("compare reveal audit: %v", rs)
	}
}

func TestImportAndExportFormats(t *testing.T) {
	e := newEnv(t)
	x := e.seed()
	res := e.admin.expect(200, "POST", fmt.Sprintf("/api/v1/files/%d/import?env=%d", x.file, x.uat),
		"# pasted\nexport PORT=9000\nNEW_KEY=\"multi\nline\"\nDB_PASSWORD='p w'\n")
	if len(res["added"].([]any)) != 3 || len(res["keysCreated"].([]any)) != 1 {
		t.Fatalf("import summary %v", res)
	}
	res = e.admin.expect(200, "POST", fmt.Sprintf("/api/v1/files/%d/import?env=%d", x.file, x.uat), "PORT=9001\nNEW_KEY=\"multi\nline\"\n")
	if len(res["updated"].([]any)) != 1 || len(res["unchanged"].([]any)) != 1 {
		t.Fatalf("second import summary %v", res)
	}
	resp, body := e.admin.do("POST", fmt.Sprintf("/api/v1/files/%d/import?env=%d", x.file, x.uat), "OK=1\nBROKEN=\"never closed secret-canary\n")
	if resp.StatusCode != 400 || !strings.Contains(string(body), "parse_error") || !strings.Contains(string(body), "line 2") || strings.Contains(string(body), "secret-canary") {
		t.Fatalf("parse error: %d %s", resp.StatusCode, body)
	}

	for format, want := range map[string]string{
		"dotenv":     "PORT=9001\nDB_PASSWORD='p w'\nNEW_KEY='multi\nline'\n",
		"json":       "{\n  \"PORT\": 9001,\n  \"DB_PASSWORD\": \"p w\",\n  \"NEW_KEY\": \"multi\\nline\"\n}\n",
		"properties": "PORT=9001\nDB_PASSWORD=p w\nNEW_KEY=multi\\nline\n",
	} {
		resp, body := e.admin.do("GET", fmt.Sprintf("/api/v1/files/%d/export?env=%d&format=%s", x.file, x.uat, format), nil)
		if resp.StatusCode != 200 || string(body) != want {
			t.Errorf("%s export: %d\n%s", format, resp.StatusCode, body)
		}
	}
	resp, _ = e.admin.do("GET", fmt.Sprintf("/api/v1/files/%d/export?env=%d&format=toml", x.file, x.uat), nil)
	if resp.StatusCode != 400 {
		t.Fatalf("unknown format: %d", resp.StatusCode)
	}

	// Raw files hold the whole body.
	raw := id(e.admin.expect(201, "POST", fmt.Sprintf("/api/v1/repos/%d/files", x.repo), map[string]string{"name": "nginx.conf", "format": "raw"})["id"])
	e.admin.expect(200, "POST", fmt.Sprintf("/api/v1/files/%d/import?env=%d", raw, x.test), "server {\n  listen 80;\n}\n")
	resp, body = e.admin.do("GET", fmt.Sprintf("/api/v1/files/%d/export?env=%d", raw, x.test), nil)
	if string(body) != "server {\n  listen 80;\n}\n" || resp.Header.Get("Content-Disposition") != "attachment; filename=nginx.test.conf" {
		t.Fatalf("raw export %q %q", body, resp.Header.Get("Content-Disposition"))
	}
}

func TestHistoryAndRestore(t *testing.T) {
	e := newEnv(t)
	x := e.seed()
	p := valuePath(x.plainKey, x.test)
	e.admin.expect(200, "PUT", p, map[string]string{"value": "v1"})
	e.admin.expect(200, "PUT", p, map[string]string{"value": "v2"})
	e.admin.expect(200, "DELETE", p, nil)
	if ch := e.admin.expect(200, "PUT", p, map[string]string{"value": "v2"}); ch["version"].(float64) != 4 {
		t.Fatalf("set after delete: %v", ch)
	}
	e.admin.expect(200, "POST", p+"/restore", map[string]int{"version": 1})
	versions := e.admin.expect(200, "GET", p+"/history", nil)["versions"].([]any)
	if len(versions) != 5 {
		t.Fatalf("history length %d", len(versions))
	}
	latest, deleted := versions[0].(map[string]any), versions[2].(map[string]any)
	if latest["value"] != "v1" || deleted["present"] != false {
		t.Fatalf("history %v", versions)
	}
	secretHistory := e.admin.expect(200, "GET", valuePath(x.secretKey, x.test)+"/history", nil)
	if len(secretHistory["versions"].([]any)) != 0 {
		t.Fatal("unexpected secret history")
	}
	e.admin.expect(200, "PUT", valuePath(x.secretKey, x.test), map[string]string{"value": "hidden"})
	_, body := e.admin.do("GET", valuePath(x.secretKey, x.test)+"/history", nil)
	if strings.Contains(string(body), "hidden") {
		t.Fatal("history leaked a secret")
	}
}

func TestSessionExpiryAndLogout(t *testing.T) {
	e := newEnv(t)
	e.admin.expect(200, "GET", "/api/v1/auth/me", nil)
	e.admin.expect(204, "POST", "/api/v1/auth/logout", nil)
	e.admin.expect(401, "GET", "/api/v1/auth/me", nil)

	c := e.login("admin@example.com", "admin-password")
	e.st.Now = func() time.Time { return time.Now().Add(store.SessionTTL + time.Minute) }
	defer func() { e.st.Now = time.Now }()
	c.expect(401, "GET", "/api/v1/auth/me", nil)
}

func TestUserAdministration(t *testing.T) {
	e := newEnv(t)
	m := e.member("m@example.com")
	users := e.admin.expect(200, "GET", "/api/v1/users", nil)["list"].([]any)
	var memberID int64
	for _, u := range users {
		if u.(map[string]any)["email"] == "m@example.com" {
			memberID = id(u.(map[string]any)["id"])
		}
		if _, leaked := u.(map[string]any)["passwordHash"]; leaked {
			t.Fatal("password hash in API")
		}
	}
	e.admin.expect(200, "PATCH", fmt.Sprintf("/api/v1/users/%d", memberID), map[string]any{"password": "reset-password"})
	m.expect(401, "GET", "/api/v1/auth/me", nil) // reset ends sessions
	m = e.login("m@example.com", "reset-password")
	e.admin.expect(200, "PATCH", fmt.Sprintf("/api/v1/users/%d", memberID), map[string]any{"disabled": true})
	m.expect(401, "GET", "/api/v1/auth/me", nil)
	resp, _ := e.anon().do("POST", "/api/v1/auth/login", map[string]string{"email": "m@example.com", "password": "reset-password"})
	if resp.StatusCode != 401 {
		t.Fatal("disabled user signed in")
	}
	me := e.admin.expect(200, "GET", "/api/v1/auth/me", nil)
	resp, body := e.admin.do("PATCH", fmt.Sprintf("/api/v1/users/%d", id(me["id"])), map[string]any{"role": "member"})
	if resp.StatusCode != 409 || !strings.Contains(string(body), "last_admin") {
		t.Fatalf("demoting the last admin: %d %s", resp.StatusCode, body)
	}
	resp, _ = e.admin.do("PATCH", "/api/v1/auth/me", map[string]any{"currentPassword": "wrong", "newPassword": "another-password"})
	if resp.StatusCode != 400 {
		t.Fatal("password change without the current password")
	}
	e.admin.expect(200, "PATCH", "/api/v1/auth/me", map[string]any{"currentPassword": "admin-password", "newPassword": "another-password"})
	e.admin.expect(200, "GET", "/api/v1/auth/me", nil) // fresh session issued
	if len(e.auditActions("user_created")) != 1 {
		t.Fatal("user_created not audited")
	}
}

// TestValuesNeverLogged drives every path a value can travel, including
// failures, and then scans the captured log output for the canary.
func TestValuesNeverLogged(t *testing.T) {
	const canary = "CANARY-7f3a9c-do-not-log"
	e := newEnv(t)
	x := e.seed()
	m := e.member("m@example.com")
	f := fmt.Sprintf("/api/v1/files/%d", x.file)

	e.admin.expect(200, "PUT", valuePath(x.secretKey, x.test), map[string]string{"value": canary})
	e.admin.expect(200, "PUT", valuePath(x.plainKey, x.test), map[string]string{"value": canary})
	e.admin.expect(200, "GET", valuePath(x.secretKey, x.test)+"/reveal", nil)
	e.admin.expect(200, "GET", valuePath(x.secretKey, x.test)+"/reveal?purpose=copy", nil)
	e.admin.expect(200, "GET", valuePath(x.secretKey, x.test)+"/history", nil)
	e.admin.expect(200, "GET", f+"/grid", nil)
	e.admin.expect(200, "GET", fmt.Sprintf("%s/compare?a=%d&b=%d&reveal=true", f, x.test, x.prod), nil)
	e.admin.do("GET", fmt.Sprintf("%s/export?env=%d&format=yaml", f, x.test), nil)
	e.admin.expect(200, "POST", fmt.Sprintf("%s/import?env=%d", f, x.uat), "DB_PASSWORD="+canary+"\n")
	e.admin.expect(200, "POST", valuePath(x.secretKey, x.uat)+"/copy", map[string]any{"fromEnvironmentId": x.test})
	// Failures: protected write, parse error, oversized value, bad JSON.
	m.do("PUT", valuePath(x.secretKey, x.prod), map[string]string{"value": canary})
	m.do("POST", fmt.Sprintf("%s/import?env=%d", f, x.prod), "DB_PASSWORD="+canary+"\n")
	e.admin.do("POST", fmt.Sprintf("%s/import?env=%d", f, x.uat), "BROKEN=\""+canary+"\n")
	e.admin.do("PUT", valuePath(x.secretKey, x.uat), map[string]string{"value": canary + strings.Repeat("x", maxValue)})
	e.admin.do("PUT", valuePath(x.secretKey, x.uat), `{"value": "`+canary)
	e.admin.do("POST", "/api/v1/auth/login", map[string]string{"email": "admin@example.com", "password": canary})
	e.admin.do("POST", fmt.Sprintf("/api/v1/files/%d/keys", x.file), map[string]any{"name": "bad key", "description": canary})

	if e.logs.Len() == 0 {
		t.Fatal("no log output captured")
	}
	if strings.Contains(e.logs.String(), canary) {
		t.Fatalf("a value reached the logs:\n%s", e.logs.String())
	}
	if !strings.Contains(e.logs.String(), `"msg":"request"`) {
		t.Fatal("request log missing")
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:5555"
	r.Header.Add("X-Forwarded-For", "6.6.6.6, 1.2.3.4")
	if got := (&Server{}).clientIP(r); got != "10.0.0.1" {
		t.Fatalf("untrusted proxy header was used: %s", got)
	}
	trusting := &Server{cfg: Config{TrustProxy: true}}
	if got := trusting.clientIP(r); got != "1.2.3.4" {
		t.Fatalf("want the rightmost entry (added by our proxy), got %s", got)
	}
	r.Header.Set("X-Forwarded-For", "not-an-ip")
	if got := trusting.clientIP(r); got != "10.0.0.1" {
		t.Fatalf("garbage header was used: %s", got)
	}
}

func TestExportFileName(t *testing.T) {
	cases := map[[3]string]string{
		{"backend.env", "dotenv", "dotenv"}: "backend.prod.env",
		{".env", "dotenv", "dotenv"}:        ".env.prod",
		{".env", "dotenv", "json"}:          ".env.prod.json",
		{"config.json", "json", "yaml"}:     "config.prod.yaml",
		{"Dockerfile", "raw", "raw"}:        "Dockerfile.prod",
	}
	for in, want := range cases {
		if got := exportFileName(&store.File{Name: in[0], Format: in[1]}, "prod", in[2]); got != want {
			t.Errorf("%v: %q, want %q", in, got, want)
		}
	}
}

func TestImportAnyFormat(t *testing.T) {
	e := newEnv(t)
	x := e.seed() // app.env, a dotenv file
	imp := func(file int64, body string) (int, string) {
		resp, data := e.admin.do("POST", fmt.Sprintf("/api/v1/files/%d/import?env=%d", file, x.test), body)
		return resp.StatusCode, string(data)
	}
	php := "<?php\nclass constants {\n    const  PAGE_SIZE = 10;\n    const SITE_URL = 'http://localhost/site'; // 100 MB\n    const LIST = [\n        'a' => 1,\n    ];\n}\n"

	// PHP pasted into a .env file: refused, with a reason that names both formats.
	code, body := imp(x.file, php)
	if code != 400 || !strings.Contains(body, "looks like PHP constants") || !strings.Contains(body, "app.env is a .env file") {
		t.Fatalf("php into dotenv: %d %s", code, body)
	}
	// Plain text: refused, pointing at plain text files.
	code, body = imp(x.file, "Rotate the keys every quarter.\nThanks\n")
	if code != 400 || !strings.Contains(body, "plain text") {
		t.Fatalf("text into dotenv: %d %s", code, body)
	}
	// JSON into a .env file converts, and says so.
	code, body = imp(x.file, `{"PORT": 8080, "db": {"host": "h"}}`)
	if code != 200 || !strings.Contains(body, `"readAs":"json"`) {
		t.Fatalf("json into dotenv: %d %s", code, body)
	}

	// A PHP file keeps expressions verbatim and exports them back.
	phpFile := id(e.admin.expect(201, "POST", fmt.Sprintf("/api/v1/repos/%d/files", x.repo), map[string]string{"name": "constants.php", "format": "php"})["id"])
	if code, body := imp(phpFile, php); code != 200 || !strings.Contains(body, `"added":["PAGE_SIZE","SITE_URL","LIST"]`) {
		t.Fatalf("php import: %d %s", code, body)
	}
	resp, out := e.admin.do("GET", fmt.Sprintf("/api/v1/files/%d/export?env=%d", phpFile, x.test), nil)
	want := "const PAGE_SIZE = 10;\nconst SITE_URL = 'http://localhost/site';\nconst LIST = [\n        'a' => 1,\n    ];\n"
	if resp.StatusCode != 200 || string(out) != want {
		t.Fatalf("php export: %d %q", resp.StatusCode, out)
	}
	if resp, _ := e.admin.do("GET", fmt.Sprintf("/api/v1/files/%d/export?env=%d&format=json", phpFile, x.test), nil); resp.StatusCode != 400 {
		t.Fatalf("php exported as json: %d", resp.StatusCode)
	}
	grid := e.admin.expect(200, "GET", fmt.Sprintf("/api/v1/files/%d/grid", phpFile), nil)
	pageSize := id(grid["rows"].([]any)[0].(map[string]any)["key"].(map[string]any)["id"])
	resp, body2 := e.admin.do("PUT", valuePath(pageSize, x.test), map[string]string{"value": "hello world"})
	if resp.StatusCode != 400 || !strings.Contains(string(body2), "invalid_value") {
		t.Fatalf("unquoted text accepted as PHP: %d %s", resp.StatusCode, body2)
	}
	e.admin.expect(200, "PUT", valuePath(pageSize, x.test), map[string]string{"value": " 'hello world' "})

	// Detection reports the format and key names, never values.
	resp, det := e.admin.do("POST", "/api/v1/formats/detect?name=", php)
	if resp.StatusCode != 200 || !strings.Contains(string(det), `"format":"php"`) || !strings.Contains(string(det), `"keys":3`) || strings.Contains(string(det), "localhost") {
		t.Fatalf("detect: %d %s", resp.StatusCode, det)
	}
	_, det = e.admin.do("POST", "/api/v1/formats/detect?name=notes.txt", "just some notes\n")
	if !strings.Contains(string(det), `"format":"raw"`) {
		t.Fatalf("detect text: %s", det)
	}
}

func TestBranchStyleEnvironmentNames(t *testing.T) {
	e := newEnv(t)
	x := e.seed()
	env := id(e.admin.expect(201, "POST", fmt.Sprintf("/api/v1/repos/%d/environments", x.repo), map[string]any{"name": "feature/login page"})["id"])
	e.admin.expect(201, "POST", fmt.Sprintf("/api/v1/repos/%d/environments", x.repo), map[string]any{"name": "qa-2"})
	e.admin.expect(200, "PUT", valuePath(x.plainKey, env), map[string]string{"value": "1"})
	resp, _ := e.admin.do("GET", fmt.Sprintf("/api/v1/files/%d/export?env=%d", x.file, env), nil)
	if cd := resp.Header.Get("Content-Disposition"); cd != "attachment; filename=app.feature-login-page.env" {
		t.Fatalf("content-disposition %q", cd)
	}
	for _, bad := range []string{"", "/leading", "has\nnewline", strings.Repeat("x", 64)} {
		resp, _ := e.admin.do("POST", fmt.Sprintf("/api/v1/repos/%d/environments", x.repo), map[string]any{"name": bad})
		if resp.StatusCode != 400 {
			t.Errorf("environment name %q accepted", bad)
		}
	}
}

// The grid groups equal values per key without revealing secrets.
func TestGridValueGroups(t *testing.T) {
	e := newEnv(t)
	x := e.seed()
	e.admin.expect(200, "PUT", valuePath(x.secretKey, x.test), map[string]string{"value": "same-secret"})
	e.admin.expect(200, "PUT", valuePath(x.secretKey, x.uat), map[string]string{"value": "other-secret"})
	e.admin.expect(200, "PUT", valuePath(x.secretKey, x.prod), map[string]string{"value": "same-secret"})
	e.admin.expect(200, "PUT", valuePath(x.plainKey, x.uat), map[string]string{"value": "1"})
	resp, body := e.admin.do("GET", fmt.Sprintf("/api/v1/files/%d/grid", x.file), nil)
	if resp.StatusCode != 200 || strings.Contains(string(body), "same-secret") {
		t.Fatalf("grid: %d", resp.StatusCode)
	}
	var g struct {
		Rows []struct {
			Key   struct{ Name string }
			Cells []cellJSON
		}
	}
	_ = json.Unmarshal(body, &g)
	group := func(c cellJSON) int {
		if c.Group == nil {
			return -1
		}
		return *c.Group
	}
	secret, plain := g.Rows[1].Cells, g.Rows[0].Cells
	if group(secret[0]) != 0 || group(secret[1]) != 1 || group(secret[2]) != 0 {
		t.Fatalf("secret groups %d %d %d", group(secret[0]), group(secret[1]), group(secret[2]))
	}
	if group(plain[0]) != -1 || group(plain[1]) != 0 || group(plain[2]) != -1 {
		t.Fatalf("plain groups %d %d %d", group(plain[0]), group(plain[1]), group(plain[2]))
	}
}

func TestRepoSourceURL(t *testing.T) {
	e := newEnv(t)
	repo := e.admin.expect(201, "POST", "/api/v1/repos", map[string]string{"name": "app", "sourceUrl": "github.com/acme/app"})
	if repo["sourceUrl"] != "https://github.com/acme/app" {
		t.Fatalf("scheme not added: %v", repo["sourceUrl"])
	}
	path := fmt.Sprintf("/api/v1/repos/%d", id(repo["id"]))
	for _, bad := range []string{"javascript:alert(1)", "file:///etc/passwd", "https://", "ftp://example.com/x"} {
		resp, _ := e.admin.do("PATCH", path, map[string]string{"sourceUrl": bad})
		if resp.StatusCode != 400 {
			t.Errorf("%q accepted", bad)
		}
	}
	if got := e.admin.expect(200, "PATCH", path, map[string]string{"sourceUrl": "https://gitlab.example.com/team/app"})["sourceUrl"]; got != "https://gitlab.example.com/team/app" {
		t.Fatalf("update: %v", got)
	}
	if got := e.admin.expect(200, "PATCH", path, map[string]string{"sourceUrl": ""})["sourceUrl"]; got != "" {
		t.Fatalf("clearing: %v", got)
	}
	if got := e.admin.expect(200, "PATCH", path, map[string]string{"description": "kept"})["sourceUrl"]; got != "" {
		t.Fatalf("an unrelated update changed the URL: %v", got)
	}
}

func TestValidationRules(t *testing.T) {
	e := newEnv(t)
	x := e.seed()
	m := e.member("m@example.com")
	keyPath := fmt.Sprintf("/api/v1/keys/%d", x.plainKey)

	// Only admins set rules; anyone can tag.
	if resp, _ := m.do("PATCH", keyPath, map[string]any{"required": true}); resp.StatusCode != 403 {
		t.Fatalf("member changed rules: %d", resp.StatusCode)
	}
	m.expect(200, "PATCH", keyPath, map[string]any{"tags": []string{"Infra", "infra", " web "}})
	if resp, _ := e.admin.do("PATCH", keyPath, map[string]any{"pattern": "([unclosed"}); resp.StatusCode != 400 {
		t.Fatal("invalid pattern accepted")
	}
	k := e.admin.expect(200, "PATCH", keyPath, map[string]any{"required": true, "pattern": "^[0-9]+$"})
	if k["required"] != true || k["pattern"] != "^[0-9]+$" || fmt.Sprint(k["tags"]) != "[infra web]" {
		t.Fatalf("key after update: %v", k)
	}

	// Writes that break a rule are refused, naming the key and rule, not the value.
	resp, body := e.admin.do("PUT", valuePath(x.plainKey, x.test), map[string]string{"value": "not-a-number-canary"})
	if resp.StatusCode != 400 || !strings.Contains(string(body), "rule_failed") || strings.Contains(string(body), "canary") {
		t.Fatalf("pattern: %d %s", resp.StatusCode, body)
	}
	e.admin.expect(200, "PUT", valuePath(x.plainKey, x.test), map[string]string{"value": "8080"})
	if resp, body := e.admin.do("DELETE", valuePath(x.plainKey, x.test), nil); resp.StatusCode != 400 || !strings.Contains(string(body), "required") {
		t.Fatalf("delete of required value: %d %s", resp.StatusCode, body)
	}
	resp, body = e.admin.do("POST", fmt.Sprintf("/api/v1/files/%d/import?env=%d", x.file, x.uat), "PORT=abc\nOTHER=1\n")
	if resp.StatusCode != 400 || !strings.Contains(string(body), "PORT must match") {
		t.Fatalf("import: %d %s", resp.StatusCode, body)
	}

	// The grid flags problems: the missing required values, nothing on the good one.
	grid := e.admin.expect(200, "GET", fmt.Sprintf("/api/v1/files/%d/grid", x.file), nil)
	cells := grid["rows"].([]any)[0].(map[string]any)["cells"].([]any)
	if cells[0].(map[string]any)["problem"] != nil || cells[1].(map[string]any)["problem"] != "required" {
		t.Fatalf("grid problems: %v", cells)
	}
}

func TestChangeRequests(t *testing.T) {
	e := newEnv(t)
	x := e.seed()
	m := e.member("m@example.com")
	other := e.member("o@example.com")
	propose := func(c *client, key int64, body any) map[string]any {
		t.Helper()
		return c.expect(201, "POST", valuePath(key, x.prod)+"/requests", body)
	}

	// A member cannot write prod, but can propose; a secret stays masked.
	e.admin.expect(200, "PUT", valuePath(x.secretKey, x.prod), map[string]string{"value": "old-secret"})
	req := propose(m, x.secretKey, map[string]any{"value": "new-secret-canary", "reason": "rotate"})
	if req["masked"] != true || req["proposed"] != nil || req["status"] != "pending" {
		t.Fatalf("request view: %v", req)
	}
	list := m.expect(200, "GET", "/api/v1/change-requests", nil)["list"].([]any)
	if len(list) != 1 || strings.Contains(fmt.Sprint(list), "canary") {
		t.Fatalf("list: %v", list)
	}
	grid := e.admin.expect(200, "GET", fmt.Sprintf("/api/v1/files/%d/grid", x.file), nil)
	if pending := grid["rows"].([]any)[1].(map[string]any)["cells"].([]any)[2].(map[string]any)["pending"]; pending != float64(1) {
		t.Fatalf("grid pending: %v", pending)
	}
	if v := e.admin.expect(200, "GET", fmt.Sprintf("/api/v1/change-requests/%d/reveal", id(req["id"])), nil)["value"]; v != "new-secret-canary" {
		t.Fatalf("reveal: %v", v)
	}

	// Only admins decide; approval applies the value and is audited.
	reqPath := fmt.Sprintf("/api/v1/change-requests/%d", id(req["id"]))
	if resp, _ := m.do("POST", reqPath+"/approve", map[string]string{}); resp.StatusCode != 403 {
		t.Fatal("member approved a request")
	}
	if done := e.admin.expect(200, "POST", reqPath+"/approve", map[string]string{"note": "ok"}); done["status"] != "approved" {
		t.Fatalf("approve: %v", done)
	}
	if v := e.admin.expect(200, "GET", valuePath(x.secretKey, x.prod)+"/reveal", nil)["value"]; v != "new-secret-canary" {
		t.Fatalf("value after approval: %v", v)
	}
	if resp, body := e.admin.do("POST", reqPath+"/approve", map[string]string{}); resp.StatusCode != 409 || !strings.Contains(string(body), "not_pending") {
		t.Fatalf("second approval: %d %s", resp.StatusCode, body)
	}
	if len(e.auditActions("approve_change")) != 1 || len(e.auditActions("request_change")) != 1 {
		t.Fatal("request lifecycle not audited")
	}

	// A proposal made before someone else changed the value is stale.
	stale := propose(m, x.plainKey, map[string]any{"value": "1"})
	e.admin.expect(200, "PUT", valuePath(x.plainKey, x.prod), map[string]string{"value": "2"})
	resp, body := e.admin.do("POST", fmt.Sprintf("/api/v1/change-requests/%d/approve", id(stale["id"])), map[string]string{})
	if resp.StatusCode != 409 || !strings.Contains(string(body), "stale") {
		t.Fatalf("stale approval: %d %s", resp.StatusCode, body)
	}
	e.admin.expect(200, "POST", fmt.Sprintf("/api/v1/change-requests/%d/reject", id(stale["id"])), map[string]string{"note": "outdated"})

	// Deletions can be proposed; only the author or an admin cancels.
	del := propose(m, x.plainKey, map[string]any{"delete": true})
	if resp, _ := other.do("POST", fmt.Sprintf("/api/v1/change-requests/%d/cancel", id(del["id"])), map[string]string{}); resp.StatusCode != 403 {
		t.Fatal("someone else cancelled a request")
	}
	m.expect(204, "POST", fmt.Sprintf("/api/v1/change-requests/%d/cancel", id(del["id"])), map[string]string{})

	// Proposals that break a rule are refused up front.
	e.admin.expect(200, "PATCH", fmt.Sprintf("/api/v1/keys/%d", x.plainKey), map[string]any{"pattern": "^[0-9]+$"})
	if resp, _ := m.do("POST", valuePath(x.plainKey, x.prod)+"/requests", map[string]any{"value": "abc"}); resp.StatusCode != 400 {
		t.Fatal("rule-breaking proposal accepted")
	}
	if n := e.admin.expect(200, "GET", "/api/v1/change-requests/count", nil)["pending"]; n != float64(0) {
		t.Fatalf("pending count: %v", n)
	}
}

func TestRollback(t *testing.T) {
	e := newEnv(t)
	x := e.seed()
	m := e.member("m@example.com") // signed in before the clock is faked
	clock := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	e.st.Now = func() time.Time { return clock }
	defer func() { e.st.Now = time.Now }()
	set := func(key int64, v string) {
		t.Helper()
		e.admin.expect(200, "PUT", valuePath(key, x.test), map[string]string{"value": v})
	}
	set(x.plainKey, "1")
	set(x.secretKey, "s1")
	clock = clock.Add(time.Hour) // 10:00: the moment to go back to
	at := clock.Format(time.RFC3339)
	clock = clock.Add(time.Hour)
	set(x.plainKey, "2")
	set(x.secretKey, "s2")
	newKey := id(e.admin.expect(201, "POST", fmt.Sprintf("/api/v1/files/%d/keys", x.file), map[string]any{"name": "LATER"})["id"])
	set(newKey, "x")

	preview := e.admin.expect(200, "GET", fmt.Sprintf("/api/v1/files/%d/rollback?env=%d&at=%s", x.file, x.test, at), nil)
	changes := preview["changes"].([]any)
	if len(changes) != 3 || strings.Contains(fmt.Sprint(changes), "s1") {
		t.Fatalf("preview: %v", changes)
	}
	if resp, body := m.do("POST", fmt.Sprintf("/api/v1/files/%d/rollback", x.file), map[string]any{"env": x.prod, "at": at}); resp.StatusCode != 403 {
		t.Fatalf("member restored a protected environment: %d %s", resp.StatusCode, body)
	}
	done := e.admin.expect(200, "POST", fmt.Sprintf("/api/v1/files/%d/rollback", x.file), map[string]any{"env": x.test, "at": at})
	if len(done["changed"].([]any)) != 3 {
		t.Fatalf("rollback: %v", done)
	}
	if v := e.admin.expect(200, "GET", valuePath(x.plainKey, x.test)+"/reveal", nil)["value"]; v != "1" {
		t.Fatalf("plain after rollback: %v", v)
	}
	if v := e.admin.expect(200, "GET", valuePath(x.secretKey, x.test)+"/reveal", nil)["value"]; v != "s1" {
		t.Fatalf("secret after rollback: %v", v)
	}
	if resp, _ := e.admin.do("GET", valuePath(newKey, x.test)+"/reveal", nil); resp.StatusCode != 404 {
		t.Fatal("a key created later kept its value")
	}
	// History is kept: the rollback added versions, it did not remove any.
	if n := len(e.admin.expect(200, "GET", valuePath(x.plainKey, x.test)+"/history", nil)["versions"].([]any)); n != 3 {
		t.Fatalf("history length %d", n)
	}
	if len(e.auditActions("restore_environment")) != 1 {
		t.Fatal("rollback not audited")
	}
	if again := e.admin.expect(200, "GET", fmt.Sprintf("/api/v1/files/%d/rollback?env=%d&at=%s", x.file, x.test, at), nil); len(again["changes"].([]any)) != 0 {
		t.Fatal("a second preview still shows changes")
	}
}

func TestReferencesResolveOnExport(t *testing.T) {
	e := newEnv(t)
	x := e.seed()
	key := func(name string) int64 {
		return id(e.admin.expect(201, "POST", fmt.Sprintf("/api/v1/files/%d/keys", x.file), map[string]any{"name": name})["id"])
	}
	host, url, cross, shell := key("HOST"), key("URL"), key("FROM_PROD"), key("SHELL_PATH")
	set := func(k, env int64, v string) {
		e.admin.expect(200, "PUT", valuePath(k, env), map[string]string{"value": v})
	}
	set(host, x.test, "db.test")
	set(host, x.prod, "db.prod")
	set(url, x.test, "postgres://${HOST}:5432")
	set(cross, x.test, "${prod.HOST}")
	set(shell, x.test, "${HOME}/bin")
	path := fmt.Sprintf("/api/v1/files/%d/export?env=%d&format=dotenv", x.file, x.test)
	_, body := e.admin.do("GET", path, nil)
	for _, want := range []string{"URL=postgres://db.test:5432", "FROM_PROD=db.prod", "SHELL_PATH='${HOME}/bin'"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("export lacks %q:\n%s", want, body)
		}
	}
	_, raw := e.admin.do("GET", path+"&resolve=false", nil)
	if !strings.Contains(string(raw), "URL='postgres://${HOST}:5432'") {
		t.Errorf("resolve=false still resolved:\n%s", raw)
	}
	// A loop is an error naming keys, not values.
	set(host, x.test, "${URL}")
	resp, msg := e.admin.do("GET", path, nil)
	if resp.StatusCode != 400 || !strings.Contains(string(msg), "loop") || strings.Contains(string(msg), "postgres") {
		t.Fatalf("cycle: %d %s", resp.StatusCode, msg)
	}
}

func TestInsights(t *testing.T) {
	e := newEnv(t)
	x := e.seed()
	clock := time.Now().Add(-200 * 24 * time.Hour)
	e.st.Now = func() time.Time { return clock }
	e.admin.expect(200, "PUT", valuePath(x.secretKey, x.test), map[string]string{"value": "shared-canary"})
	e.st.Now = time.Now
	e.admin.expect(200, "PUT", valuePath(x.secretKey, x.prod), map[string]string{"value": "shared-canary"})
	e.admin.expect(200, "PATCH", fmt.Sprintf("/api/v1/keys/%d", x.plainKey), map[string]any{"required": true})

	resp, body := e.admin.do("GET", "/api/v1/insights?days=90", nil)
	if resp.StatusCode != 200 || strings.Contains(string(body), "canary") {
		t.Fatalf("insights: %d %s", resp.StatusCode, body)
	}
	var in struct {
		Stale    []insightPlace
		Reused   [][]insightPlace
		Problems []insightPlace
	}
	_ = json.Unmarshal(body, &in)
	if len(in.Stale) != 1 || in.Stale[0].Environment != "test" {
		t.Fatalf("stale: %+v", in.Stale)
	}
	if len(in.Reused) != 1 || len(in.Reused[0]) != 2 {
		t.Fatalf("reused: %+v", in.Reused)
	}
	if len(in.Problems) != 3 || in.Problems[0].Problem != "required" {
		t.Fatalf("problems: %+v", in.Problems)
	}
}

func TestAPITokensAndAccessExpiry(t *testing.T) {
	e := newEnv(t)
	x := e.seed()
	created := e.admin.expect(201, "POST", "/api/v1/auth/tokens", map[string]any{"name": "ci", "expiresInDays": 30})
	token := created["token"].(string)
	if !strings.HasPrefix(token, "egt_") || created["info"].(map[string]any)["prefix"] != token[:10] {
		t.Fatalf("token: %v", created)
	}
	bearer := func(method, path string, body string) (*http.Response, []byte) {
		var r io.Reader
		if body != "" {
			r = strings.NewReader(body)
		}
		return e.anon().raw(method, path, r, map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json"})
	}
	if resp, _ := bearer("GET", "/api/v1/repos", ""); resp.StatusCode != 200 {
		t.Fatalf("token read: %d", resp.StatusCode)
	}
	// No CSRF header needed with a bearer token, and names resolve to ids.
	if resp, body := bearer("PUT", valuePath(x.plainKey, x.test), `{"value":"9"}`); resp.StatusCode != 200 {
		t.Fatalf("token write: %d %s", resp.StatusCode, body)
	}
	_, ids := bearer("GET", "/api/v1/resolve?repo=SHOP&file=app.env&env=prod", "")
	if !strings.Contains(string(ids), fmt.Sprintf(`"env":%d`, x.prod)) {
		t.Fatalf("resolve: %s", ids)
	}
	if resp, _ := bearer("POST", "/api/v1/auth/tokens", `{"name":"more"}`); resp.StatusCode != 403 {
		t.Fatal("a token created another token")
	}
	// Expired and revoked tokens stop working.
	e.st.Now = func() time.Time { return time.Now().Add(31 * 24 * time.Hour) }
	if resp, _ := bearer("GET", "/api/v1/repos", ""); resp.StatusCode != 401 {
		t.Fatal("expired token accepted")
	}
	e.st.Now = time.Now
	tokens := e.admin.expect(200, "GET", "/api/v1/auth/tokens", nil)["list"].([]any)
	e.admin.expect(204, "DELETE", fmt.Sprintf("/api/v1/auth/tokens/%d", id(tokens[0].(map[string]any)["id"])), nil)
	if resp, _ := bearer("GET", "/api/v1/repos", ""); resp.StatusCode != 401 {
		t.Fatal("revoked token accepted")
	}

	// Temporary access: past the end date the session and sign-in stop.
	m := e.member("temp@example.com")
	users := e.admin.expect(200, "GET", "/api/v1/users", nil)["list"].([]any)
	var memberID int64
	for _, u := range users {
		if u.(map[string]any)["email"] == "temp@example.com" {
			memberID = id(u.(map[string]any)["id"])
		}
	}
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	e.admin.expect(200, "PATCH", fmt.Sprintf("/api/v1/users/%d", memberID), map[string]any{"expiresAt": past})
	m.expect(401, "GET", "/api/v1/auth/me", nil)
	resp, body := e.anon().do("POST", "/api/v1/auth/login", map[string]string{"email": "temp@example.com", "password": "member-password"})
	if resp.StatusCode != 401 || !strings.Contains(string(body), "access_expired") {
		t.Fatalf("expired login: %d %s", resp.StatusCode, body)
	}
	e.admin.expect(200, "PATCH", fmt.Sprintf("/api/v1/users/%d", memberID), map[string]any{"expiresAt": ""})
	e.login("temp@example.com", "member-password")
	// The last permanent admin cannot be given an end date.
	me := e.admin.expect(200, "GET", "/api/v1/auth/me", nil)
	future := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	if resp, _ := e.admin.do("PATCH", fmt.Sprintf("/api/v1/users/%d", id(me["id"])), map[string]any{"expiresAt": future}); resp.StatusCode != 409 {
		t.Fatal("last admin got an end date")
	}
}

func TestImportDryRunSearchAuditFiltersAndLastSignIn(t *testing.T) {
	e := newEnv(t)
	x := e.seed()
	e.admin.expect(200, "PUT", valuePath(x.plainKey, x.test), map[string]string{"value": "8080"})

	// A dry run answers what would happen and stores nothing, audit included.
	before := len(e.auditActions(""))
	path := fmt.Sprintf("/api/v1/files/%d/import?env=%d&dryRun=true", x.file, x.test)
	res := e.admin.expect(200, "POST", path, "PORT=9090\nNEW_ONE=1\n")
	if len(res["updated"].([]any)) != 1 || len(res["added"].([]any)) != 1 || len(res["keysCreated"].([]any)) != 1 {
		t.Fatalf("dry run result: %v", res)
	}
	grid := e.admin.expect(200, "GET", fmt.Sprintf("/api/v1/files/%d/grid", x.file), nil)
	if rows := grid["rows"].([]any); len(rows) != 2 {
		t.Fatalf("dry run created keys: %d rows", len(rows))
	}
	if after := len(e.auditActions("")); after != before {
		t.Fatalf("dry run wrote audit rows: %d then %d", before, after)
	}

	// Audit filters by file, environment and key.
	e.admin.expect(200, "PUT", valuePath(x.plainKey, x.uat), map[string]string{"value": "8081"})
	items := e.admin.expect(200, "GET", fmt.Sprintf("/api/v1/audit?file=%d&env=%d&key=%d", x.file, x.uat, x.plainKey), nil)["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["action"] != "create_value" {
		t.Fatalf("filtered audit: %v", items)
	}

	// Search finds names only, prefix matches first, and treats % literally.
	hits := e.admin.expect(200, "GET", "/api/v1/search?q=por", nil)["list"].([]any)
	if len(hits) != 1 || hits[0].(map[string]any)["kind"] != "key" || hits[0].(map[string]any)["name"] != "PORT" {
		t.Fatalf("search: %v", hits)
	}
	if hits := e.admin.expect(200, "GET", "/api/v1/search?q=%25", nil)["list"].([]any); len(hits) != 0 {
		t.Fatalf("search for a literal %%: %v", hits)
	}

	// Signing in records when.
	me := e.admin.expect(200, "GET", "/api/v1/auth/me", nil)
	if s, _ := me["lastSignedInAt"].(string); s == "" {
		t.Fatalf("lastSignedInAt missing: %v", me)
	}
}

func TestAuditSettings(t *testing.T) {
	e := newEnv(t)
	x := e.seed()
	e.admin.expect(400, "PUT", "/api/v1/settings/audit", map[string]any{"off": []string{"access"}})
	e.member("m@example.com").expect(403, "PUT", "/api/v1/settings/audit", map[string]any{"off": []string{"logins"}})
	e.admin.expect(200, "PUT", "/api/v1/settings/audit", map[string]any{"off": []string{"values"}})
	before := len(e.auditActions("update_value")) + len(e.auditActions("create_value"))
	e.admin.expect(200, "PUT", valuePath(x.plainKey, x.test), map[string]string{"value": "1"})
	e.admin.expect(200, "GET", fmt.Sprintf("%s/reveal", valuePath(x.plainKey, x.test)), nil)
	if after := len(e.auditActions("update_value")) + len(e.auditActions("create_value")); after != before {
		t.Fatalf("value change recorded while off: %d then %d", before, after)
	}
	if len(e.auditActions("reveal_secret")) == 0 {
		t.Fatal("a reveal was not recorded")
	}
	if len(e.auditActions("update_audit_settings")) != 1 {
		t.Fatal("the settings change itself was not recorded")
	}
}

func TestAuditDateFilter(t *testing.T) {
	e := newEnv(t)
	e.seed()
	now := time.Now().UTC()
	q := func(since, until time.Time) int {
		path := fmt.Sprintf("/api/v1/audit?since=%s&until=%s", since.Format(time.RFC3339), until.Format(time.RFC3339))
		return len(e.admin.expect(200, "GET", path, nil)["items"].([]any))
	}
	if n := q(now.Add(-time.Hour), now.Add(time.Hour)); n == 0 {
		t.Fatal("nothing found for the last hour")
	}
	if n := q(now.Add(time.Hour), now.Add(2*time.Hour)); n != 0 {
		t.Fatalf("future range found %d entries", n)
	}
	e.admin.expect(400, "GET", "/api/v1/audit?since=yesterday", nil)
}
