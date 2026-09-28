// @vitest-environment jsdom
/** The record's body: Markdown under the sheet, rendered as a document, the
 * full width of the document column in reading and in editing, a footer that
 * says how it saves, and one PATCH when it changes. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import type { TiptapEditorHTMLElement } from "@tiptap/core"
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
import { kindsQueryOptions } from "@/lib/api/kinds"
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
  const body = (at: SubstrateRecord) => (
    <QueryClientProvider client={client}>
      <RecordBody record={at} spec={spec} readOnly={false} />
    </QueryClientProvider>
  )
  const view = render(body(r))
  return { ...view, refresh: (at: SubstrateRecord) => view.rerender(body(at)) }
}

/** The body under a router, for the links it renders. */
function renderRouted(r: SubstrateRecord) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  client.setQueryData(kindsQueryOptions.queryKey, [])
  const rootRoute = createRootRoute({
    component: () => (
      <QueryClientProvider client={client}>
        <RecordBody record={r} spec={spec} readOnly={false} />
      </QueryClientProvider>
    ),
  })
  const router = createRouter({
    routeTree: rootRoute,
    history: createMemoryHistory({ initialEntries: ["/"] }),
  })
  return render(
    <RouterProvider
      router={
        router as unknown as Parameters<typeof RouterProvider>[0]["router"]
      }
    />
  )
}

/** The editor's chunk loads lazily, slower under a full parallel run. */
const LOADED = { timeout: 5000 }

/** The open editor, once its chunk has loaded. */
const editorBox = () =>
  screen.findByRole("textbox", { name: "Details" }, LOADED)

/** Replace the open editor's document, as typing would. */
async function type(markdown: string) {
  const box = await editorBox()
  ;(box as TiptapEditorHTMLElement).editor!.commands.setContent(markdown, {
    contentType: "markdown",
  })
  return box
}

/** Whether an element caps its own width: the body must not. */
const capped = (el: Element) => /(^|\s)max-w-/.test(el.className)

afterEach(() => {
  cleanup()
  wire.writes = []
})

describe("RecordBody", () => {
  it("reads and edits across the whole document column", async () => {
    renderBody(record("One paragraph.\n\nAnother."))
    const reader = screen.getByRole("button", { name: "Edit Details" })
    expect(capped(reader)).toBe(false)
    fireEvent.click(reader)
    const editor = await editorBox()
    const box = editor.closest('[data-slot="markdown"]')!.parentElement!
    expect(capped(box)).toBe(false)
    expect(box.className).toMatch(/w-\[calc\(100%\+1rem\)\]/)
  })

  it("renders the Markdown, record links as links to the record", async () => {
    renderRouted(
      record(
        "# Plan\n\n- one\n- two\n\nWith [Grace](substrate://ada.example.com/people/person/grace)."
      )
    )
    const heading = await screen.findByRole("heading", { level: 1 }, LOADED)
    expect(heading.textContent).toBe("Plan")
    expect(screen.getAllByRole("listitem")).toHaveLength(2)
    const link = await screen.findByRole("link", {}, LOADED)
    expect(link.getAttribute("href")).toBe(
      "/data/ada.example.com/people/person/grace"
    )
    // Following the link is not editing the body.
    fireEvent.click(link)
    expect(screen.queryByRole("textbox")).toBeNull()
  })

  it("invites prose where there is none, and saves it on ⌘Enter", async () => {
    renderBody(record())
    fireEvent.click(screen.getByText("Add details…"))
    const editor = await type("Met at **the conference**.")
    fireEvent.keyDown(editor, { key: "Enter", metaKey: true })
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0]).toEqual({
      properties: { details: "Met at **the conference**." },
      ifVersion: 4,
    })
  })

  it("shows how it saves under the editor, and Save writes", async () => {
    renderBody(record("Old."))
    fireEvent.click(screen.getByRole("button", { name: "Edit Details" }))
    expect(screen.getByText(/⌘Enter saves · Esc\s+cancels/)).toBeTruthy()
    const editor = await type("New.")
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

  it("Cancel throws the draft away and writes nothing", async () => {
    renderBody(record("Old."))
    fireEvent.click(screen.getByRole("button", { name: "Edit Details" }))
    await type("New.")
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect(screen.queryByRole("textbox")).toBeNull()
    expect(wire.writes).toHaveLength(0)
  })

  it("saves on leaving the editor and says it did", async () => {
    renderBody(record("Old."))
    fireEvent.click(screen.getByRole("button", { name: "Edit Details" }))
    const editor = await type("New.")
    fireEvent.blur(editor, { relatedTarget: document.body })
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect((await screen.findByRole("status")).textContent).toContain("Saved")
  })

  it("gives focus back to the body after Esc", async () => {
    renderBody(record("Old."))
    fireEvent.click(screen.getByRole("button", { name: "Edit Details" }))
    fireEvent.keyDown(await editorBox(), {
      key: "Escape",
    })
    expect(document.activeElement).toBe(
      screen.getByRole("button", { name: "Edit Details" })
    )
  })

  it.each([["metaKey"], ["ctrlKey"]])(
    "claims %s+K for the record picker, not the palette",
    async (modifier) => {
      renderBody(record("Old."))
      fireEvent.click(screen.getByRole("button", { name: "Edit Details" }))
      const editor = await editorBox()
      // false: the editor prevented it, so the shell's palette stays shut.
      expect(fireEvent.keyDown(editor, { key: "k", [modifier]: true })).toBe(
        false
      )
      expect(editor.textContent).toBe("Old. @")
    }
  )

  it("keeps the version it began from across a live refresh", async () => {
    const { refresh } = renderBody(record("Old."))
    fireEvent.click(screen.getByRole("button", { name: "Edit Details" }))
    await type("Mine.")
    refresh({ ...record("Theirs."), version: 5 })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(wire.writes).toHaveLength(1))
    expect(wire.writes[0]).toEqual({
      properties: { details: "Mine." },
      ifVersion: 4,
    })
  })
})
