import { describe, expect, it } from "vitest"

import { preventPinchZoom } from "./no-zoom"

describe("preventPinchZoom", () => {
  it("refuses Safari's pinch gestures", () => {
    const target = new EventTarget()
    preventPinchZoom(target)
    for (const type of ["gesturestart", "gesturechange", "gestureend"]) {
      const event = new Event(type, { cancelable: true })
      target.dispatchEvent(event)
      expect(event.defaultPrevented).toBe(true)
    }
  })
})
