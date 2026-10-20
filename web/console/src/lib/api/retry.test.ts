/** The retry policy every console query follows: a busy server's 503 is
 * asked again twice after the Retry-After it names, a re-derivation never,
 * and a definitive refusal never. */

import { describe, expect, it } from "vitest"

import { retryDelay, retryRead } from "./retry"
import { ApiError } from "./types"

const busy = new ApiError("unavailable", "busy", 503, [], [], 7)
const rederiving = new ApiError(
  "unavailable",
  "re-deriving",
  503,
  [],
  [],
  30,
  "observed_rederiving"
)
const invalid = new ApiError("validation", "no", 422, ["aggregate.range: no"])
const offline = new ApiError("network", "offline", 0)

describe("retryRead", () => {
  it("asks a busy server again twice, then stops", () => {
    expect(retryRead(0, busy)).toBe(true)
    expect(retryRead(1, busy)).toBe(true)
    expect(retryRead(2, busy)).toBe(false)
  })

  it("never asks again while the server re-derives, or after a definitive refusal", () => {
    expect(retryRead(0, rederiving)).toBe(false)
    expect(retryRead(0, invalid)).toBe(false)
  })

  it("asks again after transport trouble", () => {
    expect(retryRead(0, offline)).toBe(true)
    expect(retryRead(0, new TypeError("fetch failed"))).toBe(true)
  })
})

describe("retryDelay", () => {
  it("waits the Retry-After the server named, else backs off", () => {
    expect(retryDelay(0, busy)).toBe(7000)
    expect(retryDelay(0, offline)).toBe(1000)
    expect(retryDelay(2, offline)).toBe(4000)
    expect(retryDelay(9, offline)).toBe(30_000)
  })
})
