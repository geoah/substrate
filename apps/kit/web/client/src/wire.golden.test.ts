/** The kit's half of the wire guard for a refusal.
 *
 * `failure` (errors.ts) reads a refusal's body by hand. The server's word is
 * the console's two goldens, which Go writes: `wire.golden.json` from the
 * structs internal/substrate serializes (a field and whether it is always
 * there), `errorcodes.golden.json` from the codes internal/api answers with.
 * What a reader recovers from a `410 compacted` with is the code, and the
 * head and generation to start over from: each must be a field Go sends,
 * under that name, and the kit must read a refusal that leaves out what Go
 * may leave out.
 *
 * When this fails, fix the kit: the goldens are the server's word. */

import { expect, it } from "vitest"

import codes from "../../../../../web/console/src/lib/api/errorcodes.golden.json"
import wire from "../../../../../web/console/src/lib/api/wire.golden.json"
import { failure } from "./errors"

const envelope: Record<string, boolean> = wire.ErrorEnvelope
const payload: Record<string, boolean> = wire.ErrorPayload

it("reads a compacted refusal by the fields the server sends", async () => {
  expect(codes).toContain("compacted")
  expect(envelope).toEqual({ error: true })
  // What a reader recovers with, and whether the server always sends it.
  expect(payload.code).toBe(true)
  expect(payload.head).toBe(false)
  expect(payload.generation).toBe(false)

  const refusal = (fields: Record<string, unknown>) =>
    failure(
      new Response(JSON.stringify({ error: fields }), {
        status: 410,
        statusText: "Gone",
      })
    )
  const sent = await refusal({
    code: "compacted",
    message: "seq 3 is below the repository's horizon 9",
    head: 12,
    generation: "g1",
  })
  expect(sent.compacted).toBe(true)
  expect(sent.head).toBe(12)
  expect(sent.generation).toBe("g1")

  // Only what the golden says is always there.
  const bare = await refusal(
    Object.fromEntries(
      Object.entries({
        code: "compacted",
        message: "a cursor the server can no longer resume",
      }).filter(([field]) => payload[field])
    )
  )
  expect(bare.compacted).toBe(true)
  expect(bare.head).toBeUndefined()
  expect(bare.generation).toBeUndefined()
})
