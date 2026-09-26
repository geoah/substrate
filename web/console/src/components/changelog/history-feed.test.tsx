// @vitest-environment jsdom
/** A History sentence about one record says its values where the server sent
 * them, and the names of what moved where it did not (an older server). */

import type { ReactNode } from "react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router"
import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { HistoryEntryRow } from "./history-feed"
import {
  ConsolePreferencesContext,
  type ConsolePreferencesContextValue,
} from "@/hooks/use-console-preferences"
import { kindsQueryOptions } from "@/lib/api/kinds"
import type { ChangeRow, KindInfo } from "@/lib/api/types"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"
import { foldHistory } from "@/lib/history"

const TASK = "samples.substrate.reamde.dev/tasks/task"

beforeEach(() => {
  // Nothing here reads the network: the registry answers from the cache and
  // a record title from the row.
  vi.stubGlobal(
    "fetch",
    vi.fn(() => Promise.reject(new Error("offline")))
  )
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function preferences(
  technicalDetails: boolean
): ConsolePreferencesContextValue {
  return {
    preferences: {
      collapsed: [],
      favorites: [],
      sidebarOpen: true,
      ...DEFAULT_SETTINGS,
      technicalDetails,
    },
    busy: false,
    change: () => {},
    set: () => {},
  }
}

const kind = {
  identity: TASK,
  definition: {
    properties: {
      priority: {
        type: "enum",
        values: [
          { value: "high", label: "High" },
          { value: "urgent", label: "Urgent" },
        ],
      },
    },
  },
} as unknown as KindInfo

function renderRow(ui: ReactNode, technical = false) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(kindsQueryOptions.queryKey, [kind])
  const rootRoute = createRootRoute({
    component: () => (
      <QueryClientProvider client={client}>
        <ConsolePreferencesContext.Provider value={preferences(technical)}>
          {ui}
        </ConsolePreferencesContext.Provider>
      </QueryClientProvider>
    ),
  })
  const routeTree = rootRoute.addChildren(
    [
      "/data/$authority/$pkg/$name/$id",
      "/data/$authority/$pkg/$name",
      "/actors/$actorId",
    ].map((path) =>
      createRoute({
        getParentRoute: () => rootRoute,
        path,
        component: () => null,
      })
    )
  )
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: ["/"] }),
  })
  return render(
    <RouterProvider
      router={
        router as unknown as Parameters<typeof RouterProvider>[0]["router"]
      }
    />
  )
}

function patch(withValues: boolean): ChangeRow {
  return {
    seq: 7,
    ts: new Date().toISOString(),
    actor: "console",
    op: "patch",
    recordId: "t1",
    kind: TASK,
    payload: { properties: ["priority"] },
    affected: [
      {
        kind: TASK,
        id: "t1",
        version: 2,
        ...(withValues && {
          properties: [{ name: "priority", before: "high", after: "urgent" }],
        }),
      },
    ],
  }
}

describe("HistoryEntryRow", () => {
  it("says a one-record change in values", async () => {
    const [entry] = foldHistory([patch(true)])
    renderRow(<HistoryEntryRow entry={entry} today />)
    const move = (await screen.findByText("Priority:")).closest(
      "[data-slot=value-move]"
    )
    expect(move?.textContent).toBe("Priority:High→toUrgent")
  })

  it("falls back to the names against a server that sends none", async () => {
    const [entry] = foldHistory([patch(false)])
    renderRow(<HistoryEntryRow entry={entry} today />)
    expect(await screen.findByText("Priority")).toBeTruthy()
    expect(document.querySelector("[data-slot=value-move]")).toBeNull()
    expect(screen.queryByText("Urgent")).toBeNull()
  })

  it("says a provider's update as what it is, and keeps the digest for technical details", async () => {
    const row: ChangeRow = {
      seq: 1053,
      ts: new Date().toISOString(),
      actor: "bundle:providers.substrate.reamde.dev:google",
      op: "put",
      recordId: "providers.substrate.reamde.dev/google",
      kind: "substrate.reamde.dev/core/package",
      payload: { properties: ["version", "originDigest"] },
      affected: [
        {
          kind: "substrate.reamde.dev/core/package",
          id: "providers.substrate.reamde.dev/google",
          version: 3,
          properties: [
            { name: "version", before: 34, after: 35 },
            { name: "originDigest", before: "82007523", after: "075e3623" },
          ],
        },
      ],
    }
    const [entry] = foldHistory([row])
    renderRow(<HistoryEntryRow entry={entry} today />)
    expect(
      await screen.findByText("updated its package to version 35")
    ).toBeTruthy()
    expect(document.body.textContent).not.toContain("82007523")
    cleanup()
    renderRow(<HistoryEntryRow entry={entry} today />, true)
    expect(await screen.findByText("originDigest:")).toBeTruthy()
  })
})
