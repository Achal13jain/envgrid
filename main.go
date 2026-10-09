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
  envgrid [serve]   start the server (default)
  envgrid seed      create the demo repo "acme-shop"
  envgrid genkey    print a new random ENVGRID_MASTER_KEY
  envgrid version   print the version

Client commands (talk to a running envgrid with an API token from Settings):
  envgrid export --repo R --file F --env E [--format json] [--no-resolve]
                    print one file for one environment
  envgrid run --repo R --file F --env E -- command [args...]
                    run a command with that file's values as environment variables
  Both read ENVGRID_URL (default http://localhost:8080) and ENVGRID_TOKEN.

Configuration (environment variables):
  ENVGRID_MASTER_KEY      required; 32 bytes, base64. Encrypts every value.
  ENVGRID_DATA_DIR        directory for envgrid.db (default /data; on Windows %LOCALAPPDATA%\envgrid\data)
  ENVGRID_LISTEN          listen address (default :8080)
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

func loadConfig() (*config, error) {
	c := &config{
		dataDir:       env("ENVGRID_DATA_DIR", defaultDataDir()),
		listen:        env("ENVGRID_LISTEN", ":8080"),
		adminEmail:    env("ENVGRID_ADMIN_EMAIL", ""),
		adminPassword: os.Getenv("ENVGRID_ADMIN_PASSWORD"),
	}
	var err error
	if c.secureCookies, err = strconv.ParseBool(env("ENVGRID_SECURE_COOKIES", "true")); err != nil {
		return nil, fmt.Errorf("ENVGRID_SECURE_COOKIES must be true or false")
	}
	if c.trustProxy, err = strconv.ParseBool(env("ENVGRID_TRUST_PROXY", "false")); err != nil {
		return nil, fmt.Errorf("ENVGRID_TRUST_PROXY must be true or false")
	}
	if err := c.logLevel.UnmarshalText([]byte(env("ENVGRID_LOG_LEVEL", "info"))); err != nil {
		return nil, fmt.Errorf("ENVGRID_LOG_LEVEL must be debug, info, warn or error")
	}
	raw := env("ENVGRID_MASTER_KEY", "")
	if raw == "" {
		return nil, errNoMasterKey
	}
	if c.masterKey, err = crypto.ParseKey(raw); err != nil {
		return nil, fmt.Errorf("ENVGRID_MASTER_KEY: %w", err)
	}
	return c, nil
}

// noKeyHelp is printed once, on stderr and never through the logger.
func noKeyHelp() string {
	key := crypto.GenerateKey()
	return fmt.Sprintf(`ENVGRID_MASTER_KEY is not set, so envgrid will not start.

Every value is encrypted with this key. Here is a freshly generated one:

    ENVGRID_MASTER_KEY=%s

1. Save it in your password manager now. Without it the database cannot be
   decrypted, and envgrid cannot recover it for you.
2. Start envgrid again with the variable set, for example:

    docker run -p 8080:8080 -v envgrid-data:/data \
      -e ENVGRID_MASTER_KEY=%s \
      -e ENVGRID_ADMIN_EMAIL=you@example.com -e ENVGRID_ADMIN_PASSWORD='choose-a-password' \
      ghcr.io/achal13jain/envgrid
`, key, key)
}

// run loads config, opens the store and bootstraps the first admin before
// handing over to the command.
func run(fn func(context.Context, *config, *store.Store, *slog.Logger) error) error {
	cfg, err := loadConfig()
	if errors.Is(err, errNoMasterKey) {
		fmt.Fprint(os.Stderr, noKeyHelp())
		os.Exit(1)
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
	log.Info("envgrid listening", "addr", cfg.listen, "version", version, "data_dir", cfg.dataDir)

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
