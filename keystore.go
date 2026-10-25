package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// secretStore keeps the master key in the operating system's per-user secure
// storage, so a local install can start without ENVGRID_MASTER_KEY. The key is
// never passed on a command line, where other users could see it in the
// process list: the macOS and Linux tools receive it on standard input.
type secretStore interface {
	Name() string // shown to people, for example "your default macOS keychain"
	Get() ([]byte, error)
	Set(key []byte) error
	// Delete removes key, and never a different key: callers pass the key the
	// person confirmed, so a key stored meanwhile by someone else stays.
	Delete(key []byte) error
}

// errKeyNotStored means the store answered and holds no envgrid key. Any
// other error from Get means the store could not be read, and then nothing
// may be assumed about what it holds.
var errKeyNotStored = errors.New("no envgrid key is stored")

// openSecretStore is replaced in tests.
var openSecretStore = platformSecretStore

// errLockBusy means another process holds the setup lock.
var errLockBusy = errors.New("the setup lock is held by another process")

// deleteIfStored removes key from a store that holds one item, after checking
// that the item still holds key.
func deleteIfStored(st secretStore, key []byte, remove func() error) error {
	got, err := st.Get()
	switch {
	case errors.Is(err, errKeyNotStored):
		return nil
	case err != nil:
		return err
	case !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(key)):
		return nil // key is not there; the key that is stays
	}
	return remove()
}

// toolError is a helper program that exited with a failure status.
type toolError struct {
	name   string
	code   int
	stderr string
}

func (e *toolError) Error() string {
	if e.stderr != "" {
		return fmt.Sprintf("%s exited with status %d: %s", e.name, e.code, e.stderr)
	}
	return fmt.Sprintf("%s exited with status %d", e.name, e.code)
}

// runTool runs a helper program with stdin and returns its standard output.
// It is replaced in tests.
var runTool = func(stdin, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...) //nolint:gosec // G204: name is /usr/bin/security or secret-tool, args are fixed; the key goes on stdin
	cmd.Stdin = strings.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return nil, &toolError{name: name, code: exit.ExitCode(), stderr: strings.TrimSpace(stderr.String())}
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}

// exitedWith reports whether err is the tool exiting with code and, when
// quiet is set, having printed nothing on standard error.
func exitedWith(err error, code int, quiet bool) bool {
	var te *toolError
	return errors.As(err, &te) && te.code == code && (!quiet || te.stderr == "")
}

// storeHint says how to make the platform's store usable again.
func storeHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "Unlock your keychain, normally the login keychain (open Keychain Access, or sign in to the Mac again), and try again."
	case "linux":
		return "envgrid needs secret-tool (package libsecret-tools on Debian and Ubuntu) and a running, unlocked keyring such as GNOME Keyring or KWallet. Servers without a desktop session usually have none: there, set ENVGRID_MASTER_KEY in the service's environment instead."
	case "windows":
		return "Sign in to Windows as the same user who ran envgrid init, and try again."
	}
	return "Set ENVGRID_MASTER_KEY in the environment instead."
}

// keychainStore uses the default macOS keychain, normally the login keychain,
// through /usr/bin/security: add-generic-password without a keychain name
// adds to the default one (SecurityTool/macOS/keychain_add.c). By default an
// item trusts the program that created it, here the security tool, so envgrid
// can read the key without a dialog at every start.
type keychainStore struct{}

const (
	keyService = "envgrid"
	keyAccount = "master-key"
)

func (keychainStore) Name() string { return "your default macOS keychain" }

func (keychainStore) Get() ([]byte, error) {
	out, err := runTool("", "/usr/bin/security", "find-generic-password", "-a", keyAccount, "-s", keyService, "-w")
	// Not found makes main return errSecItemNotFound (-25300), which the exit
	// status keeps as its low 8 bits, 44 (apple-oss-distributions/Security,
	// SecurityTool/macOS keychain_find.c and security.c).
	if exitedWith(err, 44, false) {
		return nil, errKeyNotStored
	}
	if err != nil {
		return nil, err
	}
	return bytes.TrimRight(out, "\r\n"), nil
}

func (keychainStore) Set(key []byte) error {
	// Interactive mode (-i) reads the command from standard input, so the key
	// is not an argument of any process. A base64 key has no spaces or quotes,
	// and without -l the item's label is the service name. There is no -U:
	// exit status 44 from Get can also mean a failed search, so an existing
	// item must make this fail rather than be replaced.
	_, err := runTool(fmt.Sprintf("add-generic-password -a %s -s %s -w %s\n", keyAccount, keyService, key), "/usr/bin/security", "-i")
	return err
}

func (s keychainStore) Delete(key []byte) error {
	return deleteIfStored(s, key, func() error {
		_, err := runTool("", "/usr/bin/security", "delete-generic-password", "-a", keyAccount, "-s", keyService)
		return err
	})
}

// secretToolStore uses the freedesktop Secret Service (GNOME Keyring, KWallet)
// through secret-tool, which reads the secret from standard input. The
// keyring can hold several envgrid keys side by side; id, the key-id saved in
// the setup, says which one is meant.
type secretToolStore struct{ id string }

var secretAttrs = []string{"service", keyService, "item", keyAccount}

// withKeyID points a store that can hold several envgrid keys at the one with
// this id. Stores that hold one key are returned as they are.
func withKeyID(st secretStore, id string) secretStore {
	if s, ok := st.(secretToolStore); ok && id != "" {
		s.id = id
		return s
	}
	return st
}

func (secretToolStore) Name() string { return "your desktop keyring (Secret Service)" }

// Get counts the matching items first with search, which shows locked items
// too and asks for no unlocking (tool/secret-tool.c), so it never has to
// guess: with no item it reports nothing stored, and with several it refuses
// rather than take whichever lookup happens to return first.
func (s secretToolStore) Get() ([]byte, error) {
	attrs := append([]string{}, secretAttrs...)
	if s.id != "" {
		attrs = append(attrs, "key-id", s.id)
	}
	found, err := runTool("", "secret-tool", append([]string{"search", "--all"}, attrs...)...)
	if err != nil {
		return nil, err
	}
	items := 0
	for _, line := range bytes.Split(found, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("[")) {
			items++
		}
	}
	switch {
	case items == 0:
		return nil, errKeyNotStored
	case items > 1:
		return nil, fmt.Errorf("the keyring holds %d envgrid keys, and envgrid will not guess which one opens the database. secret-tool search --all service envgrid item master-key lists them with their key-id; keep the one your setup needs", items)
	}
	out, err := runTool("", "secret-tool", append([]string{"lookup"}, attrs...)...)
	out = bytes.TrimRight(out, "\r\n")
	// lookup exits 1 silently when an unlock prompt is dismissed
	// (libsecret/secret-methods.c).
	if exitedWith(err, 1, true) || (err == nil && len(out) == 0) {
		return nil, errors.New("the keyring holds an envgrid key but did not hand it over; it may be locked")
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// keyID names a key without revealing it: the first 8 bytes of its SHA-256,
// in hex. secret-tool store replaces an item whose attributes all match, so
// an id per key means storing one key can never replace an item holding
// another, even one a locked keyring did not show.
func keyID(key []byte) string {
	sum := sha256.Sum256(bytes.TrimSpace(key))
	return hex.EncodeToString(sum[:8])
}

func (secretToolStore) Set(key []byte) error {
	args := append([]string{"store", "--label=envgrid master key"}, secretAttrs...)
	_, err := runTool(string(key), "secret-tool", append(args, "key-id", keyID(key))...)
	return err
}

// Delete removes only the item holding key. clear removes unlocked items
// only, and exits 1 silently when it removed nothing (tool/secret-tool.c), so
// a search, which shows locked items too, decides whether the key is gone.
func (secretToolStore) Delete(key []byte) error {
	attrs := append(append([]string{}, secretAttrs...), "key-id", keyID(key))
	if _, err := runTool("", "secret-tool", append([]string{"clear"}, attrs...)...); err != nil && !exitedWith(err, 1, true) {
		return err
	}
	found, err := runTool("", "secret-tool", append([]string{"search"}, attrs...)...)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(found)) > 0 {
		return errors.New("the keyring still holds the key; it may be locked")
	}
	return nil
}
