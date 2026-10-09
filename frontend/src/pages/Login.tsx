import { useEffect, useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { CircleAlert, CircleCheck, CircleSlash, Clock, Eye, EyeOff, GitBranch, Lock, ScrollText, Server } from "lucide-react";
import { Link } from "react-router";
import { Logo } from "@/components/AppShell";
import { useDocumentTitle } from "@/components/PageHeader";
import { Button, Field, Input } from "@/components/ui";
import { ApiError, api } from "@/lib/api";
import { MASK } from "@/lib/format";
import { cn } from "@/lib/utils";

/** Each sign-in failure says what happened and what to do next. */
function LoginError({ error }: { error: Error }) {
  const code = error instanceof ApiError ? error.code : "";
  const [Icon, tone, text] =
    code === "access_expired"
      ? [Lock, "border-protected bg-protected-soft text-protected", "Your access to envgrid has ended. Ask an admin to extend it, then sign in again."]
      : code === "rate_limited"
        ? [Clock, "border-line bg-surface-2 text-ink", "Too many sign-in attempts from this network. Wait a minute, then try again."]
        : code === "invalid_credentials"
          ? [
              CircleAlert,
              "border-missing bg-missing-soft text-missing",
              "The email or password is wrong. Check both and try again. If you have forgotten your password, an admin can reset it.",
            ]
          : [CircleAlert, "border-missing bg-missing-soft text-missing", error.message];
  return (
    <p role="alert" className={cn("flex items-start gap-2 rounded-md border px-2.5 py-2 text-sm", tone)}>
      <Icon className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
      <span>{text}</span>
    </p>
  );
}

const facts = [
  { icon: Lock, title: "Encrypted at rest", text: "Every value is sealed with AES-256-GCM before it reaches the database." },
  { icon: ScrollText, title: "Masked until asked", text: "Secrets stay hidden, and reveals, copies and exports are recorded by default." },
  { icon: Server, title: "On your own server", text: "One binary and one SQLite file. Nothing calls out to the internet." },
];

export function LoginPage() {
  useDocumentTitle("Sign in");
  const qc = useQueryClient();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const login = useMutation({
    mutationFn: () => api.login(email.trim(), password),
    onSuccess: (user) => qc.setQueryData(["me"], user),
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    login.mutate();
  };

  return (
    <div className="grid min-h-dvh lg:h-dvh lg:grid-cols-[1.3fr_1fr]">
      <section className="hidden flex-col gap-6 overflow-y-auto border-r border-line bg-surface p-8 lg:flex xl:px-14" aria-label="About envgrid">
        <Link to="/" className="mt-auto self-start rounded-md" aria-label="About envgrid">
          <Logo className="text-xl" />
        </Link>
        <div className="mb-auto max-w-3xl">
          <h2 className="text-4xl leading-[1.1] font-bold tracking-tight xl:text-5xl">Every key, in every environment, side by side.</h2>
          <p className="mt-4 max-w-xl text-lg text-muted">See what prod is missing before it ships, and fill the gap in one place.</p>
          <GridSketch className="mt-8" />
        </div>
        <ul className="hidden max-w-2xl gap-6 text-sm xl:grid-cols-3 [@media(min-height:62rem)]:grid">
          {facts.map(({ icon: Icon, title, text }) => (
            <li key={title} className="flex gap-3 xl:block">
              <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-accent-soft text-accent" aria-hidden="true">
                <Icon className="size-4" />
              </span>
              <span className="block xl:mt-3">
                <span className="block font-bold">{title}</span>
                <span className="mt-0.5 block text-muted">{text}</span>
              </span>
            </li>
          ))}
        </ul>
      </section>

      <main className="grid-paper flex items-center justify-center overflow-y-auto px-4 py-10">
        <div className="w-full max-w-sm">
          <div className="mb-8 lg:hidden">
            <Logo className="text-xl" />
            <p className="mt-3 text-muted">Every key, in every environment, side by side.</p>
          </div>
          <form onSubmit={submit} className="space-y-5 rounded-2xl border border-line bg-surface p-7 shadow-lg" aria-labelledby="login-title">
            <div>
              <h1 id="login-title" className="text-2xl font-bold">
                Sign in
              </h1>
              <p className="mt-1 text-sm text-muted">Use the account an envgrid admin created for you.</p>
            </div>
            <Field label="Email" htmlFor="email">
              <Input
                id="email"
                type="email"
                autoComplete="username"
                required
                autoFocus
                placeholder="you@company.com"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
              />
            </Field>
            <Field label="Password" htmlFor="password">
              <div className="relative">
                <Input
                  id="password"
                  type={showPassword ? "text" : "password"}
                  autoComplete="current-password"
                  required
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  className="pr-10"
                />
                <button
                  type="button"
                  onClick={() => setShowPassword((v) => !v)}
                  aria-pressed={showPassword}
                  aria-label="Show password"
                  title={showPassword ? "Hide password" : "Show password"}
                  className="absolute top-1/2 right-1 inline-flex size-8 -translate-y-1/2 cursor-pointer items-center justify-center rounded-md text-muted hover:bg-surface-2 hover:text-ink"
                >
                  {showPassword ? <EyeOff className="size-4" aria-hidden="true" /> : <Eye className="size-4" aria-hidden="true" />}
                </button>
              </div>
            </Field>
            {login.isError && <LoginError error={login.error} />}
            <Button type="submit" variant="primary" className="h-10 w-full" disabled={login.isPending}>
              {login.isPending ? "Signing in…" : "Sign in"}
            </Button>
            <p className="border-t border-line pt-4 text-xs text-muted">
              Forgotten your password? There is no reset by email: an admin sets a new one for you from the Users page.
            </p>
          </form>
        </div>
      </main>
    </div>
  );
}

/**
 * A small picture of the grid that tells the product's story once: prod is
 * missing a value, and a moment later it is filled in. Decorative; with
 * reduced motion it stays still.
 */
export function GridSketch({ className = "mt-10" }: { className?: string }) {
  const [filled, setFilled] = useState(false);
  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    const t = setTimeout(() => setFilled(true), 1600);
    return () => clearTimeout(t);
  }, []);
  const envs = ["test", "uat", "prod", "feature/login"];
  const rows: [string, (string | null)[]][] = [
    ["DATABASE_URL", [MASK, MASK, MASK, MASK]],
    ["REDIS_URL", ["redis://test", "redis://uat", filled ? "redis://prod" : null, "redis://feature"]],
    ["LOG_LEVEL", ["debug", "debug", "info", "debug"]],
    ["SENTRY_DSN", [null, "https://uat", "https://prod", "https://feature"]],
  ];
  // The branch column is a little wider, so its name fits.
  const cols = "grid grid-cols-[8rem_repeat(3,minmax(0,1fr))_minmax(0,1.2fr)]";
  return (
    <div className={className} aria-hidden="true">
      <div className="overflow-hidden rounded-xl border border-line text-sm shadow-sm">
        <div className={`${cols} bg-surface-2 font-mono font-bold`}>
          <div className="border-r border-b-2 border-line px-2.5 py-2 font-sans">Key</div>
          {envs.map((e) => (
            <div
              key={e}
              className={`flex min-w-0 items-center gap-1 border-r border-b-2 border-line px-2.5 py-2 last:border-r-0 ${e === "prod" ? "bg-protected-soft text-protected" : ""}`}
            >
              {e === "prod" && <Lock className="size-3.5 shrink-0" />}
              {e.includes("/") && <GitBranch className="size-3.5 shrink-0" />}
              <span className="truncate">{e}</span>
            </div>
          ))}
        </div>
        {rows.map(([key, cells], i) => (
          <div key={key} className={`${cols} font-mono ${i % 2 ? "bg-surface-alt" : "bg-surface"}`}>
            <div className="truncate border-r border-b border-line px-2.5 py-2 font-semibold">{key}</div>
            {cells.map((c, j) =>
              c === null ? (
                <div key={j} className="hatch-missing flex items-center gap-1 border-r border-b border-line px-2.5 py-2 font-sans font-semibold text-missing last:border-r-0">
                  <CircleSlash className="size-3.5" /> missing
                </div>
              ) : (
                <div
                  key={j}
                  className={cn(
                    "truncate border-r border-b border-line px-2.5 py-2 text-muted transition-colors duration-700 last:border-r-0",
                    key === "REDIS_URL" && j === 2 && "bg-accent-soft font-semibold text-ink",
                  )}
                >
                  {c}
                </div>
              ),
            )}
          </div>
        ))}
      </div>
      <p className="mt-3 flex items-center gap-1.5 text-sm text-muted">
        {filled ? (
          <>
            <CircleCheck className="size-4 text-accent" /> REDIS_URL is set in prod. One gap left, in test.
          </>
        ) : (
          <>
            <CircleSlash className="size-4 text-missing" /> prod is missing REDIS_URL, and test is missing SENTRY_DSN.
          </>
        )}
      </p>
    </div>
  );
}
