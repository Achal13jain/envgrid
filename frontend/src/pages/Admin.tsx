import { useRef, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CalendarX, Clock, KeyRound, MoreHorizontal, Pencil, Plus, Search, ShieldCheck, TriangleAlert, UserCheck, UserX, X } from "lucide-react";
import { Navigate } from "react-router";
import { useMe } from "@/App";
import {
  Badge,
  Button,
  ConfirmDialog,
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
  ErrorNotice,
  Field,
  InlineMessage,
  Input,
  Loading,
  Menu,
  MenuContent,
  MenuItem,
  MenuTrigger,
  Select,
} from "@/components/ui";
import { PageHeader } from "@/components/PageHeader";
import { api, ApiError } from "@/lib/api";
import { errorMessage, useAnnounce } from "@/lib/announce";
import { hasEnded, personName, relativeTime } from "@/lib/format";
import type { APIToken, Role, User } from "@/lib/types";
import { cn } from "@/lib/utils";

/** Users: admins only. */
export function UsersPage() {
  const me = useMe().data!;
  if (me.role !== "admin") return <Navigate to="/" replace />;
  return (
    <div className="mx-auto w-full max-w-7xl space-y-6 p-4 pb-16 md:p-6 md:pb-16">
      <UsersPanel me={me} />
    </div>
  );
}

type Status = "active" | "disabled" | "ended";
type RowAction = "edit" | "reset" | "disable" | "enable";
type UserChanges = Parameters<typeof api.updateUser>[1];

/** A read-only list: every change goes through the edit drawer or a confirmation. */
function UsersPanel({ me }: { me: User }) {
  const announce = useAnnounce();
  const users = useQuery({ queryKey: ["users"], queryFn: api.users });
  const [adding, setAdding] = useState(false);
  const [pending, setPending] = useState<{ kind: Exclude<RowAction, "enable">; user: User } | null>(null);
  const [query, setQuery] = useState("");
  const [role, setRole] = useState("");
  const [status, setStatus] = useState("");
  const searchRef = useRef<HTMLInputElement>(null);
  const toggle = useMutation({
    mutationFn: (u: User) => api.updateUser(u.id, { disabled: !u.disabled }),
    onSuccess: (u) =>
      announce(
        u.disabled
          ? `Disabled ${u.email}. They were signed out.`
          : hasEnded(u.expiresAt)
            ? `Enabled ${u.email}. Their access end date has passed, so they still cannot sign in.`
            : `Enabled ${u.email}. They can sign in again.`,
      ),
    onError: (e) => announce(saveError(e), "error"),
  });

  const all = users.data ?? [];
  const q = query.trim().toLowerCase();
  const shown = all.filter(
    (u) => (!q || `${u.name}\n${u.email}`.toLowerCase().includes(q)) && (!role || u.role === role) && (!status || statusOf(u) === status),
  );
  const filtered = query !== "" || role !== "" || status !== "";
  const working = all.filter((u) => u.role === "admin" && !u.disabled && !u.expiresAt);
  const onlyAdminId = working.length === 1 ? working[0].id : null;
  const clearFilters = () => {
    setQuery("");
    setRole("");
    setStatus("");
    searchRef.current?.focus();
  };
  const actions = (u: User) => (
    <RowActions user={u} self={u.id === me.id} onPick={(a) => (a === "enable" ? toggle.mutate(u) : setPending({ kind: a, user: u }))} />
  );

  return (
    <>
      <PageHeader
        title="Users"
        description="Manage who can sign in to envgrid and what they can do."
        actions={
          <Button variant="primary" onClick={() => setAdding(true)}>
            <Plus /> Add user
          </Button>
        }
      />
      {users.isError ? (
        <ErrorNotice error={users.error} />
      ) : !users.data ? (
        <Loading />
      ) : (
        <>
          <div role="search" aria-label="Filter people" className="grid grid-cols-2 gap-2 rounded-xl border border-line bg-surface p-3 shadow-sm sm:flex sm:flex-wrap sm:items-center">
            <div className="relative col-span-2 sm:w-80">
              <Search className="pointer-events-none absolute top-2.5 left-2.5 size-4 text-muted" aria-hidden="true" />
              <Input
                ref={searchRef}
                type="search"
                aria-label="Search by name or email"
                placeholder="Search by name or email"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                className="pl-8"
              />
            </div>
            <Select aria-label="Filter by role" className="sm:w-36" value={role} onValueChange={setRole} options={roleFilters} />
            <Select aria-label="Filter by status" className="sm:w-40" value={status} onValueChange={setStatus} options={statusFilters} />
            {filtered && (
              <Button variant="ghost" className="col-span-2 justify-self-start" onClick={clearFilters}>
                <X /> Clear filters
              </Button>
            )}
          </div>

          {shown.length === 0 ? (
            <p className="rounded-xl border border-line bg-surface p-6 text-center text-muted">No one matches these filters.</p>
          ) : (
            <>
              <div className="hidden overflow-x-auto rounded-xl border border-line bg-surface shadow-sm lg:block">
                <table className="w-full text-left text-sm">
                  <caption className="sr-only">People</caption>
                  <thead className="bg-surface-2 text-xs text-muted">
                    <tr className="[&_th]:px-4 [&_th]:py-2.5 [&_th]:font-semibold">
                      <th scope="col">Name and email</th>
                      <th scope="col">Role</th>
                      <th scope="col">Status</th>
                      <th scope="col">Access ends</th>
                      <th scope="col">Last signed in</th>
                      <th scope="col">Added</th>
                      <th scope="col" className="text-right">
                        Actions
                      </th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-line [&_td]:px-4 [&_td]:py-3">
                    {shown.map((u) => (
                      <tr key={u.id}>
                        <td>
                          <Person user={u} self={u.id === me.id} />
                        </td>
                        <td>
                          <RoleBadge role={u.role} />
                        </td>
                        <td>
                          <StatusBadge user={u} />
                        </td>
                        <td>
                          <AccessEndsText iso={u.expiresAt} />
                        </td>
                        <td>
                          <Stamp iso={u.lastSignedInAt} />
                        </td>
                        <td>
                          <Stamp iso={u.createdAt} />
                        </td>
                        <td>{actions(u)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>

              {/* Below desktop width the table becomes one card per person. */}
              <ul className="grid gap-3 sm:grid-cols-2 lg:hidden">
                {shown.map((u) => (
                  <li key={u.id} className="rounded-xl border border-line bg-surface p-4 shadow-sm">
                    <div className="flex items-start justify-between gap-2">
                      <Person user={u} self={u.id === me.id} />
                      {actions(u)}
                    </div>
                    <div className="mt-3 flex flex-wrap gap-1.5">
                      <RoleBadge role={u.role} />
                      <StatusBadge user={u} />
                    </div>
                    <dl className="mt-3 grid grid-cols-2 gap-3 text-sm [&_dt]:text-xs [&_dt]:font-semibold [&_dt]:text-muted">
                      <div className="col-span-2">
                        <dt>Access ends</dt>
                        <dd className="mt-0.5">
                          <AccessEndsText iso={u.expiresAt} />
                        </dd>
                      </div>
                      <div>
                        <dt>Last signed in</dt>
                        <dd className="mt-0.5">
                          <Stamp iso={u.lastSignedInAt} />
                        </dd>
                      </div>
                      <div>
                        <dt>Added</dt>
                        <dd className="mt-0.5">
                          <Stamp iso={u.createdAt} />
                        </dd>
                      </div>
                    </dl>
                  </li>
                ))}
              </ul>
            </>
          )}
          <p className="text-sm text-muted" aria-live="polite">
            Showing {shown.length} of {all.length} {all.length === 1 ? "person" : "people"}.
          </p>
        </>
      )}

      {adding && <AddUserDialog onClose={() => setAdding(false)} />}
      {pending?.kind === "edit" && (
        <EditUserDrawer
          user={pending.user}
          self={pending.user.id === me.id}
          onlyAdmin={pending.user.id === onlyAdminId}
          onClose={() => setPending(null)}
        />
      )}
      {pending?.kind === "reset" && <ResetPasswordDialog user={pending.user} onClose={() => setPending(null)} />}
      <ConfirmDialog
        open={pending?.kind === "disable"}
        onOpenChange={(o) => !o && setPending(null)}
        title={pending?.kind === "disable" ? `Disable ${pending.user.email}?` : ""}
        description="They are signed out at once and their API tokens stop working. They cannot sign in until an admin enables them again. Nothing they made is removed."
        confirmLabel="Disable user"
        onConfirm={() => pending && toggle.mutate(pending.user)}
      />
    </>
  );
}

function RowActions({ user, self, onPick }: { user: User; self: boolean; onPick: (a: RowAction) => void }) {
  return (
    // The group's label stands in for the buttons when a cell's name is read
    // from its content, so the email names only the person's own cell.
    <div role="group" aria-label="Actions" className="flex shrink-0 items-center justify-end gap-1">
      <Button size="sm" aria-label={`Edit ${user.email}`} onClick={() => onPick("edit")}>
        <Pencil /> Edit
      </Button>
      <Menu>
        <MenuTrigger asChild>
          <Button variant="ghost" size="icon" aria-label={`More actions for ${user.email}`}>
            <MoreHorizontal />
          </Button>
        </MenuTrigger>
        <MenuContent align="end">
          <MenuItem onSelect={() => onPick("reset")}>
            <KeyRound /> Reset password
          </MenuItem>
          {user.disabled ? (
            <MenuItem onSelect={() => onPick("enable")}>
              <UserCheck /> Enable
            </MenuItem>
          ) : self ? (
            // Focusable but inert, so keyboard and screen reader users still hear why.
            <MenuItem aria-disabled="true" className="cursor-not-allowed items-start text-muted" onSelect={(e) => e.preventDefault()}>
              <UserX className="mt-0.5" />
              <span>
                Disable
                <span className="block text-xs">You cannot disable your own account.</span>
              </span>
            </MenuItem>
          ) : (
            <MenuItem danger onSelect={() => onPick("disable")}>
              <UserX /> Disable
            </MenuItem>
          )}
        </MenuContent>
      </Menu>
    </div>
  );
}

function EditUserDrawer({ user, self, onlyAdmin, onClose }: { user: User; self: boolean; onlyAdmin: boolean; onClose: () => void }) {
  const announce = useAnnounce();
  const qc = useQueryClient();
  const [name, setName] = useState(user.name);
  const [role, setRole] = useState<Role>(user.role);
  const [disabled, setDisabled] = useState(user.disabled);
  const [ends, setEnds] = useState(dateInput(user.expiresAt));
  const endsRef = useRef<HTMLInputElement>(null);
  const save = useMutation({
    mutationFn: (changes: UserChanges) => api.updateUser(user.id, changes),
    onSuccess: (u, changes) => {
      announce(`Saved ${u.email}: ${describeChanges(changes)}.`);
      // The refresh after every write skips "me", so do it when you edit yourself.
      if (self) void qc.invalidateQueries({ queryKey: ["me"] });
      onClose();
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    // Only what changed is sent, so a save never touches the other fields.
    const changes: UserChanges = {};
    if (name.trim() !== user.name) changes.name = name.trim();
    if (role !== user.role) changes.role = role;
    if (disabled !== user.disabled) changes.disabled = disabled;
    if (ends !== dateInput(user.expiresAt)) changes.expiresAt = ends ? endOfDay(ends) : "";
    if (Object.keys(changes).length === 0) {
      announce(`Nothing changed for ${user.email}.`);
      return onClose();
    }
    save.mutate(changes);
  };
  const end = ends ? accessEnd(endOfDay(ends)) : null;

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right">
        <form onSubmit={submit} className="flex min-h-full flex-col">
          <DialogTitle>Edit user</DialogTitle>
          <DialogDescription>Nothing changes until you save.</DialogDescription>
          <div className="mt-4 border-y border-line py-4">
            <Person user={user} self={self} large />
          </div>
          <div className="mt-5 space-y-5">
            <Field label="Name" htmlFor="edit-name" hint="Optional.">
              <Input id="edit-name" value={name} onChange={(e) => setName(e.target.value)} />
            </Field>
            <Field
              label="Role"
              htmlFor="edit-role"
              hint={`Members can read everything and change values outside protected environments; in a protected one they propose a change for an admin to approve. Admins can also change protected environments, approve those changes, set key rules, delete environments, files and repos, and manage users.${self && role === "member" ? " If you make yourself a member, you can no longer manage users." : ""}`}
            >
              <Select id="edit-role" value={role} onValueChange={(v) => setRole(v as Role)} options={roleOptions} />
            </Field>
            <Field
              label="Status"
              htmlFor="edit-status"
              hint={
                self
                  ? "You cannot disable your own account, because that would sign you out at once."
                  : disabled
                    ? "They cannot sign in or use their API tokens until you enable them again."
                    : "Disabling someone signs them out at once and stops their API tokens. You can enable them again later."
              }
            >
              <Select
                id="edit-status"
                disabled={self}
                value={disabled ? "disabled" : "active"}
                onValueChange={(v) => setDisabled(v === "disabled")}
                options={[
                  { value: "active", label: "Active" },
                  { value: "disabled", label: "Disabled" },
                ]}
              />
            </Field>
            <Field label="Access ends" htmlFor="edit-ends" hint="They lose access at the end of this day, in your time zone. Leave it empty for no end date.">
              <div className="flex gap-2">
                <Input ref={endsRef} id="edit-ends" type="date" value={ends} onChange={(e) => setEnds(e.target.value)} />
                {ends && (
                  <Button
                    variant="ghost"
                    size="icon"
                    className="size-9"
                    aria-label="Clear the access end date"
                    onClick={() => {
                      setEnds("");
                      endsRef.current?.focus();
                    }}
                  >
                    <X />
                  </Button>
                )}
              </div>
            </Field>
            {end?.ended ? (
              <InlineMessage tone="error" title="This date has passed.">
                They cannot sign in while it is set. Clear it or choose a later date to give them access.
              </InlineMessage>
            ) : (
              end?.soon && (
                <InlineMessage tone="warning" title={`Access ends ${end.when}.`}>
                  They lose access at the end of {end.date}.
                </InlineMessage>
              )
            )}
            <dl className="text-sm">
              <dt className="font-semibold">Last signed in</dt>
              <dd className="mt-1">
                {user.lastSignedInAt ? (
                  <>
                    {formatDate(user.lastSignedInAt)} at {formatTime(user.lastSignedInAt)}{" "}
                    <span className="text-muted">({relativeTime(user.lastSignedInAt)})</span>
                  </>
                ) : (
                  "They have never signed in."
                )}
              </dd>
            </dl>
            {!self && <UserTokens user={user} />}
            {user.role === "admin" && (
              <InlineMessage
                tone={onlyAdmin ? "warning" : "info"}
                title={onlyAdmin ? "This is the only working admin." : "envgrid always keeps at least one working admin."}
              >
                {onlyAdmin
                  ? "envgrid always keeps at least one working admin, meaning an admin who is not disabled and has no access end date. Until someone else is a working admin, this person cannot be disabled, made a member or given an end date."
                  : "A working admin is not disabled and has no access end date, so the last one cannot be disabled, made a member or given an end date."}
              </InlineMessage>
            )}
            {save.isError && (
              <InlineMessage tone="error" role="alert" title="Your changes were not saved.">
                {saveError(save.error)}
              </InlineMessage>
            )}
          </div>
          <DialogFooter className="mt-auto pt-5">
            <DialogClose asChild>
              <Button>Cancel</Button>
            </DialogClose>
            <Button type="submit" variant="primary" disabled={save.isPending}>
              Save changes
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** Someone else's API tokens, each with Revoke. Your own are in Settings. */
function UserTokens({ user }: { user: User }) {
  const announce = useAnnounce();
  const qc = useQueryClient();
  const [revoking, setRevoking] = useState<APIToken | null>(null);
  const tokens = useQuery({ queryKey: ["user-tokens", user.id], queryFn: () => api.userTokens(user.id) });
  const revoke = useMutation({
    mutationFn: (t: APIToken) => api.deleteUserToken(user.id, t.id),
    onSuccess: (_, t) => {
      announce(`Revoked ${t.name}. Scripts using it stop working at once.`);
      void qc.invalidateQueries({ queryKey: ["user-tokens", user.id] });
    },
    onError: (e) => announce(errorMessage(e)),
  });
  return (
    <section aria-labelledby="user-tokens-title" className="text-sm">
      <h3 id="user-tokens-title" className="font-semibold">
        API tokens
      </h3>
      {tokens.isPending && <Loading label="Loading tokens" />}
      {tokens.isError && <ErrorNotice error={tokens.error} />}
      {tokens.data?.length === 0 && <p className="mt-1 text-muted">They have no API tokens.</p>}
      {!!tokens.data?.length && (
        <ul className="mt-2 divide-y divide-line rounded-lg border border-line">
          {tokens.data.map((t) => (
            <li key={t.id} className="flex items-center gap-3 px-3 py-2">
              <span className="min-w-0 flex-1">
                <span className="block truncate font-semibold">{t.name}</span>
                <span className="block text-xs text-muted">
                  <span className="font-mono">{t.prefix}</span>, last used {t.lastUsedAt ? relativeTime(t.lastUsedAt) : "never"},{" "}
                  {t.expiresAt ? (hasEnded(t.expiresAt) ? "ended" : `ends ${relativeTime(t.expiresAt)}`) : "no end date"}
                </span>
              </span>
              <Button size="sm" className="text-missing hover:bg-missing-soft" onClick={() => setRevoking(t)}>
                Revoke<span className="sr-only"> {t.name}</span>
              </Button>
            </li>
          ))}
        </ul>
      )}
      <ConfirmDialog
        open={revoking !== null}
        onOpenChange={(o) => !o && setRevoking(null)}
        title={revoking ? `Revoke ${revoking.name}?` : ""}
        description="Scripts that use this token stop working at once. This cannot be undone."
        confirmLabel="Revoke token"
        onConfirm={() => revoking && revoke.mutate(revoking)}
      />
    </section>
  );
}

function AddUserDialog({ onClose }: { onClose: () => void }) {
  const announce = useAnnounce();
  const [form, setForm] = useState({ email: "", name: "", password: "", role: "member" as Role });
  const create = useMutation({
    mutationFn: () => api.createUser(form),
    onSuccess: (u) => {
      announce(`Added ${u.email}. Share the password with them through a safe channel.`);
      onClose();
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
          <DialogTitle>Add user</DialogTitle>
          <DialogDescription>They sign in with this email and password, and can change the password in Settings.</DialogDescription>
          <div className="mt-4 space-y-4">
            <Field label="Email" htmlFor="new-email">
              <Input id="new-email" type="email" required value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} />
            </Field>
            <Field label="Name" htmlFor="new-name" hint="Optional.">
              <Input id="new-name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
            </Field>
            <Field label="Initial password" htmlFor="new-password" hint="At least 8 characters.">
              <Input
                id="new-password"
                type="password"
                autoComplete="new-password"
                required
                minLength={8}
                value={form.password}
                onChange={(e) => setForm({ ...form, password: e.target.value })}
              />
            </Field>
            <Field label="Role" htmlFor="new-role">
              <Select id="new-role" value={form.role} onValueChange={(v) => setForm({ ...form, role: v as Role })} options={roleOptions} />
            </Field>
            {create.isError && <p role="alert" className="text-sm text-missing">{create.error.message}</p>}
          </div>
          <DialogFooter>
            <Button type="submit" variant="primary" disabled={create.isPending}>
              Add user
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function ResetPasswordDialog({ user, onClose }: { user: User; onClose: () => void }) {
  const announce = useAnnounce();
  const [password, setPassword] = useState("");
  const reset = useMutation({
    mutationFn: () => api.updateUser(user.id, { password }),
    onSuccess: () => {
      announce(`Reset the password of ${user.email}. Their sessions were signed out.`);
      onClose();
    },
  });
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            reset.mutate();
          }}
        >
          <DialogTitle>Reset password</DialogTitle>
          <DialogDescription>Sets a new password for {user.email} and signs them out everywhere.</DialogDescription>
          <div className="mt-4 space-y-4">
            <Field label="New password" htmlFor="reset-password" hint="At least 8 characters.">
              <Input id="reset-password" type="password" autoComplete="new-password" required minLength={8} value={password} onChange={(e) => setPassword(e.target.value)} />
            </Field>
            {reset.isError && <p role="alert" className="text-sm text-missing">{reset.error.message}</p>}
          </div>
          <DialogFooter>
            <Button type="submit" variant="primary" disabled={reset.isPending}>
              Reset password
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** The initial, name and email; the initial is decoration, so screen readers skip it. */
function Person({ user, self, large }: { user: User; self: boolean; large?: boolean }) {
  const name = personName(user);
  return (
    <div className="flex min-w-0 items-center gap-3">
      <span
        aria-hidden="true"
        className={cn("flex shrink-0 items-center justify-center rounded-full bg-accent-soft font-bold text-accent", large ? "size-12 text-lg" : "size-9")}
      >
        {[...name][0].toUpperCase()}
      </span>
      <div className="min-w-0 [overflow-wrap:anywhere]">
        <div className="font-semibold">
          {name}
          {self && <Badge className="ml-2 align-middle">You</Badge>}
        </div>
        {name !== user.email && <div className="text-muted">{user.email}</div>}
      </div>
    </div>
  );
}

function RoleBadge({ role }: { role: Role }) {
  return role === "admin" ? (
    <Badge className="border-ink/30 text-ink">
      <ShieldCheck className="size-3" aria-hidden="true" /> Admin
    </Badge>
  ) : (
    <Badge>Member</Badge>
  );
}

const statuses: Record<Status, { label: string; className: string }> = {
  active: { label: "Active", className: "border-accent/40 bg-accent-soft text-accent" },
  disabled: { label: "Disabled", className: "bg-surface-2" },
  ended: { label: "Access ended", className: "border-missing/40 bg-missing-soft text-missing" },
};

function statusOf(u: User): Status {
  return u.disabled ? "disabled" : hasEnded(u.expiresAt) ? "ended" : "active";
}

function StatusBadge({ user }: { user: User }) {
  const s = statuses[statusOf(user)];
  return <Badge className={s.className}>{s.label}</Badge>;
}

/** An end date in words, with an icon and tone for soon (within 14 days) and passed. */
function AccessEndsText({ iso }: { iso: string | null }) {
  if (!iso)
    return (
      <span className="text-muted">
        <span aria-hidden="true">-</span>
        <span className="sr-only">No end date</span>
      </span>
    );
  const end = accessEnd(iso);
  const Icon = end.ended ? CalendarX : end.soon ? TriangleAlert : Clock;
  return (
    <div className={cn("inline-flex items-start gap-2", end.soon && "rounded-md bg-protected-soft px-2 py-1")}>
      <Icon className={cn("mt-0.5 size-4 shrink-0", end.ended ? "text-missing" : end.soon ? "text-protected" : "text-muted")} aria-hidden="true" />
      <div>
        <div className={cn(end.ended && "text-missing", end.soon && "font-semibold text-protected")}>
          {end.ended ? "Access ended on" : "Access ends on"} {end.date}
        </div>
        <div className={cn("text-xs", end.soon ? "text-protected" : "text-muted")}>{end.when}</div>
      </div>
    </div>
  );
}

/** A date over its time, or "Never". */
function Stamp({ iso }: { iso: string | null }) {
  if (!iso) return <span className="text-muted">Never</span>;
  return (
    <time dateTime={iso} className="block whitespace-nowrap">
      {formatDate(iso)}
      <span className="block text-xs text-muted">{formatTime(iso)}</span>
    </time>
  );
}

const formatDate = (iso: string) => new Date(iso).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" });
const formatTime = (iso: string) => new Date(iso).toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
const dayWords = new Intl.RelativeTimeFormat("en", { numeric: "auto" });

/** Where an end date stands: passed, within 14 days, or later, with "in 5 days" style words. */
function accessEnd(iso: string, now: Date = new Date()) {
  const start = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  // Rounded, so a 23 or 25 hour day around a clock change still counts as one.
  const days = Math.round((start(new Date(iso)) - start(now)) / 86_400_000);
  const ended = hasEnded(iso, now);
  const soon = !ended && days <= 14;
  return { date: formatDate(iso), ended, soon, when: soon ? dayWords.format(days, "day") : relativeTime(iso, now) };
}

/** "2026-12-31" for a date input, in local time. */
function dateInput(iso: string | null): string {
  if (!iso) return "";
  const d = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** Access lasts to the end of the chosen local day. */
const endOfDay = (day: string) => new Date(`${day}T23:59:59`).toISOString();

/** The saved changes as words for the announcement. */
function describeChanges(c: UserChanges): string {
  const parts: string[] = [];
  if (c.name !== undefined) parts.push(c.name ? `name changed to ${c.name}` : "name removed");
  if (c.role) parts.push(c.role === "admin" ? "now an admin" : "now a member");
  if (c.disabled !== undefined) parts.push(c.disabled ? "disabled and signed out" : "enabled");
  if (c.expiresAt !== undefined) parts.push(c.expiresAt ? `access ends on ${formatDate(c.expiresAt)}` : "no access end date");
  return parts.join(", ");
}

/** A refused save in plain words; the server's last-admin message is terse. */
function saveError(e: unknown): string {
  if (e instanceof ApiError && e.code === "last_admin")
    return "envgrid needs at least one working admin, meaning an admin who is not disabled and has no access end date. Make sure another working admin exists first.";
  return errorMessage(e);
}

const roleOptions = [
  { value: "member", label: "Member" },
  { value: "admin", label: "Admin" },
];

const roleFilters = [{ value: "", label: "All roles" }, { value: "admin", label: "Admins" }, { value: "member", label: "Members" }];

const statusFilters = [{ value: "", label: "All statuses" }, ...Object.entries(statuses).map(([value, s]) => ({ value, label: s.label }))];
