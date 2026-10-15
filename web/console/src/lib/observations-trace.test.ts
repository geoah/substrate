/** A trace's own values: placed in time, broken past a level's hold, and
 * the managed stats. */

import { describe, expect, it } from "vitest"

import { durationMs } from "@/lib/observations-time"
import {
  breakAtGaps,
  stddevOf,
  tracePoints,
  traceStats,
} from "@/lib/observations-trace"
import { HEART_TRACE } from "@/lib/observations.test-fixtures"

describe("a trace's own values", () => {
  it("places irregular values at `at` plus their offsets", () => {
    const points = tracePoints(HEART_TRACE.properties)
    expect(points).toHaveLength(7)
    expect(new Date(points[4].at).toISOString()).toBe(
      "2026-10-07T06:30:00.000Z"
    )
    expect(points[6].value).toBe("124")
  })

  it("places regular values at `at` plus `every` times their position", () => {
    const points = tracePoints({
      at: "2026-10-07T06:00:00Z",
      every: "PT1S",
      values: ["100", null, "102"],
    })
    expect(points.map((p) => p.at - points[0].at)).toEqual([0, 1000, 2000])
    expect(points[1].value).toBeNull()
  })

  it("breaks a level where two values sit further apart than its hold", () => {
    const points = tracePoints(HEART_TRACE.properties)
    const held = breakAtGaps(points, durationMs("PT4M"))
    expect(held.indexOf(null)).toBe(4)
    expect(held.filter((p) => p === null)).toHaveLength(1)
    expect(breakAtGaps(points, undefined)).toHaveLength(7)
  })

  it("reads the managed stats and the spread they hold", () => {
    const stats = traceStats(HEART_TRACE.properties.stats)!
    expect(stats).toMatchObject({ count: 1812, min: "55", max: "171" })
    expect(stddevOf(stats)).toBeCloseTo(Math.sqrt(2203418.2 / 1812), 6)
  })
})
