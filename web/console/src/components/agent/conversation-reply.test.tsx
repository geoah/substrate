// @vitest-environment jsdom
/** Answering a question in your own words (decision 0327): the card's
 * button hands the next message to the question, the composer says which
 * question it answers, and the send carries the question's id with the
 * message's own. The composer goes back to an ordinary message once the
 * server holds it. A server from before the field is offered nothing. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { useState, type ReactNode } from "react"
import { afterEach, expect, it, vi } from "vitest"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
}))

type ChatOpts = {
  agent: string
  thread?: string
  message: string
  messageId?: string
  repliesTo?: string
  onEvent: (event: AgentEvent) => void
  onError?: (error: Error) => void
  onDone?: () => void
}
const calls: ChatOpts[] = []

vi.mock("@/lib/api/agents", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api/agents")>()
  const idle = () => ({ stop() {} })
  return {
    ...actual,
    streamChat: (opts: ChatOpts) => {
      calls.push(opts)
      return idle()
    },
    attachThread: idle,
    continueThread: idle,
  }
})

// The revision of the `agents` feature the server under test says it has.
let agentsRevision = 7
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

import type { AgentEvent } from "@/lib/api/agents"
import { ApiError, type SubstrateRecord } from "@/lib/api/types"
import { resetUnsent } from "@/lib/unsent-messages"
import { Conversation } from "./conversation"

afterEach(() => {
  cleanup()
  calls.length = 0
  agentsRevision = 7
  vi.unstubAllGlobals()
  resetUnsent()
})

function record(
  kind: string,
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  return {
    id,
    kind: `substrate.reamde.dev/llm/${kind}`,
    properties,
    labels: {},
    version: 2,
    createdAt: "2026-11-15T10:00:00Z",
    updatedAt: "2026-11-15T10:00:00Z",
  }
}

/** A settled chat that ended on a question: the rows, newest first as the
 * wire answers them, and the interaction its ask landed. */
function serveAskedThread() {
  const rows = [
    record("message", "m3", { turn: 3, role: "assistant", content: "Asked." }),
    record("message", "m2", {
      turn: 2,
      role: "tool",
      toolCallId: "c1",
      name: "ask",
      content: '{"id":"i1"}',
      ok: true,
      changes: [
        {
          seq: 9,
          op: "put",
          kind: "substrate.reamde.dev/llm/interaction",
          id: "i1",
        },
      ],
    }),
    record("message", "m1", {
      turn: 1,
      role: "assistant",
      content: "",
      toolCalls: [{ id: "c1", name: "ask", arguments: "{}" }],
    }),
    record("message", "m0", { turn: 0, role: "user", content: "Plan my week" }),
  ]
  const interaction = record("interaction", "i1", {
    state: "pending",
    thread: { ref: "substrate.reamde.dev/llm/thread/t1" },
    questions: [
      {
        id: "day",
        prompt: "Which day suits you?",
        options: [{ value: "mon", label: "Monday" }, { value: "tue" }],
      },
    ],
  })
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = decodeURIComponent(String(input))
      if (url.includes("/llm/interaction/i1")) return Response.json(interaction)
      if (url.includes("/api/v1/records") && url.includes("/llm/message"))
        return Response.json({ records: rows, head: 0, generation: "g" })
      return Response.json({ records: [], head: 0, generation: "g" })
    })
  )
}

function Harness() {
  const [draft, setDraft] = useState("")
  return (
    <Conversation
      agentId="helper"
      thread="t1"
      title="Plan my week"
      onThread={() => {}}
      draft={draft}
      onDraft={setDraft}
    />
  )
}

function mount() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <Harness />
    </QueryClientProvider>
  )
}

it("sends the next message as the question's answer, in one request", async () => {
  serveAskedThread()
  const { container } = mount()
  fireEvent.click(
    await screen.findByRole("button", { name: "Answer in your own words" })
  )
  // The composer has the cursor and says which question it answers; the
  // card sets its options aside.
  const note = container.querySelector("[data-slot=replying-to-question]")
  expect(note?.textContent).toContain("Answering in your own words:")
  expect(note?.textContent).toContain("Which day suits you?")
  expect(document.activeElement).toBe(screen.getByRole("textbox"))
  expect(screen.queryByRole("button", { name: "Dismiss" })).toBeNull()

  fireEvent.change(screen.getByRole("textbox"), {
    target: { value: "Neither: Thursday morning" },
  })
  fireEvent.click(screen.getByRole("button", { name: "Send" }))
  expect(calls).toHaveLength(1)
  expect(calls[0]).toMatchObject({
    thread: "t1",
    message: "Neither: Thursday morning",
    repliesTo: "i1",
  })
  expect(calls[0].messageId).toBeTruthy()

  // The server holds the message: the next one is an ordinary message.
  act(() => calls[0].onEvent({ kind: "thread", thread: "t1" }))
  expect(container.querySelector("[data-slot=replying-to-question]")).toBeNull()
})

it("keeps the choice when the send is refused, and drops it on Cancel", async () => {
  serveAskedThread()
  const { container } = mount()
  fireEvent.click(
    await screen.findByRole("button", { name: "Answer in your own words" })
  )
  fireEvent.change(screen.getByRole("textbox"), {
    target: { value: "Thursday" },
  })
  fireEvent.click(screen.getByRole("button", { name: "Send" }))
  act(() =>
    calls[0].onError?.(new ApiError("conflict", "three turns are running", 409))
  )
  // The message came back, still the question's answer.
  expect(
    container.querySelector("[data-slot=replying-to-question]")
  ).toBeTruthy()
  fireEvent.click(screen.getByRole("button", { name: "Send" }))
  expect(calls[1].repliesTo).toBe("i1")
  expect(calls[1].messageId).not.toBe(calls[0].messageId)
  act(() =>
    calls[1].onError?.(new ApiError("conflict", "three turns are running", 409))
  )

  fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
  expect(container.querySelector("[data-slot=replying-to-question]")).toBeNull()
  expect(
    await screen.findByRole("button", { name: "Answer in your own words" })
  ).toBeTruthy()
  fireEvent.click(screen.getByRole("button", { name: "Send" }))
  expect(calls[2].repliesTo).toBeUndefined()
})

it("offers nothing against a server that does not take the field", async () => {
  agentsRevision = 6
  serveAskedThread()
  mount()
  await screen.findByRole("button", { name: "Answer" })
  expect(
    screen.queryByRole("button", { name: "Answer in your own words" })
  ).toBeNull()
  fireEvent.change(screen.getByRole("textbox"), {
    target: { value: "Thursday" },
  })
  fireEvent.click(screen.getByRole("button", { name: "Send" }))
  expect(calls[0].repliesTo).toBeUndefined()
})
