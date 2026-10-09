import { useEffect, useState, type ReactNode } from "react";
import {
  ArrowLeftRight,
  ArrowUpRight,
  Check,
  ChevronDown,
  CircleCheck,
  CircleSlash,
  CircleX,
  Cloud,
  Copy,
  EyeOff,
  FileText,
  FileUp,
  FolderSearch,
  GitPullRequestArrow,
  History,
  KeyRound,
  LayoutGrid,
  Lightbulb,
  Lock,
  MessageCircleMore,
  RefreshCw,
  ScrollText,
  ShieldCheck,
  Terminal,
  TriangleAlert,
  UserRound,
  UsersRound,
  Zap,
  type LucideIcon,
} from "lucide-react";
import { Link } from "react-router";
import { Logo, ThemeButton } from "@/components/AppShell";
import { useDocumentTitle } from "@/components/PageHeader";
import { Button } from "@/components/ui";
import { useAnnounce } from "@/lib/announce";
import { MASK } from "@/lib/format";
import { cn, copyText } from "@/lib/utils";
import { REPO_URL, SITE } from "@/site";

// Everything on this page restates the README; keep the two in step.

const benefits: [LucideIcon, string, string, string][] = [
  [Lock, "Keep secrets safe", "Encrypted at rest and masked until someone asks.", "bg-accent-soft text-accent"],
  [UsersRound, "Work together", "Roles, protected environments and change requests.", "bg-[color-mix(in_oklab,var(--diff-a)_14%,transparent)] text-(--diff-a)"],
  [ShieldCheck, "Stay accountable", "Every value keeps its history, and activity is logged.", "bg-[color-mix(in_oklab,var(--changed)_14%,transparent)] text-changed"],
  [Zap, "Move faster", "See every gap at a glance and copy values across.", "bg-protected-soft text-protected"],
];

const without: [LucideIcon, string, string][] = [
  [FileText, "Manual tracking", "Each environment's .env file drifts on its own, and prod finds out first."],
  [MessageCircleMore, "Lost in messages", "Secrets travel through chat messages and shared documents."],
  [UserRound, "No change history", "Nobody knows who changed prod, or what it was before."],
];
const withIt: [LucideIcon, string, string][] = [
  [LayoutGrid, "One source of truth", "One grid marks every value that is missing or different, per environment."],
  [Lock, "Encrypted at rest", "Values are encrypted at rest, secrets stay masked, and reveals are recorded by default."],
  [History, "Full history", "Every value keeps its versions, with who and when, and any of them can be restored."],
];

const groups: { icon: LucideIcon; title: string; text: string; features: [LucideIcon, string, string][] }[] = [
  {
    icon: LayoutGrid,
    title: "See the whole picture",
    text: "Know what each environment has before anything ships.",
    features: [
      [LayoutGrid, "The grid", "Keys as rows, environments as columns, gaps hatched and counted."],
      [ArrowLeftRight, "Compare", "Two environments side by side, with the changed words marked and a copy button."],
      [Lightbulb, "Insights", "Secrets not changed for months, secrets used twice, and values that break a rule."],
    ],
  },
  {
    icon: ShieldCheck,
    title: "Change it safely",
    text: "Protected environments, reviews and a full history.",
    features: [
      [GitPullRequestArrow, "Change requests", "Members propose changes to prod; an admin approves them."],
      [History, "History and restore", "Every version kept; restore one value or a whole environment to a time."],
      [TriangleAlert, "Rules", "Mark keys required or give them a pattern; bad values are refused."],
    ],
  },
  {
    icon: FileUp,
    title: "Bring what you have",
    text: "No migration project: start from the files you already use.",
    features: [
      [FileUp, "Import any file", ".env, JSON, YAML, INI, .properties, PHP constants and CSV, detected for you."],
      [KeyRound, "Any number of environments", "test, uat and prod, or branch names such as feature/login."],
      [Terminal, "Command line and API", "Export a file or run a command with its values, using a personal token."],
    ],
  },
];

const security: [LucideIcon, string, string][] = [
  [ShieldCheck, "Encrypted at rest", "Every value is encrypted with AES-256-GCM before it reaches the database, bound to its key and environment."],
  [EyeOff, "Masked until asked", "Secrets reach the browser masked. Revealing, copying or exporting one is recorded in the activity log by default."],
  [KeyRound, "Careful with passwords", "argon2id password hashes, five sign-in attempts a minute per address, and only hashes of session tokens stored."],
  [ScrollText, "Nothing leaks into logs", "A test sends a known value through every path and fails if it ever appears in the logs."],
];

const limits: [LucideIcon, string, string][] = [
  [Cloud, "It is not a cloud service.", "You run it, and your data stays in one SQLite file on your disk."],
  [RefreshCw, "It has no integrations.", "It does not sync with cloud secret managers, CI systems or Kubernetes, and nothing calls out to the internet."],
  [UserRound, "It has no single sign-on yet.", "Accounts are email and password, created by an admin."],
];

const nav = [
  ["#features", "Features"],
  ["#security", "Security"],
  ["/docs", "Docs"],
  ["https://github.com/Achal13jain/envgrid", "GitHub"],
];

/** The public front page: what envgrid is, how it protects values, and how to run it. */
export function LandingPage() {
  useDocumentTitle("Every key, in every environment");
  return (
    <div className="min-h-dvh bg-background">
      <a href="#main" className="sr-only z-50 rounded-md bg-accent px-3 py-2 text-accent-ink focus:not-sr-only focus:fixed focus:top-2 focus:left-2">
        Skip to content
      </a>
      <header className="sticky top-0 z-30 border-b border-line bg-surface/95 backdrop-blur">
        <div className="mx-auto flex h-16 max-w-6xl items-center gap-8 px-4">
          <Link to="/" className="rounded-md" aria-label="envgrid home">
            <Logo className="text-xl" />
          </Link>
          <nav aria-label="On this page" className="hidden items-center gap-6 text-sm font-semibold text-muted md:flex">
            {nav.map(([href, label]) => (
              <a key={href} href={href} className="hover:text-ink">
                {label}
              </a>
            ))}
          </nav>
          <div className="ml-auto flex items-center gap-2">
            <ThemeButton />
            <Button asChild variant="primary">
              {SITE ? <Link to="/docs">Get started</Link> : <Link to="/signin">Sign in</Link>}
            </Button>
          </div>
        </div>
      </header>

      <main id="main">
        <section className="relative overflow-hidden border-b border-line">
          <div className="mx-auto grid max-w-6xl grid-cols-1 items-center gap-14 px-4 pt-10 pb-16 md:pb-20 lg:grid-cols-[1fr_1.05fr]">
            <div>
              <p className="inline-flex items-center gap-2 rounded-full bg-accent-soft px-3 py-1 text-sm font-semibold text-accent">
                <ShieldCheck className="size-4" aria-hidden="true" /> Open source, self-hosted, your data stays with you
              </p>
              <h1 className="mt-5 text-4xl leading-[1.05] font-bold tracking-tight sm:text-5xl lg:text-6xl">Every key, in every environment, side by side.</h1>
              <p className="mt-6 max-w-xl text-lg text-muted">
                envgrid is a small, open-source home for your team's environment variables, constants and config files. You run it yourself, and you see
                what prod is missing before it ships.
              </p>
              <div className="mt-8 flex flex-wrap items-center gap-3">
                <Button asChild variant="primary" className="h-11 px-5 text-base">
                  {SITE ? (
                    <Link to="/docs">
                      <ArrowUpRight /> Get started
                    </Link>
                  ) : (
                    <Link to="/signin">
                      <ArrowUpRight /> Sign in
                    </Link>
                  )}
                </Button>
                <Button asChild className="h-11 px-5 text-base">
                  {SITE ? <a href={REPO_URL}>View on GitHub</a> : <Link to="/docs">Read the docs</Link>}
                </Button>
              </div>
              <p className="mt-5 text-sm text-muted">MIT licence. One binary and one SQLite file. Running in about a minute.</p>
            </div>
            <ProductShot />
          </div>
        </section>

        {SITE && (
          <section id="tour" aria-label="A short tour of envgrid" className="border-b border-line bg-surface-alt">
            <div className="mx-auto max-w-6xl px-4 py-14">
              <TourVideo />
            </div>
          </section>
        )}

        <section aria-label="Why teams use it" className="border-b border-line bg-surface">
          <ul className="mx-auto grid max-w-6xl gap-8 px-4 py-10 sm:grid-cols-2 lg:grid-cols-4 lg:gap-0 lg:divide-x lg:divide-line">
            {benefits.map(([Icon, title, text, tone]) => (
              <li key={title} className="flex gap-4 lg:px-6 lg:first:pl-0 lg:last:pr-0">
                <IconTile icon={Icon} className={tone} />
                <span>
                  <span className="block font-bold">{title}</span>
                  <span className="mt-1 block text-sm text-muted">{text}</span>
                </span>
              </li>
            ))}
          </ul>
        </section>

        <Section id="why" title="Why a grid" text="Config usually lives in one file per environment, and the differences hide between them." art={<StackedFiles />}>
          <div className="grid overflow-hidden rounded-2xl border border-line bg-surface shadow-sm md:grid-cols-2 md:divide-x md:divide-line">
            <Column tone="missing" icon={CircleX} title="Without envgrid" rows={without} />
            <Column tone="accent" icon={CircleCheck} title="With envgrid" rows={withIt} />
          </div>
        </Section>

        <Section id="features" title="What it does" text="Everything a team needs to keep config in step, and nothing it has to set up first." art={<InSync />} tinted>
          <div className="grid gap-5 lg:grid-cols-3">
            {groups.map((g) => (
              <section key={g.title} aria-labelledby={`group-${g.title}`} className="rounded-2xl border border-line bg-surface p-6 shadow-sm">
                <div className="flex gap-4">
                  <IconTile icon={g.icon} className="bg-accent-soft text-accent" />
                  <div>
                    <h3 id={`group-${g.title}`} className="text-lg font-bold">
                      {g.title}
                    </h3>
                    <p className="mt-1 text-sm text-muted">{g.text}</p>
                  </div>
                </div>
                <ul className="mt-5 divide-y divide-line border-t border-line">
                  {g.features.map(([Icon, name, text]) => (
                    <li key={name} className="flex gap-4 py-4 last:pb-0">
                      <IconTile icon={Icon} small className="bg-surface-2 text-accent" />
                      <span>
                        <span className="block font-semibold">{name}</span>
                        <span className="mt-0.5 block text-sm text-muted">{text}</span>
                      </span>
                    </li>
                  ))}
                </ul>
              </section>
            ))}
          </div>
        </Section>

        <Section id="security" title="Built to be trusted with secrets" text="The safeguards are on by default; there is nothing to switch on.">
          <ul className="grid gap-5 sm:grid-cols-2">
            {security.map(([Icon, title, text]) => (
              <li key={title} className="flex gap-4 rounded-2xl border border-line bg-surface p-6 shadow-sm">
                <IconTile icon={Icon} className="bg-accent-soft text-accent" />
                <span>
                  <span className="block text-lg font-bold">{title}</span>
                  <span className="mt-1 block text-sm text-muted">{text}</span>
                </span>
              </li>
            ))}
          </ul>
          <div className="mt-5 flex gap-4 rounded-2xl border border-line bg-surface-alt p-6">
            <IconTile icon={Lock} className="bg-surface text-accent" />
            <p className="self-center text-sm text-muted">
              <span className="font-bold text-ink">What it cannot protect against:</span> anyone with both the database and the master key can read everything,
              and an admin can read every value. Run it behind HTTPS and keep the master key apart from your backups.
            </p>
          </div>
        </Section>

        <Section id="limits" title="What it is not" text="So you can decide quickly whether it fits." art={<LimitsArt />} tinted>
          <ul className="max-w-2xl space-y-6">
            {limits.map(([Icon, title, text]) => (
              <li key={title} className="flex gap-4">
                <IconTile icon={Icon} className="bg-accent-soft text-accent" />
                <span>
                  <span className="block font-bold">{title}</span>
                  <span className="mt-0.5 block text-muted">{text}</span>
                </span>
              </li>
            ))}
          </ul>
        </Section>
      </main>

      <footer className="border-t border-line bg-surface-alt">
        <div className="mx-auto flex max-w-6xl flex-wrap items-center gap-x-8 gap-y-4 px-4 py-8 text-sm text-muted">
          <Logo />
          <p>Open source under the MIT licence.</p>
          <nav aria-label="Footer" className="flex flex-wrap gap-x-6 gap-y-2 md:ml-auto">
            {nav.map(([href, label]) => (
              <a key={href} href={href} className="hover:text-ink">
                {label}
              </a>
            ))}
            {!SITE && (
              <Link to="/signin" className="font-semibold text-ink hover:underline">
                Sign in
              </Link>
            )}
          </nav>
        </div>
      </footer>
    </div>
  );
}

function IconTile({ icon: Icon, className, small }: { icon: LucideIcon; className?: string; small?: boolean }) {
  return (
    <span className={cn("flex shrink-0 items-center justify-center rounded-xl", small ? "size-9" : "size-12", className)} aria-hidden="true">
      <Icon className={small ? "size-4" : "size-5"} />
    </span>
  );
}

function Section({
  id,
  title,
  text,
  art,
  tinted,
  children,
}: {
  id: string;
  title: string;
  text: string;
  art?: ReactNode;
  tinted?: boolean;
  children: ReactNode;
}) {
  return (
    <section id={id} aria-labelledby={`${id}-title`} className={cn("scroll-mt-16 border-b border-line", tinted && "bg-surface-alt")}>
      <div className="mx-auto max-w-6xl px-4 py-16 md:py-20">
        <div className="mb-8 flex flex-wrap items-end justify-between gap-8">
          <div className="max-w-2xl">
            <h2 id={`${id}-title`} className="text-4xl font-bold tracking-tight md:text-5xl">
              {title}
            </h2>
            <p className="mt-3 text-lg text-muted">{text}</p>
          </div>
          {art && (
            <div className="hidden md:block" aria-hidden="true">
              {art}
            </div>
          )}
        </div>
        {children}
      </div>
    </section>
  );
}

function Column({ tone, icon: Icon, title, rows }: { tone: "missing" | "accent"; icon: LucideIcon; title: string; rows: [LucideIcon, string, string][] }) {
  const soft = tone === "missing" ? "bg-missing-soft text-missing" : "bg-accent-soft text-accent";
  return (
    <div className="p-6 md:row-span-4 md:grid md:grid-rows-subgrid">
      <h3 className={cn("mb-4 inline-flex items-center gap-2 rounded-full px-4 py-1.5 text-lg font-bold md:justify-self-start", soft)}>
        <Icon className="size-5" aria-hidden="true" /> <span className="text-ink">{title}</span>
      </h3>
      <ul className="divide-y divide-line md:contents">
        {rows.map(([RowIcon, name, text]) => (
          <li key={name} className="flex gap-4 py-5 last:pb-0">
            <IconTile icon={RowIcon} className={soft} />
            <span>
              <span className="block font-bold">{name}</span>
              <span className="mt-1 block text-sm text-muted">{text}</span>
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

/** A step of the install, which really is a sequence. */
export function Code({ text }: { text: string }) {
  const announce = useAnnounce();
  const [copied, setCopied] = useState(false);
  return (
    <div className="flex items-start gap-2 rounded-xl bg-[#17212f] p-2.5 text-[#e6edf5]">
      <pre className="min-w-0 flex-1 overflow-x-auto p-1.5 font-mono text-sm">{text}</pre>
      <Button
        size="sm"
        className="shrink-0 border-white/20 bg-white/5 text-[#e6edf5] hover:bg-white/10"
        onClick={() =>
          void copyText(text).then(() => {
            announce("Copied the command.");
            setCopied(true);
            setTimeout(() => setCopied(false), 2000);
          })
        }
      >
        {copied ? <Check /> : <Copy />} {copied ? "Copied" : "Copy"}
      </Button>
    </div>
  );
}

// Decorative pictures. Each is hidden from screen readers by its container.

/** Soft shapes and a dot pattern behind a picture. */
function Backdrop({ className }: { className?: string }) {
  return (
    <>
      <span className={cn("absolute rounded-[45%] bg-accent-soft dark:opacity-50", className)} />
      <span className="absolute -top-4 -right-6 size-24 bg-[radial-gradient(var(--line)_1.5px,transparent_1.5px)] [background-size:12px_12px]" />
    </>
  );
}

/** The app in miniature: the files and environments of a repo, its grid, and a reminder. */
function ProductShot() {
  const [shown, setShown] = useState(false);
  useEffect(() => {
    const t = setTimeout(() => setShown(true), window.matchMedia("(prefers-reduced-motion: reduce)").matches ? 0 : 900);
    return () => clearTimeout(t);
  }, []);
  const envs: [string, string][] = [
    ["prod", "bg-accent"],
    ["uat", "bg-(--diff-a)"],
    ["test", "bg-(--diff-b)"],
  ];
  const rows: [string, (string | null)[]][] = [
    ["DATABASE_URL", [MASK, MASK, MASK]],
    ["REDIS_URL", [null, "redis://uat", "redis://test"]],
    ["SMTP_HOST", [null, "smtp.uat", "smtp.test"]],
    ["FEATURE_FLAG", ["on", "on", "off"]],
  ];
  return (
    <div className="relative mx-auto w-full max-w-xl" aria-hidden="true">
      <Backdrop className="-inset-x-8 -inset-y-10 rotate-6" />
      <div className="relative overflow-hidden rounded-2xl border border-line bg-surface shadow-2xl">
        <div className="flex gap-1.5 bg-[#334155] px-4 py-2.5">
          {[0, 1, 2].map((i) => (
            <span key={i} className="size-2.5 rounded-full bg-white/40" />
          ))}
        </div>
        <div className="grid grid-cols-[8.5rem_minmax(0,1fr)] text-xs">
          <div className="space-y-1 border-r border-line p-3">
            <p className="mb-2 flex items-center gap-1.5 font-bold">
              acme-shop <ChevronDown className="size-3 text-muted" />
            </p>
            {envs.map(([e, dot], i) => (
              <p key={e} className={cn("flex items-center gap-2 rounded-md px-2 py-1.5 font-semibold", i === 0 && "bg-accent-soft")}>
                <span className={cn("size-2 rounded-full", dot)} /> {e}
              </p>
            ))}
            <hr className="my-2 border-line" />
            {[".env", "config.yml", "flags.ini"].map((f) => (
              <p key={f} className="flex items-center gap-2 px-2 py-1 text-muted">
                <FileText className="size-3.5" /> {f}
              </p>
            ))}
          </div>
          <table className="m-3 table-fixed border-separate border-spacing-0 font-mono">
            <thead>
              <tr className="text-muted">
                <th className="w-[38%] pb-2 text-left font-sans font-semibold">Key</th>
                {envs.map(([e]) => (
                  <th key={e} className="pb-2 text-left font-sans font-semibold">
                    {e}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map(([key, cells]) => (
                <tr key={key}>
                  <td className="truncate border-t border-line py-2 pr-2 font-semibold">{key}</td>
                  {cells.map((c, j) =>
                    c === null ? (
                      <td key={j} className="hatch-missing border-t border-line px-1 py-2 font-sans font-semibold text-missing">
                        missing
                      </td>
                    ) : c === "on" || c === "off" ? (
                      <td key={j} className="border-t border-line py-2">
                        <span className={cn("rounded-full px-2 py-0.5 font-sans font-semibold", c === "on" ? "bg-accent-soft text-accent" : "bg-surface-2 text-muted")}>
                          {c}
                        </span>
                      </td>
                    ) : (
                      <td key={j} className="truncate border-t border-line py-2 pr-3 text-muted">
                        {c}
                      </td>
                    ),
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
      <div
        className={cn(
          "absolute -right-3 -bottom-14 flex max-w-72 items-center gap-3 rounded-xl border border-line bg-surface p-3.5 shadow-xl transition-all duration-700 sm:-right-8",
          shown ? "translate-y-0 opacity-100" : "translate-y-3 opacity-0",
        )}
      >
        <span className="flex size-9 shrink-0 items-center justify-center rounded-full bg-missing-soft text-missing">
          <CircleSlash className="size-5" />
        </span>
        <span className="text-sm">
          <span className="block font-bold">prod is missing 2 keys</span>
          <span className="block text-xs text-muted">Fill them in before the release.</span>
        </span>
      </div>
    </div>
  );
}

/** One file, different values per environment. */
function StackedFiles() {
  const cards: [string, string][] = [
    [".env", "bg-[#334155] text-white"],
    ["prod", "bg-accent text-accent-ink"],
    ["uat", "bg-(--diff-a) text-white"],
    ["test", "bg-(--diff-b) text-white"],
  ];
  return (
    <div className="relative flex h-40 w-96 items-center justify-center">
      <Backdrop className="inset-x-6 inset-y-2" />
      {cards.map(([label, tone], i) => (
        <div
          key={label}
          className="absolute w-36 rounded-xl border border-line bg-surface p-2.5 shadow-lg"
          style={{ transform: `translateX(${(i - 1.5) * 52}px) rotate(${(i - 1.5) * 5}deg)`, zIndex: i }}
        >
          <span className={cn("inline-block rounded-md px-2 py-0.5 text-xs font-bold", tone)}>{label}</span>
          <span className="mt-2 block space-y-1.5">
            {["w-full", "w-4/5", "w-3/5"].map((w) => (
              <span key={w} className={cn("block h-1.5 rounded-full bg-line", w)} />
            ))}
          </span>
        </div>
      ))}
      <p className="absolute -right-4 -bottom-2 rotate-[-4deg] text-sm text-muted italic">Same file. Different values.</p>
    </div>
  );
}

/** Every environment in step. */
function InSync() {
  return (
    <div className="relative flex h-36 w-80 items-center justify-center">
      <Backdrop className="inset-x-4 inset-y-0" />
      <div className="relative w-44 rounded-xl border border-line bg-surface p-3 shadow-lg">
        {["dev", "test", "uat", "prod"].map((e, i) => (
          <p key={e} className="flex items-center gap-2 py-0.5 text-xs font-semibold">
            <span className={cn("size-2 rounded-full", ["bg-(--diff-b)", "bg-accent", "bg-(--diff-a)", "bg-accent"][i])} /> {e}
          </p>
        ))}
      </div>
      <div className="absolute right-0 bottom-3 flex items-center gap-2.5 rounded-xl border border-line bg-surface p-3 shadow-xl">
        <ShieldCheck className="size-7 text-accent" />
        <span className="text-xs">
          <span className="block font-bold text-accent">In step</span>
          <span className="text-muted">Across all environments</span>
        </span>
      </div>
    </div>
  );
}

/** A terminal that has just started envgrid. */
function LimitsArt() {
  return (
    <div className="relative flex h-28 w-52 items-center justify-center">
      <Backdrop className="inset-x-6 inset-y-0" />
      <FolderSearch className="relative size-16 text-accent" strokeWidth={1.5} />
    </div>
  );
}

/** Follows the page theme: theme.js and the theme button set the dark class. */
function useDarkTheme() {
  const [dark, setDark] = useState(() => document.documentElement.classList.contains("dark"));
  useEffect(() => {
    const watch = new MutationObserver(() => setDark(document.documentElement.classList.contains("dark")));
    watch.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });
    return () => watch.disconnect();
  }, []);
  return dark;
}

/** The product tour in the page's theme, playing silently on a loop; with reduced motion, only its first frame. */
function TourVideo() {
  const theme = useDarkTheme() ? "dark" : "light";
  const still = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  return (
    <video
      key={theme}
      className="w-full rounded-2xl border border-line shadow-xl"
      src={`/envgrid-tour-${theme}.mp4`}
      poster={`/envgrid-tour-${theme}.jpg`}
      autoPlay={!still}
      muted
      loop
      playsInline
      preload={still ? "none" : "auto"}
      aria-label="A short tour of envgrid: the grid, filling a gap, history, compare and the activity log"
    />
  );
}
