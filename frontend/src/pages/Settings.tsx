import { useEffect, useState, type ComponentProps, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Clock, Copy, Eye, EyeOff, Keyboard, KeyRound, Lock, Palette, Plus, ScrollText, Terminal, User } from "lucide-react";
import { useMe } from "@/App";
import { PageHeader } from "@/components/PageHeader";
import { ShortcutList } from "@/components/ShortcutsDialog";
import { Badge, Button, ConfirmDialog, ErrorNotice, Field, InlineMessage, Input, Kbd, Loading, Panel, Select } from "@/components/ui";
import { api } from "@/lib/api";
import { errorMessage, useAnnounce } from "@/lib/announce";
import { hasEnded, relativeTime } from "@/lib/format";
import type { APIToken } from "@/lib/types";
import { cn, copyText } from "@/lib/utils";
import { applySize, applyTheme, getSize, getTheme, type SizeChoice, type ThemeChoice } from "@/lib/theme";

const sections = [
  { id: "profile", label: "Profile", icon: User },
  { id: "password", label: "Password", icon: Lock },
  { id: "tokens", label: "API tokens", icon: KeyRound },
  { id: "theme", label: "Theme and size", icon: Palette },
  { id: "activity-log", label: "Activity log", icon: ScrollText, admin: true },
  { id: "shortcuts", label: "Keyboard shortcuts", icon: Keyboard },
];

// On a phone the window scrolls under the sticky header; on wide screens only the main area scrolls.
const anchor = "scroll-mt-20 md:scroll-mt-6";

const dateFormat = new Intl.DateTimeFormat(undefined, { day: "numeric", month: "short", year: "numeric" });

/** "Ends on 4 Jan 2027, in 90 days." or "Has no end date." */
function endsInWords(iso: string | null) {
  if (!iso) return "Has no end date.";
  const days = Math.round((Date.parse(iso) - Date.now()) / 86_400_000);
  return `Ends on ${dateFormat.format(new Date(iso))}, in ${days} ${days === 1 ? "day" : "days"}.`;
}

/**
 * The last section whose top has reached the top of the window, just under
 * the header. A section link scrolls its section to 80 pixels from the top.
 */
function useSectionInView() {
  const [current, setCurrent] = useState(sections[0].id);
  useEffect(() => {
    const update = () => {
      let id = sections[0].id;
      for (const s of sections) {
        const top = document.getElementById(s.id)?.getBoundingClientRect().top;
        if (top !== undefined && top <= 160) id = s.id;
      }
      setCurrent(id);
    };
    // Scroll events do not bubble, so capture them: this hears the window and the main area alike.
    document.addEventListener("scroll", update, { capture: true, passive: true });
    // Also once after the first layout, for a link straight to a section.
    const first = requestAnimationFrame(update);
    return () => {
      cancelAnimationFrame(first);
      document.removeEventListener("scroll", update, { capture: true });
    };
  }, []);
  return current;
}

export function SettingsPage() {
  const me = useMe().data!;
  const current = useSectionInView();
  return (
    <div className="mx-auto w-full max-w-7xl p-4 pb-16 md:p-6 md:pb-16 lg:grid lg:grid-cols-[13rem_minmax(0,1fr)] lg:items-start lg:gap-8">
      <div className="space-y-4 lg:sticky lg:top-6">
        <PageHeader title="Settings" description="Manage your account and your preferences." />
        <nav aria-label="Settings sections">
          <ul className="flex flex-wrap gap-1.5 lg:flex-col lg:gap-0.5">
            {sections.filter((s) => !s.admin || me.role === "admin").map(({ id, label, icon: Icon }) => (
              <li key={id}>
                <a
                  href={`#${id}`}
                  aria-current={current === id ? "true" : undefined}
                  className={cn(
                    "flex min-h-9 items-center gap-2 rounded-md border px-2.5 py-1.5 text-sm",
                    current === id
                      ? "border-accent bg-accent-soft font-semibold text-ink"
                      : "border-line text-muted hover:bg-surface-2 hover:text-ink lg:border-transparent",
                  )}
                >
                  <Icon className="size-4 shrink-0" aria-hidden="true" />
                  {label}
                </a>
              </li>
            ))}
          </ul>
        </nav>
      </div>

      <div className="mt-6 space-y-4 lg:mt-0">
        <div id="profile" className={anchor}>
          <Panel
            title="Profile"
            description="Your account details. Only your name can be changed here."
            actions={<p className="text-xs text-muted">Member since {dateFormat.format(new Date(me.createdAt))}</p>}
          >
            <ProfileForm initialName={me.name} email={me.email} role={me.role} />
          </Panel>
        </div>
        <div id="password" className={anchor}>
          <Panel title="Password" description="Change the password you sign in with.">
            <PasswordForm />
          </Panel>
        </div>
        <div id="tokens" className={anchor}>
          <Panel
            title="API tokens"
            description="A token lets a script or the envgrid command line act as you, with your role. Every use is in the audit log like your own changes."
          >
            <TokensSection />
          </Panel>
        </div>
        <div id="theme" className={anchor}>
          <Panel title="Theme and size" description="Your choices are saved in this browser only.">
            <ThemePicker />
            <SizePicker />
          </Panel>
        </div>
        {me.role === "admin" && (
          <div id="activity-log" className={anchor}>
            <Panel
              title="Activity log"
              description="Choose which activity is recorded for everyone. Recording less keeps the log short; what is not recorded cannot be looked up later."
            >
              <AuditSettings />
            </Panel>
          </div>
        )}
        {/* Room below the last section, so every section can reach the top and be marked in the list. */}
        <div id="shortcuts" className={cn(anchor, "lg:min-h-[calc(100dvh-7rem)]")}>
          <Panel
            title="Keyboard shortcuts"
            description={
              <>
                Press <Kbd>?</Kbd> anywhere to see this list.
              </>
            }
          >
            <div className="md:columns-2 md:gap-8 [&_dl>div]:break-inside-avoid [&_h3]:break-after-avoid">
              <ShortcutList />
            </div>
          </Panel>
        </div>
      </div>
    </div>
  );
}

function ProfileForm({ initialName, email, role }: { initialName: string; email: string; role: string }) {
  const qc = useQueryClient();
  const announce = useAnnounce();
  const [name, setName] = useState(initialName);
  const save = useMutation({
    mutationFn: () => api.updateMe({ name }),
    onSuccess: (u) => {
      qc.setQueryData(["me"], u);
      announce("Profile saved.");
    },
  });
  return (
    <form
      className="grid gap-x-6 gap-y-4 md:grid-cols-2"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
    >
      <div className="space-y-4">
        <Field label="Email" htmlFor="profile-email" hint="Ask an admin to change it.">
          <Input id="profile-email" value={email} readOnly className="bg-surface-2" />
        </Field>
        <dl className="space-y-1">
          <dt className="text-sm font-semibold">Role</dt>
          <dd>
            <Badge className="border-accent text-accent capitalize">{role}</Badge>
          </dd>
        </dl>
      </div>
      <div className="space-y-4">
        <Field label="Name" htmlFor="profile-name" hint="Shown next to your changes in the grid and the audit log.">
          <Input id="profile-name" value={name} maxLength={100} onChange={(e) => setName(e.target.value)} />
        </Field>
        {save.isError && <p role="alert" className="text-sm text-missing">{save.error.message}</p>}
        <Button type="submit" variant="primary" disabled={save.isPending}>
          Save profile
        </Button>
      </div>
    </form>
  );
}

/** A password field with its own show button inside it. */
function PasswordInput({ id, label, hint, showLabel, ...props }: ComponentProps<"input"> & { id: string; label: string; hint?: string; showLabel: string }) {
  const [show, setShow] = useState(false);
  return (
    <Field label={label} htmlFor={id} hint={hint}>
      <div className="relative">
        <Input id={id} type={show ? "text" : "password"} className="pr-10" {...props} />
        <button
          type="button"
          onClick={() => setShow((v) => !v)}
          aria-pressed={show}
          aria-label={showLabel}
          title={showLabel}
          className="absolute top-1/2 right-0.5 inline-flex size-8 -translate-y-1/2 cursor-pointer items-center justify-center rounded-md text-muted hover:bg-surface-2 hover:text-ink"
        >
          {show ? <EyeOff className="size-4" aria-hidden="true" /> : <Eye className="size-4" aria-hidden="true" />}
        </button>
      </div>
    </Field>
  );
}

function PasswordForm() {
  const announce = useAnnounce();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [mismatch, setMismatch] = useState(false);
  const change = useMutation({
    mutationFn: () => api.updateMe({ currentPassword: current, newPassword: next }),
    onSuccess: () => {
      setCurrent("");
      setNext("");
      setConfirm("");
      announce("Password changed. Other sessions were signed out.");
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setMismatch(next !== confirm);
    if (next === confirm) change.mutate();
  };
  return (
    <form className="space-y-4" onSubmit={submit}>
      <div className="grid items-start gap-4 md:grid-cols-3">
        <PasswordInput
          id="pw-current"
          label="Current password"
          showLabel="Show current password"
          autoComplete="current-password"
          required
          value={current}
          onChange={(e) => setCurrent(e.target.value)}
        />
        <PasswordInput
          id="pw-new"
          label="New password"
          hint="At least 8 characters."
          showLabel="Show new password"
          autoComplete="new-password"
          required
          minLength={8}
          value={next}
          onChange={(e) => setNext(e.target.value)}
        />
        <PasswordInput
          id="pw-confirm"
          label="Repeat new password"
          showLabel="Show repeated password"
          autoComplete="new-password"
          required
          aria-invalid={mismatch || undefined}
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
        />
      </div>
      {mismatch && <p role="alert" className="text-sm text-missing">The new passwords do not match.</p>}
      {change.isError && <p role="alert" className="text-sm text-missing">{change.error.message}</p>}
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <p id="pw-note" className="text-sm text-muted">
          Changing your password signs you out of every other session.
        </p>
        <Button type="submit" variant="primary" disabled={change.isPending} aria-describedby="pw-note">
          Change password
        </Button>
      </div>
    </form>
  );
}

const themeOptions: [ThemeChoice, string, string][] = [
  ["system", "Match my system", "Follows your device's setting."],
  ["light", "Light", "Dark text on a light background."],
  ["dark", "Dark", "Light text on a dark background."],
];

// Fixed on purpose: each preview shows its theme whatever the page uses now.
const previewColours = {
  light: { frame: "#eceff4", card: "#ffffff", line: "#d3d9e2", accent: "#0b7672" },
  dark: { frame: "#0b1017", card: "#151c27", line: "#313d4f", accent: "#52c7bf" },
};

function ThemePreview({ name, active, onPick }: { name: "light" | "dark"; active: boolean; onPick: () => void }) {
  const c = previewColours[name];
  return (
    <button type="button" tabIndex={-1} onClick={onPick} className="cursor-pointer text-center">
      <div
        className="w-28 space-y-1.5 rounded-lg border-2 p-2"
        style={{ background: c.frame, borderColor: active ? "var(--accent)" : c.line }}
      >
        <div className="h-2 w-10 rounded-full" style={{ background: c.accent }} />
        <div className="space-y-1 rounded p-1.5" style={{ background: c.card }}>
          {["w-full", "w-3/4", "w-1/2"].map((w) => (
            <div key={w} className={cn("h-1.5 rounded-full", w)} style={{ background: c.line }} />
          ))}
        </div>
      </div>
      <p className={cn("mt-1.5 text-xs", active ? "font-semibold" : "text-muted")}>{name === "light" ? "Light" : "Dark"}</p>
    </button>
  );
}

function ThemePicker() {
  const [theme, setTheme] = useState<ThemeChoice>(getTheme);
  const choose = (t: ThemeChoice) => {
    setTheme(t);
    applyTheme(t);
  };
  const dark = theme === "dark" || (theme === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);
  return (
    <div className="flex flex-wrap items-center justify-between gap-x-10 gap-y-5">
      <fieldset className="flex flex-wrap gap-x-10 gap-y-3">
        <legend className="sr-only">Theme</legend>
        {themeOptions.map(([value, label, description]) => (
          <div key={value}>
            <label className="flex items-center gap-2 font-semibold">
              <input
                type="radio"
                name="theme"
                className="size-4 accent-(--accent)"
                checked={theme === value}
                onChange={() => choose(value)}
                aria-describedby={`theme-${value}-hint`}
              />
              {label}
            </label>
            <p id={`theme-${value}-hint`} className="ml-6 text-xs text-muted">
              {description}
            </p>
          </div>
        ))}
      </fieldset>
      {/* The radios carry the choice for keyboards and screen readers; the pictures are a shortcut for the mouse. */}
      <div className="flex gap-4" aria-hidden="true">
        <ThemePreview name="light" active={!dark} onPick={() => choose("light")} />
        <ThemePreview name="dark" active={dark} onPick={() => choose("dark")} />
      </div>
    </div>
  );
}

const sizeOptions: [SizeChoice, string, string][] = [
  ["100", "Larger (100%)", "Text and spacing at the browser's own size."],
  ["90", "Default (90%)", "Fits more rows on a laptop screen."],
  ["80", "Small (80%)", "Fits the most on screen, like zooming the browser out to 80%."],
];

/** Scales the whole app, so a small screen shows more of the grid without browser zoom. */
function SizePicker() {
  const [size, setSize] = useState<SizeChoice>(getSize);
  return (
    <div className="mt-6 border-t border-line pt-5">
      <Field label="Display size" htmlFor="display-size" hint={sizeOptions.find(([v]) => v === size)?.[2]}>
        <Select
          id="display-size"
          className="w-56"
          value={size}
          onValueChange={(v) => {
            setSize(v as SizeChoice);
            applySize(v as SizeChoice);
          }}
          options={sizeOptions.map(([value, label]) => ({ value, label }))}
        />
      </Field>
    </div>
  );
}

const expiryOptions = [
  { value: 30, label: "30 days" },
  { value: 90, label: "90 days" },
  { value: 365, label: "1 year" },
  { value: 0, label: "No end date" },
];

function TokensSection() {
  const announce = useAnnounce();
  const qc = useQueryClient();
  const tokens = useQuery({ queryKey: ["tokens"], queryFn: api.tokens });
  const [name, setName] = useState("");
  const [days, setDays] = useState(90);
  const [fresh, setFresh] = useState<{ token: string; info: APIToken } | null>(null);
  const [revoking, setRevoking] = useState<APIToken | null>(null);
  const create = useMutation({
    mutationFn: () => api.createToken(name.trim(), days),
    onSuccess: (t) => {
      setFresh(t);
      setName("");
      announce(`Created the token ${t.info.name}. Copy it now: it is shown only once.`);
      void qc.invalidateQueries({ queryKey: ["tokens"] });
    },
  });
  const revoke = async (t: APIToken) => {
    try {
      await api.deleteToken(t.id);
      announce(`Revoked ${t.name}. Scripts using it stop working at once.`);
      if (fresh?.info.id === t.id) setFresh(null);
      await qc.invalidateQueries({ queryKey: ["tokens"] });
    } catch (e) {
      announce(errorMessage(e), "error");
    }
  };
  const copy = async (text: string, done: string) => {
    try {
      await copyText(text);
      announce(done);
    } catch (e) {
      announce(errorMessage(e), "error");
    }
  };
  const dismiss = () => {
    setFresh(null);
    document.getElementById("token-name")?.focus();
  };
  const commands = `export ENVGRID_URL=${window.location.origin}
export ENVGRID_TOKEN=egt_...
envgrid export --repo my-repo --file backend.env --env production > .env
envgrid run --repo my-repo --file backend.env --env production -- node server.js`;

  return (
    <div className="space-y-5">
      <div className={cn("grid gap-4", fresh && "xl:grid-cols-2")}>
        <form
          className="rounded-lg border border-line bg-surface-alt p-4"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate();
          }}
        >
          <h3 className="mb-3 font-bold">Create a token</h3>
          <div className="flex flex-wrap items-end gap-3">
            <div className="min-w-44 flex-1">
              <Field label="Token name" htmlFor="token-name">
                <Input id="token-name" className="h-9" required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} aria-describedby="token-name-hint" />
              </Field>
            </div>
            <Field label="Ends after" htmlFor="token-days">
              <Select id="token-days" className="w-36" value={days} onValueChange={(v) => setDays(Number(v))} options={expiryOptions} />
            </Field>
            <Button type="submit" variant="primary" className="h-9" disabled={create.isPending}>
              <Plus /> Create token
            </Button>
          </div>
          <p id="token-name-hint" className="mt-2 text-xs text-muted">
            Say where it is used, for example deploy script or laptop.
          </p>
          {create.isError && (
            <p role="alert" className="mt-2 text-sm text-missing">
              {create.error.message}
            </p>
          )}
        </form>
        {fresh && (
          <InlineMessage tone="success" role="status" className="p-4" title={`Copy the token for ${fresh.info.name} now.`}>
            <p>It is not shown again; envgrid keeps only a hash of it.</p>
            <div className="mt-3 flex flex-wrap items-center gap-2">
              <code className="min-w-0 flex-1 rounded border border-line bg-surface px-2 py-1.5 font-mono text-sm break-all">{fresh.token}</code>
              <Button size="sm" onClick={() => void copy(fresh.token, "Copied the token.")}>
                <Copy /> Copy token
              </Button>
            </div>
            <p className="mt-2 flex items-center gap-1.5 text-xs">
              <Clock className="size-3.5 shrink-0" aria-hidden="true" />
              {endsInWords(fresh.info.expiresAt)}
            </p>
            <Button size="sm" className="mt-3" onClick={dismiss}>
              I have copied it
            </Button>
          </InlineMessage>
        )}
      </div>

      {tokens.isError && <ErrorNotice error={tokens.error} />}
      {!tokens.data ? (
        !tokens.isError && <Loading />
      ) : tokens.data.length === 0 ? (
        <p className="text-sm text-muted">You have no API tokens yet.</p>
      ) : (
        <div className="relative overflow-x-auto rounded-md border border-line">
          <table className="w-full min-w-[34rem] text-left text-sm">
            <caption className="px-3 py-2 text-left font-bold">Your API tokens</caption>
            <thead className="bg-surface-2">
              <tr>
                <th scope="col" className="px-3 py-2">Name</th>
                <th scope="col" className="px-3 py-2">Starts with</th>
                <th scope="col" className="px-3 py-2">Last used</th>
                <th scope="col" className="px-3 py-2">Ends</th>
                <th scope="col" className="px-3 py-2">
                  <span className="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line">
              {tokens.data.map((t) => {
                const ended = hasEnded(t.expiresAt);
                return (
                  <tr key={t.id}>
                    <td className="px-3 py-2 font-semibold">{t.name}</td>
                    <td className="px-3 py-2 font-mono">{t.prefix}</td>
                    <td className="px-3 py-2 text-muted">{relativeTime(t.lastUsedAt)}</td>
                    <td className={ended ? "px-3 py-2 font-semibold text-missing" : "px-3 py-2 text-muted"} title={t.expiresAt ?? undefined}>
                      {t.expiresAt ? (ended ? "ended" : relativeTime(t.expiresAt)) : "no end date"}
                    </td>
                    <td className="px-3 py-2 text-right">
                      <Button size="sm" className="text-missing hover:bg-missing-soft" onClick={() => setRevoking(t)}>
                        Revoke<span className="sr-only"> {t.name}</span>
                      </Button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      <div className="rounded-lg border border-line bg-surface-alt p-4">
        <div className="flex flex-wrap items-start gap-3">
          <Terminal className="mt-0.5 size-5 shrink-0 text-accent" aria-hidden="true" />
          <div className="min-w-0 flex-1 basis-64">
            <h3 className="font-bold">Use a token from the command line</h3>
            <p className="mt-1 text-sm text-muted">
              The envgrid binary is also the command line client. Set <code className="font-mono">ENVGRID_URL</code> to this server and{" "}
              <code className="font-mono">ENVGRID_TOKEN</code> to your token. Then print a file for one environment, or run a command with that
              environment's values as environment variables. References such as {"${NAME}"} are filled in.
            </p>
          </div>
          <Button size="sm" onClick={() => void copy(commands, "Copied the commands.")}>
            <Copy /> Copy commands
          </Button>
        </div>
        <pre tabIndex={0} role="region" aria-label="Commands" className="mt-3 overflow-x-auto rounded-md border border-line bg-surface p-3 font-mono text-xs">
          {commands}
        </pre>
      </div>

      <ConfirmDialog
        open={revoking !== null}
        onOpenChange={(o) => !o && setRevoking(null)}
        title={revoking ? `Revoke ${revoking.name}?` : ""}
        description="Scripts that use this token stop working at once. This cannot be undone."
        confirmLabel="Revoke token"
        onConfirm={() => revoking && void revoke(revoking)}
      />
    </div>
  );
}

/** Admins choose the kinds of activity to record; the trail of reveals, access and deletions stays on. */
function AuditSettings() {
  const announce = useAnnounce();
  const qc = useQueryClient();
  const cats = useQuery({ queryKey: ["audit-settings"], queryFn: api.auditSettings });
  const save = useMutation({
    mutationFn: (off: string[]) => api.setAuditSettings(off),
    onSuccess: (next) => {
      qc.setQueryData(["audit-settings"], next);
      announce("Saved what the activity log records.");
    },
    onError: (e) => announce(errorMessage(e), "error"),
  });
  if (cats.isError) return <ErrorNotice error={cats.error} />;
  if (!cats.data) return <Loading />;
  const toggle = (id: string, on: boolean) =>
    save.mutate(cats.data.filter((c) => !c.locked && (c.id === id ? !on : !c.enabled)).map((c) => c.id));
  return (
    <fieldset>
      <legend className="sr-only">Activity to record</legend>
      <ul className="divide-y divide-line">
        {cats.data.map((c) => (
          <li key={c.id} className="py-2.5 first:pt-0 last:pb-0">
            <label className={cn("flex items-start gap-2.5", c.locked ? "cursor-not-allowed" : "cursor-pointer")}>
              <input
                type="checkbox"
                className="mt-0.5 size-4 accent-(--accent)"
                checked={c.enabled}
                disabled={c.locked || save.isPending}
                aria-describedby={`audit-${c.id}-hint`}
                onChange={(e) => toggle(c.id, e.target.checked)}
              />
              <span>
                <span className="font-semibold">{c.label}</span>
                {c.locked && <span className="ml-2 text-xs text-muted">Always recorded</span>}
                <span id={`audit-${c.id}-hint`} className="block text-sm text-muted">
                  {c.description}
                </span>
              </span>
            </label>
          </li>
        ))}
      </ul>
    </fieldset>
  );
}
