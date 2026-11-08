// @vitest-environment jsdom
/** A chat marked Working in the list is watched until it settles: the chats
 * column follows the thread rows while one of its chats runs, so one that
 * settles behind another chat loses the mark, and nothing is watched when
 * none runs. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, waitFor } from "@testing-library/react"
import { NuqsTestingAdapter } from "nuqs/adapters/testing"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

const watches: { kinds: string[]; invalidate: unknown[] }[] = []

vi.mock("@/hooks/use-live-records", () => ({
  useLiveRecords: (kinds: string[], invalidate: unknown[]) => {
    watches.push({ kinds, invalidate })
  },
}))
vi.mock("@/components/agent/conversation", () => ({ Conversation: () => null }))
vi.mock("@/components/agent/thread-list", () => ({ ThreadList: () => null }))
vi.mock("@/components/agent/agent-panel", () => ({ AgentPanel: () => null }))

import { THREAD_LISTS_KEY } from "@/lib/api/agents"
import { AgentsPage } from "./agents"

let status = "running"

beforeEach(() => {
  watches.length = 0
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
    vi.fn(async (url: string) => {
      const filter = new URL(String(url), "http://x").searchParams.get("filter")
      // The chat list: every conversation, by its mode.
      if (filter?.includes('"mode"') && filter.includes("llm/thread"))
        return new Response(
          JSON.stringify({
            records: [
              {
                id: "t-1",
                kind: "substrate.reamde.dev/llm/thread",
                properties: {
                  agent: { ref: "substrate.reamde.dev/core/agent/helper" },
                  mode: "chat",
                  status,
                },
                labels: {},
                version: 1,
                createdAt: "2026-11-08T10:00:00Z",
                updatedAt: "2026-11-08T10:00:00Z",
              },
            ],
            head: 1,
            generation: "g",
          }),
          { status: 200 }
        )
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
  it("watches the thread rows while a listed chat runs", async () => {
    status = "running"
    renderPage()
    await waitFor(() =>
      expect(watches.at(-1)?.kinds).toEqual(["substrate.reamde.dev/llm/thread"])
    )
    expect(watches.at(-1)?.invalidate).toEqual([[...THREAD_LISTS_KEY]])
  })

  it("watches nothing when no listed chat runs", async () => {
    status = "ok"
    renderPage()
    await waitFor(() => expect(vi.mocked(fetch)).toHaveBeenCalled())
    await new Promise((r) => setTimeout(r, 50))
    expect(watches.length).toBeGreaterThan(0)
    expect(watches.every((w) => w.kinds.length === 0)).toBe(true)
  })
})
