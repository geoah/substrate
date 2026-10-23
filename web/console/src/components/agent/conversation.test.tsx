// @vitest-environment jsdom
/** A new chat whose run fails before the server names its thread has nothing
 * to hand over to: there are no rows to read back. The composer is released,
 * the error is shown, and what was typed comes back so a retry is one press.
 * A question another page handed over to be asked is sent once, on open.
 * A running turn shows Stop, and a thread that is still running when it is
 * opened is attached to, without the events its stored rows already hold.
 * A send is never dropped: one made before the previous turn hands over is
 * held, shown and sent once it can, a re-read that never lands releases the
 * composer after RELOAD_WAIT_MS, and a refusal puts the message back. */

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
let stopAnswer: () => Promise<unknown> = () => new Promise(() => {})

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
      return stopAnswer()
    },
  }
})

import type { AgentEvent } from "@/lib/api/agents"
import { ApiError, type SubstrateRecord } from "@/lib/api/types"
import { TransportError } from "@/lib/api/agents"
import {
  Conversation,
  RECONCILE_DELAY_MS,
  RELOAD_WAIT_MS,
} from "./conversation"

afterEach(() => {
  cleanup()
  calls.length = 0
  attaches.length = 0
  stops.length = 0
  stopAnswer = () => new Promise(() => {})
  vi.unstubAllGlobals()
  vi.useRealTimers()
  sessionStorage.clear()
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
    act(() => {
      calls[0].onEvent({ kind: "thread", thread: "t-1" })
      calls[0].onEvent({
        kind: "done",
        result: {
          reply: "hi",
          thread: "t-1",
          status: "ok",
          effects: 0,
          turns: 1,
          toolCalls: 0,
          promptTokens: 1,
          completionTokens: 1,
          totalTokens: 2,
          costUSD: 0,
        },
      })
      calls[0].onDone?.()
    })

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

  it("keeps a streaming reply whose turn a resolution written meanwhile passed", async () => {
    stubRows([
      message("m2", 2, "system", "a decision"),
      message("m0", 0, "user", "question"),
    ])
    mount("t-3", { threadRecord: runningThread("t-3") })
    await waitFor(() => expect(attaches).toHaveLength(1))
    act(() => {
      attaches[0].onEvent({ kind: "thread", thread: "t-3" })
      attaches[0].onEvent({ kind: "delta", text: "the live reply", turn: 1 })
    })
    expect(screen.getByText("the live reply")).toBeTruthy()
  })

  it("attaches again when a stream ends without done", async () => {
    stubRows([message("m0", 0, "user", "question")])
    mount("t-4", { threadRecord: runningThread("t-4") })
    await waitFor(() => expect(attaches).toHaveLength(1))
    act(() => {
      attaches[0].onEvent({ kind: "thread", thread: "t-4" })
      attaches[0].onDone?.()
    })
    await waitFor(() => expect(attaches).toHaveLength(2), { timeout: 3000 })
    expect(attaches[1].thread).toBe("t-4")
    // A dropped connection is not shown as the turn's failure.
    act(() => attaches[1].onError?.(new Error("network down")))
    expect(screen.queryByText(/network down/)).toBeNull()
  })

  it("says nothing when a stop loses the race to the turn's own end", async () => {
    stopAnswer = () =>
      Promise.reject(new ApiError("conflict", "no running chat turn", 409))
    mount()
    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "go" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    act(() => calls[0].onEvent({ kind: "thread", thread: "t-5" }))
    fireEvent.click(screen.getAllByRole("button", { name: /Stop/ })[0])
    await act(() => new Promise((r) => setTimeout(r, 0)))
    expect(stops).toEqual(["t-5"])
    expect(screen.queryByText(/no running chat turn/)).toBeNull()
  })

  it("stops attaching when the thread is gone, and says so", async () => {
    stubRows([message("m0", 0, "user", "question")])
    mount("t-6", { threadRecord: runningThread("t-6") })
    await waitFor(() => expect(attaches).toHaveLength(1))
    act(() =>
      attaches[0].onError?.(
        new ApiError("not_found", "substrate: not found: thread t-6", 404)
      )
    )
    expect(screen.getByText(/thread t-6/)).toBeTruthy()
    await act(() => new Promise((r) => setTimeout(r, 1200)))
    expect(attaches).toHaveLength(1)
    expect(
      screen.getByRole<HTMLButtonElement>("button", { name: "Send" })
    ).toBeTruthy()
  })

  it("gives the message back when a turn is refused before it starts", async () => {
    stubRows([message("m0", 0, "user", "question")])
    mount("t-7")
    await act(() => new Promise((r) => setTimeout(r, 0)))
    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "one more thing" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    const refusal =
      "substrate: version conflict: 4 chat turns are already running in this repository: wait for one to finish, or stop one"
    act(() => calls[0].onError?.(new ApiError("conflict", refusal, 409)))
    expect(screen.getByRole("alert").textContent).toContain(
      "Your message wasn’t sent"
    )
    expect(screen.getByText(/4 chat turns are already running/)).toBeTruthy()
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(
      "one more thing"
    )
    await act(() => new Promise((r) => setTimeout(r, 50)))
    expect(attaches).toHaveLength(0)
  })
})

describe("a send after a turn", () => {
  const settled = {
    reply: "hi",
    thread: "t-1",
    status: "ok",
    effects: 0,
    turns: 1,
    toolCalls: 0,
    promptTokens: 1,
    completionTokens: 1,
    totalTokens: 2,
    costUSD: 0,
  }

  /** Mounts thread t-1, sends one message and settles its turn. Every
   * records read after the settle answers with `reread`. */
  async function settleOneTurn(reread: () => Promise<Response>) {
    let settledYet = false
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        settledYet
          ? reread()
          : new Response(JSON.stringify({ records: [] }), { status: 200 })
      )
    )
    mount("t-1")
    await act(() => new Promise((r) => setTimeout(r, 0)))
    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "first" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    settledYet = true
    act(() => {
      calls[0].onEvent({ kind: "thread", thread: "t-1" })
      calls[0].onEvent({ kind: "delta", text: "the reply" })
      calls[0].onEvent({ kind: "done", result: settled })
      calls[0].onDone?.()
    })
  }

  function type(text: string) {
    fireEvent.change(screen.getByRole("textbox"), { target: { value: text } })
  }

  it("holds a send made before the re-read lands, shows it, and sends it after", async () => {
    let land: (r: Response) => void = () => {}
    await settleOneTurn(() => new Promise<Response>((r) => (land = r)))
    type("second")
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    // Not dropped: held, shown, and out of the composer.
    expect(calls).toHaveLength(1)
    expect(screen.getByText(/Sends when/).textContent).toContain("second")
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe("")

    await act(async () => {
      land(new Response(JSON.stringify({ records: [] }), { status: 200 }))
      await new Promise((r) => setTimeout(r, 10))
    })
    await waitFor(() => expect(calls).toHaveLength(2))
    expect(calls[1].message).toBe("second")
    expect(calls[1].thread).toBe("t-1")
    expect(screen.queryByText(/Sends when/)).toBeNull()
  })

  it("holds a send made by Enter too, and gives a held message back on request", async () => {
    await settleOneTurn(() => new Promise<Response>(() => {}))
    type("second")
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter" })
    expect(screen.getByText(/Sends when/).textContent).toContain("second")
    fireEvent.click(screen.getByRole("button", { name: "Don’t send" }))
    expect(screen.queryByText(/Sends when/)).toBeNull()
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(
      "second"
    )
    expect(calls).toHaveLength(1)
  })

  it("releases the composer when the re-read never comes back", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    await settleOneTurn(() => new Promise<Response>(() => {}))
    await act(async () => {
      vi.advanceTimersByTime(RELOAD_WAIT_MS)
      await Promise.resolve()
    })
    const note = await screen.findByRole("alert")
    expect(note.textContent).toContain("has not come back")
    expect(
      screen.getByRole<HTMLButtonElement>("button", { name: "Try again" })
        .disabled
    ).toBe(false)
    type("second")
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    expect(calls).toHaveLength(2)
    expect(calls[1].message).toBe("second")
    // The turn whose rows never arrived is still on screen.
    expect(screen.getByText("the reply")).toBeTruthy()
  })

  it("sends a held message once the re-read is given up on", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    await settleOneTurn(() => new Promise<Response>(() => {}))
    type("second")
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    expect(calls).toHaveLength(1)
    await act(async () => {
      vi.advanceTimersByTime(RELOAD_WAIT_MS + 10)
      await Promise.resolve()
    })
    await waitFor(() => expect(calls).toHaveLength(2))
    expect(calls[1].message).toBe("second")
    expect(screen.getByText("the reply")).toBeTruthy()
  })

  it("sends what was held once the re-read fails", async () => {
    await settleOneTurn(
      async () =>
        new Response(JSON.stringify({ error: { message: "db down" } }), {
          status: 500,
        })
    )
    type("second")
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    await waitFor(() => expect(calls).toHaveLength(2))
    expect(calls[1].message).toBe("second")
  })

  it("puts a refused message back ahead of what was typed since", async () => {
    const rows = [
      message("m1", 1, "assistant", "the stored reply"),
      message("m0", 0, "user", "first"),
    ]
    await settleOneTurn(
      async () =>
        new Response(JSON.stringify({ records: rows }), { status: 200 })
    )
    await waitFor(() =>
      expect(
        screen.getByRole<HTMLButtonElement>("button", { name: "Send" })
      ).toBeTruthy()
    )
    type("second")
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    await waitFor(() => expect(calls).toHaveLength(2))
    type("third")
    act(() =>
      calls[1].onError?.(
        new ApiError("conflict", "a chat turn is already running", 409)
      )
    )
    expect(screen.getByRole("alert").textContent).toContain(
      "a chat turn is already running"
    )
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(
      "second\n\nthird"
    )
    // The settled turn is still on screen, not wiped by the refusal.
    expect(screen.getByText("the stored reply")).toBeTruthy()
  })

  it("keeps what was typed after a held message when the held message goes", async () => {
    let land: (r: Response) => void = () => {}
    await settleOneTurn(() => new Promise<Response>((r) => (land = r)))
    type("second")
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    type("third")
    await act(async () => {
      land(new Response(JSON.stringify({ records: [] }), { status: 200 }))
      await new Promise((r) => setTimeout(r, 10))
    })
    await waitFor(() => expect(calls).toHaveLength(2))
    expect(calls[1].message).toBe("second")
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe("third")
  })

  it("keeps a held message across leaving the chat and coming back", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(JSON.stringify({ records: [] })))
    )
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const view = render(
      <QueryClientProvider client={client}>
        <Keyed thread="t-1" />
      </QueryClientProvider>
    )
    await act(() => new Promise((r) => setTimeout(r, 0)))
    type("first")
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    act(() => calls[0].onEvent({ kind: "thread", thread: "t-1" }))
    // The turn is still running: this one is held.
    type("second")
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter" })
    expect(screen.getByText(/Sends when/).textContent).toContain("second")

    view.rerender(
      <QueryClientProvider client={client}>
        <Keyed thread="t-2" />
      </QueryClientProvider>
    )
    expect(screen.queryByText(/Sends when/)).toBeNull()
    view.rerender(
      <QueryClientProvider client={client}>
        <Keyed thread="t-1" />
      </QueryClientProvider>
    )
    await waitFor(() => expect(calls).toHaveLength(2))
    expect(calls[1].message).toBe("second")
    expect(calls[1].thread).toBe("t-1")
  })

  it("starts a fresh read when Try again meets a new chat's stuck first read", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const fetches = vi.fn(() => new Promise<Response>(() => {}))
    vi.stubGlobal("fetch", fetches)
    mount()
    type("hello")
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    act(() => {
      calls[0].onEvent({ kind: "thread", thread: "t-new" })
      calls[0].onEvent({ kind: "done", result: settled })
      calls[0].onDone?.()
    })
    await act(async () => {
      vi.advanceTimersByTime(RELOAD_WAIT_MS)
      await Promise.resolve()
    })
    await screen.findByRole("alert")
    const before = fetches.mock.calls.length
    expect(before).toBeGreaterThan(0)
    fireEvent.click(screen.getByRole("button", { name: "Try again" }))
    await waitFor(() =>
      expect(fetches.mock.calls.length).toBeGreaterThan(before)
    )
  })
})

describe("a late transcript read", () => {
  const first = [
    message("m1", 1, "assistant", "the reply"),
    message("m0", 0, "user", "first"),
  ]
  const both = [
    message("m3", 3, "assistant", "reply two"),
    message("m2", 2, "user", "second"),
    ...first,
  ]
  const result = {
    reply: "",
    thread: "t-1",
    status: "ok",
    effects: 0,
    turns: 1,
    toolCalls: 0,
    promptTokens: 1,
    completionTokens: 1,
    totalTokens: 2,
    costUSD: 0,
  }

  /** Every records read waits until the test answers it, by its index. */
  function deferredReads() {
    const reads: ((rows: unknown[]) => void)[] = []
    vi.stubGlobal(
      "fetch",
      vi.fn(
        () =>
          new Promise<Response>((resolve) => {
            reads.push((rows) =>
              resolve(new Response(JSON.stringify({ records: rows })))
            )
          })
      )
    )
    return reads
  }

  async function answer(read: (rows: unknown[]) => void, rows: unknown[]) {
    await act(async () => {
      read(rows)
      await new Promise((r) => setTimeout(r, 10))
    })
  }

  /** Turn one settles, its read hangs past the wait, turn two starts. */
  async function turnTwoAfterAStuckRead() {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const reads = deferredReads()
    mount("t-1")
    await waitFor(() => expect(reads).toHaveLength(1))
    await answer(reads[0], [])
    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "first" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    act(() => {
      calls[0].onEvent({ kind: "thread", thread: "t-1" })
      calls[0].onEvent({ kind: "delta", text: "the reply" })
      calls[0].onEvent({ kind: "done", result })
      calls[0].onDone?.()
    })
    await waitFor(() => expect(reads).toHaveLength(2))
    await act(async () => {
      vi.advanceTimersByTime(RELOAD_WAIT_MS)
      await Promise.resolve()
    })
    await screen.findByRole("alert")
    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "second" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    expect(calls).toHaveLength(2)
    act(() => {
      calls[1].onEvent({ kind: "thread", thread: "t-1" })
      calls[1].onEvent({ kind: "delta", text: "reply two" })
    })
    return reads
  }

  function finishTurnTwo() {
    act(() => {
      calls[1].onEvent({ kind: "done", result })
      calls[1].onDone?.()
    })
  }

  it("does not double turn one when it lands during turn two", async () => {
    const reads = await turnTwoAfterAStuckRead()
    await answer(reads[1], first)
    expect(screen.getAllByText("the reply")).toHaveLength(1)
    finishTurnTwo()
    await waitFor(() => expect(reads).toHaveLength(3))
    await answer(reads[2], both)
    expect(screen.getAllByText("the reply")).toHaveLength(1)
    expect(screen.getAllByText("reply two")).toHaveLength(1)
  })

  it("does not hide turn two when it lands after turn two settles", async () => {
    const reads = await turnTwoAfterAStuckRead()
    finishTurnTwo()
    await waitFor(() => expect(reads).toHaveLength(3))
    await answer(reads[1], first)
    expect(screen.getAllByText("reply two")).toHaveLength(1)
    await answer(reads[2], both)
    expect(screen.getAllByText("reply two")).toHaveLength(1)
    expect(screen.getAllByText("the reply")).toHaveLength(1)
  })
})

describe("a send whose delivery is unknown", () => {
  let rows: ReturnType<typeof message>[] = []
  function stubLiveRows() {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(JSON.stringify({ records: rows }), { status: 200 })
      )
    )
  }

  async function sendThenLoseTheConnection(fail: () => void) {
    rows = [message("m0", 0, "user", "question")]
    stubLiveRows()
    mount("t-8")
    await act(() => new Promise((r) => setTimeout(r, 0)))
    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "one more thing" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    act(fail)
    // Not "wasn't sent", and nothing offered for a resend yet.
    expect(screen.getByRole("alert").textContent).toContain(
      "Checking whether it reached the chat"
    )
    expect(screen.getByRole("alert").textContent).not.toContain("wasn’t sent")
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe("")
  }

  const dropped = () =>
    calls[0].onError?.(
      new TransportError(
        "the connection failed before the server answered (Load failed)"
      )
    )

  it("gives the message back once the thread shows it never arrived", async () => {
    await sendThenLoseTheConnection(dropped)
    await waitFor(
      () =>
        expect(screen.getByRole("alert").textContent).toContain(
          "It isn’t in this chat"
        ),
      { timeout: RECONCILE_DELAY_MS + 2000 }
    )
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(
      "one more thing"
    )
  })

  it("offers no resend when the thread shows the message arrived", async () => {
    await sendThenLoseTheConnection(dropped)
    rows = [message("m1", 1, "user", "one more thing"), ...rows]
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull(), {
      timeout: RECONCILE_DELAY_MS + 2000,
    })
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe("")
    expect(calls).toHaveLength(1)
  })

  it("checks the thread when the stream closes before any event", async () => {
    await sendThenLoseTheConnection(() => calls[0].onDone?.())
    await waitFor(
      () =>
        expect(screen.getByRole("alert").textContent).toContain(
          "It isn’t in this chat"
        ),
      { timeout: RECONCILE_DELAY_MS + 2000 }
    )
  })

  it("points a new chat at the chat list before a resend", () => {
    mount()
    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "hello" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    act(dropped)
    expect(screen.getByRole("alert").textContent).toContain(
      "look for it in the chat list"
    )
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe("hello")
  })
})

/** A conversation the page remounts by thread, as the agents page does. */
function Keyed({ thread }: { thread: string }) {
  const [draft, setDraft] = useState("")
  return (
    <Conversation
      key={thread}
      agentId="helper"
      thread={thread}
      title="Chat"
      onThread={() => {}}
      draft={draft}
      onDraft={setDraft}
    />
  )
}

function message(id: string, turn: number, role: string, content: string) {
  return {
    id,
    kind: "substrate.reamde.dev/llm/message",
    properties: { turn, role, content },
    labels: {},
    version: 1,
    createdAt: "2026-10-17T10:00:00Z",
    updatedAt: "2026-10-17T10:00:00Z",
  }
}

/** Every records read answers these rows, newest first. */
function stubRows(records: ReturnType<typeof message>[]) {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () => new Response(JSON.stringify({ records }), { status: 200 })
    )
  )
}

function runningThread(id: string): SubstrateRecord {
  return {
    id,
    kind: "substrate.reamde.dev/llm/thread",
    properties: { status: "running" },
    labels: {},
    version: 1,
    createdAt: "2026-10-17T10:00:00Z",
    updatedAt: "2026-10-17T10:00:00Z",
  }
}
