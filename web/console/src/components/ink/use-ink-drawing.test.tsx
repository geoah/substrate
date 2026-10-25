// @vitest-environment jsdom
/** A finger on an ink page scrolls it unless "Draw with finger" is on: the
 * page turns off the browser's touch scrolling so a stroke is never read as
 * a scroll, so the finger scrolls the page itself, by its own movement. Where
 * a finger draws, a second finger scrolls instead. While a pen is down, a
 * finger (the palm) does nothing, and a scroll it began stops. */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { useInkDrawing } from "./use-ink-drawing"

function Surface({
  onStroke,
  onGestureStart,
  fingerDraws = false,
}: {
  onStroke: () => void
  onGestureStart: () => void
  fingerDraws?: boolean
}) {
  const { handlers } = useInkDrawing({
    settings: { instrument: "pen", color: "#000000", width: 2 },
    enabled: true,
    fingerDraws,
    onStroke,
    onErase: () => undefined,
    onGestureStart,
  })
  return <svg data-testid="page" {...handlers} />
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

/** The page, with the scroll it would do recorded. jsdom lays nothing out,
 * so the page has no screen transform. */
function setup(fingerDraws = false) {
  const scrollBy = vi.fn()
  const scroller = document.scrollingElement ?? document.documentElement
  Object.defineProperty(scroller, "scrollBy", {
    value: scrollBy,
    configurable: true,
  })
  const onStroke = vi.fn()
  const onGestureStart = vi.fn()
  render(
    <Surface
      onStroke={onStroke}
      onGestureStart={onGestureStart}
      fingerDraws={fingerDraws}
    />
  )
  const page = screen.getByTestId("page")
  Object.defineProperty(page, "getScreenCTM", {
    value: () => null,
    configurable: true,
  })
  return { page, scrollBy, onStroke, onGestureStart }
}

/** Points read through the page's transform: jsdom has none, so client
 * coordinates stand in for page coordinates and a stroke can begin. */
function withGeometry(page: HTMLElement) {
  vi.stubGlobal(
    "DOMPoint",
    class {
      x: number
      y: number
      constructor(x = 0, y = 0) {
        this.x = x
        this.y = y
      }
      matrixTransform() {
        return this
      }
    }
  )
  Object.defineProperty(page, "getScreenCTM", {
    value: () => ({ inverse: () => ({}) }),
  })
}

describe("useInkDrawing", () => {
  it("scrolls the page under a finger", () => {
    const { page, scrollBy, onStroke, onGestureStart } = setup()
    fireEvent.pointerDown(page, {
      pointerId: 2,
      pointerType: "touch",
      clientX: 100,
      clientY: 300,
    })
    fireEvent.pointerMove(page, {
      pointerId: 2,
      pointerType: "touch",
      clientX: 90,
      clientY: 220,
    })
    fireEvent.pointerUp(page, { pointerId: 2, pointerType: "touch" })

    // Dragging up by 80 scrolls down by 80, as a native scroll would.
    expect(scrollBy).toHaveBeenCalledWith(10, 80)
    expect(onGestureStart).not.toHaveBeenCalled()
    expect(onStroke).not.toHaveBeenCalled()
  })

  it("draws with a finger when Draw with finger is on", () => {
    const { page, scrollBy, onGestureStart } = setup(true)
    fireEvent.pointerDown(page, { pointerId: 3, pointerType: "touch" })
    fireEvent.pointerMove(page, {
      pointerId: 3,
      pointerType: "touch",
      clientY: 50,
    })
    expect(onGestureStart).toHaveBeenCalledTimes(1)
    expect(scrollBy).not.toHaveBeenCalled()
  })

  it("does nothing under a finger while the pen is down", () => {
    const { page, scrollBy, onGestureStart } = setup()
    fireEvent.pointerDown(page, { pointerId: 4, pointerType: "pen" })
    onGestureStart.mockClear()
    fireEvent.pointerDown(page, {
      pointerId: 5,
      pointerType: "touch",
      clientY: 300,
    })
    fireEvent.pointerMove(page, {
      pointerId: 5,
      pointerType: "touch",
      clientY: 200,
    })
    expect(scrollBy).not.toHaveBeenCalled()
    expect(onGestureStart).not.toHaveBeenCalled()
  })

  it("stops a scroll the palm began when the pen lands", () => {
    const { page, scrollBy } = setup()
    fireEvent.pointerDown(page, {
      pointerId: 6,
      pointerType: "touch",
      clientY: 300,
    })
    fireEvent.pointerDown(page, { pointerId: 7, pointerType: "pen" })
    fireEvent.pointerMove(page, {
      pointerId: 6,
      pointerType: "touch",
      clientY: 250,
    })
    expect(scrollBy).not.toHaveBeenCalled()
  })

  it("drops a finger's stroke and scrolls when a second finger lands", () => {
    const { page, scrollBy, onStroke } = setup(true)
    withGeometry(page)
    const finger = (pointerId: number, clientX: number, clientY: number) => ({
      pointerId,
      pointerType: "touch",
      clientX,
      clientY,
    })
    fireEvent.pointerDown(page, finger(8, 100, 100))
    fireEvent.pointerMove(page, finger(8, 120, 110))
    fireEvent.pointerDown(page, finger(9, 300, 300))
    fireEvent.pointerMove(page, finger(9, 300, 240))
    fireEvent.pointerMove(page, finger(8, 140, 120))
    fireEvent.pointerUp(page, finger(8, 140, 120))
    fireEvent.pointerUp(page, finger(9, 300, 240))
    expect(scrollBy).toHaveBeenCalledWith(0, 60)
    expect(onStroke).not.toHaveBeenCalled()

    // With both lifted, one finger draws again.
    fireEvent.pointerDown(page, finger(10, 100, 100))
    fireEvent.pointerMove(page, finger(10, 150, 100))
    fireEvent.pointerUp(page, finger(10, 150, 100))
    expect(onStroke).toHaveBeenCalledTimes(1)
  })
})
