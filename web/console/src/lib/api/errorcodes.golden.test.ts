/** The console's half of the error-code guard.
 *
 * `ServerErrorCode` in types.ts is written BY HAND to mirror the codes
 * internal/api/errors.go answers with. `errorcodes.golden.json` is where the
 * two meet: a Go test in internal/api writes the server's closed set to it,
 * sorted, and this test asserts the union names exactly those.
 *
 * A union is erased at runtime, so the test cannot list its members. The
 * `Record<ServerErrorCode, true>` literal below will not COMPILE unless it has
 * a key for every member and no other, so `tsc` holds the literal to the
 * union and this holds the literal to the golden.
 *
 * When this fails, fix the union: the golden is the server's word. A code the
 * console makes up itself belongs in `ClientErrorCode`, never here. */

import { expect, it } from "vitest"

import golden from "./errorcodes.golden.json"
import type { ServerErrorCode } from "./types"

const serverErrorCodes: Record<ServerErrorCode, true> = {
  auth: true,
  bad_request: true,
  compacted: true,
  conflict: true,
  forbidden: true,
  function_failed: true,
  guard: true,
  internal: true,
  lossy: true,
  not_found: true,
  parked: true,
  rate_limited: true,
  totp_required: true,
  unavailable: true,
  validation: true,
}

it("ServerErrorCode is exactly the server's closed set", () => {
  // The golden is non-empty, or the comparison below proves nothing.
  expect(golden.length).toBeGreaterThan(0)
  expect(Object.keys(serverErrorCodes).sort()).toEqual(golden)
})
