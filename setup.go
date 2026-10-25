package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/term"

	"github.com/Achal13jain/envgrid/internal/crypto"
	"github.com/Achal13jain/envgrid/internal/server"
	"github.com/Achal13jain/envgrid/internal/store"
)

// localSetup is what `envgrid init` remembers so that later starts need no
// environment variables. It holds no secret: the master key lives in the
// operating system's secure store (see secretStore).
type localSetup struct {
	DataDir       string `json:"dataDir"`
	Listen        string `json:"listen"`
	SecureCookies bool   `json:"secureCookies"`
	KeyStore      string `json:"keyStore"`
	// KeyID names the stored key (see keyID) without revealing it, so a store
	// that holds several envgrid keys always returns this setup's.
	KeyID string `json:"keyId,omitempty"`
}

// setupDir is the per-user settings folder: ~/Library/Application
// Support/envgrid on macOS, ~/.config/envgrid on Linux, and on Windows
// %LOCALAPPDATA%\envgrid, which stays on this computer: a roaming profile
// would carry the settings and key to computers that do not have the
// database. It is replaced in tests.
var setupDir = func() (string, error) {
	d, err := os.UserConfigDir()
	if runtime.GOOS == "windows" {
		d, err = os.UserCacheDir() // %LOCALAPPDATA%
	}
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "envgrid"), nil
}

// localDataDir is where `envgrid init` puts the database unless told
// otherwise: the Windows default from defaultDataDir, and on macOS and Linux
// the ~/.envgrid folder the setup guide has always used. Replaced in tests.
var localDataDir = func() (string, error) {
	if runtime.GOOS == "windows" {
		return defaultDataDir(), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".envgrid"), nil
}

// localListen keeps a local install reachable from this computer only, which
// is what makes plain-HTTP session cookies acceptable for it.
const localListen = "127.0.0.1:8080"

func setupFile() (string, error) {
	dir, err := setupDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "setup.json"), nil
}

// readSetup returns nil when envgrid init has not been run for this user.
func readSetup() (*localSetup, error) {
	path, err := setupFile()
	if err != nil {
		return nil, nil
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: the path is the user's own settings folder
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var s localSetup
	if err := json.Unmarshal(data, &s); err != nil || s.DataDir == "" {
		return nil, fmt.Errorf(`%s is damaged. It records which folder holds your database, so fix it rather than deleting it.
envgrid key still prints the stored key, and envgrid forget removes the key and this file once you have a copy`, path)
	}
	return &s, nil
}

// setAsideSuffix names the settings file while envgrid forget runs.
const setAsideSuffix = ".forgotten"

// settleSetAside finishes or undoes an envgrid forget that stopped after it
// set the settings file aside. The key decides: while it is stored the
// forget did not happen, so the file goes back; once it is gone, so does the
// file. It runs under the setup lock.
func settleSetAside(st secretStore) error {
	path, err := setupFile()
	if err != nil {
		return err
	}
	aside := path + setAsideSuffix
	if _, err := os.Lstat(aside); err != nil {
		return nil // nothing set aside
	}
	if _, err := os.Lstat(path); err == nil {
		return nil // the settings file is in place; the copy is not read
	}
	_, getErr := st.Get()
	switch {
	case getErr == nil:
		if err := os.Rename(aside, path); err != nil {
			return fmt.Errorf("an earlier envgrid forget stopped part way and left %s, and envgrid could not put it back (%v). Rename it to %s yourself, then try again. Nothing was changed", aside, err, path)
		}
	case errors.Is(getErr, errKeyNotStored):
		if err := os.Remove(aside); err != nil {
			return fmt.Errorf("an earlier envgrid forget removed the key but not %s (%v). Delete that file, then try again. Nothing was changed", aside, err)
		}
	default:
		return errStore(st, "check what is stored in", getErr)
	}
	return nil
}

func writeSetup(s *localSetup) error {
	path, err := setupFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func isLoopback(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func dbPath(dataDir string) string { return filepath.Join(dataDir, "envgrid.db") }

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// databaseState reports whether dataDir holds envgrid.db. Only "does not
// exist" counts as absent: any other error, such as a folder envgrid may not
// read, is returned, so it is never taken for an empty place to start fresh.
func databaseState(dataDir string) (bool, error) {
	info, err := os.Stat(dbPath(dataDir))
	switch {
	case err == nil && info.IsDir():
		return false, fmt.Errorf("%s is a folder, not a database", dbPath(dataDir))
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	}
	return false, fmt.Errorf("cannot check for a database in %s: %w", dataDir, err)
}

// claimDatabase creates an empty envgrid.db that did not exist, exclusively.
// If one appears in the meantime, from another envgrid init or envgrid
// itself, the claim fails, so this run never treats that database as its own.
func claimDatabase(dataDir string) error {
	if err := journalLeftovers(dataDir); err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	if err := os.Chmod(dataDir, 0o700); err != nil { //nolint:gosec // G302: a directory needs the execute bit
		return fmt.Errorf("restrict data dir: %w", err)
	}
	f, err := os.OpenFile(dbPath(dataDir), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("a database appeared in %s while envgrid init was running. envgrid left it alone; run envgrid init again", dataDir)
	}
	if err != nil {
		return err
	}
	return f.Close()
}

// journalLeftovers refuses a folder with SQLite journal files but no
// envgrid.db: a database was probably moved away, perhaps for a restore, and
// opening a new one there would delete them.
func journalLeftovers(dataDir string) error {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(dbPath(dataDir) + suffix); err == nil {
			return fmt.Errorf("found %s but no envgrid.db in %s: a database may have been moved away, so envgrid will not start a new one there, which would delete that file. Nothing was changed. To start a new database there, move the file away first", filepath.Base(dbPath(dataDir))+suffix, dataDir)
		}
	}
	return nil
}

// emptyDatabase reports a zero-length envgrid.db: one claimed by an envgrid
// init that was interrupted before it stored a key. It cannot hold any data.
func emptyDatabase(dataDir string) bool {
	info, err := os.Stat(dbPath(dataDir))
	return err == nil && info.Size() == 0
}

// leftoverDatabase explains an empty envgrid.db to envgrid init, which only
// gets here when no setup exists, so envgrid forget cannot undo a working one.
func leftoverDatabase(dataDir string) error {
	return fmt.Errorf("%s is empty (0 bytes) and holds no data: an envgrid init was interrupted before it finished, or the file was emptied. If you have a backup of envgrid.db, restore it. Otherwise delete the empty file, and if envgrid key then prints a key, save it and run envgrid forget. Then run envgrid init again", dbPath(dataDir))
}

// truncatedDatabase explains an empty envgrid.db found when starting. envgrid
// never opens one, because SQLite would quietly make it a new, empty database.
func truncatedDatabase(dataDir string) error {
	return fmt.Errorf(`%s is empty (0 bytes), so envgrid will not start a new database in its place.

Either an envgrid init was interrupted before it finished, or the file was
truncated, for example by a failed copy or restore. Restore envgrid.db from a
backup if you have one; otherwise delete the empty file to start a new database`, dbPath(dataDir))
}

// wrongKey explains a key that does not open the database, naming where the
// key came from instead of always blaming ENVGRID_MASTER_KEY.
func wrongKey(err error, source, dataDir string) error {
	if !errors.Is(err, store.ErrWrongMasterKey) {
		return err
	}
	next := "Use the key you saved when that database was created"
	if strings.HasPrefix(source, "the key stored in") {
		next = fmt.Sprintf(`Put back the database this setup was made with, or start envgrid with ENVGRID_MASTER_KEY set to this database's key. To store that key instead, save what envgrid key prints, run envgrid forget, then run envgrid init --data-dir "%s" and paste the right key`, dataDir)
	}
	return fmt.Errorf("%s does not open the database in %s, which was created with a different key. %s", source, dataDir, next)
}

// keyMissing explains a setup whose key is no longer in the store, for
// example because it was removed there by hand.
func keyMissing(st secretStore, dataDir string) error {
	path, _ := setupFile()
	return fmt.Errorf(`no envgrid key is stored in %s, but the setup in %s needs one.

To start with your saved copy of the key, set ENVGRID_MASTER_KEY to it. To store
it again, delete %s and run envgrid init --data-dir "%s", which asks for the key`, st.Name(), path, path, dataDir)
}

// lockSetup lets only one envgrid init or envgrid forget run at a time for
// this user, so two runs cannot replace each other's key and settings. The
// operating system drops the lock when the process ends, however it ends.
func lockSetup() (unlock func(), err error) {
	dir, err := setupDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "setup.lock"), os.O_RDWR|os.O_CREATE, 0o600) //nolint:gosec // G304: the path is the user's own settings folder
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		if errors.Is(err, errLockBusy) {
			return nil, errors.New("another envgrid init or envgrid forget is running for this user. Let it finish, then try again. Nothing was changed")
		}
		return nil, fmt.Errorf("could not lock %s: %w. Nothing was changed", f.Name(), err)
	}
	return func() { _ = f.Close() }, nil
}

// errStore explains that the secure store could not be used and that nothing
// was created or changed.
func errStore(st secretStore, action string, err error) error {
	name := "the secure key store"
	if st != nil {
		name = st.Name()
	}
	return fmt.Errorf(`envgrid could not %s %s: %v

Nothing was created or changed. %s
To start envgrid without the store, set ENVGRID_MASTER_KEY to your saved copy of the key, in this terminal only`, action, name, err, storeHint())
}

// openExisting opens a database that already exists, which also checks the key.
func openExisting(ctx context.Context, dataDir string, key []byte) (*store.Store, error) {
	cipher, err := crypto.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return store.Open(ctx, dbPath(dataDir), cipher)
}

// Terminal input, replaced in tests. One shared reader, so piped lines are
// not lost between questions.
var (
	stdin      = bufio.NewReader(os.Stdin)
	isTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
	readSecret = func() ([]byte, error) { return term.ReadPassword(int(os.Stdin.Fd())) }
)

// askSecret reads a line without echo from a terminal, or a plain line from a pipe.
func askSecret(prompt string) (string, error) {
	if isTerminal() {
		fmt.Fprint(os.Stderr, prompt)
		b, err := readSecret()
		fmt.Fprintln(os.Stderr)
		return strings.TrimSpace(string(b)), err
	}
	line, err := stdin.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// askAdmin takes the first admin from ENVGRID_ADMIN_EMAIL and
// ENVGRID_ADMIN_PASSWORD when both are set, and otherwise asks in the
// terminal, with the password hidden, so it never lands in shell history.
func askAdmin() (email, password string, err error) {
	email, password = env("ENVGRID_ADMIN_EMAIL", ""), os.Getenv("ENVGRID_ADMIN_PASSWORD")
	if email == "" || password == "" {
		if !isTerminal() {
			return "", "", errors.New("run envgrid init in a terminal so it can ask for the first admin, or set ENVGRID_ADMIN_EMAIL and ENVGRID_ADMIN_PASSWORD")
		}
		fmt.Fprint(os.Stderr, "Email for the first admin: ")
		line, err := stdin.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", "", err
		}
		email = strings.TrimSpace(line)
		if password, err = askSecret(fmt.Sprintf("Password for %s (at least %d characters, not shown): ", email, server.MinPasswordLength)); err != nil {
			return "", "", err
		}
		again, err := askSecret("Type the password again: ")
		if err != nil {
			return "", "", err
		}
		if again != password {
			return "", "", errors.New("the two passwords differ; nothing was created, run envgrid init again")
		}
	}
	if !strings.Contains(email, "@") {
		return "", "", errors.New("the admin email must be an email address")
	}
	if len([]rune(password)) < server.MinPasswordLength {
		return "", "", fmt.Errorf("the admin password must be at least %d characters", server.MinPasswordLength)
	}
	return email, password, nil
}

// initCmd is `envgrid init`: a one-time local setup. It never makes a new key
// for a database that already exists, checks every key against the database
// before keeping it, and changes nothing when run again.
func initCmd(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	dataDirFlag := fs.String("data-dir", "", "folder for envgrid.db (default: "+describeLocalDataDir()+")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	unlock, err := lockSetup()
	if err != nil {
		return err
	}
	defer unlock()
	st, err := openSecretStore()
	if err != nil {
		return errStore(nil, "use", err)
	}
	if err := settleSetAside(st); err != nil {
		return err
	}
	existing, err := readSetup()
	if err != nil {
		return err
	}
	if existing != nil {
		return checkSetup(ctx, existing, st, *dataDirFlag)
	}

	dataDir := *dataDirFlag
	if dataDir == "" {
		dataDir = env("ENVGRID_DATA_DIR", "")
	}
	if dataDir == "" {
		if dataDir, err = localDataDir(); err != nil {
			return err
		}
	}
	if dataDir, err = filepath.Abs(dataDir); err != nil {
		return err
	}
	dbExists, err := databaseState(dataDir)
	if err != nil {
		return err
	}
	if dbExists && emptyDatabase(dataDir) {
		return leftoverDatabase(dataDir)
	}

	// The key: from ENVGRID_MASTER_KEY, from the person for an existing
	// database, or new. A new key is only ever made when there is no database.
	var key []byte
	generated := false
	source := "ENVGRID_MASTER_KEY"
	raw := env("ENVGRID_MASTER_KEY", "")
	switch {
	case raw != "":
	case dbExists:
		source = "the key you pasted"
		if raw, err = askSecret(fmt.Sprintf("A database already exists in %s.\nPaste the master key you saved for it (not shown): ", dataDir)); err != nil {
			return err
		}
		if raw == "" {
			return fmt.Errorf("a database already exists in %s, and envgrid will not make a new key for it. Run envgrid init again and paste the key you saved (envgrid key prints a stored one), or set ENVGRID_MASTER_KEY", dataDir)
		}
	default:
		raw, generated = crypto.GenerateKey(), true
	}
	if key, err = crypto.ParseKey(raw); err != nil {
		return fmt.Errorf("master key: %w", err)
	}
	// Keep the key in one canonical form: base64 decoding ignores line breaks,
	// and none must reach the key stores or the screen.
	raw = base64.StdEncoding.EncodeToString(key)

	// Never replace a key stored for another setup: it may be the only copy of
	// another database's key, for example after the settings file was lost.
	// Only a definite "nothing stored" lets init go on: a store that cannot be
	// read may hold exactly that key. This comes before the database is opened,
	// so a refusal here leaves everything as it was.
	prev, err := st.Get()
	alreadyStored := err == nil && bytes.Equal(bytes.TrimSpace(prev), []byte(raw))
	switch {
	case err == nil && !alreadyStored:
		return fmt.Errorf(`a different master key is already stored in %s, probably for another envgrid database.
envgrid will not replace it. Nothing was changed. envgrid key prints it; save a copy, then run envgrid forget if that setup is no longer needed`, st.Name())
	case err != nil && !errors.Is(err, errKeyNotStored):
		return errStore(st, "check what is stored in", err)
	}
	st = withKeyID(st, keyID([]byte(raw)))

	needAdmin := true
	if dbExists {
		db, err := openExisting(ctx, dataDir, key)
		if err != nil {
			return fmt.Errorf("%w. envgrid stored no key and saved no setup", wrongKey(err, source, dataDir))
		}
		n, err := db.UserCount(ctx)
		_ = db.Close()
		if err != nil {
			return err
		}
		needAdmin = n == 0
	}
	var email, password string
	if needAdmin {
		if email, password, err = askAdmin(); err != nil {
			return err
		}
	}

	// Claim a new database file before storing the key, then store the key and
	// read it back before creating anything else, so an unavailable or locked
	// store leaves nothing behind.
	dirExisted := dirExists(dataDir)
	if !dbExists {
		if err := claimDatabase(dataDir); err != nil {
			return err
		}
	}
	// unclaim removes the empty file this run claimed. Every message below says
	// no database was created, so it returns a note when the removal fails.
	unclaim := func() string {
		if dbExists {
			return ""
		}
		if err := removeNewDatabase(dataDir, dirExisted); err != nil {
			return fmt.Sprintf("\n\nenvgrid could not remove the empty %s it had claimed (%v). It holds no data; delete it before running envgrid init again.", dbPath(dataDir), err)
		}
		return ""
	}
	// A key that is already stored is left as it is: it is not stored again,
	// and nothing below removes it.
	if !alreadyStored {
		if err := st.Set([]byte(raw)); err != nil {
			fallback := fmt.Sprintf(`start envgrid with ENVGRID_MASTER_KEY set to the key you gave and ENVGRID_DATA_DIR set to "%s"`, dataDir)
			if generated {
				fallback = fmt.Sprintf(`make a key with envgrid genkey, save it in your password manager, and start envgrid with ENVGRID_MASTER_KEY set to it and ENVGRID_DATA_DIR set to "%s", as "Starting with environment variables" in docs/configuration.md shows`, dataDir)
			}
			return fmt.Errorf(`envgrid could not save the key in %s: %v

No key was stored and no setup was saved. %s
To run envgrid without the store, %s%s`, st.Name(), err, storeHint(), fallback, unclaim())
		}
		got, err := st.Get()
		switch {
		case err != nil:
			return fmt.Errorf(`envgrid saved the key in %s but could not read it back: %v

envgrid created no database and saved no setup. The key may still be stored:
envgrid key prints it, and envgrid forget removes it. Then run envgrid init again%s`, st.Name(), err, unclaim())
		case !bytes.Equal(got, []byte(raw)):
			// Another key arrived meanwhile, perhaps beside this one: remove this
			// run's key only, and leave the other alone.
			removed := "removed the key it had just saved, if it was still there"
			if err := st.Delete([]byte(raw)); err != nil {
				removed = fmt.Sprintf("could not remove the key it had just saved (%v)", err)
			}
			return fmt.Errorf(`%s now holds a different key than the one envgrid just saved, perhaps
from another program. envgrid left that key alone, %s, created no database
and saved no setup%s`, st.Name(), removed, unclaim())
		}
	}

	// From here on this run may fill a database. If a later step fails,
	// undoInit makes sure no database is left that no stored or shown key opens,
	// and only ever removes the database this run claimed and a key this run stored.
	if err := createDatabase(ctx, dataDir, key, needAdmin, email, password); err != nil {
		return undoInit(st, dataDir, dbExists, dirExisted, raw, !alreadyStored, err)
	}
	s := &localSetup{DataDir: dataDir, Listen: localListen, SecureCookies: false, KeyStore: st.Name(), KeyID: keyID([]byte(raw))}
	if err := saveSetup(s); err != nil {
		return undoInit(st, dataDir, dbExists, dirExisted, raw, !alreadyStored, fmt.Errorf("save the setup: %w", err))
	}
	path, _ := setupFile()
	fmt.Printf("envgrid is set up.\n\n  Data:      %s\n  Key:       stored in %s\n  Settings:  %s\n  Address:   http://%s (this computer only)\n\n", dbPath(dataDir), st.Name(), path, localListen)
	if generated {
		fmt.Printf("Your master key, shown only now:\n\n    %s\n\n", raw)
	}
	fmt.Print(`Save a copy of the master key in your password manager. Without it the data
cannot be decrypted, for example if this computer or your user account is lost.
envgrid key prints it again while you are signed in.

Start envgrid with:  envgrid serve
`)
	return nil
}

// saveSetup, removeNewDatabase and createDatabase are replaced in tests to
// make a step fail.
var (
	saveSetup         = writeSetup
	removeNewDatabase = removeDatabaseFiles
	createDatabase    = createLocal
)

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// undoInit runs when init fails after the key was stored. A database that
// existed before is left alone: the person already has its key. A database
// this run claimed is removed, then the key if this run stored it, so init can
// simply be run again. If that database cannot be removed, the key stays
// stored and is shown, because only it can open the database.
func undoInit(st secretStore, dataDir string, dbExisted, dirExisted bool, raw string, storedByRun bool, cause error) error {
	if dbExisted {
		if storedByRun && !deleteIfOurs(st, raw) {
			return fmt.Errorf("%w. The setup was not saved, and the key may still be stored in %s: envgrid forget removes it", cause, st.Name())
		}
		return fmt.Errorf("%w. The setup was not saved; the database still opens with the key you gave", cause)
	}
	// Only the claimed file, while still empty, is ever removed. Once a
	// database is in it, it stays: another envgrid may already be using it,
	// for example one started with the same ENVGRID_MASTER_KEY, and nothing
	// can tell for sure.
	var oe openError
	if !emptyDatabase(dataDir) && !errors.As(cause, &oe) {
		return fmt.Errorf(`%w

envgrid init had already created the database in %s, so it kept the
database and saved no setup. The key stays in %s; save a copy now:

    %s

Then run envgrid init again and paste this key: it finds the database and
finishes the setup`, cause, dataDir, st.Name(), raw)
	}
	if !emptyDatabase(dataDir) {
		const other = "%w\n\nThe database file envgrid init had claimed in %s is no longer empty, so another envgrid process may have started a database there. envgrid init left that file alone and saved no setup"
		if errors.Is(cause, store.ErrWrongMasterKey) {
			// It holds another key's check: this run's key opens nothing.
			switch {
			case !storedByRun:
				return fmt.Errorf(other+". The key that was already stored was left as it was. Stop the other process, then run envgrid init again", cause, dataDir)
			case !deleteIfOurs(st, raw):
				return fmt.Errorf(other+", but the key it had stored may still be in %s: envgrid forget removes it. Stop the other process, then run envgrid init again", cause, dataDir, st.Name())
			}
			return fmt.Errorf(other+", and removed the key it had stored, which opens nothing. Stop the other process, then run envgrid init again", cause, dataDir)
		}
		// Whose it is cannot be told, so the stored key stays too.
		return fmt.Errorf(other+". The key stays in %s, in case that database needs it: envgrid key prints it. Stop any other envgrid using this folder, then run envgrid init again", cause, dataDir, st.Name())
	}
	// The file is still empty, so no key belongs to it yet.
	note := ""
	if err := removeNewDatabase(dataDir, dirExisted); err != nil {
		note = fmt.Sprintf(" envgrid could not remove the empty %s it had claimed (%v); it holds no data, so delete it before running envgrid init again.", dbPath(dataDir), err)
	}
	if storedByRun && !deleteIfOurs(st, raw) {
		return fmt.Errorf("%w. No database was created, but the key may still be stored in %s: envgrid key prints it and envgrid forget removes it. Then run envgrid init again.%s", cause, st.Name(), note)
	}
	return fmt.Errorf("%w. Nothing was kept: no database was created, and any key this run stored was removed, so you can run envgrid init again.%s", cause, note)
}

// deleteIfOurs removes the key this run stored, and never another: Delete
// removes only the key it is given. It reports whether that key is gone.
func deleteIfOurs(st secretStore, raw string) bool {
	return st.Delete([]byte(raw)) == nil && !stillStored(st, raw)
}

// stillStored reports whether the store still holds the key raw. A store that
// cannot be read counts as still holding it.
func stillStored(st secretStore, raw string) bool {
	got, err := st.Get()
	if errors.Is(err, errKeyNotStored) {
		return false
	}
	return err != nil || bytes.Equal(bytes.TrimSpace(got), []byte(raw))
}

// removeDatabaseFiles removes the database files a failed init created, and
// the data folder if init created it and it is now empty. envgrid.db goes
// last, so if anything cannot be removed the database is still there for the
// key that envgrid then shows.
func removeDatabaseFiles(dataDir string, dirExisted bool) error {
	for _, suffix := range []string{"-wal", "-shm", "-journal", ""} {
		if err := os.Remove(dbPath(dataDir) + suffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if !dirExisted {
		_ = os.Remove(dataDir) // fails, harmlessly, if the folder is not empty
	}
	return nil
}

// openError is a database that could not be opened. After it, envgrid init
// cannot tell whose the database file is.
type openError struct{ err error }

func (e openError) Error() string { return e.err.Error() }
func (e openError) Unwrap() error { return e.err }

// createLocal creates the data folder and database (or opens the existing
// one) and adds the first admin when there is none.
func createLocal(ctx context.Context, dataDir string, key []byte, needAdmin bool, email, password string) error {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	if err := os.Chmod(dataDir, 0o700); err != nil { //nolint:gosec // G302: a directory needs the execute bit
		return fmt.Errorf("restrict data dir: %w", err)
	}
	db, err := openExisting(ctx, dataDir, key)
	if err != nil {
		return openError{err}
	}
	defer func() { _ = db.Close() }()
	if needAdmin {
		u, err := db.CreateUser(ctx, email, "Admin", password, store.RoleAdmin)
		if err != nil {
			return fmt.Errorf("create the first admin: %w", err)
		}
		return db.Audit(ctx, store.AuditEntry{UserID: u.ID, Action: "user_created", Detail: map[string]any{"email": u.Email, "role": u.Role, "via": "init"}})
	}
	return nil
}

// checkSetup is envgrid init run again: it reports and changes nothing.
func checkSetup(ctx context.Context, s *localSetup, st secretStore, dataDirFlag string) error {
	st = withKeyID(st, s.KeyID)
	path, _ := setupFile()
	if dataDirFlag != "" && !samePath(dataDirFlag, s.DataDir) {
		return fmt.Errorf("envgrid is already set up with data in %s (see %s). To use another folder, run envgrid forget first", s.DataDir, path)
	}
	if d := env("ENVGRID_DATA_DIR", ""); dataDirFlag == "" && d != "" && !samePath(d, s.DataDir) {
		return fmt.Errorf("envgrid is already set up with data in %s (see %s), but ENVGRID_DATA_DIR points to %s. envgrid serve would use that folder, which needs ENVGRID_MASTER_KEY. Unset ENVGRID_DATA_DIR to use the setup, or run envgrid forget first to set up the other folder", s.DataDir, path, d)
	}
	stored, err := st.Get()
	if errors.Is(err, errKeyNotStored) {
		return keyMissing(st, s.DataDir)
	}
	if err != nil {
		return errStore(st, "read the key from", err)
	}
	key, err := crypto.ParseKey(string(stored))
	if err != nil {
		return errStore(st, "use the key in", err)
	}
	exists, err := databaseState(s.DataDir)
	if err != nil {
		return err
	}
	if !exists || emptyDatabase(s.DataDir) {
		return missingDatabase(s.DataDir)
	}
	db, err := openExisting(ctx, s.DataDir, key)
	if err != nil {
		return wrongKey(err, "the key stored in "+st.Name(), s.DataDir)
	}
	_ = db.Close()
	fmt.Printf("envgrid is already set up, and the stored key opens the database.\n\n  Data:      %s\n  Key:       stored in %s\n  Settings:  %s\n\nThe setup was left as it is. Start envgrid with:  envgrid serve\n", dbPath(s.DataDir), st.Name(), path)
	return nil
}

// missingDatabase explains that the database envgrid init set up is gone or
// was emptied. An empty envgrid.db is never opened: SQLite would quietly make
// it a new, empty database.
func missingDatabase(dataDir string) error {
	state := "missing"
	if emptyDatabase(dataDir) {
		state = "empty (0 bytes)"
	}
	return fmt.Errorf(`the envgrid database set up with envgrid init is %s: %s

envgrid will not create a new, empty one in its place. If the folder is on a
drive that is not connected, connect it. To restore, put envgrid.db back from a
backup. To start over, run envgrid forget and then envgrid init`, state, dbPath(dataDir))
}

// keyCmd is `envgrid key`: prints the stored key, to make a recovery copy. It
// works without the settings file, so a key is never stuck in the store after
// an interrupted init or a damaged file.
func keyCmd() error {
	st, err := openSecretStore()
	if err != nil {
		return errStore(nil, "use", err)
	}
	if s, _ := readSetup(); s != nil {
		st = withKeyID(st, s.KeyID)
	}
	key, err := st.Get()
	if errors.Is(err, errKeyNotStored) {
		return fmt.Errorf("no envgrid key is stored in %s", st.Name())
	}
	if err != nil {
		return errStore(st, "read the key from", err)
	}
	fmt.Fprintln(os.Stderr, "Keep this key private. Anyone with it and a copy of the database can read every value.")
	fmt.Println(string(key))
	return nil
}

// forgetCmd is `envgrid forget`: removes the stored key and the saved setup,
// never the data. It removes both or neither: it needs a readable store, and
// asks for the key first, so nobody forgets the only copy. The settings file
// is moved aside before the key is removed, and put back if that fails.
func forgetCmd() error {
	unlock, err := lockSetup()
	if err != nil {
		return err
	}
	defer unlock()
	st, err := openSecretStore()
	if err != nil {
		return errStore(nil, "use", err)
	}
	if err := settleSetAside(st); err != nil {
		return err
	}
	// A damaged settings file can still be forgotten, as can a key left
	// stored by an interrupted init.
	s, setupErr := readSetup()
	path, err := setupFile()
	if err != nil {
		return err
	}
	if s != nil {
		st = withKeyID(st, s.KeyID)
	}
	stored, getErr := st.Get()
	if s == nil && setupErr == nil && errors.Is(getErr, errKeyNotStored) {
		return errors.New("envgrid init has not been run for this user; there is nothing to forget")
	}
	if errors.Is(getErr, errKeyNotStored) {
		return fmt.Errorf("no envgrid key is stored in %s, so there is nothing to confirm. Nothing was changed. If you removed the key yourself, delete %s to finish; your data is not touched", st.Name(), path)
	}
	if getErr != nil {
		return fmt.Errorf(`envgrid could not read its key from %s (%v), so it cannot confirm and remove it.

Nothing was changed. %s
If you removed the key from the store yourself, delete %s to finish; your data is not touched`, st.Name(), getErr, storeHint(), path)
	}
	raw, err := askSecret("To make sure you have a copy, paste the master key (not shown): ")
	if err != nil {
		return err
	}
	if !bytes.Equal(bytes.TrimSpace(stored), []byte(raw)) {
		return errors.New("that is not the key for this setup; nothing was changed")
	}
	// readSetup never reads the file set aside, so once it is moved the setup
	// is gone; moving it back undoes that.
	aside := path + setAsideSuffix
	_, statErr := os.Lstat(path)
	hasFile := statErr == nil
	if hasFile {
		if err := os.Rename(path, aside); err != nil {
			return fmt.Errorf("envgrid could not move %s aside (%v), so it removed nothing. Close any program that has the file open and try again", path, err)
		}
	}
	putBack := func() string {
		if !hasFile {
			return ""
		}
		if err := os.Rename(aside, path); err != nil {
			return fmt.Sprintf("\n\nenvgrid also could not put the settings file back (%v). Rename %s back to %s yourself.", err, aside, path)
		}
		return ""
	}
	if err := st.Delete([]byte(raw)); err != nil {
		return fmt.Errorf("envgrid could not remove the key from %s, or could not confirm that it is gone (%v). The settings file was kept, so nothing was forgotten. %s Then try envgrid forget again%s", st.Name(), err, storeHint(), putBack())
	}
	if stillStored(st, raw) {
		return fmt.Errorf("envgrid asked %s to remove the key, but cannot confirm that it is gone. The settings file was kept, so nothing was forgotten; try envgrid forget again%s", st.Name(), putBack())
	}
	if hasFile {
		if err := os.Remove(aside); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("envgrid removed the stored key but could not delete %s (%v). envgrid still reads that file as the setup, so delete it to finish; your data is not touched", aside, err)
		}
	}
	if s == nil {
		fmt.Println("envgrid forgot its stored key and settings file. No data was touched.")
		return nil
	}
	fmt.Printf("envgrid forgot its local setup and stored key. Your data is untouched in %s.\nTo use it again, run envgrid init --data-dir \"%s\" and paste your key.\n", s.DataDir, s.DataDir)
	return nil
}

func describeLocalDataDir() string {
	if runtime.GOOS == "windows" {
		return `%LOCALAPPDATA%\envgrid\data`
	}
	return "~/.envgrid"
}
