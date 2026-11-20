import {
  DAY_MS,
  defaultTick,
  gutterWidth,
  isTap,
  stepDecimals,
  tickStep,
  EMPTY_LABEL_MIN_PX,
  emptyLabelFits,
  emptySpan,
  markerPath,
  nearestIndex,
  niceTicks,
  readoutAnchor,
  readoutSide,
  timeScale,
  valueDomain,
  yearBoundaries,
  timeTickCount,
  markerRadius,
  medianSpacing,
} from "./trendMath"
import { describe, expect, it } from "vitest"
import { clipBands as appClipBands, tickText } from "./chart"

const local = (y: number, m: number, d: number) =>
  new Date(y, m - 1, d).getTime()

describe("niceTicks", () => {
  it("ends on values equal up to float noise", () => {
    // A real repository had a flat series at 0.2 whose trend ended at
    // 0.20000000000000004; adding the step never moved the loop.
    const ticks = niceTicks(0.2, 0.2 + 4e-17)
    expect(ticks.length).toBeGreaterThan(0)
    expect(ticks.length).toBeLessThanOrEqual(8)
  })
  it("never draws more than eight ticks", () => {
    expect(niceTicks(0, 1e9).length).toBeLessThanOrEqual(8)
    expect(niceTicks(1e18, 1e18 + 100).length).toBeLessThanOrEqual(8)
  })
  it("encloses the range with round steps", () => {
    const ticks = niceTicks(2.6, 3.6)
    expect(ticks[0]).toBeLessThanOrEqual(2.6)
    expect(ticks[ticks.length - 1]).toBeGreaterThanOrEqual(3.6)
    expect(ticks.length).toBeGreaterThanOrEqual(3)
    expect(ticks.length).toBeLessThanOrEqual(7)
  })

  it("returns an exact zero, never a rounding residue", () => {
    expect(niceTicks(-0.3, 0.3)).toContain(0)
    expect(
      niceTicks(-0.3, 0.3).some((v) => v !== 0 && Math.abs(v) < 1e-9)
    ).toBe(false)
  })
})

describe("valueDomain", () => {
  it("pads values equal up to float noise like a single value", () => {
    const d = valueDomain([0.2, 0.20000000000000004])
    expect(d.lo).toBeLessThan(0.2)
    expect(d.hi).toBeGreaterThan(0.2)
    expect(d.ticks.length).toBeGreaterThan(1)
  })
  it("spans the values and widens to the ticks", () => {
    const d = valueDomain([3.1, 2.7, 3.6])
    expect(d.lo).toBeLessThanOrEqual(2.7)
    expect(d.hi).toBeGreaterThanOrEqual(3.6)
    expect(d.ticks[0]).toBe(d.lo)
    expect(d.ticks[d.ticks.length - 1]).toBe(d.hi)
  })

  it("joins valueRange into the domain", () => {
    const d = valueDomain([1.1, 1.4], { min: -1, max: 3 })
    expect(d.lo).toBeLessThanOrEqual(-1)
    expect(d.hi).toBeGreaterThanOrEqual(3)
    expect(niceTicks(-1, 3)).toEqual(d.ticks)
  })

  it("keeps a one-sided valueRange one-sided", () => {
    const d = valueDomain([40, 60], { min: 0 })
    expect(d.lo).toBe(0)
    expect(d.hi).toBeGreaterThanOrEqual(60)
    expect(d.hi).toBeLessThan(100)
  })

  it("pads a single value by 10 percent either side", () => {
    const d = valueDomain([50])
    expect(d.lo).toBeLessThanOrEqual(45)
    expect(d.hi).toBeGreaterThanOrEqual(55)
  })

  it("falls back to 0 to 1 with no values", () => {
    const d = valueDomain([])
    expect(d.lo).toBe(0)
    expect(d.hi).toBe(1)
  })
})

describe("yearBoundaries", () => {
  it("lists every 1 January inside a span of 18 months or more", () => {
    const start = local(2023, 11, 30)
    const end = local(2026, 10, 25)
    expect(yearBoundaries(start, end)).toEqual([
      local(2024, 1, 1),
      local(2025, 1, 1),
      local(2026, 1, 1),
    ])
  })

  it("draws none under 18 months", () => {
    expect(yearBoundaries(local(2025, 6, 1), local(2026, 10, 25))).toEqual([])
  })

  it("draws them from exactly 18 months", () => {
    const start = local(2025, 1, 15)
    const end = start + 18 * 30.4375 * DAY_MS
    expect(yearBoundaries(start, end)).toEqual([local(2026, 1, 1)])
  })

  it("excludes a 1 January that is the start itself", () => {
    expect(yearBoundaries(local(2024, 1, 1), local(2026, 1, 1))).toEqual([
      local(2025, 1, 1),
      local(2026, 1, 1),
    ])
  })

  it("returns none for an invalid range", () => {
    expect(yearBoundaries(Number.NaN, local(2026, 1, 1))).toEqual([])
  })
})

describe("emptySpan", () => {
  it("covers the axis start to the first point", () => {
    const start = local(2021, 10, 25)
    const first = local(2025, 7, 10)
    expect(emptySpan(start, first)).toEqual({ from: start, to: first })
  })

  it("is absent when the first point is under a day after the start", () => {
    const start = local(2024, 10, 25)
    expect(emptySpan(start, start + DAY_MS - 1)).toBeUndefined()
    expect(emptySpan(start, start)).toBeUndefined()
    expect(emptySpan(start, start + DAY_MS)).toBeDefined()
  })

  it("measures 80 percent of the plot for one year of data in five", () => {
    const start = local(2021, 10, 25)
    const end = local(2026, 10, 25)
    const first = local(2025, 10, 25)
    const x = timeScale(start, end, 44, 1000)
    const span = emptySpan(start, first)
    expect(span).toBeDefined()
    if (!span) return
    expect(x(span.to) - x(span.from)).toBeCloseTo(800, 0)
  })

  it("labels the span inside it only from 96 px", () => {
    expect(EMPTY_LABEL_MIN_PX).toBe(96)
    expect(emptyLabelFits(95.9)).toBe(false)
    expect(emptyLabelFits(96)).toBe(true)
  })

  it("needs room for the label text as well", () => {
    const label = "No results before Dec 2023"
    expect(emptyLabelFits(125, label.length)).toBe(false)
    expect(emptyLabelFits(400, label.length)).toBe(true)
  })
})

describe("timeScale", () => {
  it("maps the domain onto the plot", () => {
    const x = timeScale(0, 100, 44, 200)
    expect(x(0)).toBe(44)
    expect(x(50)).toBe(144)
    expect(x(100)).toBe(244)
  })

  it("centres everything on a zero-length domain", () => {
    expect(timeScale(5, 5, 10, 100)(5)).toBe(60)
  })
})

describe("nearestIndex", () => {
  it("snaps to the closest x", () => {
    expect(nearestIndex([10, 40, 90], 60)).toBe(1)
    expect(nearestIndex([10, 40, 90], 70)).toBe(2)
    expect(nearestIndex([], 10)).toBeNull()
  })
})

describe("readout placement", () => {
  it("keeps the bubble inside the frame at both edges", () => {
    expect(readoutAnchor(2, 160, 600)).toBe(84)
    expect(readoutAnchor(598, 160, 600)).toBe(516)
    expect(readoutAnchor(300, 160, 600)).toBe(300)
  })

  it("centres a bubble wider than the frame", () => {
    expect(readoutAnchor(10, 400, 300)).toBe(150)
  })

  it("opens above when it fits, else below", () => {
    expect(readoutSide(100, 60, 200)).toBe("top")
    expect(readoutSide(20, 60, 200)).toBe("bottom")
  })

  it("takes the roomier side when neither fits", () => {
    expect(readoutSide(40, 180, 200)).toBe("bottom")
    expect(readoutSide(150, 180, 200)).toBe("top")
  })
})

describe("markerPath", () => {
  it("draws a closed path for every shape", () => {
    for (const shape of [
      "circle",
      "square",
      "diamond",
      "triangle",
      "cross",
    ] as const) {
      const d = markerPath(shape, 10, 10)
      expect(d.startsWith("M")).toBe(true)
      expect(d.endsWith("Z")).toBe(true)
    }
  })

  it("keeps every shape within the 9 px marker box", () => {
    for (const shape of ["square", "diamond", "triangle", "cross"] as const) {
      const nums = markerPath(shape, 0, 0)
        .replace(/[MLZhv]/g, " ")
        .split(/[ ,]+/)
        .filter(Boolean)
        .map(Number)
      expect(Math.max(...nums.map(Math.abs))).toBeLessThanOrEqual(7.01)
    }
  })
})

describe("tick labels", () => {
  it("uses the step's own decimals", () => {
    expect(stepDecimals(5)).toBe(0)
    expect(stepDecimals(0.5)).toBe(1)
    expect(stepDecimals(0.25)).toBe(2)
    expect(stepDecimals(2.5)).toBe(1)
    expect(stepDecimals(0.1)).toBe(1)
  })

  it("never prints duplicates for a step finer than whole numbers", () => {
    const ticks = niceTicks(6, 8)
    const labels = ticks.map((v) => defaultTick(v, tickStep(ticks)))
    expect(new Set(labels).size).toBe(labels.length)
  })

  it("prints 14.25 as 14.25, not 14.3", () => {
    expect(defaultTick(14.25, 0.25)).toBe("14.25")
  })

  it("groups thousands in en-GB with no unit", () => {
    expect(defaultTick(16000, 2000)).toBe("16,000")
    expect(defaultTick(-1, 1)).toBe("-1")
  })

  it("reads the step from the ticks", () => {
    expect(tickStep([10, 12, 14])).toBe(2)
    expect(tickStep([3])).toBe(0)
  })
})

describe("gutterWidth", () => {
  it("is 45 px for labels up to five characters", () => {
    expect(gutterWidth(["0", "2.5", "10.25"])).toBe(45)
    expect(gutterWidth([])).toBe(45)
  })

  it("widens for long labels", () => {
    expect(gutterWidth(["10,000", "16,000"])).toBeGreaterThan(45)
    expect(gutterWidth(["100,000"])).toBeGreaterThan(gutterWidth(["10,000"]))
  })
})

describe("isTap", () => {
  it("tells a tap from a drag", () => {
    expect(isTap(3, 4)).toBe(true)
    expect(isTap(0, 240)).toBe(false)
  })
})

describe("timeTickCount", () => {
  it("draws three labels when they fit, else two", () => {
    // "Oct 2024" is 8 characters of 7 px: three need 3 * 56 + 2 * 12 = 192 px.
    expect(timeTickCount(192, 8)).toBe(3)
    expect(timeTickCount(191, 8)).toBe(2)
    // The 175 px plot of a wide-view card at 1180 px gets two labels.
    expect(timeTickCount(175, 8)).toBe(2)
  })

  it("keeps two labels on a compact card", () => {
    expect(timeTickCount(170, "12 Mar 2026".length)).toBe(2)
    expect(timeTickCount(600, "12 Mar 2026".length)).toBe(3)
  })
})

// The app's own chart helpers (chart.ts).
describe("clipBands", () => {
  const b = (from?: number, to?: number) => ({
    from,
    to,
    tone: "success",
    label: "x",
  })
  // Values 40 to 100: the span is 60, so the window is 25 to 115.
  const values = [40, 70, 100]
  it("keeps a band inside the data as it is", () => {
    expect(appClipBands([b(50, 80)], values)).toEqual([b(50, 80)])
  })
  it("opens a bound beyond the window, so no false edge is drawn", () => {
    expect(appClipBands([b(30, 375)], values)).toEqual([b(30, undefined)])
    expect(appClipBands([b(10, 60)], values)).toEqual([b(undefined, 60)])
  })
  it("drops a band entirely outside", () => {
    expect(appClipBands([b(375)], values)).toEqual([])
    expect(appClipBands([b(undefined, 20)], values)).toEqual([])
  })
  it("keeps open ends open", () => {
    expect(appClipBands([b(undefined, 50), b(90, undefined)], values)).toEqual([
      b(undefined, 50),
      b(90, undefined),
    ])
  })
  it("draws no band with no values", () => {
    expect(appClipBands([b(1, 2)], [])).toEqual([])
  })
})

describe("tickText", () => {
  it("prints the step's decimals with en-GB grouping", () => {
    expect(tickText(0.25, 0.05)).toBe("0.25")
    expect(tickText(3, 0.5)).toBe("3.0")
    expect(tickText(12500, 500)).toBe("12,500")
  })
  it("prints thousands as K when the step is 1,000 or more", () => {
    expect(tickText(1200, 1000)).toBe("1.2K")
    expect(tickText(3000, 1000)).toBe("3K")
    expect(tickText(2_500_000, 500_000)).toBe("2.5M")
  })
  it("never prints more than six characters, dropping decimals first", () => {
    expect(tickText(1234.5, 0.5)).toBe("1,235")
    expect(tickText(123456.7, 0.1).length).toBeLessThanOrEqual(6)
    for (const [v, s] of [
      [0.0012, 0.0001],
      [98765.4321, 0.0001],
      [-12345, 5],
    ])
      expect(tickText(v, s).length, `${v}`).toBeLessThanOrEqual(6)
  })
})

describe("markerRadius", () => {
  it("draws 2 px dots under 10 px of spacing and 4 px markers from 10 px", () => {
    expect(markerRadius(9)).toBe(2)
    expect(markerRadius(9.99)).toBe(2)
    expect(markerRadius(10)).toBe(4)
    expect(markerRadius(40)).toBe(4)
  })

  it("draws full markers for a single point", () => {
    expect(markerRadius(medianSpacing([120]))).toBe(4)
  })
})

describe("medianSpacing", () => {
  it("is the median gap between neighbouring x positions", () => {
    expect(medianSpacing([0, 10, 20, 60])).toBe(10)
    expect(medianSpacing([0, 4, 10, 16, 100])).toBe(6)
  })

  it("averages the two middle gaps for an even count", () => {
    expect(medianSpacing([0, 2, 6, 12, 20])).toBe(5)
  })

  it("is infinite for fewer than two points", () => {
    expect(medianSpacing([])).toBe(Number.POSITIVE_INFINITY)
    expect(medianSpacing([5])).toBe(Number.POSITIVE_INFINITY)
  })

  it("puts 182 nights on a 300 px plot in dots and 26 weeks in markers", () => {
    const nights = Array.from({ length: 182 }, (_, i) => (i * 300) / 181)
    const weeks = Array.from({ length: 26 }, (_, i) => (i * 300) / 25)
    expect(markerRadius(medianSpacing(nights))).toBe(2)
    expect(markerRadius(medianSpacing(weeks))).toBe(4)
  })
})

describe("time label spacing", () => {
  it("leaves at least 12 px between neighbouring labels when it draws three", () => {
    for (const chars of [8, 11]) {
      for (let plotW = 100; plotW <= 700; plotW++) {
        if (timeTickCount(plotW, chars) !== 3) continue
        const w = chars * 7
        // start at [0, w], middle centred on plotW / 2, end at [plotW - w, plotW]
        expect(plotW / 2 - w / 2 - w).toBeGreaterThanOrEqual(12)
      }
    }
  })
})
