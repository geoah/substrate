import { afterEach, describe, expect, it, vi } from "vitest"

import { scrollMotion } from "./motion"

afterEach(() => vi.unstubAllGlobals())

function stubMotion(reduce: boolean) {
  vi.stubGlobal("window", {
    matchMedia: (q: string) => ({
      matches: reduce && q === "(prefers-reduced-motion: reduce)",
    }),
  })
}

describe("scrollMotion", () => {
  it("scrolls smoothly by default", () => {
    stubMotion(false)
    expect(scrollMotion()).toBe("smooth")
  })

  it("jumps when the reader asked for less motion", () => {
    stubMotion(true)
    expect(scrollMotion()).toBe("auto")
  })
})
