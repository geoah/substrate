import { describe, expect, it } from "vitest"
import { derivedUUID, sha256, uuidv7 } from "./ids"

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

describe("sha256", () => {
  const hex = (b: Uint8Array) =>
    Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("")
  it("is FIPS 180-4's, across one and several blocks", () => {
    expect(hex(sha256(new TextEncoder().encode("abc")))).toBe(
      "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
    )
    expect(hex(sha256(new Uint8Array()))).toBe(
      "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    )
    expect(
      hex(
        sha256(
          new TextEncoder().encode(
            "abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq"
          )
        )
      )
    ).toBe("248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1")
  })
})

describe("derivedUUID", () => {
  // The vectors TasksKit holds too: the settings records' ids.
  it("is the sha256's first 16 bytes as a lowercase version 8 UUID", () => {
    expect(derivedUUID("collection:substrate.reamde.dev/tasks/task")).toBe(
      "2fb86e45-0509-8ec1-837f-efb918705f0b"
    )
    expect(derivedUUID("collection:ada.example.com/work/chore")).toBe(
      "8efb1c53-6e28-829b-b6e1-977f8479830d"
    )
    expect(derivedUUID("preference")).toBe(
      "bc5ece60-fff8-8ba4-a305-bc10aebbf972"
    )
  })

  it("names one text alike every time", () => {
    expect(derivedUUID("x")).toBe(derivedUUID("x"))
    expect(derivedUUID("x")).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-8[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/
    )
  })
})
