// @vitest-environment jsdom
/** A file picked in a form is uploaded at once, and stored again right before
 * the record write that names it, so a form left open past the server's
 * upload grace still saves after the sweep collected the first upload. */

import { cleanup, fireEvent, render, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { createRecord, patchRecord } from "@/lib/api/records"
import type { PropSpec } from "@/lib/record-schema"

import { BlobField } from "./blob-field"

const spec = { name: "scan", label: "Scan", kind: "blobref" } as PropSpec

/** The blob store and the record write: a write naming a blob the store
 * does not hold is refused the way the engine refuses it. */
function fakeServer() {
  const server = {
    blobs: new Map<string, string>(),
    calls: [] as string[],
  }
  const digestOf = (text: string) =>
    `blob-sha256-${[...text]
      .reduce((h, c) => (h * 31 + c.charCodeAt(0)) >>> 0, 7)
      .toString(16)
      .padStart(64, "0")}`
  const json = (status: number, body: unknown) =>
    new Response(JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json" },
    })
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string, init?: RequestInit) => {
      const method = init?.method ?? "GET"
      server.calls.push(`${method} ${input.split("?")[0]}`)
      if (input.startsWith("/api/v1/blobs") && method === "PUT") {
        const text = new TextDecoder().decode(init?.body as Uint8Array)
        const digest = digestOf(text)
        server.blobs.set(digest, text)
        return json(200, {
          digest,
          size: text.length,
          name: "scan.pdf",
          status: "stored",
        })
      }
      const body = JSON.parse(String(init?.body)) as {
        properties: Record<string, string>
      }
      const digest = body.properties.scan
      if (!server.blobs.has(digest))
        return json(422, {
          error: {
            code: "validation",
            message: `props.scan: blob ${digest} is unknown`,
          },
        })
      return json(200, {
        id: "p1",
        kind: "ada.example.com/papers/paper",
        properties: body.properties,
        labels: {},
        version: 1,
        createdAt: "x",
        updatedAt: "x",
      })
    })
  )
  return server
}

let server: ReturnType<typeof fakeServer>

beforeEach(() => {
  server = fakeServer()
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

/** The browser's clock, a minute after now: past the server's grace. */
function aMinuteLater() {
  const later = Date.now() + 60_000
  vi.spyOn(Date, "now").mockReturnValue(later)
}

/** Pick `file` in a BlobField and answer the digest the form then holds. */
async function pick(file: File): Promise<string> {
  const onChange = vi.fn<(digest: string) => void>()
  const view = render(
    <BlobField id="scan" spec={spec} value="" onChange={onChange} />
  )
  const input =
    view.container.querySelector<HTMLInputElement>("input[type=file]")!
  fireEvent.change(input, { target: { files: [file] } })
  await waitFor(() => expect(onChange).toHaveBeenCalled())
  return onChange.mock.calls[0][0]
}

describe("BlobField", () => {
  it("creates the record after the sweep collected the form's upload", async () => {
    const digest = await pick(
      new File(["%PDF-1.7 page one"], "scan.pdf", { type: "application/pdf" })
    )
    expect(server.blobs.has(digest)).toBe(true)

    // The form stayed open past the grace, and the sweep ran.
    aMinuteLater()
    server.blobs.clear()
    server.calls = []

    const saved = await createRecord("ada.example.com", "papers", "paper", {
      properties: { scan: digest },
    })
    expect(saved.properties.scan).toBe(digest)
    expect(server.calls).toEqual(["PUT /api/v1/blobs", "POST /api/v1/records"])
  })

  it("stores the blob again before a patch from the record page's editor", async () => {
    const digest = await pick(new File(["%PDF-1.7 page two"], "scan.pdf"))
    aMinuteLater()
    server.blobs.clear()
    server.calls = []

    await patchRecord("ada.example.com", "papers", "paper", "p1", {
      properties: { scan: digest },
      ifVersion: 1,
    })
    expect(server.calls).toEqual([
      "PUT /api/v1/blobs",
      "PATCH /api/v1/ada.example.com/papers/paper/p1",
    ])

    // Written, the record keeps the blob: the next write uploads nothing.
    server.calls = []
    await patchRecord("ada.example.com", "papers", "paper", "p1", {
      properties: { scan: digest },
    })
    expect(server.calls).toEqual([
      "PATCH /api/v1/ada.example.com/papers/paper/p1",
    ])
  })

  it("sends the file once when the record is written soon after", async () => {
    const digest = await pick(
      new File(["%PDF-1.7 page three"], "scan.pdf", {
        type: "application/pdf",
      })
    )
    server.calls = []
    await createRecord("ada.example.com", "papers", "paper", {
      properties: { scan: digest },
    })
    expect(server.calls).toEqual(["POST /api/v1/records"])
  })
})
