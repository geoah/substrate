// @vitest-environment jsdom
/** A chat marked Working in the list is watched until it settles: the chats
 * column follows the thread rows while one of its chats runs, from the list's
 * own head, so a chat that settles between the list read and the stream
 * opening loses the mark too, and so does one that settles while the page
 * recovers from a head the server no longer holds. It recovers once: a
 * refusal of the stream it opened again ends the watch. Nothing is watched
 * when none runs. The watch is the real hook over a fake server, not a
 * stand-in. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen, waitFor } from "@testing-library/react"
import { NuqsTestingAdapter } from "nuqs/adapters/testing"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("@/components/agent/conversation", () => ({ Conversation: () => null }))
vi.mock("@/components/agent/agent-panel", () => ({ AgentPanel: () => null }))
vi.mock("@/components/agent/thread-list", () => ({
  ThreadList: ({
    rows,
  }: {
    rows: { thread: { id: string; properties: { status?: string } } }[]
  }) => (
    <ul>
      {rows.map((r) => (
        <li
          key={r.thread.id}
        >{`${r.thread.id}: ${r.thread.properties.status}`}</li>
      ))}
    </ul>
  ),
}))

import { forgetRecoveries } from "@/lib/api/compacted"

import { AgentsPage } from "./agents"

const THREAD = "substrate.reamde.dev/llm/thread"

/** The fake server: one chat, the changelog head, the open streams, and
 * what lands after a list read answered. */
let server: {
  status: string
  head: number
  rows: { seq: number; status: string }[]
  afterList?: (n: number) => void
  lists: number
  /** Refuse every resume position, as a restored repository does. */
  lost?: boolean
  /** Fix a stream's head this many ms after its request, as a request that
   * reaches the server late does. */
  watchDelay?: number
  watches: {
    from: string | null
    generation: string | null
    signal: AbortSignal
  }[]
  streams: { start: number; push: (seq: number) => void; end: () => void }[]
}

/** A chat's settle: committed at the next seq and sent to every open stream
 * past whose head it lands. */
function settle() {
  server.status = "ok"
  server.head++
  server.rows.push({ seq: server.head, status: "ok" })
  for (const s of server.streams) if (server.head > s.start) s.push(server.head)
}

/** The server ends every open stream, as a restart does: each client
 * reconnects from its own bookmark. */
function endStreams() {
  for (const s of server.streams) s.end()
  server.streams = []
}

function thread(status: string) {
  return {
    id: "t-1",
    kind: THREAD,
    properties: {
      agent: { ref: "substrate.reamde.dev/core/agent/helper" },
      mode: "chat",
      status,
    },
    labels: {},
    version: 1,
    createdAt: "2026-11-08T10:00:00Z",
    updatedAt: "2026-11-08T10:00:00Z",
  }
}

/** A watch stream as the server answers one: the bookmark, then every row
 * past `from` (or none, when the stream starts at the head), then every row
 * that lands while it is open, until the client stops it. */
function watchStream(from: number | null, signal: AbortSignal): Response {
  const enc = new TextEncoder()
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      const start = from ?? server.head
      const push = (seq: number) =>
        controller.enqueue(
          enc.encode(
            JSON.stringify({
              seq,
              ts: "2026-11-08T10:01:00Z",
              actor: "agent:substrate.reamde.dev:core:helper",
              op: "patch",
              recordId: "t-1",
              kind: THREAD,
            }) + "\n"
          )
        )
      controller.enqueue(
        enc.encode(JSON.stringify({ bookmark: start, generation: "g" }) + "\n")
      )
      for (const row of server.rows.filter((r) => r.seq > start)) push(row.seq)
      const stream = { start, push, end: () => controller.close() }
      server.streams.push(stream)
      signal.addEventListener("abort", () => {
        server.streams = server.streams.filter((s) => s !== stream)
        controller.error(new DOMException("stopped", "AbortError"))
      })
    },
  })
  return new Response(body, {
    status: 200,
    headers: { "Content-Type": "application/x-ndjson" },
  })
}

beforeEach(() => {
  server = {
    status: "running",
    head: 1,
    rows: [],
    lists: 0,
    watches: [],
    streams: [],
  }
  vi.stubGlobal(
    "matchMedia",
    (query: string) =>
      ({
        matches: true,
        media: query,
        addEventListener: () => {},
        removeEventListener: () => {},
      }) as unknown as MediaQueryList
  )
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const u = new URL(String(url), "http://x")
      if (u.searchParams.get("watch") === "1") {
        const signal = init?.signal ?? new AbortController().signal
        server.watches.push({
          from: u.searchParams.get("from"),
          generation: u.searchParams.get("generation"),
          signal,
        })
        const from = u.searchParams.get("from")
        if (from !== null && server.lost)
          return new Response(
            JSON.stringify({
              error: { code: "compacted", message: "history replaced" },
            }),
            { status: 410 }
          )
        if (from === null && server.watchDelay)
          await new Promise((r) => setTimeout(r, server.watchDelay))
        return watchStream(from === null ? null : Number(from), signal)
      }
      const filter = u.searchParams.get("filter")
      // The chat list: every conversation, by its mode.
      if (filter?.includes('"mode"') && filter.includes("llm/thread")) {
        const page = {
          records: [thread(server.status)],
          head: server.head,
          generation: "g",
        }
        server.lists++
        server.afterList?.(server.lists)
        return new Response(JSON.stringify(page), { status: 200 })
      }
      return new Response(JSON.stringify({ records: [] }), { status: 200 })
    })
  )
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  forgetRecoveries()
})

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <NuqsTestingAdapter searchParams="">
        <AgentsPage />
      </NuqsTestingAdapter>
    </QueryClientProvider>
  )
}

describe("AgentsPage chat list", () => {
  it("hears a chat that settles after the list read and before the watch opens", async () => {
    // The chat settles at seq 2, once the list answered at head 1: the
    // stream opens later, at a head that is already past the settle.
    server.afterList = (n) => {
      if (n === 1) settle()
    }
    renderPage()
    expect(await screen.findByText("t-1: running")).toBeTruthy()
    expect(
      await screen.findByText("t-1: ok", {}, { timeout: 3000 })
    ).toBeTruthy()
    expect(server.watches[0]).toMatchObject({ from: "1", generation: "g" })
    // Settled, so nothing runs and the watch stops.
    await waitFor(() => expect(server.watches[0].signal.aborted).toBe(true))
  })

  it("hears a chat that settles while the page recovers from a head the server no longer holds", async () => {
    // The list's head is refused, so the page lists again and follows from
    // the server's head. The chat settles right after that second list
    // answered, and a stream request reaches the server later than a list
    // read does: a stream opened beside the read would start past the
    // settle and leave the chat Working.
    server.lost = true
    server.watchDelay = 30
    server.afterList = (n) => {
      if (n === 2) settle()
    }
    renderPage()
    expect(await screen.findByText("t-1: running")).toBeTruthy()
    expect(
      await screen.findByText("t-1: ok", {}, { timeout: 3000 })
    ).toBeTruthy()
    expect(server.watches[0]).toMatchObject({ from: "1", generation: "g" })
    expect(server.watches[1]).toMatchObject({ from: null })
    // Settled, so nothing runs and no stream is left open.
    await waitFor(() => expect(server.streams).toEqual([]))
  })

  it("ends the watch when the stream it opened again is refused too, and lists no more", async () => {
    // The list's head is refused, so the page follows from the server's head
    // and lists again. That stream then ends, and its reconnect resumes from
    // its own bookmark, which this server refuses too: the watch's bound
    // makes that a failure, never a second recovery, so a server that keeps
    // refusing gets two streams and two list reads.
    vi.useFakeTimers({ shouldAdvanceTime: true })
    server.lost = true
    server.afterList = (n) => {
      if (n === 2) endStreams()
    }
    renderPage()
    expect(await screen.findByText("t-1: running")).toBeTruthy()
    // The reconnect waits out the stream's retry delay.
    while (server.watches.length < 3) await vi.advanceTimersByTimeAsync(500)
    await vi.advanceTimersByTimeAsync(30_000)
    expect(server.watches.map((w) => w.from)).toEqual(["1", null, "1"])
    expect(server.lists).toBe(2)
    expect(server.streams).toEqual([])
  })

  it("watches nothing when no listed chat runs", async () => {
    server.status = "ok"
    renderPage()
    expect(await screen.findByText("t-1: ok")).toBeTruthy()
    await new Promise((r) => setTimeout(r, 50))
    expect(server.watches).toEqual([])
  })
})
