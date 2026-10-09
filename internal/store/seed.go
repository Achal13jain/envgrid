package store

import (
	"context"
	"errors"
	"fmt"
)

// SeedRepoName is the demo repo created by `envgrid seed`.
const SeedRepoName = "acme-shop"

type seedKey struct {
	name, description string
	secret            bool
	values            [3]string // test, uat, prod; "" means missing
}

type seedFile struct {
	name, format string
	keys         []seedKey
}

// Obviously fake values: the demo must never look like a leaked credential.
var seedFiles = []seedFile{
	{name: "backend.env", format: "dotenv", keys: []seedKey{
		{"APP_ENV", "Runtime environment name", false, [3]string{"test", "uat", "production"}},
		{"PORT", "HTTP listen port", false, [3]string{"8080", "8080", "8080"}},
		{"LOG_LEVEL", "debug, info, warn or error", false, [3]string{"debug", "debug", "info"}},
		{"DATABASE_URL", "Primary Postgres connection string", true, [3]string{
			"postgres://shop:demo-test-pass@db.test.internal:5432/shop",
			"postgres://shop:demo-uat-pass@db.uat.internal:5432/shop",
			"postgres://shop:demo-prod-pass@db.prod.internal:5432/shop"}},
		{"REDIS_URL", "Cache and sessions", false, [3]string{"redis://redis.test.internal:6379/0", "redis://redis.uat.internal:6379/0", ""}},
		{"JWT_SECRET", "Signs customer sessions", true, [3]string{"demo-jwt-test-0000", "demo-jwt-uat-1111", "demo-jwt-prod-2222"}},
		{"PAYMENTS_API_KEY", "Payment provider key (demo)", true, [3]string{"demo-payments-sandbox-key", "demo-payments-sandbox-key", "demo-payments-live-key"}},
		{"SENTRY_DSN", "Error reporting endpoint", false, [3]string{"", "https://demo@errors.example.com/2", "https://demo@errors.example.com/3"}},
		{"FEATURE_NEW_CHECKOUT", "Roll-out flag for the new checkout", false, [3]string{"true", "true", "false"}},
		{"SMTP_PASSWORD", "Transactional email relay", true, [3]string{"demo-smtp-test", "", "demo-smtp-prod"}},
	}},
	{name: "frontend.json", format: "json", keys: []seedKey{
		{"api.baseUrl", "Public API origin", false, [3]string{"https://api.test.acme.example", "https://api.uat.acme.example", "https://api.acme.example"}},
		{"api.timeoutMs", "", false, [3]string{"10000", "10000", "5000"}},
		{"analytics.enabled", "", false, [3]string{"false", "true", "true"}},
		{"analytics.writeKey", "Browser analytics write key (demo)", true, [3]string{"", "demo-analytics-uat", "demo-analytics-prod"}},
		{"featureFlags.darkMode", "", false, [3]string{"true", "true", ""}},
		{"support.email", "", false, [3]string{"support@acme.example", "support@acme.example", "support@acme.example"}},
		{"maps.apiKey", "Maps widget key (demo)", true, [3]string{"demo-maps-test", "demo-maps-shared", "demo-maps-shared"}},
	}},
}

// ErrAlreadySeeded is returned when the demo repo exists.
var ErrAlreadySeeded = fmt.Errorf("demo repo %q already exists", SeedRepoName)

// Seed creates the demo repo with three environments, two files and
// realistic gaps and differences, owned by userID.
func (s *Store) Seed(ctx context.Context, userID int64) (*Repo, error) {
	repo, err := s.CreateRepo(ctx, SeedRepoName, "Demo data created by envgrid seed. Try the grid, compare test with prod, and open the history of a value.", "", userID)
	if errors.Is(err, ErrConflict) {
		return nil, ErrAlreadySeeded
	}
	if err != nil {
		return nil, err
	}
	envs, err := s.Environments(ctx, repo.ID)
	if err != nil {
		return nil, err
	}
	if err := s.Audit(ctx, AuditEntry{UserID: userID, Action: "create_repo", RepoID: repo.ID, Detail: map[string]any{"repo": repo.Name, "via": "seed"}}); err != nil {
		return nil, err
	}
	for _, sf := range seedFiles {
		f, err := s.CreateFile(ctx, repo.ID, sf.name, sf.format)
		if err != nil {
			return nil, err
		}
		for _, sk := range sf.keys {
			k, err := s.CreateKey(ctx, f.ID, sk.name, sk.description, sk.secret)
			if err != nil {
				return nil, err
			}
			for i, v := range sk.values {
				if v == "" {
					continue
				}
				if _, err := s.SetValue(ctx, k.ID, envs[i].ID, &v, userID); err != nil {
					return nil, err
				}
			}
			if sk.name == "LOG_LEVEL" { // some history for the drawer
				for _, v := range []string{"warn", "info"} {
					if _, err := s.SetValue(ctx, k.ID, envs[2].ID, &v, userID); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	return s.Repo(ctx, repo.ID)
}
