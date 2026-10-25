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
  within,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"
import { INK_MEDIA_TYPE, inkText, newInkDocument } from "@/lib/ink"
import type { PropSpec } from "@/lib/record-schema"

import type { KindInfo } from "@/lib/api/types"

import { InkPropertySection, InkSections } from "./ink-section"

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
    /** The record as the server holds it now, for the re-read a refused
     * save makes. */
    current: undefined as SubstrateRecord | undefined,
    /** While set, an upload waits for it: a slow save. */
    hold: undefined as Promise<void> | undefined,
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
        if (server.hold) await server.hold
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
        return json(200, { ...record(""), version: server.version })
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

function record(digest: string, version = 4, id = "s1"): SubstrateRecord {
  return {
    id,
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
    server.current = record(theirs, 5)
    view.refresh(record(theirs, 5))

    fireEvent.click(screen.getByRole("button", { name: "Add page" }))
    fireEvent.click(screen.getByRole("button", { name: "Save" }))

    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toContain(
      "another save changed this ink after you started drawing"
    )
    expect(server.patches).toEqual([])
    // The editor stays open on the two-page drawing, and offers it.
    expect(screen.getByText("Page 2 of 2")).toBeTruthy()
    expect(screen.getByRole("button", { name: "Save" })).toBeTruthy()
    expect(
      screen.getByRole("button", { name: "Download this drawing" })
    ).toBeTruthy()
  })

  it("saves over a change to another property made while drawing", async () => {
    const mine = server.store(inkText(newInkDocument()))
    const view = renderSection(record(mine, 4))
    fireEvent.click(await screen.findByRole("button", { name: "Draw" }))

    // Another tab writes the transcript: the version moves, the file does not.
    server.version = 5
    server.current = record(mine, 5)
    view.refresh(record(mine, 5))

    fireEvent.click(screen.getByRole("button", { name: "Add page" }))
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(server.patches).toHaveLength(1))
    expect(server.patches[0].ifVersion).toBe(5)
    expect(screen.queryByRole("alert")).toBeNull()
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

  it("shows a note with no file, or an empty one, as blank paper to start on", async () => {
    renderSection(record(""))
    expect(screen.getByText("No handwriting yet.")).toBeTruthy()
    expect(screen.getByRole("button", { name: "Start drawing" })).toBeTruthy()
    cleanup()

    // A 0-byte file, as a lost upload body once stored: nothing to lose.
    const empty = record("")
    empty.properties = {
      sketch: {
        digest: `blob-sha256-${"e".repeat(64)}`,
        size: 0,
        mediaType: INK_MEDIA_TYPE,
      },
    }
    renderSection(empty)
    expect(screen.getByText("No handwriting yet.")).toBeTruthy()
    expect(screen.queryByText(/not valid JSON/)).toBeNull()
    expect(screen.getByRole("button", { name: "Start drawing" })).toBeTruthy()
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
  it("takes no change to the drawing while a save is running", async () => {
    const mine = server.store(inkText(newInkDocument()))
    renderSection(record(mine, 4))
    fireEvent.click(await screen.findByRole("button", { name: "Draw" }))
    fireEvent.click(screen.getByRole("button", { name: "Add page" }))
    expect(screen.getByText("Page 2 of 2")).toBeTruthy()

    let release = () => {}
    server.hold = new Promise((resolve) => (release = resolve))
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await screen.findByRole("button", { name: "Saving…" })

    // The save took two pages; nothing may change them before it lands.
    const add = screen.getByRole<HTMLButtonElement>("button", {
      name: "Add page",
    })
    expect(add.disabled).toBe(true)
    fireEvent.click(add)
    fireEvent.keyDown(window, { key: "z", ctrlKey: true })
    expect(screen.getByText("Page 2 of 2")).toBeTruthy()
    expect(
      screen.getByRole<HTMLButtonElement>("button", { name: "Undo" }).disabled
    ).toBe(true)

    release()
    await waitFor(() => expect(server.patches).toHaveLength(1))
    const saved = [...server.blobs.values()].at(-1)!
    expect(JSON.parse(saved).pages).toHaveLength(2)
  })
})

describe("InkPropertySection, leaving", () => {
  it("asks before discarding a drawing, and keeps the saved note either way", async () => {
    const mine = server.store(inkText(newInkDocument()))
    renderSection(record(mine, 4))
    fireEvent.click(await screen.findByRole("button", { name: "Draw" }))
    fireEvent.click(screen.getByRole("button", { name: "Add page" }))

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    const ask = await screen.findByRole("dialog", {
      name: "Discard your changes?",
    })
    fireEvent.click(within(ask).getByRole("button", { name: "Discard" }))
    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: "Drawing" })).toBeNull()
    )
    expect(server.patches).toEqual([])

    // Unchanged, the editor closes without a question.
    fireEvent.click(await screen.findByRole("button", { name: "Draw" }))
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect(screen.queryByRole("dialog", { name: "Drawing" })).toBeNull()
    expect(
      screen.queryByRole("dialog", { name: "Discard your changes?" })
    ).toBeNull()
  })
})

describe("InkSections", () => {
  const kind = {
    identity: KIND,
    name: "sketch",
    authority: "ada.example.com",
    package: "notes",
    version: 1,
    source: "installed",
    description: "",
    definition: {
      names: { singular: "sketch" },
      properties: {
        sketch: { type: "blobref", mediaTypes: [INK_MEDIA_TYPE] },
      },
    },
  } as unknown as KindInfo

  it("closes an open editor when the page moves to another record", async () => {
    const a = server.store(inkText(newInkDocument()))
    const b = server.store(inkText(newInkDocument()))
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const sections = (r: SubstrateRecord) => (
      <QueryClientProvider client={client}>
        <InkSections record={r} kind={kind} readOnly={false} />
      </QueryClientProvider>
    )
    const view = render(sections(record(a, 4, "s1")))
    fireEvent.click(await screen.findByRole("button", { name: "Draw" }))
    fireEvent.click(screen.getByRole("button", { name: "Add page" }))

    // The page reuses the section for the next record, as the router does.
    view.rerender(sections(record(b, 4, "s2")))
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull()
    expect(await screen.findByRole("button", { name: "Draw" })).toBeTruthy()
    expect(server.patches).toEqual([])
  })
})
