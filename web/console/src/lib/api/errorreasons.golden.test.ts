/** The console's half of the error-reason guard.
 *
 * `ErrorReason` in types.ts is written BY HAND to mirror the reasons
 * internal/substrate/errors.go lists (`ErrorReasons`). `errorreasons.golden.json`
 * is where the two meet: a Go test in internal/api writes the server's closed
 * set to it, sorted, and this test asserts the union names exactly those.
 *
 * A union is erased at runtime, so the test cannot list its members. The
 * `Record<ErrorReason, true>` literal below will not COMPILE unless it has a
 * key for every member and no other, so `tsc` holds the literal to the union
 * and this holds the literal to the golden.
 *
 * When this fails, fix the union: the golden is the server's word. */

import { expect, it } from "vitest"

import golden from "./errorreasons.golden.json"
import type { ErrorReason } from "./types"

const errorReasons: Record<ErrorReason, true> = {
  erasing: true,
  first_attempt_landed: true,
  first_attempt_running: true,
  in_doubt: true,
  key_refused: true,
  observed_rederiving: true,
  quota: true,
  repository_required: true,
  set_aside: true,
  stale_revision: true,
}

it("ErrorReason is exactly the server's closed set", () => {
  // The golden is non-empty, or the comparison below proves nothing.
  expect(golden.length).toBeGreaterThan(0)
  expect(Object.keys(errorReasons).sort()).toEqual(golden)
})
