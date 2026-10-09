import { Fragment, useEffect, useId, useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router";
import {
  Activity,
  ArrowDown,
  ArrowUpDown,
  Ban,
  Check,
  ChevronDown,
  ChevronRight,
  CircleX,
  Copy,
  Download,
  Eye,
  GitPullRequestArrow,
  History,
  KeyRound,
  LogIn,
  Pencil,
  PencilLine,
  Plus,
  SquarePen,
  Trash2,
  UserPlus,
  UserRound,
  type LucideIcon,
} from "lucide-react";
import { useMe } from "@/App";
import { PageHeader } from "@/components/PageHeader";
import { Badge, Button, ErrorNotice, Input, Loading, Panel, Select } from "@/components/ui";
import { api } from "@/lib/api";
import { actionLabel, activitySentence, auditActions, dayLabel, personName } from "@/lib/format";
import type { AuditRecord } from "@/lib/types";
import { cn } from "@/lib/utils";

/** Activity: everyone. Members see events in repos and their own account events. */
export function ActivityPage() {
  const me = useMe().data!;
  return (
    <div className="mx-auto w-full max-w-7xl space-y-6 p-4 pb-16 md:p-6 md:pb-16">
      <PageHeader
        title="Activity"
        description={
          me.role === "admin"
            ? "Every change, reveal, copy, export and sign-in, newest first."
            : "Every change, reveal, copy and export in the repos, and your own sign-ins, newest first."
        }
      />
      <ActivityLog isAdmin={me.role === "admin"} meId={me.id} />
    </div>
  );
}

type Tone = "accent" | "changed" | "protected" | "missing" | "neutral";

const tones: Record<Tone, string> = {
  accent: "border-accent/40 bg-accent-soft text-accent",
  changed: "border-changed/40 bg-changed-soft text-changed",
  protected: "border-protected/40 bg-protected-soft text-protected",
  missing: "border-missing/40 bg-missing-soft text-missing",
  neutral: "bg-surface-2",
};

/**
 * The badge for each action: a word and an icon, so colour is never the only
 * signal. Amber marks a value leaving envgrid or waiting for review, purple a
 * change, red a removal and teal something new or approved.
 */
const kinds: Record<string, [string, LucideIcon, Tone]> = {
  login: ["Signed in", LogIn, "neutral"],
  user_created: ["Created", UserPlus, "accent"],
  update_user: ["Updated", SquarePen, "changed"],
  create_repo: ["Created", Plus, "accent"],
  update_repo: ["Updated", SquarePen, "changed"],
  delete_repo: ["Deleted", Trash2, "missing"],
  create_environment: ["Created", Plus, "accent"],
  update_environment: ["Updated", SquarePen, "changed"],
  delete_environment: ["Deleted", Trash2, "missing"],
  reorder_environments: ["Reordered", ArrowUpDown, "changed"],
  create_file: ["Created", Plus, "accent"],
  update_file: ["Renamed", SquarePen, "changed"],
  delete_file: ["Deleted", Trash2, "missing"],
  create_key: ["Created", Plus, "accent"],
  update_key: ["Updated", SquarePen, "changed"],
  delete_key: ["Deleted", Trash2, "missing"],
  reorder_keys: ["Reordered", ArrowUpDown, "changed"],
  create_value: ["Set", PencilLine, "changed"],
  update_value: ["Changed", Pencil, "changed"],
  delete_value: ["Deleted", Trash2, "missing"],
  reveal_secret: ["Revealed", Eye, "protected"],
  export_file: ["Exported", Download, "protected"],
  copy_value: ["Copied", Copy, "protected"],
  request_change: ["Change requested", GitPullRequestArrow, "protected"],
  approve_change: ["Approved", Check, "accent"],
  reject_change: ["Rejected", CircleX, "missing"],
  cancel_change: ["Cancelled", Ban, "neutral"],
  restore_environment: ["Restored", History, "changed"],
  create_token: ["Token created", KeyRound, "accent"],
  delete_token: ["Token revoked", KeyRound, "missing"],
};

function ActionBadge({ action }: { action: string }) {
  const [word, Icon, tone] = kinds[action] ?? [actionLabel(action), Activity, "neutral"];
  return (
    <Badge className={cn("py-0.5", tones[tone])}>
      <Icon className="size-3.5" aria-hidden="true" />
      {word}
    </Badge>
  );
}

/** Detail fields the sentence and the breadcrumb already show. */
const shownElsewhere = new Set(["repo", "file", "key", "env", "email"]);

const detailLabels: Record<string, string> = {
  version: "Version",
  via: "Through",
  by: "Requested by",
  request: "Request number",
  delete: "Deletes the value",
  previousName: "Previous name",
  changed: "What changed",
  secret: "Secret",
  required: "Required",
  pattern: "Pattern",
  protected: "Protected",
  format: "Format",
  restoredTo: "Restored to",
  restoredFrom: "Restored from version",
  copiedFrom: "Copied from",
  target: "Copied to",
  token: "Token",
  prefix: "Token prefix",
  role: "Role",
  disabled: "Disabled",
  self: "Own account",
  ip: "IP address",
  keys: "Keys",
  envs: "Environments",
  added: "Added",
  updated: "Changed",
  keysCreated: "New keys",
};

/** The rest of an entry's detail as label and text pairs, in plain words. */
function detailPairs(r: AuditRecord): [string, string][] {
  return Object.entries(r.detail)
    .filter(([k]) => !shownElsewhere.has(k))
    .map(([k, v]) => {
      const words = k.replace(/([A-Z])/g, " $1").toLowerCase();
      const label = k === "keys" && !Array.isArray(v) ? "Number of keys" : (detailLabels[k] ?? words[0].toUpperCase() + words.slice(1));
      let text: string;
      if (Array.isArray(v)) text = v.map(String).join(", ");
      else if (typeof v === "boolean") text = v ? "Yes" : "No";
      else if (k === "restoredTo" && typeof v === "string") text = fullTime(v);
      else if (typeof v === "string") text = v.replace(/_/g, " ");
      else text = typeof v === "object" ? JSON.stringify(v) : String(v);
      return [label, text || "none"];
    });
}

const clock = (iso: string) => new Date(iso).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
const fullTime = (iso: string) => new Date(iso).toLocaleString(undefined, { dateStyle: "full", timeStyle: "medium" });
const shortDate = (iso: string) => new Date(iso).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" });

type FilterName = "action" | "user" | "repo" | "from" | "to";

/** A "2026-10-06" day from a date field as the start of that local day, in ISO. */
const dayStart = (day: string, plusDays = 0) => {
  const [y, m, d] = day.split("-").map(Number);
  return new Date(y, m - 1, d + plusDays).toISOString();
};

/** The log with its filters. With `repoId`, it shows that repo only and has no repo filter. */
export function ActivityLog({ isAdmin, meId, repoId }: { isAdmin: boolean; meId: number; repoId?: number }) {
  // Filters live in the address, so a filtered view can be shared and survives a reload.
  const [params, setParams] = useSearchParams();
  const userParam = params.get("user") ?? "";
  const filters = {
    action: params.get("action") ?? "",
    // Members cannot pick a user, so a shared link naming someone else does not apply to them.
    user: isAdmin || userParam === String(meId) ? userParam : "",
    repo: repoId ? String(repoId) : (params.get("repo") ?? ""),
    from: params.get("from") ?? "",
    to: params.get("to") ?? "",
  };
  const hasFilters = !!(filters.action || filters.user || (!repoId && filters.repo) || filters.from || filters.to);
  const setFilter = (name: FilterName, value: string) =>
    setParams(
      (p) => {
        const next = new URLSearchParams(p);
        if (value) next.set(name, value);
        else next.delete(name);
        return next;
      },
      { replace: true },
    );
  const clear = () => setParams({}, { replace: true });
  const quickClass = (on: boolean) => cn("h-8 shrink-0", on && "border-accent bg-accent-soft hover:bg-accent-soft");

  // Only admins can list users; members filter by action and repo.
  const users = useQuery({ queryKey: ["users"], queryFn: api.users, enabled: isAdmin });
  const repos = useQuery({ queryKey: ["repos"], queryFn: api.repos });
  const audit = useInfiniteQuery({
    queryKey: ["audit", filters],
    queryFn: ({ pageParam }) =>
      api.audit({
        action: filters.action || undefined,
        user: Number(filters.user) || undefined,
        repo: Number(filters.repo) || undefined,
        // Whole local days: from the start of From to the end of To.
        since: filters.from ? dayStart(filters.from) : undefined,
        until: filters.to ? dayStart(filters.to, 1) : undefined,
        cursor: pageParam,
        limit: 50,
      }),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
  const items = audit.data?.pages.flatMap((p) => p.items) ?? [];
  const repoNames = new Map((repos.data ?? []).map((r) => [r.id, r.name]));

  const days: [string, AuditRecord[]][] = [];
  for (const r of items) {
    const label = dayLabel(r.createdAt);
    const last = days[days.length - 1];
    if (last && last[0] === label) last[1].push(r);
    else days.push([label, [r]]);
  }

  // After loading older entries, keyboard focus moves to the first of them.
  const [focusId, setFocusId] = useState<number>();
  useEffect(() => {
    if (focusId) document.getElementById(`entry-${focusId}`)?.focus();
  }, [focusId]);
  const loadOlder = async () => {
    const res = await audit.fetchNextPage();
    const pages = res.data?.pages ?? [];
    setFocusId(pages[pages.length - 1]?.items[0]?.id);
  };

  const quick: { name: FilterName; value: string; label: string; hint: string; Icon: LucideIcon }[] = [
    { name: "user", value: String(meId), label: "My activity", hint: "Shows only your own actions.", Icon: UserRound },
    { name: "action", value: "reveal_secret", label: "Reveals", hint: "Shows only secrets that were revealed.", Icon: Eye },
    { name: "action", value: "export_file", label: "Exports", hint: "Shows only files that were exported.", Icon: Download },
  ];

  return (
    <div className="space-y-4">
      <section aria-label="Filters" className="flex flex-wrap items-center gap-2 rounded-xl border border-line bg-surface p-2.5 shadow-sm xl:flex-nowrap">
        <Select
          id="f-action"
          aria-label="Action"
          className="h-8 w-48 text-sm xl:min-w-24"
          value={filters.action}
          onValueChange={(v) => setFilter("action", v)}
          options={[{ value: "", label: "All actions" }, ...auditActions.map((a) => ({ value: a, label: actionLabel(a) }))]}
        />
        {isAdmin && (
          <Select
            id="f-user"
            aria-label="User"
            className="h-8 w-52 text-sm xl:min-w-24"
            value={filters.user}
            onValueChange={(v) => setFilter("user", v)}
            options={[{ value: "", label: "Everyone" }, ...(users.data ?? []).map((u) => ({ value: u.id, label: u.email }))]}
          />
        )}
        {!repoId && (
          <Select
            id="f-repo"
            aria-label="Repo"
            className="h-8 w-44 text-sm xl:min-w-24"
            value={filters.repo}
            onValueChange={(v) => setFilter("repo", v)}
            options={[{ value: "", label: "All repos" }, ...(repos.data ?? []).map((r) => ({ value: r.id, label: r.name }))]}
          />
        )}
        <label className="flex shrink-0 items-center gap-1.5 text-sm text-muted">
          From
          <Input type="date" className="h-8 w-36" value={filters.from} max={filters.to || undefined} onChange={(e) => setFilter("from", e.target.value)} />
        </label>
        <label className="flex shrink-0 items-center gap-1.5 text-sm text-muted">
          To
          <Input type="date" className="h-8 w-36" value={filters.to} min={filters.from || undefined} onChange={(e) => setFilter("to", e.target.value)} />
        </label>
        <span className="mx-1 hidden h-6 w-px shrink-0 bg-line sm:block" aria-hidden="true" />
        {quick.map((q) => {
          const on = filters[q.name] === q.value;
          return (
            <Button key={q.label} size="sm" aria-pressed={on} title={q.hint} className={cn("shrink-0", quickClass(on))} onClick={() => setFilter(q.name, on ? "" : q.value)}>
              {on ? <Check className="text-accent" /> : <q.Icon className="text-accent" />}
              {q.label}
            </Button>
          );
        })}
        {hasFilters && (
          <Button variant="ghost" size="sm" className="ml-auto h-8 shrink-0" onClick={clear}>
            Clear filters
          </Button>
        )}
      </section>

      <div className="min-w-0 space-y-4">
        {audit.isPending && (
          <Panel>
            <Loading />
          </Panel>
        )}
        {audit.isError && <ErrorNotice error={audit.error} />}
        {audit.data && items.length === 0 && (
          <Panel>
            <p className="py-6 text-center text-muted">No entries match these filters.</p>
          </Panel>
        )}
        {days.map(([day, list], i) => {
          // The last day may continue on the next page, so its count is what has loaded.
          const partial = i === days.length - 1 && audit.hasNextPage;
          return (
            <Panel
              key={day}
              bodyClassName="p-0"
              title={
                <>
                  {day}
                  {(day === "Today" || day === "Yesterday") && <span className="ml-2 font-normal text-muted">{shortDate(list[0].createdAt)}</span>}
                  {!partial && (
                  <Badge className="ml-2.5 align-text-bottom">
                    {list.length} {list.length === 1 ? "entry" : "entries"}

                  </Badge>
                  )}
                </>
              }
            >
              <ol className="divide-y divide-line">
                {list.map((r) => (
                  <Entry key={r.id} record={r} repoName={r.repoId ? repoNames.get(r.repoId) : undefined} />
                ))}
              </ol>
            </Panel>
          );
        })}
        {audit.hasNextPage && (
          // Not a scroll anchor, so new entries load below the ones on screen instead of pushing the page down.
          <div className="flex justify-center [overflow-anchor:none]">
            <Button onClick={loadOlder} disabled={audit.isFetchingNextPage}>
              <ArrowDown aria-hidden="true" />
              {audit.isFetchingNextPage ? "Loading older entries…" : "Load older entries"}
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}

function Entry({ record: r, repoName }: { record: AuditRecord; repoName?: string }) {
  const [open, setOpen] = useState(false);
  const id = useId();
  const s = activitySentence(r);
  const d = r.detail;
  const str = (k: string) => (typeof d[k] === "string" ? (d[k] as string) : "");
  const who = r.user ? personName(r.user) : "A removed user";

  // Links stop at whatever this entry deleted.
  const repoLink = r.repoId && r.action !== "delete_repo" ? `/repos/${r.repoId}` : "";
  const fileLink = repoLink && r.fileId && r.action !== "delete_file" ? `${repoLink}/files/${r.fileId}` : "";
  const cellLink =
    fileLink && r.keyId && r.action !== "delete_key" ? `${fileLink}?key=${r.keyId}${r.environmentId ? `&env=${r.environmentId}` : ""}` : "";
  // An entry with a key names that key; otherwise the target may be the file.
  const targetLink = r.keyId ? cellLink : s.target && s.target === str("file") ? fileLink : "";
  const crumbs = [
    { label: "Repo", text: str("repo") || repoName, to: repoLink },
    { label: "File", text: str("file"), to: targetLink === fileLink ? "" : fileLink },
    { label: "Environment", text: str("env"), to: "" },
  ].filter((c) => c.text);
  const pairs: [string, string][] = [["Time", fullTime(r.createdAt)], ...detailPairs(r)];
  const named = Object.values(d).includes(s.target);

  return (
    <li id={`entry-${r.id}`} tabIndex={-1} className="flex items-start gap-3 px-4 py-3 outline-offset-[-2px]">
      <time dateTime={r.createdAt} title={fullTime(r.createdAt)} className="w-16 shrink-0 pt-1.5 text-xs text-muted tabular-nums sm:w-20 sm:text-sm">
        {clock(r.createdAt)}
      </time>
      <span className="flex size-8 shrink-0 items-center justify-center rounded-full bg-accent-soft text-sm font-bold text-accent" aria-hidden="true">
        {r.user ? who.slice(0, 1).toUpperCase() : "?"}
      </span>
      <div className="min-w-0 flex-1">
        <div className="sm:flex sm:items-start sm:gap-3">
          <div className="min-w-0 flex-1 pt-1">
            <p id={`${id}-text`}>
              <span className="font-semibold">{who}</span> {s.verb}{" "}
              {s.target &&
                (targetLink ? (
                  <Link to={targetLink} className="font-mono font-semibold text-accent underline underline-offset-2 [overflow-wrap:anywhere] hover:decoration-2">
                    {s.target}
                  </Link>
                ) : (
                  <span className={cn(named && "font-mono", "[overflow-wrap:anywhere]")}>{s.target}</span>
                ))}{" "}
              {s.where}
            </p>
            {crumbs.length > 0 && (
              <p className="mt-0.5 flex flex-wrap items-center gap-x-1 text-xs text-muted">
                {crumbs.map((c, i) => (
                  <Fragment key={c.label}>
                    {i > 0 && <ChevronRight className="size-3.5 shrink-0" aria-hidden="true" />}
                    <span className="sr-only">{c.label} </span>
                    {c.to ? (
                      <Link to={c.to} className="underline underline-offset-2 [overflow-wrap:anywhere] hover:text-ink">
                        {c.text}
                      </Link>
                    ) : (
                      <span className="[overflow-wrap:anywhere]">{c.text}</span>
                    )}
                  </Fragment>
                ))}
              </p>
            )}
          </div>
          <div className="mt-1.5 shrink-0 sm:mt-1">
            <ActionBadge action={r.action} />
          </div>
        </div>
        <dl
          id={`${id}-detail`}
          hidden={!open}
          className="mt-3 grid grid-cols-[max-content_minmax(0,1fr)] gap-x-4 gap-y-1 rounded-md bg-surface-2 px-3 py-2 text-sm"
        >
          {pairs.map(([label, text]) => (
            <Fragment key={label}>
              <dt className="text-muted">{label}</dt>
              <dd className="[overflow-wrap:anywhere]">{text}</dd>
            </Fragment>
          ))}
        </dl>
      </div>
      <Button
        variant="ghost"
        size="icon"
        aria-label="Details"
        aria-describedby={`${id}-text`}
        aria-expanded={open}
        aria-controls={`${id}-detail`}
        onClick={() => setOpen((o) => !o)}
      >
        <ChevronDown className={cn("transition-transform", open && "rotate-180")} aria-hidden="true" />
      </Button>
    </li>
  );
}
