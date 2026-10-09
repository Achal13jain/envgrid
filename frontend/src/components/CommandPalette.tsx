import { useEffect, useId, useState, type KeyboardEvent, type ReactNode } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { FileText, FolderGit2, KeyRound, Search, type LucideIcon } from "lucide-react";
import { useNavigate } from "react-router";
import { api } from "@/lib/api";
import { errorMessage } from "@/lib/announce";
import { cn } from "@/lib/utils";
import { Dialog, DialogContent, DialogDescription, DialogTitle, Input, Kbd } from "./ui";

/** A page the palette can go to; `key` is the second key of its "g then" shortcut. */
export interface PageCommand {
  to: string;
  label: string;
  icon: LucideIcon;
  key: string;
}

interface Option {
  to: string;
  name: string;
  icon: LucideIcon;
  /** Where it lives: the repo, and for a key also the file. */
  repo?: string;
  file?: string;
  mono?: boolean;
  hint: ReactNode;
}

/**
 * Search by name across repos, files and keys, plus the pages, following the
 * combobox pattern: focus stays in the input and the arrows move the active
 * option. Names only; values are never searched or shown.
 */
export function CommandPalette({ open, onOpenChange, pages }: { open: boolean; onOpenChange: (open: boolean) => void; pages: PageCommand[] }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {/* data-palette lets the Ctrl+K handler tell this dialog from others. */}
      <DialogContent data-palette className="top-4 flex max-w-xl translate-y-0 flex-col overflow-hidden p-0 sm:top-[12vh] sm:max-h-[76dvh]">
        <Palette pages={pages} close={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  );
}

// Inside the dialog content, so the text and results start fresh on every open.
function Palette({ pages, close }: { pages: PageCommand[]; close: () => void }) {
  const navigate = useNavigate();
  const listId = useId();
  const [text, setText] = useState("");
  const [active, setActive] = useState(0);
  const q = text.trim();

  // Ask the server 150 ms after the last keystroke. Answers are kept per text,
  // so a slow answer to an older text never replaces a newer one.
  const [debounced, setDebounced] = useState("");
  useEffect(() => {
    const t = setTimeout(() => setDebounced(q), 150);
    return () => clearTimeout(t);
  }, [q]);
  const search = useQuery({
    queryKey: ["search", debounced],
    queryFn: () => api.search(debounced),
    enabled: debounced !== "",
    placeholderData: keepPreviousData,
  });
  const loading = q !== "" && (q !== debounced || search.isFetching);
  const hits = q ? (search.data ?? []) : [];

  const lower = q.toLowerCase();
  const groups: { label: string; options: Option[] }[] = [
    {
      label: "Pages",
      options: pages
        .filter((p) => `go to ${p.label}`.toLowerCase().includes(lower))
        .map((p) => ({
          to: p.to,
          name: `Go to ${p.label}`,
          icon: p.icon,
          hint: (
            <span className="flex items-center gap-1">
              <Kbd>g</Kbd> then <Kbd>{p.key}</Kbd>
            </span>
          ),
        })),
    },
    {
      label: "Repos",
      options: hits.filter((h) => h.kind === "repo").map((h) => ({ to: `/repos/${h.id}`, name: h.name, icon: FolderGit2, hint: "Go to repo" })),
    },
    {
      label: "Files",
      options: hits
        .filter((h) => h.kind === "file")
        .map((h) => ({ to: `/repos/${h.repoId}/files/${h.id}`, name: h.name, icon: FileText, repo: h.repoName, mono: true, hint: "Open file" })),
    },
    {
      label: "Keys",
      options: hits.filter((h) => h.kind === "key").map((h) => ({
        to: `/repos/${h.repoId}/files/${h.fileId}?key=${h.id}`,
        name: h.name,
        icon: KeyRound,
        repo: h.repoName,
        file: h.fileName,
        mono: true,
        hint: "Go to key",
      })),
    },
  ].filter((g) => g.options.length > 0);
  const options = groups.flatMap((g) => g.options);
  const n = options.length;
  const current = Math.min(active, n - 1);
  const optionId = (i: number) => `${listId}-${i}`;

  useEffect(() => {
    document.getElementById(optionId(current))?.scrollIntoView({ block: "nearest" });
  });

  const go = (o: Option) => {
    close();
    navigate(o.to);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "ArrowDown" && n) {
      e.preventDefault();
      setActive((current + 1) % n);
    } else if (e.key === "ArrowUp" && n) {
      e.preventDefault();
      setActive((current - 1 + n) % n);
    } else if (e.key === "Enter" && !e.nativeEvent.isComposing && options[current]) {
      e.preventDefault();
      go(options[current]);
    }
  };

  const message = !q
    ? "Type part of a name to find a repo, file or key, or choose a page."
    : search.isError
      ? `Search failed. ${errorMessage(search.error)}`
      : loading
        ? "Searching…"
        : n === 0
          ? `No repo, file, key or page has a name that contains “${q}”.`
          : null;

  return (
    <>
      <div className="space-y-3 p-4 pb-2">
        <DialogTitle className="text-base">Search repos, files and keys</DialogTitle>
        <div className="relative">
          <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted" aria-hidden="true" />
          <Input
            role="combobox"
            aria-label="Search repos, files and keys"
            aria-expanded={n > 0}
            aria-controls={listId}
            aria-activedescendant={n > 0 ? optionId(current) : undefined}
            aria-autocomplete="list"
            autoComplete="off"
            spellCheck={false}
            maxLength={200}
            placeholder="Type part of a name"
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              setActive(0);
            }}
            onKeyDown={onKeyDown}
            className="h-10 pl-9 text-base"
          />
        </div>
      </div>
      <div role="status" className="px-4 text-sm text-muted">
        {message ? <p className="py-2">{message}</p> : <span className="sr-only">{n === 1 ? "1 result." : `${n} results.`}</span>}
      </div>
      <div id={listId} role="listbox" aria-label="Results" className="min-h-0 flex-1 overflow-y-auto px-2 pb-2">
        {groups.map((g) => (
          <div key={g.label} role="group" aria-labelledby={`${listId}-${g.label}`}>
            <div id={`${listId}-${g.label}`} role="presentation" className="px-2.5 pt-3 pb-1 text-xs font-bold text-muted">
              {g.label}
            </div>
            {g.options.map((o) => {
              const i = options.indexOf(o);
              return (
                <div
                  key={o.to}
                  id={optionId(i)}
                  role="option"
                  aria-selected={i === current}
                  onMouseMove={() => setActive(i)}
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => go(o)}
                  className="flex cursor-pointer items-center gap-3 rounded-md px-2.5 py-2 aria-selected:bg-accent-soft aria-selected:shadow-[inset_3px_0_0_var(--accent)]"
                >
                  <o.icon className="size-4 shrink-0 text-muted" aria-hidden="true" />
                  <span className="min-w-0 flex-1 truncate">
                    <span className={cn("font-semibold", o.mono && "font-mono")}>{o.name}</span>
                    {o.repo && (
                      <span className="text-muted">
                        <span className="sr-only">, in</span> {o.repo}
                        {o.file && (
                          <>
                            {" / "}
                            <span className="font-mono">{o.file}</span>
                          </>
                        )}
                      </span>
                    )}
                  </span>
                  <span className="hidden shrink-0 text-xs text-muted sm:inline" aria-hidden="true">
                    {o.hint}
                  </span>
                </div>
              );
            })}
          </div>
        ))}
      </div>
      <DialogDescription className="mt-0 border-t border-line bg-surface-alt px-4 py-2.5 text-xs">
        Press <Kbd>↑</Kbd> or <Kbd>↓</Kbd> to move, <Kbd>Enter</Kbd> to open and <Kbd>Esc</Kbd> to close.
      </DialogDescription>
    </>
  );
}
