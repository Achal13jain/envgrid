import { useState, type FormEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Clock, FileText, GitBranch, Plus, Search } from "lucide-react";
import { Link, useNavigate } from "react-router";
import { PageHeader } from "@/components/PageHeader";
import { Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, ErrorNotice, Field, Input, Loading, Panel } from "@/components/ui";
import { api } from "@/lib/api";
import { activitySentence, collapseRepeats, displayUrl, personName, relativeTime } from "@/lib/format";
import type { AuditRecord, Repo } from "@/lib/types";
import { cn } from "@/lib/utils";

export function ReposPage() {
  const repos = useQuery({ queryKey: ["repos"], queryFn: api.repos });
  const [creating, setCreating] = useState(false);
  const [query, setQuery] = useState("");

  const list = repos.data ?? [];
  const shown = list.filter((r) => {
    const q = query.trim().toLowerCase();
    return !q || r.name.toLowerCase().includes(q) || r.description.toLowerCase().includes(q);
  });


  return (
    <div className="mx-auto w-full max-w-7xl space-y-6 p-4 pb-16 md:p-6 md:pb-16">
      <PageHeader
        title="Repos"
        actions={
          <Button variant="primary" className="h-10" onClick={() => setCreating(true)}>
            <Plus /> New repo
          </Button>
        }
      />

      {repos.isPending && <Loading />}
      {repos.isError && <ErrorNotice error={repos.error} />}

      {repos.data?.length === 0 && <GettingStarted onCreate={() => setCreating(true)} />}


      {list.length > 0 && (
        <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_20rem]">
          <section aria-label="Repo list" className="space-y-4">
            <div className="flex flex-wrap items-center gap-3 rounded-xl border border-line bg-surface p-3 shadow-sm">
              <div className="relative min-w-48 flex-1">
                <Search className="pointer-events-none absolute top-2.5 left-2.5 size-4 text-muted" aria-hidden="true" />
                <Input type="search" aria-label="Filter repos" placeholder="Filter repos" value={query} onChange={(e) => setQuery(e.target.value)} className="pl-8" />
              </div>
              <p className="text-sm text-muted" aria-live="polite">
                Showing {shown.length} of {list.length}
              </p>
            </div>
            {shown.length === 0 ? (
              <Panel>
                <p className="text-center text-muted">No repos match. Clear the filter to see them all.</p>
              </Panel>
            ) : (
              <ul className="grid gap-4 xl:grid-cols-2">
                {shown.map((r) => (
                  <RepoCard key={r.id} repo={r} />
                ))}
              </ul>
            )}
          </section>
          <RecentActivity />
        </div>
      )}
      <CreateRepoDialog open={creating} onOpenChange={setCreating} />
    </div>
  );
}

/** A repo in the list: its name, where its code lives, its files and when it last changed. */
function RepoCard({ repo }: { repo: Repo }) {
  return (
    <li>
      <article className="relative flex h-full flex-col gap-1.5 rounded-xl border border-line bg-surface p-4 shadow-sm transition-colors focus-within:border-accent focus-within:ring-2 focus-within:ring-(--focus) hover:border-accent">
        <h2 className="font-mono text-lg font-bold break-all">
          {/* The link covers the whole card, so the card is one click target. */}
          <Link to={`/repos/${repo.id}`} className="rounded outline-none after:absolute after:inset-0 after:rounded-xl focus-visible:underline">
            {repo.name}
          </Link>
        </h2>
        {repo.sourceUrl && (
          <a
            href={repo.sourceUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="relative z-10 inline-flex min-w-0 items-center gap-1.5 self-start rounded text-sm text-muted hover:text-ink hover:underline"
          >
            <GitBranch className="size-4 shrink-0" aria-hidden="true" />
            <span className="truncate">{displayUrl(repo.sourceUrl)}</span>
            <span className="sr-only">(opens in a new tab)</span>
          </a>
        )}
        <p className="mt-auto flex flex-wrap gap-x-4 gap-y-1 pt-1 text-sm text-muted">
          <span className="inline-flex items-center gap-1.5">
            <FileText className="size-4" aria-hidden="true" />
            {repo.fileCount} {repo.fileCount === 1 ? "file" : "files"}
          </span>
          <span className="inline-flex items-center gap-1.5" title={repo.lastChangeAt}>
            <Clock className="size-4" aria-hidden="true" />
            Changed {relativeTime(repo.lastChangeAt)}
          </span>
        </p>
      </article>
    </li>
  );
}

/** With `fill`, on wide screens the panel takes its column's height and its list scrolls. */
export function RecentActivity({ fill = false }: { fill?: boolean }) {
  // Fetch extra rows: repeats (three sign-ins in a row) collapse into one line.
  const audit = useQuery({ queryKey: ["audit", "recent"], queryFn: () => api.audit({ limit: 20 }) });
  const groups = collapseRepeats(audit.data?.items ?? []).slice(0, 6);
  return (
    <Panel
      title="Recent activity"
      className={fill ? "xl:flex xl:h-full xl:flex-col" : undefined}
      bodyClassName={cn("flex flex-col p-0", fill && "xl:min-h-0 xl:flex-1")}
    >
      {audit.isPending && <Loading />}
      {audit.isError && <ErrorNotice error={audit.error} />}
      {audit.data?.items.length === 0 && <p className="p-4 text-sm text-muted">Nothing has happened yet.</p>}
      {/* Only the list scrolls, so the button below always shows. */}
      <ol className={cn("divide-y divide-line", fill && "relative xl:min-h-0 xl:flex-1 xl:overflow-y-auto")}>
        {groups.map(({ record, count }) => (
          <ActivityItem key={record.id} record={record} count={count} />
        ))}
      </ol>
      <div className="mt-auto border-t border-line p-3">
        <Button asChild size="sm" className="w-full">
          <Link to="/activity">See all activity</Link>
        </Button>
      </div>
    </Panel>
  );
}

export function ActivityItem({ record: r, count = 1 }: { record: AuditRecord; count?: number }) {
  const s = activitySentence(r);
  const who = personName(r.user);
  const text = (
    <>
      <span className="font-semibold">{who}</span> {s.verb} {s.target && <span className="font-mono [overflow-wrap:anywhere]">{s.target}</span>} {s.where}
      {count > 1 && ` ${count} times`}
    </>
  );
  return (
    <li className="flex gap-3 px-4 py-3">
      <span className="flex size-8 shrink-0 items-center justify-center rounded-full bg-surface-2 text-xs font-bold" aria-hidden="true">
        {who.slice(0, 1).toUpperCase()}
      </span>
      <div className="min-w-0 text-sm">
        <p>
          {r.repoId && r.fileId && !r.action.startsWith("delete") ? (
            <Link to={`/repos/${r.repoId}/files/${r.fileId}`} className="hover:underline">
              {text}
            </Link>
          ) : (
            text
          )}
        </p>
        <p className="mt-0.5 text-xs text-muted" title={r.createdAt}>
          {relativeTime(r.createdAt)}
        </p>
      </div>
    </li>
  );
}

export function GettingStarted({ onCreate }: { onCreate: () => void }) {
  const steps: [string, string][] = [
    ["Create a repo", "One per project. It starts with test, uat and prod, and prod is protected so only admins can change it."],
    ["Import a file you already have", "Upload or paste a .env, JSON, YAML, INI, .properties or PHP constants file; envgrid finds the keys."],
    ["Fill the gaps", "The grid marks every missing value. Compare two environments and copy values across in one click."],
  ];
  return (
    <Panel title="Get started" description="Three steps to your first grid.">
      <ol className="grid gap-4 md:grid-cols-3">
        {steps.map(([title, text], i) => (
          <li key={title} className="rounded-lg border border-line bg-surface-alt p-4">
            <span className="flex size-7 items-center justify-center rounded-full bg-accent text-sm font-bold text-accent-ink" aria-hidden="true">
              {i + 1}
            </span>
            <h3 className="mt-3 font-bold">{title}</h3>
            <p className="mt-1 text-sm text-muted">{text}</p>
          </li>
        ))}
      </ol>
      <Button variant="primary" className="mt-4" onClick={onCreate}>
        <Plus /> Create your first repo
      </Button>
    </Panel>
  );
}

export function CreateRepoDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [sourceUrl, setSourceUrl] = useState("");
  const create = useMutation({
    mutationFn: () => api.createRepo({ name, description, sourceUrl }),
    onSuccess: (repo) => {
      onOpenChange(false);
      setName("");
      setDescription("");
      setSourceUrl("");
      navigate(`/repos/${repo.id}`);
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    create.mutate();
  };
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form onSubmit={submit}>
          <DialogTitle>New repo</DialogTitle>
          <DialogDescription>It starts with test, uat and prod. Prod is protected: only admins can change it. You can add more environments later.</DialogDescription>
          <div className="mt-4 space-y-4">
            <Field label="Name" htmlFor="repo-name">
              <Input id="repo-name" required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} placeholder="checkout-service" />
            </Field>
            <Field label="Description" htmlFor="repo-desc" hint="Optional.">
              <Input id="repo-desc" value={description} onChange={(e) => setDescription(e.target.value)} />
            </Field>
            <Field label="Repository URL" htmlFor="repo-source" hint="Optional. The code this config belongs to, for example https://github.com/acme/checkout.">
              <Input id="repo-source" inputMode="url" autoComplete="url" placeholder="https://github.com/..." value={sourceUrl} onChange={(e) => setSourceUrl(e.target.value)} />
            </Field>
            {create.isError && (
              <p role="alert" className="text-sm text-missing">
                {create.error.message}
              </p>
            )}
          </div>
          <DialogFooter>
            <Button onClick={() => onOpenChange(false)}>Cancel</Button>
            <Button type="submit" variant="primary" disabled={create.isPending}>
              Create repo
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
