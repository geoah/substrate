// @vitest-environment jsdom
/** A new chat is new: once a new chat's run has named its thread, starting
 * another chat with the same agent opens a fresh conversation rather than the
 * one the first run adopted. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { NuqsTestingAdapter } from "nuqs/adapters/testing"
import { useState, type ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

let mounts = 0

vi.mock("@/components/agent/conversation", () => ({
  Conversation: (props: {
    thread: string
    onThread: (thread: string) => void
    actions?: ReactNode
  }) => {
    const [mount] = useState(() => ++mounts)
    return (
      <div data-testid="conversation" data-mount={mount}>
        <span data-testid="thread">{props.thread}</span>
        <button type="button" onClick={() => props.onThread("t-1")}>
          Mint
        </button>
        {props.actions}
      </div>
    )
  },
}))
vi.mock("@/components/agent/thread-list", () => ({ ThreadList: () => null }))
vi.mock("@/components/agent/agent-panel", () => ({ AgentPanel: () => null }))

import { AgentsPage } from "./agents"

beforeEach(() => {
  mounts = 0
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
      // The minted thread, read on its own once the address names it.
      if (String(url).endsWith("/llm/thread/t-1"))
        return new Response(
          JSON.stringify({
            id: "t-1",
            kind: "substrate.reamde.dev/llm/thread",
            properties: {
              agent: { ref: "substrate.reamde.dev/core/agent/helper" },
            },
            labels: {},
            version: 1,
            createdAt: "2026-09-25T10:00:00Z",
            updatedAt: "2026-09-25T10:00:00Z",
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

describe("AgentsPage", () => {
  it("opens a fresh conversation for a new chat after the last one adopted its thread", async () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <NuqsTestingAdapter searchParams="?agent=helper" hasMemory>
          <AgentsPage />
        </NuqsTestingAdapter>
      </QueryClientProvider>
    )

    const first = screen.getByTestId("conversation").dataset.mount
    await act(async () => fireEvent.click(screen.getByText("Mint")))
    // The run's thread is adopted without remounting the conversation.
    expect(screen.getByTestId("thread").textContent).toBe("t-1")
    expect(screen.getByTestId("conversation").dataset.mount).toBe(first)
    // The thread's own read lands, naming its agent: the same one.
    await act(() => new Promise((r) => setTimeout(r, 20)))

    await act(async () =>
      fireEvent.click(screen.getByRole("button", { name: /New chat/ }))
    )
    expect(screen.getByTestId("thread").textContent).toBe("")
    expect(screen.getByTestId("conversation").dataset.mount).not.toBe(first)
  })
})
