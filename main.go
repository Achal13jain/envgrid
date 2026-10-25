// Command envgrid is a self-hosted grid of config keys across environments:
// one binary, one SQLite file.
package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Achal13jain/envgrid/internal/crypto"
	"github.com/Achal13jain/envgrid/internal/server"
	"github.com/Achal13jain/envgrid/internal/store"
)

//go:embed all:frontend/dist
var dist embed.FS

// version is set at build time with -ldflags "-X main.version=v1.2.3".
var version = "dev"

const usage = `envgrid: shared environment variables and config files, one grid per file.

Usage:
  envgrid init      one-time local setup: stores the master key in this
                    computer's secure store and remembers the data folder
  envgrid [serve]   start the server (default)
  envgrid seed      create the demo repo "acme-shop"
  envgrid key       print the stored master key, to save a recovery copy
  envgrid forget    remove the local setup and stored key (not the data)
  envgrid genkey    print a new random ENVGRID_MASTER_KEY
  envgrid version   print the version

Client commands (talk to a running envgrid with an API token from Settings):
  envgrid export --repo R --file F --env E [--format json] [--no-resolve]
                    print one file for one environment
  envgrid run --repo R --file F --env E -- command [args...]
                    run a command with that file's values as environment variables
  Both read ENVGRID_URL (default http://localhost:8080) and ENVGRID_TOKEN.

Configuration: each setting comes from its environment variable when set,
otherwise from the local setup made by envgrid init, otherwise its default.
  ENVGRID_MASTER_KEY      32 bytes, base64. Encrypts every value. Required
                          unless envgrid init stored it.
  ENVGRID_DATA_DIR        directory for envgrid.db (default /data; on Windows %LOCALAPPDATA%\envgrid\data)
  ENVGRID_LISTEN          listen address (default :8080; 127.0.0.1:8080 after envgrid init)
  ENVGRID_SECURE_COOKIES  set false only for plain-HTTP local use (default true)
  ENVGRID_ADMIN_EMAIL     with ENVGRID_ADMIN_PASSWORD, creates the first admin
  ENVGRID_ADMIN_PASSWORD  on a database with no users
  ENVGRID_TRUST_PROXY     take client IPs from X-Forwarded-For (default false)
  ENVGRID_LOG_LEVEL       debug, info, warn or error (default info)
`

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "serve":
		err = run(serve)
	case "seed":
		err = run(seed)
	case "init":
		err = initCmd(os.Args[2:])
	case "key":
		err = keyCmd()
	case "forget":
		err = forgetCmd()
	case "genkey":
		fmt.Println(crypto.GenerateKey())
	case "export", "run":
		os.Exit(cli(cmd, os.Args[2:]))
	case "version", "--version", "-v":
		fmt.Println(version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "envgrid:", err)
		os.Exit(1)
	}
}

type config struct {
	dataDir       string
	listen        string
	masterKey     []byte
	keySource     string // where the key came from, for the start-up log
	fromSetup     bool   // dataDir and key come from envgrid init's setup
	secureCookies bool
	trustProxy    bool
	adminEmail    string
	adminPassword string
	logLevel      slog.Level
}

func env(name, def string) string {
	if v, ok := os.LookupEnv(name); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

var errNoMasterKey = errors.New("ENVGRID_MASTER_KEY is not set")

// defaultDataDir is /data, the Docker volume, except on Windows, where /data
// is the root of the current drive and inherits whatever that drive allows.
// %LOCALAPPDATA% belongs to the signed-in user.
func defaultDataDir() string {
	if runtime.GOOS == "windows" {
		if d, err := os.UserCacheDir(); err == nil {
			return filepath.Join(d, "envgrid", "data")
		}
	}
	return "/data"
}

// loadConfig takes each setting from its environment variable when set, then
// from the local setup saved by envgrid init, then from the default. The
// stored key is only used for the setup's own data folder. When no key is
// found it still returns the config, with errNoMasterKey.
func loadConfig() (*config, error) {
	setup, err := readSetup()
	if err != nil {
		return nil, err
	}
	c := &config{
		dataDir:       env("ENVGRID_DATA_DIR", ""),
		listen:        env("ENVGRID_LISTEN", ""),
		adminEmail:    env("ENVGRID_ADMIN_EMAIL", ""),
		adminPassword: os.Getenv("ENVGRID_ADMIN_PASSWORD"),
	}
	if c.dataDir == "" && setup != nil {
		c.dataDir = setup.DataDir
	}
	if c.dataDir == "" {
		c.dataDir = defaultDataDir()
	}
	c.fromSetup = setup != nil && samePath(c.dataDir, setup.DataDir)
	// The setup's address and cookie setting belong to its own folder, so a
	// service for another folder keeps the defaults.
	if c.listen == "" && c.fromSetup {
		c.listen = setup.Listen
	}
	if c.listen == "" {
		c.listen = ":8080"
	}
	if raw := env("ENVGRID_SECURE_COOKIES", ""); raw != "" {
		if c.secureCookies, err = strconv.ParseBool(raw); err != nil {
			return nil, fmt.Errorf("ENVGRID_SECURE_COOKIES must be true or false")
		}
	} else if c.fromSetup {
		// The setup turns secure cookies off only because it listens on this
		// computer alone; refuse that combination on any other address.
		c.secureCookies = setup.SecureCookies
		if !c.secureCookies && !isLoopback(c.listen) {
			return nil, fmt.Errorf("envgrid init set up plain-HTTP cookies for this computer only, but envgrid would listen on %s. Set ENVGRID_SECURE_COOKIES=true behind HTTPS, or listen on 127.0.0.1", c.listen)
		}
	} else {
		c.secureCookies = true
	}
	if c.trustProxy, err = strconv.ParseBool(env("ENVGRID_TRUST_PROXY", "false")); err != nil {
		return nil, fmt.Errorf("ENVGRID_TRUST_PROXY must be true or false")
	}
	if err := c.logLevel.UnmarshalText([]byte(env("ENVGRID_LOG_LEVEL", "info"))); err != nil {
		return nil, fmt.Errorf("ENVGRID_LOG_LEVEL must be debug, info, warn or error")
	}
	// The database envgrid init recorded must still be there, whatever the key's
	// source, so a lost database is reported and never replaced by an empty one.
	// Nor is any empty envgrid.db opened: SQLite would make it a new database.
	if c.fromSetup {
		exists, err := databaseState(c.dataDir)
		if err != nil {
			return nil, err
		}
		if !exists || emptyDatabase(c.dataDir) {
			return nil, missingDatabase(c.dataDir)
		}
	} else if emptyDatabase(c.dataDir) {
		return nil, truncatedDatabase(c.dataDir)
	} else if !fileExists(dbPath(c.dataDir)) {
		if err := journalLeftovers(c.dataDir); err != nil {
			return nil, err
		}
	}
	raw := env("ENVGRID_MASTER_KEY", "")
	switch {
	case raw != "":
		if c.masterKey, err = crypto.ParseKey(raw); err != nil {
			return nil, fmt.Errorf("ENVGRID_MASTER_KEY: %w", err)
		}
		c.keySource = "ENVGRID_MASTER_KEY"
	case c.fromSetup:
		st, err := openSecretStore()
		if err != nil {
			return nil, errStore(nil, "use", err)
		}
		st = withKeyID(st, setup.KeyID)
		stored, err := st.Get()
		if errors.Is(err, errKeyNotStored) {
			return nil, keyMissing(st, c.dataDir)
		}
		if err != nil {
			return nil, errStore(st, "read the key from", err)
		}
		if c.masterKey, err = crypto.ParseKey(string(stored)); err != nil {
			return nil, errStore(st, "use the key in", err)
		}
		c.keySource = st.Name()
	default:
		return c, errNoMasterKey
	}
	return c, nil
}

// samePath reports whether two paths name the same folder. When both exist
// the operating system decides, which covers letter case and links. When one
// is missing, for example on a drive that is not connected, links are
// resolved in the part that exists and the rest is compared, ignoring case on
// Windows and macOS, whose disks usually do.
func samePath(a, b string) bool {
	if ia, err := os.Stat(a); err == nil {
		if ib, err := os.Stat(b); err == nil {
			return os.SameFile(ia, ib)
		}
	}
	pa, pb := resolvePath(a), resolvePath(b)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(pa, pb)
	}
	return pa == pb
}

// resolvePath makes p absolute and resolves links in the deepest part of it
// that exists.
func resolvePath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	dir, rest := abs, ""
	for {
		if r, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return abs
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
	}
}

// noKeyHelp explains a missing key. It never offers a new key when a
// database already exists, because that key could not open it.
func noKeyHelp(dataDir string) error {
	exists, err := databaseState(dataDir)
	if err != nil {
		return fmt.Errorf("no master key is set, and %w", err)
	}
	// With a setup, a missing key means ENVGRID_DATA_DIR points elsewhere.
	if setup, _ := readSetup(); setup != nil {
		return fmt.Errorf(`no master key is set for %s, so envgrid will not start.

The key stored by envgrid init is only used for the folder it set up, %s.
Unset ENVGRID_DATA_DIR to use that setup, or set ENVGRID_MASTER_KEY to the key
for %s`, dataDir, setup.DataDir, dataDir)
	}
	if exists {
		return fmt.Errorf(`no master key is set, and a database already exists in %s.

envgrid never makes a new key for an existing database: only the key it was
created with can open it. Either run envgrid init and paste that key when it
asks, so later starts find it on their own, or set ENVGRID_MASTER_KEY to it`, dataDir)
	}
	// No key is printed here: this text often lands in docker logs or the
	// system journal, where a key must never be.
	return errors.New(`no master key is set, so envgrid will not start.

On your own computer, run envgrid init once: it creates the key, stores it in
this computer's secure store and remembers the data folder, so envgrid serve
needs nothing set afterwards.

For Docker, a service or a server, set ENVGRID_MASTER_KEY instead. Make a key
in your own terminal with envgrid genkey (with Docker: docker run --rm
ghcr.io/achal13jain/envgrid genkey) and save it in your password manager. Pass
it from a file only you can read rather than on the command line, for example:

    docker run -p 8080:8080 -v envgrid-data:/data --env-file envgrid.env ghcr.io/achal13jain/envgrid

where envgrid.env holds ENVGRID_MASTER_KEY, ENVGRID_ADMIN_EMAIL and
ENVGRID_ADMIN_PASSWORD, one NAME=value per line`)
}

// run loads config, opens the store and bootstraps the first admin before
// handing over to the command.
func run(fn func(context.Context, *config, *store.Store, *slog.Logger) error) error {
	cfg, err := loadConfig()
	if errors.Is(err, errNoMasterKey) {
		return noKeyHelp(cfg.dataDir)
	}
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.logLevel}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(cfg.dataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	// MkdirAll leaves an existing directory as it is, so tighten it: the
	// database holds password hashes and roles in plain rows. Windows ignores
	// the mode; there the folder keeps its parent's permissions.
	if err := os.Chmod(cfg.dataDir, 0o700); err != nil { //nolint:gosec // G302: a directory needs the execute bit
		return fmt.Errorf("restrict data dir: %w", err)
	}
	cipher, err := crypto.NewCipher(cfg.masterKey)
	if err != nil {
		return err
	}
	dbPath := filepath.Join(cfg.dataDir, "envgrid.db")
	st, err := store.Open(ctx, dbPath, cipher)
	if err != nil {
		if cfg.keySource != "ENVGRID_MASTER_KEY" {
			err = wrongKey(err, "the key stored in "+cfg.keySource, cfg.dataDir)
		}
		return fmt.Errorf("open %s: %w", dbPath, err)
	}
	defer func() { _ = st.Close() }()
	if err := bootstrapAdmin(ctx, cfg, st, log); err != nil {
		return err
	}
	return fn(ctx, cfg, st, log)
}

func bootstrapAdmin(ctx context.Context, cfg *config, st *store.Store, log *slog.Logger) error {
	n, err := st.UserCount(ctx)
	if err != nil || n > 0 {
		return err
	}
	if cfg.adminEmail == "" || cfg.adminPassword == "" {
		log.Warn("no users exist yet: set ENVGRID_ADMIN_EMAIL and ENVGRID_ADMIN_PASSWORD and restart to create the first admin")
		return nil
	}
	if len([]rune(cfg.adminPassword)) < server.MinPasswordLength {
		return fmt.Errorf("ENVGRID_ADMIN_PASSWORD must be at least %d characters", server.MinPasswordLength)
	}
	u, err := st.CreateUser(ctx, cfg.adminEmail, "Admin", cfg.adminPassword, store.RoleAdmin)
	if err != nil {
		return fmt.Errorf("create first admin: %w", err)
	}
	log.Info("created first admin", "email", u.Email)
	return st.Audit(ctx, store.AuditEntry{UserID: u.ID, Action: "user_created", Detail: map[string]any{"email": u.Email, "role": u.Role, "via": "bootstrap"}})
}

func serve(ctx context.Context, cfg *config, st *store.Store, log *slog.Logger) error {
	static, err := fs.Sub(dist, "frontend/dist")
	if err != nil {
		return err
	}
	if !cfg.secureCookies {
		log.Warn("ENVGRID_SECURE_COOKIES=false: session cookies will be sent over plain HTTP; use only for local testing")
	}
	srv := &http.Server{
		Addr: cfg.listen,
		Handler: server.New(st, log, server.Config{
			SecureCookies: cfg.secureCookies, TrustProxy: cfg.trustProxy, Static: static, Version: version,
		}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("envgrid listening", "addr", cfg.listen, "version", version, "data_dir", cfg.dataDir, "key_from", cfg.keySource)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func seed(ctx context.Context, _ *config, st *store.Store, _ *slog.Logger) error {
	admin, err := st.FirstAdmin(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return errors.New("no admin exists: set ENVGRID_ADMIN_EMAIL and ENVGRID_ADMIN_PASSWORD and run seed again")
	}
	if err != nil {
		return err
	}
	repo, err := st.Seed(ctx, admin.ID)
	if errors.Is(err, store.ErrAlreadySeeded) {
		fmt.Println(err.Error() + "; nothing to do")
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Printf("Created demo repo %q with %d environments and %d files, owned by %s.\n", repo.Name, repo.EnvCount, repo.FileCount, admin.Email)
	return nil
}
