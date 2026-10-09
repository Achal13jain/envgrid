import { useEffect, type ReactNode } from "react";
import { ChevronRight } from "lucide-react";
import { Link } from "react-router";

/** Sets the browser tab title, so every page is identifiable in tabs and history. */
export function useDocumentTitle(title: string | undefined) {
  useEffect(() => {
    document.title = title ? `${title} | envgrid` : "envgrid";
  }, [title]);
}

/** The top of a page: its one h1, an optional sentence, and page actions. */
export function PageHeader({ title, description, actions }: { title: string; description?: ReactNode; actions?: ReactNode }) {
  useDocumentTitle(title);
  return (
    <div className="flex flex-wrap items-end gap-x-6 gap-y-3">
      <div className="min-w-0">
        <h1 className="text-2xl font-bold tracking-tight sm:text-3xl">{title}</h1>
        {description && <p className="mt-1.5 max-w-3xl text-muted">{description}</p>}
      </div>
      {actions && <div className="ml-auto flex flex-wrap items-center gap-2">{actions}</div>}
    </div>
  );
}

export interface Crumb {
  label: ReactNode;
  to?: string;
}

/** Where you are, with a link back to every level above. */
export function Breadcrumbs({ items }: { items: Crumb[] }) {
  return (
    <nav aria-label="Breadcrumb" className="min-w-0">
      <ol className="flex flex-wrap items-center gap-0.5 text-sm">
        {items.map((it, i) => {
          const last = i === items.length - 1;
          return (
            <li key={i} className="flex min-w-0 items-center gap-0.5">
              {i > 0 && <ChevronRight className="size-4 shrink-0 text-muted" aria-hidden="true" />}
              {it.to && !last ? (
                <Link to={it.to} className="truncate rounded px-1.5 py-1 text-muted hover:bg-surface-2 hover:text-ink">
                  {it.label}
                </Link>
              ) : (
                <span aria-current={last ? "page" : undefined} className="truncate px-1.5 py-1 font-semibold">
                  {it.label}
                </span>
              )}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}
