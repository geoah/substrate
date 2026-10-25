// @vitest-environment jsdom
/** A note's section on the record page never replaces a stored note it did
 * not mean to: a save asserts the version drawing began from, so a save made
 * in another tab meanwhile is refused as a conflict with the drawing kept, and
 * a note that failed to load offers Retry, never blank paper to save over it. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"
import { INK_MEDIA_TYPE, inkText, newInkDocument } from "@/lib/ink"
import type { PropSpec } from "@/lib/record-schema"

import { InkPropertySection } from "./ink-section"

const KIND = "ada.example.com/notes/sketch"

const spec = {
  name: "sketch",
  label: "Sketch",
  kind: "blobref",
  mediaTypes: [INK_MEDIA_TYPE],
} as PropSpec

/** The substrate behind the section: a blob store whose reads can fail, and
 * one record whose PATCH honours `ifVersion` the way the engine does. */
function fakeServer() {
  const server = {
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
        const text = await (init?.body as Blob).text()
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
        return json(200, { ...record(""), version: server.version })
      }
      return json(404, { error: { code: "not_found", message: input } })
    })
  )
  return server
}

function record(digest: string, version = 4): SubstrateRecord {
  return {
    id: "s1",
    kind: KIND,
    properties: digest
      ? { sketch: { digest, size: 10, mediaType: INK_MEDIA_TYPE } }
      : {},
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
      <InkPropertySection record={r} spec={spec} readOnly={false} />
    </QueryClientProvider>
  )
  const view = render(section(at))
  return { ...view, refresh: (r: SubstrateRecord) => view.rerender(section(r)) }
}

let server: ReturnType<typeof fakeServer>

beforeEach(() => {
  server = fakeServer()
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("InkPropertySection", () => {
  it("refuses a save over another tab's save that arrived while drawing, and keeps the drawing", async () => {
    const mine = server.store(inkText(newInkDocument()))
    const view = renderSection(record(mine, 4))
    fireEvent.click(await screen.findByRole("button", { name: "Draw" }))

    // Another tab saves; the live refresh hands the section version 5.
    const theirs = server.store(inkText(newInkDocument()))
    server.version = 5
    view.refresh(record(theirs, 5))

    fireEvent.click(screen.getByRole("button", { name: "Add page" }))
    fireEvent.click(screen.getByRole("button", { name: "Save" }))

    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toContain(
      "another save changed this record after you started drawing"
    )
    expect(server.patches).toEqual([])
    // The editor stays open on the two-page drawing, and offers it.
    expect(screen.getByText("Page 2 of 2")).toBeTruthy()
    expect(screen.getByRole("button", { name: "Save" })).toBeTruthy()
    expect(
      screen.getByRole("button", { name: "Download this drawing" })
    ).toBeTruthy()
  })

  it("saves against the version drawing began from when nothing moved", async () => {
    const mine = server.store(inkText(newInkDocument()))
    renderSection(record(mine, 4))
    fireEvent.click(await screen.findByRole("button", { name: "Draw" }))
    fireEvent.click(screen.getByRole("button", { name: "Add page" }))
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(server.patches).toHaveLength(1))
    expect(server.patches[0].ifVersion).toBe(4)
  })

  it("offers Retry, not blank paper, for a note that failed to load", async () => {
    const stored = server.store(inkText(newInkDocument()))
    server.failing.add(stored)
    renderSection(record(stored, 4))

    expect(await screen.findByText(/The note didn’t load/)).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Start drawing" })).toBeNull()
    expect(screen.queryByRole("button", { name: "Draw" })).toBeNull()

    server.failing.delete(stored)
    fireEvent.click(screen.getByRole("button", { name: "Retry" }))
    expect(await screen.findByRole("button", { name: "Draw" })).toBeTruthy()
  })
})
