import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { RouterProvider } from "@tanstack/react-router"

import "./index.css"
import { ThemeProvider } from "@/components/theme-provider"
import { Toaster } from "@/components/ui/toast"
import {
  setSessionChangedHandler,
  setUnauthorizedHandler,
} from "@/lib/api/session"
import { retryDelay, retryRead } from "@/lib/api/retry"
import { reloadOnStaleChunk } from "@/lib/stale-chunks"
import { router } from "@/router"

// A tab left open across a deploy asks for chunks the server no longer has;
// one reload picks up the new build.
reloadOnStaleChunk(window)

// A 401 anywhere drops the session (session.ts does that part); this routes
// what's left of the page to the login door.
setUnauthorizedHandler(() => {
  void router.navigate({ to: "/login", replace: true })
})

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      // Definitive answers (auth, not found, validation) don't improve on
      // retry; transport trouble and a busy server earn two more attempts.
      retry: retryRead,
      retryDelay,
    },
  },
})

// A cached answer belongs to the repository whose token fetched it, and no
// query key says which; signing in as anybody else drops the lot rather than
// answering the new session out of the old one's cache.
setSessionChangedHandler(() => {
  queryClient.clear()
})

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ThemeProvider>
      <QueryClientProvider client={queryClient}>
        {/* first-party Toast, mounted once — the MR verdicts are the only
            writers today. The nuqs adapter is the router's InnerWrap
            (router.tsx): it needs the router's context. */}
        <Toaster>
          <RouterProvider router={router} />
        </Toaster>
      </QueryClientProvider>
    </ThemeProvider>
  </StrictMode>
)
