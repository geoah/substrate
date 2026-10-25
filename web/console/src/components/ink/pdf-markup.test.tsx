// @vitest-environment jsdom
/** A PDF's ink layer is never replaced by accident: a layer that failed to
 * download offers Retry and no Annotate, so no blank layer can be saved over
 * it, and a save asserts the version annotating began from, so another tab's
 * save meanwhile is refused as a conflict with the drawing kept. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"
import { INK_MEDIA_TYPE, inkText, newInkDocument } from "@/lib/ink"
import type { PropSpec } from "@/lib/record-schema"

// PDF.js needs a canvas and a worker jsdom has neither of; the section only
// needs to learn the page count and sizes.
vi.mock("pdfjs-dist", () => ({
  GlobalWorkerOptions: {},
  AnnotationMode: { ENABLE: 1 },
  getDocument: () => ({
    promise: Promise.resolve({
      numPages: 1,
      getPage: () =>
        Promise.resolve({
          getViewport: () => ({ width: 595.2756, height: 841.8898 }),
          render: () => ({ promise: Promise.resolve(), cancel() {} }),
        }),
    }),
    destroy: () => Promise.resolve(),
  }),
}))
vi.mock("pdfjs-dist/build/pdf.worker.min.mjs?url", () => ({
  default: "pdf.worker.js",
}))

import { PdfMarkupSection } from "./pdf-markup"

const KIND = "ada.example.com/papers/paper"

const documentSpec = {
  name: "document",
  label: "Document",
  kind: "blobref",
  mediaTypes: ["application/pdf"],
} as PropSpec

const inkSpec = {
  name: "markup",
  label: "Markup",
  kind: "blobref",
  mediaTypes: [INK_MEDIA_TYPE],
} as PropSpec

/** The substrate behind the section: a blob store whose reads can fail, and
 * one record whose PATCH honours `ifVersion` the way the engine does. */
function fakeServer() {
  const server = {
    /** The record as the server holds it now, for the re-read a refused
     * save makes. */
    current: undefined as SubstrateRecord | undefined,
    version: 4,
    blobs: new Map<string, string>(),
    failing: new Set<string>(),
    patches: [] as {
      properties: Record<string, unknown>
      ifVersion?: number
    }[],
    store(text: string) {
      const digest = `blob-sha256-${(server.blobs.size + 1).toString(16).padStart(64, "0")}`
      server.blobs.set(digest, text)
      return digest
    },
  }
  const json = (status: number, body: unknown) =>
    new Response(JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json" },
    })
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string, init?: RequestInit) => {
      const method = init?.method ?? "GET"
      if (input.startsWith("/api/v1/blobs") && method === "PUT") {
        const text = new TextDecoder().decode(init?.body as Uint8Array)
        const digest = server.store(text)
        return json(200, { digest, size: text.length, status: "stored" })
      }
      if (input.startsWith("/api/v1/blobs/")) {
        const digest = input.split("/").pop()!
        const text = server.blobs.get(digest)
        if (text === undefined || server.failing.has(digest))
          return json(503, { error: { code: "unavailable", message: "down" } })
        return new Response(text)
      }
      if (method === "PATCH") {
        const body = JSON.parse(String(init?.body)) as {
          properties: Record<string, unknown>
          ifVersion?: number
        }
        if (body.ifVersion !== server.version)
          return json(409, {
            error: { code: "conflict", message: "version moved" },
          })
        server.version++
        server.patches.push(body)
        return json(200, { ...record("", ""), version: server.version })
      }
      if (
        method === "GET" &&
        input.startsWith(`/api/v1/${KIND}/`) &&
        server.current
      )
        return json(200, { ...server.current, version: server.version })
      return json(404, { error: { code: "not_found", message: input } })
    })
  )
  return server
}

function record(pdf: string, ink: string, version = 4): SubstrateRecord {
  const blob = (digest: string, mediaType: string) =>
    digest ? { digest, size: 10, mediaType } : undefined
  return {
    id: "p1",
    kind: KIND,
    properties: {
      document: blob(pdf, "application/pdf"),
      markup: blob(ink, INK_MEDIA_TYPE),
    },
    labels: {},
    version,
    createdAt: "x",
    updatedAt: "x",
  }
}

function renderSection(at: SubstrateRecord) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const section = (r: SubstrateRecord) => (
    <QueryClientProvider client={client}>
      <PdfMarkupSection
        record={r}
        document={documentSpec}
        ink={inkSpec}
        readOnly={false}
      />
    </QueryClientProvider>
  )
  const view = render(section(at))
  return { ...view, refresh: (r: SubstrateRecord) => view.rerender(section(r)) }
}

/** One mouse stroke across the layer page. jsdom has no SVG geometry, so
 * client coordinates are page coordinates. */
function drawStroke() {
  const svg = screen.getByRole("img", { name: "Ink on page 1" })
  const at = (x: number, y: number) => ({
    pointerId: 1,
    pointerType: "mouse",
    button: 0,
    buttons: 1,
    clientX: x,
    clientY: y,
  })
  fireEvent.pointerDown(svg, at(100, 100))
  fireEvent.pointerMove(svg, at(200, 150))
  fireEvent.pointerUp(svg, at(200, 150))
}

let server: ReturnType<typeof fakeServer>

beforeEach(() => {
  server = fakeServer()
  vi.stubGlobal(
    "DOMPoint",
    class {
      x: number
      y: number
      constructor(x = 0, y = 0) {
        this.x = x
        this.y = y
      }
      matrixTransform() {
        return this
      }
    }
  )
  Object.defineProperty(SVGSVGElement.prototype, "getScreenCTM", {
    configurable: true,
    value: () => ({ inverse: () => ({}) }),
  })
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("PdfMarkupSection", () => {
  it("offers Retry and no Annotate when the stored layer fails to download", async () => {
    const pdf = server.store("%PDF-1.7")
    const layer = server.store(inkText(newInkDocument()))
    server.failing.add(layer)
    renderSection(record(pdf, layer))

    expect(await screen.findByText(/The ink layer didn’t load/)).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Annotate" })).toBeNull()
    expect(screen.queryByRole("img", { name: "Ink on page 1" })).toBeNull()
    expect(server.patches).toEqual([])

    server.failing.delete(layer)
    fireEvent.click(screen.getByRole("button", { name: "Retry" }))
    expect(await screen.findByRole("button", { name: "Annotate" })).toBeTruthy()
  })

  it("starts a blank layer only where the record names none", async () => {
    const pdf = server.store("%PDF-1.7")
    renderSection(record(pdf, ""))
    expect(await screen.findByRole("button", { name: "Annotate" })).toBeTruthy()
  })

  it("refuses a save over another tab's save that arrived while annotating, and keeps the drawing", async () => {
    const pdf = server.store("%PDF-1.7")
    const mine = server.store(inkText(newInkDocument()))
    const view = renderSection(record(pdf, mine, 4))
    fireEvent.click(await screen.findByRole("button", { name: "Annotate" }))

    const theirs = server.store(inkText(newInkDocument()))
    server.version = 5
    server.current = record(pdf, theirs, 5)
    view.refresh(record(pdf, theirs, 5))

    drawStroke()
    fireEvent.click(screen.getByRole("button", { name: "Save" }))

    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toContain(
      "another save changed this ink after you started drawing"
    )
    expect(server.patches).toEqual([])
    expect(screen.getByRole("button", { name: "Save" })).toBeTruthy()
    expect(
      screen.getByRole("button", { name: "Download this drawing" })
    ).toBeTruthy()
  })

  it("saves against the version annotating began from when nothing moved", async () => {
    const pdf = server.store("%PDF-1.7")
    renderSection(record(pdf, "", 4))
    fireEvent.click(await screen.findByRole("button", { name: "Annotate" }))
    drawStroke()
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(server.patches).toHaveLength(1))
    expect(server.patches[0].ifVersion).toBe(4)
  })

  it("asks before discarding a layer drawn on but not saved", async () => {
    const pdf = server.store("%PDF-1.7")
    renderSection(record(pdf, ""))
    fireEvent.click(await screen.findByRole("button", { name: "Annotate" }))
    drawStroke()
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    const ask = await screen.findByRole("dialog", {
      name: "Discard your changes?",
    })
    fireEvent.click(within(ask).getByRole("button", { name: "Discard" }))
    expect(await screen.findByRole("button", { name: "Annotate" })).toBeTruthy()
    expect(server.patches).toEqual([])
  })
})
