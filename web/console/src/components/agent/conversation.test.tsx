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
import { resetUnsent } from "@/lib/unsent-messages"
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
  resetUnsent()
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

  it("blocks a send before the re-read lands, says why, and sends nothing later", async () => {
    let land: (r: Response) => void = () => {}
    await settleOneTurn(() => new Promise<Response>((r) => (land = r)))
    type("second")
    expect(
      screen.getByRole<HTMLButtonElement>("button", { name: "Send" }).disabled
    ).toBe(true)
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter" })
    expect(calls).toHaveLength(1)
    expect(
      screen.getByText("Waiting for the previous turn to finish")
    ).toBeTruthy()
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(
      "second"
    )

    await act(async () => {
      land(new Response(JSON.stringify({ records: [] }), { status: 200 }))
      await new Promise((r) => setTimeout(r, 20))
    })
    await waitFor(() =>
      expect(
        screen.getByRole<HTMLButtonElement>("button", { name: "Send" }).disabled
      ).toBe(false)
    )
    // Released, not sent on its own.
    expect(calls).toHaveLength(1)
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(
      "second"
    )
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    expect(calls).toHaveLength(2)
    expect(calls[1].message).toBe("second")
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

  it("releases the composer once the re-read fails, and sends nothing on its own", async () => {
    await settleOneTurn(
      async () =>
        new Response(JSON.stringify({ error: { message: "db down" } }), {
          status: 500,
        })
    )
    type("second")
    await waitFor(() =>
      expect(
        screen.getByRole<HTMLButtonElement>("button", { name: "Send" }).disabled
      ).toBe(false)
    )
    expect(calls).toHaveLength(1)
  })

  it("shows a refused message with a copy action when something was typed since", async () => {
    const rows = [
      message("m1", 1, "assistant", "the stored reply"),
      message("m0", 0, "user", "first"),
    ]
    await settleOneTurn(
      async () =>
        new Response(JSON.stringify({ records: rows }), { status: 200 })
    )
    type("second")
    await waitFor(() =>
      expect(
        screen.getByRole<HTMLButtonElement>("button", { name: "Send" }).disabled
      ).toBe(false)
    )
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    expect(calls).toHaveLength(2)
    type("third")
    act(() =>
      calls[1].onError?.(
        new ApiError("conflict", "a chat turn is already running", 409)
      )
    )
    const note = screen.getByRole("alert")
    expect(note.textContent).toContain("Your message wasn’t sent")
    expect(note.textContent).toContain("a chat turn is already running")
    expect(note.textContent).toContain("second")
    expect(
      screen.getByRole("button", { name: "Copy your message" })
    ).toBeTruthy()
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe("third")
    // The settled turn is still on screen, not wiped by the refusal.
    expect(screen.getByText("the stored reply")).toBeTruthy()

    // Sending the next message keeps the refused one recoverable.
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    expect(calls).toHaveLength(3)
    expect(calls[2].message).toBe("third")
    const kept = screen.getByRole("alert")
    expect(kept.textContent).toContain("second")
    expect(
      screen.getByRole("button", { name: "Copy your message" })
    ).toBeTruthy()
    // Until it is dismissed.
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }))
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("sends nothing after a reload during a busy turn", async () => {
    stubRows([message("m0", 0, "user", "question")])
    const view = mount("t-1", { threadRecord: runningThread("t-1") })
    await waitFor(() => expect(attaches).toHaveLength(1))
    act(() => attaches[0].onEvent({ kind: "thread", thread: "t-1" }))
    type("next")
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter" })
    expect(
      screen.getByText("Waiting for the previous turn to finish")
    ).toBeTruthy()
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe("next")
    view.unmount()
    mount("t-1", { threadRecord: runningThread("t-1") })
    await waitFor(() => expect(attaches).toHaveLength(2))
    await act(() => new Promise((r) => setTimeout(r, 50)))
    expect(calls).toHaveLength(0)
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
  // The thread record's status, or "fail" for a read the server refuses.
  let threadStatus = "ok"
  // The next transcript list read answers what the thread held when it was
  // asked, but only after the delivery check has started.
  let slowNext = false
  function stubLiveRows() {
    threadStatus = "ok"
    slowNext = false
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        if (String(input).includes("/llm/thread/")) {
          if (threadStatus === "fail") {
            return new Response(
              JSON.stringify({
                error: { code: "internal", message: "db down" },
              }),
              { status: 500 }
            )
          }
          const record = runningThread("t-8")
          record.properties = { status: threadStatus }
          return new Response(JSON.stringify(record), { status: 200 })
        }
        const snapshot = rows
        if (slowNext) {
          slowNext = false
          await new Promise((r) => setTimeout(r, RECONCILE_DELAY_MS + 500))
        }
        return new Response(JSON.stringify({ records: snapshot }), {
          status: 200,
        })
      })
    )
  }
  const lost = (reason = "the connection failed before the server answered") =>
    calls[0].onError?.(new TransportError(reason))

  async function sendThenLose(
    text: string,
    fail: () => void,
    initial = [
      message("m1", 1, "assistant", "an answer"),
      message("m0", 0, "user", "question"),
    ]
  ) {
    rows = initial
    stubLiveRows()
    mount("t-8")
    await waitFor(() => expect(screen.getByText("an answer")).toBeTruthy())
    type(text)
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    act(fail)
    const note = screen.getByRole("alert")
    expect(note.textContent).toContain("Checking whether it arrived")
    expect(note.textContent).not.toContain("wasn’t sent")
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe("")
  }

  const settledCheck = { timeout: RECONCILE_DELAY_MS + 2000 }
  const MAYBE = "may not have arrived"

  /** Waits for the check to end with the message still unaccounted for:
   * kept to copy, never put back, never called missing. */
  async function expectStillUnknown(text: string) {
    await waitFor(
      () => expect(screen.getByRole("alert").textContent).toContain(MAYBE),
      settledCheck
    )
    const note = screen.getByRole("alert")
    expect(note.textContent).toContain(text)
    expect(note.textContent).not.toContain("didn’t arrive")
    expect(
      screen.getByRole("button", { name: "Copy your message" })
    ).toBeTruthy()
  }

  it("keeps the message to copy when the thread does not hold it", async () => {
    await sendThenLose("one more thing", () => lost())
    await expectStillUnknown("one more thing")
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe("")
  })

  it("says nothing failed when admission comes after the check began", async () => {
    await sendThenLose("one more thing", () => lost())
    await expectStillUnknown("one more thing")
    // The request was still waiting for admission when the thread was read.
    rows = [message("m2", 2, "user", "one more thing"), ...rows]
    threadStatus = "running"
    await act(() => new Promise((r) => setTimeout(r, 100)))
    expect(screen.queryByText(/didn’t arrive/)).toBeNull()
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe("")
  })

  it("blocks a second send during the check and says why", async () => {
    await sendThenLose("one more thing", () => lost())
    type("another")
    expect(
      screen.getByText("Checking whether your last message arrived")
    ).toBeTruthy()
    expect(
      screen.getByRole<HTMLButtonElement>("button", { name: "Send" }).disabled
    ).toBe(true)
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter" })
    expect(calls).toHaveLength(1)
    await expectStillUnknown("one more thing")
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(
      "another"
    )
  })

  it("does not take an identical earlier prompt for the lost one", async () => {
    await sendThenLose("continue", () => lost(), [
      message("m1", 1, "assistant", "an answer"),
      message("m0", 0, "user", "continue"),
    ])
    await expectStillUnknown("continue")
  })

  it("does not take another tab's newer message for the lost one", async () => {
    await sendThenLose("one more thing", () => lost())
    rows = [message("m2", 2, "user", "from another tab"), ...rows]
    await expectStillUnknown("one more thing")
  })

  it("clears the notice after a 504 when the thread holds the message", async () => {
    await sendThenLose("one more thing", () =>
      lost("the request ended with HTTP 504 and no answer from the server")
    )
    rows = [message("m2", 2, "user", "one more thing"), ...rows]
    await waitFor(
      () => expect(screen.queryByRole("alert")).toBeNull(),
      settledCheck
    )
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe("")
    expect(calls).toHaveLength(1)
  })

  it("attaches to the turn the check finds running, with no running record given", async () => {
    await sendThenLose("one more thing", () => lost())
    rows = [message("m2", 2, "user", "one more thing"), ...rows]
    threadStatus = "running"
    await waitFor(() => expect(attaches).toHaveLength(1), settledCheck)
    expect(attaches[0].thread).toBe("t-8")
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("keeps the message to copy when the check's read fails", async () => {
    await sendThenLose("one more thing", () => lost())
    threadStatus = "fail"
    await expectStillUnknown("one more thing")
  })

  it("does not trust a transcript read that started before the message landed", async () => {
    await sendThenLose("one more thing", () => {
      slowNext = true
      lost()
    })
    // The message lands after the slow read took its snapshot.
    rows = [message("m2", 2, "user", "one more thing"), ...rows]
    await waitFor(
      () => expect(screen.queryByRole("alert")).toBeNull(),
      settledCheck
    )
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe("")
  })

  it("does not draw an attached turn's stored rows over its live ones", async () => {
    rows = [
      message("m1", 1, "assistant", "an answer"),
      message("m0", 0, "user", "question"),
    ]
    stubLiveRows()
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const tree = (record?: SubstrateRecord) => (
      <QueryClientProvider client={client}>
        <Harness initialThread="t-8" threadRecord={record} />
      </QueryClientProvider>
    )
    const view = render(tree())
    await waitFor(() => expect(screen.getByText("an answer")).toBeTruthy())
    type("one more thing")
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    act(() => lost())
    // The message landed and its turn runs: the page attaches to it.
    rows = [message("m2", 2, "user", "one more thing"), ...rows]
    threadStatus = "running"
    view.rerender(tree(runningThread("t-8")))
    await waitFor(() => expect(attaches).toHaveLength(1))
    act(() => {
      attaches[0].onEvent({ kind: "thread", thread: "t-8" })
      attaches[0].onEvent({ kind: "delta", text: "live part", turn: 3 })
    })
    // The turn stores its reply so far while the check is waiting.
    rows = [message("m3", 3, "assistant", "live part"), ...rows]
    await act(() => new Promise((r) => setTimeout(r, RECONCILE_DELAY_MS + 500)))
    expect(screen.getAllByText("live part")).toHaveLength(1)
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("checks the thread when the stream closes before any event", async () => {
    await sendThenLose("one more thing", () => calls[0].onDone?.())
    await expectStillUnknown("one more thing")
  })

  it("leaves the next chat's draft alone when the chat is left during a check", async () => {
    rows = [message("m0", 0, "user", "question")]
    stubLiveRows()
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const view = render(
      <QueryClientProvider client={client}>
        <Keyed thread="t-8" />
      </QueryClientProvider>
    )
    await waitFor(() => expect(screen.getByText("question")).toBeTruthy())
    type("lost one")
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    act(() => lost())
    view.rerender(
      <QueryClientProvider client={client}>
        <Keyed thread="t-9" />
      </QueryClientProvider>
    )
    type("for the other chat")
    await act(() => new Promise((r) => setTimeout(r, RECONCILE_DELAY_MS + 300)))
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(
      "for the other chat"
    )
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("keeps an unsent message across leaving a chat and coming back", async () => {
    rows = []
    stubLiveRows()
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const at = (thread: string) => (
      <QueryClientProvider client={client}>
        <Keyed thread={thread} />
      </QueryClientProvider>
    )
    // A new chat: no thread to check, so only the notice holds the text.
    const view = render(at(""))
    type("hello")
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    act(() => lost())
    expect(screen.getByRole("alert").textContent).toContain("hello")
    view.rerender(at("t-9"))
    expect(screen.queryByRole("alert")).toBeNull()
    view.rerender(at(""))
    const note = screen.getByRole("alert")
    expect(note.textContent).toContain(MAYBE)
    expect(note.textContent).toContain("hello")
    expect(
      screen.getByRole("button", { name: "Copy your message" })
    ).toBeTruthy()
  })

  it("points a new chat at the chat list and shows the message to copy", () => {
    mount()
    type("hello")
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    act(() => lost())
    const note = screen.getByRole("alert")
    expect(note.textContent).toContain("chat list")
    expect(note.textContent).toContain("hello")
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe("")
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

function type(text: string) {
  fireEvent.change(screen.getByRole("textbox"), { target: { value: text } })
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
