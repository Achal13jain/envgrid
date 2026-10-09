export type Role = "admin" | "member";

export interface User {
  id: number;
  email: string;
  name: string;
  role: Role;
  disabled: boolean;
  createdAt: string;
  /** When access ends; null for no end date. */
  expiresAt: string | null;
  /** When the user last signed in; null if never. */
  lastSignedInAt: string | null;
}

export interface UserRef {
  id: number;
  name: string;
  email: string;
}

export interface Repo {
  id: number;
  name: string;
  description: string;
  /** Optional link to the code repository; empty when not set. */
  sourceUrl: string;
  createdBy: number;
  createdAt: string;
  fileCount: number;
  envCount: number;
  keyCount: number;
  lastChangeAt: string;
  /** Per environment, in display order: how many keys have a value there. */
  coverage: EnvCoverage[];
}

export interface EnvCoverage {
  environmentId: number;
  name: string;
  isProtected: boolean;
  present: number;
}

export interface Environment {
  id: number;
  repoId: number;
  name: string;
  position: number;
  isProtected: boolean;
}

export type Format = "dotenv" | "json" | "yaml" | "properties" | "ini" | "php" | "raw";

/** Formats for export and import: every file format plus CSV, which is never a file's own format. */
export type ExportFormat = Format | "csv";

export interface ConfigFile {
  id: number;
  repoId: number;
  name: string;
  format: Format;
  createdAt: string;
  keyCount: number;
}

export interface Key {
  id: number;
  fileId: number;
  name: string;
  description: string;
  isSecret: boolean;
  position: number;
  /** Every environment must have a value (admins set this). */
  required: boolean;
  /** A regular expression every value must match; empty for none. */
  pattern: string;
  tags: string[];
}

/** A cell as the server sends it: value is null when missing or masked. */
export interface Cell {
  environmentId: number;
  present: boolean;
  masked: boolean;
  value: string | null;
  version: number;
  updatedAt?: string;
  updatedBy: UserRef | null;
  /** Distinct values of the key across environments, numbered left to right; absent when missing. */
  group?: number;
  /** Set when the value breaks the key's rules. */
  problem?: "required" | "pattern";
  /** Number of change requests waiting for this cell. */
  pending?: number;
}

export interface GridRow {
  key: Key;
  cells: Cell[];
}

export interface Grid {
  file: ConfigFile;
  environments: Environment[];
  rows: GridRow[];
}

export interface CompareItem {
  key: Key;
  a: Cell;
  b: Cell;
}

export interface Comparison {
  a: Environment;
  b: Environment;
  missingInA: CompareItem[];
  missingInB: CompareItem[];
  different: CompareItem[];
  same: CompareItem[];
}

export interface HistoryVersion {
  version: number;
  present: boolean;
  masked: boolean;
  value: string | null;
  createdAt: string;
  createdBy: UserRef | null;
}

export interface History {
  key: Key;
  environment: Environment;
  versions: HistoryVersion[];
}

export interface ImportResult {
  added: string[];
  updated: string[];
  unchanged: string[];
  keysCreated: string[];
  /** Set when the content was read in another format than the file's own. */
  readAs?: ExportFormat;
}

export interface Detection {
  format: ExportFormat;
  label: string;
  keys: number;
  sample: string[];
  error?: string;
}

export interface AuditRecord {
  id: number;
  action: string;
  user: UserRef | null;
  repoId: number | null;
  fileId: number | null;
  keyId: number | null;
  environmentId: number | null;
  detail: Record<string, unknown>;
  createdAt: string;
}

export interface AuditPage {
  items: AuditRecord[];
  nextCursor: number | null;
}

export type RequestStatus = "pending" | "approved" | "rejected" | "cancelled";

export interface ChangeRequest {
  id: number;
  repoId: number;
  repoName: string;
  fileId: number;
  fileName: string;
  keyId: number;
  keyName: string;
  isSecret: boolean;
  environmentId: number;
  environmentName: string;
  /** True when the request proposes deleting the value. */
  delete: boolean;
  /** The proposed value; null for a deletion or a masked secret. */
  proposed: string | null;
  masked: boolean;
  current: Cell;
  baseVersion: number;
  reason: string;
  status: RequestStatus;
  createdBy: UserRef | null;
  createdAt: string;
  decidedBy: UserRef | null;
  decidedAt?: string;
  decisionNote: string;
}

export interface RollbackChange {
  key: Key;
  current: Cell;
  then: Cell;
}

export interface RollbackPreview {
  at: string;
  environment: Environment;
  changes: RollbackChange[];
}

/** One value's location in an insights report; never the value itself. */
export interface InsightPlace {
  repoId: number;
  repoName: string;
  fileId: number;
  fileName: string;
  keyId: number;
  key: string;
  environmentId: number;
  environment: string;
  updatedAt?: string;
  updatedBy?: UserRef;
  problem?: "required" | "pattern";
}

export interface Insights {
  days: number;
  stale: InsightPlace[];
  /** Groups of places that hold the same secret value. */
  reused: InsightPlace[][];
  problems: InsightPlace[];
  pendingRequests: number;
}

export interface APIToken {
  id: number;
  name: string;
  prefix: string;
  createdAt: string;
  expiresAt: string | null;
  lastUsedAt: string | null;
}

/** A repo, file or key whose name matches a search. Names only, never values. */
export interface SearchHit {
  kind: "repo" | "file" | "key";
  id: number;
  name: string;
  repoId: number;
  repoName: string;
  fileId?: number;
  fileName?: string;
}

/** A kind of activity the log can record; locked kinds are always recorded. */
export interface AuditCategory {
  id: string;
  label: string;
  description: string;
  locked: boolean;
  enabled: boolean;
}
