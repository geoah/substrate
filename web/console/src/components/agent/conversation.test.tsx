// @vitest-environment jsdom
/** A new chat whose run fails before the server names its thread has nothing
 * to hand over to: there are no rows to read back. The composer is released,
 * the error is shown, and what was typed comes back so a retry is one press.
 * A question another page handed over to be asked is sent once, on open.
 * A running turn shows Stop, and a thread that is still running when it is
 * opened is attached to, without the events its stored rows already hold.
 * A send is never dropped: one made before the previous turn hands over is
 * held, shown and sent once it can, a re-read that never lands releases the
 * composer after RELOAD_WAIT_MS, and a refusal puts the message back. A send
 * goes under an id of its own and an unanswered one is sent again under it;
 * Continue and Try again continue the thread and send no message. */

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
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
}))

type ChatOpts = {
  agent: string
  thread?: string
  message: string
  messageId?: string
  onEvent: (event: AgentEvent) => void
  onError?: (error: Error) => void
  onDone?: () => void
}
const calls: ChatOpts[] = []
type ThreadOpts = Omit<ChatOpts, "agent" | "message" | "messageId"> & {
  thread: string
}
const attaches: ThreadOpts[] = []
const continues: ThreadOpts[] = []
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
    attachThread: (opts: ThreadOpts) => {
      attaches.push(opts)
      return { stop() {} }
    },
    continueThread: (opts: ThreadOpts) => {
      continues.push(opts)
      return { stop() {} }
    },
    stopThread: (thread: string) => {
      stops.push(thread)
      return stopAnswer()
    },
  }
})

// The revision of the `agents` feature the server under test says it has.
let agentsRevision = 3
vi.mock("@/lib/api/discovery", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api/discovery")>()
  const doc = () => ({
    features: [{ name: "agents", revision: agentsRevision }],
  })
  return {
    ...actual,
    discoveryQueryOptions: {
      queryKey: ["discovery", "server"],
      queryFn: () => Promise.resolve(doc()),
      initialData: doc,
    },
  }
})

import {
  agentRunsQueryOptions,
  conversationsQueryOptions,
  lastChatQueryOptions,
  threadMessagesQueryOptions,
  type AgentEvent,
} from "@/lib/api/agents"
import { ApiError, type SubstrateRecord } from "@/lib/api/types"
import { TransportError } from "@/lib/api/agents"
import {
  ID_TAKEN_REFUSAL,
  RESEND_DELAYS_MS,
  resetUnsent,
} from "@/lib/unsent-messages"
import { Conversation, RELOAD_WAIT_MS } from "./conversation"

// The waits before an unanswered send goes again, shortened for the tests.
const RESEND_WAITS = [...RESEND_DELAYS_MS]
beforeEach(() => {
  RESEND_DELAYS_MS.splice(0, RESEND_DELAYS_MS.length, 20, 20, 20)
})

afterEach(() => {
  RESEND_DELAYS_MS.splice(0, RESEND_DELAYS_MS.length, ...RESEND_WAITS)
  cleanup()
  calls.length = 0
  attaches.length = 0
  continues.length = 0
  agentsRevision = 3
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
  keylessProvider,
}: {
  initialThread?: string
  initialDraft?: string
  autoSend?: boolean
  onAutoSent?: () => void
  threadRecord?: SubstrateRecord
  keylessProvider?: string
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
      keylessProvider={keylessProvider}
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
  it("puts a new chat in the chat lists as soon as its run names the thread", () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const empty = { records: [], head: 0, generation: "g" }
    const lists = [
      conversationsQueryOptions().queryKey,
      lastChatQueryOptions().queryKey,
      agentRunsQueryOptions("helper").queryKey,
    ]
    const transcript = threadMessagesQueryOptions("t-0").queryKey
    for (const key of [...lists, transcript]) client.setQueryData(key, empty)
    render(
      <QueryClientProvider client={client}>
        <Harness />
      </QueryClientProvider>
    )
    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "can you make a new kind?" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    for (const key of lists)
      expect(client.getQueryState(key)?.isInvalidated).toBe(false)

    act(() => calls[0].onEvent({ kind: "thread", thread: "t-1" }))

    // Mid-run, before any settle: the lists re-read and find the thread
    // running, and no transcript is touched while the stream shows it.
    for (const key of lists)
      expect(client.getQueryState(key)?.isInvalidated).toBe(true)
    expect(client.getQueryState(transcript)?.isInvalidated).toBe(false)
  })

  it("releases the composer and keeps the message when a new chat fails before its thread", () => {
    mount()
    const box = screen.getByRole("textbox")
    fireEvent.change(box, { target: { value: "hello there" } })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    expect(calls).toHaveLength(1)

    act(() =>
      calls[0].onError?.(new ApiError("guard", "dispatch refused", 403))
    )

    expect(screen.getByText(/dispatch refused/)).toBeTruthy()
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(
      "hello there"
    )
    const send = screen.getByRole("button", { name: "Send" })
    expect((send as HTMLButtonElement).disabled).toBe(false)
    // The optimistic bubble is not left behind as if it had been sent: the
    // only copy outside the composer is the notice's.
    expect(
      screen
        .queryAllByText("hello there", { ignore: "textarea" })
        .filter((el) => !el.closest("[role=alert]"))
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
        callable:
          "substrate.reamde.dev/core/function/substrate.reamde.dev/core/query",
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

  it("says why the last turn didn't finish and continues it, leaving the draft alone", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(JSON.stringify({ records: [] }), { status: 200 })
      )
    )
    const cut = runningThread("t-9")
    cut.properties = {
      status: "stopped",
      reason: "stopped: the server shut down during the turn",
    }
    mount("t-9", { threadRecord: cut, initialDraft: "half a thought" })
    await act(() => new Promise((r) => setTimeout(r, 0)))
    expect(screen.getByText("Helper didn’t finish")).toBeTruthy()
    expect(
      screen.getByText("The server restarted while it was working.")
    ).toBeTruthy()

    fireEvent.click(screen.getByRole("button", { name: "Continue" }))
    // The thread is continued: no message is sent, so none is written.
    expect(calls).toHaveLength(0)
    expect(continues).toHaveLength(1)
    expect(continues[0].thread).toBe("t-9")
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(
      "half a thought"
    )
    // The turn it started is running: the note is gone.
    expect(screen.queryByText("Helper didn’t finish")).toBeNull()
    // And it streams as a sent turn does, with no user message above it.
    act(() => {
      continues[0].onEvent({ kind: "thread", thread: "t-9" })
      continues[0].onEvent({ kind: "delta", text: "and the rest", turn: 4 })
    })
    expect(screen.getByText("and the rest")).toBeTruthy()
    expect(screen.getAllByRole("button", { name: "Stop" })).not.toHaveLength(0)
  })

  it("offers Try again on a failed turn, which continues the thread", async () => {
    stubRows([message("m0", 0, "user", "question")])
    const failed = runningThread("t-13")
    failed.properties = { status: "error", reason: "llm: overloaded" }
    mount("t-13", { threadRecord: failed })
    await waitFor(() => expect(screen.getByText("question")).toBeTruthy())
    expect(screen.queryByRole("button", { name: "Continue" })).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Try again" }))
    expect(continues.map((c) => c.thread)).toEqual(["t-13"])
    expect(calls).toHaveLength(0)
    // The question is not written a second time.
    expect(screen.getAllByText("question")).toHaveLength(1)
  })

  it("attaches to the turn when a continue finds the thread busy", async () => {
    stubRows([message("m0", 0, "user", "question")])
    const cut = runningThread("t-14")
    cut.properties = { status: "stopped", reason: "stopped by its owner" }
    mount("t-14", { threadRecord: cut })
    await waitFor(() => expect(screen.getByText("question")).toBeTruthy())
    fireEvent.click(screen.getByRole("button", { name: "Continue" }))
    act(() =>
      continues[0].onError?.(
        new ApiError(
          "conflict",
          "thread t-14 already has a running turn",
          409,
          [],
          [],
          undefined,
          "thread_busy"
        )
      )
    )
    await waitFor(() => expect(attaches).toHaveLength(1))
    expect(attaches[0].thread).toBe("t-14")
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("says the server's words when a continue is refused", async () => {
    stubRows([message("m0", 0, "user", "question")])
    const cut = runningThread("t-15")
    cut.properties = { status: "stopped", reason: "stopped by its owner" }
    mount("t-15", { threadRecord: cut })
    await waitFor(() => expect(screen.getByText("question")).toBeTruthy())
    fireEvent.click(screen.getByRole("button", { name: "Continue" }))
    act(() =>
      continues[0].onError?.(
        new ApiError("guard", "spend cap reached: 512 of 500 cents", 403)
      )
    )
    expect(screen.getByRole("alert").textContent).toContain(
      "spend cap reached: 512 of 500 cents"
    )
    // The note and its button are back, to press again.
    expect(screen.getByRole("button", { name: "Continue" })).toBeTruthy()
    await act(() => new Promise((r) => setTimeout(r, 50)))
    expect(attaches).toHaveLength(0)
  })

  it("reads the thread again when a continue's answer is lost", async () => {
    stubRows([message("m0", 0, "user", "question")])
    const cut = runningThread("t-16")
    cut.properties = { status: "stopped", reason: "stopped by its owner" }
    mount("t-16", { threadRecord: cut })
    await waitFor(() => expect(screen.getByText("question")).toBeTruthy())
    fireEvent.click(screen.getByRole("button", { name: "Continue" }))
    act(() =>
      continues[0].onError?.(
        new TransportError("the connection failed before the server answered")
      )
    )
    // Whether the turn started is the thread's to say: an attach asks it.
    await waitFor(() => expect(attaches).toHaveLength(1))
    expect(continues).toHaveLength(1)
  })

  it("says a failure once, in the notice, when the thread stored it", async () => {
    stubRows([message("m0", 0, "user", "question")])
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const view = (threadRecord: SubstrateRecord) => (
      <QueryClientProvider client={client}>
        <Harness initialThread="t-11" threadRecord={threadRecord} />
      </QueryClientProvider>
    )
    const { rerender } = render(view(runningThread("t-11")))
    await waitFor(() => expect(attaches).toHaveLength(1))
    act(() =>
      attaches[0].onError?.(new ApiError("bad_request", "llm: overloaded", 400))
    )
    expect(screen.getByText(/couldn’t answer: llm: overloaded/)).toBeTruthy()
    // The thread's record catches up with how the turn ended.
    const failed = runningThread("t-11")
    failed.properties = { status: "error", reason: "llm: overloaded" }
    rerender(view(failed))
    await waitFor(() =>
      expect(
        screen.getByText("Helper ran into a problem and didn’t finish")
      ).toBeTruthy()
    )
    expect(screen.queryByText(/couldn’t answer/)).toBeNull()
  })

  it("holds Continue while the composer is blocked, and says why beside it", async () => {
    stubRows([])
    const cut = runningThread("t-12")
    cut.properties = {
      status: "stopped",
      reason: "stopped: the server shut down during the turn",
    }
    mount("t-12", { threadRecord: cut, keylessProvider: "anthropic" })
    await act(() => new Promise((r) => setTimeout(r, 0)))
    const go = screen.getByRole("button", { name: "Continue" })
    expect(go.getAttribute("aria-disabled")).toBe("true")
    expect(go.getAttribute("aria-describedby")).toBeTruthy()
    expect(
      document.getElementById(go.getAttribute("aria-describedby")!)?.textContent
    ).toBe("Add an API key first")
    fireEvent.click(go)
    expect(calls).toHaveLength(0)
    expect(continues).toHaveLength(0)
  })

  it("says nothing about a turn that finished", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(JSON.stringify({ records: [] }), { status: 200 })
      )
    )
    const done = runningThread("t-10")
    done.properties = { status: "ok" }
    mount("t-10", { threadRecord: done })
    await act(() => new Promise((r) => setTimeout(r, 0)))
    expect(screen.queryByRole("button", { name: "Continue" })).toBeNull()
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

  it("says a taken id by its reason, gives the message back and sends it under a new id", async () => {
    stubRows([message("m0", 0, "user", "question")])
    mount("t-7")
    await act(() => new Promise((r) => setTimeout(r, 0)))
    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "one more thing" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    const first = calls[0].messageId
    expect(first).toBeTruthy()
    act(() =>
      calls[0].onError?.(
        new ApiError(
          "conflict",
          `substrate: version conflict: the messageId is taken: messageId "${first}": send the message under a new id`,
          409,
          [],
          [],
          undefined,
          "message_id_taken"
        )
      )
    )
    const note = screen.getByRole("alert").textContent ?? ""
    expect(note).toContain("Your message wasn’t sent")
    expect(note).toContain(ID_TAKEN_REFUSAL)
    expect(note).not.toContain("the messageId is taken")
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(
      "one more thing"
    )
    // Nothing goes again on its own, and the next send is a new request.
    await act(() => new Promise((r) => setTimeout(r, 50)))
    expect(calls).toHaveLength(1)
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    expect(calls).toHaveLength(2)
    expect(calls[1].message).toBe("one more thing")
    expect(calls[1].messageId).toBeTruthy()
    expect(calls[1].messageId).not.toBe(first)
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

  /** Every transcript read waits until the test answers it, by its index.
   * The conversation's read of its waiting approvals answers at once with
   * none, and the change feed's watch never answers: neither is counted. */
  function deferredReads() {
    const reads: ((rows: unknown[]) => void)[] = []
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const url = decodeURIComponent(String(input))
        if (url.includes("llm/approval")) {
          return Promise.resolve(new Response(JSON.stringify({ records: [] })))
        }
        if (url.includes("/changes")) return new Promise<Response>(() => {})
        return new Promise<Response>((resolve) => {
          reads.push((rows) =>
            resolve(new Response(JSON.stringify({ records: rows })))
          )
        })
      })
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

describe("a send the server did not answer", () => {
  const lost = (
    at = calls.length - 1,
    reason = "the connection failed before the server answered"
  ) => calls[at].onError?.(new TransportError(reason))

  async function sendThenLose(text: string, fail: () => void = () => lost()) {
    stubRows([
      message("m1", 1, "assistant", "an answer"),
      message("m0", 0, "user", "question"),
    ])
    mount("t-8")
    await waitFor(() => expect(screen.getByText("an answer")).toBeTruthy())
    type(text)
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    act(fail)
  }

  it("sends every message under an id of its own", async () => {
    stubRows([])
    mount("t-8")
    await act(() => new Promise((r) => setTimeout(r, 0)))
    type("first")
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    act(() => {
      calls[0].onEvent({ kind: "thread", thread: "t-8" })
      calls[0].onEvent({
        kind: "done",
        result: { ...settled, thread: "t-8" },
      })
      calls[0].onDone?.()
    })
    await waitFor(() =>
      expect(
        screen.getByRole<HTMLButtonElement>("button", { name: "Send" })
      ).toBeTruthy()
    )
    type("second")
    await waitFor(() => {
      fireEvent.click(screen.getByRole("button", { name: "Send" }))
      expect(calls).toHaveLength(2)
    })
    expect(calls[0].messageId).toMatch(/^[0-9a-f-]{32,36}$/)
    expect(calls[1].messageId).not.toBe(calls[0].messageId)
  })

  it("sends it again as it was, and keeps it on the page meanwhile", async () => {
    await sendThenLose("one more thing")
    // Nothing is taken back while it goes again, and nothing new is sent.
    expect(screen.getByText("one more thing")).toBeTruthy()
    expect(screen.getByText(/Sending your message again/)).toBeTruthy()
    expect(screen.queryByRole("alert")).toBeNull()
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe("")
    await waitFor(() => expect(calls).toHaveLength(2))
    expect(calls[1].messageId).toBe(calls[0].messageId)
    expect(calls[1].message).toBe("one more thing")
    expect(calls[1].thread).toBe("t-8")
    // The second copy is answered: the page streams it as any send.
    act(() => {
      calls[1].onEvent({ kind: "thread", thread: "t-8" })
      calls[1].onEvent({ kind: "delta", text: "here it is", turn: 3 })
    })
    expect(screen.getByText("here it is")).toBeTruthy()
    expect(screen.queryByText(/Sending your message again/)).toBeNull()
  })

  it("reads the rows when the server already held the message and its turn settled", async () => {
    await sendThenLose("one more thing")
    await waitFor(() => expect(calls).toHaveLength(2))
    stubRows([
      message("m3", 3, "assistant", "the reply it missed"),
      message("m2", 2, "user", "one more thing"),
      message("m1", 1, "assistant", "an answer"),
      message("m0", 0, "user", "question"),
    ])
    // A repeat's whole answer: the thread, then the one idle line.
    act(() => {
      calls[1].onEvent({ kind: "thread", thread: "t-8" })
      calls[1].onEvent({ kind: "idle", thread: "t-8", status: "ok" })
      calls[1].onDone?.()
    })
    await waitFor(() =>
      expect(screen.getByText("the reply it missed")).toBeTruthy()
    )
    expect(screen.getAllByText("one more thing")).toHaveLength(1)
    expect(screen.queryByRole("alert")).toBeNull()
    expect(calls).toHaveLength(2)
    expect(attaches).toHaveLength(0)
  })

  it("blocks a second send while the first goes again, and says why", async () => {
    await sendThenLose("one more thing")
    type("another")
    expect(screen.getByText("Sending your last message again")).toBeTruthy()
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter" })
    expect(calls).toHaveLength(1)
    await waitFor(() => expect(calls).toHaveLength(2))
    expect(calls[1].message).toBe("one more thing")
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(
      "another"
    )
  })

  it("treats a 503 with the server's envelope as unanswered, not as refused", async () => {
    await sendThenLose("one more thing", () =>
      calls[0].onError?.(
        new ApiError(
          "unavailable",
          "the server is shutting down: send the message again once it is back",
          503
        )
      )
    )
    await waitFor(() => expect(calls).toHaveLength(2))
    expect(calls[1].messageId).toBe(calls[0].messageId)
    expect(screen.queryByText(/wasn’t sent/)).toBeNull()
  })

  it("sends it again when the stream closes before any event", async () => {
    await sendThenLose("one more thing", () => calls[0].onDone?.())
    await waitFor(() => expect(calls).toHaveLength(2))
    expect(calls[1].messageId).toBe(calls[0].messageId)
  })

  it("gives the message back when the copy sent again is refused", async () => {
    await sendThenLose("one more thing")
    await waitFor(() => expect(calls).toHaveLength(2))
    act(() =>
      calls[1].onError?.(
        new ApiError("guard", "spend cap reached: 512 of 500 cents", 403)
      )
    )
    expect(screen.getByRole("alert").textContent).toContain(
      "Your message wasn’t sent"
    )
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(
      "one more thing"
    )
    await act(() => new Promise((r) => setTimeout(r, 80)))
    expect(calls).toHaveLength(2)
  })

  it("keeps the message with Send again once every copy went unanswered", async () => {
    await sendThenLose("one more thing")
    for (let n = 2; n <= 1 + RESEND_DELAYS_MS.length; n++) {
      await waitFor(() => expect(calls).toHaveLength(n))
      act(() => lost())
    }
    const note = await screen.findByRole("alert")
    expect(note.textContent).toContain("may not have arrived")
    expect(note.textContent).toContain("it cannot be sent twice")
    expect(note.textContent).toContain("one more thing")
    expect(
      screen.getByRole("button", { name: "Copy your message" })
    ).toBeTruthy()
    // It is not put back in the box: typed again, it would go under a new
    // id and could be sent twice.
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe("")
    await act(() => new Promise((r) => setTimeout(r, 80)))
    const before = calls.length
    expect(before).toBe(1 + RESEND_DELAYS_MS.length)

    fireEvent.click(screen.getByRole("button", { name: "Send again" }))
    expect(calls).toHaveLength(before + 1)
    expect(calls[before].messageId).toBe(calls[0].messageId)
    expect(calls[before].message).toBe("one more thing")
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("sends a new chat's first message again under the same id, naming no thread", () => {
    mount()
    type("hello")
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    act(() => lost())
    return waitFor(() => {
      expect(calls).toHaveLength(2)
      expect(calls[1].thread).toBeUndefined()
      expect(calls[1].messageId).toBe(calls[0].messageId)
    })
  })

  it("sends an opening message again to no thread, though the page moved to another chat", async () => {
    stubRows([])
    mount()
    type("first chat")
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    act(() => lost())
    for (let n = 2; n <= 1 + RESEND_DELAYS_MS.length; n++) {
      await waitFor(() => expect(calls).toHaveLength(n))
      act(() => lost())
    }
    await screen.findByRole("button", { name: "Send again" })
    const firstId = calls[0].messageId
    // Another message opens a chat of its own; the note comes along.
    type("second chat")
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    const second = calls.length - 1
    act(() => {
      calls[second].onEvent({ kind: "thread", thread: "t-b" })
      calls[second].onEvent({
        kind: "done",
        result: { ...settled, thread: "t-b" },
      })
      calls[second].onDone?.()
    })
    await waitFor(() => {
      fireEvent.click(screen.getByRole("button", { name: "Send again" }))
      expect(calls).toHaveLength(second + 2)
    })
    // The request it went out as: no thread, the same agent, the same id.
    const replay = calls[second + 1]
    expect(replay.thread).toBeUndefined()
    expect(replay.agent).toBe("helper")
    expect(replay.messageId).toBe(firstId)
    expect(replay.message).toBe("first chat")
  })

  it("keeps the message and its id when the chat is left while it waits to go again", async () => {
    stubRows([message("m0", 0, "user", "question")])
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const at = (thread: string) => (
      <QueryClientProvider client={client}>
        <Keyed thread={thread} />
      </QueryClientProvider>
    )
    const view = render(at("t-8"))
    await waitFor(() => expect(screen.getByText("question")).toBeTruthy())
    type("lost one")
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    act(() => lost())
    // Left before the first wait is over: nothing went again.
    view.rerender(at("t-9"))
    await act(() => new Promise((r) => setTimeout(r, 120)))
    expect(calls).toHaveLength(1)
    view.rerender(at("t-8"))
    const note = await screen.findByRole("alert")
    expect(note.textContent).toContain("lost one")
    fireEvent.click(screen.getByRole("button", { name: "Send again" }))
    expect(calls).toHaveLength(2)
    expect(calls[1].messageId).toBe(calls[0].messageId)
    expect(calls[1].thread).toBe("t-8")
  })

  it("sends nothing again from a chat that was left", async () => {
    stubRows([message("m0", 0, "user", "question")])
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
    await act(() => new Promise((r) => setTimeout(r, 120)))
    expect(calls).toHaveLength(1)
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(
      "for the other chat"
    )
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("keeps an unanswered message across leaving a chat and coming back", async () => {
    stubRows([])
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const at = (thread: string) => (
      <QueryClientProvider client={client}>
        <Keyed thread={thread} />
      </QueryClientProvider>
    )
    const view = render(at(""))
    type("hello")
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    act(() => lost())
    for (let n = 2; n <= 1 + RESEND_DELAYS_MS.length; n++) {
      await waitFor(() => expect(calls).toHaveLength(n))
      act(() => lost())
    }
    expect((await screen.findByRole("alert")).textContent).toContain("hello")
    view.rerender(at("t-9"))
    expect(screen.queryByRole("alert")).toBeNull()
    view.rerender(at(""))
    const note = screen.getByRole("alert")
    expect(note.textContent).toContain("may not have arrived")
    expect(note.textContent).toContain("hello")
    expect(screen.getByRole("button", { name: "Send again" })).toBeTruthy()
  })
})

const settled = {
  reply: "ok",
  thread: "",
  status: "ok",
  effects: 0,
  turns: 1,
  toolCalls: 0,
  promptTokens: 1,
  completionTokens: 1,
  totalTokens: 2,
  costUSD: 0,
}

describe("a server from before a message could name its id", () => {
  beforeEach(() => {
    agentsRevision = 2
  })

  it("sends without an id, and never sends again on its own", async () => {
    stubRows([message("m0", 0, "user", "question")])
    mount("t-8")
    await waitFor(() => expect(screen.getByText("question")).toBeTruthy())
    type("one more thing")
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    // The server refuses a body field it does not know.
    expect(calls[0].messageId).toBeUndefined()
    act(() =>
      calls[0].onError?.(
        new TransportError("the connection failed before the server answered")
      )
    )
    const note = await screen.findByRole("alert")
    expect(note.textContent).toContain("may not have arrived")
    expect(note.textContent).toContain("Look for it in the chat")
    expect(screen.queryByRole("button", { name: "Send again" })).toBeNull()
    expect(
      screen.getByRole("button", { name: "Copy your message" })
    ).toBeTruthy()
    await act(() => new Promise((r) => setTimeout(r, 120)))
    expect(calls).toHaveLength(1)
  })

  it("goes on by sending the words, since the server has no continue", async () => {
    stubRows([])
    const cut = runningThread("t-9")
    cut.properties = { status: "stopped", reason: "stopped by its owner" }
    mount("t-9", { threadRecord: cut })
    await act(() => new Promise((r) => setTimeout(r, 0)))
    fireEvent.click(screen.getByRole("button", { name: "Continue" }))
    expect(continues).toHaveLength(0)
    expect(calls).toHaveLength(1)
    expect(calls[0].message).toBe("Continue where you left off.")
    expect(calls[0].messageId).toBeUndefined()
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
