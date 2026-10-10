package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Achal13jain/envgrid/internal/crypto"
	"github.com/Achal13jain/envgrid/internal/formats"
)

func TestMain(m *testing.M) {
	crypto.PasswordParams = crypto.Argon2Params{Time: 1, Memory: 1024, Threads: 1}
	os.Exit(m.Run())
}

var ctx = context.Background()

func newCipher(t *testing.T) *crypto.Cipher {
	t.Helper()
	key, _ := crypto.ParseKey(crypto.GenerateKey())
	c, err := crypto.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(ctx, ":memory:", newCipher(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func must[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

func str(s string) *string { return &s }

// fixture: one admin, one repo with test/uat/prod, one dotenv file.
type fixture struct {
	s    *Store
	user *User
	repo *Repo
	envs []Environment
	file *File
}

func newFixture(t *testing.T) fixture {
	s := newStore(t)
	u := must[*User](t)(s.CreateUser(ctx, "admin@example.com", "Admin", "password123", RoleAdmin))
	r := must[*Repo](t)(s.CreateRepo(ctx, "shop", "", "", u.ID))
	envs := must[[]Environment](t)(s.Environments(ctx, r.ID))
	f := must[*File](t)(s.CreateFile(ctx, r.ID, "app.env", formats.Dotenv))
	return fixture{s, u, r, envs, f}
}

func TestMigrationsAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "envgrid.db")
	c := newCipher(t)
	s := must[*Store](t)(Open(ctx, path, c))
	_ = s.Close()
	s = must[*Store](t)(Open(ctx, path, c))
	defer func() { _ = s.Close() }()
	var n int
	entries, _ := migrations.ReadDir("migrations")
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil || n != len(entries) {
		t.Fatalf("schema_migrations rows = %d, err %v", n, err)
	}
}

func TestWrongMasterKeyIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "envgrid.db")
	s := must[*Store](t)(Open(ctx, path, newCipher(t)))
	_ = s.Close()
	if _, err := Open(ctx, path, newCipher(t)); !errors.Is(err, ErrWrongMasterKey) {
		t.Fatalf("expected ErrWrongMasterKey, got %v", err)
	}
}

func TestDefaultEnvironments(t *testing.T) {
	fx := newFixture(t)
	if len(fx.envs) != 3 || fx.envs[0].Name != "test" || fx.envs[1].Name != "uat" || fx.envs[2].Name != "prod" {
		t.Fatalf("unexpected envs %+v", fx.envs)
	}
	if fx.envs[0].IsProtected || fx.envs[1].IsProtected || !fx.envs[2].IsProtected {
		t.Fatal("only prod should be protected")
	}
}

func TestValuesAreEncryptedAtRest(t *testing.T) {
	fx := newFixture(t)
	k := must[*Key](t)(fx.s.CreateKey(ctx, fx.file.ID, "PLAIN", "", false))
	must[ValueChange](t)(fx.s.SetValue(ctx, k.ID, fx.envs[0].ID, str("not-a-secret-but-encrypted"), fx.user.ID))
	var ct []byte
	if err := fx.s.db.QueryRow(`SELECT ciphertext FROM value_versions`).Scan(&ct); err != nil {
		t.Fatal(err)
	}
	if len(ct) < 28 || string(ct) == "not-a-secret-but-encrypted" {
		t.Fatal("value stored in plaintext")
	}
	// A ciphertext moved to another environment must not decrypt.
	if _, err := fx.s.db.Exec(`UPDATE value_versions SET environment_id = ?`, fx.envs[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.s.Grid(ctx, fx.file.ID); !errors.Is(err, crypto.ErrDecrypt) {
		t.Fatalf("moved ciphertext decrypted: %v", err)
	}
}

func TestGridWithVersionsAndDeletions(t *testing.T) {
	fx := newFixture(t)
	s, test, uat, prod := fx.s, fx.envs[0].ID, fx.envs[1].ID, fx.envs[2].ID
	a := must[*Key](t)(s.CreateKey(ctx, fx.file.ID, "A", "first", false))
	b := must[*Key](t)(s.CreateKey(ctx, fx.file.ID, "B", "", true))
	c := must[*Key](t)(s.CreateKey(ctx, fx.file.ID, "C", "", false))
	set := func(k *Key, env int64, v *string) {
		t.Helper()
		must[ValueChange](t)(s.SetValue(ctx, k.ID, env, v, fx.user.ID))
	}
	set(a, test, str("a1"))
	set(a, test, str("a2"))
	set(a, test, str("a3")) // three versions: latest wins
	set(a, uat, str("u1"))
	set(a, uat, nil) // deleted: missing, but versioned
	set(b, prod, str("p1"))
	set(b, prod, nil)
	set(b, prod, str("p2")) // re-created after deletion
	set(c, test, str(""))   // empty string is present

	// Other files' values must not leak into this grid.
	other := must[*File](t)(s.CreateFile(ctx, fx.repo.ID, "other.env", formats.Dotenv))
	ok := must[*Key](t)(s.CreateKey(ctx, other.ID, "A", "", false))
	set(ok, test, str("other"))

	g := must[*Grid](t)(s.Grid(ctx, fx.file.ID))
	if len(g.Environments) != 3 || len(g.Rows) != 3 {
		t.Fatalf("grid shape %d envs x %d rows", len(g.Environments), len(g.Rows))
	}
	type want struct {
		present bool
		value   string
		version int
	}
	expect := map[string][3]want{
		"A": {{true, "a3", 3}, {false, "", 2}, {false, "", 0}},
		"B": {{false, "", 0}, {false, "", 0}, {true, "p2", 3}},
		"C": {{true, "", 1}, {false, "", 0}, {false, "", 0}},
	}
	for _, r := range g.Rows {
		for i, c := range r.Cells {
			w := expect[r.Key.Name][i]
			if c.Present != w.present || c.Plaintext != w.value || c.Version != w.version || c.EnvironmentID != g.Environments[i].ID {
				t.Errorf("%s/%s: got %+v want %+v", r.Key.Name, g.Environments[i].Name, c, w)
			}
			if c.Version > 0 && (c.UpdatedBy == nil || c.UpdatedBy.Email != "admin@example.com" || c.UpdatedAt == "") {
				t.Errorf("%s/%s: missing who/when", r.Key.Name, g.Environments[i].Name)
			}
		}
	}
	if g.Rows[0].Key.Name != "A" || g.Rows[0].Key.Description != "first" || !g.Rows[1].Key.IsSecret {
		t.Error("key metadata wrong")
	}

	// Reordering keys and environments changes grid order.
	if err := s.ReorderKeys(ctx, fx.file.ID, []int64{c.ID, a.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReorderEnvironments(ctx, fx.repo.ID, []int64{prod, test, uat}); err != nil {
		t.Fatal(err)
	}
	g = must[*Grid](t)(s.Grid(ctx, fx.file.ID))
	if g.Rows[0].Key.Name != "C" || g.Environments[0].Name != "prod" || g.Rows[2].Cells[0].Plaintext != "p2" {
		t.Fatal("reorder not reflected in grid")
	}
	if err := s.ReorderKeys(ctx, fx.file.ID, []int64{c.ID, c.ID, b.ID}); !errors.Is(err, ErrBadOrder) {
		t.Fatalf("duplicate ids accepted: %v", err)
	}
}

func TestGridWithoutKeysStillListsEnvironments(t *testing.T) {
	fx := newFixture(t)
	g := must[*Grid](t)(fx.s.Grid(ctx, fx.file.ID))
	if len(g.Environments) != 3 || len(g.Rows) != 0 {
		t.Fatalf("got %d envs, %d rows", len(g.Environments), len(g.Rows))
	}
}

func TestSetValueNoOpWhenUnchanged(t *testing.T) {
	fx := newFixture(t)
	k := must[*Key](t)(fx.s.CreateKey(ctx, fx.file.ID, "K", "", false))
	env := fx.envs[0].ID
	if ch := must[ValueChange](t)(fx.s.SetValue(ctx, k.ID, env, str("v"), fx.user.ID)); !ch.Changed || ch.WasPresent || ch.Version != 1 {
		t.Fatalf("first set: %+v", ch)
	}
	if ch := must[ValueChange](t)(fx.s.SetValue(ctx, k.ID, env, str("v"), fx.user.ID)); ch.Changed || ch.Version != 1 {
		t.Fatalf("same value wrote a version: %+v", ch)
	}
	if ch := must[ValueChange](t)(fx.s.SetValue(ctx, k.ID, env, nil, fx.user.ID)); !ch.Changed || !ch.WasPresent || ch.Version != 2 {
		t.Fatalf("delete: %+v", ch)
	}
	if ch := must[ValueChange](t)(fx.s.SetValue(ctx, k.ID, env, nil, fx.user.ID)); ch.Changed {
		t.Fatalf("second delete wrote a version: %+v", ch)
	}
	h := must[[]Version](t)(fx.s.History(ctx, k.ID, env))
	if len(h) != 2 || h[0].Version != 2 || h[0].Present || h[1].Plaintext != "v" {
		t.Fatalf("history %+v", h)
	}
	if v, present, err := fx.s.ValueAt(ctx, k.ID, env, 1); err != nil || !present || v != "v" {
		t.Fatalf("ValueAt(1) = %q %v %v", v, present, err)
	}
	if _, _, err := fx.s.ValueAt(ctx, k.ID, env, 9); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ValueAt(9) err %v", err)
	}
}

func TestCompare(t *testing.T) {
	fx := newFixture(t)
	s, test, prod := fx.s, fx.envs[0].ID, fx.envs[2].ID
	vals := []struct {
		name       string
		a, b       *string
		wantBucket string
	}{
		{"ONLY_B", nil, str("x"), "missingInA"},
		{"ONLY_A", str("x"), nil, "missingInB"},
		{"DIFF", str("1"), str("2"), "different"},
		{"SAME", str("s"), str("s"), "same"},
		{"EMPTY_VS_SET", str(""), str("v"), "different"},
		{"NEITHER", nil, nil, ""},
		{"DELETED_IN_B", str("x"), str("x"), "missingInB"},
	}
	for _, v := range vals {
		k := must[*Key](t)(s.CreateKey(ctx, fx.file.ID, v.name, "", v.name == "DIFF"))
		if v.a != nil {
			must[ValueChange](t)(s.SetValue(ctx, k.ID, test, v.a, fx.user.ID))
		}
		if v.b != nil {
			must[ValueChange](t)(s.SetValue(ctx, k.ID, prod, v.b, fx.user.ID))
		}
		if v.name == "DELETED_IN_B" {
			must[ValueChange](t)(s.SetValue(ctx, k.ID, prod, nil, fx.user.ID))
		}
	}
	g := must[*Grid](t)(s.Grid(ctx, fx.file.ID))
	c := must[*Comparison](t)(Compare(g, test, prod))
	got := map[string]string{}
	for bucket, items := range map[string][]CompareItem{"missingInA": c.MissingInA, "missingInB": c.MissingInB, "different": c.Different, "same": c.Same} {
		for _, it := range items {
			got[it.Key.Name] = bucket
		}
	}
	for _, v := range vals {
		if got[v.name] != v.wantBucket {
			t.Errorf("%s: bucket %q, want %q", v.name, got[v.name], v.wantBucket)
		}
	}
	if _, err := Compare(g, test, 9999); !errors.Is(err, ErrUnknownEnvironment) {
		t.Fatal("unknown environment accepted")
	}
}

func TestImportSummary(t *testing.T) {
	fx := newFixture(t)
	s, env := fx.s, fx.envs[0].ID
	existing := must[*Key](t)(s.CreateKey(ctx, fx.file.ID, "KEEP", "", false))
	must[ValueChange](t)(s.SetValue(ctx, existing.ID, env, str("same"), fx.user.ID))
	changed := must[*Key](t)(s.CreateKey(ctx, fx.file.ID, "CHANGE", "", false))
	must[ValueChange](t)(s.SetValue(ctx, changed.ID, env, str("old"), fx.user.ID))
	must[*Key](t)(s.CreateKey(ctx, fx.file.ID, "EMPTY_SLOT", "", false)) // key exists, no value
	untouched := must[*Key](t)(s.CreateKey(ctx, fx.file.ID, "UNTOUCHED", "", false))
	must[ValueChange](t)(s.SetValue(ctx, untouched.ID, env, str("stay"), fx.user.ID))

	kvs := must[[]formats.KV](t)(formats.Parse(formats.Dotenv, []byte("KEEP=same\nCHANGE=new\nEMPTY_SLOT=filled\nNEW_KEY=1\nAPI_TOKEN=t\n")))
	base := AuditEntry{UserID: fx.user.ID, RepoID: fx.repo.ID, FileID: fx.file.ID, EnvironmentID: env, Detail: map[string]any{"env": "test"}}
	res := must[*ImportResult](t)(s.Import(ctx, fx.file, env, kvs, base, false))

	check := func(name string, got []string, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s = %v, want %v", name, got, want)
			}
		}
	}
	check("added", res.Added, "EMPTY_SLOT", "NEW_KEY", "API_TOKEN")
	check("updated", res.Updated, "CHANGE")
	check("unchanged", res.Unchanged, "KEEP")
	check("keysCreated", res.KeysCreated, "NEW_KEY", "API_TOKEN")

	g := must[*Grid](t)(s.Grid(ctx, fx.file.ID))
	byName := map[string]GridRow{}
	for _, r := range g.Rows {
		byName[r.Key.Name] = r
	}
	if byName["UNTOUCHED"].Cells[0].Plaintext != "stay" || byName["CHANGE"].Cells[0].Plaintext != "new" {
		t.Fatal("import wrote the wrong values")
	}
	if !byName["API_TOKEN"].Key.IsSecret || byName["NEW_KEY"].Key.IsSecret {
		t.Fatal("secret-name heuristic not applied to new keys")
	}
	audit := must[[]AuditRecord](t)(s.AuditLog(ctx, AuditFilter{RepoID: fx.repo.ID}))
	if len(audit) != 1 || audit[0].Action != "import_file" {
		t.Fatalf("import wrote %d audit rows, want one import_file", len(audit))
	}

	// Re-running the same import changes nothing.
	again := must[*ImportResult](t)(s.Import(ctx, fx.file, env, kvs, base, false))
	if len(again.Added)+len(again.Updated)+len(again.KeysCreated) != 0 || len(again.Unchanged) != 5 {
		t.Fatalf("second import %+v", again)
	}

	// Invalid key names abort the whole import.
	bad := []formats.KV{{Key: "OK", Value: "1"}, {Key: "bad key", Value: "2"}}
	if _, err := s.Import(ctx, fx.file, env, bad, base, false); err == nil {
		t.Fatal("invalid key accepted")
	}
	for _, r := range must[*Grid](t)(s.Grid(ctx, fx.file.ID)).Rows {
		if r.Key.Name == "OK" {
			t.Fatal("partial import")
		}
	}
}

func TestRawFileHasImplicitKey(t *testing.T) {
	fx := newFixture(t)
	f := must[*File](t)(fx.s.CreateFile(ctx, fx.repo.ID, "nginx.conf", formats.Raw))
	kvs := must[[]formats.KV](t)(formats.Parse(formats.Raw, []byte("server {}\n")))
	res := must[*ImportResult](t)(fx.s.Import(ctx, f, fx.envs[0].ID, kvs, AuditEntry{UserID: fx.user.ID}, false))
	if len(res.KeysCreated) != 0 || len(res.Added) != 1 {
		t.Fatalf("raw import %+v", res)
	}
	g := must[*Grid](t)(fx.s.Grid(ctx, f.ID))
	if len(g.Rows) != 1 || g.Rows[0].Key.Name != formats.RawKey || g.Rows[0].Cells[0].Plaintext != "server {}\n" {
		t.Fatalf("raw grid %+v", g.Rows)
	}
}

func TestSessionExpiry(t *testing.T) {
	s := newStore(t)
	u := must[*User](t)(s.CreateUser(ctx, "a@example.com", "", "password123", RoleMember))
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }

	token, expires, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !expires.Equal(now.Add(30 * 24 * time.Hour)) {
		t.Fatalf("expiry %v", expires)
	}
	if got, err := s.SessionUser(ctx, token); err != nil || got.ID != u.ID {
		t.Fatalf("fresh session rejected: %v", err)
	}
	now = now.Add(30*24*time.Hour - time.Second)
	if _, err := s.SessionUser(ctx, token); err != nil {
		t.Fatal("session expired early")
	}
	now = now.Add(time.Second)
	if _, err := s.SessionUser(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired session accepted")
	}
	// Creating a new session sweeps expired rows.
	must[string](t)(func() (string, error) { tok, _, err := s.CreateSession(ctx, u.ID); return tok, err }())
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n)
	if n != 1 {
		t.Fatalf("expired session not swept, %d rows", n)
	}
}

func TestSessionsEndOnDisableAndPasswordChange(t *testing.T) {
	s := newStore(t)
	must[*User](t)(s.CreateUser(ctx, "root@example.com", "", "password123", RoleAdmin))
	u := must[*User](t)(s.CreateUser(ctx, "a@example.com", "", "password123", RoleMember))
	tok, _, _ := s.CreateSession(ctx, u.ID)
	must[*User](t)(s.UpdateUser(ctx, u.ID, UserPatch{Password: str("new-password")}))
	if _, err := s.SessionUser(ctx, tok); err == nil {
		t.Fatal("session survived password change")
	}
	if _, err := s.Authenticate(ctx, "A@example.com", "new-password"); err != nil {
		t.Fatalf("login with new password (case-insensitive email): %v", err)
	}
	tok, _, _ = s.CreateSession(ctx, u.ID)
	disabled := true
	must[*User](t)(s.UpdateUser(ctx, u.ID, UserPatch{Disabled: &disabled}))
	if _, err := s.SessionUser(ctx, tok); err == nil {
		t.Fatal("session survived disable")
	}
	if _, err := s.Authenticate(ctx, "a@example.com", "new-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal("disabled user authenticated")
	}
}

func TestLastAdminCannotBeRemoved(t *testing.T) {
	s := newStore(t)
	a := must[*User](t)(s.CreateUser(ctx, "root@example.com", "", "password123", RoleAdmin))
	member := RoleMember
	if _, err := s.UpdateUser(ctx, a.ID, UserPatch{Role: &member}); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("demoted the last admin: %v", err)
	}
	if u := must[*User](t)(s.User(ctx, a.ID)); u.Role != RoleAdmin {
		t.Fatal("change was not rolled back")
	}
}

func TestAuditVisibilityForMembers(t *testing.T) {
	fx := newFixture(t)
	s := fx.s
	m := must[*User](t)(s.CreateUser(ctx, "m@example.com", "", "password123", RoleMember))
	_ = s.Audit(ctx, AuditEntry{UserID: fx.user.ID, Action: "login"})
	_ = s.Audit(ctx, AuditEntry{UserID: m.ID, Action: "login"})
	_ = s.Audit(ctx, AuditEntry{UserID: fx.user.ID, Action: "create_repo", RepoID: fx.repo.ID})

	all := must[[]AuditRecord](t)(s.AuditLog(ctx, AuditFilter{}))
	mine := must[[]AuditRecord](t)(s.AuditLog(ctx, AuditFilter{MemberID: m.ID}))
	if len(all) != 3 || len(mine) != 2 {
		t.Fatalf("admin sees %d, member sees %d", len(all), len(mine))
	}
	page := must[[]AuditRecord](t)(s.AuditLog(ctx, AuditFilter{Limit: 1}))
	next := must[[]AuditRecord](t)(s.AuditLog(ctx, AuditFilter{Limit: 5, Before: page[0].ID}))
	if len(page) != 1 || len(next) != 2 || next[0].ID >= page[0].ID {
		t.Fatal("cursor pagination broken")
	}
	logins := must[[]AuditRecord](t)(s.AuditLog(ctx, AuditFilter{Action: "login", UserID: m.ID}))
	if len(logins) != 1 || logins[0].User.Email != "m@example.com" {
		t.Fatal("action/user filter broken")
	}
}

func TestKeyHasProtectedValues(t *testing.T) {
	fx := newFixture(t)
	k := must[*Key](t)(fx.s.CreateKey(ctx, fx.file.ID, "K", "", false))
	must[ValueChange](t)(fx.s.SetValue(ctx, k.ID, fx.envs[0].ID, str("x"), fx.user.ID))
	if has := must[bool](t)(fx.s.KeyHasProtectedValues(ctx, k.ID)); has {
		t.Fatal("test env is not protected")
	}
	must[ValueChange](t)(fx.s.SetValue(ctx, k.ID, fx.envs[2].ID, str("x"), fx.user.ID))
	if has := must[bool](t)(fx.s.KeyHasProtectedValues(ctx, k.ID)); !has {
		t.Fatal("prod value not detected")
	}
	// Deleting the value keeps its prod history, which only admins may remove.
	must[ValueChange](t)(fx.s.SetValue(ctx, k.ID, fx.envs[2].ID, nil, fx.user.ID))
	if has := must[bool](t)(fx.s.KeyHasProtectedValues(ctx, k.ID)); !has {
		t.Fatal("prod history not counted after the value was deleted")
	}
}

func TestSeed(t *testing.T) {
	s := newStore(t)
	u := must[*User](t)(s.CreateUser(ctx, "root@example.com", "", "password123", RoleAdmin))
	repo := must[*Repo](t)(s.Seed(ctx, u.ID))
	if repo.FileCount != 2 || repo.EnvCount != 3 {
		t.Fatalf("seed repo %+v", repo)
	}
	if _, err := s.Seed(ctx, u.ID); !errors.Is(err, ErrAlreadySeeded) {
		t.Fatalf("second seed: %v", err)
	}
	files := must[[]File](t)(s.Files(ctx, repo.ID))
	keys, missing := 0, 0
	for _, f := range files {
		g := must[*Grid](t)(s.Grid(ctx, f.ID))
		for _, r := range g.Rows {
			keys++
			for _, c := range r.Cells {
				if !c.Present {
					missing++
				}
			}
		}
	}
	if keys < 15 || missing == 0 {
		t.Fatalf("seed has %d keys, %d missing cells", keys, missing)
	}
}

// The 0002 rebuild of the files table must keep keys and values, which would
// be cascade-deleted if foreign keys were on during the migration.
func TestFilesRebuildKeepsChildRows(t *testing.T) {
	fx := newFixture(t)
	k := must[*Key](t)(fx.s.CreateKey(ctx, fx.file.ID, "KEEP_ME", "", false))
	must[ValueChange](t)(fx.s.SetValue(ctx, k.ID, fx.envs[0].ID, str("still here"), fx.user.ID))

	if _, err := fx.s.db.Exec(`DELETE FROM schema_migrations WHERE version = 2`); err != nil {
		t.Fatal(err)
	}
	if err := fx.s.migrate(ctx); err != nil {
		t.Fatal(err)
	}
	g := must[*Grid](t)(fx.s.Grid(ctx, fx.file.ID))
	if len(g.Rows) != 1 || g.Rows[0].Cells[0].Plaintext != "still here" {
		t.Fatalf("rebuild lost data: %+v", g.Rows)
	}
	var fk int
	if err := fx.s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign keys not back on: %d %v", fk, err)
	}
	must[*File](t)(fx.s.CreateFile(ctx, fx.repo.ID, "constants.php", formats.PHP))
	must[*File](t)(fx.s.CreateFile(ctx, fx.repo.ID, "app.ini", formats.INI))
	// Cascades still work after the rebuild.
	if err := fx.s.DeleteFile(ctx, fx.file.ID); err != nil {
		t.Fatal(err)
	}
	var keys int
	_ = fx.s.db.QueryRow(`SELECT COUNT(*) FROM keys`).Scan(&keys)
	if keys != 0 {
		t.Fatalf("deleting the file left %d keys behind", keys)
	}
}

func TestRepoCoverage(t *testing.T) {
	fx := newFixture(t)
	a := must[*Key](t)(fx.s.CreateKey(ctx, fx.file.ID, "A", "", false))
	b := must[*Key](t)(fx.s.CreateKey(ctx, fx.file.ID, "B", "", false))
	other := must[*File](t)(fx.s.CreateFile(ctx, fx.repo.ID, "other.env", formats.Dotenv))
	c := must[*Key](t)(fx.s.CreateKey(ctx, other.ID, "C", "", false))
	test, prod := fx.envs[0].ID, fx.envs[2].ID
	for _, k := range []*Key{a, b, c} {
		must[ValueChange](t)(fx.s.SetValue(ctx, k.ID, test, str("v"), fx.user.ID))
	}
	must[ValueChange](t)(fx.s.SetValue(ctx, a.ID, prod, str("v"), fx.user.ID))
	must[ValueChange](t)(fx.s.SetValue(ctx, b.ID, prod, str("v"), fx.user.ID))
	must[ValueChange](t)(fx.s.SetValue(ctx, b.ID, prod, nil, fx.user.ID)) // deleted: not present

	// A second repo must not leak into the first one's counts.
	r2 := must[*Repo](t)(fx.s.CreateRepo(ctx, "second", "", "", fx.user.ID))
	f2 := must[*File](t)(fx.s.CreateFile(ctx, r2.ID, "x.env", formats.Dotenv))
	k2 := must[*Key](t)(fx.s.CreateKey(ctx, f2.ID, "X", "", false))
	envs2 := must[[]Environment](t)(fx.s.Environments(ctx, r2.ID))
	must[ValueChange](t)(fx.s.SetValue(ctx, k2.ID, envs2[0].ID, str("v"), fx.user.ID))

	all := must[[]Repo](t)(fx.s.Repos(ctx))
	shop := &all[1] // sorted by name: "second", then "shop"
	for _, r := range []*Repo{must[*Repo](t)(fx.s.Repo(ctx, fx.repo.ID)), shop} {
		if r.Name != "shop" || r.KeyCount != 3 || len(r.Coverage) != 3 {
			t.Fatalf("repo %+v", r)
		}
		got := []int{r.Coverage[0].Present, r.Coverage[1].Present, r.Coverage[2].Present}
		if got[0] != 3 || got[1] != 0 || got[2] != 1 || r.Coverage[2].Name != "prod" || !r.Coverage[2].IsProtected {
			t.Fatalf("coverage %+v", r.Coverage)
		}
	}
}

func TestOpenRefusesNewerSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "envgrid.db")
	c := newCipher(t)
	s, err := Open(ctx, path, c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (999, 'later')`); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if _, err := Open(ctx, path, c); err == nil || !strings.Contains(err.Error(), "newer envgrid") {
		t.Fatalf("opening a newer schema: %v", err)
	}
}

func TestEnvironmentLimit(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	for i := len(fx.envs); i < MaxEnvironments; i++ {
		if _, err := fx.s.CreateEnvironment(ctx, fx.repo.ID, fmt.Sprintf("e%d", i), false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fx.s.CreateEnvironment(ctx, fx.repo.ID, "one-too-many", false); !errors.Is(err, ErrTooMany) {
		t.Fatalf("environment past the limit: %v", err)
	}
}

func TestFileByteBudget(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	big := strings.Repeat("x", 1<<20)
	var first int64
	for i := 0; i < MaxFileBytes>>20; i++ {
		k := must[*Key](t)(fx.s.CreateKey(ctx, fx.file.ID, fmt.Sprintf("BIG_%d", i), "", false))
		must[ValueChange](t)(fx.s.SetValue(ctx, k.ID, fx.envs[0].ID, &big, fx.user.ID))
		if i == 0 {
			first = k.ID
		}
	}
	k := must[*Key](t)(fx.s.CreateKey(ctx, fx.file.ID, "ONE_MORE", "", false))
	if _, err := fx.s.SetValue(ctx, k.ID, fx.envs[0].ID, &big, fx.user.ID); !errors.Is(err, ErrTooMany) {
		t.Fatalf("value past the file budget: %v", err)
	}
	// Deletes cascade past setValue, so they must give the bytes back.
	if err := fx.s.DeleteKey(ctx, first); err != nil {
		t.Fatal(err)
	}
	must[ValueChange](t)(fx.s.SetValue(ctx, k.ID, fx.envs[0].ID, &big, fx.user.ID))
	if err := fx.s.DeleteEnvironment(ctx, fx.envs[0].ID); err != nil {
		t.Fatal(err)
	}
	var left int64
	if err := fx.s.db.QueryRow(`SELECT value_bytes FROM files WHERE id = ?`, fx.file.ID).Scan(&left); err != nil || left != 0 {
		t.Fatalf("bytes after deleting the environment: %d %v", left, err)
	}
}

func TestLatestValueQueriesUseTheIndex(t *testing.T) {
	fx := newFixture(t)
	for _, q := range []string{gridQuery, coverageQuery, valuesAtQuery} {
		if p := queryPlan(t, fx.s, q, fx.file.ID, fx.envs[0].ID, "2026-01-01T00:00:00Z"); strings.Contains(p, "MATERIALIZE") || strings.Contains(p, "SCAN value_versions") || strings.Contains(p, "value_versions_env") {
			t.Fatalf("query reads whole history:\n%s", p)
		}
	}
}

func queryPlan(t *testing.T, s *Store, q string, args ...any) string {
	t.Helper()
	rows, err := s.db.Query("EXPLAIN QUERY PLAN "+q, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(detail + "\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return plan.String()
}
