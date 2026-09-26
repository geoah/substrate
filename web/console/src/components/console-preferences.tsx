import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import type { ReactNode } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"

import { useOptionalTheme } from "@/components/theme-provider"
import { SidebarProvider } from "@/components/ui/sidebar"
import {
  ConsolePreferencesContext,
  type ConsolePreferencesContextValue,
} from "@/hooks/use-console-preferences"
import { kindsQueryOptions } from "@/lib/api/kinds"
import {
  applyConsoleAction,
  consolePreferencesOptions,
  declaredSettings,
  preferencesOf,
  readLocalSettings,
  saveConsoleAction,
  writeLocalSidebar,
  type ConsoleAction,
  type SettingAction,
} from "@/lib/console-preferences"

interface Pending {
  action: ConsoleAction
}

export function ConsolePreferencesProvider({
  children,
}: {
  children: ReactNode
}) {
  const client = useQueryClient()
  const options = consolePreferencesOptions()
  const query = useQuery(options)
  const theme = useOptionalTheme()
  const [local, setLocal] = useState(readLocalSettings)
  const [pending, setPending] = useState<Pending[]>([])
  const [failed, setFailed] = useState<ConsoleAction | null>(null)
  // Actions save one at a time, in the order they were made: each is a
  // read-modify-CAS, so running two at once would only race into conflicts.
  const queue = useRef<Promise<void>>(Promise.resolve())
  const queryKey = options.queryKey

  const change = useCallback(
    (action: ConsoleAction) => {
      // The sidebar is this window's alone: nothing to queue or save.
      if (action.type === "sidebar") {
        writeLocalSidebar(action.open)
        setLocal(readLocalSettings())
        return
      }
      if (action.type === "set" && action.key === "theme")
        theme?.setTheme(action.value)
      const entry: Pending = { action }
      setPending((list) => [...list, entry])
      setFailed(null)
      queue.current = queue.current.then(async () => {
        try {
          // The stored declaration decides which settings the record takes;
          // without the registry, everything stays in this browser.
          const kinds = await client
            .ensureQueryData(kindsQueryOptions)
            .catch(() => [])
          const record = await saveConsoleAction(
            action,
            declaredSettings(kinds)
          )
          if (record) client.setQueryData(queryKey, record)
          if (action.type === "set") setLocal(readLocalSettings())
        } catch {
          setFailed(action)
        } finally {
          setPending((list) => list.filter((p) => p !== entry))
        }
      })
    },
    // queryKey is derived from the repository, which a session never changes
    // under a mounted shell.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [client, theme?.setTheme]
  )

  const localWithTheme = useMemo(
    () => (theme ? { ...local, theme: theme.theme } : local),
    [local, theme]
  )
  const preferences = useMemo(
    () =>
      pending.reduce(
        (prefs, p) => applyConsoleAction(prefs, p.action),
        preferencesOf(query.data, localWithTheme)
      ),
    [pending, query.data, localWithTheme]
  )

  // A theme the record holds outranks this browser's: another session chose
  // it, and this one follows.
  const shownTheme = preferences.theme
  useEffect(() => {
    if (theme && theme.theme !== shownTheme) theme.setTheme(shownTheme)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shownTheme])

  const busy = query.isPending || query.isError
  const value = useMemo<ConsolePreferencesContextValue>(
    () => ({
      preferences,
      busy,
      change,
      set: (key, v) => change({ type: "set", key, value: v } as SettingAction),
    }),
    [preferences, busy, change]
  )

  return (
    <ConsolePreferencesContext.Provider value={value}>
      <SidebarProvider
        open={preferences.sidebarOpen}
        onOpenChange={(open) => change({ type: "sidebar", open })}
      >
        {children}
        {(query.isError || failed) && (
          <div
            role="alert"
            className="fixed right-4 bottom-4 z-50 max-w-sm rounded-lg border bg-popover p-4 text-sm shadow-lg"
          >
            <p>
              Your console preferences could not be{" "}
              {query.isError ? "loaded" : "saved"}.
            </p>
            <button
              className="mt-2 underline"
              onClick={() =>
                query.isError ? void query.refetch() : failed && change(failed)
              }
            >
              Try again
            </button>
          </div>
        )}
      </SidebarProvider>
    </ConsolePreferencesContext.Provider>
  )
}

/** The name the shell has always mounted. */
export const NavigationProvider = ConsolePreferencesProvider
