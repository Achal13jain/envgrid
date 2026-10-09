package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Achal13jain/envgrid/internal/crypto"
)

// TokenPrefix starts every API token, so leaked tokens are easy to spot.
const TokenPrefix = "egt_"

// APIToken describes a personal token; the token itself is shown once.
type APIToken struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	CreatedAt  string  `json:"createdAt"`
	ExpiresAt  *string `json:"expiresAt"`
	LastUsedAt *string `json:"lastUsedAt"`
}

// CreateAPIToken makes a token for userID. Only its SHA-256 is stored.
func (s *Store) CreateAPIToken(ctx context.Context, userID int64, name string, expires *time.Time) (string, *APIToken, error) {
	token := TokenPrefix + crypto.Token()
	var exp any
	if expires != nil {
		exp = expires.UTC().Format(timeFormat)
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO api_tokens (user_id, name, token_hash, prefix, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		userID, name, hashToken(token), token[:len(TokenPrefix)+6], s.now(), exp)
	if err != nil {
		return "", nil, mapErr(err)
	}
	id, _ := res.LastInsertId()
	t, err := s.apiToken(ctx, id, userID)
	return token, t, err
}

const tokenCols = `id, name, prefix, created_at, expires_at, last_used_at` //nolint:gosec // G101: column names, not a credential

func scanToken(row scanner) (*APIToken, error) {
	var t APIToken
	var exp, used sql.NullString
	if err := row.Scan(&t.ID, &t.Name, &t.Prefix, &t.CreatedAt, &exp, &used); err != nil {
		return nil, mapErr(err)
	}
	if exp.Valid {
		t.ExpiresAt = &exp.String
	}
	if used.Valid {
		t.LastUsedAt = &used.String
	}
	return &t, nil
}

func (s *Store) apiToken(ctx context.Context, id, userID int64) (*APIToken, error) {
	return scanToken(s.db.QueryRowContext(ctx, `SELECT `+tokenCols+` FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID))
}

// APITokens lists a user's tokens, newest first.
func (s *Store) APITokens(ctx context.Context, userID int64) ([]APIToken, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+tokenCols+` FROM api_tokens WHERE user_id = ? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIToken{}
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// DeleteAPIToken revokes one of a user's tokens.
func (s *Store) DeleteAPIToken(ctx context.Context, id, userID int64) error {
	return affected(s.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID))
}

// TokenUser resolves a bearer token to its active user, and records its use.
func (s *Store) TokenUser(ctx context.Context, token string) (*User, error) {
	now := s.now()
	var tokenID int64
	u, err := scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+`, t.id FROM api_tokens t
		JOIN users u ON u.id = t.user_id
		WHERE t.token_hash = ? AND (t.expires_at IS NULL OR t.expires_at > ?)
		  AND u.disabled = 0 AND (u.expires_at IS NULL OR u.expires_at > ?)`, hashToken(token), now, now), &tokenID)
	if err != nil {
		return nil, err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, now, tokenID)
	return u, nil
}

// ErrAccessExpired is returned at sign-in when a user's access has ended.
var ErrAccessExpired = errors.New("access has ended")
