import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { CircleCheck, FileText, Lock, Plus, Search } from "lucide-react";
import { Link } from "react-router";
import { useMe } from "@/App";
import { useDocumentTitle } from "@/components/PageHeader";
import { Button, ErrorNotice, Kbd, Loading, Panel } from "@/components/ui";
import { api } from "@/lib/api";
import { greeting, personName, relativeTime } from "@/lib/format";
import type { AuditRecord, Repo } from "@/lib/types";
import { cn } from "@/lib/utils";
import { CreateRepoDialog, GettingStarted, RecentActivity } from "./Repos";

/** Environment columns shown in the coverage table; the rest are named in a note. */
const MAX_ENV_COLUMNS = 6;

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

/** Asks the app shell to open the command palette. */
const openSearch = () => window.dispatchEvent(new Event("envgrid:search"));

/**
 * The first screen after signing in: how complete every repo is in every
 * environment, and what changed lately.
 */
export function HomePage() {
  useDocumentTitle("Home");
  const me = useMe().data!;
  const repos = useQuery({ queryKey: ["repos"], queryFn: api.repos });
  const [creating, setCreating] = useState(false);
  const name = (me.name.trim() || me.email.split("@")[0]).split(/\s+/)[0];
  const list = repos.data ?? [];

  return (
    <div className="mx-auto w-full max-w-7xl space-y-6 p-4 pb-16 md:p-6 md:pb-16">
      <header className="flex flex-col gap-4 md:flex-row md:items-end md:gap-6 xl:grid xl:grid-cols-[minmax(0,1fr)_24rem]">
        <div className="min-w-0 flex-1">
          <h1 className="text-3xl font-bold tracking-tight md:text-4xl">
            {list.length === 0 && repos.isSuccess ? `Welcome to envgrid, ${name}.` : `${greeting()}, ${name}.`}
          </h1>
        </div>
        <div className="flex flex-wrap items-center gap-2 xl:flex-nowrap">
          <Button onClick={openSearch} aria-keyshortcuts="Control+K" title="Find a repo, file or key (Ctrl K)" className="xl:min-w-0 xl:flex-1 xl:justify-start">
            <Search /> <span className="truncate">Find a repo, file or key</span>
            <span className="hidden items-center gap-0.5 sm:inline-flex xl:hidden" aria-hidden="true">
              <Kbd>Ctrl</Kbd>
              <Kbd>K</Kbd>
            </span>
          </Button>
          <Button variant="primary" className="shrink-0" onClick={() => setCreating(true)}>
            <Plus /> New repo
          </Button>
        </div>
      </header>

      {repos.isPending && <Loading />}
      {repos.isError && <ErrorNotice error={repos.error} />}

      {repos.isSuccess && list.length === 0 && (
        <>
          <GettingStarted onCreate={() => setCreating(true)} />
          <p className="text-sm text-muted">
            To look around first, an admin can run <code className="rounded bg-surface-2 px-1 font-mono">envgrid seed</code> on the server to add a demo
            repo.
          </p>
        </>
      )}

      {list.length > 0 && (
        <>
          <div className="grid items-start gap-6 xl:grid-cols-[minmax(0,1fr)_24rem] xl:items-stretch">
            <div className="min-w-0 space-y-6">
              <CoverageTable repos={list} />
              <JumpBackIn repos={list} />
            </div>
            {/* Out of the flow on wide screens, so the left column alone sets the height. */}
            <div className="relative xl:min-h-[16rem]">
              <div className="xl:absolute xl:inset-0">
                <RecentActivity fill />
              </div>
            </div>
          </div>
        </>
      )}
      <CreateRepoDialog open={creating} onOpenChange={setCreating} />
    </div>
  );
}

/**
 * Every repo against the environments most repos share: a bar and a count of
 * keys with a value, hatched where values are missing, never colour alone.
 */
function CoverageTable({ repos }: { repos: Repo[] }) {
  // Environment names by how many repos use them, then by their position.
  const seen = new Map<string, { count: number; order: number }>();
  repos.forEach((r) =>
    r.coverage.forEach((c, i) => {
      const s = seen.get(c.name);
      if (s) s.count++;
      else seen.set(c.name, { count: 1, order: i });
    }),
  );
  const names = [...seen.entries()].sort((a, b) => b[1].count - a[1].count || a[1].order - b[1].order).map(([n]) => n);
  const columns = names.slice(0, MAX_ENV_COLUMNS);
  const more = names.length - columns.length;
  const sorted = [...repos].sort((a, b) => (b.lastChangeAt ?? "").localeCompare(a.lastChangeAt ?? ""));

  return (
    <Panel
      title="Every repo, every environment"
      description="How many keys have a value in each environment. A hatched bar and a red count mean values are missing."
      actions={
        <Button asChild size="sm">
          <Link to="/repos">All repos</Link>
        </Button>
      }
      bodyClassName="p-0"
    >
      <div className="relative overflow-x-auto">
        <table className="w-full min-w-[40rem] border-separate border-spacing-x-2 border-spacing-y-2 px-2 text-left text-sm">
          <caption className="sr-only">Keys with a value in each environment, per repo, most recently changed first</caption>
          <thead>
            <tr>
              <th scope="col" className="px-2 pt-1 font-semibold">
                Repo
              </th>
              {columns.map((n) => (
                <th key={n} scope="col" className="px-1 pt-1 font-mono font-semibold">
                  <span className="block max-w-36 truncate" title={n}>
                    {n}
                  </span>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {sorted.map((r) => (
              <tr key={r.id}>
                <th scope="row" className="px-2 align-middle font-normal">
                  <Link to={`/repos/${r.id}`} className="font-mono font-semibold hover:underline">
                    {r.name}
                  </Link>
                  <span className="block text-xs text-muted">
                    {plural(r.fileCount, "file", "files")}, {plural(r.keyCount, "key", "keys")}
                  </span>
                </th>
                {columns.map((n) => (
                  <CoverageCell key={n} repo={r} env={n} />
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {more > 0 && (
        <p className="border-t border-line px-4 py-2 text-xs text-muted">
          {plural(more, "more environment", "more environments")}, such as {names[MAX_ENV_COLUMNS]}, {more === 1 ? "is" : "are"} shown on each repo's page.
        </p>
      )}
    </Panel>
  );
}

/** One repo in one environment as a tile: keys set out of all, a bar, and the gap in words. */
function CoverageCell({ repo, env }: { repo: Repo; env: string }): ReactNode {
  const c = repo.coverage.find((x) => x.name === env);
  if (!c) {
    return (
      <td className="rounded-lg border border-dashed border-line px-3 py-2 text-xs text-muted">
        <span aria-hidden="true">not used</span>
        <span className="sr-only">{repo.name} has no {env} environment</span>
      </td>
    );
  }
  const keys = repo.keyCount;
  const missing = Math.max(0, keys - c.present);
  const pct = keys === 0 ? 100 : Math.round((c.present / keys) * 100);
  const complete = keys > 0 && missing === 0;
  return (
    <td className={cn("rounded-lg border px-3 py-2", complete ? "border-accent bg-accent-soft" : "border-line bg-surface-alt")}>
      <span className="flex items-center gap-1.5">
        <span className="font-mono text-base font-bold">
          {c.present}
          <span className="text-xs font-normal text-muted">/{keys}</span>
        </span>
        {c.isProtected && (
          <span className="ml-auto text-protected" title="Protected: only admins change it directly">
            <Lock className="size-3.5" aria-hidden="true" />
            <span className="sr-only">protected,</span>
          </span>
        )}
      </span>
      <span className="hatch-missing mt-1.5 block h-1.5 w-full min-w-16 overflow-hidden rounded-full border border-line" aria-hidden="true">
        <span className="block h-full rounded-full bg-accent" style={{ width: `${pct}%` }} />
      </span>
      <span className={cn("mt-1 flex items-center gap-1 text-xs font-semibold whitespace-nowrap", missing > 0 ? "text-missing" : "text-muted")}>
        {complete && <CircleCheck className="size-3.5 text-accent" aria-hidden="true" />}
        {keys === 0 ? "no keys yet" : missing > 0 ? `${missing} missing` : "complete"}
        <span className="sr-only">, {c.present} of {keys} keys set</span>
      </span>
    </td>
  );
}

/** Actions that change a file's keys or values, for "Jump back in". */
const fileChanges = new Set(["create_value", "update_value", "delete_value", "create_key", "update_key", "delete_key", "approve_change", "restore_environment", "create_file", "update_file"]);

/** The files changed most recently, so people can pick up where they left off. */
function JumpBackIn({ repos }: { repos: Repo[] }) {
  const audit = useQuery({ queryKey: ["audit", "home-files"], queryFn: () => api.audit({ limit: 100 }) });
  const repoName = new Map(repos.map((r) => [r.id, r.name]));
  const deleted = new Set((audit.data?.items ?? []).filter((a) => a.action === "delete_file").map((a) => a.fileId));
  const files: AuditRecord[] = [];
  for (const a of audit.data?.items ?? []) {
    if (!a.fileId || !a.repoId || !repoName.has(a.repoId) || deleted.has(a.fileId) || !fileChanges.has(a.action)) continue;
    if (!files.some((f) => f.fileId === a.fileId)) files.push(a);
    if (files.length === 4) break;
  }
  if (files.length === 0) return null;
  return (
    <section aria-labelledby="jump-title">
      <h2 id="jump-title" className="mb-2 text-sm font-bold">
        Jump back in
      </h2>
      <ul className="grid gap-3 sm:grid-cols-2">
        {files.map((a) => (
          <li key={a.fileId}>
            <Link
              to={`/repos/${a.repoId}/files/${a.fileId}`}
              className="flex items-start gap-3 rounded-xl border border-line bg-surface p-3.5 shadow-sm transition-colors hover:border-accent focus-visible:border-accent"
            >
              <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-accent-soft text-accent" aria-hidden="true">
                <FileText className="size-4" />
              </span>
              <span className="min-w-0">
                <span className="block truncate font-mono font-semibold">{typeof a.detail.file === "string" ? a.detail.file : "File"}</span>
                <span className="block truncate text-xs text-muted">in {repoName.get(a.repoId ?? 0)}</span>
                <span className="mt-1 block text-xs text-muted">
                  Changed {relativeTime(a.createdAt)} by {personName(a.user)}
                </span>
              </span>
            </Link>
          </li>
        ))}
      </ul>
    </section>
  );
}
