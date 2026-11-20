import { describe, expect, it } from "vitest"
import {
  addMonths,
  dateIn,
  epoch,
  instantIn,
  instantMs,
  isoText,
  knownZone,
  offsetMinutes,
  parseClock,
  parseIso,
  runStarted,
  wallClock,
  zoned,
} from "./time"

const LONDON = "Europe/London"
const at = (y: number, mo: number, d: number, h = 0, mi = 0, s = 0, us = 0) =>
  wallClock(y, mo, d, h, mi, s, us)!

describe("a wall-clock time in a named zone", () => {
  it("takes the zone's offset at that moment", () => {
    expect(zoned(at(2026, 1, 2, 8, 30), LONDON)).toBe(
      "2026-01-02T08:30:00+00:00"
    )
    expect(zoned(at(2026, 7, 1, 9, 30), LONDON)).toBe(
      "2026-07-01T09:30:00+01:00"
    )
    expect(zoned(at(2025, 1, 15, 10, 15), "Europe/Athens")).toBe(
      "2025-01-15T10:15:00+02:00"
    )
    expect(zoned(at(2025, 7, 15, 22, 0), "Europe/Athens")).toBe(
      "2025-07-15T22:00:00+03:00"
    )
    expect(zoned(at(2026, 1, 1, 12), "UTC")).toBe("2026-01-01T12:00:00+00:00")
  })

  it("takes the earlier offset for an hour the zone repeats", () => {
    expect(zoned(at(2026, 10, 25, 1, 30), LONDON)).toBe(
      "2026-10-25T01:30:00+01:00"
    )
    expect(zoned(at(2026, 11, 1, 1, 30), "America/New_York")).toBe(
      "2026-11-01T01:30:00-04:00"
    )
  })

  it("keeps the offset from before the change in an hour the zone skips", () => {
    expect(zoned(at(2026, 3, 29, 1, 30), LONDON)).toBe(
      "2026-03-29T01:30:00+00:00"
    )
    expect(zoned(at(2026, 3, 8, 2, 30), "America/New_York")).toBe(
      "2026-03-08T02:30:00-05:00"
    )
  })

  it("is undefined in a zone this browser does not know", () => {
    expect(knownZone("Mars/Olympus")).toBe(false)
    expect(zoned(at(2026, 1, 1), "Mars/Olympus")).toBeUndefined()
  })

  it("refuses a time no calendar has", () => {
    expect(wallClock(2026, 2, 29)).toBeUndefined()
    expect(wallClock(2024, 2, 29)).toBeDefined()
    expect(wallClock(2026, 1, 1, 24)).toBeUndefined()
  })

  it("writes a fraction only where it is not zero", () => {
    expect(isoText(at(2026, 1, 1, 10))).toBe("2026-01-01T10:00:00")
    expect(isoText(at(2026, 1, 1, 10, 0, 0, 123000), 0)).toBe(
      "2026-01-01T10:00:00.123000+00:00"
    )
    expect(isoText(at(2026, 1, 1), -330)).toBe("2026-01-01T00:00:00-05:30")
  })
})

describe("an instant", () => {
  it("is shown in a zone with its offset", () => {
    expect(instantIn(1767261600000, LONDON)).toBe("2026-01-01T10:00:00+00:00")
    expect(instantIn(1782903600123, LONDON)).toBe(
      "2026-07-01T12:00:00.123000+01:00"
    )
  })

  it("falls on a date in a zone", () => {
    expect(dateIn("2026-06-30T23:30:00+00:00", LONDON)).toBe("2026-07-01")
    expect(dateIn("2026-01-31T23:30:00Z", LONDON)).toBe("2026-01-31")
    expect(dateIn("2026-06-30T23:30:00", LONDON)).toBe("2026-06-30")
  })

  it("is read from ISO 8601 as Python reads it", () => {
    expect(parseIso("2026-01-02T08:30:00.1234567Z")).toEqual({
      wall: at(2026, 1, 2, 8, 30, 0, 123456),
      offset: 0,
    })
    expect(parseIso("2026-01-02 08:30")?.wall).toEqual(at(2026, 1, 2, 8, 30))
    expect(parseIso("2026-01-02")).toEqual({ wall: at(2026, 1, 2) })
    expect(parseIso("2026-01-02T08:30:00+0530")?.offset).toBe(330)
    expect(parseIso("2026-13-02")).toBeUndefined()
    expect(parseIso("02/01/2026")).toBeUndefined()
    expect(instantMs("2026-01-01T00:00:00.5+01:00")).toBe(1767222000500)
  })

  it("names its offset in minutes, or none", () => {
    expect(offsetMinutes("2026-01-01T00:00:00+05:30")).toBe(330)
    expect(offsetMinutes("2026-01-01T00:00:00-04:00")).toBe(-240)
    expect(offsetMinutes("2026-01-01T00:00:00Z")).toBe(0)
    expect(offsetMinutes("2026-01-01T00:00:00")).toBeUndefined()
  })
})

describe("a revision", () => {
  it("is whole seconds since the epoch, a .NET fraction dropped", () => {
    expect(epoch("2026-10-28T14:04:05.803874636Z")).toBe(1793196245)
    expect(epoch("2026-07-01T12:00:00.1234567", LONDON)).toBe(1782903600)
    expect(epoch("2026-01-01")).toBe(1767225600)
    expect(epoch("2026-01-01T00:00:00+01:00")).toBe(1767222000)
    expect(epoch("not a time")).toBeUndefined()
  })

  it("is the newest run's start in a fetcher's manifest", () => {
    expect(runStarted({ startedAt: "2026-10-28T14:04:05.803874636Z" })).toBe(
      1793196245
    )
    expect(
      runStarted({
        runs: [
          { startedAt: "2026-01-02T00:00:00Z" },
          { startedAt: "2026-01-03T00:00:00Z" },
          { startedAt: "2026-01-01T00:00:00Z" },
        ],
      })
    ).toBe(epoch("2026-01-03T00:00:00Z"))
    expect(runStarted({ runs: [{ started: "2026-01-02T00:00:00Z" }] })).toBe(
      epoch("2026-01-02T00:00:00Z")
    )
    expect(runStarted({ runs: [] })).toBeUndefined()
    expect(runStarted(null)).toBeUndefined()
  })
})

describe("a provider's own date formats", () => {
  it("reads strptime's directives as Python does", () => {
    expect(parseClock("01 Jan 2026 14:05", "%d %b %Y %H:%M")).toEqual(
      at(2026, 1, 1, 14, 5)
    )
    expect(parseClock("01 JAN 2026  14:05", "%d %b %Y %H:%M")).toEqual(
      at(2026, 1, 1, 14, 5)
    )
    expect(parseClock("2-Feb-2026 09:05:07", "%d-%b-%Y %H:%M:%S")).toEqual(
      at(2026, 2, 2, 9, 5, 7)
    )
    expect(parseClock("1 January 2026", "%d %B %Y")).toEqual(at(2026, 1, 1))
    expect(parseClock("31/12/2025 23:59", "%d/%m/%Y %H:%M")).toEqual(
      at(2025, 12, 31, 23, 59)
    )
    expect(parseClock("12:15:00 PM", "%I:%M:%S %p")?.hour).toBe(12)
    expect(parseClock("12:15:00 AM", "%I:%M:%S %p")?.hour).toBe(0)
    expect(parseClock("30 Feb 2026", "%d %b %Y")).toBeUndefined()
    expect(parseClock("01 Jan 2026 extra", "%d %b %Y")).toBeUndefined()
  })

  it("adds calendar months, the day clamped to the month's end", () => {
    expect(addMonths("2026-01-31", 1)).toBe("2026-02-28")
    expect(addMonths("2024-01-31", 1)).toBe("2024-02-29")
    expect(addMonths("2026-08-15", 6)).toBe("2027-02-15")
    expect(addMonths("2026-03-31", -1)).toBe("2026-02-28")
    expect(addMonths("31/01/2026", 1)).toBeUndefined()
  })
})
