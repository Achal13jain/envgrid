package main

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Achal13jain/envgrid/internal/crypto"
	"github.com/Achal13jain/envgrid/internal/formats"
	"github.com/Achal13jain/envgrid/internal/server"
	"github.com/Achal13jain/envgrid/internal/store"
)

func TestCLIExportAndEnv(t *testing.T) {
	crypto.PasswordParams = crypto.Argon2Params{Time: 1, Memory: 1024, Threads: 1}
	ctx := context.Background()
	key, _ := crypto.ParseKey(crypto.GenerateKey())
	c, _ := crypto.NewCipher(key)
	st, err := store.Open(ctx, ":memory:", c)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	u, _ := st.CreateUser(ctx, "admin@example.com", "", "password123", store.RoleAdmin)
	repo, _ := st.CreateRepo(ctx, "shop", "", "", u.ID)
	envs, _ := st.Environments(ctx, repo.ID)
	f, _ := st.CreateFile(ctx, repo.ID, "app.env", formats.Dotenv)
	// A slice, not a map: the export follows creation order, and map order is random.
	for _, kv := range [][2]string{{"HOST", "db.prod"}, {"URL", "postgres://${HOST}/x"}} {
		k, err := st.CreateKey(ctx, f.ID, kv[0], "", false)
		if err != nil {
			t.Fatal(err)
		}
		v := kv[1]
		if _, err := st.SetValue(ctx, k.ID, envs[2].ID, &v, u.ID); err != nil {
			t.Fatal(err)
		}
	}
	token, _, err := st.CreateAPIToken(ctx, u.ID, "cli", nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(server.New(st, slog.New(slog.NewTextHandler(io.Discard, nil)), server.Config{}).Handler())
	defer srv.Close()

	target := cliTarget{url: srv.URL, token: token, repo: "shop", file: "app.env", env: "prod"}
	body, err := cliExport(ctx, target, "", true)
	if err != nil {
		t.Fatal(err)
	}
	vars, err := envList(body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(vars, ";") != "HOST=db.prod;URL=postgres://db.prod/x" {
		t.Fatalf("env: %v", vars)
	}
	if _, err := cliExport(ctx, cliTarget{url: srv.URL, token: "egt_wrong", repo: "shop", file: "app.env", env: "prod"}, "", true); err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("bad token: %v", err)
	}
	if _, err := cliExport(ctx, cliTarget{url: srv.URL, token: token, repo: "nope", file: "app.env", env: "prod"}, "", true); err == nil || !strings.Contains(err.Error(), "no repo named nope") {
		t.Fatalf("unknown repo: %v", err)
	}
}
