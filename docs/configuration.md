# Configuration and operations

## Settings

All configuration is through environment variables.

| Variable | Default | Meaning |
| --- | --- | --- |
| `ENVGRID_MASTER_KEY` | none, required | 32 random bytes, base64 encoded. Encrypts every value. Generate one with `envgrid genkey`. |
| `ENVGRID_DATA_DIR` | `/data` (on Windows `%LOCALAPPDATA%\envgrid\data`) | Directory that holds `envgrid.db`. |
| `ENVGRID_LISTEN` | `:8080` | Address and port to listen on. |
| `ENVGRID_SECURE_COOKIES` | `true` | Marks the session cookie as HTTPS only. Set `false` only to try envgrid over plain HTTP. |
| `ENVGRID_ADMIN_EMAIL` | empty | With the password below, creates the first admin when the database has no users. Ignored afterwards. |
| `ENVGRID_ADMIN_PASSWORD` | empty | At least 8 characters. |
| `ENVGRID_TRUST_PROXY` | `false` | Takes the client IP for rate limiting and the audit log from the last `X-Forwarded-For` entry. Enable it only behind exactly one reverse proxy that sets that header. |
| `ENVGRID_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. Logs are JSON on standard output. |

Commands: `envgrid serve` (the default), `envgrid seed`, `envgrid genkey` and `envgrid version`.

## Command line client

The same binary reads values from a running envgrid with an API token. Create a token in Settings, then:

```sh
export ENVGRID_URL=https://envgrid.example.com
export ENVGRID_TOKEN=egt_...
envgrid export --repo acme-shop --file backend.env --env prod > .env
envgrid export --repo acme-shop --file backend.env --env prod --format json
envgrid run --repo acme-shop --file backend.env --env prod -- node server.js
```

`--no-resolve` keeps `${...}` references as written. The token acts as its owner, with the same role, and every read is in the audit log.

## Backup and restore

Everything lives in one file, `envgrid.db`, in the data directory. A backup is a copy of that file plus the master key, kept apart from each other.

To back up, stop envgrid so the database is closed cleanly, copy the data directory, and start envgrid again:

```sh
docker stop envgrid
docker run --rm -v envgrid-data:/data -v "$PWD":/backup alpine cp -a /data/. /backup/envgrid-backup/
docker start envgrid
```

To restore, stop envgrid, put `envgrid.db` back into the data directory, and start envgrid with the same `ENVGRID_MASTER_KEY`. If the key does not match, envgrid refuses to start and says so. Without the original key a backup cannot be decrypted, and nobody can recover it.

## Build from source

To build from source you need Go 1.26 and Node.js 22 or later. Run `cd frontend && npm ci && npm run build && cd ..`, then `go build -o envgrid .`.
