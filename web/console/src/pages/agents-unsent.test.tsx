// @vitest-environment jsdom
/** A refused message stays recoverable across the page's own chat switch:
 * opening another chat clears the draft (`openThread`), so the notice keeps
 * the saved copy with Copy and Dismiss instead of pointing at a composer
 * that no longer holds it, and sending something else keeps it. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { NuqsTestingAdapter } from "nuqs/adapters/testing"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
}))
vi.mock("@/components/agent/thread-list", () => ({
  ThreadList: (props: { onSelect: (thread: string) => void }) => (
    <div>
      <button type="button" onClick={() => props.onSelect("t-1")}>
        Open t-1
      </button>
      <button type="button" onClick={() => props.onSelect("t-9")}>
        Open t-9
      </button>
    </div>
  ),
}))
vi.mock("@/components/agent/agent-panel", () => ({ AgentPanel: () => null }))

type ChatOpts = { message: string; onError?: (error: Error) => void }
const calls: ChatOpts[] = []
vi.mock("@/lib/api/agents", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api/agents")>()
  return {
    ...actual,
    streamChat: (opts: ChatOpts) => {
      calls.push(opts)
      return { stop() {} }
    },
  }
})

import { ApiError } from "@/lib/api/types"
import { resetUnsent } from "@/lib/unsent-messages"
import { AgentsPage } from "./agents"

function thread(id: string) {
  return {
    id,
    kind: "substrate.reamde.dev/llm/thread",
    properties: {
      status: "ok",
      agent: { ref: "substrate.reamde.dev/core/agent/helper" },
    },
    labels: {},
    version: 1,
    createdAt: "2026-10-23T10:00:00Z",
    updatedAt: "2026-10-23T10:00:00Z",
  }
}

beforeEach(() => {
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
      const hit = /\/llm\/thread\/(t-\d+)$/.exec(String(url))
      if (hit) return new Response(JSON.stringify(thread(hit[1])))
      return new Response(JSON.stringify({ records: [] }))
    })
  )
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  calls.length = 0
  resetUnsent()
})

function box() {
  return screen.getByRole<HTMLTextAreaElement>("textbox")
}

describe("AgentsPage and a refused message", () => {
  it("keeps the saved copy after the page clears the draft on a chat switch", async () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <NuqsTestingAdapter searchParams="?thread=t-1" hasMemory>
          <AgentsPage />
        </NuqsTestingAdapter>
      </QueryClientProvider>
    )
    await waitFor(() => expect(box().disabled).toBe(false))
    fireEvent.change(box(), { target: { value: "hello" } })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    expect(calls).toHaveLength(1)
    act(() =>
      calls[0].onError?.(
        new ApiError("conflict", "a chat turn is already running", 409)
      )
    )
    // Refused: back in the composer, and a copy kept.
    expect(box().value).toBe("hello")

    // The page's own switch clears the draft.
    await act(async () => fireEvent.click(screen.getByText("Open t-9")))
    await act(async () => fireEvent.click(screen.getByText("Open t-1")))
    await waitFor(() => expect(box().disabled).toBe(false))
    expect(box().value).toBe("")
    const note = screen.getByRole("alert")
    expect(note.textContent).toContain("Your message wasn’t sent")
    expect(note.textContent).toContain("hello")
    expect(
      screen.getByRole("button", { name: "Copy your message" })
    ).toBeTruthy()

    // Sending something else keeps it.
    fireEvent.change(box(), { target: { value: "something else" } })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    expect(calls).toHaveLength(2)
    expect(screen.getByRole("alert").textContent).toContain("hello")
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }))
    expect(screen.queryByRole("alert")).toBeNull()
  })
})
