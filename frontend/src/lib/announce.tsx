import { createContext, useCallback, useContext, useRef, useState, type ReactNode } from "react";
import { CircleAlert, CircleCheck } from "lucide-react";
import { cn } from "./utils";

type Tone = "ok" | "error";
type Announce = (message: string, tone?: Tone) => void;

const AnnounceContext = createContext<Announce>(() => {});

/** Announces results (saved, copied, revealed) to screen readers and shows them as a toast. */
export function useAnnounce() {
  return useContext(AnnounceContext);
}

export function AnnounceProvider({ children }: { children: ReactNode }) {
  const [msg, setMsg] = useState<{ text: string; tone: Tone; n: number } | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const announce = useCallback<Announce>((text, tone = "ok") => {
    clearTimeout(timer.current);
    setMsg((m) => ({ text, tone, n: (m?.n ?? 0) + 1 }));
    timer.current = setTimeout(() => setMsg(null), tone === "error" ? 8000 : 4000);
  }, []);

  return (
    <AnnounceContext.Provider value={announce}>
      {children}
      {/* Both regions always exist so assistive tech picks up changes. */}
      <div aria-live="polite" role="status" className="sr-only">
        {msg?.tone === "ok" ? msg.text : ""}
      </div>
      <div aria-live="assertive" role="alert" className="sr-only">
        {msg?.tone === "error" ? msg.text : ""}
      </div>
      {msg && (
        <div
          key={msg.n}
          aria-hidden="true"
          className={cn(
            "fixed right-4 bottom-20 left-4 z-50 flex items-center gap-2 rounded-md border px-4 py-2.5 shadow-lg sm:left-auto sm:max-w-md",
            "bg-surface text-ink",
            msg.tone === "error" ? "border-missing" : "border-line",
          )}
        >
          {msg.tone === "error" ? (
            <CircleAlert className="size-4 shrink-0 text-missing" />
          ) : (
            <CircleCheck className="size-4 shrink-0 text-accent" />
          )}
          <span>{msg.text}</span>
        </div>
      )}
    </AnnounceContext.Provider>
  );
}

/** Message for a failed request, without echoing anything the user typed. */
export function errorMessage(e: unknown): string {
  return e instanceof Error ? e.message : "Something went wrong.";
}
