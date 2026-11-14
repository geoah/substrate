// @vitest-environment jsdom
/** History's live tail told its position is gone: it reads History again
 * from the newest page and opens the tail again at the head, and that tail
 * is held to the watch's bound. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, renderHook, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import {
  ConsolePreferencesContext,
  type ConsolePreferencesContextValue,
} from "@/hooks/use-console-preferences"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"

const watch = vi.hoisted(() => ({
  calls: [] as {
    from?: number
    generation?: string
    reader: { key: string }
    onCompacted?: () => void
  }[],
}))

vi.mock("@/lib/api/changes", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/changes")>()),
  watchChanges: (opts: (typeof watch.calls)[number]) => {
    watch.calls.push(opts)
    return { stop: () => {} }
  },
}))

import { useHistoryFeed } from "./use-history-feed"

const preferences: ConsolePreferencesContextValue = {
  preferences: {
    collapsed: [],
    favorites: [],
    sidebarOpen: true,
    ...DEFAULT_SETTINGS,
  },
  busy: false,
  change: () => {},
  set: () => {},
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  watch.calls = []
})

describe("useHistoryFeed's tail", () => {
  it("opens again at the head after a compacted, held to the watch's bound", async () => {
    let reads = 0
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        reads++
        return new Response(
          JSON.stringify({ runs: [], head: 7, generation: "g" }),
          { status: 200 }
        )
      })
    )
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const wrapper = ({ children }: { children: ReactNode }) => (
      <ConsolePreferencesContext.Provider value={preferences}>
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      </ConsolePreferencesContext.Provider>
    )
    renderHook(() => useHistoryFeed({}), { wrapper })
    await waitFor(() => expect(watch.calls).toHaveLength(1))
    expect(watch.calls[0]).toMatchObject({ from: 7, generation: "g" })
    act(() => watch.calls[0].onCompacted?.())
    await waitFor(() => expect(watch.calls).toHaveLength(2))
    expect(watch.calls[1]).toMatchObject({ from: undefined })
    expect(watch.calls[1].reader.key).toBe(watch.calls[0].reader.key)
    await waitFor(() => expect(reads).toBe(2))
  })
})
