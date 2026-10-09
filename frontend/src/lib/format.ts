import type { AuditRecord, Cell, ExportFormat, Format, Grid, UserRef } from "./types";

/** Fixed width so the mask never hints at the secret's length. */
export const MASK = "••••••••";

export type CellView =
  | { kind: "missing"; text: string }
  | { kind: "masked"; text: string }
  | { kind: "empty"; text: string }
  | { kind: "value"; text: string };

/**
 * What a cell shows. A secret shows its mask unless a plaintext was revealed
 * for it; an empty string is a value, distinct from missing.
 */
export function cellView(cell: Pick<Cell, "present" | "masked" | "value">, revealed?: string): CellView {
  if (!cell.present) return { kind: "missing", text: "missing" };
  const v = revealed ?? (cell.masked ? null : cell.value);
  if (v === null || v === undefined) return { kind: "masked", text: MASK };
  if (v === "") return { kind: "empty", text: "empty" };
  return { kind: "value", text: v };
}

/** One-line preview of a value for compact layouts. */
export function preview(value: string, max = 120): string {
  const lines = value.split("\n");
  let out = lines[0];
  if (out.length > max) out = `${out.slice(0, max - 1)}…`;
  if (lines.length > 1) out += ` (+${lines.length - 1} more line${lines.length > 2 ? "s" : ""})`;
  return out;
}

const units: [Intl.RelativeTimeFormatUnit, number][] = [
  ["year", 365 * 24 * 3600],
  ["month", 30 * 24 * 3600],
  ["week", 7 * 24 * 3600],
  ["day", 24 * 3600],
  ["hour", 3600],
  ["minute", 60],
];

const rtf = new Intl.RelativeTimeFormat("en", { numeric: "auto" });

/** "5 minutes ago", "yesterday", "just now". */
export function relativeTime(iso: string | undefined | null, now: Date = new Date()): string {
  if (!iso) return "never";
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "unknown time";
  const seconds = Math.round((t - now.getTime()) / 1000);
  if (Math.abs(seconds) < 45) return "just now";
  for (const [unit, size] of units) {
    if (Math.abs(seconds) >= size || unit === "minute") return rtf.format(Math.round(seconds / size), unit);
  }
  return "just now";
}

export function personName(u: UserRef | null | undefined): string {
  if (!u) return "a removed user";
  return u.name.trim() || u.email;
}

/** The who/when line for a cell, as a sentence. */
export function describeCell(cell: Cell, now: Date = new Date()): string {
  if (cell.version === 0) return "Never set.";
  const when = relativeTime(cell.updatedAt, now);
  const who = personName(cell.updatedBy);
  const verb = cell.present ? "Updated" : "Deleted";
  return `${verb} ${when} by ${who} (version ${cell.version}).`;
}

/** Formats whose values are plain text, so they convert into each other. */
const textFormats: ExportFormat[] = ["dotenv", "json", "yaml", "properties", "ini", "csv"];

/** Mirrors the server: content in one text format imports into another. */
export function convertible(from: ExportFormat, to: ExportFormat): boolean {
  return textFormats.includes(from) && textFormats.includes(to);
}

/** Export formats offered for a file, its own format first. PHP and plain text only export as themselves. */
export function exportFormats(format: Format): ExportFormat[] {
  if (!textFormats.includes(format)) return [format];
  return [format, ...textFormats.filter((f) => f !== format)];
}

/** Guesses a format from a file name, or null when the name says nothing. */
export function inferFormat(name: string): Format | null {
  const n = name.trim().toLowerCase();
  if (n === ".env" || n.endsWith(".env") || n.startsWith(".env.")) return "dotenv";
  if (n.endsWith(".json")) return "json";
  if (n.endsWith(".yaml") || n.endsWith(".yml")) return "yaml";
  if (n.endsWith(".properties")) return "properties";
  if (n.endsWith(".ini") || n.endsWith(".cfg")) return "ini";
  if (n.endsWith(".php")) return "php";
  if (n.endsWith(".txt")) return "raw";
  return null;
}

export const formatLabel: Record<ExportFormat, string> = {
  dotenv: ".env",
  json: "JSON",
  yaml: "YAML",
  properties: ".properties",
  ini: "INI",
  php: "PHP",
  raw: "plain text",
  csv: "CSV",
};

/** Longer names for format pickers. */
export const formatOptions: [Format, string][] = [
  ["dotenv", ".env (KEY=value)"],
  ["json", "JSON"],
  ["yaml", "YAML"],
  ["properties", ".properties (Java)"],
  ["ini", "INI ([section] key = value)"],
  ["php", "PHP constants (const NAME = value;)"],
  ["raw", "Plain text (kept whole)"],
];

export function countMissing(grid: Pick<Grid, "rows">): number {
  return grid.rows.reduce((n, r) => n + r.cells.filter((c) => !c.present).length, 0);
}

const wordChar = /[\p{L}\p{N}_]/u;

/**
 * Splits two strings into a shared prefix, the differing middles and a
 * shared suffix, for side-by-side highlighting. The middles are widened to
 * whole words, so "api.test.example" against "api.uat.example" marks
 * "test" and "uat" rather than "tes" and "ua".
 */
export function diffSegments(a: string, b: string) {
  let start = 0;
  while (start < a.length && start < b.length && a[start] === b[start]) start++;
  let end = 0;
  while (end < a.length - start && end < b.length - start && a[a.length - 1 - end] === b[b.length - 1 - end]) end++;
  if (a !== b) {
    while (start > 0 && wordChar.test(a[start - 1])) start--;
    while (end > 0 && wordChar.test(a[a.length - end])) end--;
  }
  return {
    prefix: a.slice(0, start),
    a: a.slice(start, a.length - end),
    b: b.slice(start, b.length - end),
    suffix: a.slice(a.length - end),
  };
}

const actionLabels: Record<string, string> = {
  login: "Signed in",
  login_failed: "Failed sign-in",
  password_check_failed: "Wrong current password",
  user_created: "Created user",
  update_user: "Updated user",
  create_repo: "Created repo",
  update_repo: "Updated repo",
  delete_repo: "Deleted repo",
  create_environment: "Created environment",
  update_environment: "Updated environment",
  delete_environment: "Deleted environment",
  reorder_environments: "Reordered environments",
  create_file: "Created file",
  update_file: "Renamed file",
  delete_file: "Deleted file",
  create_key: "Created key",
  update_key: "Updated key",
  delete_key: "Deleted key",
  reorder_keys: "Reordered keys",
  create_value: "Set value",
  update_value: "Changed value",
  delete_value: "Deleted value",
  reveal_secret: "Revealed secret",
  export_file: "Exported file",
  copy_value: "Copied value",
  request_change: "Requested change",
  approve_change: "Approved change",
  reject_change: "Rejected change",
  cancel_change: "Cancelled request",
  restore_environment: "Restored environment",
  import_file: "Imported file",
  create_token: "Created API token",
  delete_token: "Revoked API token",
  update_audit_settings: "Changed what is recorded",
};

export const auditActions = Object.keys(actionLabels);

export function actionLabel(action: string): string {
  return actionLabels[action] ?? action.replace(/_/g, " ");
}

const verbs: Record<string, string> = {
  login: "signed in",
  login_failed: "failed to sign in as",
  password_check_failed: "entered a wrong current password",
  user_created: "added user",
  update_user: "updated user",
  create_repo: "created repo",
  update_repo: "updated repo",
  delete_repo: "deleted repo",
  create_environment: "added environment",
  update_environment: "updated environment",
  delete_environment: "deleted environment",
  reorder_environments: "reordered the environments of",
  create_file: "created file",
  update_file: "renamed file",
  delete_file: "deleted file",
  create_key: "added key",
  update_key: "updated key",
  delete_key: "deleted key",
  reorder_keys: "reordered the keys of",
  create_value: "set",
  update_value: "changed",
  delete_value: "deleted the value of",
  reveal_secret: "revealed",
  export_file: "exported",
  copy_value: "copied",
  request_change: "asked to change",
  approve_change: "approved the change to",
  reject_change: "rejected the change to",
  cancel_change: "cancelled the request for",
  restore_environment: "restored",
  import_file: "imported into",
  create_token: "created an API token",
  delete_token: "revoked an API token",
  update_audit_settings: "changed what the activity log records",
};

/** An audit entry as a short sentence: verb, what it acted on, and where. */
export function activitySentence(r: Pick<AuditRecord, "action" | "detail">): { verb: string; target: string; where: string } {
  const d = r.detail as Record<string, unknown>;
  const str = (k: string) => (typeof d[k] === "string" ? (d[k] as string) : "");
  const key = str("key") === "__raw__" ? "the file body" : str("key");
  const verb = verbs[r.action] ?? r.action.replace(/_/g, " ");
  switch (r.action) {
    case "login":
    case "password_check_failed":
    case "create_token":
    case "delete_token":
    case "update_audit_settings":
      return { verb, target: "", where: "" };
    case "restore_environment":
      return { verb, target: str("file"), where: `in ${str("env")} to ${localStamp(str("restoredTo"))}` };
    case "import_file": {
      const n = (k: string) => (Array.isArray(d[k]) ? d[k].length : 0);
      return { verb, target: str("file"), where: `in ${str("env")}, ${n("added")} added and ${n("updated")} changed` };
    }
    case "login_failed":
      return { verb, target: str("email"), where: str("ip") ? `from ${str("ip")}` : "" };
    case "user_created":
    case "update_user":
      return { verb, target: str("email"), where: "" };
    case "create_repo":
    case "update_repo":
    case "delete_repo":
    case "reorder_environments":
      return { verb, target: str("repo"), where: "" };
    case "create_environment":
    case "update_environment":
    case "delete_environment":
      return { verb, target: str("env"), where: "" };
    case "create_file":
    case "update_file":
    case "delete_file":
    case "reorder_keys":
      return { verb, target: str("file"), where: "" };
    case "export_file":
      return { verb, target: str("file"), where: str("env") ? `for ${str("env")}` : "" };
    case "reveal_secret":
      if (Array.isArray(d.keys)) {
        const n = d.keys.length;
        return { verb, target: `${n} ${n === 1 ? "secret" : "secrets"}`, where: str("file") ? `in ${str("file")}` : "" };
      }
      return { verb, target: key, where: str("env") ? `in ${str("env")}` : "" };
    case "create_key":
    case "update_key":
    case "delete_key":
      return { verb, target: key, where: str("file") ? `in ${str("file")}` : "" };
  }
  return { verb, target: key || str("file"), where: str("env") ? `in ${str("env")}` : "" };
}

/** An ISO time as a short local date and time, or the text as given when it is not a time. */
function localStamp(iso: string): string {
  const t = new Date(iso);
  return Number.isNaN(t.getTime()) ? iso : t.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

/** True when a key holds more than one distinct value across environments. */
export function rowDiffers(cells: Pick<Cell, "present" | "group">[]): boolean {
  return new Set(cells.filter((c) => c.present).map((c) => c.group)).size > 1;
}

/** "A" for the first distinct value, "B" for the next, and so on. */
export function groupLetter(group: number): string {
  return group < 26 ? String.fromCharCode(65 + group) : String(group + 1);
}

/** CSS colour for a value group: three validated hues, then a neutral grey. */
export function groupColor(group: number): string {
  return ["var(--diff-a)", "var(--diff-b)", "var(--diff-c)"][group] ?? "var(--diff-other)";
}

/** A repository URL as people read it: "github.com/acme/app". */
export function displayUrl(url: string): string {
  return url.replace(/^https?:[/][/]/, "").replace(/[/]$/, "").replace(/[.]git$/, "");
}

/** True when an end date has passed; null means no end date. */
export function hasEnded(iso: string | null | undefined, now: Date = new Date()): boolean {
  return !!iso && Date.parse(iso) < now.getTime();
}

/** "Today", "Yesterday" or a short date, in local time, for grouping entries by day. */
export function dayLabel(iso: string, now: Date = new Date()): string {
  const d = new Date(iso);
  const start = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  // Rounded, so a 23 or 25 hour day around a clock change still counts as one.
  const days = Math.round((start(now) - start(d)) / 86_400_000);
  if (days === 0) return "Today";
  if (days === 1) return "Yesterday";
  return d.toLocaleDateString(undefined, { weekday: "short", day: "numeric", month: "short", year: "numeric" });
}

/** A key's display name: a plain text file's single body key reads as "File body". */
export function keyLabel(k: { name: string }): string {
  return k.name === "__raw__" ? "File body" : k.name;
}

/** Audit actions that write values. Reveals, copies to the clipboard and exports change nothing. */
const changeActions = new Set(["create_value", "update_value", "delete_value", "approve_change", "restore_environment", "import_file"]);

/**
 * Audit records closer together than this are one change: an import, an
 * approval or a restore writes several. The server stamps each record just
 * after it writes the value, so "just before" is this long before the first.
 * ponytail: assumes a write is recorded within a second; exact if audit
 * records carried the time of the version they wrote.
 */
const ONE_CHANGE_MS = 1000;

export interface ChangePoint {
  id: string;
  /** The instant to restore to, as ISO. */
  at: string;
  /** The audit records of the change, oldest first. */
  records: AuditRecord[];
}

/** Groups the audit log, newest first, into changes a person can restore to just before. */
export function changePoints(items: AuditRecord[], complete: boolean): ChangePoint[] {
  const groups: AuditRecord[][] = [];
  for (const r of items) {
    if (!changeActions.has(r.action)) continue;
    const last = groups.at(-1);
    if (last && Date.parse(last[0].createdAt) - Date.parse(r.createdAt) < ONE_CHANGE_MS) last.unshift(r);
    else groups.push([r]);
  }
  // The oldest change may go on past this page of the log, so its start is unknown.
  if (!complete) groups.pop();
  return groups.map((records) => ({
    id: String(records[0].id),
    at: new Date(Date.parse(records[0].createdAt) - ONE_CHANGE_MS).toISOString(),
    records,
  }));
}

/** Folds runs of the same person doing the same thing (three sign-ins in a row) into one entry with a count. */
export function collapseRepeats(items: AuditRecord[]): { record: AuditRecord; count: number }[] {
  const out: { record: AuditRecord; count: number; key: string }[] = [];
  for (const r of items) {
    const key = `${r.user?.id ?? ""}|${r.action}|${JSON.stringify(activitySentence(r))}`;
    const last = out.at(-1);
    if (last?.key === key) last.count++;
    else out.push({ record: r, count: 1, key });
  }
  return out.map(({ record, count }) => ({ record, count }));
}

/** "Good morning", "Good afternoon" or "Good evening", by the local hour. */
export function greeting(now: Date = new Date()): string {
  const h = now.getHours();
  return h >= 5 && h < 12 ? "Good morning" : h >= 12 && h < 18 ? "Good afternoon" : "Good evening";
}

/** Orders for the grid's rows. "position" is the file's own order, the one export uses. */
export type KeySort = "position" | "name" | "name-desc" | "newest" | "oldest" | "changed" | "changed-oldest" | "gaps";

export const keySorts: { value: KeySort; label: string }[] = [
  { value: "position", label: "File order" },
  { value: "name", label: "Name, A to Z" },
  { value: "name-desc", label: "Name, Z to A" },
  { value: "newest", label: "Newest keys first" },
  { value: "oldest", label: "Oldest keys first" },
  { value: "changed", label: "Recently changed first" },
  { value: "changed-oldest", label: "Least recently changed first" },
  { value: "gaps", label: "Most missing values first" },
];

type SortableRow = { key: { id: number; name: string; position: number }; cells: { present: boolean; updatedAt?: string }[] };

const lastChange = (r: SortableRow) => r.cells.reduce((m, c) => (c.updatedAt && c.updatedAt > m ? c.updatedAt : m), "");
const byName = new Intl.Collator(undefined, { numeric: true, sensitivity: "base" });

/**
 * Rows in the chosen order; ties keep the file order. Key ids grow as keys
 * are created, so they give the order keys were added.
 */
export function sortRows<T extends SortableRow>(rows: T[], sort: KeySort): T[] {
  const out = [...rows].sort((a, b) => a.key.position - b.key.position || a.key.id - b.key.id);
  const gaps = (r: SortableRow) => r.cells.filter((c) => !c.present).length;
  switch (sort) {
    case "name":
      return out.sort((a, b) => byName.compare(a.key.name, b.key.name));
    case "name-desc":
      return out.sort((a, b) => byName.compare(b.key.name, a.key.name));
    case "newest":
      return out.sort((a, b) => b.key.id - a.key.id);
    case "oldest":
      return out.sort((a, b) => a.key.id - b.key.id);
    case "changed":
      return out.sort((a, b) => lastChange(b).localeCompare(lastChange(a)));
    case "changed-oldest":
      return out.sort((a, b) => lastChange(a).localeCompare(lastChange(b)));
    case "gaps":
      return out.sort((a, b) => gaps(b) - gaps(a));
  }
  return out;
}
