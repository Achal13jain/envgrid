import { useId, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CalendarClock, CircleMinus, CirclePlus, CircleSlash, History, PenLine, RotateCcw } from "lucide-react";
import { Badge, Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, InlineMessage, Input, Loading } from "@/components/ui";
import { api } from "@/lib/api";
import { errorMessage, useAnnounce } from "@/lib/announce";
import { activitySentence, cellView, changePoints, diffSegments, keyLabel, MASK, personName, preview, relativeTime, type CellView, type ChangePoint } from "@/lib/format";
import type { Environment, RollbackChange } from "@/lib/types";
import { cn } from "@/lib/utils";

/** "2026-10-06T09:30" in local time, the value a datetime-local input takes. */
function localInputValue(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

const count = (n: number, word: string) => `${n} ${word}${n === 1 ? "" : "s"}`;
const when = (iso: string) => `${new Date(iso).toLocaleString([], { dateStyle: "medium", timeStyle: "short" })} (${relativeTime(iso)})`;

function PointLabel({ point, envName }: { point: ChangePoint; envName: string }) {
  const first = point.records[0];
  const who = personName(first.user);
  const keys = new Set(point.records.map((r) => r.detail.key).filter((k) => typeof k === "string"));
  if (point.records.some((r) => r.action === "restore_environment")) return <>Just before {who} restored {envName} to an earlier time</>;
  if (keys.size > 1) return <>Just before {who} changed {keys.size} keys</>;
  const s = activitySentence(first);
  return (
    <>
      Just before {who} {s.verb} <span className="font-mono [overflow-wrap:anywhere]">{s.target}</span>
    </>
  );
}

const kinds = {
  changed: { label: "Changed", Icon: PenLine, className: "border-changed bg-changed-soft text-changed" },
  removed: { label: "Removed", Icon: CircleMinus, className: "border-missing bg-missing-soft text-missing" },
  added: { label: "Added", Icon: CirclePlus, className: "border-accent bg-accent-soft text-accent" },
};

const kindOf = (c: RollbackChange): keyof typeof kinds => (!c.then.present ? "removed" : !c.current.present ? "added" : "changed");

const head = (s: string, n: number) => (s.length > n ? `${s.slice(0, n)}…` : s);
const tail = (s: string, n: number) => (s.length > n ? `…${s.slice(-n)}` : s);

/** A value as the preview shows it. Long values keep a little context around the change. */
function Value({ view, diff, mark }: { view: CellView; diff: { prefix: string; mid: string; suffix: string } | null; mark?: boolean }) {
  return (
    <td className={cn("px-3 py-2.5", view.kind === "missing" && "hatch-missing")}>
      {view.kind === "missing" && (
        <span className="inline-flex items-center gap-1.5 font-semibold text-missing">
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
      {view.kind === "empty" && <span className="text-muted italic">empty</span>}
      {view.kind === "value" && (
        <span className="font-mono whitespace-pre-wrap [overflow-wrap:anywhere]">
          {diff ? (
            <>
              {tail(diff.prefix, 40)}
              {mark && diff.mid ? (
                <mark className="rounded-sm bg-changed-soft px-0.5 text-changed underline decoration-dotted underline-offset-2">
                  <span className="sr-only">changed: </span>
                  {head(diff.mid, 200)}
                </mark>
              ) : (
                head(diff.mid, 200)
              )}
              {head(diff.suffix, 40)}
            </>
          ) : (
            preview(view.text)
          )}
        </span>
      )}
    </td>
  );
}

function ChangeRow({ change }: { change: RollbackChange }) {
  const now = cellView(change.current);
  const then = cellView(change.then);
  const d = now.kind === "value" && then.kind === "value" ? diffSegments(now.text, then.text) : null;
  const kind = kinds[kindOf(change)];
  const markers = Object.entries({ secret: change.key.isSecret, required: change.key.required, pattern: !!change.key.pattern })
    .filter(([, on]) => on)
    .map(([m]) => m);
  return (
    <tr className="align-top">
      <th scope="row" className="px-3 py-2.5 text-left font-normal">
        <span className="font-mono font-semibold [overflow-wrap:anywhere]">{keyLabel(change.key)}</span>
        {markers.length > 0 && (
          <span className="mt-1 flex flex-wrap gap-1">
            {markers.map((m) => (
              <Badge key={m}>{m}</Badge>
            ))}
          </span>
        )}
      </th>
      <Value view={now} diff={d && { prefix: d.prefix, mid: d.a, suffix: d.suffix }} />
      <Value view={then} diff={d && { prefix: d.prefix, mid: d.b, suffix: d.suffix }} mark />
      <td className="px-3 py-2.5">
        <Badge className={kind.className}>
          <kind.Icon className="size-3.5" aria-hidden="true" />
          {kind.label}
        </Badge>
      </td>
    </tr>
  );
}

const CUSTOM = "custom";

/** Puts one environment of a file back to how it was at a time, as new versions. */
export function RestoreDialog({ fileId, env, onClose }: { fileId: number; env: Environment; onClose: () => void }) {
  const announce = useAnnounce();
  const qc = useQueryClient();
  const previewId = useId();
  const customErrorId = useId();
  const [choice, setChoice] = useState<string | null>(null);
  const [custom, setCustom] = useState(() => localInputValue(new Date(Date.now() - 3600_000)));
  const [customProblem, setCustomProblem] = useState<string | null>(null);

  // Loaded once, so a refetch cannot move "Most recent" while the person is choosing.
  const audit = useQuery({
    queryKey: ["audit", "restore", fileId, env.id],
    queryFn: () => api.audit({ file: fileId, env: env.id, limit: 50 }),
    staleTime: Infinity,
  });
  const points = audit.data ? changePoints(audit.data.items, audit.data.nextCursor === null).slice(0, 10) : [];
  const chosen = choice ?? (audit.isPending ? null : (points[0]?.id ?? CUSTOM));

  const at = chosen === CUSTOM ? (customProblem ? null : new Date(custom).toISOString()) : (points.find((p) => p.id === chosen)?.at ?? null);

  const plan = useQuery({
    queryKey: ["rollback-preview", fileId, env.id, at],
    queryFn: () => api.rollbackPreview(fileId, env.id, at!),
    enabled: at !== null,
  });
  const restore = useMutation({
    // The preview echoes the time without milliseconds, so the restore sends the chosen instant itself.
    mutationFn: (to: string) => api.rollback(fileId, env.id, to),
    onSuccess: ({ changed }) => {
      const n = changed?.length ?? 0;
      announce(`Restored ${count(n, "key")} in ${env.name}. Each change is a new version, so this can be undone from history.`);
      onClose();
      void qc.invalidateQueries({ predicate: (q) => q.queryKey[0] !== "me" });
    },
  });
  const pick = (id: string) => {
    setChoice(id);
    restore.reset();
  };

  const changes = plan.data?.changes ?? [];
  const total = changes.length;
  const removed = changes.filter((c) => kindOf(c) === "removed");
  // The server keeps a value in every required key, so it refuses a restore that would remove one.
  const blocked = removed.filter((c) => c.key.required).map((c) => keyLabel(c.key));
  const others = total - removed.length;
  const summary = !plan.data
    ? ""
    : total === 0
      ? "Nothing to restore."
      : `${total} ${total === 1 ? "key changes" : "keys change"}${removed.length ? `; ${removed.length} of them ${removed.length === 1 ? "becomes" : "become"} missing` : ""}.`;

  const option = (selected: boolean) =>
    cn("flex cursor-pointer items-start gap-3 px-3 py-2.5 text-sm", selected ? "bg-accent-soft" : "hover:bg-surface-2");
  const radio = "mt-0.5 size-4 shrink-0 accent-accent";

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-6xl pb-0">
        <div className="flex items-start gap-3">
          <span className="hidden size-10 shrink-0 items-center justify-center rounded-full bg-accent-soft text-accent sm:flex" aria-hidden="true">
            <History className="size-5" />
          </span>
          <div className="min-w-0">
            <DialogTitle>Restore {env.name} to an earlier time</DialogTitle>
            <DialogDescription>
              Every key in this file goes back to the value it had in {env.name} at the time you choose, so later changes are undone too. Nothing
              is erased: each change is saved as a new version.
            </DialogDescription>
          </div>
        </div>
        <div className="mt-5 grid gap-5 lg:grid-cols-[22rem_minmax(0,1fr)]">
          <fieldset className="min-w-0">
            <legend className="text-base font-bold">Choose when to restore</legend>
            <p className="mt-1 text-sm text-muted">Pick the moment just before a recent change, or a date and time of your own.</p>
            {audit.isPending && <Loading label="Loading recent changes" />}
            {audit.isError && (
              <InlineMessage tone="error" className="mt-3" title="Recent changes could not be loaded.">
                {errorMessage(audit.error)}
              </InlineMessage>
            )}
            {audit.isSuccess && points.length === 0 && (
              <p className="mt-3 text-sm">The audit log shows no recent changes to this file in {env.name}. Pick a date and time instead.</p>
            )}
            {!audit.isPending && (
              <div className="mt-3 divide-y divide-line overflow-hidden rounded-lg border border-line">
                {points.map((p, i) => (
                  <label key={p.id} className={option(chosen === p.id)}>
                    <input type="radio" name="restore-point" className={radio} checked={chosen === p.id} onChange={() => pick(p.id)} />
                    <span className="min-w-0 flex-1">
                      <span className="font-semibold">
                        <PointLabel point={p} envName={env.name} />
                      </span>
                      {i === 0 && <Badge className="ml-2 border-accent text-accent">Most recent</Badge>}
                      <span className="mt-0.5 block text-xs text-muted">{when(p.records[0].createdAt)}</span>
                    </span>
                  </label>
                ))}
                <div className={chosen === CUSTOM ? "bg-accent-soft" : undefined}>
                  <label className={cn(option(chosen === CUSTOM), "font-semibold")}>
                    <input type="radio" name="restore-point" className={radio} checked={chosen === CUSTOM} onChange={() => pick(CUSTOM)} />
                    <span className="inline-flex items-center gap-1.5">
                      <CalendarClock className="size-4 text-muted" aria-hidden="true" /> Pick a date and time
                    </span>
                  </label>
                  {chosen === CUSTOM && (
                    <div className="px-3 pb-3 pl-10">
                      <Field label="Date and time" htmlFor="restore-at">
                        <Input
                          id="restore-at"
                          type="datetime-local"
                          required
                          max={localInputValue(new Date())}
                          value={custom}
                          aria-invalid={customProblem !== null}
                          aria-describedby={customProblem ? customErrorId : undefined}
                          onChange={(e) => {
                            const t = e.target.value ? new Date(e.target.value).getTime() : NaN;
                            setCustom(e.target.value);
                            setCustomProblem(
                              Number.isNaN(t) ? "Enter a date and time." : t > Date.now() ? "Choose a time that is not in the future." : null,
                            );
                            restore.reset();
                          }}
                        />
                      </Field>
                      {customProblem && (
                        <p id={customErrorId} className="mt-1 text-xs font-semibold text-missing">
                          {customProblem}
                        </p>
                      )}
                    </div>
                  )}
                </div>
              </div>
            )}
          </fieldset>

          <div className="min-w-0 lg:border-l lg:border-line lg:pl-5">
            {removed.length > 0 && (
              <InlineMessage
                tone="warning"
                className="mb-4"
                title={`This restore removes ${count(removed.length, "value")}${others ? ` and changes ${others} ${others === 1 ? "other" : "others"}` : ""}.`}
              >
                Keys that had no value at that time become missing in {env.name}.
                {blocked.length > 0 &&
                  ` ${new Intl.ListFormat("en").format(blocked)} ${blocked.length === 1 ? "is" : "are"} required and must keep a value, so this restore will be refused.`}
              </InlineMessage>
            )}
            <h3 id={previewId} className="text-base font-bold">
              Preview of changes
            </h3>
            <p className="mt-1 text-sm text-muted">Only the keys whose value in {env.name} would change are listed. Secret values stay hidden.</p>
            <div className="mt-3">
              {at === null ? (
                !audit.isPending && <p className="text-sm text-muted">Choose a time to see what would change.</p>
              ) : plan.isPending ? (
                <Loading label="Loading the preview" />
              ) : plan.isError ? (
                <InlineMessage tone="error" title="The preview could not be loaded.">
                  {errorMessage(plan.error)}
                </InlineMessage>
              ) : total === 0 ? (
                <p className="rounded-md border border-line bg-surface-2 p-3 text-sm">Nothing to restore: {env.name} already matches that time.</p>
              ) : (
                // Focusable so the table scrolls by keyboard.
                <div role="region" aria-labelledby={previewId} tabIndex={0} className="max-h-[55vh] overflow-auto rounded-lg border border-line">
                  <table className="w-full min-w-[36rem] text-left text-sm">
                    <caption className="sr-only">Each key that changes, with its value now and after restoring</caption>
                    <thead className="sticky top-0 z-10 bg-surface-2 text-xs text-muted">
                      <tr>
                        <th scope="col" className="px-3 py-2 font-semibold">
                          Key
                        </th>
                        <th scope="col" className="px-3 py-2 font-semibold">
                          Now
                        </th>
                        <th scope="col" className="px-3 py-2 font-semibold">
                          After restoring
                        </th>
                        <th scope="col" className="px-3 py-2 font-semibold">
                          Change
                        </th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-line">
                      {changes.map((c) => (
                        <ChangeRow key={c.key.id} change={c} />
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          </div>
        </div>

        <DialogFooter className="sticky bottom-0 -mx-5 items-center border-t border-line bg-surface px-5 py-3">
          {restore.isError && (
            <InlineMessage tone="error" role="alert" className="w-full" title="The restore did not finish.">
              {errorMessage(restore.error)}
            </InlineMessage>
          )}
          <p role="status" className="mr-auto text-sm">
            {summary}
          </p>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" disabled={!at || total === 0 || restore.isPending} onClick={() => at && restore.mutate(at)}>
            <RotateCcw /> {total > 0 ? `Restore ${count(total, "key")}` : "Restore"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
