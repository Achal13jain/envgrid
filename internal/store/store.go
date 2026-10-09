// Package store is envgrid's SQLite persistence layer: hand-written SQL,
// embedded numbered migrations, and encryption of every value at rest.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/Achal13jain/envgrid/internal/crypto"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Sentinel errors mapped to HTTP statuses by the server.
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("already exists")
	// ErrTooMany refuses growth past the limits below.
	ErrTooMany            = errors.New("limit reached")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrWrongMasterKey     = errors.New("ENVGRID_MASTER_KEY does not match the key this database was created with")
)

// timeFormat is fixed width UTC, so text comparison is time comparison.
const timeFormat = "2006-01-02T15:04:05.000Z"

// Store wraps the database and the value cipher.
type Store struct {
	db     *sql.DB
	cipher *crypto.Cipher
	// Now is the clock; tests replace it to move time forward.
	Now func() time.Time
	// off holds the audit categories an admin chose not to record.
	off atomic.Pointer[map[string]bool]
}

// Open opens (or creates) the database at path, applies pending migrations
// and verifies the master key. Use ":memory:" for a private in-memory DB.
func Open(ctx context.Context, path string, c *crypto.Cipher) (*Store, error) {
	dsn := path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)&_txlock=immediate"
	if path != ":memory:" {
		dsn += "&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if path == ":memory:" {
		// Every new connection would get its own empty database.
		db.SetMaxOpenConns(1)
	}
	s := &Store{db: db, cipher: c, Now: time.Now}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.checkKey(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.loadAuditOff(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Ping checks the database is reachable.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) now() string { return s.Now().UTC().Format(timeFormat) }

// migrate applies pending migrations in order, each in its own transaction.
// Foreign keys are off while migrations run, as SQLite recommends for schema
// changes, so a table rebuild cannot cascade-delete rows; each migration must
// leave foreign_key_check clean before it commits.
func (s *Store) migrate(ctx context.Context) error {
	conn, err := s.db.Conn(ctx) // PRAGMAs are per connection: pin one
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer func() { _, _ = conn.ExecContext(context.WithoutCancel(ctx), `PRAGMA foreign_keys = ON`) }()

	entries, err := fs.ReadDir(migrations, "migrations") // sorted by name
	if err != nil {
		return err
	}
	// An older envgrid must not run against a database a newer one upgraded.
	var newest int
	if err := conn.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&newest); err != nil {
		return err
	}
	if last, _, _ := strings.Cut(entries[len(entries)-1].Name(), "_"); newest > mustAtoi(last) {
		return fmt.Errorf("this database was upgraded by a newer envgrid (schema %d); install that version or newer", newest)
	}
	for _, e := range entries {
		num, _, _ := strings.Cut(e.Name(), "_")
		version, err := strconv.Atoi(num)
		if err != nil {
			return fmt.Errorf("migration %s: name must start with a number", e.Name())
		}
		var applied int
		if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&applied); err != nil {
			return err
		}
		if applied > 0 {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		if err := s.applyMigration(ctx, conn, version, string(body)); err != nil {
			return fmt.Errorf("migration %s: %w", e.Name(), err)
		}
	}
	return nil
}

func (s *Store) applyMigration(ctx context.Context, conn *sql.Conn, version int, body string) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, body); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("foreign key check failed")
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, version, s.now()); err != nil {
		return err
	}
	return tx.Commit()
}

const keyCheckPlaintext = "envgrid-key-check"

func (s *Store) checkKey(ctx context.Context) error {
	aad := []byte("meta:key_check")
	var ct []byte
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'key_check'`).Scan(&ct)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = s.db.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES ('key_check', ?)`, s.cipher.Seal([]byte(keyCheckPlaintext), aad))
		return err
	}
	if err != nil {
		return err
	}
	pt, err := s.cipher.Open(ct, aad)
	if err != nil || string(pt) != keyCheckPlaintext {
		return ErrWrongMasterKey
	}
	return nil
}

// querier is satisfied by *sql.DB and *sql.Tx.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *Store) tx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// mapErr turns driver errors into the package sentinels.
func mapErr(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	var se *sqlite.Error
	if errors.As(err, &se) && (se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE || se.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY) {
		return ErrConflict
	}
	return err
}

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func valueAAD(keyID, envID int64) []byte {
	return []byte(fmt.Sprintf("envgrid:v1:key=%d:env=%d", keyID, envID))
}

func (s *Store) open(ct []byte, keyID, envID int64) (string, error) {
	pt, err := s.cipher.Open(ct, valueAAD(keyID, envID))
	if err != nil {
		return "", fmt.Errorf("key %d env %d: %w", keyID, envID, err)
	}
	return string(pt), nil
}

func mustAtoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
