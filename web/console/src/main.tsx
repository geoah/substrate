import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { RouterProvider } from "@tanstack/react-router"
import { NuqsAdapter } from "nuqs/adapters/tanstack-router"

import "./index.css"
import { ThemeProvider } from "@/components/theme-provider"
import { Toaster } from "@/components/ui/toast"
import { setUnauthorizedHandler } from "@/lib/api/session"
import { ApiError } from "@/lib/api/types"
import { router } from "@/router"

// A 401 anywhere drops the session (session.ts does that part); this routes
// what's left of the page to the login door.
setUnauthorizedHandler(() => {
  void router.navigate({ to: "/login", replace: true })
})

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: (failureCount, error) => {
        // Definitive answers (auth, not found, validation…) don't improve on
        // retry; only transport-level trouble earns another attempt.
        if (
          error instanceof ApiError &&
          error.code !== "network" &&
          error.code !== "rate_limited"
        ) {
          return false
        }
        return failureCount < 2
      },
    },
  },
})

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ThemeProvider>
      <QueryClientProvider client={queryClient}>
        <NuqsAdapter>
          {/* first-party Toast, mounted once — the MR verdicts are the only
              writers today */}
          <Toaster>
            <RouterProvider router={router} />
          </Toaster>
        </NuqsAdapter>
      </QueryClientProvider>
    </ThemeProvider>
  </StrictMode>
)
