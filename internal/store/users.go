package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/Achal13jain/envgrid/internal/crypto"
)

// Roles.
const (
	RoleAdmin  = "admin"
	RoleMember = "member"
)

// SessionTTL is the fixed lifetime of a login session.
const SessionTTL = 30 * 24 * time.Hour

// ErrLastAdmin protects against locking everyone out.
var ErrLastAdmin = errors.New("at least one active admin is required")

// User is an account. The password hash never leaves this package.
type User struct {
	ID       int64  `json:"id"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Disabled bool   `json:"disabled"`
	// ExpiresAt ends temporary access: after it the user cannot sign in or
	// use tokens. Nil means no end date.
	ExpiresAt *string `json:"expiresAt"`
	CreatedAt string  `json:"createdAt"`
	// LastSignedInAt is when the user last signed in; nil if never.
	LastSignedInAt *string `json:"lastSignedInAt"`
}

// IsAdmin reports whether the user has the admin role.
func (u *User) IsAdmin() bool { return u.Role == RoleAdmin }

// UserRef is the short form of a user embedded in other records.
type UserRef struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

const userCols = `u.id, u.email, u.name, u.role, u.disabled, u.created_at, u.expires_at, u.last_login_at`

type scanner interface{ Scan(dest ...any) error }

func scanUser(row scanner, extra ...any) (*User, error) {
	var u User
	var expires, lastLogin sql.NullString
	if err := row.Scan(append([]any{&u.ID, &u.Email, &u.Name, &u.Role, &u.Disabled, &u.CreatedAt, &expires, &lastLogin}, extra...)...); err != nil {
		return nil, mapErr(err)
	}
	if expires.Valid {
		u.ExpiresAt = &expires.String
	}
	if lastLogin.Valid {
		u.LastSignedInAt = &lastLogin.String
	}
	return &u, nil
}

// CreateUser hashes the password with argon2id and inserts the user.
func (s *Store) CreateUser(ctx context.Context, email, name, password, role string) (*User, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (email, name, password_hash, role, created_at) VALUES (?, ?, ?, ?, ?)`,
		email, name, crypto.HashPassword(password), role, s.now())
	if err != nil {
		return nil, mapErr(err)
	}
	id, _ := res.LastInsertId()
	return s.User(ctx, id)
}

// User returns one user by id.
func (s *Store) User(ctx context.Context, id int64) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users u WHERE u.id = ?`, id))
}

// Users lists all users by email.
func (s *Store) Users(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userCols+` FROM users u ORDER BY u.email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

// UserCount returns the number of accounts.
func (s *Store) UserCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// FirstAdmin returns the oldest active admin.
func (s *Store) FirstAdmin(ctx context.Context) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx,
		`SELECT `+userCols+` FROM users u WHERE u.role = 'admin' AND u.disabled = 0 ORDER BY u.id LIMIT 1`))
}

// UserPatch lists optional changes; nil fields are left alone.
type UserPatch struct {
	Name     *string
	Role     *string
	Disabled *bool
	Password *string
	// ExpiresAt sets the end of access; "" removes it.
	ExpiresAt *string
}

// UpdateUser applies a patch. Disabling a user or changing their password
// ends all of their sessions. It refuses to leave zero active admins.
func (s *Store) UpdateUser(ctx context.Context, id int64, p UserPatch) (*User, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id = ?`, id).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return ErrNotFound
		}
		if p.Name != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE users SET name = ? WHERE id = ?`, *p.Name, id); err != nil {
				return err
			}
		}
		if p.Role != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE users SET role = ? WHERE id = ?`, *p.Role, id); err != nil {
				return err
			}
		}
		if p.Disabled != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE users SET disabled = ? WHERE id = ?`, *p.Disabled, id); err != nil {
				return err
			}
		}
		if p.ExpiresAt != nil {
			var v any
			if *p.ExpiresAt != "" {
				v = *p.ExpiresAt
			}
			if _, err := tx.ExecContext(ctx, `UPDATE users SET expires_at = ? WHERE id = ?`, v, id); err != nil {
				return err
			}
		}
		if p.Password != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, crypto.HashPassword(*p.Password), id); err != nil {
				return err
			}
		}
		if (p.Disabled != nil && *p.Disabled) || p.Password != nil {
			if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
				return err
			}
		}
		// A password reset is the recovery step after a compromise, so it
		// ends API tokens as well as sessions.
		if p.Password != nil {
			if _, err := tx.ExecContext(ctx, `DELETE FROM api_tokens WHERE user_id = ?`, id); err != nil {
				return err
			}
		}
		var admins int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin' AND disabled = 0 AND expires_at IS NULL`).Scan(&admins); err != nil {
			return err
		}
		if admins == 0 {
			return ErrLastAdmin
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.User(ctx, id)
}

// dummyHash gives unknown emails the same argon2id cost as real ones, so
// response time does not reveal which accounts exist.
var dummyHash = sync.OnceValue(func() string { return crypto.HashPassword("envgrid-timing-equaliser") })

// Authenticate checks an email and password. Disabled users fail the same
// way as wrong passwords.
func (s *Store) Authenticate(ctx context.Context, email, password string) (*User, error) {
	var hash string
	u, err := scanUser(s.db.QueryRowContext(ctx,
		`SELECT `+userCols+`, u.password_hash FROM users u WHERE u.email = ?`, email), &hash)
	if errors.Is(err, ErrNotFound) {
		crypto.VerifyPassword(password, dummyHash())
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if !crypto.VerifyPassword(password, hash) || u.Disabled {
		return nil, ErrInvalidCredentials
	}
	// Only after a correct password, so it does not reveal which accounts exist.
	if u.ExpiresAt != nil && *u.ExpiresAt <= s.now() {
		return u, ErrAccessExpired
	}
	return u, nil
}

// CheckPassword verifies a user's current password.
func (s *Store) CheckPassword(ctx context.Context, userID int64, password string) bool {
	var hash string
	if err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&hash); err != nil {
		return false
	}
	return crypto.VerifyPassword(password, hash)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateSession starts a session and returns the cookie token. Only the
// token's hash is stored. Expired sessions are swept at the same time.
func (s *Store) CreateSession(ctx context.Context, userID int64) (string, time.Time, error) {
	token := crypto.Token()
	expires := s.Now().UTC().Add(SessionTTL)
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, s.now()); err != nil {
		return "", time.Time{}, err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)`,
		hashToken(token), userID, expires.Format(timeFormat)); err != nil {
		return "", time.Time{}, err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, s.now(), userID)
	return token, expires, err
}

// SessionUser resolves a cookie token to its active, unexpired user.
func (s *Store) SessionUser(ctx context.Context, token string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.id = ?1 AND s.expires_at > ?2 AND u.disabled = 0 AND (u.expires_at IS NULL OR u.expires_at > ?2)`,
		hashToken(token), s.now()))
}

// DeleteSession ends one session.
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, hashToken(token))
	return err
}
