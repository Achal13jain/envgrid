import { useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { ArrowLeftRight, ArrowRight, ChevronDown, CircleSlash, Copy, Equal, EqualNot, Eye, EyeOff, Lock, Search } from "lucide-react";
import { useParams, useSearchParams } from "react-router";
import { useMe } from "@/App";
import { useDocumentTitle } from "@/components/PageHeader";
import {
  Breakable,
  Button,
  EmptyState,
  ErrorNotice,
  Field,
  Input,
  Loading,
  Menu,
  MenuContent,
  MenuItem,
  MenuTrigger,
  Panel,
  Select,
} from "@/components/ui";
import { api } from "@/lib/api";
import { errorMessage, useAnnounce } from "@/lib/announce";
import { cellView, diffSegments, MASK } from "@/lib/format";
import type { Cell, CompareItem, Comparison, Environment, User } from "@/lib/types";
import { cn } from "@/lib/utils";

type Status = "missingInA" | "missingInB" | "different" | "same";
const statuses: Status[] = ["missingInA", "missingInB", "different", "same"];

/** What the "Show" dropdown can select. "attention" (the default) hides matching keys, which are rarely what you came to see. */
type View = "attention" | Status | "all";
const viewIncludes = (view: View, status: Status) => view === "all" || view === status || (view === "attention" && status !== "same");

export function ComparePage() {
  const fileId = Number(useParams().fileId);
  const user = useMe().data!;
  const grid = useQuery({ queryKey: ["grid", fileId], queryFn: () => api.grid(fileId) });
  const [params, setParams] = useSearchParams();
  const [reveal, setReveal] = useState(false);
  const [view, setView] = useState<View>("all");
  const [query, setQuery] = useState("");
  useDocumentTitle(grid.data ? `Compare ${grid.data.file.name}` : "Compare");

  const envs = grid.data?.environments ?? [];
  const a = Number(params.get("a")) || envs[0]?.id || 0;
  const b = Number(params.get("b")) || envs[envs.length - 1]?.id || 0;
  const cmp = useQuery({
    queryKey: ["compare", fileId, a, b, reveal],
    queryFn: () => api.compare(fileId, a, b, reveal),
    enabled: !!a && !!b && a !== b,
  });

  // One list in the file's key order, each row tagged with its condition.
  const rows = useMemo(() => {
    if (!cmp.data) return [];
    const all = statuses.flatMap((s) => (cmp.data as Comparison)[s].map((item) => ({ status: s, item })));
    return all.sort((x, y) => x.item.key.position - y.item.key.position || x.item.key.id - y.item.key.id);
  }, [cmp.data]);

  if (grid.isError) return <ErrorNotice error={grid.error} />;
  if (!grid.data) return <Loading />;
  const file = grid.data.file;
  const set = (next: { a?: number; b?: number }) => setParams({ a: String(next.a ?? a), b: String(next.b ?? b) }, { replace: true });
  const envA = envs.find((e) => e.id === a);
  const envB = envs.find((e) => e.id === b);
  const q = query.trim().toLowerCase();
  const visible = rows.filter((r) => viewIncludes(view, r.status) && (!q || r.item.key.name.toLowerCase().includes(q)));
  const label = (s: Status) =>
    s === "missingInA" ? `Missing in ${envA?.name}` : s === "missingInB" ? `Missing in ${envB?.name}` : s === "different" ? "Different" : "Same";

  return (
    <div className="mx-auto w-full max-w-7xl space-y-4 p-4 pb-16">
      <Panel>
        <div className="flex flex-wrap items-start justify-between gap-3">
          <h1 className="text-2xl font-bold">
            Compare <span className="font-mono">{file.name}</span>
          </h1>
          <Button aria-pressed={reveal} onClick={() => setReveal((r) => !r)}>
            {reveal ? <EyeOff /> : <Eye />}
            {reveal ? "Hide secret values" : "Show secret values"}
          </Button>
        </div>
        <div className="mt-4 flex flex-wrap items-end gap-3">
          <Field label="Environment A" htmlFor="cmp-a">
            <Select
              id="cmp-a"
              className="w-44 font-mono"
              itemClassName="font-mono"
              value={a}
              onValueChange={(v) => set({ a: Number(v) })}
              options={envs.map((e) => ({ value: e.id, label: e.name }))}
            />
          </Field>
          <Button aria-label="Swap A and B" title="Swap A and B" size="icon" className="mb-0.5 size-9" onClick={() => set({ a: b, b: a })}>
            <ArrowLeftRight />
          </Button>
          <Field label="Environment B" htmlFor="cmp-b">
            <Select
              id="cmp-b"
              className="w-44 font-mono"
              itemClassName="font-mono"
              value={b}
              onValueChange={(v) => set({ b: Number(v) })}
              options={envs.map((e) => ({ value: e.id, label: e.name }))}
            />
          </Field>
        </div>
      </Panel>

      {a === b && (
        <Panel>
          <EmptyState icon={ArrowLeftRight} title="Pick two different environments">Choose another environment for A or B.</EmptyState>
        </Panel>
      )}
      {cmp.isError && <ErrorNotice error={cmp.error} />}
      {a !== b && cmp.isPending && <Loading label="Comparing" />}

      {cmp.data && envA && envB && (
        <section aria-labelledby="results-title" className="overflow-hidden rounded-xl border border-line bg-surface shadow-sm">
          <div className="flex flex-wrap items-center gap-x-4 gap-y-3 border-b border-line px-4 py-3">
            <h2 id="results-title" className="sr-only">
              Comparison results
            </h2>
            <div className="flex items-center gap-2">
              <label htmlFor="cmp-view" className="text-sm font-bold">
                Show
              </label>
              <Select
                id="cmp-view"
                className="w-80 max-w-full"
                value={view}
                onValueChange={(v) => setView(v as View)}
                options={[
                  { value: "all", label: `All keys (${rows.length})` },
                  { value: "attention", label: `Needs attention: missing or different (${rows.length - cmp.data.same.length})` },
                  ...statuses.map((s) => ({ value: s, label: `${label(s)} (${cmp.data[s].length})` })),
                ]}
              />
            </div>
            <div className="relative ml-auto w-full sm:w-56">
              <Search className="pointer-events-none absolute top-2.5 left-2.5 size-4 text-muted" aria-hidden="true" />
              <Input type="search" aria-label="Filter keys" placeholder="Filter keys" value={query} onChange={(e) => setQuery(e.target.value)} className="pl-8" />
            </div>
          </div>
          <p className="border-b border-line px-4 py-2 text-xs text-muted" aria-live="polite">
            Showing {visible.length} of {rows.length} keys.
            {cmp.data.different.length > 0 && " In different values, the part that changed is underlined."} Copies run on the server, so values never pass
            through your browser.
          </p>
          {visible.length === 0 ? (
            <p className="p-6 text-center text-muted">No keys match. Choose another option under Show, or clear the key filter.</p>
          ) : (
            <div className="relative overflow-x-auto">
              <table className="w-full min-w-[56rem] table-fixed border-separate border-spacing-0 text-left text-sm">
                <caption className="sr-only">
                  Comparison of {envA.name} (A) and {envB.name} (B), in key order
                </caption>
                <colgroup>
                  <col className="w-[22%]" />
                  <col className="w-40" />
                  <col />
                  <col />
                  <col className="w-60" />
                </colgroup>
                <thead className="bg-surface-2">
                  <tr>
                    <th scope="col" className="border-b border-line px-3 py-2">
                      Key
                    </th>
                    <th scope="col" className="border-b border-line px-3 py-2">
                      Status
                    </th>
                    <th scope="col" className="border-b border-l border-line px-3 py-2 font-mono">
                      {envA.name} <span className="font-sans font-normal text-muted">(A)</span>
                    </th>
                    <th scope="col" className="border-b border-l border-line px-3 py-2 font-mono">
                      {envB.name} <span className="font-sans font-normal text-muted">(B)</span>
                    </th>
                    <th scope="col" className="border-b border-l border-line px-3 py-2">
                      Copy
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {visible.map(({ status, item }, i) => (
                    <Row key={item.key.id} status={status} label={label(status)} item={item} a={envA} b={envB} user={user} stripe={i % 2 === 1} />
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      )}
    </div>
  );
}

const statusStyle: Record<Status, string> = {
  missingInA: "border-missing bg-missing-soft text-missing",
  missingInB: "border-missing bg-missing-soft text-missing",
  different: "border-changed bg-changed-soft text-changed",
  same: "border-line bg-surface-2 text-ink",
};

function StatusIcon({ status }: { status: Status }) {
  if (status === "different") return <EqualNot className="size-3.5" aria-hidden="true" />;
  if (status === "same") return <Equal className="size-3.5" aria-hidden="true" />;
  return <CircleSlash className="size-3.5" aria-hidden="true" />;
}

function Row({
  status,
  label,
  item,
  a,
  b,
  user,
  stripe,
}: {
  status: Status;
  label: string;
  item: CompareItem;
  a: Environment;
  b: Environment;
  user: User;
  stripe: boolean;
}) {
  const announce = useAnnounce();
  const copy = useMutation({
    mutationFn: ({ from, to }: { from: Environment; to: Environment }) => api.copyValue(item.key.id, to.id, from.id),
    onSuccess: (_, { from, to }) => announce(`Copied ${item.key.name} from ${from.name} to ${to.name}.`),
    onError: (e) => announce(errorMessage(e), "error"),
  });
  const bothSet = item.a.present && item.b.present;
  const va = cellView(item.a).kind === "value" || cellView(item.a).kind === "empty" ? (item.a.value ?? "") : null;
  const vb = cellView(item.b).kind === "value" || cellView(item.b).kind === "empty" ? (item.b.value ?? "") : null;
  const diff = bothSet && va !== null && vb !== null && va !== vb ? diffSegments(va, vb) : null;
  const name = item.key.name === "__raw__" ? "File body" : item.key.name;
  const bg = stripe ? "bg-surface-alt" : "bg-surface";

  const blocked = (to: Environment) => to.isProtected && user.role !== "admin";
  const copyButton = (from: Environment, to: Environment) => {
    const what = `Copy the ${from.name} value of ${name} to ${to.name}`;
    return (
      <Button
        size="sm"
        disabled={blocked(to) || copy.isPending}
        aria-label={what}
        title={blocked(to) ? `${to.name} is protected: only admins can change it.` : what}
        onClick={() => copy.mutate({ from, to })}
      >
        {blocked(to) ? <Lock /> : <ArrowRight />}
        Copy {from.name} value to {to.name}
      </Button>
    );
  };
  // Two values differ: either direction may be right, so the person picks one by name.
  const copyMenu = (
    <Menu>
      <MenuTrigger asChild>
        <Button size="sm" disabled={copy.isPending} aria-label={`Copy a value of ${name}`}>
          <Copy /> Copy a value <ChevronDown />
        </Button>
      </MenuTrigger>
      <MenuContent align="end">
        {[
          [a, b],
          [b, a],
        ].map(([from, to]) => (
          <MenuItem key={from.id} disabled={blocked(to)} onSelect={() => copy.mutate({ from, to })}>
            {blocked(to) ? <Lock /> : <ArrowRight />} Copy {from.name} value to {to.name}
            {blocked(to) && " (protected)"}
          </MenuItem>
        ))}
      </MenuContent>
    </Menu>
  );

  return (
    <tr className="align-top">
      <th scope="row" className={cn("border-b border-line px-3 py-2.5 font-normal", bg)}>
        <span className="font-mono font-semibold [overflow-wrap:anywhere]">
          <Breakable text={name} />
        </span>
        {item.key.isSecret && <span className="ml-1.5 text-xs text-muted">secret</span>}
      </th>
      <td className={cn("border-b border-line px-3 py-2.5", bg)}>
        <span className={cn("inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-semibold whitespace-nowrap", statusStyle[status])}>
          <StatusIcon status={status} />
          {label}
        </span>
      </td>
      <Side cell={item.a} bg={bg} highlight={diff ? { prefix: diff.prefix, mid: diff.a, suffix: diff.suffix } : null} />
      <Side cell={item.b} bg={bg} highlight={diff ? { prefix: diff.prefix, mid: diff.b, suffix: diff.suffix } : null} />
      <td className={cn("border-b border-l border-line px-3 py-2", bg)}>
        {status === "missingInB" && copyButton(a, b)}
        {status === "missingInA" && copyButton(b, a)}
        {status === "different" && copyMenu}
        {status === "same" && <span className="text-xs text-muted">Nothing to copy</span>}
      </td>
    </tr>
  );
}

function Side({ cell, bg, highlight }: { cell: Cell; bg: string; highlight: { prefix: string; mid: string; suffix: string } | null }) {
  const view = cellView(cell);
  return (
    <td className={cn("min-w-0 border-b border-l border-line px-3 py-2.5", view.kind === "missing" ? "hatch-missing" : bg)}>
      {view.kind === "missing" && (
        <span className="inline-flex items-center gap-1.5 text-sm font-semibold text-missing">
          <CircleSlash className="size-4" aria-hidden="true" /> missing
        </span>
      )}
      {view.kind === "masked" && (
        <span className="text-muted">
          <span className="font-mono tracking-widest" aria-hidden="true">
            {MASK}
          </span>
          <span className="sr-only">secret value, hidden</span>
        </span>
      )}
      {view.kind === "empty" && <span className="text-sm text-muted italic">empty</span>}
      {view.kind === "value" && (
        <span className="font-mono text-sm whitespace-pre-wrap [overflow-wrap:anywhere]">
          {highlight ? (
            <>
              <Breakable text={highlight.prefix} />
              <mark className="rounded-sm bg-changed-soft px-0.5 text-changed underline decoration-dotted underline-offset-2">{highlight.mid}</mark>
              <Breakable text={highlight.suffix} />
            </>
          ) : (
            <Breakable text={view.text} />
          )}
        </span>
      )}
    </td>
  );
}
