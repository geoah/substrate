// @vitest-environment jsdom
/** The watch's bound against the real stream, one recovery a minute: a
 * position the server no longer holds is followed from the head once, a
 * refusal within a minute of that recovery is a failure, `stopped`, never a
 * third stream, and one later recovers again, whatever came between. A
 * watch opened for a recovery is one whose reader's recovery has started. */

import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query"
import { act, cleanup, renderHook } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { watchChanges, type WatchStatus } from "@/lib/api/changes"
import {
  forgetRecoveries,
  nothingSaved,
  recoverOrFail,
  unshownFailures,
  type ResumingReader,
} from "@/lib/api/compacted"
import { ApiError } from "@/lib/api/types"

import { useLiveRecords } from "./use-live-records"

const TASK = "acme.example.com/tasks/task"

const refused = () =>
  new Response(
    JSON.stringify({
      error: {
        code: "compacted",
        message:
          "seq 7 is not in this history: re-list and resume from the head",
        head: 9,
        generation: "g2",
      },
    }),
    { status: 410, headers: { "content-type": "application/json" } }
  )

/** A watch's reader, whose recovery started now: the watch is the one
 * opened for it. */
function recovering(fail = vi.fn()): ResumingReader {
  const reader = { key: "tail", drop: nothingSaved, fail }
  recoverOrFail(reader, new ApiError("compacted", "refused", 410))
  return reader
}

function server() {
  const fetchMock = vi.fn<typeof fetch>(async () => refused())
  vi.stubGlobal("fetch", fetchMock)
  const asked = (n: number) =>
    new URL(String(fetchMock.mock.calls[n]?.[0]), "http://x").searchParams
  return { fetchMock, asked }
}

describe("the watch's bound", () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => {
    cleanup()
    vi.useRealTimers()
    vi.unstubAllGlobals()
    forgetRecoveries()
  })

  it("stops a watch opened after a re-list on a compacted, and asks for nothing more", async () => {
    const { fetchMock } = server()
    const statuses: [WatchStatus, string | undefined][] = []
    const onCompacted = vi.fn()
    const fail = vi.fn()
    const handle = watchChanges({
      from: 9,
      generation: "g2",
      reader: recovering(fail),
      onRow: () => {},
      onStatus: (status, detail) => statuses.push([status, detail]),
      onCompacted,
    })
    await vi.advanceTimersByTimeAsync(30_000)
    expect(statuses.at(-1)).toEqual([
      "stopped",
      expect.stringContaining("re-list"),
    ])
    expect(statuses.map(([s]) => s)).not.toContain("compacted")
    expect(onCompacted).not.toHaveBeenCalled()
    expect(fail).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ code: "compacted" })
    )
    expect(fetchMock).toHaveBeenCalledOnce()
    handle.stop()
  })

  // A re-listed watch that opened, sent its bookmark and then lost its
  // stream after `after` ms, its reconnect refused.
  async function refusedAgainAfter(after: number) {
    let opened = 0
    const fetchMock = vi.fn<typeof fetch>(async () => {
      if (opened++ > 0) return refused()
      const body = new ReadableStream<Uint8Array>({
        start(controller) {
          controller.enqueue(
            new TextEncoder().encode(
              JSON.stringify({ bookmark: 9, generation: "g2" }) + "\n"
            )
          )
          setTimeout(() => controller.close(), after)
        },
      })
      return new Response(body, {
        status: 200,
        headers: { "content-type": "application/x-ndjson" },
      })
    })
    vi.stubGlobal("fetch", fetchMock)
    const statuses: WatchStatus[] = []
    const onCompacted = vi.fn()
    const handle = watchChanges({
      reader: recovering(),
      onRow: () => {},
      onStatus: (status) => statuses.push(status),
      onCompacted,
    })
    await vi.advanceTimersByTimeAsync(after + 10_000)
    handle.stop()
    return { fetchMock, statuses, onCompacted }
  }

  it("stops a re-listed watch refused again within the minute, though its bookmark came between", async () => {
    const { fetchMock, statuses, onCompacted } = await refusedAgainAfter(30_000)
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(statuses).toContain("stopped")
    expect(statuses).not.toContain("compacted")
    expect(onCompacted).not.toHaveBeenCalled()
  })

  it("recovers a re-listed watch refused again an hour after its recovery", async () => {
    const { fetchMock, statuses, onCompacted } = await refusedAgainAfter(
      60 * 60_000
    )
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(statuses).toContain("compacted")
    expect(statuses).not.toContain("stopped")
    expect(onCompacted).toHaveBeenCalledOnce()
  })

  it("follows a refused position from the head once, then stops: two streams, never a third", async () => {
    const { fetchMock, asked } = server()
    const statuses: WatchStatus[] = []
    const client = new QueryClient()
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    )
    const hook = renderHook(
      () =>
        useLiveRecords([TASK], [["records"]], (s) => statuses.push(s), {
          head: 7,
          generation: "old",
        }),
      { wrapper }
    )
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000)
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(asked(0).get("from")).toBe("7")
    expect(asked(1).has("from")).toBe(false)
    expect(statuses.filter((s) => s === "compacted")).toHaveLength(1)
    expect(statuses.at(-1)).toBe("stopped")
    hook.unmount()
  })

  // RULE W's minute to the millisecond. A collection followed from a
  // position the server refuses: the recovery starts as the watch opens
  // again at the head. That stream sends its bookmark, the list read again
  // lands, a change is delivered (and read), and then, `after` ms from the
  // recovery's start, the server refuses the stream again.
  async function refusedAgainAt(after: number) {
    const encoder = new TextEncoder()
    const line = (value: unknown) =>
      encoder.encode(JSON.stringify(value) + "\n")
    let opened = 0
    const recoveredAt: number[] = []
    const fetchMock = vi.fn<typeof fetch>(async () => {
      opened++
      if (opened === 1) return refused()
      recoveredAt.push(Date.now())
      const second = opened === 2
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
                  kind: TASK,
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
    vi.stubGlobal("fetch", fetchMock)
    let lists = 0
    const statuses: WatchStatus[] = []
    const client = new QueryClient()
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    )
    const hook = renderHook(
      () => {
        useQuery({
          queryKey: ["records", "tasks"],
          queryFn: () => Promise.resolve(++lists),
        })
        useLiveRecords(
          [TASK],
          [["records", "tasks"]],
          (s) => statuses.push(s),
          {
            head: 7,
            generation: "old",
          }
        )
      },
      { wrapper }
    )
    await act(async () => {
      await vi.advanceTimersByTimeAsync(after - 1_000)
    })
    // The reopened stream's bookmark, the list read again and the delivered
    // change all came between.
    const between = lists
    await act(async () => {
      await vi.advanceTimersByTimeAsync(11_000)
    })
    hook.unmount()
    const asked = (n: number) =>
      new URL(String(fetchMock.mock.calls[n]?.[0]), "http://x").searchParams
    return { fetchMock, statuses, between, recoveredAt, asked }
  }

  it("stops on a refusal 59,999 ms after the recovery's start, whatever came between", async () => {
    const { fetchMock, statuses, between, recoveredAt } =
      await refusedAgainAt(59_999)
    expect(recoveredAt).toHaveLength(1)
    expect(between).toBeGreaterThanOrEqual(3)
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(statuses.filter((s) => s === "compacted")).toHaveLength(1)
    expect(statuses).toContain("stopped")
  })

  it("recovers on a refusal 60,000 ms after the recovery's start", async () => {
    const { fetchMock, statuses, between, asked } = await refusedAgainAt(60_000)
    expect(between).toBeGreaterThanOrEqual(3)
    expect(fetchMock).toHaveBeenCalledTimes(3)
    expect(asked(2).has("from")).toBe(false)
    expect(statuses.filter((s) => s === "compacted")).toHaveLength(2)
    expect(statuses).not.toContain("stopped")
  })

  // The surface's read (7 under `old`) is the caller's, still cached after
  // the refusal; the stream opened for the recovery is refused 30 s on, the
  // failure. Mounted again within the minute, with that read still in hand.
  it("never resumes a read it dropped when mounted again within the minute, and fails a refusal there", async () => {
    const encoder = new TextEncoder()
    const line = (value: unknown) =>
      encoder.encode(JSON.stringify(value) + "\n")
    const opened: (string | null)[] = []
    const open = new Set<ReadableStreamDefaultController<Uint8Array>>()
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>(async (input, init) => {
        const from = new URL(String(input), "http://x").searchParams.get("from")
        opened.push(from)
        if (from !== null) return refused()
        const body = new ReadableStream<Uint8Array>({
          start(controller) {
            open.add(controller)
            init?.signal?.addEventListener("abort", () => {
              open.delete(controller)
              controller.error(new DOMException("stopped", "AbortError"))
            })
            controller.enqueue(line({ bookmark: 9, generation: "g2" }))
          },
        })
        return new Response(body, {
          status: 200,
          headers: { "content-type": "application/x-ndjson" },
        })
      })
    )
    const replace = () =>
      act(async () => {
        for (const controller of open)
          controller.enqueue(
            line({
              error: { code: "compacted", message: "the history was replaced" },
            })
          )
        open.clear()
        await vi.advanceTimersByTimeAsync(0)
      })
    let lists = 0
    const client = new QueryClient()
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    )
    const surface = () =>
      renderHook(
        () => {
          useQuery({
            queryKey: ["records", "tasks"],
            queryFn: () => Promise.resolve(++lists),
          })
          useLiveRecords([TASK], [["records", "tasks"]], undefined, {
            head: 7,
            generation: "old",
          })
        },
        { wrapper }
      )
    const first = surface()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000)
    })
    expect(opened).toEqual(["7", null])
    await replace()
    first.unmount()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000)
    })
    const read = lists
    surface()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000)
    })
    // The dropped read is not resumed: the stream opens at the head, and
    // the surface reads again once it is live.
    expect(opened.slice(2)).toEqual([null])
    expect(lists).toBeGreaterThan(read)
    // Refused 45 s after the recovery started: the failure, never a stream
    // opened again.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(4_000)
    })
    await replace()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000)
    })
    expect(opened).toHaveLength(3)
    expect(unshownFailures()).toHaveLength(2)
    expect(unshownFailures()[1]).toMatchObject({
      key: expect.stringContaining("live records"),
      refusal: { code: "compacted", message: "the history was replaced" },
    })
  })
})
