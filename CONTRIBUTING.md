# Contributing to envgrid

Thank you for helping. envgrid aims to stay small: one binary, one SQLite file, no integrations. Changes that keep it that way are the easiest to accept, so if you plan something large, open an issue first and describe the problem it solves. To report a security problem, follow [SECURITY.md](SECURITY.md) instead of opening an issue.

## What you need

- Go 1.26 or later.
- Node.js 22 or later, with npm.
- No C compiler: the SQLite driver is pure Go.

## Layout

| Path | What lives there |
| --- | --- |
| `main.go`, `cli.go` | Configuration, the `serve`, `seed` and `genkey` commands, the command line client and the embedded UI |
| `internal/crypto` | AES-256-GCM for values and argon2id for passwords |
| `internal/formats` | Parsers and exporters for every file format |
| `internal/store` | SQLite schema, migrations and every query |
| `internal/server` | HTTP routes, authentication, permissions, auditing and security headers |
| `frontend/` | The React app and the website (Vite, TypeScript, Tailwind) |

## Running from source

Build the UI once, then the binary, which embeds the UI:

```sh
cd frontend && npm ci && npm run build && cd ..
go build -o envgrid .
```

Run it against a local data directory:

```sh
export ENVGRID_MASTER_KEY="$(./envgrid genkey)"
export ENVGRID_DATA_DIR=./data ENVGRID_SECURE_COOKIES=false
export ENVGRID_ADMIN_EMAIL=admin@example.com ENVGRID_ADMIN_PASSWORD=admin-password
./envgrid seed    # optional demo data
./envgrid
```

For UI work, keep that server running on port 8080 and start the Vite dev server in a second terminal with `cd frontend && npm run dev`. It proxies `/api` to the Go server and reloads the page as you edit.

## Checks

CI runs these on every pull request. Please run them before you open one.

```sh
# Go
go vet . ./internal/...
go test . ./internal/...

# Frontend
cd frontend
npm run lint
npm test
npm run build
```

The Go commands name their packages because `frontend/node_modules` can contain stray Go files that `./...` would pick up.

## Rules of the road

- **Never log values.** Not in debug logs, not in error messages, not in audit details. `TestValuesNeverLogged` in `internal/server` drives values through the API and fails if one appears in the log output; extend it when you add a path that handles values.
- **Plaintext leaves the server only through audited endpoints** (reveal, export, compare with reveal).
- **Permission checks belong on the server.** The UI may hide a button, but the handler must refuse the request.
- **Schema changes are new migrations.** Add `internal/store/migrations/000N_description.sql`; never edit a migration that has shipped.
- **No new dependencies for small things.** Hand-written SQL, and the standard library first.
- **Accessibility is part of done.** Everything works from the keyboard, focus is visible, and colour is never the only signal.

## Pull requests

Keep each pull request to one change, and describe what changed and how you tested it. By contributing you agree that your work is released under the MIT licence of this project.
