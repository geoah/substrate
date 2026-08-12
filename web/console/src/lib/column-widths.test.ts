import { describe, expect, it } from "vitest"

import { distributeWidths, type ColumnWidthSpec } from "./column-widths"

function total(widths: Record<string, number>): number {
  return Object.values(widths).reduce((s, n) => s + n, 0)
}

describe("distributeWidths", () => {
  it("returns fixed columns verbatim when nothing flexes", () => {
    expect(
      distributeWidths(1000, [
        { id: "a", px: 100 },
        { id: "b", px: 200 },
      ])
    ).toEqual({ a: 100, b: 200 })
  })

  it("splits leftover equally among equal-weight columns", () => {
    const w = distributeWidths(1000, [
      { id: "fixed", px: 200 },
      { id: "a", flex: { min: 100 } },
      { id: "b", flex: { min: 100 } },
    ])
    expect(w).toEqual({ fixed: 200, a: 400, b: 400 })
  })

  it("splits leftover proportionally to weight", () => {
    const w = distributeWidths(900, [
      { id: "a", flex: { min: 100, weight: 2 } },
      { id: "b", flex: { min: 100, weight: 1 } },
    ])
    expect(w).toEqual({ a: 600, b: 300 })
  })

  it("clamps to min and redistributes the demand", () => {
    // b's fair share (100) sits below its min; a absorbs the difference.
    const w = distributeWidths(1000, [
      { id: "a", flex: { min: 100, weight: 9 } },
      { id: "b", flex: { min: 200, weight: 1 } },
    ])
    expect(w).toEqual({ a: 800, b: 200 })
  })

  it("clamps to max and redistributes the freed space", () => {
    const w = distributeWidths(1000, [
      { id: "a", flex: { min: 100, max: 300, weight: 1 } },
      { id: "b", flex: { min: 100, weight: 1 } },
    ])
    expect(w).toEqual({ a: 300, b: 700 })
  })

  it("floors everything at min when the mins overflow the container", () => {
    const w = distributeWidths(300, [
      { id: "fixed", px: 100 },
      { id: "a", flex: { min: 150 } },
      { id: "b", flex: { min: 150 } },
    ])
    expect(w).toEqual({ fixed: 100, a: 150, b: 150 })
    expect(total(w)).toBe(400) // the caller renders this width and scrolls
  })

  it("keeps caps hard while a sibling still has headroom", () => {
    // a clamps at min, b at max; the leftover belongs to a (headroom), not b.
    const w = distributeWidths(280, [
      { id: "a", flex: { min: 200, weight: 1 } },
      { id: "b", flex: { min: 50, max: 60, weight: 1 } },
    ])
    expect(w).toEqual({ a: 220, b: 60 })
  })

  it("goes soft on max only when every column is capped", () => {
    const w = distributeWidths(1000, [
      { id: "a", flex: { min: 100, max: 200, weight: 1 } },
      { id: "b", flex: { min: 100, max: 200, weight: 3 } },
    ])
    // 600 leftover past both caps redistributes 1:3.
    expect(w).toEqual({ a: 350, b: 650 })
  })

  it("fills the container exactly (integer widths, no drift)", () => {
    const specs: ColumnWidthSpec[] = [
      { id: "fixed", px: 137 },
      { id: "a", flex: { min: 90, weight: 1.5 } },
      { id: "b", flex: { min: 90, weight: 1 } },
      { id: "c", flex: { min: 90, weight: 1 } },
    ]
    for (const width of [703, 1024, 1280, 1531]) {
      const w = distributeWidths(width, specs)
      expect(total(w)).toBe(width)
      expect(Object.values(w).every(Number.isInteger)).toBe(true)
    }
  })

  it("defaults weight to 1 and ignores non-positive weights", () => {
    const w = distributeWidths(600, [
      { id: "a", flex: { min: 100 } },
      { id: "b", flex: { min: 100, weight: 0 } },
      { id: "c", flex: { min: 100, weight: -5 } },
    ])
    expect(w).toEqual({ a: 200, b: 200, c: 200 })
  })

  it("treats a zero or negative container as full overflow", () => {
    expect(
      distributeWidths(0, [
        { id: "a", flex: { min: 120 } },
        { id: "b", flex: { min: 80, max: 100 } },
      ])
    ).toEqual({ a: 120, b: 80 })
  })
})
