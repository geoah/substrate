// @vitest-environment jsdom
/** History's live tail under RULE W across a mount, against the real stream
 * and the feed as History renders it. The tail's failure is shown: the feed
 * says it didn't load, with Try again, and the tail `Stopped`. The minute
 * belongs to the feed's filter, not to a mount: mounted again within it,
 * the feed sends nothing from the history a refusal dropped, and a refusal
 * there is the failure, never a second recovery. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { HistoryFeed, LiveStatus } from "@/components/changelog/history-feed"
import {
  ConsolePreferencesContext,
  type ConsolePreferencesContextValue,
} from "@/hooks/use-console-preferences"
import { forgetRecoveries } from "@/lib/api/compacted"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"

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

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  })

const encoder = new TextEncoder()
const line = (value: unknown) => encoder.encode(JSON.stringify(value) + "\n")

interface Tail {
  from: string | null
  generation: string | null
}

const refusal = (message: string) =>
  json({ error: { code: "compacted", message } }, 410)

/** A server whose history `replace` replaces: each History read answers
 * the current generation (head 7 under g1, then 9 under g2, …), a tail that
 * resumes a position under an older one is refused, and a replacement ends
 * every open tail with a `compacted` frame. */
function server() {
  let generation = 1
  const head = () => 5 + 2 * generation
  let histories = 0
  const tails: Tail[] = []
  const open = new Set<ReadableStreamDefaultController<Uint8Array>>()
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>(async (input, init) => {
      const q = new URL(String(input), "http://x").searchParams
      if (!q.has("watch")) {
        histories++
        return json({ runs: [], head: head(), generation: `g${generation}` })
      }
      tails.push({ from: q.get("from"), generation: q.get("generation") })
      if (q.has("from") && q.get("generation") !== `g${generation}`)
        return refusal(`seq ${q.get("from")} is not in this history`)
      const body = new ReadableStream<Uint8Array>({
        start(controller) {
          open.add(controller)
          init?.signal?.addEventListener("abort", () => {
            open.delete(controller)
            controller.error(new DOMException("stopped", "AbortError"))
          })
          controller.enqueue(
            line({ bookmark: head(), generation: `g${generation}` })
          )
        },
      })
      return new Response(body, {
        status: 200,
        headers: { "content-type": "application/x-ndjson" },
      })
    })
  )
  const replace = () => {
    generation++
    for (const controller of open)
      controller.enqueue(
        line({
          error: { code: "compacted", message: "the history was replaced" },
        })
      )
    open.clear()
  }
  return { tails, histories: () => histories, replace }
}

function Feed() {
  const feed = useHistoryFeed({})
  return (
    <>
      <LiveStatus status={feed.status} />
      <HistoryFeed feed={feed} empty="Nothing has changed yet." />
    </>
  )
}

describe("History's tail across a mount", () => {
  let client: QueryClient
  let wrapper: (props: { children: ReactNode }) => ReactNode
  beforeEach(() => {
    vi.useFakeTimers()
    client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    wrapper = ({ children }) => (
      <ConsolePreferencesContext.Provider value={preferences}>
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      </ConsolePreferencesContext.Provider>
    )
  })
  afterEach(() => {
    cleanup()
    client.clear()
    vi.useRealTimers()
    vi.unstubAllGlobals()
    forgetRecoveries()
  })

  // React renders what a stream told it as each `act` ends, so the clock
  // moves in steps.
  const step = async (ms: number) => {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ms)
    })
    for (let i = 0; i < 10; i++)
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0)
      })
  }

  const replaced = (replace: () => void) =>
    act(async () => {
      replace()
      await vi.advanceTimersByTimeAsync(0)
    })

  it("shows the tail's failure: History didn't load, with Try again, and Stopped", async () => {
    const { tails, replace } = server()
    render(<Feed />, { wrapper })
    await step(0)
    expect(tails).toEqual([{ from: "7", generation: "g1" }])
    // Replaced at T0: the recovery. Replaced again 30 s on: the failure.
    await replaced(replace)
    await step(0)
    expect(screen.getByText("Live")).toBeTruthy()
    await step(30_000)
    await replaced(replace)
    await step(0)
    expect(screen.getByText("Stopped")).toBeTruthy()
    expect(
      screen.getByText("History didn’t load: the history was replaced")
    ).toBeTruthy()
    expect(screen.getByRole("button", { name: "Try again" })).toBeTruthy()
    expect(tails).toHaveLength(2)
  })

  it("sends nothing from the refused history when mounted again within the minute, and fails there", async () => {
    const { tails, histories, replace } = server()
    const first = render(<Feed />, { wrapper })
    await step(0)
    await replaced(replace)
    await step(30_000)
    // The tail opened for the recovery read History under g2; replaced
    // again, g2 is refused, and so is every position read under it.
    await replaced(replace)
    await step(0)
    expect(tails).toHaveLength(2)
    first.unmount()
    await step(10_000)
    const read = histories()
    render(<Feed />, { wrapper })
    await step(0)
    const again = tails.slice(2)
    // The feed reads its first page and resumes the head it names: nothing
    // from g2 is sent again.
    expect(again.filter((t) => t.generation === "g2")).toEqual([])
    expect(again).toEqual([{ from: "11", generation: "g3" }])
    expect(histories()).toBe(read + 1)
    expect(screen.getByText("Live")).toBeTruthy()
    // Replaced 45 s after the recovery started: the failure, shown.
    await step(5_000)
    await replaced(replace)
    await step(0)
    expect(screen.getByText("Stopped")).toBeTruthy()
    expect(
      screen.getByText("History didn’t load: the history was replaced")
    ).toBeTruthy()
    expect(tails).toHaveLength(3)
  })

  it("fails a refusal on a later mount within the minute of the recovery, never a second recovery", async () => {
    const { tails, histories, replace } = server()
    const first = render(<Feed />, { wrapper })
    await step(0)
    await replaced(replace)
    await step(0)
    expect(tails).toHaveLength(2)
    first.unmount()
    await step(20_000)
    render(<Feed />, { wrapper })
    await step(0)
    expect(screen.getByText("Live")).toBeTruthy()
    const opened = tails.length
    const read = histories()
    // Replaced 30 s after the recovery started, on another mount: the
    // failure, never a tail at the head and a History read for a second
    // recovery.
    await step(10_000)
    await replaced(replace)
    await step(60_000)
    expect(screen.getByText("Stopped")).toBeTruthy()
    expect(
      screen.getByText("History didn’t load: the history was replaced")
    ).toBeTruthy()
    expect(tails).toHaveLength(opened)
    expect(histories()).toBe(read)
  })
})
