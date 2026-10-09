import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Eye, RotateCcw } from "lucide-react";
import { api } from "@/lib/api";
import { errorMessage, useAnnounce } from "@/lib/announce";
import { dayLabel, diffSegments, MASK, personName } from "@/lib/format";
import type { Environment, HistoryVersion, Key } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Badge, Button, Dialog, DialogContent, DialogDescription, DialogTitle, ErrorNotice, Loading } from "./ui";

const clock = (iso: string) => new Date(iso).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });

/** Versions of one key in one environment, newest first, grouped by day. */
export function HistoryDrawer({
  item,
  env,
  canWrite,
  onClose,
}: {
  item: Key;
  env: Environment;
  canWrite: boolean;
  onClose: () => void;
}) {
  const announce = useAnnounce();
  const history = useQuery({ queryKey: ["history", item.id, env.id], queryFn: () => api.history(item.id, env.id) });
  const [shown, setShown] = useState<Record<number, string>>({});
  const [confirming, setConfirming] = useState<number | null>(null);
  const name = item.name === "__raw__" ? "the file body" : item.name;

  const restore = useMutation({
    mutationFn: (version: number) => api.restore(item.id, env.id, version),
    onSuccess: (r, version) => {
      setConfirming(null);
      announce(r.changed ? `Restored version ${version} of ${name} in ${env.name} as version ${r.version}.` : `Version ${version} is already the current value.`);
    },
    onError: (e) => announce(errorMessage(e), "error"),
  });

  const reveal = async (version: number) => {
    try {
      const { value } = await api.reveal(item.id, env.id, { version });
      setShown((s) => ({ ...s, [version]: value }));
      announce(`Showing version ${version}.`);
    } catch (e) {
      announce(errorMessage(e), "error");
    }
  };

  const versions = history.data?.versions ?? [];
  const text = (v: HistoryVersion) => (v.present ? (shown[v.version] ?? v.value) : null);
  const days: [string, HistoryVersion[]][] = [];
  for (const v of versions) {
    const label = dayLabel(v.createdAt);
    const last = days[days.length - 1];
    if (last && last[0] === label) last[1].push(v);
    else days.push([label, [v]]);
  }

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right">
        <DialogTitle>
          History of <span className="font-mono">{name}</span>
        </DialogTitle>
        <DialogDescription>
          In {env.name}. Every change is kept; restoring writes the old value again as a new version.
          {versions.length > 1 && " Words that changed from the version before are marked."}
        </DialogDescription>
        {history.isPending && <Loading />}
        {history.isError && <ErrorNotice error={history.error} />}
        {history.data?.versions.length === 0 && <p className="mt-6 text-muted">No value has ever been set here.</p>}
        <div className="mt-4 space-y-5">
          {days.map(([day, list]) => (
            <section key={day} aria-label={day}>
              <h3 className="mb-2 text-xs font-bold text-muted">{day}</h3>
              <ol className="ml-1.5 space-y-3 border-l-2 border-line pl-4">
                {list.map((v) => {
                  const index = versions.indexOf(v);
                  const current = index === 0;
                  const value = text(v);
                  const older = versions[index + 1];
                  const before = older ? text(older) : null;
                  const diff = value !== null && before !== null && value !== before ? diffSegments(before, value) : null;
                  return (
                    <li key={v.version} className="relative rounded-md border border-line p-3">
                      <span
                        className={cn(
                          "absolute top-4 -left-[1.5rem] size-3 rounded-full border-2 border-surface",
                          current ? "bg-accent" : !v.present ? "bg-missing" : "bg-line",
                        )}
                        aria-hidden="true"
                      />
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-bold">Version {v.version}</span>
                        {current && <Badge className="border-accent text-accent">Current</Badge>}
                        {!v.present && <Badge className="border-missing text-missing">Deleted</Badge>}
                        <span className="ml-auto text-sm text-muted" title={v.createdAt}>
                          {clock(v.createdAt)}
                        </span>
                      </div>
                      <p className="mt-0.5 text-sm text-muted">by {personName(v.createdBy)}</p>
                      {v.present && (
                        <pre className="mt-2 max-h-48 overflow-auto rounded bg-surface-2 p-2 font-mono text-sm break-all whitespace-pre-wrap">
                          {value === null ? (
                            <>
                              <span aria-hidden="true">{MASK}</span>
                              <span className="sr-only">secret, hidden</span>
                            </>
                          ) : value === "" ? (
                            <span className="text-muted italic">empty</span>
                          ) : diff?.b ? (
                            <>
                              {diff.prefix}
                              <mark className="rounded-sm bg-changed-soft px-0.5 text-changed underline decoration-dotted underline-offset-2">
                                <span className="sr-only">changed: </span>
                                {diff.b}
                              </mark>
                              {diff.suffix}
                            </>
                          ) : (
                            value
                          )}
                        </pre>
                      )}
                      {confirming === v.version ? (
                        <div className="mt-2 rounded-md border border-accent bg-accent-soft p-2.5 text-sm" role="group" aria-label={`Restore version ${v.version}`}>
                          <p>
                            Restore version {v.version}? The value from {new Date(v.createdAt).toLocaleString([], { dateStyle: "medium", timeStyle: "short" })} becomes the current value again, as a new version.
                          </p>
                          <div className="mt-2 flex gap-2">
                            <Button size="sm" variant="primary" autoFocus disabled={restore.isPending} onClick={() => restore.mutate(v.version)}>
                              <RotateCcw /> Restore
                            </Button>
                            <Button size="sm" onClick={() => setConfirming(null)}>
                              Cancel
                            </Button>
                          </div>
                        </div>
                      ) : (
                        <div className="mt-2 flex flex-wrap gap-2">
                          {v.present && value === null && (
                            <Button size="sm" onClick={() => reveal(v.version)}>
                              <Eye /> Reveal
                            </Button>
                          )}
                          {!current && (
                            <Button
                              size="sm"
                              disabled={!canWrite || restore.isPending}
                              title={canWrite ? undefined : `${env.name} is protected: only admins can change it.`}
                              onClick={() => setConfirming(v.version)}
                            >
                              <RotateCcw /> Restore this version
                            </Button>
                          )}
                        </div>
                      )}
                    </li>
                  );
                })}
              </ol>
            </section>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  );
}
