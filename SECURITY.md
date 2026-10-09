# Security policy

envgrid holds secrets, so security reports are welcome and taken seriously.

## Reporting a vulnerability

Please do not open a public issue for a security problem. Instead, write to the maintainers privately at **jainachal38@gmail.com**, with:

- what you found and where (the endpoint, page or file),
- the steps to reproduce it, and
- what an attacker could do with it.

You will get an answer within a few working days. Once a fix is released, the report is credited in the changelog unless you ask otherwise.

## Supported versions

Security fixes are made for the latest release. Run the newest version, and back up `envgrid.db` and keep your master key before you upgrade.

## Who can do what

There are two roles. Every signed-in user can see every repo.

| Action | Member | Admin |
| --- | --- | --- |
| Read values, reveal secrets, copy, export, compare | yes | yes |
| Create repos, files, keys and unprotected environments | yes | yes |
| Change values in an unprotected environment | yes | yes |
| Change values in a protected environment (prod by default) | proposes a change for an admin to approve | yes |
| Approve or reject change requests | no | yes |
| Set a key's rules (required, pattern) | no | yes |
| Rename or delete a key that has a value in a protected environment | no | yes |
| Protect or unprotect an environment, delete environments, files or repos | no | yes |
| Manage users, set an end date for someone's access, and see account events in the audit log | no | yes |

Every new repo starts with `test`, `uat` and `prod`, and `prod` is protected. Add as many more as you need from the Environments box beside the file list. Protection is a setting per environment, and the server enforces it on every write, including imports and copies.

## Security model in plain language

- **Values are always encrypted.** Every value, secret or not, is encrypted with AES-256-GCM using your master key before it is written to the database. Each value gets its own random nonce, and each ciphertext is bound to its key and environment, so a value moved to another row in the database will not decrypt. The database alone is useless without the master key.
- **The master key never touches the disk.** envgrid reads it from the environment at start-up and checks it against a known value in the database, so a wrong key stops the server instead of producing garbage. envgrid never logs it.
- **Secrets stay masked until you ask.** The grid, history and compare views send secrets to the browser masked. The plaintext of a secret only leaves the server when someone reveals it, copies it, exports the file or compares with secrets shown, and each of those is written to the audit log first, unless an admin has turned off recording of reveals, copies and exports in Settings. Values that are not marked secret are shown in the grid. Editing a secret starts from an empty box, so changing it does not require revealing it.
- **Passwords** are hashed with argon2id (3 passes, 64 MiB, 2 lanes) and checked in constant time. Sign-in is limited to five attempts per minute per address, and IPv6 addresses count per /64. There is deliberately no per-account limit, because anyone who knows an email could then lock that person out; use long passwords.
- **Sessions** are random tokens in an HttpOnly, SameSite=Lax cookie that lasts 30 days. The database stores only a hash of each token. The cookie name has the `__Host-` prefix when Secure cookies are on. Signing out, a password change or disabling an account ends sessions immediately, and a password change also revokes the account's API tokens. Checking the current password is limited to five attempts a minute per account, and API tokens cannot change passwords or add people.
- **Cross-site requests** are refused: every API request except the health check, reads included, must carry a custom header that other sites cannot send, or an API token. The UI is served with a strict Content Security Policy that only allows its own scripts, and it cannot be framed.
- **Logs never contain values.** Request logs record the method, path, status, timing and user id, never bodies or query strings. A test pushes a known value through every path a value takes (set, reveal, copy, history, grid, compare, export, import) and through failing requests, and fails if the value appears in the logs.

What envgrid does not protect against: anyone who has both the database and the master key can read everything, and an admin can read every value. Run envgrid behind HTTPS, keep the master key separate from the database backups, and give the admin role to as few people as you can.
