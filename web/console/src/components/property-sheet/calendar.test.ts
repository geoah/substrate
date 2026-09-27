/** The calendar's arithmetic: six Monday-first weeks per month, key moves
 * across month and year edges, month steps clamped to the month's end, and
 * times read the ways people type them. */

import { describe, expect, it } from "vitest"

import {
  addMonths,
  dayKey,
  monthGrid,
  moveDay,
  parseDay,
  parseTime,
} from "./calendar"

describe("monthGrid", () => {
  it("draws six weeks from the Monday on or before the 1st", () => {
    // 1 October 2026 is a Thursday.
    const weeks = monthGrid(2026, 9)
    expect(weeks).toHaveLength(6)
    expect(weeks.every((w) => w.length === 7)).toBe(true)
    expect(dayKey(weeks[0][0])).toBe("2026-09-28")
    expect(dayKey(weeks[0][3])).toBe("2026-10-01")
    expect(dayKey(weeks[5][6])).toBe("2026-11-08")
  })

  it("starts on the 1st when the month does", () => {
    // 1 June 2026 is a Monday.
    expect(dayKey(monthGrid(2026, 5)[0][0])).toBe("2026-06-01")
  })
})

describe("moveDay", () => {
  const d = parseDay("2026-12-31")!
  it("crosses the year on the arrows", () => {
    expect(dayKey(moveDay(d, "ArrowRight")!)).toBe("2027-01-01")
    expect(dayKey(moveDay(d, "ArrowDown")!)).toBe("2027-01-07")
    expect(dayKey(moveDay(d, "ArrowUp")!)).toBe("2026-12-24")
  })

  it("goes to the week's ends on Home and End", () => {
    // 31 December 2026 is a Thursday.
    expect(dayKey(moveDay(d, "Home")!)).toBe("2026-12-28")
    expect(dayKey(moveDay(d, "End")!)).toBe("2027-01-03")
  })

  it("steps a month, or a year with Shift, clamped to the month's end", () => {
    expect(dayKey(moveDay(parseDay("2026-01-31")!, "PageDown")!)).toBe(
      "2026-02-28"
    )
    expect(dayKey(moveDay(d, "PageUp", true)!)).toBe("2025-12-31")
    expect(dayKey(addMonths(parseDay("2028-01-31")!, 1))).toBe("2028-02-29")
  })

  it("ignores a key it does not answer", () => {
    expect(moveDay(d, "a")).toBeUndefined()
  })
})

describe("parseTime", () => {
  it.each([
    ["9", "09:00"],
    ["9:30", "09:30"],
    ["0930", "09:30"],
    ["9.30", "09:30"],
    ["14:05", "14:05"],
    ["9pm", "21:00"],
    ["12am", "00:00"],
    ["12:15 pm", "12:15"],
    ["", ""],
  ])("reads %j as %j", (text, want) => {
    expect(parseTime(text)).toBe(want)
  })

  it.each(["25:00", "9:75", "13pm", "noon"])("refuses %j", (text) => {
    expect(parseTime(text)).toBeUndefined()
  })
})
