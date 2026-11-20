import { describe, expect, it } from "vitest"
import { emptySpan, inDomain, isRangeId, pageDomain } from "./timeDomain"

const today = new Date(2026, 9, 25, 8, 0, 0)
const results = [
  { at: new Date(2024, 0, 15, 9, 0).toISOString() },
  { at: new Date(2023, 10, 2, 9, 0).toISOString() },
  { at: new Date(2026, 9, 20, 9, 0).toISOString() },
]

describe("pageDomain", () => {
  it("ends at noon on today's day", () => {
    const d = pageDomain("2Y", today, results)
    expect(d.end).toEqual(new Date(2026, 9, 25, 12, 0, 0))
  })
  it("starts 1, 2 and 5 years back", () => {
    expect(pageDomain("1Y", today, results).start).toEqual(
      new Date(2025, 9, 25, 12)
    )
    expect(pageDomain("2Y", today, results).start).toEqual(
      new Date(2024, 9, 25, 12)
    )
    expect(pageDomain("5Y", today, results).start).toEqual(
      new Date(2021, 9, 25, 12)
    )
  })
  it("starts All at the earliest result, whatever the order", () => {
    expect(pageDomain("All", today, results).start).toEqual(
      new Date(2023, 10, 2, 9, 0)
    )
  })
  it("falls back to a year for All with no results", () => {
    expect(pageDomain("All", today, []).start).toEqual(
      new Date(2025, 9, 25, 12)
    )
  })
  it("admits both bounds", () => {
    const d = pageDomain("1Y", today, results)
    expect(inDomain(d.start, d)).toBe(true)
    expect(inDomain(d.end, d)).toBe(true)
    expect(inDomain(d.start.getTime() - 1, d)).toBe(false)
  })
})

describe("emptySpan", () => {
  const start = new Date(2021, 9, 25, 12)
  it("spans from the start to the first result, labelled by its month", () => {
    const first = new Date(2025, 6, 14, 9)
    const span = emptySpan(start, first.toISOString())
    expect(span?.from).toEqual(start)
    expect(span?.to).toEqual(first)
    expect(span?.label).toBe("No results before Jul 2025")
  })
  it("is absent when the first result is under a day after the start", () => {
    expect(
      emptySpan(start, new Date(2021, 9, 26, 11).toISOString())
    ).toBeUndefined()
    expect(emptySpan(start, start)).toBeUndefined()
    expect(emptySpan(start, new Date(2021, 9, 26, 12))).toBeDefined()
  })
})

describe("isRangeId", () => {
  it("accepts the four ranges only", () => {
    expect(["1Y", "2Y", "5Y", "All"].every(isRangeId)).toBe(true)
    expect(isRangeId("3Y")).toBe(false)
    expect(isRangeId(null)).toBe(false)
  })
})
