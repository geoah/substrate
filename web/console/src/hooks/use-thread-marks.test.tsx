// @vitest-environment jsdom
/** Read state over the chat list: which chats are unread, the write that
 * follows reading the open one (after it has been in front, held back a
 * little, and sent at once on leaving), Mark as Unread on the open chat
 * staying put, a mark from another device arriving on the feed, and a server
 * from before marks. The hook is real; the server and the feed are fakes. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, renderHook, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"

const watch = vi.hoisted(() => ({
  calls: [] as {
    filter?: { kinds?: string[] }
    onRow: (row: unknown) => void
    stopped: boolean
  }[],
}))

vi.mock("@/lib/api/changes", () => ({
  watchChanges: (opts: {
    filter?: { kinds?: string[] }
    onRow: (row: unknown) => void
  }) => {
    const call = { ...opts, stopped: false }
    watch.calls.push(call)
    return {
      stop: () => {
        call.stopped = true
      },
    }
  },
}))

import { SEEN_MS, useThreadMarks, WRITE_MS } from "./use-thread-marks"

const THREAD = "substrate.reamde.dev/llm/thread"
const MESSAGE = "substrate.reamde.dev/llm/message"
const MARK = "substrate.reamde.dev/llm/threadmark"

function record(id: string, kind: string, properties: Record<string, unknown>) {
  return {
    id,
    kind,
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-11-14T10:00:00Z",
    updatedAt: "2026-11-14T10:00:00Z",
  } as unknown as SubstrateRecord
}

function thread(id: string, last: string, status = "ok") {
  return record(id, THREAD, {
    status,
    lastMessage: { ref: `${MESSAGE}/${last}` },
  })
}

/** The fake server: the caller's marks by thread, every write it took, and
 * whether it knows the kind at all. */
let server: {
  marks: Map<string, { readThrough?: string; markedUnread?: boolean }>
  writes: { thread: string; readThrough?: string; markedUnread?: boolean }[]
  lists: number
  known: boolean
  /** Holds a write of a position until it is called, as a slow request. */
  hold?: Promise<void>
}

function stored(threadId: string): SubstrateRecord {
  const m = server.marks.get(threadId) ?? {}
  return record(`${threadId}.me`, MARK, {
    thread: { ref: `${THREAD}/${threadId}` },
    ...(m.readThrough
      ? { readThrough: { ref: `${MESSAGE}/${m.readThrough}` } }
      : {}),
    ...(m.markedUnread === undefined ? {} : { markedUnread: m.markedUnread }),
  })
}

beforeEach(() => {
  watch.calls.length = 0
  server = { marks: new Map(), writes: [], lists: 0, known: true }
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const u = new URL(String(url), "http://x")
      if (!server.known)
        return new Response(
          JSON.stringify({
            error: { code: "not_found", message: "unknown kind" },
          }),
          { status: 404 }
        )
      if (init?.method === "POST") {
        const body = JSON.parse(String(init.body)) as {
          properties: {
            thread: string
            readThrough?: string
            markedUnread?: boolean
          }
        }
        const { thread, readThrough, markedUnread } = body.properties
        server.writes.push(body.properties)
        if (readThrough && server.hold) await server.hold
        const held = server.marks.get(thread) ?? {}
        // As the server does: a taken position clears the flag unless the
        // write sets it.
        if (readThrough)
          server.marks.set(thread, {
            readThrough,
            markedUnread: markedUnread ?? false,
          })
        else server.marks.set(thread, { ...held, markedUnread })
        return new Response(JSON.stringify(stored(thread)), { status: 200 })
      }
      // The caller's own marks of the page's threads.
      expect(u.searchParams.get("filter")).toContain('"readBy":{"eq":"me"}')
      server.lists++
      return new Response(
        JSON.stringify({
          records: [...server.marks.keys()].map(stored),
          head: 1,
          generation: "g",
        }),
        { status: 200 }
      )
    })
  )
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

function wrapper() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
}

const threads = [thread("t1", "m2"), thread("t2", "m5"), thread("t3", "m9")]
/** The second chat open, as props a rerender may change to none. */
const opened: { open: SubstrateRecord | undefined } = { open: threads[1] }

describe("useThreadMarks", () => {
  it("says which chats are unread: no mark, a mark behind, and one marked so", async () => {
    server.marks.set("t1", { readThrough: "m2" })
    server.marks.set("t2", { readThrough: "m4" })
    const { result } = renderHook(() => useThreadMarks(threads, undefined), {
      wrapper: wrapper(),
    })
    await waitFor(() => expect(result.current.supported).toBe(true))
    expect([...result.current.unread].sort()).toEqual(["t2", "t3"])

    act(() => result.current.markUnread("t1"))
    await waitFor(() => expect(result.current.unread.has("t1")).toBe(true))
    expect(server.writes).toEqual([{ thread: "t1", markedUnread: true }])

    act(() => result.current.markRead("t1"))
    await waitFor(() => expect(result.current.unread.has("t1")).toBe(false))
    expect(server.writes[1]).toEqual({ thread: "t1", readThrough: "m2" })
  })

  it("reads the open chat once it has been in front, and holds the write back", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const { result } = renderHook(() => useThreadMarks(threads, threads[1]), {
      wrapper: wrapper(),
    })
    await waitFor(() => expect(result.current.supported).toBe(true))
    expect(result.current.unread.has("t2")).toBe(true)
    await act(() => vi.advanceTimersByTimeAsync(SEEN_MS - 50))
    expect(server.writes).toEqual([])
    // Seen, and the write waits for a newer reply to replace it.
    await act(() => vi.advanceTimersByTimeAsync(100))
    expect(server.writes).toEqual([])
    await act(() => vi.advanceTimersByTimeAsync(WRITE_MS))
    expect(server.writes).toEqual([{ thread: "t2", readThrough: "m5" }])
    await waitFor(() => expect(result.current.unread.has("t2")).toBe(false))
  })

  it("sends the read at once on leaving the chat, and nothing for a glance", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const { result, rerender } = renderHook(
      ({ open }: { open: SubstrateRecord | undefined }) =>
        useThreadMarks(threads, open),
      {
        wrapper: wrapper(),
        initialProps: opened,
      }
    )
    await waitFor(() => expect(result.current.supported).toBe(true))
    await act(() => vi.advanceTimersByTimeAsync(SEEN_MS + 50))
    expect(server.writes).toEqual([])
    // Another chat is opened before the held write went out.
    rerender({ open: threads[2] })
    await waitFor(() =>
      expect(server.writes).toEqual([{ thread: "t2", readThrough: "m5" }])
    )
    // That one is left before it was in front long enough.
    await act(() => vi.advanceTimersByTimeAsync(SEEN_MS / 2))
    rerender({ open: undefined })
    await act(() => vi.advanceTimersByTimeAsync(SEEN_MS + WRITE_MS))
    expect(server.writes).toHaveLength(1)
    expect(result.current.unread.has("t3")).toBe(true)
  })

  it("leaves the open chat unread after Mark as Unread, until it is left", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    server.marks.set("t2", { readThrough: "m5" })
    const { result, rerender } = renderHook(
      ({ open }: { open: SubstrateRecord | undefined }) =>
        useThreadMarks(threads, open),
      {
        wrapper: wrapper(),
        initialProps: opened,
      }
    )
    await waitFor(() => expect(result.current.supported).toBe(true))
    act(() => result.current.markUnread("t2"))
    await waitFor(() => expect(result.current.unread.has("t2")).toBe(true))
    await act(() => vi.advanceTimersByTimeAsync(SEEN_MS + WRITE_MS + 100))
    expect(server.writes).toEqual([{ thread: "t2", markedUnread: true }])
    // Left and opened again: reading it clears the flag.
    rerender({ open: undefined })
    rerender({ open: threads[1] })
    await act(() => vi.advanceTimersByTimeAsync(SEEN_MS + WRITE_MS + 100))
    expect(server.writes[1]).toEqual({ thread: "t2", readThrough: "m5" })
    await waitFor(() => expect(result.current.unread.has("t2")).toBe(false))
  })

  it("sends Mark as Unread after a read that was already on its way", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    let release = () => {}
    server.hold = new Promise<void>((resolve) => (release = resolve))
    const { result } = renderHook(() => useThreadMarks(threads, threads[1]), {
      wrapper: wrapper(),
    })
    await waitFor(() => expect(result.current.supported).toBe(true))
    // The read is sent and has not been answered yet.
    await act(() => vi.advanceTimersByTimeAsync(SEEN_MS + WRITE_MS + 100))
    expect(server.writes).toEqual([{ thread: "t2", readThrough: "m5" }])
    act(() => result.current.markUnread("t2"))
    await act(() => vi.advanceTimersByTimeAsync(200))
    // Held back until the read is answered, so it lands after it.
    expect(server.writes).toHaveLength(1)
    release()
    await waitFor(() =>
      expect(server.writes).toEqual([
        { thread: "t2", readThrough: "m5" },
        { thread: "t2", markedUnread: true },
      ])
    )
    await waitFor(() => expect(result.current.unread.has("t2")).toBe(true))
    expect(server.marks.get("t2")).toEqual({
      readThrough: "m5",
      markedUnread: true,
    })
  })

  it("follows a mark another device wrote, from the change feed", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const { result } = renderHook(() => useThreadMarks(threads, undefined), {
      wrapper: wrapper(),
    })
    await waitFor(() => expect(result.current.supported).toBe(true))
    expect(result.current.unread.has("t3")).toBe(true)
    await waitFor(() => expect(watch.calls).toHaveLength(1))
    expect(watch.calls[0].filter?.kinds).toEqual([MARK])
    // The phone reads the chat: its mark lands on the feed.
    server.marks.set("t3", { readThrough: "m9" })
    const lists = server.lists
    act(() => watch.calls[0].onRow({ seq: 2, kind: MARK, recordId: "t3.me" }))
    await act(() => vi.advanceTimersByTimeAsync(1500))
    await waitFor(() => expect(result.current.unread.has("t3")).toBe(false))
    expect(server.lists).toBeGreaterThan(lists)
  })

  it("shows nothing unread and writes nothing against a server from before marks", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    server.known = false
    const { result } = renderHook(() => useThreadMarks(threads, threads[1]), {
      wrapper: wrapper(),
    })
    await act(() => vi.advanceTimersByTimeAsync(SEEN_MS + WRITE_MS + 100))
    expect(result.current.supported).toBe(false)
    expect(result.current.unread.size).toBe(0)
    expect(server.writes).toEqual([])
    expect(watch.calls).toEqual([])
  })
})
