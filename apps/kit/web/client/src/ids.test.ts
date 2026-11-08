import { describe, expect, it } from "vitest"
import { uuidv7 } from "./ids"

describe("uuidv7", () => {
  it("is a lowercase version 7, variant 10 UUID", () => {
    const id = uuidv7()
    expect(id).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/
    )
  })

  it("carries its millisecond in the first 48 bits, so ids sort by time", () => {
    const at = Date.parse("2026-11-08T12:00:00Z")
    const id = uuidv7(at)
    expect(parseInt(id.replace(/-/g, "").slice(0, 12), 16)).toBe(at)
    expect(uuidv7(at) < uuidv7(at + 1)).toBe(true)
  })

  it("is not repeated", () => {
    const ids = new Set(Array.from({ length: 1000 }, () => uuidv7(0)))
    expect(ids.size).toBe(1000)
  })
})
