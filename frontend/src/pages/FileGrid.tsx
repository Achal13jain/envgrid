import { useEffect, useMemo, useReducer, useRef, useState, type CSSProperties, type FormEvent, type KeyboardEvent as ReactKeyboardEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowDown,
  ArrowUp,
  CircleSlash,
  Columns2,
  Copy,
  Download,
  Eye,
  EyeOff,
  FileUp,
  GitPullRequestArrow,
  GripVertical,
  History,
  Info,
  KeyRound,
  Link2,
  Lock,
  MoreHorizontal,
  Pencil,
  Plus,
  RotateCcw,
  Search,
  Trash2,
  TriangleAlert,
} from "lucide-react";
import { useNavigate, useParams, useSearchParams } from "react-router";
import { useMe } from "@/App";
import { HistoryDrawer } from "@/components/HistoryDrawer";
import { ImportDialog } from "@/components/ImportDialog";
import { ProposeDialog } from "@/components/ProposeDialog";
import { RestoreDialog } from "@/components/RestoreDialog";
import { useDocumentTitle } from "@/components/PageHeader";
import { AddEnvironmentDialog } from "@/pages/Repo";
import {
  Badge,
  Breakable,
  Button,
  ConfirmDialog,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
  EmptyState,
  ErrorNotice,
  Field,
  Input,
  Kbd,
  Loading,
  Menu,
  MenuContent,
  MenuItem,
  MenuLabel,
  MenuSeparator,
  MenuTrigger,
  Panel,
  Select,
} from "@/components/ui";
import { api } from "@/lib/api";
import { errorMessage, useAnnounce } from "@/lib/announce";
import {
  cellView,
  countMissing,
  describeCell,
  exportFormats,
  formatLabel,
  groupColor,
  groupLetter,
  keyLabel,
  keySorts,
  rowDiffers,
  sortRows,
  type KeySort,
} from "@/lib/format";
import { gridReducer, initialGridState, keyToIntent, type GridCommand } from "@/lib/gridNav";
import type { Cell, Environment, ExportFormat, Grid, Key, User } from "@/lib/types";
import { cn, copyText, downloadText, isTyping } from "@/lib/utils";

export function FileGridPage() {
  const fileId = Number(useParams().fileId);
  const user = useMe().data!;
  const grid = useQuery({ queryKey: ["grid", fileId], queryFn: () => api.grid(fileId) });
  if (grid.isError) return <ErrorNotice error={grid.error} />;
  if (!grid.data) return <Loading />;
  return <FileGrid key={fileId} grid={grid.data} user={user} />;
}

type Pending =
  | { kind: "addKey" }
  | { kind: "editKey"; key: Key }
  | { kind: "deleteKey"; key: Key }
  | { kind: "deleteValue"; key: Key; env: Environment }
  | { kind: "import"; envId: number | null }
  | { kind: "history"; key: Key; env: Environment }
  | { kind: "renameFile" }
  | { kind: "addEnv" }
  | { kind: "deleteFile" }
  | { kind: "propose"; key: Key; env: Environment; cell: Cell; remove: boolean }
  | { kind: "restore"; env: Environment }
  | null;

const REVEAL_MS = 10_000;


function FileGrid({ grid, user }: { grid: Grid; user: User }) {
  const { file, environments: envs } = grid;
  useDocumentTitle(file.name);
  const isAdmin = user.role === "admin";
  const isRaw = file.format === "raw";
  const announce = useAnnounce();
  const navigate = useNavigate();
  const qc = useQueryClient();

  const [filter, setFilter] = useState("");
  const [show, setShow] = useState<"all" | "attention" | "different" | "missing" | "rules">("all");
  const [tag, setTag] = useState("");
  const [sort, setSort] = useState<KeySort>(savedSort);
  const chooseSort = (s: KeySort) => {
    setSort(s);
    try {
      if (s === "position") localStorage.removeItem(SORT_KEY);
      else localStorage.setItem(SORT_KEY, s);
    } catch {
      /* storage blocked: the order lasts for this visit */
    }
  };
  const allTags = useMemo(() => [...new Set(grid.rows.flatMap((r) => r.key.tags))].sort(), [grid.rows]);
  const rows = useMemo(() => {
    const f = filter.trim().toLowerCase();
    const shown = grid.rows.filter((r) => {
      const k = r.key;
      if (f && !k.name.toLowerCase().includes(f) && !k.description.toLowerCase().includes(f) && !k.tags.some((t) => t.includes(f))) return false;
      if (tag && !k.tags.includes(tag)) return false;
      if (show === "different") return rowDiffers(r.cells);
      if (show === "missing") return r.cells.some((c) => !c.present);
      if (show === "rules") return r.cells.some((c) => c.problem);
      return show === "all" || rowDiffers(r.cells) || r.cells.some((c) => !c.present || c.problem);
    });
    return sortRows(shown, sort);
  }, [grid.rows, filter, show, tag, sort]);
  // Moving keys changes the file order, so it only makes sense when that order is on screen.
  const reorderBlocked = filter !== "" || sort !== "position";
  const reorderHint = filter ? "Clear the filter to reorder keys." : "Choose File order under Sort to reorder keys.";
  const cols = envs.length + 1;

  const [nav, dispatch] = useReducer(gridReducer, undefined, () => initialGridState(rows.length, cols));
  useEffect(() => dispatch({ type: "resize", rows: rows.length, cols }), [rows.length, cols]);

  const [draft, setDraft] = useState("");
  const [pending, setPending] = useState<Pending>(null);
  const [menuFor, setMenuFor] = useState<number | null>(null);
  const [revealed, setRevealed] = useState<Record<string, string>>({});
  const [dragId, setDragId] = useState<number | null>(null);
  const [dropIndex, setDropIndex] = useState<number | null>(null);
  const timers = useRef<Record<string, ReturnType<typeof setTimeout>>>({});
  const cellRefs = useRef(new Map<string, HTMLElement>());
  const searchRef = useRef<HTMLInputElement>(null);
  const wantFocus = useRef(false);

  useEffect(() => {
    const t = timers.current;
    return () => Object.values(t).forEach(clearTimeout);
  }, []);

  // Move DOM focus only when the user moved the cursor, never on refetch.
  useEffect(() => {
    if (!wantFocus.current || nav.editing) return;
    wantFocus.current = false;
    cellRefs.current.get(`${nav.row}:${nav.col}`)?.focus();
  }, [nav.row, nav.col, nav.editing]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "/" && !isTyping(e) && !e.ctrlKey && !e.metaKey && !document.querySelector("[role=dialog]")) {
        e.preventDefault();
        searchRef.current?.focus();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const focusCell = (row: number, col: number) => {
    wantFocus.current = true;
    dispatch({ type: "focus", row, col });
  };

  // A link such as ?key=12&env=3 (from Insights or search) selects that cell,
  // once per link, also when the file is already open.
  const [params] = useSearchParams();
  const linked = useRef("");
  useEffect(() => {
    const link = `${params.get("key") ?? ""}:${params.get("env") ?? ""}`;
    if (linked.current === link) return;
    linked.current = link;
    const keyId = Number(params.get("key"));
    if (!keyId) return;
    const row = rows.findIndex((r) => r.key.id === keyId);
    const col = envs.findIndex((e) => e.id === Number(params.get("env"))) + 1;
    if (row >= 0) focusCell(row, col > 0 ? col : 0);
  });
  const refocus = () => {
    wantFocus.current = false;
    cellRefs.current.get(`${nav.row}:${nav.col}`)?.focus();
  };
  const canWrite = (env: Environment) => isAdmin || !env.isProtected;
  const refresh = () => qc.invalidateQueries({ predicate: (q) => q.queryKey[0] !== "me" });
  const cellId = (key: Key, env: Environment) => `${key.id}:${env.id}`;
  const forget = (id: string) =>
    setRevealed((r) => {
      const next = { ...r };
      delete next[id];
      return next;
    });

  /** Runs a write, announces the outcome and refreshes the grid. */
  const act = async (fn: () => Promise<unknown>, ok: string) => {
    try {
      await fn();
      announce(ok);
      await refresh();
      return true;
    } catch (e) {
      announce(errorMessage(e), "error");
      return false;
    }
  };

  const at = (row: number, col: number) => {
    const r = rows[row];
    const env = col > 0 ? envs[col - 1] : undefined;
    return { row: r, key: r?.key, env, cell: r && env ? r.cells[col - 1] : undefined };
  };
  const active = at(nav.row, nav.col);

  // commands

  const startEdit = (row = nav.row, col = nav.col) => {
    const { key, env, cell } = at(row, col);
    if (!key) return;
    if (!env || !cell) {
      setMenuFor(key.id);
      return;
    }
    if (!canWrite(env)) return setPending({ kind: "propose", key, env, cell, remove: false });
    setDraft(key.isSecret ? "" : (cell.value ?? ""));
    dispatch({ type: "focus", row, col });
    dispatch({ type: "edit" });
  };

  const stopEdit = () => {
    wantFocus.current = true;
    dispatch({ type: "stopEditing" });
  };

  const save = async () => {
    const { key, env, cell } = active;
    if (!key || !env || !cell) return;
    if (key.isSecret && draft === "" && cell.present) {
      announce("Nothing saved. Type the new secret, or delete the value instead.");
      return stopEdit();
    }
    if (!key.isSecret && cell.present && draft === cell.value) return stopEdit();
    if (!cell.present && draft === "") {
      announce("Nothing saved. Type a value first.");
      return stopEdit();
    }
    const ok = await act(() => api.setValue(key.id, env.id, draft), `Saved ${keyLabel(key)} in ${env.name}.`);
    if (ok) {
      forget(cellId(key, env));
      stopEdit();
    }
  };

  const reveal = async (target = active) => {
    const { key, env, cell } = target;
    if (!key || !env || !cell) return;
    if (!cell.present) return announce(`${keyLabel(key)} has no value in ${env.name}.`);
    if (!key.isSecret) return announce(`${keyLabel(key)} is not secret, so its value is already shown.`);
    try {
      const { value } = await api.reveal(key.id, env.id);
      const id = cellId(key, env);
      setRevealed((r) => ({ ...r, [id]: value }));
      clearTimeout(timers.current[id]);
      timers.current[id] = setTimeout(() => forget(id), REVEAL_MS);
      announce(`Showing ${keyLabel(key)} in ${env.name} for 10 seconds.`);
    } catch (e) {
      announce(errorMessage(e), "error");
    }
  };

  const copy = async () => {
    const { key, env, cell } = active;
    if (!key) return;
    try {
      if (!env || !cell) {
        await copyText(key.name);
        return announce(`Copied the key name ${key.name}.`);
      }
      if (!cell.present) return announce(`Nothing to copy: ${keyLabel(key)} has no value in ${env.name}.`);
      const { value } = await api.reveal(key.id, env.id, { purpose: "copy" });
      await copyText(value);
      announce(`Copied ${keyLabel(key)} from ${env.name} to the clipboard.`);
    } catch (e) {
      announce(errorMessage(e), "error");
    }
  };

  const remove = () => {
    const { key, env, cell } = active;
    if (!key) return;
    if (!env || !cell) {
      if (isRaw) return announce("A raw file has one body; delete the file instead.", "error");
      return setPending({ kind: "deleteKey", key });
    }
    if (!cell.present) return announce(`${keyLabel(key)} already has no value in ${env.name}.`);
    if (!canWrite(env)) return setPending({ kind: "propose", key, env, cell, remove: true });
    setPending({ kind: "deleteValue", key, env });
  };

  const reorder = async (ids: number[], ok: string, focusKeyId?: number) => {
    // Optimistic: the row moves at once, and the server confirms.
    qc.setQueryData<Grid>(["grid", file.id], (g) => g && { ...g, rows: ids.map((id) => g.rows.find((r) => r.key.id === id)!) });
    if (focusKeyId !== undefined) focusCell(ids.indexOf(focusKeyId), nav.col);
    await act(() => api.reorderKeys(file.id, ids), ok);
  };

  const moveRow = (key: Key, d: -1 | 1) => {
    if (reorderBlocked) return announce(reorderHint, "error");
    const ids = grid.rows.map((r) => r.key.id);
    const i = ids.indexOf(key.id);
    const j = i + d;
    if (j < 0 || j >= ids.length) return;
    [ids[i], ids[j]] = [ids[j], ids[i]];
    void reorder(ids, `Moved ${key.name} ${d < 0 ? "up" : "down"}.`, key.id);
  };

  const drop = (targetIndex: number) => {
    const from = dragId;
    setDragId(null);
    setDropIndex(null);
    if (from === null || reorderBlocked) return;
    const ids = grid.rows.map((r) => r.key.id).filter((id) => id !== from);
    const moved = grid.rows.find((r) => r.key.id === from)!.key;
    const fromIndex = grid.rows.findIndex((r) => r.key.id === from);
    ids.splice(targetIndex > fromIndex ? targetIndex - 1 : targetIndex, 0, from);
    if (ids.join() !== grid.rows.map((r) => r.key.id).join()) void reorder(ids, `Moved ${moved.name}.`, from);
  };

  const run = (command: GridCommand) => {
    const { key, env } = active;
    switch (command) {
      case "copy":
        return void copy();
      case "reveal":
        return void reveal();
      case "history":
        if (key && env) setPending({ kind: "history", key, env });
        return;
      case "delete":
        return remove();
      case "search":
        return searchRef.current?.focus();
      case "moveRowUp":
        return key && !isRaw && moveRow(key, -1);
      case "moveRowDown":
        return key && !isRaw && moveRow(key, 1);
      case "keyActions":
        if (key) setMenuFor(key.id);
        return;
    }
  };

  const onGridKeyDown = (e: ReactKeyboardEvent) => {
    if (nav.editing || isTyping(e) || menuFor !== null) return;
    const intent = keyToIntent(e);
    if (!intent) return;
    e.preventDefault();
    if ("command" in intent) return run(intent.command);
    if (intent.action.type === "edit") return startEdit();
    wantFocus.current = true;
    dispatch(intent.action);
  };

  // file-level actions

  const download = (env: Environment, format: ExportFormat) =>
    act(async () => {
      const f = await api.exportFile(file.id, env.id, format);
      downloadText(f.name, f.body, f.type);
    }, `Downloaded ${file.name} for ${env.name} as ${formatLabel[format]}.`);

  const copyFile = async (env: Environment) => {
    try {
      const f = await api.exportFile(file.id, env.id, file.format);
      await copyText(f.body);
      announce(`Copied the whole ${file.name} for ${env.name} to the clipboard.`);
    } catch (e) {
      announce(errorMessage(e), "error");
    }
  };

  const missing = countMissing(grid);
  const problems = grid.rows.reduce((n, r) => n + r.cells.filter((c) => c.problem).length, 0);
  const pendingRequests = grid.rows.reduce((n, r) => n + r.cells.reduce((m, c) => m + (c.pending ?? 0), 0), 0);
  const differing = grid.rows.filter((r) => rowDiffers(r.cells)).length;
  const missingByEnv = envs.map((_, i) => grid.rows.filter((r) => !r.cells[i].present).length);
  const legend = [
    differing > 0 && {
      key: "diff",
      mark: (
        <span className="inline-flex gap-0.5" aria-hidden="true">
          {[0, 1, 2].map((g) => (
            <span
              key={g}
              className="diff-chip inline-flex size-4 items-center justify-center rounded border text-[10px] font-bold text-ink"
              style={{ "--diff": groupColor(g) } as CSSProperties}
            >
              {groupLetter(g)}
            </span>
          ))}
        </span>
      ),
      text: "same letter, same value",
      long: "Values are lettered and coloured; cells with the same letter hold the same value.",
    },
    {
      key: "missing",
      mark: <span className="hatch-missing inline-block size-3.5 shrink-0 rounded-sm border border-missing" aria-hidden="true" />,
      text: "missing",
      long: "Hatched cells have no value in that environment.",
    },
    envs.some((e) => e.isProtected) && {
      key: "protected",
      mark: <Lock className="size-3.5 shrink-0 text-protected" aria-hidden="true" />,
      text: "protected",
      long: "Protected environments: only admins change them directly.",
    },
    problems > 0 && {
      key: "rule",
      mark: <TriangleAlert className="size-3.5 shrink-0 text-missing" aria-hidden="true" />,
      text: "breaks a rule",
      long: "The value breaks its key's rule: required, or a pattern.",
    },
    pendingRequests > 0 && {
      key: "request",
      mark: <GitPullRequestArrow className="size-3.5 shrink-0 text-protected" aria-hidden="true" />,
      text: "change request waiting",
      long: "Someone proposed a change that waits for an admin.",
    },
  ].filter((l) => !!l);
  const compareTo = (env: Environment) => {
    const other = envs.find((e) => e.id !== env.id);
    navigate(`/repos/${file.repoId}/files/${file.id}/compare?a=${env.id}${other ? `&b=${other.id}` : ""}`);
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3 p-3">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 rounded-xl border border-line bg-surface px-4 py-2 shadow-sm">
        <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
          <div className="flex min-w-0 items-center gap-2">
            <h1 className="truncate font-mono text-lg font-bold">{file.name}</h1>
            <Badge>{formatLabel[file.format]}</Badge>
          </div>
          <ul className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted" aria-label="Summary">
            <li>
              {grid.rows.length} {grid.rows.length === 1 ? "key" : "keys"}
            </li>
            {differing > 0 && (
              <li>
                {differing} {differing === 1 ? "key differs" : "keys differ"}
              </li>
            )}
            {problems > 0 && (
              <li className="inline-flex items-center gap-1 font-semibold text-missing">
                <TriangleAlert className="size-3.5" aria-hidden="true" />
                {problems} {problems === 1 ? "value breaks" : "values break"} a rule
              </li>
            )}
            {missing > 0 ? (
              <li className="inline-flex items-center gap-1 font-semibold text-missing">
                <CircleSlash className="size-3.5" aria-hidden="true" />
                {missing} missing
              </li>
            ) : (
              grid.rows.length > 0 && <li className="font-semibold text-accent">Every environment has every key</li>
            )}
          </ul>
        </div>
        <div className="ml-auto flex flex-wrap items-center gap-2">
          {!isRaw && (
            <div className="relative">
              <Search className="pointer-events-none absolute top-2 left-2.5 size-4 text-muted" aria-hidden="true" />
              <Input
                ref={searchRef}
                type="search"
                aria-label="Filter keys"
                aria-keyshortcuts="/"
                placeholder="Filter keys"
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Escape" || e.key === "ArrowDown" || e.key === "Enter") {
                    if (e.key === "Escape") setFilter("");
                    e.preventDefault();
                    focusCell(0, nav.col);
                  }
                }}
                className="h-8 w-44 pr-8 pl-8"
              />
              {!filter && (
                <span className="pointer-events-none absolute top-1.5 right-2" aria-hidden="true">
                  <Kbd>/</Kbd>
                </span>
              )}
            </div>
          )}
          <Button size="sm" onClick={() => setPending({ kind: "import", envId: null })}>
            <FileUp /> Import
          </Button>
          {!isRaw && (
            <Button size="sm" variant="primary" onClick={() => setPending({ kind: "addKey" })}>
              <Plus /> Add key
            </Button>
          )}
          <Menu>
            <MenuTrigger asChild>
              <Button variant="ghost" size="icon" className="size-8" aria-label="More file actions">
                <MoreHorizontal />
              </Button>
            </MenuTrigger>
            <MenuContent align="end">
              <MenuItem onSelect={() => setPending({ kind: "addEnv" })}>
                <Plus /> Add environment
              </MenuItem>
              <MenuItem onSelect={() => setPending({ kind: "renameFile" })}>
                <Pencil /> Rename file
              </MenuItem>
              {isAdmin && (
                <MenuItem danger onSelect={() => setPending({ kind: "deleteFile" })}>
                  <Trash2 /> Delete file
                </MenuItem>
              )}
            </MenuContent>
          </Menu>
        </div>
      </div>

      {grid.rows.length === 0 ? (
        <Panel>
        <EmptyState
          icon={KeyRound}
          title="No keys yet"
          actions={
            <>
              <Button variant="primary" onClick={() => setPending({ kind: "addKey" })}>
                <Plus /> Add key
              </Button>
              <Button onClick={() => setPending({ kind: "import", envId: null })}>
                <FileUp /> Import a file
              </Button>
            </>
          }
        >
          Add keys one at a time, or import a file you already have into one environment.
        </EmptyState>
        </Panel>
      ) : (
        <div className="flex min-h-[20rem] flex-1 flex-col overflow-hidden rounded-xl border border-line bg-surface shadow-sm">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-line px-3 py-1.5 text-xs md:flex-nowrap">
            {!isRaw && (
              <Select
                aria-label="Sort keys"
                className="h-7 w-auto shrink-0 gap-1.5 px-2 text-xs"
                itemClassName="text-xs"
                value={sort}
                onValueChange={(v) => chooseSort(v as KeySort)}
                options={keySorts.map((s) => ({ value: s.value, label: `Sort: ${s.label}` }))}
              />
            )}
            {allTags.length > 0 && (
              <Select
                aria-label="Filter by tag"
                className="h-7 w-auto shrink-0 gap-1.5 px-2 text-xs"
                itemClassName="text-xs"
                value={tag}
                onValueChange={setTag}
                options={[{ value: "", label: "All tags" }, ...allTags.map((t) => ({ value: t, label: `Tag: ${t}` }))]}
              />
            )}
            {/* The legend sits inline where it fits; on narrower screens it opens from a button. */}
            <ul aria-label="Legend" className="hidden min-w-0 items-center gap-x-3 whitespace-nowrap text-muted 2xl:flex">
              {legend.map((l) => (
                <li key={l.key} className="inline-flex items-center gap-1">
                  {l.mark}
                  {l.text}
                </li>
              ))}
            </ul>
            <Menu>
              <MenuTrigger asChild>
                <Button variant="ghost" size="sm" className="h-7 shrink-0 px-2 text-xs 2xl:hidden">
                  <Info /> Legend
                </Button>
              </MenuTrigger>
              <MenuContent align="start" className="w-72">
                {legend.map((l) => (
                  <MenuLabel key={l.key} className="flex items-center gap-2 py-1.5 font-normal text-ink">
                    {l.mark}
                    {l.long}
                  </MenuLabel>
                ))}
              </MenuContent>
            </Menu>
            <Select
              aria-label="Show keys"
              className="ml-auto h-7 w-auto shrink-0 gap-1.5 px-2 text-xs"
              itemClassName="text-xs"
              value={show}
              onValueChange={(v) => setShow(v as typeof show)}
              options={[
                { value: "all", label: "Show: All keys" },
                { value: "attention", label: "Show: Different, missing or breaking a rule" },
                { value: "different", label: "Show: Different" },
                { value: "missing", label: "Show: Missing somewhere" },
                { value: "rules", label: "Show: Breaking a rule" },
              ]}
            />
            <p className="shrink-0 whitespace-nowrap text-muted" aria-live="polite">
              {rows.length === grid.rows.length ? `${rows.length} keys` : `${rows.length} of ${grid.rows.length} keys`}
            </p>
          </div>
          <div className="relative min-h-0 flex-1 overflow-auto">
            <table
              role="grid"
              aria-label={`${file.name}: keys by environment`}
              aria-rowcount={rows.length + 1}
              aria-colcount={cols}
              aria-describedby="grid-help"
              onKeyDown={onGridKeyDown}
              className="w-full table-fixed border-separate border-spacing-0 text-left"
              style={{ minWidth: `${11 + envs.length * (isRaw ? 24 : 14)}rem` }}
            >
              <thead>
                <tr role="row" aria-rowindex={1}>
                  <th
                    role="columnheader"
                    scope="col"
                    aria-colindex={1}
                    className="sticky top-0 left-0 z-20 w-44 border-r border-b-2 border-line bg-surface-2 px-3 py-2.5 text-sm font-bold sm:w-64"
                  >
                    Key
                  </th>
                  {envs.map((env, i) => (
                    <th
                      key={env.id}
                      role="columnheader"
                      scope="col"
                      aria-colindex={i + 2}
                      className={cn(
                        "sticky top-0 z-10 border-r border-b-2 border-line px-3 py-1.5 align-middle",
                        env.isProtected ? "border-t-4 border-t-protected bg-protected-soft" : "bg-surface-2",
                      )}
                    >
                      <div className="flex items-center gap-1.5">
                        {env.isProtected && (
                          <span className="shrink-0 text-protected" title={`${env.name} is protected: only admins change it directly`}>
                            <Lock className="size-3.5" aria-hidden="true" />
                            <span className="sr-only">protected:</span>
                          </span>
                        )}
                        <span className={cn("min-w-0 truncate font-mono font-bold", env.isProtected && "text-protected")} title={env.name}>
                          {env.name}
                        </span>
                        {missingByEnv[i] > 0 ? (
                          <span className="shrink-0 rounded-full border border-missing bg-missing-soft px-1.5 text-xs font-semibold whitespace-nowrap text-missing">
                            {missingByEnv[i]} missing
                          </span>
                        ) : (
                          <span className="shrink-0 rounded-full border border-line px-1.5 text-xs font-semibold whitespace-nowrap text-muted">complete</span>
                        )}
                        <Menu>
                          <MenuTrigger asChild>
                            <Button variant="ghost" size="icon" className="ml-auto size-7 shrink-0" aria-label={`Actions for ${env.name}`}>
                              <MoreHorizontal />
                            </Button>
                          </MenuTrigger>
                          <MenuContent align="end">
                            {exportFormats(file.format).map((f) => (
                              <MenuItem key={f} onSelect={() => void download(env, f)}>
                                <Download /> Download as {formatLabel[f]}
                              </MenuItem>
                            ))}
                            <MenuItem onSelect={() => void copyFile(env)}>
                              <Copy /> Copy whole file
                            </MenuItem>
                            <MenuSeparator />
                            <MenuItem disabled={!canWrite(env)} onSelect={() => setPending({ kind: "import", envId: env.id })}>
                              <FileUp /> Import into this environment
                            </MenuItem>
                            {envs.length > 1 && (
                              <MenuItem onSelect={() => compareTo(env)}>
                                <Columns2 /> Compare with another environment
                              </MenuItem>
                            )}
                            {!isRaw && (
                              <MenuItem disabled={!canWrite(env)} onSelect={() => setPending({ kind: "restore", env })}>
                                <RotateCcw /> Restore to an earlier time
                              </MenuItem>
                            )}
                          </MenuContent>
                        </Menu>
                      </div>
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.map((row, r) => {
                  const key = row.key;
                  const isActiveRow = r === nav.row;
                  const headerActive = isActiveRow && nav.col === 0;
                  const rest = r % 2 === 1 ? "bg-surface-alt" : "bg-surface";
                  const differs = rowDiffers(row.cells);
                  return (
                    <tr
                      key={key.id}
                      role="row"
                      aria-rowindex={r + 2}
                      onDragOver={(e) => {
                        if (dragId === null) return;
                        e.preventDefault();
                        const rect = e.currentTarget.getBoundingClientRect();
                        const idx = grid.rows.findIndex((x) => x.key.id === key.id);
                        setDropIndex(e.clientY > rect.top + rect.height / 2 ? idx + 1 : idx);
                      }}
                      onDrop={(e) => {
                        e.preventDefault();
                        if (dropIndex !== null) drop(dropIndex);
                      }}
                      className={cn(
                        "group/row",
                        dropIndex !== null && grid.rows[dropIndex]?.key.id === key.id && "[&>*]:border-t-2 [&>*]:border-t-accent",
                        dropIndex === grid.rows.length && r === rows.length - 1 && "[&>*]:border-b-2 [&>*]:border-b-accent",
                        dragId === key.id && "opacity-50",
                      )}
                    >
                      <th
                        role="rowheader"
                        scope="row"
                        aria-colindex={1}
                        tabIndex={headerActive ? 0 : -1}
                        ref={(el) => {
                          if (el) cellRefs.current.set(`${r}:0`, el);
                          else cellRefs.current.delete(`${r}:0`);
                        }}
                        onClick={() => focusCell(r, 0)}
                        onFocus={() => !nav.editing && (nav.row !== r || nav.col !== 0) && dispatch({ type: "focus", row: r, col: 0 })}
                        className={cn(
                          "grid-cell sticky left-0 z-[5] border-r border-b border-line px-2 py-1.5 align-top font-normal",
                          headerActive ? "bg-accent-soft" : rest,
                        )}
                      >
                        <div className="flex items-start gap-1">
                          {!isRaw && (
                            <span
                              draggable={!reorderBlocked}
                              onDragStart={(e) => {
                                e.dataTransfer.effectAllowed = "move";
                                e.dataTransfer.setData("text/plain", key.name);
                                setDragId(key.id);
                              }}
                              onDragEnd={() => {
                                setDragId(null);
                                setDropIndex(null);
                              }}
                              className={cn("mt-0.5 text-muted", reorderBlocked ? "cursor-not-allowed opacity-40" : "cursor-grab")}
                              title={reorderBlocked ? reorderHint : "Drag to reorder, or press Alt+Up and Alt+Down"}
                              aria-hidden="true"
                            >
                              <GripVertical className="size-4" />
                            </span>
                          )}
                          <div className="min-w-0 flex-1">
                            <div className="flex flex-wrap items-center gap-x-1.5">
                              <span className="font-mono font-semibold [overflow-wrap:anywhere]">
                                <Breakable text={keyLabel(key)} />
                              </span>
                              {key.isSecret && (
                                <span className="inline-flex items-center gap-0.5 text-xs text-muted">
                                  <KeyRound className="size-3.5" aria-hidden="true" />
                                  secret
                                </span>
                              )}
                              {key.required && <span className="text-xs font-semibold text-muted">required</span>}
                              {key.pattern && (
                                <span className="text-xs text-muted" title={`Values must match ${key.pattern}`}>
                                  pattern
                                </span>
                              )}
                            </div>
                            {key.tags.length > 0 && (
                              <ul className="mt-0.5 flex flex-wrap gap-1" aria-label="Tags">
                                {key.tags.map((t) => (
                                  <li key={t} className="rounded border border-line bg-surface-2 px-1 text-[11px] leading-4 text-muted">
                                    {t}
                                  </li>
                                ))}
                              </ul>
                            )}
                            {key.description && (
                              <p className="line-clamp-1 text-xs text-muted" title={key.description}>
                                {key.description}
                              </p>
                            )}
                          </div>
                          <Menu open={menuFor === key.id} onOpenChange={(o) => setMenuFor(o ? key.id : null)}>
                            <MenuTrigger asChild>
                              <Button variant="ghost" size="icon" tabIndex={-1} className="-my-0.5 size-7 shrink-0" aria-label={`Actions for ${keyLabel(key)}`}>
                                <MoreHorizontal />
                              </Button>
                            </MenuTrigger>
                            <MenuContent
                              align="start"
                              onCloseAutoFocus={(e) => {
                                e.preventDefault();
                                refocus();
                              }}
                            >
                              <MenuItem onSelect={() => setPending({ kind: "editKey", key })}>
                                <Pencil /> {isRaw ? "Edit description" : "Edit name, tags and rules"}
                              </MenuItem>
                              <MenuItem
                                onSelect={() =>
                                  void act(
                                    () => api.updateKey(key.id, { isSecret: !key.isSecret }),
                                    key.isSecret ? `${keyLabel(key)} is no longer secret.` : `${keyLabel(key)} is now secret and masked.`,
                                  )
                                }
                              >
                                <KeyRound /> {key.isSecret ? "Mark as not secret" : "Mark as secret"}
                              </MenuItem>
                              {!isRaw && (
                                <>
                                  <MenuItem disabled={reorderBlocked || r === 0} onSelect={() => moveRow(key, -1)}>
                                    <ArrowUp /> Move up
                                  </MenuItem>
                                  <MenuItem disabled={reorderBlocked || r === rows.length - 1} onSelect={() => moveRow(key, 1)}>
                                    <ArrowDown /> Move down
                                  </MenuItem>
                                  <MenuSeparator />
                                  <MenuItem danger onSelect={() => setPending({ kind: "deleteKey", key })}>
                                    <Trash2 /> Delete key
                                  </MenuItem>
                                </>
                              )}
                            </MenuContent>
                          </Menu>
                        </div>
                      </th>
                      {row.cells.map((cell, i) => {
                        const c = i + 1;
                        const env = envs[i];
                        const isActive = isActiveRow && nav.col === c;
                        const editing = isActive && nav.editing;
                        const group = differs && cell.present && cell.group !== undefined ? cell.group : undefined;
                        const revealedValue = revealed[cellId(row.key, env)];
                        return (
                          <td
                            key={env.id}
                            role="gridcell"
                            aria-colindex={c + 1}
                            aria-selected={isActive}
                            aria-readonly={!canWrite(env)}
                            aria-describedby={isActive ? "cell-meta" : undefined}
                            tabIndex={isActive && !editing ? 0 : -1}
                            title={describeCell(cell)}
                            ref={(el) => {
                              if (el) cellRefs.current.set(`${r}:${c}`, el);
                              else cellRefs.current.delete(`${r}:${c}`);
                            }}
                            onClick={() => !nav.editing && focusCell(r, c)}
                            onDoubleClick={() => !nav.editing && startEdit(r, c)}
                            onFocus={() => !nav.editing && (nav.row !== r || nav.col !== c) && dispatch({ type: "focus", row: r, col: c })}
                            style={group !== undefined ? ({ "--diff": groupColor(group) } as CSSProperties) : undefined}
                            className={cn(
                              "grid-cell border-r border-b border-line px-3 py-1.5 align-top",
                              !cell.present && !editing && "hatch-missing",
                              cell.present && group !== undefined && "diff-cell",
                              cell.present && (isActive ? "bg-accent-soft" : group === undefined && rest),
                              !cell.present && isActive && "ring-2 ring-accent ring-inset",
                            )}
                          >
                            {editing ? (
                              <CellEditor
                                value={draft}
                                onChange={setDraft}
                                onSave={() => void save()}
                                onCancel={stopEdit}
                                secret={row.key.isSecret}
                                hadValue={cell.present}
                                label={`${keyLabel(row.key)} in ${env.name}`}
                                tall={isRaw}
                                php={file.format === "php"}
                              />
                            ) : (
                              <CellValue
                                cell={cell}
                                pattern={row.key.pattern}
                                revealed={revealedValue}
                                raw={isRaw}
                                group={group}
                                label={`${keyLabel(row.key)} in ${env.name}`}
                                onReveal={() => void reveal({ row, key: row.key, env, cell })}
                                onHide={() => forget(cellId(row.key, env))}
                              />
                            )}
                          </td>
                        );
                      })}
                    </tr>
                  );
                })}
              </tbody>
            </table>
            {rows.length === 0 && (
              <p className="p-6 text-muted">
                {filter ? `No keys match “${filter}”. Press Esc in the filter box to clear it.` : "Every key has the same value in every environment."}
              </p>
            )}
          </div>

          <Inspector
            keyItem={active.key}
            env={active.env}
            cell={active.cell}
            canWrite={active.env ? canWrite(active.env) : true}
            onEdit={() => startEdit()}
            onCommand={run}
            raw={isRaw}
          />
          <p id="grid-help" className="sr-only">
            Use arrow keys to move. Enter edits, c copies, r reveals a secret, h opens history, Delete removes. Press question mark for all shortcuts.
          </p>
        </div>
      )}

      {pending?.kind === "addKey" && (
        <AddKeyDialog
          fileId={file.id}
          onClose={() => setPending(null)}
          onAdded={(k) => {
            setFilter("");
            announce(`Added ${k.name}.`);
            void refresh().then(() => focusCell(grid.rows.length, 1));
          }}
        />
      )}
      {pending?.kind === "editKey" && <EditKeyDialog item={pending.key} raw={isRaw} isAdmin={isAdmin} onClose={() => setPending(null)} />}
      {pending?.kind === "propose" && (
        <ProposeDialog
          item={pending.key}
          env={pending.env}
          cell={pending.cell}
          remove={pending.remove}
          php={file.format === "php"}
          onClose={() => setPending(null)}
        />
      )}
      {pending?.kind === "restore" && <RestoreDialog fileId={file.id} env={pending.env} onClose={() => setPending(null)} />}
      {pending?.kind === "import" && (
        <ImportDialog file={file} environments={envs} user={user} envId={pending.envId} onClose={() => setPending(null)} />
      )}
      {pending?.kind === "history" && (
        <HistoryDrawer item={pending.key} env={pending.env} canWrite={canWrite(pending.env)} onClose={() => setPending(null)} />
      )}
      {pending?.kind === "renameFile" && <RenameFileDialog id={file.id} name={file.name} onClose={() => setPending(null)} />}
      {pending?.kind === "addEnv" && <AddEnvironmentDialog repoId={file.repoId} user={user} onClose={() => setPending(null)} />}
      <ConfirmDialog
        open={pending?.kind === "deleteKey"}
        onOpenChange={(o) => !o && setPending(null)}
        title={pending?.kind === "deleteKey" ? `Delete ${pending.key.name}?` : ""}
        description="Its values in every environment, and all their history, are deleted. This cannot be undone."
        confirmLabel="Delete key"
        onConfirm={() => pending?.kind === "deleteKey" && void act(() => api.deleteKey(pending.key.id), `Deleted ${pending.key.name}.`)}
      />
      <ConfirmDialog
        open={pending?.kind === "deleteValue"}
        onOpenChange={(o) => !o && setPending(null)}
        title={pending?.kind === "deleteValue" ? `Delete ${keyLabel(pending.key)} in ${pending.env.name}?` : ""}
        description="The key stays and this cell shows as missing. You can restore the old value from its history."
        confirmLabel="Delete value"
        onConfirm={() =>
          pending?.kind === "deleteValue" &&
          void act(() => api.deleteValue(pending.key.id, pending.env.id), `Deleted ${keyLabel(pending.key)} in ${pending.env.name}.`)
        }
      />
      <ConfirmDialog
        open={pending?.kind === "deleteFile"}
        onOpenChange={(o) => !o && setPending(null)}
        title={`Delete ${file.name}?`}
        description="Every key, value and version in this file is deleted. This cannot be undone."
        confirmLabel="Delete file"
        onConfirm={() =>
          void act(() => api.deleteFile(file.id), `Deleted ${file.name}.`).then((ok) => ok && navigate(`/repos/${file.repoId}`))
        }
      />
    </div>
  );
}

/** Warnings and markers under a cell's value: broken rules, waiting requests, references. */
function CellNotes({ cell, pattern, text }: { cell: Cell; pattern: string; text?: string }) {
  const notes = [];
  if (cell.problem === "required") notes.push(<span key="r">Required key</span>);
  if (cell.problem === "pattern")
    notes.push(
      <span key="p" title={`Values must match ${pattern}`}>
        Breaks the pattern<span className="sr-only"> {pattern}</span>
      </span>,
    );
  if (notes.length === 0 && !cell.pending && !text?.includes("${")) return null;
  return (
    <span className="mt-1 flex flex-col gap-0.5 text-xs">
      {notes.length > 0 && (
        <span className="inline-flex items-start gap-1 font-semibold text-missing">
          <TriangleAlert className="mt-px size-3.5 shrink-0" aria-hidden="true" />
          {notes}
        </span>
      )}
      {!!cell.pending && (
        <span className="inline-flex items-center gap-1 font-semibold text-protected">
          <GitPullRequestArrow className="size-3.5 shrink-0" aria-hidden="true" />
          {cell.pending === 1 ? "1 change request waiting" : `${cell.pending} change requests waiting`}
        </span>
      )}
      {text?.includes("${") && (
        <span className="inline-flex items-center gap-1 text-muted" title="References such as ${NAME}, or ${prod.NAME} for another environment, are filled in when the file is exported.">
          <Link2 className="size-3.5 shrink-0" aria-hidden="true" />
          Uses references, filled in on export
        </span>
      )}
    </span>
  );
}

function CellValue({
  cell,
  pattern,
  revealed,
  raw,
  group,
  label,
  onReveal,
  onHide,
}: {
  cell: Cell;
  pattern: string;
  revealed?: string;
  raw: boolean;
  group?: number;
  label: string;
  onReveal: () => void;
  onHide: () => void;
}) {
  const view = cellView(cell, revealed);
  const chip =
    group !== undefined ? (
      <span
        className="diff-chip ml-auto inline-flex size-5 shrink-0 items-center justify-center rounded border text-[11px] font-bold text-ink opacity-60 transition-opacity group-hover/row:opacity-100 group-focus-within/row:opacity-100"
        style={{ "--diff": groupColor(group) } as CSSProperties}
        title={`Value ${groupLetter(group)}: cells with the same letter hold the same value`}
      >
        <span className="sr-only">, value </span>
        {groupLetter(group)}
      </span>
    ) : null;
  // The eye is for pointers; keyboard users press r. Kept out of the tab order
  // so the grid stays one tab stop.
  const eye = (hide: boolean) => (
    <button
      type="button"
      tabIndex={-1}
      onClick={hide ? onHide : onReveal}
      className="-my-0.5 inline-flex size-7 shrink-0 cursor-pointer items-center justify-center rounded-md text-muted hover:bg-surface-2 hover:text-ink"
      aria-label={hide ? `Hide ${label}` : `Reveal ${label} for 10 seconds`}
      title={hide ? "Hide" : "Reveal for 10 seconds (r)"}
    >
      {hide ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
    </button>
  );
  const notes = <CellNotes cell={cell} pattern={pattern} text={view.kind === "value" ? view.text : undefined} />;
  switch (view.kind) {
    case "missing":
      return (
        <>
          <span className="inline-flex items-center gap-1.5 text-sm font-semibold text-missing">
            <CircleSlash className="size-4" aria-hidden="true" />
            missing
          </span>
          {notes}
        </>
      );
    case "masked":
      return (
        <>
          <span className="flex items-center gap-1.5 text-muted">
            <span className="font-mono tracking-widest" aria-hidden="true">
              {view.text}
            </span>
            <span className="sr-only">secret value, hidden</span>
            {eye(false)}
            {chip}
          </span>
          {notes}
        </>
      );
    case "empty":
      return (
        <>
          <span className="flex items-center gap-1.5">
            <span className="text-sm text-muted italic">empty</span>
            {chip}
          </span>
          {notes}
        </>
      );
    case "value":
      return (
        <>
          <span className="flex items-start gap-1.5">
            <span className={cn("min-w-0 font-mono text-sm whitespace-pre-wrap [overflow-wrap:anywhere]", raw ? "line-clamp-[12]" : "line-clamp-3")}>
              {raw ? view.text : <Breakable text={view.text} />}
            </span>
            {revealed !== undefined && eye(true)}
            {chip}
          </span>
          {notes}
        </>
      );
  }
}

function CellEditor({
  value,
  onChange,
  onSave,
  onCancel,
  secret,
  hadValue,
  label,
  tall,
  php,
}: {
  value: string;
  onChange: (v: string) => void;
  onSave: () => void;
  onCancel: () => void;
  secret: boolean;
  hadValue: boolean;
  label: string;
  tall: boolean;
  php: boolean;
}) {
  const lines = value.split("\n").length;
  return (
    <div className="space-y-1.5">
      <textarea
        autoFocus
        aria-label={`Value of ${label}`}
        spellCheck={false}
        autoComplete="off"
        value={value}
        rows={Math.min(tall ? 20 : 8, Math.max(tall ? 8 : 1, lines))}
        placeholder={secret && hadValue ? "Type a new value; the current one stays hidden" : "Value"}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.shiftKey && !e.altKey) {
            e.preventDefault();
            onSave();
          } else if (e.key === "Escape") {
            e.preventDefault();
            onCancel();
          }
          e.stopPropagation();
        }}
        className="block w-full min-w-48 resize-y rounded border border-accent bg-surface px-2 py-1 font-mono text-sm text-ink"
      />
      <div className="flex items-center gap-1.5 text-xs text-muted">
        <Button size="sm" variant="primary" className="h-7" onClick={onSave}>
          Save
        </Button>
        <Button size="sm" className="h-7" onClick={onCancel}>
          Cancel
        </Button>
        <span className="hidden sm:inline">
          <Kbd>Enter</Kbd> saves, <Kbd>Shift</Kbd>+<Kbd>Enter</Kbd> new line
        </span>
      </div>
      {php && <p className="text-xs text-muted">PHP value: quote text, for example 'hello', or write a number, true, NULL or an array.</p>}
    </div>
  );
}

function Inspector({
  keyItem,
  env,
  cell,
  canWrite,
  onEdit,
  onCommand,
  raw,
}: {
  keyItem?: Key;
  env?: Environment;
  cell?: Cell;
  canWrite: boolean;
  onEdit: () => void;
  onCommand: (c: GridCommand) => void;
  raw: boolean;
}) {
  if (!keyItem) return null;
  const onValue = !!env && !!cell;
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-2 border-t border-line bg-surface px-4 py-1.5 md:flex-nowrap" aria-label="Selected cell">
      <p id="cell-meta" className="min-w-0 flex-1 text-sm md:truncate" title={onValue ? describeCell(cell) : undefined}>
        <span className="font-mono font-semibold">{keyLabel(keyItem)}</span>
        {env && (
          <>
            {" in "}
            <span className="font-mono font-semibold">{env.name}</span>
          </>
        )}
        {". "}
        <span className="text-muted">
          {onValue ? describeCell(cell) : "Key name. Enter opens key actions; Alt with the up or down arrow moves the key."}
          {env && !canWrite && ` ${env.name} is protected: your changes go to an admin for approval.`}
        </span>
      </p>
      <div className="ml-auto flex shrink-0 flex-wrap gap-1.5 [&_kbd]:max-2xl:hidden">
        {onValue ? (
          <>
            <Button size="sm" onClick={onEdit}>
              {canWrite ? <Pencil /> : <GitPullRequestArrow />} {canWrite ? "Edit" : "Propose change"} <Kbd>Enter</Kbd>
            </Button>
            <Button size="sm" disabled={!cell.present} onClick={() => onCommand("copy")}>
              <Copy /> Copy <Kbd>c</Kbd>
            </Button>
            {keyItem.isSecret && (
              <Button size="sm" disabled={!cell.present} onClick={() => onCommand("reveal")}>
                <Eye /> Reveal <Kbd>r</Kbd>
              </Button>
            )}
            <Button size="sm" disabled={cell.version === 0} onClick={() => onCommand("history")}>
              <History /> History <Kbd>h</Kbd>
            </Button>
            <Button size="sm" disabled={!cell.present} onClick={() => onCommand("delete")}>
              <Trash2 /> {canWrite ? "Delete" : "Propose deletion"}
            </Button>
          </>
        ) : (
          <>
            <Button size="sm" onClick={() => onCommand("keyActions")}>
              <MoreHorizontal /> Key actions <Kbd>m</Kbd>
            </Button>
            {!raw && (
              <Button size="sm" onClick={() => onCommand("delete")}>
                <Trash2 /> Delete key
              </Button>
            )}
          </>
        )}
      </div>
    </div>
  );
}

const SORT_KEY = "envgrid-grid-sort";

/** The row order chosen last time in this browser; file order when none or unknown. */
function savedSort(): KeySort {
  try {
    const s = localStorage.getItem(SORT_KEY);
    return keySorts.some((k) => k.value === s) ? (s as KeySort) : "position";
  } catch {
    return "position";
  }
}

const SECRET_HINT = /secret|passw|pwd|token|api[_.-]?key|private|credential|auth|dsn|cert|salt/i;

function AddKeyDialog({ fileId, onClose, onAdded }: { fileId: number; onClose: () => void; onAdded: (k: Key) => void }) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [secret, setSecret] = useState(false);
  const [secretTouched, setSecretTouched] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      const k = await api.createKey(fileId, { name: name.trim(), description, isSecret: secret });
      onClose();
      onAdded(k);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <form onSubmit={submit}>
          <DialogTitle>Add key</DialogTitle>
          <DialogDescription>The key appears as a new row, missing in every environment until you set its values.</DialogDescription>
          <div className="mt-4 space-y-4">
            <Field label="Name" htmlFor="key-name">
              <Input
                id="key-name"
                required
                className="font-mono"
                value={name}
                placeholder="DATABASE_URL"
                onChange={(e) => {
                  setName(e.target.value);
                  if (!secretTouched) setSecret(SECRET_HINT.test(e.target.value));
                }}
              />
            </Field>
            <Field label="Description" htmlFor="key-desc" hint="Optional. Say what the value is for and who owns it.">
              <Input id="key-desc" value={description} onChange={(e) => setDescription(e.target.value)} />
            </Field>
            <label className="flex items-start gap-2">
              <input
                type="checkbox"
                className="mt-0.5 size-4 accent-(--accent)"
                checked={secret}
                onChange={(e) => {
                  setSecret(e.target.checked);
                  setSecretTouched(true);
                }}
              />
              <span>
                <span className="font-semibold">Secret</span>
                <span className="block text-sm text-muted">Masked in the grid until someone reveals it.</span>
              </span>
            </label>
            {error && (
              <p role="alert" className="text-sm text-missing">
                {error}
              </p>
            )}
          </div>
          <DialogFooter>
            <Button type="submit" variant="primary" disabled={busy}>
              Add key
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function EditKeyDialog({ item, raw, isAdmin, onClose }: { item: Key; raw: boolean; isAdmin: boolean; onClose: () => void }) {
  const announce = useAnnounce();
  const qc = useQueryClient();
  const [name, setName] = useState(item.name);
  const [description, setDescription] = useState(item.description);
  const [tags, setTags] = useState(item.tags.join(", "));
  const [required, setRequired] = useState(item.required);
  const [pattern, setPattern] = useState(item.pattern);
  const [error, setError] = useState<string | null>(null);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const tagList = tags
      .split(",")
      .map((t) => t.trim())
      .filter(Boolean);
    try {
      await api.updateKey(
        item.id,
        raw
          ? { description }
          : { name: name.trim(), description, tags: tagList, ...(isAdmin ? { required, pattern: pattern.trim() } : {}) },
      );
      announce(`Saved ${raw ? "the description" : name.trim()}.`);
      await qc.invalidateQueries({ predicate: (q) => q.queryKey[0] !== "me" });
      onClose();
    } catch (err) {
      setError(errorMessage(err));
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <form onSubmit={submit}>
          <DialogTitle>Edit key</DialogTitle>
          <div className="mt-4 space-y-4">
            {!raw && (
              <Field label="Name" htmlFor="edit-key-name" hint="Renaming changes every environment's exported file.">
                <Input id="edit-key-name" required className="font-mono" value={name} onChange={(e) => setName(e.target.value)} />
              </Field>
            )}
            <Field label="Description" htmlFor="edit-key-desc">
              <Input id="edit-key-desc" value={description} onChange={(e) => setDescription(e.target.value)} />
            </Field>
            {!raw && (
              <Field label="Tags" htmlFor="edit-key-tags" hint="Optional. Separate with commas, for example payments, backend. Filter the grid by tag.">
                <Input id="edit-key-tags" value={tags} onChange={(e) => setTags(e.target.value)} />
              </Field>
            )}
            {!raw && isAdmin && (
              <fieldset className="space-y-4 rounded-lg border border-line p-4">
                <legend className="px-1 text-sm font-bold">Rules</legend>
                <label className="flex items-start gap-2">
                  <input
                    type="checkbox"
                    className="mt-0.5 size-4 accent-(--accent)"
                    checked={required}
                    onChange={(e) => setRequired(e.target.checked)}
                  />
                  <span>
                    <span className="font-semibold">Required in every environment</span>
                    <span className="block text-sm text-muted">Missing values are flagged, and nobody can delete a value that is set.</span>
                  </span>
                </label>
                <Field
                  label="Pattern"
                  htmlFor="edit-key-pattern"
                  hint="Optional regular expression that every value must match, for example ^https:// for a web address."
                >
                  <Input id="edit-key-pattern" className="font-mono" value={pattern} onChange={(e) => setPattern(e.target.value)} />
                </Field>
              </fieldset>
            )}
            {!raw && !isAdmin && (item.required || item.pattern) && (
              <p className="text-sm text-muted">
                Rules set by an admin: {[item.required && "required in every environment", item.pattern && `values match ${item.pattern}`].filter(Boolean).join("; ")}.
              </p>
            )}
            {error && (
              <p role="alert" className="text-sm text-missing">
                {error}
              </p>
            )}
          </div>
          <DialogFooter>
            <Button type="submit" variant="primary">
              Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function RenameFileDialog({ id, name: initial, onClose }: { id: number; name: string; onClose: () => void }) {
  const announce = useAnnounce();
  const qc = useQueryClient();
  const [name, setName] = useState(initial);
  const [error, setError] = useState<string | null>(null);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    try {
      const f = await api.updateFile(id, { name });
      announce(`Renamed to ${f.name}.`);
      await qc.invalidateQueries({ predicate: (q) => q.queryKey[0] !== "me" });
      onClose();
    } catch (err) {
      setError(errorMessage(err));
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <form onSubmit={submit}>
          <DialogTitle>Rename file</DialogTitle>
          <div className="mt-4 space-y-4">
            <Field label="File name" htmlFor="rename-file">
              <Input id="rename-file" required className="font-mono" value={name} onChange={(e) => setName(e.target.value)} />
            </Field>
            {error && (
              <p role="alert" className="text-sm text-missing">
                {error}
              </p>
            )}
          </div>
          <DialogFooter>
            <Button type="submit" variant="primary">
              Rename
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
