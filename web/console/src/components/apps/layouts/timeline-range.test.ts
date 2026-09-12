/** The range helpers, pure: the day key the URL carries, the four quick
 * ranges at a fixed now (month ends clamped), the window as days, a partial
 * or inverted address resolved, the bounds a read sends, and the label. */

import { describe, expect, it } from "vitest"

import {
  activeQuickRange,
  inRange,
  parseDayKey,
  QUICK_RANGES,
  rangeBounds,
  rangeLabel,
  resolveRange,
  windowRange,
} from "./timeline-range"

/** Saturday 12 September 2026, noon, local. */
const NOW = new Date(2026, 8, 12, 12).getTime()

function quick(key: string, now = NOW) {
  const q = QUICK_RANGES.find((r) => r.key === key)
  if (!q) throw new Error(`no quick range ${key}`)
  return q.range(now)
}

describe("parseDayKey", () => {
  it("accepts a real local calendar day and nothing else", () => {
    expect(parseDayKey("2026-09-12")).toBe("2026-09-12")
    expect(parseDayKey(" 2026-09-12 ")).toBe("2026-09-12")
    expect(parseDayKey("2026-02-30")).toBeUndefined()
    expect(parseDayKey("2026-13-01")).toBeUndefined()
    expect(parseDayKey("12/09/2026")).toBeUndefined()
    expect(parseDayKey("2026-9-1")).toBeUndefined()
    expect(parseDayKey("")).toBeUndefined()
    expect(parseDayKey(5)).toBeUndefined()
    expect(parseDayKey(null)).toBeUndefined()
  })
})

describe("quick ranges", () => {
  it("offers the four spans in chip order", () => {
    expect(QUICK_RANGES.map((q) => q.label)).toEqual([
      "This week",
      "This month",
      "Next 3 months",
      "Past month",
    ])
  })

  it("runs the week Monday to Sunday", () => {
    expect(quick("week")).toEqual({ from: "2026-09-07", to: "2026-09-13" })
    // A Monday starts its own week; a Sunday ends the one before.
    expect(quick("week", new Date(2026, 8, 7, 9).getTime()).from).toBe(
      "2026-09-07"
    )
    expect(quick("week", new Date(2026, 8, 13, 23).getTime())).toEqual({
      from: "2026-09-07",
      to: "2026-09-13",
    })
  })

  it("spans the whole calendar month", () => {
    expect(quick("month")).toEqual({ from: "2026-09-01", to: "2026-09-30" })
    expect(quick("month", new Date(2026, 1, 3).getTime())).toEqual({
      from: "2026-02-01",
      to: "2026-02-28",
    })
  })

  it("runs three months from today, the day before the same date", () => {
    expect(quick("next3")).toEqual({ from: "2026-09-12", to: "2026-12-11" })
    // 30 Nov + 3 months lands in February, which has no 30th: clamped.
    expect(quick("next3", new Date(2026, 10, 30).getTime())).toEqual({
      from: "2026-11-30",
      to: "2027-02-27",
    })
  })

  it("runs the past month up to today, clamped at a short month", () => {
    expect(quick("past")).toEqual({ from: "2026-08-12", to: "2026-09-12" })
    expect(quick("past", new Date(2026, 2, 31).getTime())).toEqual({
      from: "2026-02-28",
      to: "2026-03-31",
    })
  })

  it("names the quick range a range is, day for day", () => {
    expect(activeQuickRange(quick("month"), NOW)).toBe("month")
    expect(activeQuickRange(quick("past"), NOW)).toBe("past")
    expect(
      activeQuickRange({ from: "2026-09-01", to: "2026-09-29" }, NOW)
    ).toBeUndefined()
  })
})

describe("window and address", () => {
  const spec = { window: { past: "P7D", future: "P30D" } } as Parameters<
    typeof windowRange
  >[0]

  it("reads the window as the days its two ends fall on", () => {
    expect(windowRange(spec, NOW)).toEqual({
      from: "2026-09-05",
      to: "2026-10-12",
    })
    expect(windowRange({ ...spec, window: {} }, NOW)).toEqual({
      from: "2026-09-05",
      to: "2026-10-12",
    })
  })

  it("resolves nothing from an empty address", () => {
    const window = windowRange(spec, NOW)
    expect(resolveRange({ from: null, to: null }, window)).toBeUndefined()
  })

  it("completes a lone end from the window and rights an inverted pair", () => {
    const window = windowRange(spec, NOW)
    expect(resolveRange({ from: "2026-09-10", to: null }, window)).toEqual({
      from: "2026-09-10",
      to: "2026-10-12",
    })
    expect(resolveRange({ from: null, to: "2026-09-20" }, window)).toEqual({
      from: "2026-09-05",
      to: "2026-09-20",
    })
    expect(
      resolveRange({ from: "2026-09-20", to: "2026-09-10" }, window)
    ).toEqual({ from: "2026-09-10", to: "2026-09-20" })
  })

  it("sends the local start of from and the start of the day after to", () => {
    expect(rangeBounds({ from: "2026-09-01", to: "2026-09-30" })).toEqual({
      gte: new Date(2026, 8, 1).toISOString(),
      lt: new Date(2026, 9, 1).toISOString(),
    })
    expect(rangeBounds({ from: "2026-12-31", to: "2026-12-31" })).toEqual({
      gte: new Date(2026, 11, 31).toISOString(),
      lt: new Date(2027, 0, 1).toISOString(),
    })
  })

  it("holds a day in a range at both ends", () => {
    const range = { from: "2026-09-05", to: "2026-10-12" }
    expect(inRange("2026-09-05", range)).toBe(true)
    expect(inRange("2026-10-12", range)).toBe(true)
    expect(inRange("2026-09-04", range)).toBe(false)
    expect(inRange("2026-10-13", range)).toBe(false)
  })
})

describe("rangeLabel", () => {
  it("reads day then month, the year joining once it is not this year", () => {
    expect(rangeLabel({ from: "2026-09-12", to: "2026-10-12" }, NOW)).toBe(
      "12 Sep – 12 Oct"
    )
    expect(rangeLabel({ from: "2026-12-28", to: "2027-01-03" }, NOW)).toBe(
      "28 Dec – 3 Jan 2027"
    )
    expect(rangeLabel({ from: "2026-09-12", to: "2026-09-12" }, NOW)).toBe(
      "12 Sep"
    )
  })
})
