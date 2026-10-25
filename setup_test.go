package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/Achal13jain/envgrid/internal/crypto"
	"github.com/Achal13jain/envgrid/internal/store"
)

// memStore stands in for the operating system's secure store.
type memStore struct {
	key        []byte
	fail       error // Get and Set fail
	getFail    error // only Get fails, while a key may still be stored
	setFail    error // only Set fails
	deleteFail error // only for keyKeeper
}

func (m *memStore) Name() string { return "the test store" }

func (m *memStore) Get() ([]byte, error) {
	if m.fail != nil {
		return nil, m.fail
	}
	if m.getFail != nil {
		return nil, m.getFail
	}
	if m.key == nil {
		return nil, errKeyNotStored
	}
	return m.key, nil
}

func (m *memStore) Set(k []byte) error {
	if m.fail != nil {
		return m.fail
	}
	if m.setFail != nil {
		return m.setFail
	}
	m.key = append([]byte(nil), k...)
	return nil
}

func (m *memStore) Delete(k []byte) error {
	if _, err := m.Get(); err != nil && !errors.Is(err, errKeyNotStored) {
		return err
	}
	if bytes.Equal(m.key, k) {
		m.key = nil
	}
	return nil
}

// fileStore is a secure store that survives between processes, for the
// restart test only.
type fileStore struct{ path string }

func (f fileStore) Name() string { return "the test store" }
func (f fileStore) Get() ([]byte, error) {
	if os.Getenv("ENVGRID_TEST_STORE_LOCKED") == "1" {
		return nil, errors.New("the test store is locked")
	}
	b, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, errKeyNotStored
	}
	return b, err
}
func (f fileStore) Set(k []byte) error  { return os.WriteFile(f.path, k, 0o600) }
func (f fileStore) Delete([]byte) error { return os.Remove(f.path) }

// TestMain lets a test run this binary as envgrid itself, in a new process.
func TestMain(m *testing.M) {
	if os.Getenv("ENVGRID_TEST_HELPER") == "1" {
		dir := os.Getenv("ENVGRID_TEST_SETUP_DIR")
		setupDir = func() (string, error) { return dir, nil }
		openSecretStore = func() (secretStore, error) { return fileStore{path: filepath.Join(dir, "test-store")}, nil }
		isTerminal = func() bool { return false }
		os.Args = append([]string{"envgrid"}, strings.Fields(os.Getenv("ENVGRID_TEST_ARGS"))...)
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// localEnv gives a test its own settings folder, data folder and key store,
// and clears every envgrid variable that would otherwise take precedence.
func localEnv(t *testing.T) (st *memStore, dataDir string) {
	t.Helper()
	cfgDir := t.TempDir()
	dataDir = filepath.Join(t.TempDir(), "data")
	st = &memStore{}
	oldSetup, oldData, oldOpen, oldTerm, oldStdin := setupDir, localDataDir, openSecretStore, isTerminal, stdin
	setupDir = func() (string, error) { return cfgDir, nil }
	localDataDir = func() (string, error) { return dataDir, nil }
	openSecretStore = func() (secretStore, error) { return st, nil }
	isTerminal = func() bool { return false }
	t.Cleanup(func() {
		setupDir, localDataDir, openSecretStore, isTerminal, stdin = oldSetup, oldData, oldOpen, oldTerm, oldStdin
	})
	for _, k := range []string{"ENVGRID_MASTER_KEY", "ENVGRID_DATA_DIR", "ENVGRID_LISTEN", "ENVGRID_SECURE_COOKIES"} {
		t.Setenv(k, "")
	}
	t.Setenv("ENVGRID_ADMIN_EMAIL", "admin@example.com")
	t.Setenv("ENVGRID_ADMIN_PASSWORD", "admin-password")
	setStdin("")
	return st, dataDir
}

func setStdin(s string) { stdin = bufio.NewReader(strings.NewReader(s)) }

func setupExists(t *testing.T) bool {
	t.Helper()
	path, err := setupFile()
	if err != nil {
		t.Fatal(err)
	}
	return fileExists(path)
}

// makeDatabase creates a database with key, as an earlier envgrid run would.
func makeDatabase(t *testing.T, dataDir string) string {
	t.Helper()
	raw := crypto.GenerateKey()
	key, _ := crypto.ParseKey(raw)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := openExisting(context.Background(), dataDir, key)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	return raw
}

var keyPattern = regexp.MustCompile(`[A-Za-z0-9+/]{43}=`)

func TestInitThenStartNeedsNothingSet(t *testing.T) {
	st, dataDir := localEnv(t)
	if err := initCmd(nil); err != nil {
		t.Fatal(err)
	}
	if st.key == nil || !fileExists(dbPath(dataDir)) || !setupExists(t) {
		t.Fatalf("init left key=%v db=%v setup=%v", st.key != nil, fileExists(dbPath(dataDir)), setupExists(t))
	}
	t.Setenv("ENVGRID_ADMIN_EMAIL", "")
	t.Setenv("ENVGRID_ADMIN_PASSWORD", "")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	want, _ := crypto.ParseKey(string(st.key))
	if !samePath(cfg.dataDir, dataDir) || string(cfg.masterKey) != string(want) || cfg.listen != localListen || cfg.secureCookies || cfg.keySource != "the test store" {
		t.Fatalf("config after init: %+v", cfg)
	}
	db, err := openExisting(context.Background(), dataDir, cfg.masterKey)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if n, _ := db.UserCount(context.Background()); n != 1 {
		t.Fatalf("users after init: %d", n)
	}
}

func TestRestartInNewProcess(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(t.TempDir(), "data")
	envgrid := func(args string, extra ...string) (string, error) {
		cmd := exec.Command(os.Args[0])
		cmd.Env = append(os.Environ(), "ENVGRID_TEST_HELPER=1", "ENVGRID_TEST_SETUP_DIR="+dir, "ENVGRID_TEST_ARGS="+args,
			"ENVGRID_MASTER_KEY=", "ENVGRID_DATA_DIR=", "ENVGRID_LISTEN=", "ENVGRID_SECURE_COOKIES=", "ENVGRID_ADMIN_EMAIL=", "ENVGRID_ADMIN_PASSWORD=")
		cmd.Env = append(cmd.Env, extra...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	out, err := envgrid("init --data-dir "+dataDir, "ENVGRID_ADMIN_EMAIL=admin@example.com", "ENVGRID_ADMIN_PASSWORD=admin-password")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	printed := keyPattern.FindString(out)
	if printed == "" {
		t.Fatalf("init did not show the new key:\n%s", out)
	}
	// A new process with nothing set finds the folder and the key on its own.
	if out, err = envgrid("seed"); err != nil || !strings.Contains(out, "Created demo repo") {
		t.Fatalf("seed in a new process: %v\n%s", err, out)
	}
	if out, err = envgrid("key"); err != nil || !strings.Contains(out, printed) {
		t.Fatalf("key in a new process: %v\n%s", err, out)
	}
	// A locked store stops the start with instructions and changes nothing.
	if out, err = envgrid("seed", "ENVGRID_TEST_STORE_LOCKED=1"); err == nil || !strings.Contains(out, "Nothing was created or changed") {
		t.Fatalf("seed with a locked store: %v\n%s", err, out)
	}
}

func TestNoNewKeyForAnExistingDatabase(t *testing.T) {
	st, dataDir := localEnv(t)
	original := makeDatabase(t, dataDir)

	// serve without a key: no fresh key is offered next to the database.
	t.Setenv("ENVGRID_DATA_DIR", dataDir)
	err := run(seed)
	if err == nil || !strings.Contains(err.Error(), "already exists") || keyPattern.MatchString(err.Error()) {
		t.Fatalf("start without a key: %v", err)
	}

	// init without a key for it refuses, and stores nothing.
	t.Setenv("ENVGRID_DATA_DIR", "")
	if err := initCmd([]string{"--data-dir", dataDir}); err == nil || !strings.Contains(err.Error(), "will not make a new key") {
		t.Fatalf("init with no key for an existing database: %v", err)
	}
	// A wrong key is refused before anything is stored.
	setStdin(crypto.GenerateKey() + "\n")
	if err := initCmd([]string{"--data-dir", dataDir}); err == nil || !strings.Contains(err.Error(), "does not open the database") {
		t.Fatalf("init with the wrong key: %v", err)
	}
	if st.key != nil || setupExists(t) {
		t.Fatal("a refused init stored a key or a setup")
	}
	// The right key is kept.
	setStdin(original + "\n")
	if err := initCmd([]string{"--data-dir", dataDir}); err != nil {
		t.Fatal(err)
	}
	if string(st.key) != original {
		t.Fatal("init did not keep the database's own key")
	}
}

func TestEnvironmentVariablesTakePrecedence(t *testing.T) {
	_, dataDir := localEnv(t)
	if err := initCmd(nil); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "other")
	otherKey := crypto.GenerateKey()
	t.Setenv("ENVGRID_DATA_DIR", other)
	t.Setenv("ENVGRID_MASTER_KEY", otherKey)
	t.Setenv("ENVGRID_LISTEN", "127.0.0.1:9999")
	t.Setenv("ENVGRID_SECURE_COOKIES", "true")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	want, _ := crypto.ParseKey(otherKey)
	if cfg.dataDir != other || string(cfg.masterKey) != string(want) || cfg.listen != "127.0.0.1:9999" || !cfg.secureCookies || cfg.keySource != "ENVGRID_MASTER_KEY" || cfg.fromSetup {
		t.Fatalf("environment did not win: %+v", cfg)
	}
	// The stored key belongs to the setup's folder only.
	t.Setenv("ENVGRID_MASTER_KEY", "")
	if _, err := loadConfig(); !errors.Is(err, errNoMasterKey) {
		t.Fatalf("stored key used for another folder: %v", err)
	}
	_ = dataDir
}

func TestUnavailableStoreChangesNothing(t *testing.T) {
	st, dataDir := localEnv(t)
	st.fail = errors.New("the keyring is locked")
	err := initCmd(nil)
	if err == nil || !strings.Contains(err.Error(), "Nothing was created or changed") || !strings.Contains(err.Error(), "ENVGRID_MASTER_KEY") {
		t.Fatalf("init with a locked store: %v", err)
	}
	if fileExists(dbPath(dataDir)) || setupExists(t) {
		t.Fatal("init created a database or a setup without storing the key")
	}

	st.fail = nil
	if err := initCmd(nil); err != nil {
		t.Fatal(err)
	}
	st.fail = errors.New("the keyring is locked")
	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "could not read the key") {
		t.Fatalf("start with a locked store: %v", err)
	}
}

func TestMissingDatabaseIsNotRecreated(t *testing.T) {
	_, dataDir := localEnv(t)
	if err := initCmd(nil); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dataDir); err != nil {
		t.Fatal(err)
	}
	if err := run(seed); err == nil || !strings.Contains(err.Error(), "is missing") {
		t.Fatalf("start with the database gone: %v", err)
	}
	if fileExists(dbPath(dataDir)) {
		t.Fatal("a new, empty database was created")
	}
}

func TestPlainHTTPCookiesOnlyOnThisComputer(t *testing.T) {
	localEnv(t)
	if err := initCmd(nil); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENVGRID_LISTEN", ":8080")
	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "this computer only") {
		t.Fatalf("plain-HTTP cookies on every interface: %v", err)
	}
	t.Setenv("ENVGRID_SECURE_COOKIES", "true")
	if _, err := loadConfig(); err != nil {
		t.Fatalf("explicit secure cookies: %v", err)
	}
}

func TestInitAgainChangesNothing(t *testing.T) {
	st, _ := localEnv(t)
	if err := initCmd(nil); err != nil {
		t.Fatal(err)
	}
	path, _ := setupFile()
	before, _ := os.ReadFile(path)
	key := string(st.key)
	if err := initCmd(nil); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) || string(st.key) != key {
		t.Fatal("running init again changed the setup or the key")
	}
	if err := initCmd([]string{"--data-dir", t.TempDir()}); err == nil || !strings.Contains(err.Error(), "already set up") {
		t.Fatalf("init for another folder: %v", err)
	}
}

func TestForgetNeedsTheKey(t *testing.T) {
	st, dataDir := localEnv(t)
	if err := initCmd(nil); err != nil {
		t.Fatal(err)
	}
	key := string(st.key)
	setStdin(crypto.GenerateKey() + "\n")
	if err := forgetCmd(); err == nil || !setupExists(t) || st.key == nil {
		t.Fatalf("forget with the wrong key: %v", err)
	}
	setStdin(key + "\n")
	if err := forgetCmd(); err != nil {
		t.Fatal(err)
	}
	if setupExists(t) || st.key != nil || !fileExists(dbPath(dataDir)) {
		t.Fatal("forget must remove the setup and key and keep the data")
	}
}

func TestFailedInitBeforeTheDatabaseLeavesNothing(t *testing.T) {
	st, dataDir := localEnv(t)
	old := createDatabase
	defer func() { createDatabase = old }()
	createDatabase = func(context.Context, string, []byte, bool, string, string) error { return errors.New("disk full") }
	err := initCmd(nil)
	if err == nil || !strings.Contains(err.Error(), "Nothing was kept") {
		t.Fatalf("init with the setup save failing: %v", err)
	}
	if fileExists(dbPath(dataDir)) || dirExists(dataDir) || st.key != nil || setupExists(t) {
		t.Fatal("a failed init left a database, a folder or a key behind")
	}
	createDatabase = old
	if err := initCmd(nil); err != nil {
		t.Fatalf("init again after the failure: %v", err)
	}
}

func TestFailedInitShowsTheKeyIfTheDatabaseStays(t *testing.T) {
	st, dataDir := localEnv(t)
	oldSave, oldRemove := saveSetup, removeNewDatabase
	defer func() { saveSetup, removeNewDatabase = oldSave, oldRemove }()
	saveSetup = func(*localSetup) error { return errors.New("disk full") }
	removeNewDatabase = func(string, bool) error { return errors.New("file in use") }
	err := initCmd(nil)
	if err == nil || st.key == nil || !strings.Contains(err.Error(), string(st.key)) || !fileExists(dbPath(dataDir)) {
		t.Fatalf("a database that stays must keep its key, stored and shown: %v", err)
	}
}

func TestFailedInitKeepsAnExistingDatabase(t *testing.T) {
	st, dataDir := localEnv(t)
	original := makeDatabase(t, dataDir)
	old := saveSetup
	defer func() { saveSetup = old }()
	saveSetup = func(*localSetup) error { return errors.New("disk full") }
	setStdin(original + "\n")
	if err := initCmd([]string{"--data-dir", dataDir}); err == nil {
		t.Fatal("init should fail when the setup cannot be saved")
	}
	if !fileExists(dbPath(dataDir)) || st.key != nil || setupExists(t) {
		t.Fatal("a failed init must keep an existing database and leave no stored key or setup")
	}
}

func TestForgetChangesNothingWhenTheStoreIsUnreadable(t *testing.T) {
	st, _ := localEnv(t)
	if err := initCmd(nil); err != nil {
		t.Fatal(err)
	}
	key := string(st.key)
	st.fail = errors.New("the keyring is locked")
	setStdin(key + "\n")
	if err := forgetCmd(); err == nil || !strings.Contains(err.Error(), "Nothing was changed") {
		t.Fatalf("forget with a locked store: %v", err)
	}
	st.fail = nil
	if !setupExists(t) || string(st.key) != key {
		t.Fatal("forget removed the setup or the key while the store was locked")
	}
}

func TestInitNeverReplacesAnotherStoredKey(t *testing.T) {
	st, dataDir := localEnv(t)
	other := crypto.GenerateKey()
	st.key = []byte(other)
	err := initCmd(nil)
	if err == nil || !strings.Contains(err.Error(), "already stored") {
		t.Fatalf("init with another setup's key stored: %v", err)
	}
	if string(st.key) != other || fileExists(dbPath(dataDir)) || setupExists(t) {
		t.Fatal("init replaced a stored key or created a database")
	}
}

func TestClaimRefusesADatabaseThatAppeared(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dbPath(dir), []byte("someone else's"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := claimDatabase(dir); err == nil || !strings.Contains(err.Error(), "appeared") {
		t.Fatalf("claim over an existing database: %v", err)
	}
	if b, _ := os.ReadFile(dbPath(dir)); string(b) != "someone else's" {
		t.Fatal("the existing database was touched")
	}
}

func TestMissingDatabaseIsNotRecreatedWithAKeyFromTheEnvironment(t *testing.T) {
	st, dataDir := localEnv(t)
	if err := initCmd(nil); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dataDir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENVGRID_MASTER_KEY", string(st.key))
	if err := run(seed); err == nil || !strings.Contains(err.Error(), "is missing") {
		t.Fatalf("start with the database gone and ENVGRID_MASTER_KEY set: %v", err)
	}
	if fileExists(dbPath(dataDir)) {
		t.Fatal("a new, empty database was created")
	}
}

func TestAFolderInPlaceOfTheDatabaseIsNotTakenAsEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(dbPath(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := databaseState(dir); err == nil {
		t.Fatal("a folder named envgrid.db was taken as no database")
	}
}

func TestKeyAndForgetWorkWithoutTheSettingsFile(t *testing.T) {
	st, dataDir := localEnv(t)
	if err := initCmd(nil); err != nil {
		t.Fatal(err)
	}
	path, _ := setupFile()
	if err := os.WriteFile(path, []byte("{damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := keyCmd(); err != nil {
		t.Fatalf("envgrid key with a damaged settings file: %v", err)
	}
	setStdin(string(st.key) + "\n")
	if err := forgetCmd(); err != nil {
		t.Fatalf("envgrid forget with a damaged settings file: %v", err)
	}
	if st.key != nil || setupExists(t) || !fileExists(dbPath(dataDir)) {
		t.Fatal("forget must remove the key and the damaged file, and keep the data")
	}
}

func TestInitStoresTheKeyWithoutLineBreaks(t *testing.T) {
	st, _ := localEnv(t)
	key := crypto.GenerateKey()
	t.Setenv("ENVGRID_MASTER_KEY", key[:20]+"\n"+key[20:])
	if err := initCmd(nil); err != nil {
		t.Fatal(err)
	}
	if string(st.key) != key {
		t.Fatalf("stored key %q, want %q", st.key, key)
	}
}

func TestInitRefusesWhenTheStoreCannotBeRead(t *testing.T) {
	st, dataDir := localEnv(t)
	other := crypto.GenerateKey()
	st.key, st.getFail = []byte(other), errors.New("the keyring did not answer")
	if err := initCmd(nil); err == nil || !strings.Contains(err.Error(), "Nothing was created or changed") {
		t.Fatalf("init with an unreadable store: %v", err)
	}
	if string(st.key) != other || fileExists(dbPath(dataDir)) {
		t.Fatal("init overwrote a key it could not read, or created a database")
	}
}

// racingStore stands for another envgrid init storing its own key right after this one.
type racingStore struct {
	memStore
	other []byte
}

func (r *racingStore) Set(k []byte) error { r.key = append([]byte(nil), r.other...); return nil }

func TestReadBackMismatchKeepsTheOtherKey(t *testing.T) {
	_, dataDir := localEnv(t)
	other := []byte(crypto.GenerateKey())
	rs := &racingStore{other: other}
	openSecretStore = func() (secretStore, error) { return rs, nil }
	if err := initCmd(nil); err == nil || !strings.Contains(err.Error(), "different key") {
		t.Fatalf("init when another key appears: %v", err)
	}
	if string(rs.key) != string(other) || fileExists(dbPath(dataDir)) {
		t.Fatal("init deleted the other run's key or kept its own database")
	}
}

func TestAnInterruptedInitIsExplained(t *testing.T) {
	_, dataDir := localEnv(t)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbPath(dataDir), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := initCmd(nil); err == nil || !strings.Contains(err.Error(), "interrupted") || !strings.Contains(err.Error(), "envgrid forget") {
		t.Fatalf("init over an empty leftover database: %v", err)
	}
	t.Setenv("ENVGRID_DATA_DIR", dataDir)
	if err := run(seed); err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("start over an empty leftover database: %v", err)
	}
}

func TestClaimRefusesLeftoverJournalFiles(t *testing.T) {
	dir := t.TempDir()
	wal := dbPath(dir) + "-wal"
	if err := os.WriteFile(wal, []byte("pages"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := claimDatabase(dir); err == nil || fileExists(dbPath(dir)) {
		t.Fatalf("claim beside a leftover journal file: %v", err)
	}
	if b, _ := os.ReadFile(wal); string(b) != "pages" {
		t.Fatal("the leftover journal file was touched")
	}
}

func TestRerunInitWithTheSameFolderInOtherLetters(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("folder names ignore letter case only on Windows")
	}
	_, dataDir := localEnv(t)
	if err := initCmd(nil); err != nil {
		t.Fatal(err)
	}
	if err := initCmd([]string{"--data-dir", strings.ToUpper(dataDir)}); err != nil {
		t.Fatalf("init again with the folder in capitals: %v", err)
	}
}

func TestStoresTellNotStoredFromUnreadable(t *testing.T) {
	old := runTool
	defer func() { runTool = old }()
	locked := &toolError{name: "secret-tool", code: 1}
	for i, c := range []struct {
		store     secretStore
		err       error  // from the lookup
		found     string // what secret-tool search prints
		searchErr error
		notStored bool
	}{
		{keychainStore{}, &toolError{name: "security", code: 44, stderr: "The specified item could not be found in the keychain."}, "", nil, true},
		{keychainStore{}, &toolError{name: "security", code: 36, stderr: "User interaction is not allowed."}, "", nil, false},
		{secretToolStore{}, locked, "", nil, true},
		{secretToolStore{}, locked, "[/org/freedesktop/secrets/collection/login/7]\nlabel = envgrid master key\n", nil, false},
		{secretToolStore{}, locked, "", &toolError{name: "secret-tool", code: 1, stderr: "secret-tool: Cannot autolaunch D-Bus"}, false},
		{secretToolStore{}, &toolError{name: "secret-tool", code: 1, stderr: "secret-tool: Cannot autolaunch D-Bus"}, "[/org/freedesktop/secrets/collection/login/7]\n", nil, false},
	} {
		runTool = func(_ string, _ string, args ...string) ([]byte, error) {
			if args[0] == "search" {
				return []byte(c.found), c.searchErr
			}
			return nil, c.err
		}
		_, err := c.store.Get()
		if errors.Is(err, errKeyNotStored) != c.notStored {
			t.Fatalf("case %d, %s with %v: got %v", i, c.store.Name(), c.err, err)
		}
	}
}

func TestKeyToolsGetTheKeyOnStandardInput(t *testing.T) {
	old := runTool
	defer func() { runTool = old }()
	var gotStdin string
	var gotArgs []string
	runTool = func(stdin, name string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "search" {
			return []byte("[/org/freedesktop/secrets/collection/login/7]\nlabel = envgrid master key\n"), nil
		}
		gotStdin, gotArgs = stdin, append([]string{name}, args...)
		return []byte("stored-key\n"), nil
	}
	key := []byte(crypto.GenerateKey())
	for _, s := range []secretStore{keychainStore{}, secretToolStore{}} {
		if err := s.Set(key); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.Join(gotArgs, " "), string(key)) || !strings.Contains(gotStdin, string(key)) {
			t.Fatalf("%s: key must go on stdin, never in the arguments: %q", s.Name(), gotArgs)
		}
		if strings.Contains(gotStdin, " -U") {
			t.Fatalf("%s: storing must never replace an existing item: %q", s.Name(), strings.Replace(gotStdin, string(key), "KEY", 1))
		}
		if got, err := s.Get(); err != nil || string(got) != "stored-key" {
			t.Fatalf("%s get: %q %v", s.Name(), got, err)
		}
	}
}

func TestSecretToolNeverTouchesAnotherKey(t *testing.T) {
	old := runTool
	defer func() { runTool = old }()
	var calls []string
	runTool = func(_ string, _ string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		return []byte("stored-key\n"), nil
	}
	key := []byte(crypto.GenerateKey())
	s := secretToolStore{}
	if err := s.Set(key); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(calls[0], "key-id "+keyID(key)) {
		t.Fatalf("store must name the key it stores, so it replaces no other: %q", calls[0])
	}
	calls = nil
	runTool = func(_ string, _ string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		return nil, nil // clear succeeds, and search finds nothing left
	}
	if err := s.Delete(key); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || !strings.HasPrefix(calls[0], "clear ") || !strings.HasSuffix(calls[0], "key-id "+keyID(key)) ||
		!strings.HasPrefix(calls[1], "search ") || !strings.HasSuffix(calls[1], "key-id "+keyID(key)) {
		t.Fatalf("delete must clear only the confirmed key's item, then check it is gone: %q", calls)
	}
	// clear exits 1 silently when it removed nothing. A locked item survives
	// it and the search shows it, so delete fails; with nothing left, it is fine.
	for _, left := range []string{"[/org/freedesktop/secrets/collection/login/7]\nlabel = envgrid master key\n", ""} {
		runTool = func(_ string, _ string, args ...string) ([]byte, error) {
			if args[0] == "search" {
				return []byte(left), nil
			}
			return nil, &toolError{name: "secret-tool", code: 1}
		}
		if err := s.Delete(key); (err == nil) != (left == "") {
			t.Fatalf("delete with %q left: %v", left, err)
		}
	}
	// search must never ask to unlock: lookup has already had its prompt.
	runTool = func(_ string, _ string, args ...string) ([]byte, error) {
		if args[0] == "search" {
			if strings.Contains(strings.Join(args, " "), "--unlock") {
				t.Fatalf("search asks to unlock: %q", args)
			}
			return nil, nil
		}
		return nil, &toolError{name: "secret-tool", code: 1}
	}
	if _, err := s.Get(); !errors.Is(err, errKeyNotStored) {
		t.Fatalf("get with nothing stored: %v", err)
	}
}

func TestInitKeepsAKeyThatWasAlreadyStored(t *testing.T) {
	st, dataDir := localEnv(t)
	key := crypto.GenerateKey()
	st.key = []byte(key)
	t.Setenv("ENVGRID_MASTER_KEY", key)
	old := saveSetup
	defer func() { saveSetup = old }()
	saveSetup = func(*localSetup) error { return errors.New("disk full") }
	if err := initCmd(nil); err == nil {
		t.Fatal("init should fail when the setup cannot be saved")
	}
	if string(st.key) != key || !fileExists(dbPath(dataDir)) {
		t.Fatal("a failed init removed a key it had not stored, or the database it had created")
	}
	saveSetup = old
	if err := initCmd(nil); err != nil || string(st.key) != key {
		t.Fatalf("init with the same key already stored: %v", err)
	}
}

// unreadableAfterSet stands for a store that takes the key and then cannot hand it back.
type unreadableAfterSet struct{ memStore }

func (u *unreadableAfterSet) Get() ([]byte, error) {
	if u.key != nil {
		return nil, errors.New("the keyring locked itself")
	}
	return nil, errKeyNotStored
}

func TestAFailedReadBackSaysTheKeyMayBeStored(t *testing.T) {
	_, dataDir := localEnv(t)
	us := &unreadableAfterSet{}
	openSecretStore = func() (secretStore, error) { return us, nil }
	err := initCmd(nil)
	if err == nil || !strings.Contains(err.Error(), "envgrid key prints it") || !strings.Contains(err.Error(), "created no database") {
		t.Fatalf("init when the key cannot be read back: %v", err)
	}
	if fileExists(dbPath(dataDir)) || setupExists(t) {
		t.Fatal("init kept a database or settings after the read back failed")
	}
	original := makeDatabase(t, dataDir)
	us.key = nil
	setStdin(original + "\n")
	if err := initCmd([]string{"--data-dir", dataDir}); err == nil || !strings.Contains(err.Error(), "created no database") {
		t.Fatalf("init for an existing database when the key cannot be read back: %v", err)
	}
	if !fileExists(dbPath(dataDir)) {
		t.Fatal("init removed an existing database")
	}
}

func TestForgetTellsALockedStoreFromAnEmptyOne(t *testing.T) {
	st, _ := localEnv(t)
	if err := forgetCmd(); err == nil || !strings.Contains(err.Error(), "nothing to forget") {
		t.Fatalf("forget with nothing set up: %v", err)
	}
	st.key, st.getFail = []byte(crypto.GenerateKey()), errors.New("the keyring is locked")
	if err := forgetCmd(); err == nil || strings.Contains(err.Error(), "nothing to forget") || !strings.Contains(err.Error(), "Nothing was changed") {
		t.Fatalf("forget with a locked store and no settings file: %v", err)
	}
	st.key, st.getFail = nil, nil
	if err := initCmd(nil); err != nil {
		t.Fatal(err)
	}
	st.key = nil
	if err := forgetCmd(); err == nil || !strings.Contains(err.Error(), "nothing to confirm") || !setupExists(t) {
		t.Fatalf("forget with the key gone from the store: %v", err)
	}
}

func TestUndoRemovesOnlyThisRunsKey(t *testing.T) {
	mine, other := crypto.GenerateKey(), crypto.GenerateKey()
	st := &memStore{key: []byte(other)}
	if !deleteIfOurs(st, mine) || string(st.key) != other {
		t.Fatal("undo removed another setup's key")
	}
	st.key, st.getFail = []byte(mine), errors.New("the keyring is locked")
	if deleteIfOurs(st, mine) || st.key == nil {
		t.Fatal("undo removed a key it could not read, or said it was gone")
	}
	st.getFail = nil
	if !deleteIfOurs(st, mine) || st.key != nil {
		t.Fatal("undo kept this run's key")
	}
}

func TestOneInitOrForgetAtATime(t *testing.T) {
	localEnv(t)
	unlock, err := lockSetup()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := initCmd(nil); err == nil || !strings.Contains(err.Error(), "another envgrid init") {
		t.Fatalf("init while another run holds the lock: %v", err)
	}
	if err := forgetCmd(); err == nil || !strings.Contains(err.Error(), "another envgrid init") {
		t.Fatalf("forget while another run holds the lock: %v", err)
	}
	if setupExists(t) {
		t.Fatal("init ran while another run held the lock")
	}
}

func TestAnEmptiedDatabaseIsNotReplaced(t *testing.T) {
	_, dataDir := localEnv(t)
	if err := initCmd(nil); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(dbPath(dataDir), 0); err != nil {
		t.Fatal(err)
	}
	if err := initCmd(nil); err == nil || !strings.Contains(err.Error(), "empty (0 bytes)") {
		t.Fatalf("init again over an emptied database: %v", err)
	}
	if err := run(seed); err == nil || !strings.Contains(err.Error(), "empty (0 bytes)") {
		t.Fatalf("start over an emptied database: %v", err)
	}
	other := filepath.Join(t.TempDir(), "other")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbPath(other), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENVGRID_DATA_DIR", other)
	t.Setenv("ENVGRID_MASTER_KEY", crypto.GenerateKey())
	if err := run(seed); err == nil || !strings.Contains(err.Error(), "is empty (0 bytes)") {
		t.Fatalf("start over an empty database with ENVGRID_MASTER_KEY: %v", err)
	}
	for _, d := range []string{dataDir, other} {
		if info, err := os.Stat(dbPath(d)); err != nil || info.Size() != 0 {
			t.Fatalf("an empty database in %s was opened: %v", d, err)
		}
	}
}

func TestInitAgainChecksTheDatabaseAndTheKey(t *testing.T) {
	st, dataDir := localEnv(t)
	if err := initCmd(nil); err != nil {
		t.Fatal(err)
	}
	good := st.key
	st.key = []byte(crypto.GenerateKey())
	if err := initCmd(nil); err == nil || !strings.Contains(err.Error(), "does not open") || !strings.Contains(err.Error(), "envgrid forget") || !strings.Contains(err.Error(), "--data-dir") {
		t.Fatalf("init again with a wrong stored key: %v", err)
	}
	st.key = nil
	if err := initCmd(nil); err == nil || !strings.Contains(err.Error(), "needs one") {
		t.Fatalf("init again with the key gone from the store: %v", err)
	}
	if err := run(seed); err == nil || !strings.Contains(err.Error(), "needs one") {
		t.Fatalf("start with the key gone from the store: %v", err)
	}
	st.key = good
	if err := os.Remove(dbPath(dataDir)); err != nil {
		t.Fatal(err)
	}
	if err := initCmd(nil); err == nil || !strings.Contains(err.Error(), "is missing") || fileExists(dbPath(dataDir)) {
		t.Fatalf("init again with the database gone: %v", err)
	}
}

func TestTheEnvironmentKeyWinsForTheSetupFolder(t *testing.T) {
	_, dataDir := localEnv(t)
	if err := initCmd(nil); err != nil {
		t.Fatal(err)
	}
	other := crypto.GenerateKey()
	t.Setenv("ENVGRID_MASTER_KEY", other)
	cfg, err := loadConfig()
	want, _ := crypto.ParseKey(other)
	if err != nil || cfg.keySource != "ENVGRID_MASTER_KEY" || string(cfg.masterKey) != string(want) || !cfg.fromSetup || cfg.dataDir != dataDir {
		t.Fatalf("ENVGRID_MASTER_KEY must win over the stored key: %+v %v", cfg, err)
	}
}

func TestInitAgainNoticesAnotherDataFolder(t *testing.T) {
	_, dataDir := localEnv(t)
	if err := initCmd(nil); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "other")
	t.Setenv("ENVGRID_DATA_DIR", other)
	if err := initCmd(nil); err == nil || !strings.Contains(err.Error(), "ENVGRID_DATA_DIR points to") {
		t.Fatalf("init again with ENVGRID_DATA_DIR elsewhere: %v", err)
	}
	if err := run(seed); err == nil || !strings.Contains(err.Error(), "only used for the folder it set up") {
		t.Fatalf("start with ENVGRID_DATA_DIR elsewhere and no key: %v", err)
	}
	t.Setenv("ENVGRID_DATA_DIR", dataDir+string(filepath.Separator)+".")
	if err := initCmd(nil); err != nil {
		t.Fatalf("init again with the same folder written differently: %v", err)
	}
}

func TestTheNoKeyHelpShowsNoKey(t *testing.T) {
	_, dataDir := localEnv(t)
	t.Setenv("ENVGRID_DATA_DIR", dataDir)
	err := run(seed)
	if err == nil || keyPattern.MatchString(err.Error()) || !strings.Contains(err.Error(), "envgrid genkey") {
		t.Fatalf("help without a key: %v", err)
	}
}

// keyKeeper takes Delete calls without removing anything, like a keyring
// item that a clear command does not reach.
type keyKeeper struct{ memStore }

func (k *keyKeeper) Delete([]byte) error { return k.deleteFail }

func TestForgetKeepsBothOrNeither(t *testing.T) {
	localEnv(t)
	kk := &keyKeeper{}
	openSecretStore = func() (secretStore, error) { return kk, nil }
	if err := initCmd(nil); err != nil {
		t.Fatal(err)
	}
	key := string(kk.key)
	for _, fail := range []error{errors.New("the keyring refused"), nil} {
		kk.deleteFail = fail
		setStdin(key + "\n")
		if err := forgetCmd(); err == nil {
			t.Fatalf("forget succeeded although the key stayed (delete error %v)", fail)
		}
		if !setupExists(t) || string(kk.key) != key {
			t.Fatalf("forget removed the settings but not the key (delete error %v)", fail)
		}
	}
}

func TestRemovingANewDatabaseLeavesItWholeOnFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dbPath(dir), []byte("pages"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A journal that cannot be removed: a folder with something in it.
	if err := os.MkdirAll(filepath.Join(dbPath(dir)+"-wal", "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := removeDatabaseFiles(dir, true); err == nil || !fileExists(dbPath(dir)) {
		t.Fatalf("envgrid.db must stay while its journal does: %v", err)
	}
}

func TestUndoChecksTheKeyIsGone(t *testing.T) {
	mine := crypto.GenerateKey()
	kk := &keyKeeper{memStore: memStore{key: []byte(mine)}}
	if deleteIfOurs(kk, mine) {
		t.Fatal("undo said the key was gone although the store kept it")
	}
}

func TestSamePathFollowsLinks(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("cannot make a link here: %v", err)
	}
	if !samePath(link, dir) {
		t.Fatal("a link to the setup folder was taken for another folder")
	}
}

func TestKeyIDsTellKeysApart(t *testing.T) {
	a, b := []byte(crypto.GenerateKey()), []byte(crypto.GenerateKey())
	if keyID(a) == keyID(b) || len(keyID(a)) != 16 || keyID(a) != keyID(append(a, '\n')) {
		t.Fatalf("key ids: %q %q", keyID(a), keyID(b))
	}
}

func TestInitNeverReplacesAnotherStoredKeyForAnExistingDatabase(t *testing.T) {
	st, dataDir := localEnv(t)
	original := makeDatabase(t, dataDir)
	other := crypto.GenerateKey()
	st.key = []byte(other)
	setStdin(original + "\n")
	if err := initCmd([]string{"--data-dir", dataDir}); err == nil || !strings.Contains(err.Error(), "already stored") {
		t.Fatalf("init for an existing database with another key stored: %v", err)
	}
	if string(st.key) != other || setupExists(t) {
		t.Fatal("init replaced the stored key or saved a setup")
	}
}

func TestAStoreThatRefusesTheKeyLeavesNothing(t *testing.T) {
	st, dataDir := localEnv(t)
	st.setFail = errors.New("user interaction is not allowed")
	err := initCmd(nil)
	if err == nil || !strings.Contains(err.Error(), "envgrid genkey") || !strings.Contains(err.Error(), "ENVGRID_DATA_DIR") {
		t.Fatalf("init when the store refuses the key: %v", err)
	}
	if fileExists(dbPath(dataDir)) || dirExists(dataDir) || setupExists(t) {
		t.Fatal("init left a database, a folder or settings behind")
	}
	old := removeNewDatabase
	defer func() { removeNewDatabase = old }()
	removeNewDatabase = func(string, bool) error { return errors.New("file in use") }
	if err := initCmd(nil); err == nil || !strings.Contains(err.Error(), "could not remove the empty") {
		t.Fatalf("init must say when it cannot remove the file it claimed: %v", err)
	}
}

// sideBySide keeps several items, as the Linux keyring does with key-id: Get
// returns the first. Its Set stands for another init storing a key first.
type sideBySide struct {
	memStore
	items [][]byte
	other []byte
}

func (s *sideBySide) Get() ([]byte, error) {
	if len(s.items) == 0 {
		return nil, errKeyNotStored
	}
	return s.items[0], nil
}

func (s *sideBySide) Set(k []byte) error {
	s.items = append(s.items, s.other, append([]byte(nil), k...))
	return nil
}

func (s *sideBySide) Delete(k []byte) error {
	var kept [][]byte
	for _, it := range s.items {
		if !bytes.Equal(it, k) {
			kept = append(kept, it)
		}
	}
	s.items = kept
	return nil
}

func TestReadBackMismatchRemovesOnlyThisRunsKey(t *testing.T) {
	_, dataDir := localEnv(t)
	other := []byte(crypto.GenerateKey())
	sb := &sideBySide{other: other}
	openSecretStore = func() (secretStore, error) { return sb, nil }
	if err := initCmd(nil); err == nil || !strings.Contains(err.Error(), "removed the key it had just saved") {
		t.Fatalf("init when another key arrives beside its own: %v", err)
	}
	if len(sb.items) != 1 || !bytes.Equal(sb.items[0], other) || fileExists(dbPath(dataDir)) {
		t.Fatalf("init must leave only the other key and no database: %q", sb.items)
	}
}

func TestForgetLeavesNothingAside(t *testing.T) {
	st, _ := localEnv(t)
	if err := initCmd(nil); err != nil {
		t.Fatal(err)
	}
	path, _ := setupFile()
	setStdin(string(st.key) + "\n")
	if err := forgetCmd(); err != nil {
		t.Fatal(err)
	}
	if fileExists(path) || fileExists(path+setAsideSuffix) || st.key != nil {
		t.Fatal("forget left the settings file, its set-aside copy or the key behind")
	}
}

func TestAnInterruptedForgetIsSettledByTheKey(t *testing.T) {
	st, dataDir := localEnv(t)
	if err := initCmd(nil); err != nil {
		t.Fatal(err)
	}
	path, _ := setupFile()
	key := string(st.key)
	// Stopped after setting the file aside, with the key still stored: the
	// forget did not happen, so init puts the file back.
	if err := os.Rename(path, path+setAsideSuffix); err != nil {
		t.Fatal(err)
	}
	if err := initCmd(nil); err != nil || !fileExists(path) || fileExists(path+setAsideSuffix) {
		t.Fatalf("init must put the settings file back while the key is stored: %v", err)
	}
	// The same for forget, which then forgets for real.
	if err := os.Rename(path, path+setAsideSuffix); err != nil {
		t.Fatal(err)
	}
	setStdin(key + "\n")
	if err := forgetCmd(); err != nil || fileExists(path) || fileExists(path+setAsideSuffix) || st.key != nil {
		t.Fatalf("forget after an interrupted forget: %v", err)
	}
	// Stopped after the key was removed: the forget happened, so the
	// set-aside file goes, and nothing is taken for a setup without its key.
	st.key = []byte(key)
	if err := writeSetup(&localSetup{DataDir: dataDir, Listen: localListen, KeyStore: st.Name()}); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+setAsideSuffix); err != nil {
		t.Fatal(err)
	}
	st.key = nil
	if _, err := loadConfig(); err != nil && strings.Contains(err.Error(), "needs one") {
		t.Fatalf("a start read the set-aside file as a setup: %v", err)
	}
	setStdin("")
	if err := initCmd(nil); err == nil || !strings.Contains(err.Error(), "already exists") || fileExists(path+setAsideSuffix) {
		t.Fatalf("init after a forget that removed the key: %v", err)
	}
}

func TestADatabaseAnotherProcessStartedIsLeftAlone(t *testing.T) {
	st, dataDir := localEnv(t)
	otherRaw := crypto.GenerateKey()
	otherKey, _ := crypto.ParseKey(otherRaw)
	old := createDatabase
	defer func() { createDatabase = old }()
	// Another envgrid opens the claimed file with its own key first, and
	// creates its admin.
	createDatabase = func(ctx context.Context, dir string, key []byte, needAdmin bool, email, password string) error {
		db, err := openExisting(ctx, dir, otherKey)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.CreateUser(ctx, "other@example.com", "Other", "other-password", store.RoleAdmin); err != nil {
			t.Fatal(err)
		}
		_ = db.Close()
		return createLocal(ctx, dir, key, needAdmin, email, password)
	}
	err := initCmd(nil)
	if err == nil || !strings.Contains(err.Error(), "another envgrid process") {
		t.Fatalf("init when another process took the database: %v", err)
	}
	if info, statErr := os.Stat(dbPath(dataDir)); statErr != nil || info.Size() == 0 {
		t.Fatalf("the other process's database was removed: %v", statErr)
	}
	db, openErr := openExisting(context.Background(), dataDir, otherKey)
	if openErr != nil {
		t.Fatalf("the other process's database was changed: %v", openErr)
	}
	n, _ := db.UserCount(context.Background())
	_ = db.Close()
	if n != 1 {
		t.Fatalf("the other process's database lost its admin: %d users", n)
	}
	if st.key != nil || setupExists(t) || keyPattern.MatchString(err.Error()) {
		t.Fatal("init kept or showed a key that opens nothing, or saved a setup")
	}
}

func TestTheSetupAddressStaysWithItsFolder(t *testing.T) {
	localEnv(t)
	if err := initCmd(nil); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENVGRID_DATA_DIR", filepath.Join(t.TempDir(), "service"))
	t.Setenv("ENVGRID_MASTER_KEY", crypto.GenerateKey())
	cfg, err := loadConfig()
	if err != nil || cfg.listen != ":8080" || !cfg.secureCookies {
		t.Fatalf("a service for another folder must keep the defaults: %+v %v", cfg, err)
	}
}

func TestAStartKeepsLeftoverJournals(t *testing.T) {
	localEnv(t)
	dir := t.TempDir()
	wal := dbPath(dir) + "-wal"
	if err := os.WriteFile(wal, []byte("pages"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENVGRID_DATA_DIR", dir)
	t.Setenv("ENVGRID_MASTER_KEY", crypto.GenerateKey())
	if err := run(seed); err == nil || !strings.Contains(err.Error(), "moved away") {
		t.Fatalf("start beside a leftover journal: %v", err)
	}
	if b, _ := os.ReadFile(wal); string(b) != "pages" || fileExists(dbPath(dir)) {
		t.Fatal("the leftover journal was touched or a database was created")
	}
}

func TestSamePathResolvesLinksOfAMissingFolder(t *testing.T) {
	base := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(base, link); err != nil {
		t.Skipf("cannot make a link here: %v", err)
	}
	if !samePath(filepath.Join(link, "missing"), filepath.Join(base, "missing")) {
		t.Fatal("a missing folder reached through a link was taken for another folder")
	}
}

func TestKeyReportsAnEmptyStorePlainly(t *testing.T) {
	localEnv(t)
	if err := keyCmd(); err == nil || !strings.Contains(err.Error(), "no envgrid key is stored") || strings.Contains(err.Error(), "could not") {
		t.Fatalf("envgrid key with nothing stored: %v", err)
	}
}

func TestAnUnreadableClaimedFileIsLeftAlone(t *testing.T) {
	st, dataDir := localEnv(t)
	old := createDatabase
	defer func() { createDatabase = old }()
	// Something else wrote into the claimed file, so it cannot be opened.
	createDatabase = func(ctx context.Context, dir string, key []byte, needAdmin bool, email, password string) error {
		if err := os.WriteFile(dbPath(dir), []byte("not a database, written by someone else"), 0o600); err != nil {
			t.Fatal(err)
		}
		return createLocal(ctx, dir, key, needAdmin, email, password)
	}
	err := initCmd(nil)
	if err == nil || !strings.Contains(err.Error(), "another envgrid process may have started") {
		t.Fatalf("init when the claimed file cannot be opened: %v", err)
	}
	if b, _ := os.ReadFile(dbPath(dataDir)); string(b) != "not a database, written by someone else" || st.key == nil || setupExists(t) {
		t.Fatal("init removed a file it could not open, removed the key, or saved a setup")
	}
}

func TestKeychainDeletesOnlyTheConfirmedKey(t *testing.T) {
	old := runTool
	defer func() { runTool = old }()
	var deleted bool
	runTool = func(_ string, _ string, args ...string) ([]byte, error) {
		if args[0] == "delete-generic-password" {
			deleted = true
		}
		return []byte("other-key\n"), nil // find-generic-password
	}
	if err := (keychainStore{}).Delete([]byte("confirmed-key")); err != nil || deleted {
		t.Fatalf("keychain delete removed a key nobody confirmed: %v", err)
	}
	if err := (keychainStore{}).Delete([]byte("other-key")); err != nil || !deleted {
		t.Fatalf("keychain delete kept the confirmed key: %v", err)
	}
}

func TestAFailedSaveKeepsTheDatabaseAndItsData(t *testing.T) {
	st, dataDir := localEnv(t)
	old := saveSetup
	defer func() { saveSetup = old }()
	// Another envgrid with the same key opens the new database and writes to
	// it, then saving the setup fails.
	saveSetup = func(s *localSetup) error {
		key, _ := crypto.ParseKey(string(st.key))
		db, err := openExisting(context.Background(), s.DataDir, key)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.CreateUser(context.Background(), "other@example.com", "Other", "other-password", store.RoleMember); err != nil {
			t.Fatal(err)
		}
		_ = db.Close()
		return errors.New("disk full")
	}
	err := initCmd(nil)
	if err == nil || st.key == nil || !strings.Contains(err.Error(), string(st.key)) {
		t.Fatalf("a failed save must keep the key stored and show it: %v", err)
	}
	key, _ := crypto.ParseKey(string(st.key))
	db, openErr := openExisting(context.Background(), dataDir, key)
	if openErr != nil {
		t.Fatalf("the database was removed or changed: %v", openErr)
	}
	n, _ := db.UserCount(context.Background())
	_ = db.Close()
	if n != 2 {
		t.Fatalf("data written to the new database was lost: %d users", n)
	}
	saveSetup = old
	setStdin(string(st.key) + "\n")
	if err := initCmd(nil); err != nil || !setupExists(t) {
		t.Fatalf("init again must finish the setup: %v", err)
	}
}

// keyring stands for a Secret Service holding several envgrid items, which
// secret-tool search lists and lookup picks from in the keyring's own order.
type keyring struct{ keys []string }

func (k *keyring) run(stdin string, _ string, args ...string) ([]byte, error) {
	id := ""
	for i := range args {
		if args[i] == "key-id" && i+1 < len(args) {
			id = args[i+1]
		}
	}
	var match []string
	for _, key := range k.keys {
		if id == "" || keyID([]byte(key)) == id {
			match = append(match, key)
		}
	}
	switch args[0] {
	case "search":
		var out []byte
		for i := range match {
			out = append(out, fmt.Sprintf("[/org/freedesktop/secrets/collection/login/%d]\nlabel = envgrid master key\n", i)...)
		}
		return out, nil
	case "store":
		k.keys = append(k.keys, stdin)
	case "clear":
		var kept []string
		for _, key := range k.keys {
			if keyID([]byte(key)) != id {
				kept = append(kept, key)
			}
		}
		k.keys = kept
	case "lookup":
		if len(match) == 0 {
			return nil, &toolError{name: "secret-tool", code: 1}
		}
		return []byte(match[0] + "\n"), nil
	}
	return nil, nil
}

func TestLinuxPicksTheSetupsKeyAndNeverGuesses(t *testing.T) {
	old := runTool
	defer func() { runTool = old }()
	mine, other := crypto.GenerateKey(), crypto.GenerateKey()
	for _, order := range [][]string{{mine, other}, {other, mine}} {
		k := &keyring{keys: order}
		runTool = k.run
		got, err := withKeyID(secretToolStore{}, keyID([]byte(mine))).Get()
		if err != nil || string(got) != mine {
			t.Fatalf("order %d: the setup's key-id must pick its own key: %q %v", len(order), got, err)
		}
		if _, err := (secretToolStore{}).Get(); err == nil || errors.Is(err, errKeyNotStored) || !strings.Contains(err.Error(), "will not guess") {
			t.Fatalf("without a key-id, two keys must be refused: %v", err)
		}
	}
	runTool = (&keyring{keys: []string{mine}}).run
	if got, err := (secretToolStore{}).Get(); err != nil || string(got) != mine {
		t.Fatalf("one key without a key-id: %q %v", got, err)
	}
}

func TestInitSavesTheKeyID(t *testing.T) {
	st, _ := localEnv(t)
	if err := initCmd(nil); err != nil {
		t.Fatal(err)
	}
	s, err := readSetup()
	if err != nil || s == nil || s.KeyID != keyID(st.key) {
		t.Fatalf("the setup must name its key: %+v %v", s, err)
	}
}

func TestAStartUsesTheSetupsKeyAmongSeveral(t *testing.T) {
	localEnv(t)
	old := runTool
	defer func() { runTool = old }()
	k := &keyring{}
	runTool = k.run
	openSecretStore = func() (secretStore, error) { return secretToolStore{}, nil }
	if err := initCmd(nil); err != nil {
		t.Fatal(err)
	}
	mine := k.keys[0]
	k.keys = []string{crypto.GenerateKey(), mine} // another key, listed first
	cfg, err := loadConfig()
	want, _ := crypto.ParseKey(mine)
	if err != nil || string(cfg.masterKey) != string(want) {
		t.Fatalf("a start must use the setup's own key: %v", err)
	}
	if err := initCmd(nil); err != nil {
		t.Fatalf("init again with another key listed first: %v", err)
	}
	setStdin(mine + "\n")
	if err := forgetCmd(); err != nil || len(k.keys) != 1 || k.keys[0] == mine {
		t.Fatalf("forget must remove the setup's key and only that one: %v %d", err, len(k.keys))
	}
}
