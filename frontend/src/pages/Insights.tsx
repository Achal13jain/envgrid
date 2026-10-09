import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router";
import {
  ChevronRight,
  CircleCheck,
  CircleSlash,
  Clock,
  Copy,
  FileText,
  GitPullRequestArrow,
  KeyRound,
  RotateCcw,
  TriangleAlert,
  type LucideIcon,
} from "lucide-react";
import { PageHeader } from "@/components/PageHeader";
import { Badge, Breakable, Button, EmptyState, ErrorNotice, Field, Loading, Panel, Select } from "@/components/ui";
import { api } from "@/lib/api";
import { keyLabel, personName } from "@/lib/format";
import type { InsightPlace, Insights } from "@/lib/types";
import { cn } from "@/lib/utils";

const DEFAULT_DAYS = 90;
const dayOptions = [30, 90, 180, 365].map((d) => ({ value: d, label: d === 365 ? "1 year" : `${d} days` }));
// Rows a section shows before "Show all".
const FIRST_ROWS = 5;

/** Opens the file with this key and environment selected in the grid. */
const cellLink = (p: InsightPlace) => `/repos/${p.repoId}/files/${p.fileId}?key=${p.keyId}&env=${p.environmentId}`;

const dateFormat = new Intl.DateTimeFormat(undefined, { day: "numeric", month: "short", year: "numeric" });

/** Whole days since a time, or null when none is recorded. */
function daysSince(iso?: string): number | null {
  const t = Date.parse(iso ?? "");
  return Number.isNaN(t) ? null : Math.floor((Date.now() - t) / 86_400_000);
}

const unique = (names: string[]) => [...new Set(names)].join(", ");

/** Where secrets are old, shared, or break their key's rules. Locations only; values never reach this page. */
export function InsightsPage() {
  const [repo, setRepo] = useState("");
  const [days, setDays] = useState(DEFAULT_DAYS);
  const repos = useQuery({ queryKey: ["repos"], queryFn: api.repos });
  // Same query key as the Repos page, so they share the cache.
  const report = useQuery({
    queryKey: ["insights", repo, days],
    queryFn: () => api.insights({ repo: Number(repo) || undefined, days }),
  });
  const data = report.data;
  const pending = data?.pendingRequests ?? 0;

  return (
    <div className="mx-auto w-full max-w-7xl space-y-6 p-4 pb-16 md:p-6 md:pb-16">
      <PageHeader
        title="Insights"
        description="Secrets that have not changed for a while, secrets that hold the same value in more than one place, and values that break their key's rules. This page shows where, never the values."
        actions={
          pending > 0 && (
            <Link
              to="/requests"
              className="group flex max-w-md items-center gap-3 rounded-xl border border-protected bg-protected-soft px-4 py-3 text-sm"
            >
              <GitPullRequestArrow className="size-5 shrink-0 text-protected" aria-hidden="true" />
              <span className="min-w-0">
                <span className="block font-semibold group-hover:underline">
                  {pending === 1 ? "1 change request is" : `${pending} change requests are`} waiting for a decision.
                </span>{" "}
                <span className="block text-ink/80">Open requests to see the proposed changes to protected environments.</span>
              </span>
              <ChevronRight className="size-4 shrink-0" aria-hidden="true" />
            </Link>
          )
        }
      />
      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_17rem]">
        <section aria-label="Filters" className="flex flex-wrap items-end gap-3 rounded-xl border border-line bg-surface p-3 shadow-sm">
          <Field label="Repo" htmlFor="ins-repo">
            <Select
              id="ins-repo"
              className="w-56"
              value={repo}
              onValueChange={setRepo}
              options={[{ value: "", label: "All repos" }, ...(repos.data ?? []).map((r) => ({ value: r.id, label: r.name }))]}
            />
          </Field>
          <Field label="Unchanged for at least" htmlFor="ins-days">
            <Select id="ins-days" className="w-40" value={days} onValueChange={(v) => setDays(Number(v))} options={dayOptions} />
          </Field>
          <Button
            className="ml-auto"
            disabled={!repo && days === DEFAULT_DAYS}
            onClick={() => {
              setRepo("");
              setDays(DEFAULT_DAYS);
            }}
          >
            <RotateCcw aria-hidden="true" />
            Reset<span className="sr-only"> filters</span>
          </Button>
        </section>
        {report.isError && <ErrorNotice error={report.error} />}
        {data ? <Report data={data} /> : !report.isError && <Loading />}
      </div>
    </div>
  );
}

interface SectionMeta {
  id: string;
  Icon: LucideIcon;
  tone: string;
  title: string;
  count: number;
}

/** The glance column and the three sections, as items of the page grid. */
function Report({ data }: { data: Insights }) {
  const sections: Record<"problems" | "stale" | "reused", SectionMeta> = {
    problems: { id: "ins-problems", Icon: TriangleAlert, tone: "bg-missing-soft text-missing", title: "Values that break a rule", count: data.problems.length },
    stale: { id: "ins-stale", Icon: Clock, tone: "bg-accent-soft text-accent", title: `Secrets not changed in ${data.days} days`, count: data.stale.length },
    reused: { id: "ins-reused", Icon: Copy, tone: "bg-accent-soft text-accent", title: "Secrets used in more than one place", count: data.reused.length },
  };

  if (Object.values(sections).every((s) => s.count === 0)) {
    return (
      <div className="rounded-xl border border-line bg-surface shadow-sm">
        <EmptyState icon={CircleCheck} title="Nothing needs attention">
          No value breaks its key's rules, every secret was changed in the last {data.days} days, and no secret value is used in more than one place.
        </EmptyState>
      </div>
    );
  }

  return (
    <>
      <div className="lg:col-start-2 lg:row-span-2 lg:row-start-1">
        <Panel title="At a glance" className="lg:sticky lg:top-6 lg:max-h-[calc(100dvh-3.5rem-3rem)] lg:overflow-y-auto" bodyClassName="p-0">
          <ul className="divide-y divide-line">
            {Object.values(sections).map((s) => (
              <li key={s.id}>
                <a
                  href={`#${s.id}`}
                  onClick={(e) => {
                    e.preventDefault();
                    // Focus follows, so keyboard and screen reader users continue from the section.
                    const heading = document.getElementById(s.id);
                    heading?.scrollIntoView({ block: "start" });
                    heading?.focus({ preventScroll: true });
                  }}
                  className="flex items-center gap-3 px-4 py-3 text-sm hover:bg-surface-2"
                >
                  <SectionIcon meta={s} />
                  <span className="min-w-0">
                    <span className="block text-xl font-bold">{s.count}</span> {s.title}
                  </span>
                </a>
              </li>
            ))}
          </ul>
        </Panel>
      </div>
      <div className="min-w-0 space-y-6">
        <InsightSection
          meta={sections.problems}
          description="A required key with no value, or a value that does not match the key's pattern."
          unit={["value", "values"]}
          empty="Every value follows its key's rules."
          rows={data.problems}
          columns={problemColumns}
          to={cellLink}
        />
        <InsightSection
          meta={sections.stale}
          description="Rotating secrets on a schedule limits the damage when one leaks."
          unit={["secret", "secrets"]}
          empty={`Every secret was changed in the last ${data.days} days.`}
          rows={data.stale}
          columns={staleColumns}
          to={cellLink}
        />
        <InsightSection
          meta={sections.reused}
          description="Each row is one secret value held in several places. A leak in one place exposes the others."
          unit={["group", "groups"]}
          empty="No secret value is used in more than one place."
          rows={data.reused}
          columns={reusedColumns}
          to={(group) => cellLink(group[0])}
        />
      </div>
    </>
  );
}

function SectionIcon({ meta }: { meta: SectionMeta }) {
  return (
    <span aria-hidden="true" className={cn("flex size-9 shrink-0 items-center justify-center rounded-full", meta.tone)}>
      <meta.Icon className="size-5" />
    </span>
  );
}

interface Column<T> {
  label: string;
  cell: (row: T) => ReactNode;
}

/**
 * One section: a heading with its count, then a table whose rows open the
 * grid. On a phone the rows stack, each cell under its own label.
 */
function InsightSection<T>({
  meta,
  description,
  unit,
  empty,
  rows,
  columns,
  to,
}: {
  meta: SectionMeta;
  description: string;
  unit: [string, string];
  empty: string;
  rows: T[];
  columns: Column<T>[];
  to: (row: T) => string;
}) {
  const [all, setAll] = useState(false);
  const navigate = useNavigate();
  const shown = all ? rows : rows.slice(0, FIRST_ROWS);
  return (
    <section aria-labelledby={meta.id} className="rounded-xl border border-line bg-surface shadow-sm">
      <header className="flex items-start gap-3 border-b border-line px-4 py-3">
        <SectionIcon meta={meta} />
        <div className="min-w-0 flex-1">
          <h2 id={meta.id} tabIndex={-1} className="scroll-mt-20 font-bold md:scroll-mt-6">
            {meta.title} ({rows.length})
          </h2>
          <p className="mt-0.5 text-sm text-muted">{description}</p>
        </div>
        <span aria-hidden="true" className="shrink-0 rounded-full border border-line px-2.5 py-0.5 text-xs font-semibold whitespace-nowrap">
          {rows.length} {unit[rows.length === 1 ? 0 : 1]}
        </span>
      </header>
      {rows.length === 0 ? (
        <p className="flex items-center gap-2 px-4 py-4 text-sm text-muted">
          <CircleCheck className="size-4 shrink-0 text-accent" aria-hidden="true" />
          {empty}
        </p>
      ) : (
        <>
          {/* Explicit roles keep the table semantics when a phone restyles the rows as blocks. */}
          <table role="table" aria-labelledby={meta.id} className="w-full text-left text-sm max-sm:block">
            <thead role="rowgroup" className="bg-surface-2 text-xs max-sm:sr-only">
              <tr role="row">
                {columns.map((c) => (
                  <th key={c.label} role="columnheader" scope="col" className="px-4 py-2 font-semibold">
                    {c.label}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody role="rowgroup" className="divide-y divide-line max-sm:block">
              {shown.map((row) => (
                <tr
                  key={to(row)}
                  role="row"
                  // The links in the row serve the keyboard; a click anywhere else opens the first place too.
                  onClick={(e) => {
                    if (!(e.target as Element).closest("a")) navigate(to(row));
                  }}
                  className="cursor-pointer hover:bg-surface-2 focus-within:bg-surface-2 max-sm:flex max-sm:flex-col max-sm:gap-1 max-sm:px-4 max-sm:py-3"
                >
                  {columns.map((c, i) => (
                    <td key={c.label} role="cell" className="px-4 py-2.5 align-top max-sm:flex max-sm:items-baseline max-sm:gap-3 max-sm:p-0">
                      {i > 0 && (
                        <span aria-hidden="true" className="w-24 shrink-0 text-xs text-muted sm:hidden">
                          {c.label}
                        </span>
                      )}
                      {i < columns.length - 1 ? (
                        c.cell(row)
                      ) : (
                        <span className="flex flex-1 items-center gap-2">
                          {c.cell(row)}
                          <ChevronRight className="ml-auto size-4 shrink-0 text-muted" aria-hidden="true" />
                        </span>
                      )}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
          {rows.length > FIRST_ROWS && (
            <div className="border-t border-line px-2 py-1.5">
              <Button variant="ghost" size="sm" aria-expanded={all} onClick={() => setAll((v) => !v)}>
                {all ? "Show fewer" : `Show all ${rows.length} ${unit[1]}`}
              </Button>
            </div>
          )}
        </>
      )}
    </section>
  );
}

function KeyLink({ place: p }: { place: InsightPlace }) {
  return (
    <Link to={cellLink(p)} className="inline-flex items-start gap-1.5 font-mono font-semibold underline-offset-2 hover:underline">
      <KeyRound className="mt-0.5 size-3.5 shrink-0 text-muted" aria-hidden="true" />
      <span className="min-w-0">
        <Breakable text={keyLabel({ name: p.key })} />
      </span>
      <span className="sr-only">{` in ${p.environment}, ${p.fileName} in ${p.repoName}`}</span>
    </Link>
  );
}

function fileName(name: string) {
  return (
    <span className="inline-flex items-start gap-1.5">
      <FileText className="mt-0.5 size-3.5 shrink-0 text-muted" aria-hidden="true" />
      <span className="min-w-0 font-mono break-all">{name}</span>
    </span>
  );
}

const keyColumn: Column<InsightPlace> = { label: "Key", cell: (p) => <KeyLink place={p} /> };
const repoColumn: Column<InsightPlace> = { label: "Repo", cell: (p) => p.repoName };
const envColumn: Column<InsightPlace> = {
  label: "Environment",
  cell: (p) => <Badge className="rounded-full bg-surface-2 font-mono text-ink">{p.environment}</Badge>,
};
const fileColumn: Column<InsightPlace> = { label: "File", cell: (p) => fileName(p.fileName) };

const problemColumns: Column<InsightPlace>[] = [
  keyColumn,
  repoColumn,
  envColumn,
  {
    label: "Issue",
    cell: (p) =>
      p.problem === "required" ? (
        <span className="inline-flex items-start gap-1.5 text-missing">
          <CircleSlash className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />A value is required, but missing
        </span>
      ) : (
        <span className="inline-flex items-start gap-1.5 text-missing">
          <TriangleAlert className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
          The value does not match the pattern
        </span>
      ),
  },
  fileColumn,
];

const staleColumns: Column<InsightPlace>[] = [
  keyColumn,
  repoColumn,
  envColumn,
  {
    label: "Last changed",
    cell: (p) =>
      p.updatedAt ? (
        <span>
          <time dateTime={p.updatedAt}>{dateFormat.format(new Date(p.updatedAt))}</time>
          <span className="block text-xs text-muted">by {personName(p.updatedBy)}</span>
        </span>
      ) : (
        "Not recorded"
      ),
  },
  { label: "Days unchanged", cell: (p) => daysSince(p.updatedAt) ?? "Not recorded" },
  fileColumn,
];

const reusedColumns: Column<InsightPlace[]>[] = [
  { label: "Places with the same value", cell: (group) => <SharedPlaces group={group} /> },
  { label: "Repo", cell: (group) => unique(group.map((p) => p.repoName)) },
  { label: "File", cell: (group) => fileName(unique(group.map((p) => p.fileName))) },
];

/** One link per place that holds the shared value, with the file added when the places span files. */
function SharedPlaces({ group }: { group: InsightPlace[] }) {
  const files = new Set(group.map((p) => p.fileId)).size;
  const repos = new Set(group.map((p) => p.repoId)).size;
  return (
    <ul className="flex flex-wrap gap-1.5">
      {group.map((p) => (
        <li key={cellLink(p)}>
          <Link to={cellLink(p)} className="inline-block rounded-md border border-line bg-surface-2 px-2 py-0.5 text-xs underline-offset-2 hover:border-accent hover:underline">
            <span className="font-mono font-semibold">
              <Breakable text={keyLabel({ name: p.key })} />
            </span>{" "}
            in <span className="font-mono">{p.environment}</span>
            {files > 1 && <span className="text-muted">{` (${repos > 1 ? `${p.repoName}, ` : ""}${p.fileName})`}</span>}
          </Link>
        </li>
      ))}
    </ul>
  );
}
