# API

The UI uses a JSON API under `/api/v1`, which you can also call yourself with a session cookie or an API token (`Authorization: Bearer egt_...`). With a cookie, every request except `/api/v1/health` must send the header `X-Requested-With: envgrid`, reads included, because reveal, export and compare are reads that release values; with a token they need not, because other sites cannot send that header either. Errors always look like `{"error": {"code": "...", "message": "..."}}`, with 401 meaning "sign in" and 403 meaning "not allowed".

| Endpoint | Purpose |
| --- | --- |
| `POST /auth/login`, `POST /auth/logout`, `GET /auth/me`, `PATCH /auth/me` | Session and own profile |
| `GET/POST /auth/tokens`, `DELETE /auth/tokens/{id}` | Your API tokens; a new token is returned once. Tokens cannot create tokens |
| `GET /resolve?repo=&file=&env=` | Turn repo, file and environment names into ids, for scripts |
| `GET/POST /users`, `PATCH /users/{id}` | Users (admin only) |
| `GET/POST /repos`, `GET/PATCH/DELETE /repos/{id}` | Repos, with name, description and an optional repository URL |
| `GET/POST /repos/{id}/environments`, `POST /repos/{id}/environments/reorder`, `PATCH/DELETE /environments/{id}` | Environments |
| `GET/POST /repos/{id}/files`, `GET/PATCH/DELETE /files/{id}` | Files |
| `GET /files/{id}/grid` | Keys by environment, secrets masked, in one query |
| `GET /files/{id}/compare?a=&b=&reveal=` | Missing in A, missing in B, different and same |
| `GET /files/{id}/export?env=&format=&resolve=` | Download a file (audited). `format` may also be `csv`; references are filled in unless `resolve=false` |
| `GET /files/{id}/rollback?env=&at=`, `POST /files/{id}/rollback` | Preview, then restore one environment to a time (RFC 3339) |
| `POST /files/{id}/import?env=&dryRun=` | Import a file into one environment; content in another text format is converted. Returns added, updated and unchanged keys. With `dryRun=true` it only answers what would happen and stores nothing |
| `POST /formats/detect?name=` | Detect the format of an upload and list its key names (names only, nothing stored) |
| `GET/PUT /settings/audit` | Which kinds of activity are recorded (admins only); people, tokens and deletions always are |
| `GET /search?q=` | Repos, files and keys whose names contain the text, for the command palette (names only) |
| `POST /files/{id}/keys`, `POST /files/{id}/keys/reorder`, `PATCH/DELETE /keys/{id}` | Keys, with tags, and `required` and `pattern` rules (admins) |
| `PUT/DELETE /keys/{id}/values/{envId}` | Set or delete a value |
| `GET /keys/{id}/values/{envId}/reveal?version=&purpose=` | Plaintext (audited) |
| `GET /keys/{id}/values/{envId}/history` | Versions with who and when |
| `POST /keys/{id}/values/{envId}/restore`, `POST /keys/{id}/values/{envId}/copy` | Restore a version, copy from another environment |
| `POST /keys/{id}/values/{envId}/requests` | Propose setting or deleting a value, with a reason |
| `GET /change-requests?status=&repo=`, `GET /change-requests/count` | Requests, pending by default, and the number waiting |
| `GET /change-requests/{id}/reveal`, `POST /change-requests/{id}/approve`, `/reject`, `/cancel` | See a proposed secret (audited), decide (admins) or withdraw a request |
| `GET /insights?repo=&days=` | Stale secrets, reused secrets and rule problems, as locations only |
| `GET /audit?repo=&file=&env=&key=&user=&action=&since=&until=&limit=&cursor=` | Audit log, newest first; `since` and `until` are RFC 3339 times |
| `GET /health` | Liveness, no sign-in needed |
