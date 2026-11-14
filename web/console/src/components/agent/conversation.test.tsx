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
let agentsRevision = 5
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
import { TransportError, TurnFailedError } from "@/lib/api/agents"
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
  agentsRevision = 5
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
    expect(
      screen.getByText("The server restarted while Helper was working.")
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
    expect(
      screen.queryByText("The server restarted while Helper was working.")
    ).toBeNull()
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
        screen.getByText("Helper ran into a problem and didn’t finish.")
      ).toBeTruthy()
    )
    expect(screen.queryByText(/couldn’t answer/)).toBeNull()
    // The server's words are there, folded under Details.
    const details = screen.getByText("Details").closest("details")
    expect(details?.open).toBe(false)
    expect(details?.textContent).toContain("The server said: llm: overloaded")
  })

  it("says a failed turn by its cause, with one button, as soon as its stream reports it", async () => {
    stubRows([message("m0", 0, "user", "question")])
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const view = (threadRecord: SubstrateRecord) => (
      <QueryClientProvider client={client}>
        <Harness initialThread="t-17" threadRecord={threadRecord} />
      </QueryClientProvider>
    )
    const running = runningThread("t-17")
    running.properties = { status: "running", provider: "anthropic" }
    const { rerender } = render(view(running))
    await waitFor(() => expect(attaches).toHaveLength(1))
    // The stream ends with the turn's failure, its cause on the event.
    act(() =>
      attaches[0].onError?.(
        new TurnFailedError(
          "agent helper: llm: POST …: 429 Too Many Requests",
          "provider_rate_limited",
          20
        )
      )
    )
    // Whether the turn settled is the thread's to say: an attach asks it.
    await waitFor(() => expect(attaches).toHaveLength(2), { timeout: 3000 })
    act(() => {
      attaches[1].onEvent({ kind: "idle", thread: "t-17", status: "error" })
      attaches[1].onDone?.()
    })
    await waitFor(() =>
      expect(screen.getByText("Anthropic is limiting requests.")).toBeTruthy()
    )
    expect(screen.getByText("Try again in 20 seconds.")).toBeTruthy()
    expect(screen.queryByText(/couldn’t answer/)).toBeNull()
    expect(screen.queryByText(/429/)?.closest("details")).toBeTruthy()
    // The record catches up and says the same thing, once.
    const failed = runningThread("t-17")
    failed.properties = {
      status: "error",
      cause: "provider_rate_limited",
      retryAfter: 20,
      provider: "anthropic",
      reason: "POST …: 429 Too Many Requests",
    }
    rerender(view(failed))
    await waitFor(() =>
      expect(
        screen.getAllByText("Anthropic is limiting requests.")
      ).toHaveLength(1)
    )
    fireEvent.click(screen.getByRole("button", { name: "Try again" }))
    expect(continues.map((c) => c.thread)).toEqual(["t-17"])
  })

  it("counts a provider's wait down while its note shows, and drops it once it is over", async () => {
    stubRows([message("m0", 0, "user", "question")])
    const limited = runningThread("t-28")
    limited.properties = {
      status: "error",
      cause: "provider_rate_limited",
      retryAfter: 3,
      finishedAt: new Date(Date.now() - 1_000).toISOString(),
      provider: "anthropic",
    }
    mount("t-28", { threadRecord: limited })
    await waitFor(() => expect(screen.getByText("question")).toBeTruthy())
    expect(screen.getByText(/Try again in [12] seconds?\./)).toBeTruthy()
    await waitFor(() => expect(screen.queryByText(/Try again in/)).toBeNull(), {
      timeout: 4000,
    })
    // The sentence and its button stay.
    expect(screen.getByText("Anthropic is limiting requests.")).toBeTruthy()
    expect(screen.getByRole("button", { name: "Try again" })).toBeTruthy()
  })

  it("offers the provider's card for a refused key, a new chat for one too long, and no retry for either", async () => {
    stubRows([message("m0", 0, "user", "question")])
    const refused = runningThread("t-18")
    refused.properties = {
      status: "error",
      cause: "provider_key_refused",
      provider: "anthropic",
      reason: "Anthropic refused this key (401).",
    }
    const { unmount } = mount("t-18", { threadRecord: refused })
    await waitFor(() => expect(screen.getByText("question")).toBeTruthy())
    expect(screen.getByText("Anthropic refused the API key.")).toBeTruthy()
    expect(screen.getByText("Replace the key")).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull()
    unmount()

    const long = runningThread("t-19")
    long.properties = { status: "error", cause: "context_too_long" }
    mount("t-19", { threadRecord: long })
    await waitFor(() => expect(screen.getByText("question")).toBeTruthy())
    expect(
      screen.getByText("This chat is too long for the model.")
    ).toBeTruthy()
    expect(
      screen.getAllByText("New chat").some((el) => el.tagName === "A")
    ).toBe(true)
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull()
    expect(screen.queryByText("Details")).toBeNull()
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

describe("a stream joined twice", () => {
  it("keeps what it shows across an attach, and writes the replay over it", async () => {
    stubRows([message("m0", 0, "user", "question")])
    mount("t-20", { threadRecord: runningThread("t-20") })
    await waitFor(() => expect(attaches).toHaveLength(1))
    act(() => {
      attaches[0].onEvent({ kind: "thread", thread: "t-20" })
      attaches[0].onEvent({
        kind: "delta",
        text: "Tomatoes 🍅 ",
        turn: 1,
        at: 0,
      })
      attaches[0].onEvent({
        kind: "delta",
        text: "like a deep ",
        turn: 1,
        at: 11,
      })
    })
    const reply = screen.getByText("Tomatoes 🍅 like a deep")
    // The stream drops. The page attaches again, and what it shows stays
    // while the rows are read and the replay is on its way.
    act(() => attaches[0].onDone?.())
    await waitFor(() => expect(attaches).toHaveLength(2), { timeout: 3000 })
    expect(screen.getByText("Tomatoes 🍅 like a deep")).toBe(reply)
    // The replay comes back as one piece from 0, then the turn goes on.
    act(() => {
      attaches[1].onEvent({ kind: "thread", thread: "t-20" })
      attaches[1].onEvent({
        kind: "delta",
        text: "Tomatoes 🍅 like a deep watering ",
        turn: 1,
        at: 0,
      })
    })
    expect(screen.getByText("Tomatoes 🍅 like a deep watering")).toBe(reply)
    act(() =>
      attaches[1].onEvent({
        kind: "delta",
        text: "twice a week.",
        turn: 1,
        at: 32,
      })
    )
    // One reply, never two, and the same element throughout.
    expect(
      screen.getAllByText(/Tomatoes 🍅 like a deep watering twice a week\./)
    ).toEqual([reply])
  })

  it("leaves a turn the rows now hold to the rows, and keeps the one still streaming", async () => {
    let rows = [message("m0", 0, "user", "question")]
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(JSON.stringify({ records: rows }), { status: 200 })
      )
    )
    mount("t-21", { threadRecord: runningThread("t-21") })
    await waitFor(() => expect(attaches).toHaveLength(1))
    act(() => {
      attaches[0].onEvent({ kind: "thread", thread: "t-21" })
      attaches[0].onEvent({ kind: "delta", text: "Looking.", turn: 1, at: 0 })
      attaches[0].onEvent({
        kind: "toolStarted",
        id: "c1",
        tool: "query",
        args: "{}",
        turn: 2,
      })
      attaches[0].onEvent({
        kind: "toolFinished",
        id: "c1",
        tool: "query",
        output: "[]",
        ok: true,
        turn: 2,
      })
      attaches[0].onEvent({ kind: "delta", text: "Found ", turn: 3, at: 0 })
    })
    // The first turn and its call are stored by the time the page attaches
    // again; the reply is still only live.
    rows = [
      message("m2", 2, "tool", "[]"),
      message("m1", 1, "assistant", "Looking."),
      message("m0", 0, "user", "question"),
    ]
    act(() => attaches[0].onDone?.())
    await waitFor(() => expect(attaches).toHaveLength(2), { timeout: 3000 })
    act(() => {
      attaches[1].onEvent({ kind: "thread", thread: "t-21" })
      // The replay sends the stored turn again: it is dropped.
      attaches[1].onEvent({ kind: "delta", text: "Looking.", turn: 1, at: 0 })
      attaches[1].onEvent({
        kind: "delta",
        text: "Found none.",
        turn: 3,
        at: 0,
      })
    })
    expect(screen.getAllByText("Looking.")).toHaveLength(1)
    expect(screen.getAllByText("Found none.")).toHaveLength(1)
  })

  it("attaches again at once when it fell behind the turn", async () => {
    stubRows([message("m0", 0, "user", "question")])
    mount("t-22", { threadRecord: runningThread("t-22") })
    await waitFor(() => expect(attaches).toHaveLength(1))
    act(() => {
      attaches[0].onEvent({ kind: "thread", thread: "t-22" })
      attaches[0].onEvent({ kind: "delta", text: "so far", turn: 1, at: 0 })
      attaches[0].onEvent({ kind: "lagged", thread: "t-22" })
      attaches[0].onDone?.()
    })
    // No backoff: the turn is running and only this reader was slow.
    await waitFor(() => expect(attaches).toHaveLength(2), { timeout: 400 })
    expect(screen.getByText("so far")).toBeTruthy()
    expect(screen.queryByText(/couldn’t answer/)).toBeNull()
  })

  it("streams the run an answer resumed, once the thread says it is running", async () => {
    stubRows([
      message("m1", 1, "assistant", "asked."),
      message("m0", 0, "user", "pick a color"),
    ])
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const view = (threadRecord: SubstrateRecord) => (
      <QueryClientProvider client={client}>
        <Harness initialThread="t-23" threadRecord={threadRecord} />
      </QueryClientProvider>
    )
    const settled = runningThread("t-23")
    settled.properties = { status: "ok" }
    const { rerender } = render(view(settled))
    await waitFor(() => expect(screen.getByText("asked.")).toBeTruthy())
    expect(attaches).toHaveLength(0)
    // The answer resumes the thread: its record says running again.
    rerender(view(runningThread("t-23")))
    await waitFor(() => expect(attaches).toHaveLength(1))
    act(() => {
      attaches[0].onEvent({ kind: "thread", thread: "t-23" })
      attaches[0].onEvent({ kind: "delta", text: "noted: red", turn: 3, at: 0 })
    })
    // It streams, with Stop, and is not said to be working in the background.
    expect(screen.getByText("noted: red")).toBeTruthy()
    expect(screen.getAllByRole("button", { name: "Stop" })).not.toHaveLength(0)
    expect(screen.queryByText(/in the background/)).toBeNull()
  })

  it("streams a resumed run after a turn this page sent itself", async () => {
    let rows = [message("m0", 0, "user", "question")]
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(JSON.stringify({ records: rows }), { status: 200 })
      )
    )
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const view = (threadRecord: SubstrateRecord) => (
      <QueryClientProvider client={client}>
        <Harness initialThread="t-27" threadRecord={threadRecord} />
      </QueryClientProvider>
    )
    const idle = runningThread("t-27")
    idle.properties = { status: "ok" }
    const { rerender } = render(view(idle))
    await waitFor(() => expect(screen.getByText("question")).toBeTruthy())
    // The page's own turn: it asks, and settles.
    type("pick a color")
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    rows = [
      message("m2", 2, "assistant", "I asked."),
      message("m1", 1, "user", "pick a color"),
      ...rows,
    ]
    act(() => {
      calls[0].onEvent({ kind: "thread", thread: "t-27" })
      calls[0].onEvent({ kind: "delta", text: "I asked.", turn: 2, at: 0 })
      calls[0].onEvent({
        kind: "done",
        result: { ...settled, thread: "t-27" },
      })
      calls[0].onDone?.()
    })
    await waitFor(() =>
      expect(
        screen.getByRole<HTMLButtonElement>("button", { name: "Send" })
      ).toBeTruthy()
    )
    // An answer resumes the thread, and the page attaches to the run.
    rerender(view(runningThread("t-27")))
    await waitFor(() => expect(attaches).toHaveLength(1))
    act(() => {
      attaches[0].onEvent({ kind: "thread", thread: "t-27" })
      attaches[0].onEvent({
        kind: "delta",
        text: "Noted: red.",
        turn: 4,
        at: 0,
      })
    })
    // The resumed reply shows, under the turn the rows already hold, once.
    expect(screen.getByText("Noted: red.")).toBeTruthy()
    expect(screen.getAllByText("I asked.")).toHaveLength(1)
  })

  it("offers Continue on a reply the model's limit cut, and continues the thread", async () => {
    stubRows([
      message("m1", 1, "assistant", "It stops mid-sen", { cutBy: "length" }),
      message("m0", 0, "user", "question"),
    ])
    const done = runningThread("t-24")
    done.properties = { status: "ok", provider: "anthropic" }
    mount("t-24", { threadRecord: done })
    await waitFor(() =>
      expect(screen.getByText("It stops mid-sen")).toBeTruthy()
    )
    expect(
      screen.getByText("The reply stopped at the model’s limit.")
    ).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Continue" }))
    expect(continues.map((c) => c.thread)).toEqual(["t-24"])
    expect(calls).toHaveLength(0)
  })

  it("says nothing under a reply the model finished", async () => {
    stubRows([
      message("m1", 1, "assistant", "All of it."),
      message("m0", 0, "user", "question"),
    ])
    const done = runningThread("t-25")
    done.properties = { status: "ok" }
    mount("t-25", { threadRecord: done })
    await waitFor(() => expect(screen.getByText("All of it.")).toBeTruthy())
    expect(screen.queryByRole("button", { name: "Continue" })).toBeNull()
  })

  it("starts its overlay over on a server that sends no positions", async () => {
    agentsRevision = 4
    stubRows([message("m0", 0, "user", "question")])
    mount("t-26", { threadRecord: runningThread("t-26") })
    await waitFor(() => expect(attaches).toHaveLength(1))
    act(() => {
      attaches[0].onEvent({ kind: "thread", thread: "t-26" })
      attaches[0].onEvent({ kind: "delta", text: "half a ", turn: 1 })
      attaches[0].onDone?.()
    })
    await waitFor(() => expect(attaches).toHaveLength(2), { timeout: 3000 })
    act(() => {
      attaches[1].onEvent({ kind: "thread", thread: "t-26" })
      attaches[1].onEvent({ kind: "delta", text: "half a reply", turn: 1 })
    })
    // Appended to an overlay that was emptied first, it reads once.
    expect(screen.getAllByText("half a reply")).toHaveLength(1)
    expect(screen.queryByText("half a half a reply")).toBeNull()
  })
})

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

describe("a message sent while a turn runs", () => {
  beforeEach(() => {
    agentsRevision = 6
  })

  /** The waiting messages' marks, and the bubbles drawn as waiting. */
  const marks = () => document.querySelectorAll("[data-slot=queued-mark]")
  const waiting = () => document.querySelectorAll("[data-queued]")
  /** The page's text, to read what is drawn above what. */
  const page = () => document.body.textContent ?? ""

  /** A chat with one stored exchange, and a turn this page sent running. */
  async function running(thread = "t-30") {
    stubRows([
      message("m1", 1, "assistant", "an answer"),
      message("m0", 0, "user", "question"),
    ])
    mount(thread)
    await waitFor(() => expect(screen.getByText("an answer")).toBeTruthy())
    type("list my tasks")
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    act(() => calls[0].onEvent({ kind: "thread", thread }))
  }

  /** Sends `text` into the running turn, and has the server take it. */
  function sendMeanwhile(text: string) {
    type(text)
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    const call = calls[calls.length - 1]
    act(() => call.onEvent({ kind: "thread", thread: call.thread ?? "" }))
    return call
  }

  it("keeps the composer usable beside Stop, and sends into the running thread", async () => {
    await running()
    // Send stays, beside the composer's Stop and the header's.
    expect(screen.getAllByRole("button", { name: "Stop" })).toHaveLength(2)
    const box = screen.getByRole<HTMLTextAreaElement>("textbox")
    expect(box.disabled).toBe(false)
    expect(box.placeholder).toBe("Add to what Helper is working on…")
    expect(screen.queryByText(/Waiting for the previous turn/)).toBeNull()

    const call = sendMeanwhile("only the open ones")
    // The same send as any other: the thread, the text, an id of its own.
    expect(calls).toHaveLength(2)
    expect(call.thread).toBe("t-30")
    expect(call.message).toBe("only the open ones")
    expect(call.messageId).toBeTruthy()
    expect(call.messageId).not.toBe(calls[0].messageId)
    expect(box.value).toBe("")
    // The turn it joined goes on: nothing was reset, and Stop is still there.
    expect(screen.getByText("list my tasks")).toBeTruthy()
    expect(screen.getAllByRole("button", { name: "Stop" })).toHaveLength(2)
  })

  it("shows the message as queued until the model is handed it, then as any message", async () => {
    await running()
    act(() =>
      calls[0].onEvent({ kind: "delta", text: "Looking", turn: 3, at: 0 })
    )
    const call = sendMeanwhile("only the open ones")
    expect(screen.getByText("only the open ones")).toBeTruthy()
    expect(marks()).toHaveLength(1)
    expect(marks()[0].textContent).toBe("Queued")
    expect(waiting()).toHaveLength(1)

    // What the turn does meanwhile happened before the model read it, and
    // is drawn above it: the rest of the reply, and a call it makes.
    act(() => {
      calls[0].onEvent({
        kind: "messageQueued",
        id: call.messageId,
        turn: 4,
        text: "only the open ones",
      })
      calls[0].onEvent({ kind: "delta", text: " them up.", turn: 3, at: 7 })
      calls[0].onEvent({
        kind: "toolStarted",
        id: "call_0",
        tool: "query",
        args: "{}",
        turn: 5,
      })
    })
    expect(screen.getAllByText("only the open ones")).toHaveLength(1)
    expect(marks()).toHaveLength(1)
    expect(page().indexOf("Looking them up.")).toBeLessThan(
      page().indexOf("only the open ones")
    )

    act(() => {
      calls[0].onEvent({ kind: "messageTaken", id: call.messageId, turn: 4 })
      calls[0].onEvent({ kind: "delta", text: "Two are open.", turn: 6, at: 0 })
    })
    // Taken: an ordinary message, with the reply that read it under it.
    expect(marks()).toHaveLength(0)
    expect(waiting()).toHaveLength(0)
    expect(page().indexOf("only the open ones")).toBeLessThan(
      page().indexOf("Two are open.")
    )
    expect(screen.queryByText(/reads your message at its next step/)).toBeNull()
  })

  it("says what a stop does to a queued message, before and after it", async () => {
    await running()
    sendMeanwhile("not that one")
    expect(
      screen.getByText(
        "Helper reads your message at its next step. If you stop it first, the message stays here and is read when you continue."
      )
    ).toBeTruthy()
    sendMeanwhile("the other one")
    expect(
      screen.getByText(
        "Helper reads your 2 messages at its next step. If you stop it first, they stay here and are read when you continue."
      )
    ).toBeTruthy()
    // A stop on its way takes no more messages.
    fireEvent.click(screen.getAllByRole("button", { name: "Stop" })[0])
    expect(stops).toEqual(["t-30"])
    expect(screen.queryByRole("button", { name: "Send" })).toBeNull()
  })

  it("says a stopped turn left the last message unread, and that Continue reads it", async () => {
    stubRows([
      message("q1", 2, "user", "not that one"),
      message("m1", 1, "assistant", "partial"),
      message("m0", 0, "user", "question"),
    ])
    const stopped = runningThread("t-31")
    stopped.properties = { status: "stopped", cause: "requested" }
    mount("t-31", { threadRecord: stopped })
    await waitFor(() => expect(screen.getByText("not that one")).toBeTruthy())
    expect(
      screen.getByText("Helper was stopped before it finished.")
    ).toBeTruthy()
    expect(
      screen.getByText(
        "It hasn’t read your last message. Continue, and it will."
      )
    ).toBeTruthy()
    // A stored message is a message like any other: nothing is queued.
    expect(marks()).toHaveLength(0)
    fireEvent.click(screen.getByRole("button", { name: "Continue" }))
    expect(continues.map((c) => c.thread)).toEqual(["t-31"])
    expect(calls).toHaveLength(0)
  })

  it("offers Continue on a finished turn whose last message got no reply", async () => {
    stubRows([
      message("q1", 2, "user", "and the notes?"),
      message("m1", 1, "assistant", "All of it."),
      message("m0", 0, "user", "question"),
    ])
    const done = runningThread("t-32")
    done.properties = { status: "ok" }
    mount("t-32", { threadRecord: done })
    await waitFor(() => expect(screen.getByText("and the notes?")).toBeTruthy())
    expect(
      screen.getByText("Helper hasn’t answered your last message.")
    ).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Continue" }))
    expect(continues.map((c) => c.thread)).toEqual(["t-32"])
  })

  it("follows a message the turn never read to the turn it starts", async () => {
    await running()
    const call = sendMeanwhile("and the notes?")
    act(() => {
      calls[0].onEvent({
        kind: "messageQueued",
        id: call.messageId,
        turn: 4,
        text: "and the notes?",
      })
      calls[0].onEvent({ kind: "delta", text: "No tasks.", turn: 3, at: 0 })
      calls[0].onEvent({
        kind: "done",
        result: { ...settled, thread: "t-30" },
      })
      calls[0].onDone?.()
    })
    // The turn ended on its own with the message unread: the page attaches
    // to the turn the message starts, and the composer is not released as
    // if the chat were idle.
    await waitFor(() => expect(attaches).toHaveLength(1))
    expect(attaches[0].thread).toBe("t-30")
    expect(marks()).toHaveLength(1)
    act(() => {
      attaches[0].onEvent({ kind: "thread", thread: "t-30" })
      attaches[0].onEvent({
        kind: "messageTaken",
        id: call.messageId,
        turn: 4,
      })
      attaches[0].onEvent({ kind: "delta", text: "No notes.", turn: 5, at: 0 })
    })
    expect(marks()).toHaveLength(0)
    expect(screen.getAllByText("and the notes?")).toHaveLength(1)
    expect(page().indexOf("and the notes?")).toBeLessThan(
      page().indexOf("No notes.")
    )
  })

  it("reads a reply past the message as the model having read it, when no event said so", async () => {
    await running()
    const call = sendMeanwhile("only the open ones")
    act(() => {
      calls[0].onEvent({
        kind: "messageQueued",
        id: call.messageId,
        turn: 4,
        text: "only the open ones",
      })
      // A tool event past it says nothing: the call was already under way.
      calls[0].onEvent({
        kind: "toolFinished",
        id: "call_0",
        tool: "query",
        ok: true,
        output: "{}",
        turn: 5,
      })
    })
    expect(marks()).toHaveLength(1)
    act(() =>
      calls[0].onEvent({ kind: "delta", text: "Two are open.", turn: 6, at: 0 })
    )
    expect(marks()).toHaveLength(0)
  })

  it("takes a refused message out and puts it back in the composer", async () => {
    await running()
    type("too late")
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    expect(marks()).toHaveLength(1)
    act(() =>
      calls[1].onError?.(
        new ApiError("conflict", "thread t-30 already has a running turn", 409)
      )
    )
    expect(marks()).toHaveLength(0)
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(
      "too late"
    )
    expect(screen.getByRole("alert").textContent).toContain(
      "already has a running turn"
    )
    // The turn it was sent into is untouched.
    expect(screen.getAllByRole("button", { name: "Stop" })).toHaveLength(2)
  })

  it("sends an unanswered one again under the same id", async () => {
    await running()
    type("did it arrive?")
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    act(() =>
      calls[1].onError?.(
        new TransportError("the connection failed before the server answered")
      )
    )
    await waitFor(() => expect(calls).toHaveLength(3))
    expect(calls[2].messageId).toBe(calls[1].messageId)
    expect(calls[2].thread).toBe("t-30")
    expect(marks()).toHaveLength(1)
  })

  it("shows a message another window sent into the turn", async () => {
    stubRows([message("m0", 0, "user", "question")])
    mount("t-33", { threadRecord: runningThread("t-33") })
    await waitFor(() => expect(attaches).toHaveLength(1))
    act(() => {
      attaches[0].onEvent({ kind: "thread", thread: "t-33" })
      attaches[0].onEvent({ kind: "delta", text: "Working", turn: 1, at: 0 })
      attaches[0].onEvent({
        kind: "messageQueued",
        id: "from-the-phone",
        turn: 2,
        text: "also the notes",
      })
    })
    expect(screen.getByText("also the notes")).toBeTruthy()
    expect(marks()).toHaveLength(1)
  })

  it("leaves a message the rows already answer to the rows", async () => {
    stubRows([
      message("m3", 3, "assistant", "Two are open."),
      message("q1", 2, "user", "only the open ones"),
      message("m1", 1, "assistant", "Looking."),
      message("m0", 0, "user", "question"),
    ])
    mount("t-34", { threadRecord: runningThread("t-34") })
    await waitFor(() => expect(attaches).toHaveLength(1))
    act(() => {
      attaches[0].onEvent({ kind: "thread", thread: "t-34" })
      attaches[0].onEvent({
        kind: "messageQueued",
        id: "q1",
        turn: 2,
        text: "only the open ones",
      })
      attaches[0].onEvent({ kind: "messageTaken", id: "q1", turn: 2 })
    })
    // Once, where the rows have it, above the reply that read it.
    expect(screen.getAllByText("only the open ones")).toHaveLength(1)
    expect(marks()).toHaveLength(0)
    expect(page().indexOf("only the open ones")).toBeLessThan(
      page().indexOf("Two are open.")
    )
  })

  it("holds the composer shut while a turn runs on a server that takes no message meanwhile", async () => {
    agentsRevision = 5
    await running()
    expect(screen.queryByRole("button", { name: "Send" })).toBeNull()
    type("something")
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter" })
    expect(calls).toHaveLength(1)
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

function message(
  id: string,
  turn: number,
  role: string,
  content: string,
  more: { cutBy?: string } = {}
) {
  return {
    id,
    kind: "substrate.reamde.dev/llm/message",
    properties: { turn, role, content, ...more },
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
