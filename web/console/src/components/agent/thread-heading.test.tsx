// @vitest-environment jsdom
/** A conversation's heading renames its thread: Enter patches `subject`,
 * Escape leaves it, and a new chat offers no rename. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

const patchRecord = vi.fn()
vi.mock("@/lib/api/records", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/records")>()),
  patchRecord: (...args: unknown[]) => patchRecord(...args),
}))

import { ThreadHeading } from "./thread-heading"

function renderHeading(thread: string) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <ThreadHeading title="plan a trip" thread={thread} />
    </QueryClientProvider>
  )
}

afterEach(() => {
  cleanup()
  patchRecord.mockReset()
})

describe("the thread heading", () => {
  it("patches the subject on Enter", async () => {
    patchRecord.mockResolvedValue({})
    renderHeading("t1")
    fireEvent.click(screen.getByRole("button", { name: "Rename this chat" }))
    const input = screen.getByRole("textbox", { name: "Chat name" })
    fireEvent.change(input, { target: { value: "  Trip to Berlin " } })
    fireEvent.keyDown(input, { key: "Enter" })
    await waitFor(() =>
      expect(patchRecord).toHaveBeenCalledWith(
        "substrate.reamde.dev",
        "llm",
        "thread",
        "t1",
        { properties: { subject: "Trip to Berlin" } }
      )
    )
  })

  it("leaves the subject on Escape", () => {
    renderHeading("t1")
    fireEvent.click(screen.getByRole("button", { name: "Rename this chat" }))
    const input = screen.getByRole("textbox", { name: "Chat name" })
    fireEvent.change(input, { target: { value: "Other" } })
    fireEvent.keyDown(input, { key: "Escape" })
    expect(patchRecord).not.toHaveBeenCalled()
    expect(screen.getByRole("heading").textContent).toBe("plan a trip")
  })

  it("offers no rename before the thread exists", () => {
    renderHeading("")
    expect(screen.queryByRole("button", { name: "Rename this chat" })).toBe(
      null
    )
  })
})
