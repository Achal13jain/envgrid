import { useState, type FormEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  ArrowDown,
  ArrowUp,
  Columns2,
  ExternalLink,
  FilePlus,
  FileText,
  FileUp,
  GitBranch,
  History,
  Layers,
  Lock,
  PanelLeftClose,
  PanelLeftOpen,
  Plus,
  Settings2,
  Trash2,
} from "lucide-react";
import { Link, Navigate, NavLink, Outlet, useLocation, useNavigate, useParams } from "react-router";
import { useMe } from "@/App";
import { ImportDialog } from "@/components/ImportDialog";
import { PageHeader, useDocumentTitle } from "@/components/PageHeader";
import {
  Button,
  ConfirmDialog,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
  EmptyState,
  ErrorNotice,
  Field,
  Input,
  Loading,
  Panel,
  Select,
} from "@/components/ui";
import { api } from "@/lib/api";
import { errorMessage, useAnnounce } from "@/lib/announce";
import { displayUrl, formatLabel, formatOptions, inferFormat } from "@/lib/format";
import type { EnvCoverage, Environment, Format, Repo, User } from "@/lib/types";
import { cn } from "@/lib/utils";
import { ActivityLog } from "./Activity";

function useRepoData(repoId: number) {
  const repo = useQuery({ queryKey: ["repo", repoId], queryFn: () => api.repo(repoId) });
  const files = useQuery({ queryKey: ["files", repoId], queryFn: () => api.files(repoId) });
  const envs = useQuery({ queryKey: ["envs", repoId], queryFn: () => api.environments(repoId) });
  return { repo, files, envs };
}

type Dialogs = "addFile" | "import" | null;

// The current tab is marked by weight and an underline, not colour alone.
const tabClass = (active: boolean) =>
  cn(
    "inline-flex h-11 shrink-0 items-center gap-1.5 border-b-2 px-3 text-sm font-semibold whitespace-nowrap [&_svg]:size-4",
    active ? "border-accent text-ink" : "border-transparent text-muted hover:border-line hover:text-ink",
  );

/**
 * A repo's frame, laid out like a code host: tabs for its parts and, on the
 * Files and Compare tabs, the list of files, which can be hidden.
 */
export function RepoLayout() {
  const params = useParams();
  const repoId = Number(params.repoId);
  const fileId = Number(params.fileId);
  const { pathname } = useLocation();
  const section = pathname.split("/")[3] ?? "";
  const comparing = pathname.endsWith("/compare");
  const user = useMe().data!;
  const { repo, files, envs } = useRepoData(repoId);
  const [dialog, setDialog] = useState<Dialogs>(null);
  const [sidebarHidden, setSidebarHidden] = useState(initialSidebarHidden);
  const toggleSidebar = () =>
    setSidebarHidden((hidden) => {
      try {
        localStorage.setItem(SIDEBAR_KEY, hidden ? "shown" : "hidden");
      } catch {
        /* storage blocked: the choice lasts for this visit */
      }
      return !hidden;
    });

  if (repo.isError) return <ErrorNotice error={repo.error} />;
  if (!repo.data || !files.data || !envs.data) return <Loading />;

  const file = files.data.find((f) => f.id === fileId) ?? files.data[0];
  const onFiles = section === "" || section === "files";
  const base = `/repos/${repoId}`;
  const tabs = [
    { label: "Files", icon: FileText, to: file ? `${base}/files/${file.id}` : base, active: onFiles && !comparing },
    { label: "Compare", icon: Columns2, to: file ? `${base}/files/${file.id}/compare` : undefined, active: comparing },
    { label: "Environments", icon: Layers, to: `${base}/environments`, active: section === "environments", count: envs.data.length },
    { label: "Activity", icon: History, to: `${base}/activity`, active: section === "activity" },
    { label: "Settings", icon: Settings2, to: `${base}/settings`, active: section === "settings" },
  ];

  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-2 border-b border-line bg-surface px-3">
        <nav aria-label="Repo sections" className="flex min-w-0 items-center overflow-x-auto">
          {tabs.map(({ label, icon: Icon, to, active, count }) =>
            to ? (
              <Link key={label} to={to} aria-current={active ? "page" : undefined} className={tabClass(active)}>
                <Icon aria-hidden="true" />
                {label}
                {count !== undefined && <span className="rounded-full bg-surface-2 px-1.5 text-xs font-semibold text-muted">{count}</span>}
              </Link>
            ) : (
              <span key={label} className={cn(tabClass(false), "cursor-not-allowed opacity-50")} title="Add a file to compare its environments">
                <Icon aria-hidden="true" />
                {label}
              </span>
            ),
          )}
        </nav>
        {repo.data.sourceUrl && (
          <a
            href={repo.data.sourceUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="ml-auto hidden min-w-0 items-center gap-1.5 rounded px-2 py-1 text-sm text-muted hover:text-ink hover:underline lg:flex"
          >
            <GitBranch className="size-4 shrink-0" aria-hidden="true" />
            <span className="max-w-64 truncate">{displayUrl(repo.data.sourceUrl)}</span>
            <ExternalLink className="size-3 shrink-0" aria-hidden="true" />
            <span className="sr-only">(opens in a new tab)</span>
          </a>
        )}
      </div>
      <div className="flex min-h-0 flex-1 flex-col md:flex-row">
        {onFiles &&
          (sidebarHidden ? (
            <div className="flex shrink-0 border-b border-line bg-surface p-1.5 md:flex-col md:border-r md:border-b-0">
              <Button variant="ghost" size="icon" className="size-8" aria-label="Show files" title="Show files" aria-expanded={false} onClick={toggleSidebar}>
                <PanelLeftOpen />
              </Button>
            </div>
          ) : (
            <aside id="repo-files" aria-label="Repo files" className="flex shrink-0 flex-col border-b border-line bg-surface md:w-64 md:overflow-y-auto md:border-r md:border-b-0">
              <div className="flex items-center gap-2 px-3 pt-2.5 pb-1.5">
                <h2 className="text-sm font-bold">
                  Files <span className="font-normal text-muted">{files.data.length}</span>
                </h2>
                <Button
                  variant="ghost"
                  size="icon"
                  className="ml-auto size-8"
                  aria-label="Hide files"
                  title="Hide files"
                  aria-expanded={true}
                  aria-controls="repo-files"
                  onClick={toggleSidebar}
                >
                  <PanelLeftClose />
                </Button>
              </div>
              <nav aria-label="Files" className="px-2">
                {files.data.length === 0 && <p className="px-2 py-1 text-sm text-muted">No files yet.</p>}
                <ul className="space-y-0.5">
                  {files.data.map((f) => (
                    <li key={f.id}>
                      <NavLink
                        to={`${base}/files/${f.id}`}
                        className={({ isActive }) =>
                          cn(
                            "flex min-h-8 items-center gap-2 rounded-md px-2 py-1 text-sm",
                            isActive ? "bg-accent-soft font-semibold text-ink" : "text-ink hover:bg-surface-2",
                          )
                        }
                      >
                        <FileText className="size-4 shrink-0 text-muted" aria-hidden="true" />
                        <span className="min-w-0 truncate font-mono">{f.name}</span>
                        <span className="ml-auto shrink-0 text-xs text-muted">{formatLabel[f.format]}</span>
                      </NavLink>
                    </li>
                  ))}
                </ul>
              </nav>
              <div className="mt-auto grid gap-1.5 p-3 md:mt-3">
                <Button size="sm" variant="primary" onClick={() => setDialog("import")}>
                  <FileUp /> Import a file
                </Button>
                <Button size="sm" onClick={() => setDialog("addFile")}>
                  <FilePlus /> Add an empty file
                </Button>
              </div>
            </aside>
          ))}
        <section aria-label={onFiles ? "File" : "Repo"} className="relative flex min-w-0 flex-1 flex-col md:overflow-y-auto">
          <Outlet />
        </section>
      </div>

      {dialog === "addFile" && <AddFileDialog repoId={repoId} onClose={() => setDialog(null)} />}
      {dialog === "import" && <ImportDialog repoId={repoId} environments={envs.data} user={user} onClose={() => setDialog(null)} />}
    </div>
  );
}

const SIDEBAR_KEY = "envgrid.sidebar";

/** The repo sidebar starts as it was left; on a phone it starts hidden, so the grid comes first. */
function initialSidebarHidden(): boolean {
  try {
    const saved = localStorage.getItem(SIDEBAR_KEY);
    if (saved) return saved === "hidden";
  } catch {
    /* storage blocked */
  }
  return window.matchMedia("(max-width: 767px)").matches;
}

/** Opens the first file, or says the repo is empty. */
export function RepoIndex() {
  const repoId = Number(useParams().repoId);
  const { repo, files } = useRepoData(repoId);
  if (!files.data || !repo.data) return <Loading />;
  if (files.data.length > 0) return <Navigate to={`files/${files.data[0].id}`} replace />;
  return (
    <div className="p-4">
      <Panel>
        <EmptyState icon={FileUp} title={repo.data.name}>
          This repo has no files yet. Use Import a file in the Files list to upload or paste one you already have (.env, JSON, YAML, INI,
          .properties, PHP constants, CSV or plain text), or add an empty file and fill it in here.
        </EmptyState>
      </Panel>
    </div>
  );
}

/** The Environments tab: the columns of every file, with how complete each is. */
export function RepoEnvironmentsPage() {
  const repoId = Number(useParams().repoId);
  const user = useMe().data!;
  const { repo, envs } = useRepoData(repoId);
  const [adding, setAdding] = useState(false);
  useDocumentTitle(repo.data ? `Environments of ${repo.data.name}` : "Environments");
  if (repo.isError) return <ErrorNotice error={repo.error} />;
  if (!repo.data || !envs.data) return <Loading />;
  return (
    <div className="mx-auto w-full max-w-4xl space-y-5 p-4 pb-16 md:p-6 md:pb-16">
      <PageHeader
        title="Environments"
        description="Each environment is a column in every file of this repo, from left to right. Protected ones can be read by everyone and changed only by admins."
      />
      <EnvironmentsManager
        repoId={repoId}
        environments={envs.data}
        coverage={repo.data.coverage}
        keys={repo.data.keyCount}
        user={user}
        onAdd={() => setAdding(true)}
      />
      {adding && <AddEnvironmentDialog repoId={repoId} user={user} onClose={() => setAdding(false)} />}
    </div>
  );
}

/** The Activity tab: the audit log of this repo only. */
export function RepoActivityPage() {
  const repoId = Number(useParams().repoId);
  const me = useMe().data!;
  const repo = useQuery({ queryKey: ["repo", repoId], queryFn: () => api.repo(repoId) });
  useDocumentTitle(repo.data ? `Activity in ${repo.data.name}` : "Activity");
  return (
    <div className="mx-auto w-full max-w-5xl space-y-5 p-4 pb-16 md:p-6 md:pb-16">
      <PageHeader title="Activity" description={`Every change, reveal, copy and export in ${repo.data?.name ?? "this repo"}, newest first.`} />
      <ActivityLog isAdmin={me.role === "admin"} meId={me.id} repoId={repoId} />
    </div>
  );
}

/** The Settings tab: name, description and link, and deleting the repo. */
export function RepoSettingsPage() {
  const repoId = Number(useParams().repoId);
  const repo = useQuery({ queryKey: ["repo", repoId], queryFn: () => api.repo(repoId) });
  useDocumentTitle(repo.data ? `Settings of ${repo.data.name}` : "Settings");
  if (repo.isError) return <ErrorNotice error={repo.error} />;
  if (!repo.data) return <Loading />;
  return <RepoSettings key={repo.data.id} repo={repo.data} />;
}

function RepoSettings({ repo }: { repo: Repo }) {
  const user = useMe().data!;
  const navigate = useNavigate();
  const announce = useAnnounce();
  const [name, setName] = useState(repo.name);
  const [description, setDescription] = useState(repo.description);
  const [sourceUrl, setSourceUrl] = useState(repo.sourceUrl);
  const [deleting, setDeleting] = useState(false);
  const save = useMutation({
    mutationFn: () => api.updateRepo(repo.id, { name, description, sourceUrl }),
    onSuccess: (r) => announce(`Saved the settings of ${r.name}.`),
  });
  const del = useMutation({
    mutationFn: () => api.deleteRepo(repo.id),
    onSuccess: () => {
      announce(`Deleted repo ${repo.name}.`);
      navigate("/repos");
    },
    onError: (e) => announce(errorMessage(e), "error"),
  });
  return (
    <div className="mx-auto w-full max-w-3xl space-y-5 p-4 pb-16 md:p-6 md:pb-16">
      <PageHeader title="Settings" description={`The name, description and code link of ${repo.name}.`} />
      <Panel title="General">
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          <Field label="Name" htmlFor="repo-settings-name">
            <Input id="repo-settings-name" required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label="Description" htmlFor="repo-settings-desc" hint="Optional. Shown on the Repos page.">
            <Input id="repo-settings-desc" value={description} onChange={(e) => setDescription(e.target.value)} />
          </Field>
          <Field label="Repository URL" htmlFor="repo-settings-source" hint="Optional. The code this config belongs to, for example https://github.com/acme/checkout.">
            <Input
              id="repo-settings-source"
              inputMode="url"
              autoComplete="url"
              placeholder="https://github.com/..."
              value={sourceUrl}
              onChange={(e) => setSourceUrl(e.target.value)}
            />
          </Field>
          {save.isError && (
            <p role="alert" className="text-sm text-missing">
              {save.error.message}
            </p>
          )}
          <Button type="submit" variant="primary" disabled={save.isPending}>
            Save changes
          </Button>
        </form>
      </Panel>
      <Panel title="Delete this repo" className="border-missing/50">
        {user.role === "admin" ? (
          <div className="flex flex-wrap items-center gap-3">
            <p className="min-w-0 flex-1 text-sm text-muted">
              Deletes every file, key, value and version in the repo. The audit log keeps a record that it existed. This cannot be undone.
            </p>
            <Button variant="danger" onClick={() => setDeleting(true)}>
              <Trash2 /> Delete repo
            </Button>
          </div>
        ) : (
          <p className="text-sm text-muted">Only admins can delete a repo.</p>
        )}
      </Panel>
      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        title={`Delete ${repo.name}?`}
        description="This deletes every file, key, value and version in the repo. The audit log keeps a record that it existed. This cannot be undone."
        confirmLabel="Delete repo"
        onConfirm={() => del.mutate()}
      />
    </div>
  );
}

/** Rename, protect, reorder and delete environments, with how complete each is. */
function EnvironmentsManager({
  repoId,
  environments,
  coverage,
  keys,
  user,
  onAdd,
}: {
  repoId: number;
  environments: Environment[];
  coverage: EnvCoverage[];
  keys: number;
  user: User;
  onAdd: () => void;
}) {
  const announce = useAnnounce();
  const isAdmin = user.role === "admin";
  const [deleting, setDeleting] = useState<Environment | null>(null);
  const onError = (e: Error) => announce(errorMessage(e), "error");
  const update = useMutation({
    mutationFn: ({ env, body }: { env: Environment; body: { name?: string; isProtected?: boolean } }) => api.updateEnvironment(env.id, body),
    onSuccess: (e) => announce(`Saved ${e.name}${e.isProtected ? " (protected)" : ""}.`),
    onError,
  });
  const reorder = useMutation({
    mutationFn: (ids: number[]) => api.reorderEnvironments(repoId, ids),
    onSuccess: () => announce("Environment order saved."),
    onError,
  });
  const remove = useMutation({
    mutationFn: (env: Environment) => api.deleteEnvironment(env.id),
    onSuccess: () => announce("Environment deleted."),
    onError,
  });
  const move = (i: number, d: number) => {
    const ids = environments.map((e) => e.id);
    [ids[i], ids[i + d]] = [ids[i + d], ids[i]];
    reorder.mutate(ids);
  };

  return (
    <Panel
      title={`${environments.length} ${environments.length === 1 ? "environment" : "environments"}`}
      description={isAdmin ? "Rename one by editing its name. The arrows change the column order." : "Only admins can protect, unprotect or delete environments."}
      actions={
        <Button size="sm" variant="primary" onClick={onAdd}>
          <Plus /> Add environment
        </Button>
      }
      bodyClassName="p-0"
    >
      <ol className="divide-y divide-line">
        {environments.map((env, i) => {
          const c = coverage.find((x) => x.environmentId === env.id);
          const present = c?.present ?? 0;
          const missing = Math.max(0, keys - present);
          const pct = keys === 0 ? 100 : Math.round((present / keys) * 100);
          return (
            <li key={env.id} className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3">
              <span className="flex w-6 justify-center text-protected">
                {env.isProtected && (
                  <>
                    <Lock className="size-4" aria-hidden="true" />
                    <span className="sr-only">protected</span>
                  </>
                )}
              </span>
              <Input
                key={env.name}
                aria-label={`Name of environment ${i + 1}`}
                defaultValue={env.name}
                className="h-8 w-44 font-mono"
                disabled={env.isProtected && !isAdmin}
                onKeyDown={(e) => e.key === "Enter" && (e.target as HTMLInputElement).blur()}
                onBlur={(e) => e.target.value.trim() !== env.name && update.mutate({ env, body: { name: e.target.value } })}
              />
              <span className="flex min-w-40 flex-1 items-center gap-3">
                <span className="hatch-missing block h-2 w-full max-w-48 overflow-hidden rounded-full border border-line" aria-hidden="true">
                  <span className="block h-full rounded-full bg-accent" style={{ width: `${pct}%` }} />
                </span>
                <span className={cn("text-xs font-semibold whitespace-nowrap", missing > 0 ? "text-missing" : "text-muted")}>
                  {keys === 0 ? "no keys" : missing > 0 ? `${missing} missing` : "complete"}
                  <span className="sr-only">
                    , {present} of {keys} keys set
                  </span>
                </span>
              </span>
              <label className={cn("flex items-center gap-1.5 text-sm", !isAdmin && "text-muted")}>
                <input
                  type="checkbox"
                  className="size-4 accent-(--accent)"
                  checked={env.isProtected}
                  disabled={!isAdmin}
                  onChange={(e) => update.mutate({ env, body: { isProtected: e.target.checked } })}
                />
                Protected
              </label>
              <div className="flex gap-1">
                <Button size="icon" variant="ghost" aria-label={`Move ${env.name} up`} disabled={i === 0} onClick={() => move(i, -1)}>
                  <ArrowUp />
                </Button>
                <Button size="icon" variant="ghost" aria-label={`Move ${env.name} down`} disabled={i === environments.length - 1} onClick={() => move(i, 1)}>
                  <ArrowDown />
                </Button>
                {isAdmin && (
                  <Button size="icon" variant="ghost" aria-label={`Delete ${env.name}`} disabled={environments.length === 1} onClick={() => setDeleting(env)}>
                    <Trash2 />
                  </Button>
                )}
              </div>
            </li>
          );
        })}
      </ol>
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={`Delete ${deleting?.name}?`}
        description="Every value in this environment, across all files of the repo, is deleted with its history. This cannot be undone."
        confirmLabel="Delete environment"
        onConfirm={() => deleting && remove.mutate(deleting)}
      />
    </Panel>
  );
}

function AddFileDialog({ repoId, onClose }: { repoId: number; onClose: () => void }) {
  const navigate = useNavigate();
  const announce = useAnnounce();
  const [name, setName] = useState("");
  const [format, setFormat] = useState<Format>("dotenv");
  const [formatTouched, setFormatTouched] = useState(false);
  const create = useMutation({
    mutationFn: () => api.createFile(repoId, { name: name.trim(), format }),
    onSuccess: (file) => {
      announce(`Created ${file.name}.`);
      onClose();
      navigate(`/repos/${repoId}/files/${file.id}`);
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    create.mutate();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <form onSubmit={submit}>
          <DialogTitle>Add an empty file</DialogTitle>
          <DialogDescription>A file is one grid: its keys are rows and your environments are columns.</DialogDescription>
          <div className="mt-4 space-y-4">
            <Field label="File name" htmlFor="file-name">
              <Input
                id="file-name"
                required
                value={name}
                placeholder="backend.env"
                className="font-mono"
                onChange={(e) => {
                  setName(e.target.value);
                  const guess = inferFormat(e.target.value);
                  if (!formatTouched && guess) setFormat(guess);
                }}
              />
            </Field>
            <Field label="Format" htmlFor="file-format" hint={format === "raw" ? "The whole text is one value per environment." : undefined}>
              <Select
                id="file-format"
                value={format}
                onValueChange={(v) => {
                  setFormat(v as Format);
                  setFormatTouched(true);
                }}
                options={formatOptions.map(([f, label]) => ({ value: f, label }))}
              />
            </Field>
            {create.isError && (
              <p role="alert" className="text-sm text-missing">
                {create.error.message}
              </p>
            )}
          </div>
          <DialogFooter>
            <Button type="submit" variant="primary" disabled={create.isPending}>
              Add file
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** Any number of environments; names may look like branches (feature/login). */
export function AddEnvironmentDialog({ repoId, user, onClose }: { repoId: number; user: User; onClose: () => void }) {
  const announce = useAnnounce();
  const [name, setName] = useState("");
  const [protect, setProtect] = useState(false);
  const create = useMutation({
    mutationFn: () => api.createEnvironment(repoId, { name, isProtected: protect }),
    onSuccess: (e) => {
      announce(`Added environment ${e.name}. It appears as a new column in every file.`);
      onClose();
    },
  });
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate();
          }}
        >
          <DialogTitle>Add environment</DialogTitle>
          <DialogDescription>
            Add as many as you need, such as staging, qa-2 or a branch like feature/login. It becomes a new column in every file of this repo.
          </DialogDescription>
          <div className="mt-4 space-y-4">
            <Field label="Name" htmlFor="new-env-name" hint="Letters, digits, spaces and . _ - / are allowed.">
              <Input id="new-env-name" required autoFocus maxLength={63} className="font-mono" value={name} onChange={(e) => setName(e.target.value)} />
            </Field>
            {user.role === "admin" ? (
              <label className="flex items-start gap-2">
                <input type="checkbox" className="mt-0.5 size-4 accent-(--accent)" checked={protect} onChange={(e) => setProtect(e.target.checked)} />
                <span>
                  <span className="font-semibold">Protected</span>
                  <span className="block text-sm text-muted">Everyone can read it; only admins can change it.</span>
                </span>
              </label>
            ) : (
              <p className="text-sm text-muted">Only admins can make an environment protected.</p>
            )}
            {create.isError && (
              <p role="alert" className="text-sm text-missing">
                {create.error.message}
              </p>
            )}
          </div>
          <DialogFooter>
            <Button onClick={onClose}>Cancel</Button>
            <Button type="submit" variant="primary" disabled={create.isPending}>
              Add environment
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
