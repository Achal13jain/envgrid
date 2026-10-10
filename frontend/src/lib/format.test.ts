import { describe, expect, it } from "vitest";
import { filenameFromDisposition } from "./api";
import {
  collapseRepeats,
  MASK,
  activitySentence,
  changePoints,
  dayLabel,
  sortRows,
  greeting,
  hasEnded,
  cellView,
  convertible,
  countMissing,
  groupColor,
  groupLetter,
  rowDiffers,
  describeCell,
  displayUrl,
  diffSegments,
  exportFormats,
  inferFormat,
  preview,
  relativeTime,
} from "./format";
import type { AuditRecord, Cell } from "./types";

const cell = (over: Partial<Cell>): Cell => ({
  environmentId: 1,
  present: true,
  masked: false,
  value: "v",
  version: 1,
  updatedAt: "2026-01-01T00:00:00.000Z",
  updatedBy: { id: 1, name: "Ana Ruiz", email: "ana@example.com" },
  ...over,
});

describe("cellView", () => {
  it("shows missing cells as missing, even if a stale reveal exists", () => {
    expect(cellView(cell({ present: false, value: null }), "old")).toEqual({ kind: "missing", text: "missing" });
  });
  it("masks secrets with a fixed-width mask", () => {
    expect(cellView(cell({ masked: true, value: null }))).toEqual({ kind: "masked", text: MASK });
    expect(cellView(cell({ masked: true, value: null })).text).toHaveLength(8);
  });
  it("shows a revealed secret", () => {
    expect(cellView(cell({ masked: true, value: null }), "s3cret")).toEqual({ kind: "value", text: "s3cret" });
  });
  it("tells an empty string apart from missing", () => {
    expect(cellView(cell({ value: "" }))).toEqual({ kind: "empty", text: "empty" });
  });
  it("shows plain values", () => {
    expect(cellView(cell({ value: "8080" }))).toEqual({ kind: "value", text: "8080" });
  });
});

describe("relativeTime", () => {
  const now = new Date("2026-03-10T12:00:00Z");
  it.each([
    ["2026-03-10T11:59:30Z", "just now"],
    ["2026-03-10T11:55:00Z", "5 minutes ago"],
    ["2026-03-10T09:00:00Z", "3 hours ago"],
    ["2026-03-09T12:00:00Z", "yesterday"],
    ["2026-02-08T12:00:00Z", "last month"],
  ])("%s is %s", (iso, want) => {
    expect(relativeTime(iso, now)).toBe(want);
  });
  it("handles missing and invalid input", () => {
    expect(relativeTime(undefined, now)).toBe("never");
    expect(relativeTime("nope", now)).toBe("unknown time");
  });
});

describe("describeCell", () => {
  const now = new Date("2026-01-01T02:00:00Z");
  it("names who and when", () => {
    expect(describeCell(cell({ version: 3 }), now)).toBe("Updated 2 hours ago by Ana Ruiz (version 3).");
  });
  it("describes deletions and never-set cells", () => {
    expect(describeCell(cell({ present: false, version: 2 }), now)).toMatch(/^Deleted 2 hours ago/);
    expect(describeCell(cell({ present: false, version: 0, updatedBy: null }), now)).toBe("Never set.");
  });
  it("falls back to the email when the name is blank", () => {
    expect(describeCell(cell({ updatedBy: { id: 2, name: " ", email: "x@example.com" } }), now)).toContain("by x@example.com");
  });
});

describe("preview", () => {
  it("keeps the first line and counts the rest", () => {
    expect(preview("a\nb\nc")).toBe("a (+2 more lines)");
    expect(preview("a\nb")).toBe("a (+1 more line)");
    expect(preview("x".repeat(10), 5)).toBe("xxxx…");
  });
});

describe("diffSegments", () => {
  it("isolates the changed word", () => {
    expect(diffSegments("postgres://db.test:5432", "postgres://db.prod:5432")).toEqual({
      prefix: "postgres://db.",
      a: "test",
      b: "prod",
      suffix: ":5432",
    });
    // A shared trailing "t" must not split the word.
    expect(diffSegments("https://api.test.example", "https://api.uat.example")).toMatchObject({ a: "test", b: "uat", suffix: ".example" });
  });
  it("handles insertions and identical strings", () => {
    expect(diffSegments("a b c", "a bX c")).toEqual({ prefix: "a ", a: "b", b: "bX", suffix: " c" });
    expect(diffSegments("x=1,y=2", "x=1,y=2,z=3")).toEqual({ prefix: "x=1,y=", a: "2", b: "2,z=3", suffix: "" });
    expect(diffSegments("same", "same")).toEqual({ prefix: "same", a: "", b: "", suffix: "" });
  });
  it("reassembles both inputs", () => {
    for (const [a, b] of [["", "x"], ["xyz", ""], ["abcabc", "abc"], ["héllo", "hello"]]) {
      const d = diffSegments(a, b);
      expect(d.prefix + d.a + d.suffix).toBe(a);
      expect(d.prefix + d.b + d.suffix).toBe(b);
    }
  });
});

describe("misc", () => {
  it("lists the file's own format first and only raw for raw files", () => {
    expect(exportFormats("yaml")).toEqual(["yaml", "dotenv", "json", "properties", "ini", "csv"]);
    expect(exportFormats("raw")).toEqual(["raw"]);
    expect(exportFormats("php")).toEqual(["php"]);
  });
  it("converts only between text formats, like the server", () => {
    expect(convertible("json", "dotenv")).toBe(true);
    expect(convertible("ini", "yaml")).toBe(true);
    expect(convertible("php", "dotenv")).toBe(false);
    expect(convertible("dotenv", "raw")).toBe(false);
  });
  it("infers formats from file names", () => {
    expect(inferFormat(".env")).toBe("dotenv");
    expect(inferFormat(".env.local")).toBe("dotenv");
    expect(inferFormat("backend.env")).toBe("dotenv");
    expect(inferFormat("App.YML")).toBe("yaml");
    expect(inferFormat("application.properties")).toBe("properties");
    expect(inferFormat("Constants.PHP")).toBe("php");
    expect(inferFormat("settings.cfg")).toBe("ini");
    expect(inferFormat("notes.txt")).toBe("raw");
    expect(inferFormat("nginx.conf")).toBeNull();
  });
  it("counts missing cells", () => {
    expect(countMissing({ rows: [{ key: {} as never, cells: [cell({}), cell({ present: false }), cell({ present: false })] }] })).toBe(2);
  });
  it("parses Content-Disposition file names", () => {
    expect(filenameFromDisposition("attachment; filename=app.prod.env")).toBe("app.prod.env");
    expect(filenameFromDisposition('attachment; filename="my app.env"')).toBe("my app.env");
    expect(filenameFromDisposition("attachment; filename*=utf-8''caf%C3%A9.json")).toBe("café.json");
    expect(filenameFromDisposition(null)).toBeNull();
  });
});

describe("activitySentence", () => {
  it("reads value changes as verb, key and environment", () => {
    expect(activitySentence({ action: "update_value", detail: { key: "LOG_LEVEL", env: "prod", file: "app.env" } })).toEqual({
      verb: "changed",
      target: "LOG_LEVEL",
      where: "in prod",
    });
    expect(activitySentence({ action: "create_value", detail: { key: "__raw__", env: "uat" } }).target).toBe("the file body");
  });
  it("handles files, users, sign-ins and compare reveals", () => {
    expect(activitySentence({ action: "export_file", detail: { file: "app.env", env: "test" } })).toEqual({ verb: "exported", target: "app.env", where: "for test" });
    expect(activitySentence({ action: "user_created", detail: { email: "a@example.com" } }).target).toBe("a@example.com");
    expect(activitySentence({ action: "login", detail: {} })).toEqual({ verb: "signed in", target: "", where: "" });
    expect(activitySentence({ action: "import_file", detail: { file: "app.env", env: "uat", added: ["A", "B"], updated: ["C"] } })).toEqual({
      verb: "imported into",
      target: "app.env",
      where: "in uat, 2 added and 1 changed",
    });
    expect(activitySentence({ action: "reveal_secret", detail: { keys: ["A", "B"], file: "app.env", via: "compare" } })).toEqual({
      verb: "revealed",
      target: "2 secrets",
      where: "in app.env",
    });
  });
  it("folds repeated entries into one with a count", () => {
    const rec = (id: number, action: string) => ({ id, action, detail: {}, user: { id: 1, name: "A", email: "a@example.com" } }) as unknown as AuditRecord;
    const out = collapseRepeats([rec(3, "login"), rec(2, "login"), rec(1, "create_repo")]);
    expect(out.map((g) => [g.record.id, g.count])).toEqual([
      [3, 2],
      [1, 1],
    ]);
  });
  it("falls back for unknown actions", () => {
    expect(activitySentence({ action: "something_new", detail: {} }).verb).toBe("something new");
  });
});

describe("value groups", () => {
  it("flags keys whose present values differ", () => {
    expect(rowDiffers([{ present: true, group: 0 }, { present: true, group: 1 }])).toBe(true);
    expect(rowDiffers([{ present: true, group: 0 }, { present: true, group: 0 }])).toBe(false);
    expect(rowDiffers([{ present: true, group: 0 }, { present: false }])).toBe(false);
    expect(rowDiffers([])).toBe(false);
  });
  it("names and colours groups, folding past three into grey", () => {
    expect([0, 1, 2, 25].map(groupLetter)).toEqual(["A", "B", "C", "Z"]);
    expect(groupLetter(26)).toBe("27");
    expect(groupColor(0)).toBe("var(--diff-a)");
    expect(groupColor(5)).toBe("var(--diff-other)");
  });
});

describe("displayUrl", () => {
  it("drops the scheme, a trailing slash and .git", () => {
    expect(displayUrl("https://github.com/acme/app")).toBe("github.com/acme/app");
    expect(displayUrl("http://gitlab.example.com/team/app/")).toBe("gitlab.example.com/team/app");
    expect(displayUrl("https://github.com/acme/app.git")).toBe("github.com/acme/app");
  });
});

describe("dayLabel and hasEnded", () => {
  const now = new Date(2026, 9, 6, 9, 30);
  it("names today and yesterday in local time, and dates before that", () => {
    expect(dayLabel(new Date(2026, 9, 6, 0, 5).toISOString(), now)).toBe("Today");
    expect(dayLabel(new Date(2026, 9, 5, 23, 59).toISOString(), now)).toBe("Yesterday");
    const older = dayLabel(new Date(2026, 9, 1, 12).toISOString(), now);
    expect(older).not.toBe("Today");
    expect(older).toContain("2026");
  });
  it("treats no end date as never ending", () => {
    expect(hasEnded(null, now)).toBe(false);
    expect(hasEnded(new Date(2026, 9, 6, 9).toISOString(), now)).toBe(true);
    expect(hasEnded(new Date(2026, 9, 7).toISOString(), now)).toBe(false);
  });
});

describe("changePoints", () => {
  const rec = (id: number, action: string, createdAt: string) =>
    ({ id, action, createdAt, user: null, repoId: 1, fileId: 1, keyId: 1, environmentId: 1, detail: {} }) as AuditRecord;
  const log = [
    rec(6, "update_value", "2026-10-06T10:00:05.400Z"),
    rec(5, "approve_change", "2026-10-06T10:00:05.000Z"),
    rec(4, "reveal_secret", "2026-10-06T09:59:00.000Z"),
    rec(3, "update_value", "2026-10-06T09:00:00.000Z"),
    rec(2, "create_value", "2026-10-06T08:00:00.000Z"),
  ];
  it("groups records of one change, skips reads, and restores to a second before the first", () => {
    const points = changePoints(log, true);
    expect(points.map((p) => p.records.map((r) => r.id))).toEqual([[5, 6], [3], [2]]);
    expect(points[0].at).toBe("2026-10-06T10:00:04.000Z");
  });
  it("drops the oldest change when the log goes on past this page", () => {
    expect(changePoints(log, false).map((p) => p.id)).toEqual(["5", "3"]);
  });
});

describe("greeting", () => {
  it("follows the local hour", () => {
    expect(greeting(new Date(2026, 9, 6, 6))).toBe("Good morning");
    expect(greeting(new Date(2026, 9, 6, 12))).toBe("Good afternoon");
    expect(greeting(new Date(2026, 9, 6, 18))).toBe("Good evening");
    expect(greeting(new Date(2026, 9, 6, 2))).toBe("Good evening");
  });
});

describe("sortRows", () => {
  const row = (id: number, name: string, position: number, cells: [boolean, string?][]) => ({
    key: { id, name, position },
    cells: cells.map(([present, updatedAt]) => ({ present, updatedAt })),
  });
  const rows = [
    row(3, "b_key", 0, [[true, "2026-10-01T00:00:00Z"], [false]]),
    row(1, "A_KEY10", 1, [[true, "2026-10-05T00:00:00Z"], [true, "2026-09-01T00:00:00Z"]]),
    row(2, "a_key9", 2, [[false], [false]]),
  ];
  const ids = (s: Parameters<typeof sortRows>[1]) => sortRows(rows, s).map((r) => r.key.id);
  it("orders by file position, name, age, last change and gaps", () => {
    expect(ids("position")).toEqual([3, 1, 2]);
    expect(ids("name")).toEqual([2, 1, 3]);
    expect(ids("name-desc")).toEqual([3, 1, 2]);
    expect(ids("newest")).toEqual([3, 2, 1]);
    expect(ids("oldest")).toEqual([1, 2, 3]);
    expect(ids("changed")).toEqual([1, 3, 2]);
    expect(ids("changed-oldest")).toEqual([2, 3, 1]);
    expect(ids("gaps")).toEqual([2, 3, 1]);
  });
});
