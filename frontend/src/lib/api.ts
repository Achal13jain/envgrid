import type {
  APIToken,
  AuditCategory,
  AuditPage,
  ChangeRequest,
  Comparison,
  ConfigFile,
  Detection,
  Environment,
  ExportFormat,
  Format,
  Grid,
  History,
  ImportResult,
  Insights,
  Key,
  Repo,
  RequestStatus,
  Role,
  RollbackPreview,
  SearchHit,
  User,
} from "./types";

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message);
  }
}

let onUnauthorized: () => void = () => {};

/** Registers what happens when a session has expired mid-use. */
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn;
}

async function send(method: string, path: string, body?: unknown): Promise<Response> {
  // The custom header is the server's CSRF check, sent on every request, reads included.
  const headers: Record<string, string> = { "X-Requested-With": "envgrid" };
  let payload: string | undefined;
  if (typeof body === "string") {
    headers["Content-Type"] = "text/plain; charset=utf-8";
    payload = body;
  } else if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    payload = JSON.stringify(body);
  }
  const res = await fetch(`/api/v1${path}`, { method, headers, body: payload, credentials: "same-origin" });
  if (!res.ok) {
    let code = `http_${res.status}`;
    let message = res.statusText || "Request failed";
    try {
      const data = await res.json();
      code = data.error.code;
      message = data.error.message;
    } catch {
      /* not JSON */
    }
    if (res.status === 401 && path !== "/auth/login") onUnauthorized();
    throw new ApiError(res.status, code, message);
  }
  return res;
}

async function json<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await send(method, path, body);
  return (res.status === 204 ? undefined : await res.json()) as T;
}

const q = (params: Record<string, string | number | boolean | undefined | null>) => {
  const s = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== null && v !== "") s.set(k, String(v));
  const out = s.toString();
  return out ? `?${out}` : "";
};

const value = (keyId: number, envId: number) => `/keys/${keyId}/values/${envId}`;

export interface ExportedFile {
  name: string;
  body: string;
  type: string;
}

export const api = {
  login: (email: string, password: string) => json<User>("POST", "/auth/login", { email, password }),
  logout: () => json<void>("POST", "/auth/logout"),
  me: () => json<User>("GET", "/auth/me"),
  updateMe: (body: { name?: string; currentPassword?: string; newPassword?: string }) => json<User>("PATCH", "/auth/me", body),

  users: () => json<User[]>("GET", "/users"),
  createUser: (body: { email: string; name: string; password: string; role: Role }) => json<User>("POST", "/users", body),
  updateUser: (id: number, body: { name?: string; role?: Role; disabled?: boolean; password?: string; expiresAt?: string }) =>
    json<User>("PATCH", `/users/${id}`, body),

  repos: () => json<Repo[]>("GET", "/repos"),
  repo: (id: number) => json<Repo>("GET", `/repos/${id}`),
  createRepo: (body: { name: string; description: string; sourceUrl: string }) => json<Repo>("POST", "/repos", body),
  updateRepo: (id: number, body: { name?: string; description?: string; sourceUrl?: string }) => json<Repo>("PATCH", `/repos/${id}`, body),
  deleteRepo: (id: number) => json<void>("DELETE", `/repos/${id}`),

  environments: (repoId: number) => json<Environment[]>("GET", `/repos/${repoId}/environments`),
  createEnvironment: (repoId: number, body: { name: string; isProtected: boolean }) =>
    json<Environment>("POST", `/repos/${repoId}/environments`, body),
  updateEnvironment: (id: number, body: { name?: string; isProtected?: boolean }) =>
    json<Environment>("PATCH", `/environments/${id}`, body),
  deleteEnvironment: (id: number) => json<void>("DELETE", `/environments/${id}`),
  reorderEnvironments: (repoId: number, ids: number[]) =>
    json<Environment[]>("POST", `/repos/${repoId}/environments/reorder`, { ids }),

  files: (repoId: number) => json<ConfigFile[]>("GET", `/repos/${repoId}/files`),
  createFile: (repoId: number, body: { name: string; format: Format }) => json<ConfigFile>("POST", `/repos/${repoId}/files`, body),
  updateFile: (id: number, body: { name: string }) => json<ConfigFile>("PATCH", `/files/${id}`, body),
  deleteFile: (id: number) => json<void>("DELETE", `/files/${id}`),
  grid: (fileId: number) => json<Grid>("GET", `/files/${fileId}/grid`),
  compare: (fileId: number, a: number, b: number, reveal: boolean) =>
    json<Comparison>("GET", `/files/${fileId}/compare${q({ a, b, reveal: reveal || undefined })}`),
  /** Guesses the format of an upload; returns key names only. */
  detect: (body: string, name: string) => json<Detection>("POST", `/formats/detect${q({ name })}`, body),
  /** With dryRun, answers what the import would do and stores nothing. */
  importFile: (fileId: number, envId: number, body: string, dryRun = false) =>
    json<ImportResult>("POST", `/files/${fileId}/import${q({ env: envId, dryRun: dryRun || undefined })}`, body),
  /** Audited on the server as export_file. */
  exportFile: async (fileId: number, envId: number, format: ExportFormat): Promise<ExportedFile> => {
    const res = await send("GET", `/files/${fileId}/export${q({ env: envId, format })}`);
    return {
      name: filenameFromDisposition(res.headers.get("Content-Disposition")) ?? "export.txt",
      type: res.headers.get("Content-Type") ?? "text/plain",
      body: await res.text(),
    };
  },

  createKey: (fileId: number, body: { name: string; description: string; isSecret: boolean }) =>
    json<Key>("POST", `/files/${fileId}/keys`, body),
  updateKey: (
    id: number,
    body: { name?: string; description?: string; isSecret?: boolean; required?: boolean; pattern?: string; tags?: string[] },
  ) =>
    json<Key>("PATCH", `/keys/${id}`, body),
  deleteKey: (id: number) => json<void>("DELETE", `/keys/${id}`),
  reorderKeys: (fileId: number, ids: number[]) => json<void>("POST", `/files/${fileId}/keys/reorder`, { ids }),

  setValue: (keyId: number, envId: number, v: string) =>
    json<{ changed: boolean; version: number }>("PUT", value(keyId, envId), { value: v }),
  deleteValue: (keyId: number, envId: number) => json<{ changed: boolean; version: number }>("DELETE", value(keyId, envId)),
  /** Plaintext; audited as reveal_secret, or copy_value when purpose is "copy". */
  reveal: (keyId: number, envId: number, opts: { version?: number; purpose?: "copy" } = {}) =>
    json<{ value: string }>("GET", `${value(keyId, envId)}/reveal${q(opts)}`),
  history: (keyId: number, envId: number) => json<History>("GET", `${value(keyId, envId)}/history`),
  restore: (keyId: number, envId: number, version: number) =>
    json<{ changed: boolean; version: number }>("POST", `${value(keyId, envId)}/restore`, { version }),
  copyValue: (keyId: number, toEnvId: number, fromEnvironmentId: number) =>
    json<{ changed: boolean; version: number }>("POST", `${value(keyId, toEnvId)}/copy`, { fromEnvironmentId }),

  /** Proposes a change to one value; an admin approves it. */
  requestChange: (keyId: number, envId: number, body: { value?: string; delete?: boolean; reason: string }) =>
    json<ChangeRequest>("POST", `${value(keyId, envId)}/requests`, body),
  changeRequests: (params: { status?: RequestStatus | "all"; repo?: number }) =>
    json<ChangeRequest[]>("GET", `/change-requests${q(params)}`),
  pendingCount: () => json<{ pending: number }>("GET", "/change-requests/count"),
  /** Audited as reveal_secret. */
  revealRequest: (id: number) => json<{ value: string }>("GET", `/change-requests/${id}/reveal`),
  approveRequest: (id: number, note: string) => json<ChangeRequest>("POST", `/change-requests/${id}/approve`, { note }),
  rejectRequest: (id: number, note: string) => json<ChangeRequest>("POST", `/change-requests/${id}/reject`, { note }),
  cancelRequest: (id: number) => json<void>("POST", `/change-requests/${id}/cancel`),

  rollbackPreview: (fileId: number, envId: number, at: string) =>
    json<RollbackPreview>("GET", `/files/${fileId}/rollback${q({ env: envId, at })}`),
  rollback: (fileId: number, envId: number, at: string) =>
    json<{ changed: string[] | null }>("POST", `/files/${fileId}/rollback`, { env: envId, at }),

  insights: (params: { repo?: number; days?: number }) => json<Insights>("GET", `/insights${q(params)}`),
  auditSettings: () => json<AuditCategory[]>("GET", "/settings/audit"),
  /** Admins only: the categories not to record. */
  setAuditSettings: (off: string[]) => json<AuditCategory[]>("PUT", "/settings/audit", { off }),
  /** Repos, files and keys by name, for the command palette. */
  search: (text: string) => json<SearchHit[]>("GET", `/search${q({ q: text })}`),

  tokens: () => json<APIToken[]>("GET", "/auth/tokens"),
  /** The token is in the answer once; only its hash is stored. */
  createToken: (name: string, expiresInDays: number) =>
    json<{ token: string; info: APIToken }>("POST", "/auth/tokens", { name, expiresInDays }),
  deleteToken: (id: number) => json<void>("DELETE", `/auth/tokens/${id}`),
  /** Admins: another person's tokens, and revoking one of them. */
  userTokens: (userId: number) => json<APIToken[]>("GET", `/users/${userId}/tokens`),
  deleteUserToken: (userId: number, id: number) => json<void>("DELETE", `/users/${userId}/tokens/${id}`),

  audit: (params: {
    repo?: number;
    file?: number;
    env?: number;
    key?: number;
    user?: number;
    action?: string;
    since?: string;
    until?: string;
    cursor?: number;
    limit?: number;
  }) =>
    json<AuditPage>("GET", `/audit${q(params)}`),
};

/** Reads the filename from a Content-Disposition header. */
export function filenameFromDisposition(header: string | null): string | null {
  if (!header) return null;
  const star = /filename\*=(?:UTF-8|utf-8)''([^;]+)/.exec(header);
  if (star) {
    try {
      return decodeURIComponent(star[1]);
    } catch {
      return null;
    }
  }
  const plain = /filename="?([^";]+)"?/.exec(header);
  return plain ? plain[1] : null;
}
