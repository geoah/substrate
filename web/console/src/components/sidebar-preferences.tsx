import type { ReactNode } from "react"
import { SidebarPreferencesContext } from "@/hooks/use-sidebar-preferences"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { SidebarProvider } from "@/components/ui/sidebar"
import {
  preferencesOf,
  saveSidebarAction,
  sidebarPreferencesOptions,
} from "@/lib/sidebar-preferences"

export function NavigationProvider({ children }: { children: ReactNode }) {
  const client = useQueryClient()
  const options = sidebarPreferencesOptions()
  const query = useQuery(options)
  const save = useMutation({
    mutationFn: saveSidebarAction,
    scope: { id: "sidebar-preferences" },
    onSuccess: (record) => client.setQueryData(options.queryKey, record),
  })
  const preferences = preferencesOf(query.data)
  const busy = query.isPending || query.isError || save.isPending
  return (
    <SidebarPreferencesContext.Provider
      value={{
        preferences,
        busy,
        change: (action) => {
          if (!busy) save.mutate(action)
        },
      }}
    >
      <SidebarProvider
        open={preferences.sidebarOpen}
        onOpenChange={(open) => {
          if (!busy) save.mutate({ type: "sidebar", open })
        }}
      >
        {children}
        {(query.isError || save.isError) && (
          <div
            role="alert"
            className="fixed right-4 bottom-4 z-50 max-w-sm rounded-lg border bg-popover p-4 text-sm shadow-lg"
          >
            <p>
              Navigation preferences could not be{" "}
              {query.isError ? "loaded" : "saved"}.
            </p>
            <button
              className="mt-2 underline"
              onClick={() =>
                query.isError
                  ? void query.refetch()
                  : save.variables && save.mutate(save.variables)
              }
            >
              Retry
            </button>
          </div>
        )}
      </SidebarProvider>
    </SidebarPreferencesContext.Provider>
  )
}
