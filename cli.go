package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"

	"github.com/Achal13jain/envgrid/internal/formats"
)

// The client commands talk to a running envgrid with a personal API token:
//
//	envgrid export --repo acme-shop --file backend.env --env prod
//	envgrid run --repo acme-shop --file backend.env --env prod -- node server.js

type cliTarget struct {
	url, token, repo, file, env string
}

func cliFlags(name string, t *cliTarget) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.StringVar(&t.url, "url", env("ENVGRID_URL", "http://localhost:8080"), "envgrid address (or ENVGRID_URL)")
	// No default from ENVGRID_TOKEN here: flag usage prints defaults, which
	// would show the token on any flag error. It is read after parsing.
	fs.StringVar(&t.token, "token", "", "API token from Settings (or ENVGRID_TOKEN)")
	fs.StringVar(&t.repo, "repo", "", "repo name")
	fs.StringVar(&t.file, "file", "", "file name")
	fs.StringVar(&t.env, "env", "", "environment name")
	return fs
}

func (t cliTarget) check() error {
	switch {
	case t.token == "":
		return errors.New("an API token is required: create one in Settings, then pass --token or set ENVGRID_TOKEN")
	case t.repo == "" || t.file == "" || t.env == "":
		return errors.New("--repo, --file and --env are required")
	}
	return nil
}

// cliGet calls the API with the token and decodes JSON errors into messages.
func cliGet(ctx context.Context, t cliTarget, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(t.url, "/")+"/api/v1"+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+t.token)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		var e struct {
			Error struct{ Message string } `json:"error"`
		}
		if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
			return nil, errors.New(e.Error.Message)
		}
		return nil, fmt.Errorf("envgrid answered %s", resp.Status)
	}
	return body, nil
}

// cliExport downloads one file for one environment; format "" keeps the
// file's own format.
func cliExport(ctx context.Context, t cliTarget, format string, resolve bool) ([]byte, error) {
	q := url.Values{"repo": {t.repo}, "file": {t.file}, "env": {t.env}}
	raw, err := cliGet(ctx, t, "/resolve?"+q.Encode())
	if err != nil {
		return nil, err
	}
	var ids struct{ File, Env int64 }
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil, err
	}
	p := url.Values{"env": {fmt.Sprint(ids.Env)}}
	if format != "" {
		p.Set("format", format)
	}
	if !resolve {
		p.Set("resolve", "false")
	}
	return cliGet(ctx, t, fmt.Sprintf("/files/%d/export?%s", ids.File, p.Encode()))
}

// envList turns a .env export into NAME=value entries for a process.
func envList(dotenv []byte) ([]string, error) {
	kvs, err := formats.Parse(formats.Dotenv, dotenv)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(kvs))
	for _, kv := range kvs {
		out = append(out, kv.Key+"="+kv.Value)
	}
	return out, nil
}

// cli runs the export and run commands and returns the exit code.
func cli(cmd string, args []string) int {
	var t cliTarget
	fs := cliFlags(cmd, &t)
	format := ""
	if cmd == "export" {
		fs.StringVar(&format, "format", "", "dotenv, json, yaml, properties, ini or csv (default: the file's own)")
	}
	noResolve := fs.Bool("no-resolve", false, "keep ${...} references as written")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if t.token == "" {
		t.token = os.Getenv("ENVGRID_TOKEN")
	}
	resolve := !*noResolve
	if err := t.check(); err != nil {
		fmt.Fprintln(os.Stderr, "envgrid:", err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if cmd == "export" {
		body, err := cliExport(ctx, t, format, resolve)
		if err != nil {
			fmt.Fprintln(os.Stderr, "envgrid:", err)
			return 1
		}
		_, _ = os.Stdout.Write(body)
		return 0
	}

	command := fs.Args()
	if len(command) == 0 {
		fmt.Fprintln(os.Stderr, "envgrid: give the command to run after --, for example: envgrid run --repo r --file f --env e -- node app.js")
		return 2
	}
	body, err := cliExport(ctx, t, formats.Dotenv, resolve)
	if err != nil {
		fmt.Fprintln(os.Stderr, "envgrid:", err)
		return 1
	}
	vars, err := envList(body)
	if err != nil {
		fmt.Fprintln(os.Stderr, "envgrid:", err)
		return 1
	}
	c := exec.CommandContext(ctx, command[0], command[1:]...) //nolint:gosec // G204: running the user's own command is the point
	c.Env = append(os.Environ(), vars...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "envgrid:", err)
		return 1
	}
	return 0
}
