import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, FileQuestionMark } from "lucide-react";
import { Link, Navigate, Route, Routes } from "react-router";
import { AppShell } from "./components/AppShell";
import { useDocumentTitle } from "./components/PageHeader";
import { Button, Kbd, Loading } from "./components/ui";
import { ApiError, api } from "./lib/api";
import type { User } from "./lib/types";
import { ActivityPage } from "./pages/Activity";
import { UsersPage } from "./pages/Admin";
import { ComparePage } from "./pages/Compare";
import { FileGridPage } from "./pages/FileGrid";
import { HomePage } from "./pages/Home";
import { DocsPage } from "./pages/Docs";
import { LandingPage } from "./pages/Landing";
import { InsightsPage } from "./pages/Insights";
import { LoginPage } from "./pages/Login";
import { RepoActivityPage, RepoEnvironmentsPage, RepoIndex, RepoLayout, RepoSettingsPage } from "./pages/Repo";
import { ReposPage } from "./pages/Repos";
import { RequestsPage } from "./pages/Requests";
import { SettingsPage } from "./pages/Settings";

/** The signed-in user; null when signed out. */
export function useMe() {
  return useQuery<User | null>({
    queryKey: ["me"],
    queryFn: async () => {
      try {
        return await api.me();
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) return null;
        throw e;
      }
    },
    staleTime: Infinity,
  });
}

/** The public website: the landing page and the docs, with no server to sign in to. */
export function SiteApp() {
  return (
    <Routes>
      <Route index element={<LandingPage />} />
      <Route path="docs" element={<DocsPage />} />
      <Route path="*" element={<NotFound site />} />
    </Routes>
  );
}

export function App() {
  const me = useMe();
  if (me.isPending) return <Loading label="Starting envgrid" />;
  // Signed out: the landing page at the root, and the sign-in page for every
  // other address, so a link into the app still asks to sign in first.
  if (!me.data)
    return (
      <Routes>
        <Route index element={<LandingPage />} />
        <Route path="docs" element={<DocsPage />} />
        <Route path="*" element={<LoginPage />} />
      </Routes>
    );
  return (
    <Routes>
      <Route element={<AppShell user={me.data} />}>
        <Route index element={<HomePage />} />
        <Route path="repos" element={<ReposPage />} />
        <Route path="repos/:repoId" element={<RepoLayout />}>
          <Route index element={<RepoIndex />} />
          <Route path="files/:fileId" element={<FileGridPage />} />
          <Route path="files/:fileId/compare" element={<ComparePage />} />
          <Route path="environments" element={<RepoEnvironmentsPage />} />
          <Route path="activity" element={<RepoActivityPage />} />
          <Route path="settings" element={<RepoSettingsPage />} />
        </Route>
        <Route path="requests" element={<RequestsPage />} />
        <Route path="insights" element={<InsightsPage />} />
        <Route path="activity" element={<ActivityPage />} />
        <Route path="users" element={<UsersPage />} />
        <Route path="admin" element={<Navigate to="/users" replace />} />
        <Route path="signin" element={<Navigate to="/" replace />} />
        <Route path="settings" element={<SettingsPage />} />
        <Route path="*" element={<NotFound />} />
      </Route>
    </Routes>
  );
}

/** On the public website (site) it points home; in the app, to the repos and search. */
function NotFound({ site = false }: { site?: boolean }) {
  useDocumentTitle("Page not found");
  return (
    <div className="mx-auto w-full max-w-md px-4 py-16 text-center">
      <span className="mx-auto flex size-16 items-center justify-center rounded-full bg-accent-soft text-accent" aria-hidden="true">
        <FileQuestionMark className="size-8" />
      </span>
      <h1 className="mt-5 text-2xl font-bold tracking-tight">Page not found</h1>
      <p className="mt-2 text-muted">
        {site ? "The address may be mistyped." : "The address may be mistyped, or the repo or file may have been deleted."}
      </p>
      <Button asChild variant="primary" className="mt-6">
        <Link to={site ? "/" : "/repos"}>
          <ArrowLeft /> {site ? "Go to the home page" : "Go to all repos"}
        </Link>
      </Button>
      {!site && (
        <p className="mt-8 border-t border-line pt-4 text-sm text-muted">
          To find any repo, file or key by name, press <Kbd>Ctrl</Kbd> <Kbd>K</Kbd> or use the search button at the top of the page.
        </p>
      )}
    </div>
  );
}
