import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FolderGit2, GitPullRequestArrow, History, House, Keyboard, LayoutGrid, Lightbulb, LogOut, Menu as MenuIcon, Moon, Search, Settings, Sun, Users } from "lucide-react";
import { Link, NavLink, Outlet, useLocation, useMatch, useNavigate } from "react-router";
import { api } from "@/lib/api";
import { applyTheme } from "@/lib/theme";
import type { User } from "@/lib/types";
import { cn, isTyping } from "@/lib/utils";
import { CommandPalette } from "./CommandPalette";
import { ShortcutsDialog } from "./ShortcutsDialog";
import { Button, ConfirmDialog, Kbd, Menu, MenuContent, MenuItem, MenuLabel, MenuSeparator, MenuTrigger } from "./ui";

export function Logo({ className }: { className?: string }) {
  return (
    <span className={cn("inline-flex items-center gap-2 text-lg font-bold tracking-tight", className)}>
      <svg viewBox="0 0 32 32" className="size-7" aria-hidden="true">
        <rect width="32" height="32" rx="7" className="fill-accent" />
        <g className="fill-accent-ink">
          <rect x="6" y="6" width="9" height="9" rx="1.5" />
          <rect x="17" y="6" width="9" height="9" rx="1.5" />
          <rect x="6" y="17" width="9" height="9" rx="1.5" />
        </g>
        <rect x="17.8" y="17.8" width="7.4" height="7.4" rx="1" fill="none" className="stroke-accent-ink" strokeWidth="1.6" />
      </svg>
      <span className="font-mono">envgrid</span>
    </span>
  );
}

// `key` is the second key of the "g then" shortcut; the list in ShortcutsDialog matches.
function navItems(user: User) {
  return [
    { to: "/", label: "Home", icon: House, end: true, key: "h" },
    { to: "/repos", label: "Repos", icon: LayoutGrid, end: false, key: "r" },
    { to: "/requests", label: "Requests", icon: GitPullRequestArrow, end: false, key: "q" },
    { to: "/insights", label: "Insights", icon: Lightbulb, end: false, key: "i" },
    { to: "/activity", label: "Activity", icon: History, end: false, key: "a" },
    ...(user.role === "admin" ? [{ to: "/users", label: "Users", icon: Users, end: false, key: "u" }] : []),
    { to: "/settings", label: "Settings", icon: Settings, end: false, key: "s" },
  ];
}

// The active page is marked by weight, background and an underline, not colour alone.
// Below 1280 pixels the links show only their icons, so the bar never wraps.
const navLink = ({ isActive }: { isActive: boolean }) =>
  cn(
    "inline-flex h-9 items-center gap-1.5 rounded-md px-2.5 text-sm font-semibold hover:bg-surface-2 xl:px-3 [&_svg]:size-4",
    isActive ? "bg-surface-2 text-ink shadow-[inset_0_-2px_0_var(--accent)]" : "text-muted",
  );

// Anything open on top of the page, where the page's own shortcuts must not fire.
const overlay = "[role=dialog], [role=alertdialog], [role=menu], [role=listbox]";

export function AppShell({ user }: { user: User }) {
  const qc = useQueryClient();
  const location = useLocation();
  const navigate = useNavigate();
  const [shortcuts, setShortcuts] = useState(false);
  const [palette, setPalette] = useState(false);
  const [confirmSignOut, setConfirmSignOut] = useState(false);
  // Pages can open the palette too, such as the search button on Home.
  useEffect(() => {
    const open = () => setPalette(true);
    window.addEventListener("envgrid:search", open);
    return () => window.removeEventListener("envgrid:search", open);
  }, []);
  const firstRender = useRef(true);
  const gPressedAt = useRef(0);
  const logout = useMutation({
    mutationFn: api.logout,
    onSettled: () => {
      // Show the sign-in screen, then drop everything cached for this user.
      // clear() would also drop the "me" query App is watching, and App
      // would keep showing the old user.
      qc.setQueryData(["me"], null);
      qc.removeQueries({ predicate: (q) => q.queryKey[0] !== "me" });
    },
  });

  // Listens in the capture phase, so the second key of "g then r" never
  // reaches the file grid, where r alone reveals a secret.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // Ctrl+K or Cmd+K works even in a text field, but not over another dialog.
      if ((e.key === "k" || e.key === "K") && (e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey) {
        if (document.querySelector("[role=dialog]:not([data-palette]), [role=alertdialog], [role=menu]")) return;
        e.preventDefault();
        setPalette((open) => !open);
        return;
      }
      if (isTyping(e) || e.ctrlKey || e.metaKey || e.altKey || document.querySelector(overlay)) return;
      if (e.key === "?") {
        e.preventDefault();
        setShortcuts(true);
        return;
      }
      const after = gPressedAt.current && e.timeStamp - gPressedAt.current < 1000;
      gPressedAt.current = e.key === "g" ? e.timeStamp : 0;
      const target = after && navItems(user).find((i) => i.key === e.key);
      if (target) {
        e.preventDefault();
        e.stopPropagation();
        navigate(target.to);
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [user, navigate]);

  // After a navigation, move focus to the new page's heading so screen
  // readers announce it and keyboard users start from the top.
  useEffect(() => {
    if (firstRender.current) {
      firstRender.current = false;
      return;
    }
    const t = setTimeout(() => {
      if (document.querySelector("[role=dialog]")) return;
      // A link to a key (from search or Insights) focuses that key's cell in the grid instead.
      if (new URLSearchParams(window.location.search).has("key")) return;
      const target = document.querySelector<HTMLElement>("main h1") ?? document.getElementById("main");
      if (!target) return;
      if (target.tagName === "H1") target.tabIndex = -1;
      target.focus({ preventScroll: true });
    }, 50);
    return () => clearTimeout(t);
  }, [location.pathname]);

  // The waiting-requests count; refreshed by every write and once a minute.
  const pending = useQuery({ queryKey: ["requests-count"], queryFn: api.pendingCount, refetchInterval: 60_000 }).data?.pending ?? 0;
  const count = (to: string) =>
    to === "/requests" && pending > 0 ? (
      <span className="rounded-full bg-protected px-1.5 text-xs leading-5 font-bold text-surface">
        {pending}
        <span className="sr-only"> waiting</span>
      </span>
    ) : null;

  // Inside a repo, the bar shows where you are, like a code host, instead of
  // the global pages. The names come from queries the repo pages already made.
  const repoId = Number(useMatch("/repos/:repoId/*")?.params.repoId);
  const inRepo = repoId > 0;
  const fileId = Number(useMatch("/repos/:repoId/files/:fileId/*")?.params.fileId);
  const repos = useQuery({ queryKey: ["repos"], queryFn: api.repos, enabled: inRepo });
  const files = useQuery({ queryKey: ["files", repoId], queryFn: () => api.files(repoId), enabled: inRepo });
  const repoName = inRepo ? repos.data?.find((r) => r.id === repoId)?.name : undefined;
  const fileName = fileId > 0 ? files.data?.find((f) => f.id === fileId)?.name : undefined;

  const items = navItems(user);
  // Opened after the menu has closed and handed focus back to its button,
  // so closing the dialog returns focus there.
  const openFromMenu = (open: (v: boolean) => void) => () => setTimeout(() => open(true));

  return (
    <div className="flex min-h-dvh flex-col md:h-dvh md:overflow-hidden">
      <a href="#main" className="sr-only z-50 rounded-md bg-accent px-3 py-2 text-accent-ink focus:not-sr-only focus:fixed focus:top-2 focus:left-2">
        Skip to content
      </a>
      <header className="sticky top-0 z-30 border-b border-line bg-surface">
        <div className={cn("flex h-14 items-center gap-2 px-4", !inRepo && "mx-auto w-full max-w-7xl md:px-6")}>
          <Menu>
            <MenuTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                className={cn("relative size-9 shrink-0", !inRepo && "md:hidden")}
                aria-label={pending > 0 ? `Open navigation menu, ${pending} change ${pending === 1 ? "request" : "requests"} waiting` : "Open navigation menu"}
              >
                <MenuIcon />
                {pending > 0 && <span className="absolute top-1.5 right-1.5 size-2 rounded-full bg-protected" aria-hidden="true" />}
              </Button>
            </MenuTrigger>
            <MenuContent align="start" className="min-w-60">
              {items.map(({ to, label, icon: Icon, end }) => (
                <MenuItem key={to} asChild>
                  <NavLink to={to} end={end} className="aria-[current=page]:bg-surface-2 aria-[current=page]:font-bold">
                    <Icon /> {label} {count(to)}
                  </NavLink>
                </MenuItem>
              ))}
              <MenuSeparator />
              {repoName && (
                <MenuItem asChild>
                  <Link to={`/repos/${repoId}`}>
                    <FolderGit2 />
                    <span className="min-w-0 truncate">
                      <span className="text-muted">Repo </span>
                      {repoName}
                    </span>
                  </Link>
                </MenuItem>
              )}
              <MenuItem onSelect={openFromMenu(setPalette)}>
                <Search /> Search repos, files and keys
              </MenuItem>
            </MenuContent>
          </Menu>
          <Link to="/" className="shrink-0 rounded-md" aria-label="envgrid home">
            <Logo />
          </Link>
          {inRepo && (
            <nav aria-label="Breadcrumb" className="min-w-0">
              <ol className="flex min-w-0 items-center gap-0.5 text-sm">
                <li className="hidden shrink-0 items-center gap-0.5 sm:flex">
                  <span className="px-1 text-muted" aria-hidden="true">
                    /
                  </span>
                  <Link to="/repos" className="rounded px-1.5 py-1 text-muted hover:bg-surface-2 hover:text-ink">
                    Repos
                  </Link>
                </li>
                <li className="flex min-w-0 items-center gap-0.5">
                  <span className="px-1 text-muted" aria-hidden="true">
                    /
                  </span>
                  <Link
                    to={`/repos/${repoId}`}
                    aria-current={fileName ? undefined : "page"}
                    className="truncate rounded px-1.5 py-1 font-mono font-bold hover:bg-surface-2"
                  >
                    {repoName ?? "Repo"}
                  </Link>
                </li>
                {fileName && (
                  <li className="flex min-w-0 items-center gap-0.5">
                    <span className="px-1 text-muted" aria-hidden="true">
                      /
                    </span>
                    <Link to={`/repos/${repoId}/files/${fileId}`} aria-current="page" className="truncate rounded px-1.5 py-1 font-mono hover:bg-surface-2">
                      {fileName}
                    </Link>
                  </li>
                )}
              </ol>
            </nav>
          )}
          <nav aria-label="Main" className={cn("ml-2 hidden shrink-0 items-center gap-1", !inRepo && "md:flex")}>
            {items.map(({ to, label, icon: Icon, end }) => (
              <NavLink key={to} to={to} end={end} className={navLink} title={label}>
                <Icon aria-hidden="true" />
                <span className="sr-only xl:not-sr-only">{label}</span>
                {count(to)}
              </NavLink>
            ))}
          </nav>
          <div className="ml-auto flex min-w-0 items-center gap-1">
            <Button
              onClick={() => setPalette(true)}
              aria-label="Search repos, files and keys"
              aria-keyshortcuts="Control+K Meta+K"
              title="Search repos, files and keys (Ctrl K)"
              className="size-9 min-w-0 px-0 text-muted min-[1400px]:w-72 min-[1400px]:justify-start min-[1400px]:px-3"
            >
              <Search />
              <span className="hidden min-w-0 flex-1 truncate text-left font-normal min-[1400px]:block">Search repos, files and keys</span>
              <span className="hidden shrink-0 gap-0.5 min-[1400px]:flex">
                <Kbd>Ctrl</Kbd>
                <Kbd>K</Kbd>
              </span>
            </Button>
            <ThemeButton />
            <Menu>
              <MenuTrigger asChild>
                <Button variant="ghost" className="h-9 shrink-0 px-1.5" aria-label={`Account menu for ${user.email}`}>
                  <span className="flex size-7 items-center justify-center rounded-full bg-accent text-xs font-bold text-accent-ink" aria-hidden="true">
                    {(user.name || user.email).slice(0, 1).toUpperCase()}
                  </span>
                  <span className="hidden max-w-32 truncate 2xl:inline">{user.name || user.email}</span>
                </Button>
              </MenuTrigger>
              <MenuContent align="end">
                <MenuLabel>
                  Signed in as {user.email}
                  <br />
                  Role: {user.role}
                </MenuLabel>
                <MenuSeparator />
                <MenuItem asChild>
                  <Link to="/settings">
                    <Settings /> Settings
                  </Link>
                </MenuItem>
                <MenuItem onSelect={openFromMenu(setShortcuts)} aria-keyshortcuts="?">
                  <Keyboard /> Keyboard shortcuts
                  <span className="ml-auto" aria-hidden="true">
                    <Kbd>?</Kbd>
                  </span>
                </MenuItem>
                <MenuItem onSelect={() => setConfirmSignOut(true)}>
                  <LogOut /> Sign out
                </MenuItem>
              </MenuContent>
            </Menu>
          </div>
        </div>
      </header>
      <main id="main" tabIndex={-1} className="relative flex min-h-0 flex-1 flex-col outline-none md:overflow-y-auto">
        <Outlet />
      </main>
      <ShortcutsDialog open={shortcuts} onOpenChange={setShortcuts} />
      <CommandPalette open={palette} onOpenChange={setPalette} pages={items} />
      <ConfirmDialog
        open={confirmSignOut}
        onOpenChange={setConfirmSignOut}
        title="Sign out of envgrid?"
        description="You will need your email and password to sign in again. Anything you are typing in this tab and have not saved is lost."
        confirmLabel="Sign out"
        tone="primary"
        onConfirm={() => logout.mutate()}
      />
    </div>
  );
}

/** Flips between light and dark; the choice is kept in this browser. */
export function ThemeButton({ className }: { className?: string }) {
  return (
    <Button
      variant="ghost"
      size="icon"
      className={cn("size-9 shrink-0", className)}
      onClick={() => applyTheme(document.documentElement.classList.contains("dark") ? "light" : "dark")}
      aria-label="Switch between light and dark theme"
      title="Switch theme"
    >
      <Sun className="hidden dark:block" />
      <Moon className="dark:hidden" />
    </Button>
  );
}
