// @vitest-environment jsdom
/** History's live tail held to RULE W's minute to the millisecond, against
 * the real stream: told its position is gone, the feed reads History again
 * and opens the tail at the head, and that recovery starts as the tail
 * opens. A refusal 59,999 ms on is the failure, `stopped`; one 60,000 ms
 * on recovers again; a bookmark, the History read again and a delivered
 * change in between change neither. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, renderHook } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

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

/** History whose tail resumes at 7 under `g`, a position the server
 * refuses: the tail opened again at the head sends its bookmark and a
 * change, and is refused again `after` ms after it opened. */
function server(after: number) {
  let histories = 0
  const tails: { from: string | null; at: number }[] = []
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>(async (input) => {
      const q = new URL(String(input), "http://x").searchParams
      if (!q.has("watch")) {
        histories++
        return json({ runs: [], head: 7, generation: "g" })
      }
      tails.push({ from: q.get("from"), at: Date.now() })
      if (tails.length === 1)
        return json(
          {
            error: {
              code: "compacted",
              message: "seq 7 is not in this history",
            },
          },
          410
        )
      const second = tails.length === 2
      const body = new ReadableStream<Uint8Array>({
        start(controller) {
          controller.enqueue(line({ bookmark: 9, generation: "g2" }))
          if (!second) return
          setTimeout(
            () =>
              controller.enqueue(
                line({
                  seq: 10,
                  ts: new Date().toISOString(),
                  actor: "act-person",
                  op: "put",
                  kind: "acme.example.com/tasks/task",
                  recordId: "t1",
                })
              ),
            1_000
          )
          setTimeout(
            () =>
              controller.enqueue(
                line({
                  error: {
                    code: "compacted",
                    message: "seq 10 is not in this history",
                  },
                })
              ),
            after
          )
        },
      })
      return new Response(body, {
        status: 200,
        headers: { "content-type": "application/x-ndjson" },
      })
    })
  )
  return { tails, histories: () => histories }
}

describe("History's tail, by RULE W's minute", () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => {
    cleanup()
    vi.useRealTimers()
    vi.unstubAllGlobals()
    forgetRecoveries()
  })

  async function refusedAgainAt(after: number) {
    const { tails, histories } = server(after)
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const wrapper = ({ children }: { children: ReactNode }) => (
      <ConsolePreferencesContext.Provider value={preferences}>
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      </ConsolePreferencesContext.Provider>
    )
    const hook = renderHook(() => useHistoryFeed({}), { wrapper })
    // React renders what a stream told it as each `act` ends, so the clock
    // moves in steps: to the tail opened again, past its change, to a
    // millisecond before the refusal, and onto it.
    const step = (ms: number) =>
      act(async () => {
        await vi.advanceTimersByTimeAsync(ms)
      })
    for (let i = 0; i < 20 && tails.length < 2; i++) await step(0)
    expect(tails).toHaveLength(2)
    await step(1_000)
    await step(after - 1_000 - 1)
    const between = {
      histories: histories(),
      live: hook.result.current.entries.length,
      status: hook.result.current.status,
    }
    await step(1)
    for (let i = 0; i < 20; i++) await step(0)
    const status = hook.result.current.status
    hook.unmount()
    client.clear()
    return { tails, between, status }
  }

  it("stops on a refusal 59,999 ms after the tail opened again", async () => {
    const { tails, between, status } = await refusedAgainAt(59_999)
    expect(between).toEqual({ histories: 2, live: 1, status: "live" })
    expect(tails.map((t) => t.from)).toEqual(["7", null])
    expect(status).toBe("stopped")
  })

  it("recovers on a refusal 60,000 ms after the tail opened again", async () => {
    const { tails, between, status } = await refusedAgainAt(60_000)
    expect(between).toEqual({ histories: 2, live: 1, status: "live" })
    expect(tails.map((t) => t.from)).toEqual(["7", null, null])
    expect(tails[2].at - tails[1].at).toBe(60_000)
    expect(status).toBe("live")
  })
})
