// @vitest-environment jsdom
import { cleanup, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { useVisualViewportHeight } from "./use-visual-viewport"

class FakeViewport extends EventTarget {
  height = window.innerHeight
}

describe("useVisualViewportHeight", () => {
  let vv: FakeViewport

  beforeEach(() => {
    vv = new FakeViewport()
    vi.stubGlobal("visualViewport", vv)
    vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => {
      cb(0)
      return 0
    })
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  const height = () =>
    document.documentElement.style.getPropertyValue("--visual-viewport-height")

  it("holds the shell to the visible height while a bar covers the page, and lets go after", () => {
    const scrollTo = vi.fn()
    vi.stubGlobal("scrollTo", scrollTo)
    vi.stubGlobal("scrollY", 70)
    renderHook(() => useVisualViewportHeight())
    expect(height()).toBe("")

    vv.height = window.innerHeight - 70
    vv.dispatchEvent(new Event("resize"))
    expect(height()).toBe(`${window.innerHeight - 70}px`)
    expect(scrollTo).toHaveBeenCalledWith(0, 0)

    vv.height = window.innerHeight
    vv.dispatchEvent(new Event("resize"))
    expect(height()).toBe("")
  })
})
