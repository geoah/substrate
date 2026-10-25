/** The `.inkd` rendering model in numbers: the pen's width follows pressure
 * and the other tools' darkness does, a stroke without length is a dot, the
 * paper patterns stop inside the page, and an eraser hits what it touches. */

import { describe, expect, it } from "vitest"

import type { InkPage, InkStroke } from "./ink"
import {
  backgroundPattern,
  cachedInkShape,
  cropBox,
  inkBounds,
  inkShape,
  lineCount,
  MAX_PATTERN_LINES,
  strokeAlpha,
  strokeWidthAt,
  strokesHit,
} from "./ink-render"

const stroke = (
  id: string,
  points: number[][],
  extra: Partial<InkStroke> = {}
): InkStroke => ({
  id,
  tool: "pen",
  color: "#1C1C1E",
  opacity: 1,
  baseWidth: 2,
  points,
  ...extra,
})

describe("width and alpha (§8)", () => {
  it("thins and thickens a pen from 0.25 to 1.75 times its base width", () => {
    expect(strokeWidthAt("pen", 2, 0)).toBe(0.5)
    expect(strokeWidthAt("pen", 2, 0.5)).toBe(2)
    expect(strokeWidthAt("pen", 2, 1)).toBe(3.5)
    expect(strokeWidthAt("pencil", 2, 1)).toBe(2)
    expect(strokeWidthAt("marker", 20, 0)).toBe(20)
  })

  it("darkens pencil and marker with pressure, never the pen", () => {
    expect(strokeAlpha("pen", 0.8, 0)).toBe(0.8)
    expect(strokeAlpha("pencil", 1, 0)).toBeCloseTo(0.55)
    expect(strokeAlpha("pencil", 1, 1)).toBeCloseTo(1)
    expect(strokeAlpha("marker", 1, 0.5)).toBeCloseTo(0.675)
  })
})

describe("inkShape", () => {
  it("draws a stroke without length as a dot as wide as the stroke", () => {
    expect(inkShape(stroke("s", [[10, 10, 1]]))).toEqual({
      type: "dot",
      cx: 10,
      cy: 10,
      r: 1.75,
      fill: "#1C1C1E",
      opacity: 1,
      multiply: false,
    })
  })

  it("fills a closed outline, multiplied for a marker", () => {
    const shape = inkShape(
      stroke(
        "m",
        [
          [0, 0, 0.5],
          [50, 0, 0.5],
          [100, 10, 0.5],
        ],
        { tool: "marker", color: "#FFD60A", baseWidth: 20 }
      )
    )
    expect(shape?.type).toBe("path")
    expect(shape?.multiply).toBe(true)
    expect(shape?.opacity).toBeCloseTo(0.675)
    expect(shape?.type === "path" && shape.d).toMatch(/^M.*Z$/)
  })

  it("draws nothing for a stroke without a usable point", () => {
    expect(inkShape(stroke("s", []))).toBeNull()
  })

  it("is computed once per stroke object", () => {
    const s = stroke("s", [
      [0, 0],
      [10, 10],
    ])
    expect(cachedInkShape(s)).toBe(cachedInkShape(s))
    expect(cachedInkShape({ ...s })).not.toBe(cachedInkShape(s))
  })
})

describe("backgroundPattern (§5)", () => {
  it("draws lines strictly inside the page", () => {
    expect(lineCount(100, 32)).toBe(3)
    expect(lineCount(96, 32)).toBe(2)
    expect(lineCount(10, 32)).toBe(0)
    expect(backgroundPattern({ type: "lined" }, 100, 96)).toEqual({
      type: "lines",
      d: "M0 32H100M0 64H100",
      color: "#D0D7E2",
    })
  })

  it("adds the verticals for grid, and dots at the crossings", () => {
    const grid = backgroundPattern({ type: "grid", spacing: 50 }, 120, 120)
    expect(grid).toEqual({
      type: "lines",
      d: "M0 50H120M0 100H120M50 0V120M100 0V120",
      color: "#D0D7E2",
    })
    expect(
      backgroundPattern({ type: "dots", color: "#000000" }, 100, 100)
    ).toEqual({
      type: "dots",
      spacing: 32,
      columns: 3,
      rows: 3,
      color: "#000000",
    })
    expect(backgroundPattern({ type: "blank" }, 100, 100)).toEqual({
      type: "none",
    })
  })
})

describe("a pattern too dense to draw", () => {
  const none = { type: "none" }

  it("is blank paper, for a tiny spacing or a vast page", () => {
    for (const type of ["lined", "grid", "dots"]) {
      for (const [spacing, width, height] of [
        [1e-300, 1240, 1754],
        [0.001, 1240, 1754],
        [32, 1e300, 1e300],
        [1e-300, Number.MAX_VALUE, Number.MAX_VALUE],
      ])
        expect(
          backgroundPattern({ type, spacing }, width, height),
          `${type} ${spacing} ${width}x${height}`
        ).toEqual(none)
    }
  })

  it("never builds the lines of blank paper", () => {
    expect(
      backgroundPattern({ type: "blank", spacing: 1e-300 }, 1e300, 1e300)
    ).toEqual(none)
  })

  it("still draws a pattern right at the bound", () => {
    const lined = backgroundPattern(
      { type: "lined", spacing: 1 },
      10,
      MAX_PATTERN_LINES + 1
    )
    expect(lined.type).toBe("lines")
    expect(lined.type === "lines" && lined.d.split("M").length - 1).toBe(
      MAX_PATTERN_LINES
    )
    // A lined page has no verticals, so a narrow spacing on a wide page is
    // bounded by its height alone.
    expect(
      backgroundPattern({ type: "lined", spacing: 1 }, 1e6, 100).type
    ).toBe("lines")
  })
})

describe("bounds and crop", () => {
  const page = (strokes: InkStroke[]): InkPage => ({
    id: "p",
    background: { type: "blank" },
    strokes,
  })

  it("covers the ink and half the widest stroke", () => {
    expect(
      inkBounds([
        stroke("s", [
          [100, 100],
          [200, 150],
        ]),
      ])
    ).toEqual({ x: 98.25, y: 98.25, width: 103.5, height: 53.5 })
    expect(inkBounds([])).toBeNull()
  })

  it("crops a preview to the ink plus a margin, kept on the page", () => {
    const box = cropBox(
      page([
        stroke("s", [
          [10, 100],
          [200, 150],
        ]),
      ]),
      1240,
      1754
    )
    expect(box.x).toBe(0)
    expect(box.y).toBeCloseTo(66.25)
    expect(cropBox(page([]), 1240, 1754)).toEqual({
      x: 0,
      y: 0,
      width: 1240,
      height: 1754,
    })
  })
})

describe("strokesHit", () => {
  const strokes = [
    stroke("line", [
      [0, 0],
      [100, 0],
    ]),
    stroke("dot", [[300, 300]]),
  ]

  it("hits a stroke within reach of its line, not just its points", () => {
    // Reach is the radius plus half the pen's widest width: 5 + 1.75.
    expect(strokesHit(strokes, 50, 6.5, 5)).toEqual(["line"])
    expect(strokesHit(strokes, 50, 7, 5)).toEqual([])
    expect(strokesHit(strokes, 50, 20, 5)).toEqual([])
  })

  it("hits a one-point stroke around its point", () => {
    expect(strokesHit(strokes, 304, 300, 3)).toEqual(["dot"])
  })
})
