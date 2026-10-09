import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { MutationCache, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter } from "react-router";
import { App, SiteApp } from "./App";
import { SITE } from "./site";
import { ApiError, setUnauthorizedHandler } from "./lib/api";
import { AnnounceProvider } from "./lib/announce";
import "./index.css";

const queryClient: QueryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 10_000,
      retry: (count, err) => !(err instanceof ApiError && err.status < 500) && count < 2,
    },
  },
  // Any successful write refreshes what is on screen; queries are cheap.
  mutationCache: new MutationCache({
    onSuccess: (): Promise<void> => queryClient.invalidateQueries({ predicate: (q) => q.queryKey[0] !== "me" }),
  }),
});

// A session that expires mid-use drops back to the sign-in screen. Only act
// when someone was signed in: clearing while the initial "me" request is in
// flight would restart it and loop.
setUnauthorizedHandler(() => {
  if (!queryClient.getQueryData(["me"])) return;
  // Null first, on the same query object App watches, so it re-renders.
  queryClient.setQueryData(["me"], null);
  queryClient.removeQueries({ predicate: (q) => q.queryKey[0] !== "me" });
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <AnnounceProvider>
          {SITE ? <SiteApp /> : <App />}
        </AnnounceProvider>
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
);
