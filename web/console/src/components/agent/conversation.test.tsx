// @vitest-environment jsdom
/** A new chat whose run fails before the server names its thread has nothing
 * to hand over to: there are no rows to read back. The composer is released,
 * the error is shown, and what was typed comes back so a retry is one press.
 * A question another page handed over to be asked is sent once, on open.
 * A running turn shows Stop, and a thread that is still running when it is
 * opened is attached to, without the events its stored rows already hold. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { useState, type ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
}))

type ChatOpts = {
  thread?: string
  message: string
  onEvent: (event: AgentEvent) => void
  onError?: (error: Error) => void
  onDone?: () => void
}
const calls: ChatOpts[] = []
const attaches: (Omit<ChatOpts, "message"> & { thread: string })[] = []
const stops: string[] = []

vi.mock("@/lib/api/agents", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api/agents")>()
  return {
    ...actual,
    streamChat: (opts: ChatOpts) => {
      calls.push(opts)
      return { stop() {} }
    },
    attachThread: (opts: (typeof attaches)[number]) => {
      attaches.push(opts)
      return { stop() {} }
    },
    stopThread: (thread: string) => {
      stops.push(thread)
      return new Promise(() => {})
    },
  }
})

import type { AgentEvent } from "@/lib/api/agents"
import type { SubstrateRecord } from "@/lib/api/types"
import { Conversation } from "./conversation"

afterEach(() => {
  cleanup()
  calls.length = 0
  attaches.length = 0
  stops.length = 0
  vi.unstubAllGlobals()
})

function Harness({
  initialThread = "",
  initialDraft = "",
  autoSend = false,
  onAutoSent,
  threadRecord,
}: {
  initialThread?: string
  initialDraft?: string
  autoSend?: boolean
  onAutoSent?: () => void
  threadRecord?: SubstrateRecord
}) {
  const [draft, setDraft] = useState(initialDraft)
  const [thread, setThread] = useState(initialThread)
  return (
    <Conversation
      agentId="helper"
      thread={thread}
      title="New chat"
      onThread={setThread}
      draft={draft}
      onDraft={setDraft}
      autoSend={autoSend}
      onAutoSent={onAutoSent}
      threadRecord={threadRecord}
    />
  )
}

function mount(
  initialThread?: string,
  extra: Omit<Parameters<typeof Harness>[0], "initialThread"> = {}
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <Harness initialThread={initialThread} {...extra} />
    </QueryClientProvider>
  )
}

describe("Conversation", () => {
  it("releases the composer and keeps the message when a new chat fails before its thread", () => {
    mount()
    const box = screen.getByRole("textbox")
    fireEvent.change(box, { target: { value: "hello there" } })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    expect(calls).toHaveLength(1)

    act(() => calls[0].onError?.(new Error("dispatch refused")))

    expect(screen.getByText(/dispatch refused/)).toBeTruthy()
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(
      "hello there"
    )
    const send = screen.getByRole("button", { name: "Send" })
    expect((send as HTMLButtonElement).disabled).toBe(false)
    // The optimistic bubble is not left behind as if it had been sent.
    expect(
      screen.queryAllByText("hello there", { ignore: "textarea" })
    ).toHaveLength(0)

    fireEvent.click(send)
    expect(calls).toHaveLength(2)
    expect(calls[1].message).toBe("hello there")
  })

  it("says the finished run's messages did not reload, releases the composer, and retries", async () => {
    let fail = false
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        fail
          ? new Response(JSON.stringify({ error: { message: "db down" } }), {
              status: 500,
            })
          : new Response(JSON.stringify({ records: [] }), { status: 200 })
      )
    )
    mount("t-1")
    await act(() => new Promise((r) => setTimeout(r, 0)))

    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "hello" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    fail = true
    act(() => calls[0].onDone?.())

    const note = await screen.findByRole("alert")
    expect(note.textContent).toContain("db down")
    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "again" },
    })
    expect(
      screen.getByRole<HTMLButtonElement>("button", { name: "Send" }).disabled
    ).toBe(false)

    fail = false
    fireEvent.click(screen.getByRole("button", { name: "Try again" }))
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull())
  })

  it("reads a live tool call by the callable its event carries", () => {
    mount()
    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "any cups?" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    // `lookup` is the agent's alias; only the callable says it is the query
    // host function.
    act(() =>
      calls[0].onEvent({
        kind: "toolStarted",
        id: "c1",
        tool: "lookup",
        callable: "function:substrate.reamde.dev:core:query",
        args: '{"q":"cups"}',
      })
    )
    expect(screen.getByText("Searched for “cups”")).toBeTruthy()
  })

  it("sends a handed-over question once, on open", () => {
    const sent = vi.fn()
    const view = mount(undefined, {
      initialDraft: "Keep my recipes",
      autoSend: true,
      onAutoSent: sent,
    })
    expect(calls).toHaveLength(1)
    expect(calls[0].message).toBe("Keep my recipes")
    expect(sent).toHaveBeenCalledTimes(1)
    view.rerender(
      <QueryClientProvider client={new QueryClient()}>
        <Harness initialDraft="Keep my recipes" autoSend onAutoSent={sent} />
      </QueryClientProvider>
    )
    expect(calls).toHaveLength(1)
  })

  it("shows Stop while a turn streams and stops it on the server", () => {
    mount()
    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "go" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    act(() => calls[0].onEvent({ kind: "thread", thread: "t-9" }))
    const buttons = screen.getAllByRole("button", { name: /Stop/ })
    // The header's Stop and the composer's, which took the send button's place.
    expect(buttons).toHaveLength(2)
    expect(screen.queryByRole("button", { name: "Send" })).toBeNull()
    fireEvent.click(buttons[0])
    expect(stops).toEqual(["t-9"])
    expect(screen.getAllByRole("button", { name: /Stopping/ })).toHaveLength(2)
  })

  it("attaches to a thread that is still running and skips what its rows hold", async () => {
    const row = (id: string, turn: number, role: string, content: string) => ({
      id,
      kind: "substrate.reamde.dev/llm/message",
      properties: { turn, role, content },
      labels: {},
      version: 1,
      createdAt: "2026-10-17T10:00:00Z",
      updatedAt: "2026-10-17T10:00:00Z",
    })
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              records: [
                row("m1", 1, "assistant", "stored half"),
                row("m0", 0, "user", "question"),
              ],
            }),
            { status: 200 }
          )
      )
    )
    const running = {
      id: "t-1",
      kind: "substrate.reamde.dev/llm/thread",
      properties: { status: "running" },
      labels: {},
      version: 1,
      createdAt: "2026-10-17T10:00:00Z",
      updatedAt: "2026-10-17T10:00:00Z",
    } as unknown as SubstrateRecord
    mount("t-1", { threadRecord: running })
    await waitFor(() => expect(attaches).toHaveLength(1))
    expect(attaches[0].thread).toBe("t-1")
    act(() => {
      attaches[0].onEvent({ kind: "thread", thread: "t-1" })
      attaches[0].onEvent({ kind: "delta", text: "replayed", turn: 1 })
      attaches[0].onEvent({ kind: "delta", text: "fresh words", turn: 2 })
    })
    expect(screen.getByText("stored half")).toBeTruthy()
    expect(screen.getByText("fresh words")).toBeTruthy()
    expect(screen.queryByText(/replayed/)).toBeNull()
    expect(
      screen.getAllByRole("button", { name: /Stop/ }).length
    ).toBeGreaterThan(0)
  })

  it("says a turn no client can stream is working in the background", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(JSON.stringify({ records: [] }), { status: 200 })
      )
    )
    const running = {
      id: "t-2",
      kind: "substrate.reamde.dev/llm/thread",
      properties: { status: "running" },
      labels: {},
      version: 1,
      createdAt: "2026-10-17T10:00:00Z",
      updatedAt: "2026-10-17T10:00:00Z",
    } as unknown as SubstrateRecord
    mount("t-2", { threadRecord: running })
    await waitFor(() => expect(attaches).toHaveLength(1))
    act(() => {
      attaches[0].onEvent({ kind: "idle", thread: "t-2", status: "running" })
      attaches[0].onDone?.()
    })
    expect(screen.getByRole("status").textContent).toContain(
      "working on this chat in the background"
    )
    expect(screen.queryByRole("button", { name: /Stop/ })).toBeNull()
  })
})
