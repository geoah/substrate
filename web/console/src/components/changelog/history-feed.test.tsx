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
import { foldHistory, runEntry } from "@/lib/history"

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

  it("says a run the page may cut short without a count", async () => {
    const rows = [1, 2, 3].map((i): ChangeRow => ({
      ...patch(false),
      seq: 10 - i,
      op: "put",
      recordId: `t${i}`,
      payload: { created: true },
      affected: undefined,
    }))
    const [entry] = foldHistory(rows)
    renderRow(<HistoryEntryRow entry={{ ...entry, openEnded: true }} today />)
    const said = (await screen.findByText("tasks")).closest(
      "[data-slot=history-entry]"
    )
    expect(said?.textContent).not.toMatch(/\d\+|\+/)
    expect(said?.textContent).toContain("added tasks")
  })

  it("says a run the server summarized with its exact count, reading no rows", async () => {
    const entry = runEntry(
      {
        actor: "console",
        kind: TASK,
        verb: "create",
        count: 60,
        records: 60,
        newestSeq: 90,
        oldestSeq: 31,
        newestTs: new Date().toISOString(),
        oldestTs: new Date().toISOString(),
      },
      { generation: "g", filter: {}, values: true }
    )
    renderRow(<HistoryEntryRow entry={entry} today />)
    const said = (await screen.findByText("60 tasks")).closest(
      "[data-slot=history-entry]"
    )
    expect(said?.textContent).toContain("added 60 tasks")
    expect(fetch).not.toHaveBeenCalled()
  })

  it("reads a one-record run's rows to say its values", async () => {
    vi.mocked(fetch).mockImplementation(() =>
      Promise.resolve(
        new Response(
          JSON.stringify({ changes: [patch(true)], head: 9, generation: "g" }),
          { status: 200 }
        )
      )
    )
    const entry = runEntry(
      {
        actor: "console",
        kind: TASK,
        verb: "update",
        count: 1,
        records: 1,
        recordId: "t1",
        newestSeq: 7,
        oldestSeq: 7,
        newestTs: new Date().toISOString(),
        oldestTs: new Date().toISOString(),
      },
      { generation: "g", filter: {}, values: true }
    )
    renderRow(<HistoryEntryRow entry={entry} today />)
    expect(await screen.findByText("Urgent")).toBeTruthy()
    const url = String(vi.mocked(fetch).mock.calls[0][0])
    expect(url).toContain("recordId=t1")
    expect(url).toContain("values=1")
  })

  it("says a rename across many records once, by both names", async () => {
    const rows = ["t1", "t2"].map((id, i): ChangeRow => ({
      seq: 20 - i,
      ts: new Date().toISOString(),
      actor: "console",
      op: "patch",
      recordId: id,
      kind: TASK,
      payload: {
        properties: ["priority", "urgency"],
        renamed: { urgency: "priority" },
      },
    }))
    const [entry] = foldHistory(rows)
    renderRow(<HistoryEntryRow entry={entry} today />)
    expect(await screen.findByText("Urgency renamed to Priority")).toBeTruthy()
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

  it("puts the sequence number and the actor id after the sentence, each with a copy button", async () => {
    const [entry] = foldHistory([patch(true)])
    renderRow(<HistoryEntryRow entry={entry} today />, true)
    expect(
      await screen.findByRole("button", { name: "Copy the sequence number" })
    ).toBeTruthy()
    expect(
      screen.getByRole("button", { name: "Copy the actor id" })
    ).toBeTruthy()
    // The id reads once, after the sentence, not inside the actor's link.
    expect(screen.getAllByText("console")).toHaveLength(1)
  })
})
