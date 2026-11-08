// @vitest-environment jsdom
/** A chat marked Working in the list is watched until it settles: the chats
 * column follows the thread rows while one of its chats runs, from the list's
 * own head, so a chat that settles between the list read and the stream
 * opening loses the mark too; nothing is watched when none runs. The watch is
 * the real hook over a fake server, not a stand-in. */

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

import { AgentsPage } from "./agents"

const THREAD = "substrate.reamde.dev/llm/thread"

/** The fake server: one chat, the changelog head, and what lands after the
 * first list read answered. */
let server: {
  status: string
  head: number
  rows: { seq: number; status: string }[]
  afterFirstList?: () => void
  lists: number
  watches: {
    from: string | null
    generation: string | null
    signal: AbortSignal
  }[]
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
 * past `from` (or none, when the stream starts at the head), open until the
 * client stops it. */
function watchStream(from: number | null, signal: AbortSignal): Response {
  const enc = new TextEncoder()
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      const start = from ?? server.head
      controller.enqueue(
        enc.encode(JSON.stringify({ bookmark: start, generation: "g" }) + "\n")
      )
      for (const row of server.rows.filter((r) => r.seq > start))
        controller.enqueue(
          enc.encode(
            JSON.stringify({
              seq: row.seq,
              ts: "2026-11-08T10:01:00Z",
              actor: "agent:substrate.reamde.dev:core:helper",
              op: "patch",
              recordId: "t-1",
              kind: THREAD,
            }) + "\n"
          )
        )
      signal.addEventListener("abort", () =>
        controller.error(new DOMException("stopped", "AbortError"))
      )
    },
  })
  return new Response(body, {
    status: 200,
    headers: { "Content-Type": "application/x-ndjson" },
  })
}

beforeEach(() => {
  server = { status: "running", head: 1, rows: [], lists: 0, watches: [] }
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
        if (server.lists === 1) server.afterFirstList?.()
        return new Response(JSON.stringify(page), { status: 200 })
      }
      return new Response(JSON.stringify({ records: [] }), { status: 200 })
    })
  )
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
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
    server.afterFirstList = () => {
      server.status = "ok"
      server.head = 2
      server.rows.push({ seq: 2, status: "ok" })
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

  it("watches nothing when no listed chat runs", async () => {
    server.status = "ok"
    renderPage()
    expect(await screen.findByText("t-1: ok")).toBeTruthy()
    await new Promise((r) => setTimeout(r, 50))
    expect(server.watches).toEqual([])
  })
})
