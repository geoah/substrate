// @vitest-environment jsdom
/** A new chat whose run fails before the server names its thread has nothing
 * to hand over to: there are no rows to read back. The composer is released,
 * the error is shown, and what was typed comes back so a retry is one press.
 * A question another page handed over to be asked is sent once, on open. */

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

import type { AgentEvent } from "@/lib/api/agents"
import { Conversation } from "./conversation"

afterEach(() => {
  cleanup()
  calls.length = 0
  vi.unstubAllGlobals()
})

function Harness({
  initialThread = "",
  initialDraft = "",
  autoSend = false,
  onAutoSent,
}: {
  initialThread?: string
  initialDraft?: string
  autoSend?: boolean
  onAutoSent?: () => void
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
    expect((screen.getByRole("textbox") as HTMLTextAreaElement).value).toBe(
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
      (screen.getByRole("button", { name: "Send" }) as HTMLButtonElement)
        .disabled
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
})
