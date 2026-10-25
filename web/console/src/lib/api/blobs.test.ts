/** An upload sends its bytes as a buffer and stands behind what was stored:
 * the Apple shell's request forwarding received a Blob body empty, and a
 * note was stored as a 0-byte file, so a stored size that is not the sent
 * size is refused before any record names the blob. */

import { afterEach, describe, expect, it, vi } from "vitest"

import { putBlob } from "./blobs"

afterEach(() => vi.unstubAllGlobals())

function server(answer: (sent: Uint8Array) => number) {
  const bodies: unknown[] = []
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_path: string, init?: RequestInit) => {
      bodies.push(init?.body)
      const sent = init?.body as Uint8Array
      return new Response(
        JSON.stringify({
          digest: `blob-sha256-${"a".repeat(64)}`,
          size: answer(sent),
          status: "stored",
        }),
        { status: 201, headers: { "Content-Type": "application/json" } }
      )
    })
  )
  return bodies
}

describe("putBlob", () => {
  it("sends a file's bytes as a buffer, not as a Blob", async () => {
    const bodies = server((sent) => sent.byteLength)
    const file = new File(['{"format":"inkidian"}'], "note.inkd")
    const info = await putBlob(file, { name: "note.inkd" })
    expect(info.size).toBe(21)
    expect(bodies[0]).toBeInstanceOf(Uint8Array)
    expect(bodies[0]).not.toBeInstanceOf(Blob)
    expect(new TextDecoder().decode(bodies[0] as Uint8Array)).toBe(
      '{"format":"inkidian"}'
    )
  })

  it("refuses an upload the server stored at another size", async () => {
    server(() => 0)
    await expect(putBlob("not empty", { name: "note.inkd" })).rejects.toThrow(
      "The upload arrived with 0 of its 9 bytes, so nothing was saved."
    )
  })
})
