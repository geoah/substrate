import { describe, expect, it } from "vitest"
import { derivedUUID, uuidv7 } from "./ids"

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

describe("derivedUUID", () => {
  // The vectors TasksKit holds too: the settings records' ids.
  it("is the sha256's first 16 bytes as a lowercase version 8 UUID", async () => {
    expect(
      await derivedUUID("collection:substrate.reamde.dev/tasks/task")
    ).toBe("2fb86e45-0509-8ec1-837f-efb918705f0b")
    expect(await derivedUUID("collection:ada.example.com/work/chore")).toBe(
      "8efb1c53-6e28-829b-b6e1-977f8479830d"
    )
    expect(await derivedUUID("preference")).toBe(
      "bc5ece60-fff8-8ba4-a305-bc10aebbf972"
    )
  })

  it("names one text alike every time", async () => {
    expect(await derivedUUID("x")).toBe(await derivedUUID("x"))
    expect(await derivedUUID("x")).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-8[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/
    )
  })
})
