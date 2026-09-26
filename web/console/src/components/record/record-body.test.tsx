// @vitest-environment jsdom
/** The record's body: prose under the sheet, the full width of the document
 * column in reading and in editing, a footer that says how it saves, and one
 * PATCH when it changes. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

const wire = vi.hoisted(() => ({ writes: [] as unknown[] }))

vi.mock("@/lib/api/http", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api/http")>()
  return {
    ...actual,
    request: vi.fn((method: string, _path: string, body?: unknown) => {
      if (method === "PATCH") wire.writes.push(body)
      return Promise.resolve({})
    }),
  }
})

import { RecordBody } from "./record-body"
import type { SubstrateRecord } from "@/lib/api/types"
import type { PropSpec } from "@/lib/record-schema"

const spec = {
  name: "details",
  label: "Details",
  kind: "markdown",
} as PropSpec

const record = (details?: string): SubstrateRecord => ({
  id: "p1",
  kind: "ada.example.com/people/person",
  properties: details ? { details } : {},
  labels: {},
  version: 4,
  createdAt: "x",
  updatedAt: "x",
})

function renderBody(r: SubstrateRecord) {
  const client = new QueryClient()
  return render(
    <QueryClientProvider client={client}>
      <RecordBody record={r} spec={spec} readOnly={false} />
    </QueryClientProvider>
  )
}

/** Whether an element caps its own width: the body must not. */
const capped = (el: Element) => /(^|\s)max-w-/.test(el.className)

afterEach(() => {
  cleanup()
  wire.writes = []
})

describe("RecordBody", () => {
  it("reads and edits across the whole document column", () => {
    renderBody(record("One paragraph.\n\nAnother."))
    const reader = screen.getByRole("button", { name: "Edit Details" })
    expect(capped(reader)).toBe(false)
    fireEvent.click(reader)
    const editor = screen.getByRole("textbox", { name: "Details" })
    expect(capped(editor)).toBe(false)
    expect(editor.className).toMatch(/w-\[calc\(100%\+1rem\)\]/)
  })

  it("invites prose where there is none, and saves it on ⌘Enter", async () => {
    renderBody(record())
    fireEvent.click(screen.getByText("Add details…"))
    const editor = screen.getByRole("textbox", { name: "Details" })
    fireEvent.change(editor, { target: { value: "Met at the conference." } })
    fireEvent.keyDown(editor, { key: "Enter", metaKey: true })
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0]).toEqual({
      properties: { details: "Met at the conference." },
      ifVersion: 4,
    })
  })

  it("shows how it saves under the editor, and Save writes", async () => {
    renderBody(record("Old."))
    fireEvent.click(screen.getByRole("button", { name: "Edit Details" }))
    expect(screen.getByText(/⌘Enter saves · Esc cancels/)).toBeTruthy()
    const editor = screen.getByRole("textbox", { name: "Details" })
    fireEvent.change(editor, { target: { value: "New." } })
    const save = screen.getByRole("button", { name: "Save" })
    // Moving to the footer is not leaving the editor.
    fireEvent.blur(editor, { relatedTarget: save })
    expect(wire.writes).toHaveLength(0)
    fireEvent.click(save)
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0]).toEqual({
      properties: { details: "New." },
      ifVersion: 4,
    })
  })

  it("Cancel throws the draft away and writes nothing", () => {
    renderBody(record("Old."))
    fireEvent.click(screen.getByRole("button", { name: "Edit Details" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Details" }), {
      target: { value: "New." },
    })
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect(screen.queryByRole("textbox")).toBeNull()
    expect(wire.writes).toHaveLength(0)
  })

  it("saves on leaving the editor and says it did", async () => {
    renderBody(record("Old."))
    fireEvent.click(screen.getByRole("button", { name: "Edit Details" }))
    const editor = screen.getByRole("textbox", { name: "Details" })
    fireEvent.change(editor, { target: { value: "New." } })
    fireEvent.blur(editor, { relatedTarget: document.body })
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect((await screen.findByRole("status")).textContent).toContain("Saved")
  })
})
