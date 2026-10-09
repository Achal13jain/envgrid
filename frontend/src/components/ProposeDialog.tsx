import { useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Eye, GitPullRequestArrow } from "lucide-react";
import {
  Button,
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
  Field,
  InlineMessage,
  Textarea,
} from "@/components/ui";
import { api } from "@/lib/api";
import { errorMessage, useAnnounce } from "@/lib/announce";
import { cellView, keyLabel } from "@/lib/format";
import type { Cell, Environment, Key } from "@/lib/types";

/** A member's proposal for a protected environment; an admin approves it on the Requests page. */
export function ProposeDialog({
  item,
  env,
  cell,
  remove,
  php,
  onClose,
}: {
  item: Key;
  env: Environment;
  cell: Cell;
  remove: boolean;
  php: boolean;
  onClose: () => void;
}) {
  const announce = useAnnounce();
  const qc = useQueryClient();
  const [value, setValue] = useState(item.isSecret ? "" : (cell.value ?? ""));
  const [reason, setReason] = useState("");
  const [revealed, setRevealed] = useState<string>();
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const label = `${keyLabel(item)} in ${env.name}`;
  const current = cellView(cell, revealed);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      await api.requestChange(item.id, env.id, remove ? { delete: true, reason } : { value, reason });
      announce(`Sent your request for ${label} to the admins. Follow it on the Requests page.`);
      await qc.invalidateQueries({ predicate: (q) => q.queryKey[0] !== "me" });
      onClose();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };
  const reveal = async () => {
    try {
      setRevealed((await api.reveal(item.id, env.id)).value);
      announce(`Showing the current value of ${label}.`);
    } catch (err) {
      announce(errorMessage(err), "error");
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <form onSubmit={submit}>
          <DialogTitle>{remove ? `Propose deleting ${label}` : `Propose a change to ${label}`}</DialogTitle>
          <DialogDescription>
            {env.name} is protected, so an admin reviews this before it applies. The change is checked again when it is approved.
          </DialogDescription>
          <div className="mt-4 space-y-4">
            <div>
              <p className="mb-1 text-sm font-semibold">Current value</p>
              <div className="flex items-center gap-2 rounded-md border border-line bg-surface-alt px-3 py-2 text-sm">
                <div className="min-w-0 flex-1 font-mono whitespace-pre-wrap [overflow-wrap:anywhere]">
                  {current.kind === "masked" ? (
                    <>
                      <span className="tracking-widest" aria-hidden="true">
                        {current.text}
                      </span>
                      <span className="sr-only">secret, hidden</span>
                    </>
                  ) : current.kind === "value" ? (
                    current.text
                  ) : (
                    <span className={current.kind === "missing" ? "font-sans font-semibold text-missing" : "font-sans text-muted italic"}>{current.text}</span>
                  )}
                </div>
                {current.kind === "masked" && (
                  <Button size="sm" onClick={() => void reveal()}>
                    <Eye /> Reveal
                  </Button>
                )}
              </div>
            </div>
            {!remove && (
              <Field
                label="New value"
                htmlFor="propose-value"
                hint={
                  item.isSecret
                    ? "This is a secret: admins see it only when they choose to reveal it, and that is recorded."
                    : php
                      ? "PHP value: quote text, for example 'hello', or write a number, true, NULL or an array."
                      : undefined
                }
              >
                <Textarea
                  id="propose-value"
                  rows={3}
                  spellCheck={false}
                  autoComplete="off"
                  className="font-mono"
                  value={value}
                  onChange={(e) => setValue(e.target.value)}
                />
              </Field>
            )}
            <Field label="Reason" htmlFor="propose-reason" hint="Optional. Tell the admin why, for example the ticket or the incident.">
              <Textarea id="propose-reason" rows={2} maxLength={1000} value={reason} onChange={(e) => setReason(e.target.value)} />
            </Field>
            <InlineMessage>
              After an admin approves it, the change applies to {env.name} as a new version. The request and the decision are both recorded in the audit log.
            </InlineMessage>
            {error && (
              <InlineMessage tone="error" role="alert">
                {error}
              </InlineMessage>
            )}
          </div>
          <DialogFooter>
            <DialogClose asChild>
              <Button>Cancel</Button>
            </DialogClose>
            <Button type="submit" variant="primary" disabled={busy}>
              <GitPullRequestArrow /> {remove ? "Propose deletion" : "Propose change"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
