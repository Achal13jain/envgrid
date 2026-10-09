import { useRef, useState, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, Ban, Check, CircleCheck, CircleX, Clock, Eye, Inbox, Lock, Search, UserRound, X } from "lucide-react";
import { Link } from "react-router";
import { useMe } from "@/App";
import { useDocumentTitle } from "@/components/PageHeader";
import {
  Badge,
  Breakable,
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
  EmptyState,
  ErrorNotice,
  Field,
  InlineMessage,
  Input,
  Loading,
  Panel,
  Select,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  Textarea,
} from "@/components/ui";
import { api } from "@/lib/api";
import { errorMessage, useAnnounce } from "@/lib/announce";
import { cellView, diffSegments, keyLabel, MASK, personName, relativeTime, type CellView } from "@/lib/format";
import type { ChangeRequest, RequestStatus, User, UserRef } from "@/lib/types";
import { cn } from "@/lib/utils";

type Tab = RequestStatus | "all";

const tabs: [Tab, string][] = [
  ["all", "All"],
  ["pending", "Waiting"],
  ["approved", "Approved"],
  ["rejected", "Rejected"],
  ["cancelled", "Cancelled"],
];

// Every status has a word and an icon as well as a colour.
const statusInfo: Record<RequestStatus, { label: string; Icon: typeof Clock; className: string; meaning: string }> = {
  pending: {
    label: "Waiting",
    Icon: Clock,
    className: "border-protected bg-protected-soft text-protected",
    meaning: "It is waiting for an admin to approve or reject it.",
  },
  approved: {
    label: "Approved",
    Icon: CircleCheck,
    className: "border-accent bg-accent-soft text-accent",
    meaning: "An admin approved it and the change was applied.",
  },
  rejected: {
    label: "Rejected",
    Icon: CircleX,
    className: "border-missing bg-missing-soft text-missing",
    meaning: "An admin rejected it, so nothing changed.",
  },
  cancelled: { label: "Cancelled", Icon: Ban, className: "bg-surface-2", meaning: "It was cancelled before a decision, so nothing changed." },
};

const keyOf = (r: ChangeRequest) => keyLabel({ name: r.keyName });
const dateTime = (iso: string) => new Date(iso).toLocaleString([], { dateStyle: "medium", timeStyle: "short" });

/** Change requests: proposals for protected environments, decided by admins. */
export function RequestsPage() {
  useDocumentTitle("Change requests");
  const me = useMe().data!;
  const [tab, setTab] = useState<Tab>("pending");
  const [repo, setRepo] = useState("");
  const [mineOnly, setMineOnly] = useState(false);
  const [text, setText] = useState("");
  const [openId, setOpenId] = useState<number | null>(null);
  const repos = useQuery({ queryKey: ["repos"], queryFn: api.repos });
  const waiting = useQuery({ queryKey: ["requests-count"], queryFn: api.pendingCount }).data?.pending ?? 0;
  // Everything loads once and filters here, so every tab's count is known.
  const list = useQuery({
    queryKey: ["requests", "all", repo],
    queryFn: () => api.changeRequests({ status: "all", repo: Number(repo) || undefined }),
  });
  // Requests are only made for protected environments, but one may have been unprotected since.
  const locked = new Set(repos.data?.flatMap((r) => r.coverage.filter((c) => c.isProtected).map((c) => c.environmentId)));

  const q = text.trim().toLowerCase();
  const matching = (list.data ?? []).filter(
    (r) =>
      (!mineOnly || r.createdBy?.id === me.id) &&
      [keyOf(r), r.fileName, r.repoName, r.environmentName, r.createdBy?.name, r.createdBy?.email].some((s) => s?.toLowerCase().includes(q)),
  );
  const count = (t: Tab) => (t === "all" ? matching.length : matching.filter((r) => r.status === t).length);
  const shown = tab === "all" ? matching : matching.filter((r) => r.status === tab);
  const open = list.data?.find((r) => r.id === openId);
  const openButton = (r: ChangeRequest, className?: string) => {
    const verb = r.status === "pending" ? "Review" : "View";
    return (
      <Button size="sm" className={className} aria-label={`${verb} the request for ${keyOf(r)} in ${r.environmentName}`} onClick={() => setOpenId(r.id)}>
        {verb}
      </Button>
    );
  };

  return (
    <div className="mx-auto w-full max-w-7xl space-y-5 p-4 pb-16 md:p-6 md:pb-16">
      <div>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
          <h1 tabIndex={-1} className="text-2xl font-bold tracking-tight sm:text-3xl">
            Change requests
          </h1>
          {waiting > 0 && (
            <span className="rounded-full border border-protected bg-protected-soft px-2.5 py-0.5 text-sm font-semibold text-protected">
              {waiting} waiting
            </span>
          )}
        </div>
        <p className="mt-1.5 max-w-3xl text-muted">
          {me.role === "admin"
            ? "Members propose changes to protected environments here. Approving applies the change at once; it is refused if the value changed since the request was made."
            : "Your proposals for protected environments wait here until an admin approves or rejects them. You can cancel your own while they wait."}
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-2 rounded-xl border border-line bg-surface p-2 shadow-sm">
        <Button aria-pressed={mineOnly} className="aria-pressed:border-accent aria-pressed:bg-accent-soft" onClick={() => setMineOnly((v) => !v)}>
          {mineOnly ? <Check /> : <UserRound />} My requests
        </Button>
        <div className="flex items-center gap-2">
          <label htmlFor="req-repo" className="text-sm font-semibold">
            Repo
          </label>
          <Select
            id="req-repo"
            className="w-36 sm:w-48"
            value={repo}
            onValueChange={setRepo}
            options={[{ value: "", label: "All repos" }, ...(repos.data ?? []).map((r) => ({ value: r.id, label: r.name }))]}
          />
        </div>
        <div className="relative min-w-48 flex-1">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted" aria-hidden="true" />
          <Input
            type="search"
            aria-label="Filter requests by key, file, repo, environment or person"
            placeholder="Filter requests"
            className="pl-8"
            value={text}
            onChange={(e) => setText(e.target.value)}
          />
        </div>
      </div>

      <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)}>
        {/* Scrolls sideways on a phone rather than wrapping. */}
        <div className="overflow-x-auto">
          <TabsList aria-label="Status" className="w-max min-w-full">
            {tabs.map(([value, label]) => (
              <TabsTrigger key={value} value={value} className="flex items-center gap-1.5">
                {label}
                {list.data && <span className="rounded-full bg-surface-2 px-1.5 text-xs leading-5 tabular-nums">{count(value)}</span>}
              </TabsTrigger>
            ))}
          </TabsList>
        </div>
        <TabsContent value={tab}>
          {list.isError ? (
            <ErrorNotice error={list.error} />
          ) : list.isPending ? (
            <Loading />
          ) : shown.length === 0 ? (
            <Panel>
              <EmptyState icon={Inbox} title={tab === "pending" ? "Nothing is waiting" : "No requests match"}>
                When a member changes a value in a protected environment, the proposal appears here for an admin to approve or reject.
                {(q || mineOnly || repo || tab !== "pending") && " Try another status, repo or filter."}
              </EmptyState>
            </Panel>
          ) : (
            <>
              <div className="hidden overflow-x-auto rounded-xl border border-line bg-surface shadow-sm md:block">
                <table className="w-full min-w-[60rem] text-left text-sm">
                  <caption className="sr-only">Change requests: {tabs.find(([t]) => t === tab)?.[1]}</caption>
                  <thead className="bg-surface-2 text-xs">
                    <tr>
                      <th scope="col" className="px-3 py-2">Key and environment</th>
                      <th scope="col" className="px-3 py-2">Repo and file</th>
                      <th scope="col" className="px-3 py-2">Requested by</th>
                      <th scope="col" className="px-3 py-2">Requested</th>
                      <th scope="col" className="w-[30%] px-3 py-2">Change</th>
                      <th scope="col" className="px-3 py-2">Status</th>
                      <th scope="col" className="px-3 py-2 text-right">Action</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-line">
                    {shown.map((r) => (
                      <tr key={r.id} className="align-top">
                        <th scope="row" className="px-3 py-3 font-normal">
                          <span className="flex flex-wrap items-center gap-1.5">
                            <span className="font-mono font-semibold [overflow-wrap:anywhere]">
                              <Breakable text={keyOf(r)} />
                            </span>
                            <EnvPill name={r.environmentName} locked={locked.has(r.environmentId)} />
                          </span>
                        </th>
                        <td className="px-3 py-3 [overflow-wrap:anywhere]">
                          <div>{r.repoName}</div>
                          <div className="text-muted">{r.fileName}</div>
                        </td>
                        <td className="px-3 py-3">
                          <Person user={r.createdBy} />
                        </td>
                        <td className="px-3 py-3 whitespace-nowrap">
                          <When iso={r.createdAt} />
                        </td>
                        <td className="px-3 py-3">
                          <ChangeSummary r={r} />
                        </td>
                        <td className="px-3 py-3">
                          <StatusBadge status={r.status} />
                        </td>
                        <td className="px-3 py-3 text-right">{openButton(r)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <ul className="space-y-3 md:hidden" aria-label="Change requests">
                {shown.map((r) => (
                  <li key={r.id} className="relative rounded-xl border border-line bg-surface p-3 text-sm shadow-sm">
                    <div className="flex items-start gap-2">
                      <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
                        <span className="font-mono font-semibold [overflow-wrap:anywhere]">
                          <Breakable text={keyOf(r)} />
                        </span>
                        <EnvPill name={r.environmentName} locked={locked.has(r.environmentId)} />
                      </div>
                      <StatusBadge status={r.status} />
                    </div>
                    <p className="mt-1 text-muted [overflow-wrap:anywhere]">
                      {r.fileName} in {r.repoName}
                    </p>
                    <p className="text-muted">
                      Asked by {personName(r.createdBy)} {relativeTime(r.createdAt)}
                    </p>
                    <div className="mt-2">
                      <ChangeSummary r={r} />
                    </div>
                    {/* The button covers the card, so a tap anywhere opens it. */}
                    <div className="mt-3 flex justify-end">{openButton(r, "after:absolute after:inset-0 after:rounded-xl")}</div>
                  </li>
                ))}
              </ul>
            </>
          )}
        </TabsContent>
      </Tabs>
      {open && <RequestDrawer key={open.id} request={open} me={me} locked={locked.has(open.environmentId)} onClose={() => setOpenId(null)} />}
    </div>
  );
}

function StatusBadge({ status }: { status: RequestStatus }) {
  const { label, Icon, className } = statusInfo[status];
  return (
    <Badge className={className}>
      <Icon className="size-3.5" aria-hidden="true" />
      {label}
    </Badge>
  );
}

function EnvPill({ name, locked }: { name: string; locked: boolean }) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full border px-2 py-px font-mono text-xs font-normal whitespace-nowrap",
        locked ? "border-protected bg-protected-soft text-protected" : "border-line text-muted",
      )}
    >
      <span className="sr-only">in </span>
      {locked && <Lock className="size-3" aria-hidden="true" />}
      {name}
      {locked && <span className="sr-only">, protected</span>}
    </span>
  );
}

function Person({ user }: { user: UserRef | null }) {
  const name = personName(user);
  return (
    <div className="flex min-w-0 items-center gap-2">
      <span className="flex size-8 shrink-0 items-center justify-center rounded-full bg-accent-soft text-xs font-bold text-accent" aria-hidden="true">
        {name[0].toUpperCase()}
      </span>
      <div className="min-w-0 [overflow-wrap:anywhere]">
        <div className="font-semibold">{name}</div>
        {user?.name.trim() && <div className="text-xs text-muted">{user.email}</div>}
      </div>
    </div>
  );
}

function When({ iso }: { iso: string }) {
  return (
    <time dateTime={iso}>
      <span className="block">{dateTime(iso)}</span>
      <span className="block text-xs text-muted">{relativeTime(iso)}</span>
    </time>
  );
}

type Side = { kind: CellView["kind"] | "delete"; text: string; mark?: [string, string, string] };

/** Both sides of a change, with the changed words marked when both are plain, non-secret values. */
function sides(r: ChangeRequest, revealed: string | null): [Side, Side] {
  const now: Side = cellView(r.current);
  const next: Side = r.delete
    ? { kind: "delete", text: "Delete the value" }
    : cellView({ present: true, masked: r.masked, value: r.proposed }, revealed ?? undefined);
  if (!r.isSecret && now.kind === "value" && next.kind === "value" && now.text !== next.text) {
    const d = diffSegments(now.text, next.text);
    now.mark = [d.prefix, d.a, d.suffix];
    next.mark = [d.prefix, d.b, d.suffix];
  }
  return [now, next];
}

function SideText({ side }: { side: Side }) {
  switch (side.kind) {
    case "value":
      if (!side.mark) return <span className="font-mono">{side.text}</span>;
      return (
        <span className="font-mono">
          {side.mark[0]}
          {side.mark[1] && (
            <mark className="rounded-sm bg-changed-soft px-0.5 text-changed underline decoration-dotted underline-offset-2">
              <span className="sr-only">changed: </span>
              {side.mark[1]}
            </mark>
          )}
          {side.mark[2]}
        </span>
      );
    case "masked":
      return (
        <>
          <span className="font-mono tracking-widest" aria-hidden="true">
            {MASK}
          </span>
          <span className="sr-only">secret, hidden</span>
        </>
      );
    case "empty":
      return <span className="text-muted italic">{side.text}</span>;
    default:
      return <span className="font-semibold text-missing">{side.text}</span>;
  }
}

/** Now and proposed in a row, two lines at most each; the drawer shows them whole. */
function ChangeSummary({ r }: { r: ChangeRequest }) {
  const [now, next] = sides(r, null);
  const col = (label: string, side: Side) => (
    <div className="min-w-0">
      <span className="block text-xs text-muted">{label}</span>
      <span className="line-clamp-2 [overflow-wrap:anywhere]">
        <SideText side={side} />
      </span>
    </div>
  );
  return (
    <div className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-start gap-2">
      {col("Now", now)}
      <ArrowRight className="mt-4 size-4 text-muted" aria-hidden="true" />
      {col("Proposed", next)}
    </div>
  );
}

function ValueBox({ label, side, action }: { label: string; side: Side; action?: ReactNode }) {
  return (
    <div className="min-w-0 rounded-lg border border-line bg-surface-alt p-3">
      <p className="mb-1 text-xs font-bold text-muted">{label}</p>
      <div className="flex flex-wrap items-center gap-2">
        <div className="min-w-0 text-sm whitespace-pre-wrap [overflow-wrap:anywhere]">
          <SideText side={side} />
        </div>
        {action}
      </div>
    </div>
  );
}

function RequestDrawer({ request: r, me, locked, onClose }: { request: ChangeRequest; me: User; locked: boolean; onClose: () => void }) {
  const announce = useAnnounce();
  const qc = useQueryClient();
  const [note, setNote] = useState("");
  const [revealed, setRevealed] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const decided = useRef(false);
  const isAdmin = me.role === "admin";
  const mine = r.createdBy?.id === me.id;
  const where = `${keyOf(r)} in ${r.environmentName}`;
  const [now, next] = sides(r, revealed);

  const run = async (fn: () => Promise<unknown>, ok: string) => {
    setBusy(true);
    setError(null);
    try {
      await fn();
      decided.current = true;
      announce(ok);
      onClose();
      void qc.invalidateQueries({ predicate: (q) => q.queryKey[0] !== "me" });
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  const reveal = async () => {
    try {
      setRevealed((await api.revealRequest(r.id)).value);
      announce("Showing the proposed secret.");
    } catch (e) {
      announce(errorMessage(e), "error");
    }
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent
        side="right"
        className="max-w-xl"
        // After a decision the row may have left this tab, so focus goes to the page heading instead.
        onCloseAutoFocus={(e) => {
          if (!decided.current) return;
          e.preventDefault();
          document.querySelector<HTMLElement>("main h1")?.focus();
        }}
      >
        <DialogTitle className="flex flex-wrap items-center gap-2">
          <span className="font-mono [overflow-wrap:anywhere]">
            <Breakable text={keyOf(r)} />
          </span>
          <EnvPill name={r.environmentName} locked={locked} />
        </DialogTitle>
        <DialogDescription className="flex flex-wrap items-center gap-2">
          <StatusBadge status={r.status} />
          <span>
            Request {r.id}. {statusInfo[r.status].meaning}
          </span>
        </DialogDescription>

        <div className="mt-5 space-y-5">
          <dl className="grid gap-3 rounded-lg border border-line p-3 text-sm sm:grid-cols-3">
            <div className="min-w-0">
              <dt className="mb-1 text-xs font-bold text-muted">Requested by</dt>
              <dd>
                <Person user={r.createdBy} />
              </dd>
            </div>
            <div>
              <dt className="mb-1 text-xs font-bold text-muted">Requested</dt>
              <dd>
                <When iso={r.createdAt} />
              </dd>
            </div>
            <div className="min-w-0">
              <dt className="mb-1 text-xs font-bold text-muted">File</dt>
              <dd className="[overflow-wrap:anywhere]">
                <Link
                  className="font-semibold underline underline-offset-2"
                  to={`/repos/${r.repoId}/files/${r.fileId}?key=${r.keyId}&env=${r.environmentId}`}
                >
                  {r.fileName} in {r.repoName}
                </Link>
                <span className="block text-xs text-muted">Opens the grid with this value selected.</span>
              </dd>
            </div>
          </dl>

          <section>
            <h3 className="text-sm font-bold">Reason</h3>
            <p className="mt-1 text-sm whitespace-pre-wrap">{r.reason || <span className="text-muted">No reason was given.</span>}</p>
          </section>

          <section className="space-y-2">
            <h3 className="text-sm font-bold">Change</h3>
            <div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)]">
              <ValueBox label="Current value" side={now} />
              <ArrowRight className="size-4 rotate-90 self-center justify-self-center text-muted sm:rotate-0" aria-hidden="true" />
              <ValueBox
                label={r.delete ? "Proposed change" : "Proposed value"}
                side={next}
                action={
                  next.kind === "masked" && (
                    <Button size="sm" onClick={() => void reveal()}>
                      <Eye /> Reveal
                    </Button>
                  )
                }
              />
            </div>
            {next.mark && <p className="text-xs text-muted">The words that change are marked.</p>}
          </section>

          {r.status === "pending" ? (
            <InlineMessage title="What happens on approval">
              The change applies to {r.environmentName} at once, as a new version. If the value changed after this request was made, approval is refused;
              then reject the request and ask for a new one.
            </InlineMessage>
          ) : (
            r.decidedAt && (
              <section>
                <h3 className="text-sm font-bold">Decision</h3>
                <p className="mt-1 text-sm">
                  {statusInfo[r.status].label} by {personName(r.decidedBy)} {relativeTime(r.decidedAt)}, on {dateTime(r.decidedAt)}.
                </p>
                {r.decisionNote && <p className="mt-2 border-l-4 border-line pl-3 text-sm whitespace-pre-wrap">{r.decisionNote}</p>}
              </section>
            )
          )}

          {r.status === "pending" && isAdmin && (
            <Field label="Note to the requester" htmlFor="decision-note" hint="Optional. It is shown with the decision.">
              <Textarea id="decision-note" rows={2} maxLength={1000} className="min-h-16 font-sans" value={note} onChange={(e) => setNote(e.target.value)} />
            </Field>
          )}
          {error && (
            <InlineMessage tone="error" role="alert">
              {error}
            </InlineMessage>
          )}
        </div>

        {r.status === "pending" && (isAdmin || mine) && (
          <DialogFooter>
            {mine && (
              <Button variant="ghost" className="mr-auto" disabled={busy} onClick={() => void run(() => api.cancelRequest(r.id), `Cancelled your request for ${where}.`)}>
                Cancel request
              </Button>
            )}
            {isAdmin && (
              <>
                <Button disabled={busy} onClick={() => void run(() => api.rejectRequest(r.id, note), `Rejected the change to ${where}.`)}>
                  <X /> Reject
                </Button>
                <Button variant="primary" disabled={busy} onClick={() => void run(() => api.approveRequest(r.id, note), `Approved and applied the change to ${where}.`)}>
                  <Check /> Approve and apply
                </Button>
              </>
            )}
          </DialogFooter>
        )}
      </DialogContent>
    </Dialog>
  );
}
