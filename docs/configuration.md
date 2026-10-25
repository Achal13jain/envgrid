# Configuration and operations

## Settings

Each setting comes from its environment variable when that is set, otherwise from the local setup saved by `envgrid init`, otherwise from its default.

| Variable | Default | Meaning |
| --- | --- | --- |
| `ENVGRID_MASTER_KEY` | none | 32 random bytes, base64 encoded. Encrypts every value. Required unless `envgrid init` stored it. Generate one with `envgrid genkey`. |
| `ENVGRID_DATA_DIR` | `/data` (on Windows `%LOCALAPPDATA%\envgrid\data`) | Directory that holds `envgrid.db`. |
| `ENVGRID_LISTEN` | `:8080`, or `127.0.0.1:8080` after `envgrid init` | Address and port to listen on. |
| `ENVGRID_SECURE_COOKIES` | `true`, or `false` after `envgrid init` | Marks the session cookie as HTTPS only. `false` is for plain HTTP on your own computer. |
| `ENVGRID_ADMIN_EMAIL` | empty | With the password below, creates the first admin when the database has no users. Ignored afterwards. |
| `ENVGRID_ADMIN_PASSWORD` | empty | At least 8 characters. |
| `ENVGRID_TRUST_PROXY` | `false` | Takes the client IP for rate limiting and the audit log from the last `X-Forwarded-For` entry. Enable it only behind exactly one reverse proxy that sets that header. |
| `ENVGRID_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. Logs are JSON on standard output. |

Commands: `envgrid init`, `envgrid serve` (the default), `envgrid seed`, `envgrid key`, `envgrid forget`, `envgrid genkey` and `envgrid version`.

## Local setup with envgrid init

`envgrid init` is a one-time setup for running envgrid on your own computer. It:

1. picks the data folder: `--data-dir` if you give one, otherwise `ENVGRID_DATA_DIR`, otherwise `%LOCALAPPDATA%\envgrid\data` on Windows and `~/.envgrid` on macOS and Linux;
2. creates a master key, unless the folder already holds a database: then it asks for that database's key (or reads `ENVGRID_MASTER_KEY`) and checks it against the database before keeping it;
3. asks for the first admin's email and a hidden password (or reads `ENVGRID_ADMIN_EMAIL` and `ENVGRID_ADMIN_PASSWORD`, for scripts), when the database has no users yet;
4. checks the operating system's secure key store, refusing to go on if it holds a different key or cannot be read; for a new database it claims an empty `envgrid.db` file, then stores the key and reads it back;
5. creates the database and the first admin;
6. saves the data folder, the address `127.0.0.1:8080` and plain-HTTP cookies in a settings file that holds no secret: `%LOCALAPPDATA%\envgrid\setup.json` on Windows, `~/Library/Application Support/envgrid/setup.json` on macOS, `~/.config/envgrid/setup.json` on Linux.

It shows a new key once. Save it in your password manager: it is the only way to read the data if the computer or your user account is lost. Afterwards `envgrid serve` needs nothing set, in any new terminal.

If a step fails before the new database holds anything, `envgrid init` removes the empty file it claimed and the key it stored. Once it has created the database, it never removes it, because another envgrid started with the same key may already be using it: it keeps the database and the stored key, prints the key, and running `envgrid init` again with that key finishes the setup. A key that was already stored before the run is never removed. If the store accepts the key but cannot hand it back, envgrid removes the new database and says that the key may still be stored; `envgrid key` prints it and `envgrid forget` removes it. If another envgrid process starts a database in the new folder while `envgrid init` runs, init leaves that file alone. Only one `envgrid init` or `envgrid forget` runs at a time for each user; a second one stops at once and changes nothing.

- Running `envgrid init` again checks that the database is still there and that the stored key still opens it, and leaves the setup as it is. If `ENVGRID_DATA_DIR` points to another folder, it says so.
- `envgrid key` prints the stored key, to make a recovery copy. It works even if the settings file is missing or damaged.
- `envgrid forget` removes the stored key and the settings file, both or neither, never the data. It needs a readable key store and asks you to paste the key first, so you cannot forget the only copy by accident. It moves the settings file aside, removes the key, checks that the key is gone, and only then deletes the file; if the key stays, the file is put back. It removes only the key you pasted, never another key stored meanwhile. If a forget is stopped part way, the next `envgrid init` or `envgrid forget` settles it by the key: while the key is still stored, the settings file goes back; once the key is gone, the set-aside file is deleted. Run `envgrid init` again afterwards to set up a different folder.

### Which setting wins

- An environment variable always wins over the saved setup, setting by setting, so Docker, services and production keep working exactly as before.
- The stored key, the address `127.0.0.1:8080` and the plain-HTTP cookies are used only for the setup's own data folder. If `ENVGRID_DATA_DIR` points elsewhere, that folder needs `ENVGRID_MASTER_KEY` and gets the usual defaults: address `:8080` and secure cookies.
- The saved setup turns secure cookies off only because it listens on this computer alone. If `ENVGRID_LISTEN` points envgrid at another address while the cookie setting still comes from the setup, envgrid refuses to start; set `ENVGRID_SECURE_COOKIES=true` behind HTTPS, or listen on `127.0.0.1`.

### What envgrid refuses to do

- It never makes a new key for a database that already exists. Without a key it stops and names the folder that holds the database.
- It never replaces a missing database with an empty one. If the database recorded by `envgrid init` is gone, for example because a drive is not connected, it stops and says how to restore it.
- It never opens an empty (0 byte) `envgrid.db`, wherever its key comes from, because SQLite would quietly turn it into a new, empty database. A file emptied by a failed copy or restore is reported instead.
- It never replaces a master key already stored for another setup, and never deletes a stored key it cannot confirm is the one it stored itself. On macOS it adds the Keychain item without the option to update an existing one, so the `security` tool refuses if an item already exists. On Linux, `secret-tool store` replaces an item whose attributes all match, so envgrid gives each key its own `key-id` attribute: storing one key can then never replace an item that holds another, even one a locked keyring did not show. Before storing, envgrid also confirms with `secret-tool search`, which lists locked items too and asks for no unlocking, that no envgrid item exists. Sources: the `add-generic-password` help text in SecurityTool/macOS/security.c, github.com/apple-oss-distributions/Security; tool/secret-tool.c and libsecret/secret-methods.c, gitlab.gnome.org/GNOME/libsecret (both read 25 October 2026).
- If the secure store is locked, unavailable or cannot be read, it stops without creating anything, and says how to unlock the store or to start with `ENVGRID_MASTER_KEY` from your recovery copy.
- It does not start a new database beside leftover `envgrid.db-wal`, `-shm` or `-journal` files, which suggest a database was moved away, and it never deletes them. This holds for `envgrid init` and for every start, whatever the key's source.
- If an `envgrid init` is interrupted after claiming its database file, the empty `envgrid.db` it leaves holds no data; envgrid says so. Delete it, and if `envgrid key` then prints a key, save it and run `envgrid forget`. Then run `envgrid init` again.

### Where the key is stored

| System | Store | What it protects against |
| --- | --- | --- |
| Windows | Windows data protection (DPAPI) for your account, in `%LOCALAPPDATA%\envgrid\master-key.dpapi` | Microsoft documents that typically only a user with the same logon credentials can decrypt it, usually only on the same computer, and that DPAPI adds an integrity check. Source: learn.microsoft.com, CryptProtectData (read 25 October 2026). The file and `setup.json` live in the local part of your profile, which Windows does not copy to other computers with a roaming profile, so they never arrive on a computer without the database. Source: "Windows uses the Local and LocalLow folders for application data that does not roam with the user", Managing Roaming User Data Deployment Guide, learn.microsoft.com/en-us/previous-versions/windows/it-pro/windows-vista/cc766489(v=ws.10), an archived page (read 25 October 2026). |
| macOS | Your default keychain, normally the login keychain, item `envgrid` with account `master-key` | `security add-generic-password` without a keychain name adds to the default keychain (SecurityTool/macOS/keychain_add.c). The keychain is encrypted and locks when you sign out. Apple's `security` tool creates the item, and an item trusts the program that created it, so other programs running as you can read the key through that tool without a prompt. Source: Apple's `security(1)` manual, apple-oss-distributions/Security (read 25 October 2026). |
| Linux | Your desktop keyring through the Secret Service (`secret-tool`, attributes `service envgrid item master-key`, plus `key-id`, the first 16 hex digits of the key's SHA-256, which tells keys apart without revealing them). The setup records its `key-id`, so envgrid always reads that key; if several envgrid keys are stored and none is named, it refuses rather than guess | The keyring is encrypted with your login password. `secret-tool` comes in `libsecret-tools` on Debian and Ubuntu and needs a running, unlocked keyring, which servers without a desktop session usually lack; use environment variables there. Source: secret-tool(1), manpages.debian.org (read 25 October 2026). |

In every case a program running as your user can read the key, just as it could read `ENVGRID_MASTER_KEY` from the environment. The key is never written in plain text, never kept in the data folder, and never passed on a command line, where other users could see it in the process list: the macOS and Linux tools receive it on standard input.

## Starting with environment variables

For a server, a service or Docker, set the variables instead of running `envgrid init`. In a terminal they last only for that terminal window.

Linux (Bash) and macOS (zsh or Bash):

```sh
envgrid genkey          # copy the key it prints, then save it securely
export ENVGRID_MASTER_KEY='paste-the-key-printed-above'
export ENVGRID_DATA_DIR="$HOME/.envgrid"
export ENVGRID_ADMIN_EMAIL='you@example.com'
export ENVGRID_ADMIN_PASSWORD='choose-a-password-of-at-least-8-characters'
export ENVGRID_SECURE_COOKIES=false
envgrid serve
```

Windows (PowerShell):

```powershell
envgrid genkey          # copy the key it prints, then save it securely
$env:ENVGRID_MASTER_KEY = 'paste-the-key-printed-above'
$env:ENVGRID_DATA_DIR = Join-Path $env:LOCALAPPDATA 'envgrid\data'
$env:ENVGRID_ADMIN_EMAIL = 'you@example.com'
$env:ENVGRID_ADMIN_PASSWORD = 'choose-a-password-of-at-least-8-characters'
$env:ENVGRID_SECURE_COOKIES = 'false'
envgrid serve
```

`ENVGRID_SECURE_COOKIES=false` is for plain HTTP on your own computer only. For a deployment behind HTTPS, leave it unset so the secure default stays on. A password typed into these commands can be saved in your shell's history; `envgrid init` avoids that by asking for it hidden.

## Command line client

The same binary reads values from a running envgrid with an API token. Create a token in Settings, then set the URL and token in the terminal where you will run the command.

Linux (Bash) and macOS (zsh or Bash):

```sh
export ENVGRID_URL=https://envgrid.example.com
export ENVGRID_TOKEN=egt_...
envgrid export --repo acme-shop --file backend.env --env prod > .env
envgrid export --repo acme-shop --file backend.env --env prod --format json
envgrid run --repo acme-shop --file backend.env --env prod -- node server.js
```

Windows (PowerShell):

```powershell
$env:ENVGRID_URL = 'https://envgrid.example.com'
$env:ENVGRID_TOKEN = 'egt_...'
envgrid export --repo acme-shop --file backend.env --env prod > .env
envgrid run --repo acme-shop --file backend.env --env prod -- node server.js
```

These variables apply only to the current terminal session.

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
