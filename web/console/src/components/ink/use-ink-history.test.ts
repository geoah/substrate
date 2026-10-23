/** The editor's history: a stroke is one step, an eraser gesture is one step
 * however many strokes it takes, a gesture that erases nothing is no step,
 * and a new change after an undo drops what could have been redone. */

import { describe, expect, it } from "vitest"

import { newInkDocument, type InkDocument, type InkStroke } from "@/lib/ink"

import { stylusAngles } from "./use-ink-drawing"
import { inkHistoryReducer } from "./use-ink-history"

const stroke = (id: string, x: number): InkStroke => ({
  id,
  tool: "pen",
  color: "#000000",
  opacity: 1,
  baseWidth: 2,
  points: [
    [x, 100, 0.5],
    [x, 200, 0.5],
  ],
})

function start() {
  return {
    document: newInkDocument(),
    past: [] as InkDocument[],
    future: [] as InkDocument[],
    pending: false,
  }
}

const strokeIds = (state: ReturnType<typeof start>) =>
  state.document.pages[0].strokes.map((s) => s.id)

describe("inkHistoryReducer", () => {
  it("undoes and redoes one stroke at a time", () => {
    let s = start()
    s = inkHistoryReducer(s, {
      type: "stroke",
      page: 0,
      stroke: stroke("a", 100),
    })
    s = inkHistoryReducer(s, {
      type: "stroke",
      page: 0,
      stroke: stroke("b", 300),
    })
    expect(strokeIds(s)).toEqual(["a", "b"])
    s = inkHistoryReducer(s, { type: "undo" })
    expect(strokeIds(s)).toEqual(["a"])
    s = inkHistoryReducer(s, { type: "redo" })
    expect(strokeIds(s)).toEqual(["a", "b"])
  })

  it("makes one eraser gesture one step", () => {
    let s = start()
    s = inkHistoryReducer(s, {
      type: "stroke",
      page: 0,
      stroke: stroke("a", 100),
    })
    s = inkHistoryReducer(s, {
      type: "stroke",
      page: 0,
      stroke: stroke("b", 300),
    })
    s = inkHistoryReducer(s, { type: "gesture" })
    s = inkHistoryReducer(s, { type: "erase", page: 0, x: 100, y: 150 })
    s = inkHistoryReducer(s, { type: "erase", page: 0, x: 300, y: 150 })
    expect(strokeIds(s)).toEqual([])
    s = inkHistoryReducer(s, { type: "undo" })
    expect(strokeIds(s)).toEqual(["a", "b"])
  })

  it("records no step for a gesture that erased nothing", () => {
    let s = start()
    s = inkHistoryReducer(s, {
      type: "stroke",
      page: 0,
      stroke: stroke("a", 100),
    })
    const before = s
    s = inkHistoryReducer(s, { type: "gesture" })
    s = inkHistoryReducer(s, { type: "erase", page: 0, x: 900, y: 900 })
    expect(s.past).toBe(before.past)
    expect(s.document).toBe(before.document)
  })

  it("drops the redo branch when a new change follows an undo", () => {
    let s = start()
    s = inkHistoryReducer(s, {
      type: "stroke",
      page: 0,
      stroke: stroke("a", 100),
    })
    s = inkHistoryReducer(s, { type: "undo" })
    s = inkHistoryReducer(s, {
      type: "stroke",
      page: 0,
      stroke: stroke("c", 500),
    })
    expect(s.future).toEqual([])
    expect(inkHistoryReducer(s, { type: "redo" })).toBe(s)
    expect(strokeIds(s)).toEqual(["c"])
  })

  it("adds a page and changes a page's paper as steps", () => {
    let s = start()
    s = inkHistoryReducer(s, { type: "addPage", background: "grid" })
    s = inkHistoryReducer(s, {
      type: "background",
      page: 0,
      background: "dots",
    })
    expect(s.document.pages.map((p) => p.background.type)).toEqual([
      "dots",
      "grid",
    ])
    s = inkHistoryReducer(inkHistoryReducer(s, { type: "undo" }), {
      type: "undo",
    })
    expect(s.document.pages).toHaveLength(1)
  })
})

describe("stylusAngles", () => {
  it("gives a mouse the spec's defaults", () => {
    expect(stylusAngles({ pointerType: "mouse", tiltX: 30 })).toEqual([
      0,
      Math.PI / 2,
    ])
  })

  it("prefers the browser's own angles, else converts tilt", () => {
    expect(
      stylusAngles({ pointerType: "pen", altitudeAngle: 1, azimuthAngle: 2 })
    ).toEqual([2, 1])
    const [azimuth, altitude] = stylusAngles({
      pointerType: "pen",
      tiltX: 45,
      tiltY: 0,
    })
    expect(azimuth).toBeCloseTo(0)
    expect(altitude).toBeCloseTo(Math.PI / 4)
  })
})
