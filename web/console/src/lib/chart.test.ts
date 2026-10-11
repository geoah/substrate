/** The chart arithmetic, checked as the numbers and path strings an SVG
 * draws. */

import { describe, expect, it } from "vitest"

import {
  bandPath,
  barPath,
  bucketAt,
  compactNumber,
  labelEvery,
  linePath,
  linearScale,
  niceDomain,
  niceTicks,
  rectPath,
  tickLabels,
} from "@/lib/chart"

describe("linearScale", () => {
  it("maps a domain onto pixels and back, flipped for a y axis", () => {
    const y = linearScale([0, 100], [200, 0])
    expect(y(0)).toBe(200)
    expect(y(50)).toBe(100)
    expect(y.invert(50)).toBe(75)
  })

  it("never divides by an empty domain", () => {
    const x = linearScale([5, 5], [0, 10])
    expect(Number.isFinite(x(5))).toBe(true)
  })
})

describe("ticks", () => {
  it("steps by 1, 2 or 5 times a power of ten", () => {
    expect(niceTicks(0, 100, 4)).toEqual([0, 20, 40, 60, 80, 100])
    expect(niceTicks(55, 171, 4)).toEqual([40, 60, 80, 100, 120, 140, 160, 180])
    expect(niceTicks(0.1, 0.3, 2)).toEqual([0.1, 0.2, 0.3])
  })

  it("widens a flat series so it still has an axis", () => {
    expect(niceTicks(72.4, 72.4).length).toBeGreaterThan(1)
  })

  it("anchors a bar's domain at zero", () => {
    expect(niceDomain([9812, 7004, 12450], { zero: true }).domain).toEqual([
      0, 14000,
    ])
    expect(niceDomain([72.1, 73.0]).domain[0]).toBeGreaterThan(70)
    expect(niceDomain([]).domain).toEqual([0, 1])
  })

  it("covers a domain narrower than the float spacing at its magnitude", () => {
    // 16384 is one float step at 1e20, so a round step of 5000 adds
    // nothing there: the ticks fall back to the domain's own ends.
    const { domain, ticks } = niceDomain([1e20, 1e20 + 16384])
    expect(domain.every(Number.isFinite)).toBe(true)
    expect(domain[0]).toBeLessThanOrEqual(1e20)
    expect(domain[1]).toBeGreaterThanOrEqual(1e20 + 16384)
    expect(domain[0]).toBeLessThan(domain[1])
    for (let i = 1; i < ticks.length; i++) {
      expect(ticks[i]).toBeGreaterThan(ticks[i - 1])
    }
    expect(() => tickLabels(ticks)).not.toThrow()
  })

  it("ticks and labels a domain finer than toFixed can print", () => {
    const { domain, ticks } = niceDomain([1e-101, 2e-101])
    expect(domain[0]).toBeLessThanOrEqual(1e-101)
    expect(domain[1]).toBeGreaterThanOrEqual(2e-101)
    expect(ticks.length).toBeGreaterThan(2)
    for (let i = 1; i < ticks.length; i++) {
      expect(ticks[i]).toBeGreaterThan(ticks[i - 1])
    }
    const labels = tickLabels(ticks)
    expect(new Set(labels).size).toBe(labels.length)
    expect(labels[0]).toMatch(/e-10[01]$/)
  })

  it("keeps a tick at zero clean of float noise", () => {
    expect(niceTicks(-0.5, 0.5)).toEqual([-0.6, -0.4, -0.2, 0, 0.2, 0.4, 0.6])
  })

  it("labels compactly from a thousand", () => {
    expect(compactNumber(0)).toBe("0")
    expect(compactNumber(950)).toBe("950")
    expect(compactNumber(1200)).toBe("1.2K")
    expect(compactNumber(12000)).toBe("12K")
    expect(compactNumber(3_400_000)).toBe("3.4M")
    expect(compactNumber(0.25)).toBe("0.25")
  })

  it("prints each tick as precisely as the step between them", () => {
    expect(tickLabels([72, 72.2, 72.4, 72.6])).toEqual([
      "72.0",
      "72.2",
      "72.4",
      "72.6",
    ])
    expect(tickLabels([0, 20, 40])).toEqual(["0", "20", "40"])
    expect(tickLabels([0, 5000, 10000])).toEqual(["0", "5K", "10K"])
  })

  it("labels every k-th bucket so no more than the room allows", () => {
    expect(labelEvery(168, 10)).toBe(17)
    expect(labelEvery(5, 10)).toBe(1)
  })
})

describe("paths", () => {
  it("breaks a line where the data has a gap", () => {
    expect(
      linePath([
        { x: 0, y: 10 },
        { x: 10, y: 20 },
        null,
        { x: 30, y: 5 },
        { x: 40, y: 6 },
      ])
    ).toBe("M0 10L10 20M30 5L40 6")
  })

  it("closes one band per unbroken run", () => {
    expect(
      bandPath([
        { x: 0, y0: 10, y1: 2 },
        { x: 10, y0: 12, y1: 4 },
        null,
        { x: 30, y0: 8, y1: 6 },
      ])
    ).toBe("M0 2L10 4L10 12L0 10ZM30 6L30 8Z")
  })

  it("rounds a bar's data end and squares its baseline", () => {
    const up = barPath(10, 20, 200, 100)
    expect(up.startsWith("M10 200V104Q10 100 14 100")).toBe(true)
    expect(up.endsWith("V200Z")).toBe(true)
    const down = barPath(10, 20, 100, 150)
    expect(down.startsWith("M10 100V146Q10 150 14 150")).toBe(true)
    expect(barPath(0, 10, 50, 50)).toBe("")
    // A bar shorter than its corners rounds less.
    expect(barPath(0, 20, 100, 98)).toContain("V100Q0 98 2 98")
  })

  it("draws a stack's inner segment square", () => {
    expect(rectPath(5, 10, 100, 80)).toBe("M5 100V80H15V100Z")
    expect(rectPath(5, 10, 80, 80)).toBe("")
  })

  it("finds the bucket under the pointer", () => {
    const windows = [
      { x0: 0, x1: 10 },
      { x0: 10, x1: 20 },
    ]
    expect(bucketAt(5, windows)).toBe(0)
    expect(bucketAt(10, windows)).toBe(1)
    expect(bucketAt(25, windows)).toBe(-1)
  })
})
