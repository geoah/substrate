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
import { seedActors } from "@/lib/actors.test-fixtures"

import { HistoryEntryRow, HistorySentences } from "./history-feed"
import {
  ConsolePreferencesContext,
  type ConsolePreferencesContextValue,
} from "@/hooks/use-console-preferences"
import { useHistoryFeed, type RunRecovery } from "@/hooks/use-history-feed"
import { forgetRecoveries } from "@/lib/api/compacted"
import { kindsQueryOptions } from "@/lib/api/kinds"
import type { ChangeRow, KindInfo } from "@/lib/api/types"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"
import { foldHistory, runEntry } from "@/lib/history"

beforeEach(() => seedActors())

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

/** The one feed each test's rows are in, its summary's reloads counted. */
let feed: RunRecovery & { reload: ReturnType<typeof vi.fn<() => void>> }
beforeEach(() => {
  forgetRecoveries()
  feed = { key: "history runs {}", reload: vi.fn<() => void>() }
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

function renderRow(
  ui: ReactNode,
  technical = false,
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
) {
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
    actor: "act-person",
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
    renderRow(<HistoryEntryRow entry={entry} today runRecovery={feed} />)
    const move = (await screen.findByText("Priority:")).closest(
      "[data-slot=value-move]"
    )
    expect(move?.textContent).toBe("Priority:High→toUrgent")
  })

  it("falls back to the names against a server that sends none", async () => {
    const [entry] = foldHistory([patch(false)])
    renderRow(<HistoryEntryRow entry={entry} today runRecovery={feed} />)
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
    renderRow(
      <HistoryEntryRow
        entry={{ ...entry, openEnded: true }}
        today
        runRecovery={feed}
      />
    )
    const said = (await screen.findByText("tasks")).closest(
      "[data-slot=history-entry]"
    )
    expect(said?.textContent).not.toMatch(/\d\+|\+/)
    expect(said?.textContent).toContain("added tasks")
  })

  it("says a run the server summarized with its exact count, reading no rows", async () => {
    const entry = runEntry(
      {
        actor: "act-person",
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
    renderRow(<HistoryEntryRow entry={entry} today runRecovery={feed} />)
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
        actor: "act-person",
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
    renderRow(<HistoryEntryRow entry={entry} today runRecovery={feed} />)
    expect(await screen.findByText("Urgent")).toBeTruthy()
    const url = String(vi.mocked(fetch).mock.calls[0][0])
    expect(url).toContain("recordId=t1")
    expect(url).toContain("values=1")
  })

  // A run read from a position the server no longer holds (a restore
  // replaced the history): the summary that named it is read again, and the
  // refused position never again. One recovery a minute.
  const refusedRun = (newestSeq: number) =>
    runEntry(
      {
        actor: "act-person",
        kind: TASK,
        verb: "update",
        count: 1,
        records: 1,
        recordId: "t1",
        newestSeq,
        oldestSeq: newestSeq,
        newestTs: new Date().toISOString(),
        oldestTs: new Date().toISOString(),
      },
      { generation: "old", filter: {}, values: true }
    )
  const refusing = () =>
    vi.mocked(fetch).mockImplementation(() =>
      Promise.resolve(
        new Response(
          JSON.stringify({
            error: {
              code: "compacted",
              message: "the cursor addresses another history",
            },
          }),
          { status: 410, headers: { "content-type": "application/json" } }
        )
      )
    )
  const runReads = () =>
    vi
      .mocked(fetch)
      .mock.calls.filter((c) => String(c[0]).startsWith("/api/v1/changes"))
      .length
  const watched = () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    return { client, reloads: feed.reload }
  }

  it("reads the summary again when a run's rows are refused compacted, and never sends that position again", async () => {
    const at = vi.spyOn(Date, "now").mockReturnValue(1_000_000_000_000)
    refusing()
    const { client, reloads } = watched()
    const entry = refusedRun(7)
    const first = renderRow(
      <HistoryEntryRow entry={entry} today runRecovery={feed} />,
      false,
      client
    )
    await vi.waitFor(() => expect(reloads).toHaveBeenCalledOnce())
    expect(runReads()).toBe(1)
    first.unmount()
    renderRow(
      <HistoryEntryRow entry={entry} today runRecovery={feed} />,
      false,
      client
    )
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(runReads()).toBe(1)
    at.mockRestore()
  })

  it("reloads no summary for a run refused within a minute of the last reload, and does an hour on", async () => {
    let now = 2_000_000_000_000
    const at = vi.spyOn(Date, "now").mockImplementation(() => now)
    refusing()
    const { client, reloads } = watched()
    renderRow(
      <HistoryEntryRow entry={refusedRun(11)} today runRecovery={feed} />,
      false,
      client
    )
    await vi.waitFor(() => expect(reloads).toHaveBeenCalledOnce())
    now += 30_000
    renderRow(
      <HistoryEntryRow entry={refusedRun(12)} today runRecovery={feed} />,
      false,
      client
    )
    await vi.waitFor(() => expect(runReads()).toBe(2))
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(reloads).toHaveBeenCalledOnce()
    // The failure, said on its row.
    expect(
      screen.getByText(
        "What changed didn’t load: the cursor addresses another history"
      )
    ).toBeTruthy()
    now += 60 * 60_000
    renderRow(
      <HistoryEntryRow entry={refusedRun(13)} today runRecovery={feed} />,
      false,
      client
    )
    await vi.waitFor(() => expect(reloads).toHaveBeenCalledTimes(2))
    at.mockRestore()
  })

  // RULE W's minute to the millisecond, from the start of the feed's last
  // reload: a run's rows that land in between change nothing. A run refused
  // is never read again, so each call reads runs of its own (`seq` on).
  async function refusedAgainAt(after: number, seq: number) {
    const t0 = 3_000_000_000_000
    let now = t0
    const at = vi.spyOn(Date, "now").mockImplementation(() => now)
    // The second run's rows land; every other run's are refused.
    vi.mocked(fetch).mockImplementation((input) =>
      Promise.resolve(
        String(input).includes(`before=${seq + 2}`)
          ? new Response(
              JSON.stringify({
                changes: [
                  {
                    ...patch(true),
                    seq: seq + 1,
                    ts: new Date(t0).toISOString(),
                  },
                ],
                head: 40,
                generation: "old",
              }),
              { status: 200, headers: { "content-type": "application/json" } }
            )
          : new Response(
              JSON.stringify({
                error: {
                  code: "compacted",
                  message: "the cursor addresses another history",
                },
              }),
              { status: 410, headers: { "content-type": "application/json" } }
            )
      )
    )
    const { client, reloads } = watched()
    const row = (seq: number) =>
      renderRow(
        <HistoryEntryRow entry={refusedRun(seq)} today runRecovery={feed} />,
        false,
        client
      )
    row(seq)
    await vi.waitFor(() => expect(reloads).toHaveBeenCalledOnce())
    now = t0 + 30_000
    row(seq + 1)
    await vi.waitFor(() =>
      expect(
        client.getQueryCache().findAll({ queryKey: ["changes", "run"] })
      ).toSatisfy((qs: { state: { status: string } }[]) =>
        qs.some((q) => q.state.status === "success")
      )
    )
    now = t0 + after
    row(seq + 2)
    await vi.waitFor(() => expect(runReads()).toBe(3))
    await new Promise((resolve) => setTimeout(resolve, 50))
    at.mockRestore()
    return reloads
  }

  it("reloads no summary for a run refused 59,999 ms after the feed's last reload, though a run's rows landed between", async () => {
    const reloads = await refusedAgainAt(59_999, 31)
    expect(reloads).toHaveBeenCalledOnce()
  })

  it("reloads the summary for a run refused 60,000 ms after the feed's last reload", async () => {
    const reloads = await refusedAgainAt(60_000, 51)
    expect(reloads).toHaveBeenCalledTimes(2)
  })

  it("gives each feed its own minute", async () => {
    const at = vi.spyOn(Date, "now").mockReturnValue(4_000_000_000_000)
    refusing()
    const { client, reloads } = watched()
    renderRow(
      <HistoryEntryRow entry={refusedRun(41)} today runRecovery={feed} />,
      false,
      client
    )
    await vi.waitFor(() => expect(reloads).toHaveBeenCalledOnce())
    const another = { key: "history runs another", reload: feed.reload }
    renderRow(
      <HistoryEntryRow entry={refusedRun(42)} today runRecovery={another} />,
      false,
      client
    )
    await vi.waitFor(() => expect(reloads).toHaveBeenCalledTimes(2))
    at.mockRestore()
  })

  // The feed's runs under RULE W across a mount: the minute is the feed's
  // filter's, not the mount's. A server whose history is replaced under the
  // runs it summarized: each History read answers the next generation (a
  // run at 7 under g1, 9 under g2, 11 under g3), and the rows of a run under
  // a refused generation are refused.
  it("fails a run refused on a later mount within the minute of the feed's reload, and says so on its row", async () => {
    const t0 = 5_000_000_000_000
    let now = t0
    const at = vi.spyOn(Date, "now").mockImplementation(() => now)
    const refused = new Set(["g1"])
    let histories = 0
    const rowReads: string[] = []
    const json = (body: unknown, status = 200) =>
      new Response(JSON.stringify(body), {
        status,
        headers: { "content-type": "application/json" },
      })
    vi.mocked(fetch).mockImplementation((input) => {
      const url = new URL(String(input), "http://x")
      const q = url.searchParams
      if (!url.pathname.endsWith("/changes"))
        return Promise.resolve(json({ records: [], head: 1, generation: "g" }))
      if (q.has("runs")) {
        histories++
        const seq = 5 + 2 * histories
        return Promise.resolve(
          json({
            runs: [
              {
                actor: "act-person",
                kind: TASK,
                verb: "update",
                count: 1,
                records: 1,
                recordId: "t1",
                newestSeq: seq,
                oldestSeq: seq,
                newestTs: new Date(t0).toISOString(),
                oldestTs: new Date(t0).toISOString(),
              },
            ],
            head: seq,
            generation: `g${histories}`,
          })
        )
      }
      const generation = q.get("generation") ?? ""
      rowReads.push(`${q.get("before")} ${generation}`)
      if (refused.has(generation))
        return Promise.resolve(
          json(
            {
              error: {
                code: "compacted",
                message: "the cursor addresses another history",
              },
            },
            410
          )
        )
      return Promise.resolve(
        json({
          changes: [{ ...patch(true), seq: Number(q.get("before")) - 1 }],
          head: 40,
          generation,
        })
      )
    })
    function Feed() {
      const feed = useHistoryFeed({}, { live: false, values: true })
      return (
        <HistorySentences
          entries={feed.entries}
          runRecovery={feed.runRecovery}
        />
      )
    }
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    // Run 7's rows refused at T0: the feed reads its summary again, and run
    // 9's rows land.
    const first = renderRow(<Feed />, false, client)
    await vi.waitFor(() => expect(rowReads).toEqual(["8 g1", "10 g2"]))
    await screen.findByText("Urgent")
    expect(histories).toBe(2)
    first.unmount()
    // Replaced again; mounted 20 s after the reload started, the feed reads
    // its summary for the mount, and run 11's rows are refused: the failure.
    refused.add("g3")
    now = t0 + 20_000
    renderRow(<Feed />, false, client)
    await vi.waitFor(() => expect(rowReads).toContain("12 g3"))
    await new Promise((resolve) => setTimeout(resolve, 50))
    // No second reload of the summary within the minute, and the failure
    // said on the run's row.
    expect.soft(histories).toBe(3)
    expect
      .soft(
        screen.queryByText(
          "What changed didn’t load: the cursor addresses another history"
        )
      )
      .not.toBeNull()
    // The refused positions were each sent once.
    expect(rowReads).toEqual(["8 g1", "10 g2", "12 g3"])
    at.mockRestore()
  })

  it("says a rename across many records once, by both names", async () => {
    const rows = ["t1", "t2"].map((id, i): ChangeRow => ({
      seq: 20 - i,
      ts: new Date().toISOString(),
      actor: "act-person",
      op: "patch",
      recordId: id,
      kind: TASK,
      payload: {
        properties: ["priority", "urgency"],
        renamed: { urgency: "priority" },
      },
    }))
    const [entry] = foldHistory(rows)
    renderRow(<HistoryEntryRow entry={entry} today runRecovery={feed} />)
    expect(await screen.findByText("Urgency renamed to Priority")).toBeTruthy()
  })

  it("says a provider's update as what it is, and keeps the digest for technical details", async () => {
    const row: ChangeRow = {
      seq: 1053,
      ts: new Date().toISOString(),
      actor: "act-pkg-google",
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
    renderRow(<HistoryEntryRow entry={entry} today runRecovery={feed} />)
    expect(
      await screen.findByText("updated its package to version 35")
    ).toBeTruthy()
    expect(document.body.textContent).not.toContain("82007523")
    cleanup()
    renderRow(<HistoryEntryRow entry={entry} today runRecovery={feed} />, true)
    expect(await screen.findByText("originDigest:")).toBeTruthy()
  })

  it("puts the sequence number and the actor id after the sentence, each with a copy button", async () => {
    const [entry] = foldHistory([patch(true)])
    renderRow(<HistoryEntryRow entry={entry} today runRecovery={feed} />, true)
    expect(
      await screen.findByRole("button", { name: "Copy the sequence number" })
    ).toBeTruthy()
    expect(
      screen.getByRole("button", { name: "Copy the actor id" })
    ).toBeTruthy()
    // The id reads once, after the sentence, not inside the actor's link.
    expect(screen.getAllByText("act-person")).toHaveLength(1)
  })
})
