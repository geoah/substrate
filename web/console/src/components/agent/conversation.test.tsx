// @vitest-environment jsdom
/** A new chat whose run fails before the server names its thread has nothing
 * to hand over to: there are no rows to read back. The composer is released,
 * the error is shown, and what was typed comes back so a retry is one press. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { useState, type ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
}))

type ChatOpts = {
  thread?: string
  message: string
  onEvent: (event: { kind: string; thread?: string }) => void
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

import { Conversation } from "./conversation"

afterEach(() => {
  cleanup()
  calls.length = 0
})

function Harness() {
  const [draft, setDraft] = useState("")
  const [thread, setThread] = useState("")
  return (
    <Conversation
      agentId="helper"
      thread={thread}
      title="New chat"
      onThread={setThread}
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
})
