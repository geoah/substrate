import { describe, expect, it } from "vitest"

import { relativeRecordName, underHost } from "./dns"

describe("relativeRecordName", () => {
  it("splits off the last two labels as the zone", () => {
    expect(relativeRecordName("_substrate.ada.example.org")).toEqual({
      zone: "example.org",
      relative: "_substrate.ada",
    })
    expect(relativeRecordName("_substrate.example.org.")).toEqual({
      zone: "example.org",
      relative: "_substrate",
    })
  })

  it("has nothing to shorten under two labels", () => {
    expect(relativeRecordName("example.org")).toBeNull()
  })
})

describe("underHost", () => {
  it("holds one label under the host and nothing else", () => {
    expect(underHost("ada.example.com", "example.com")).toBe(true)
    expect(underHost("ada.example.com", "Example.com.")).toBe(true)
    expect(underHost("a.b.example.com", "example.com")).toBe(false)
    expect(underHost("example.com", "example.com")).toBe(false)
    expect(underHost("ada.example.org", "example.com")).toBe(false)
    expect(underHost("ada.example.com", "")).toBe(false)
  })
})
