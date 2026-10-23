/** Time on the observations pages: ranges, instants and windows in words,
 * the axis's ticks, bucket sizes and their time modes, recorded-local days,
 * and durations. The suite runs in Europe/Athens (vite.config.ts). */

import { describe, expect, it, vi } from "vitest"

import {
  allRange,
  firstDayStart,
  autoBucket,
  coveredWords,
  endPlacedBucket,
  axisTicks,
  bucketEstimate,
  bucketOf,
  bucketWords,
  coverageWords,
  dayRange,
  durationMs,
  isoWithOffset,
  labelledTicks,
  localKey,
  localSlot,
  localWindowWords,
  offsetWords,
  presetRange,
  presetReaching,
  rangeWords,
  readerTimezone,
  spanWords,
  wholeLocalDates,
} from "@/lib/observations-time"

const DAY = 86_400_000

describe("ranges and buckets", () => {
  const now = new Date(2026, 9, 7, 15, 30).getTime()

  it("counts presets back in whole local days ending with today", () => {
    const today = presetRange("today", now)
    expect(new Date(today.start).getHours()).toBe(0)
    expect(today.end - today.start).toBeGreaterThanOrEqual(23 * 3_600_000)
    const week = presetRange("7d", now)
    expect(new Date(week.start).getDate()).toBe(1)
    expect(new Date(week.end).getDate()).toBe(8)
    const year = presetRange("1y", now)
    expect(new Date(year.start).getFullYear()).toBe(2025)
  })

  it("finds the shortest preset that reaches back to a value", () => {
    expect(presetReaching(now - 2 * DAY, now)).toBe("7d")
    expect(presetReaching(now - 13 * DAY, now)).toBe("30d")
    expect(presetReaching(now - 90 * DAY, now)).toBe("6m")
    expect(presetReaching(now - 300 * DAY, now)).toBe("1y")
    expect(presetReaching(now - 500 * DAY, now)).toBeUndefined()
  })

  it("reads a custom range of days, end inclusive", () => {
    const r = dayRange("2026-09-01", "2026-09-30")!
    expect(new Date(r.start).getDate()).toBe(1)
    expect(new Date(r.end).getMonth()).toBe(9)
    expect(dayRange("2026-09-30", "2026-09-01")).toBeUndefined()
    expect(rangeWords(r)).toBe("Sep 1 to Sep 30, 2026")
  })

  it("writes instants as local wall time with the offset", () => {
    const t = new Date(2026, 8, 1).getTime()
    const iso = isoWithOffset(t)
    expect(iso).toMatch(/^2026-09-01T00:00:00[+-]\d{2}:\d{2}$/)
    expect(Date.parse(iso)).toBe(t)
  })

  it("picks a bucket from the range, under 200 for every preset", () => {
    expect(autoBucket(presetRange("today", now))).toBe("15m")
    expect(autoBucket(presetRange("7d", now))).toBe("hour")
    expect(autoBucket(presetRange("30d", now))).toBe("day")
    expect(autoBucket(presetRange("6m", now))).toBe("week")
    expect(autoBucket(presetRange("1y", now))).toBe("week")
    expect(autoBucket({ start: 0, end: 3 * 365 * DAY })).toBe("month")
    // A sleep metric reads a week in days, and a picked size as picked.
    expect(endPlacedBucket("auto", autoBucket(presetRange("7d", now)))).toBe(
      "day"
    )
    expect(endPlacedBucket("auto", "15m")).toBe("15m")
    expect(endPlacedBucket("hour", "hour")).toBe("hour")
    for (const p of ["today", "7d", "30d", "6m", "1y"] as const) {
      const r = presetRange(p, now)
      expect(bucketEstimate(r, autoBucket(r))).toBeLessThan(200)
    }
    expect(bucketEstimate(presetRange("1y", now), "15m")).toBeGreaterThan(1440)
  })

  it("labels the axis on round clock times, as often as there is room", () => {
    const day = {
      start: Date.parse("2026-10-07T00:00:00+03:00"),
      end: Date.parse("2026-10-09T00:00:00+03:00"),
    }
    const hours = axisTicks(day, "hour")
    // Room for a label every fifth hour: one every six, on 00, 06, 12, 18.
    const labelled = labelledTicks(hours, "hour", 5).map((i) =>
      new Date(hours[i]).getHours()
    )
    expect(labelled).toEqual([0, 6, 12, 18, 0, 6, 12, 18])
    // A quarter hour with room every third: on the hour.
    const quarters = axisTicks(
      { start: day.start + 10 * 60_000, end: day.start + 4 * 3_600_000 },
      "15m"
    )
    expect(
      labelledTicks(quarters, "15m", 3).map((i) =>
        new Date(quarters[i]).getMinutes()
      )
    ).toEqual([0, 0, 0])
    // Room for every one: each is labelled.
    expect(labelledTicks(hours.slice(0, 4), "hour", 1)).toEqual([0, 1, 2, 3])
    // Quarter hours with room every 50 hours: every third midnight.
    const days = axisTicks(
      { start: day.start, end: day.start + 10 * DAY },
      "15m"
    )
    expect(days).toHaveLength(960)
    expect(
      labelledTicks(days, "15m", 200).map((i) => new Date(days[i]).getDate())
    ).toEqual([7, 10, 13, 16])
  })

  it("says a point's window short: its days, its clocks, or both ends", () => {
    const t = (iso: string) => Date.parse(iso)
    expect(
      spanWords(t("2026-10-11T00:00:00+03:00"), t("2026-10-12T00:00:00+03:00"))
    ).toBe("Oct 11")
    expect(
      spanWords(t("2026-10-07T09:45:00+03:00"), t("2026-10-07T10:00:00+03:00"))
    ).toBe("Oct 7, 09:45 to 10:00")
    expect(
      spanWords(t("2026-10-06T22:41:00+03:00"), t("2026-10-07T22:38:00+03:00"))
    ).toBe("Oct 6, 22:41 to Oct 7, 22:38")
  })

  it("reads a few hours in minutes, as a trace's window opens", () => {
    const at = Date.parse("2026-10-11T05:25:00Z")
    const minutes = (n: number) => ({ start: at, end: at + n * 60_000 })
    // A 35-minute workout trace: a bucket per minute, not three.
    expect(autoBucket(minutes(35))).toBe("1m")
    expect(bucketEstimate(minutes(35), "1m")).toBe(35)
    expect(autoBucket(minutes(120))).toBe("1m")
    expect(autoBucket(minutes(6 * 60))).toBe("5m")
    expect(autoBucket(minutes(12 * 60))).toBe("15m")
    expect(bucketOf("1m")).toEqual({ every: "PT1M" })
    expect(bucketOf("5m")).toEqual({ every: "PT5M" })
    expect(axisTicks(minutes(5), "1m")).toEqual(
      [0, 1, 2, 3, 4].map((i) => at + i * 60_000)
    )
  })

  it("spells each size as the aggregate's time mode", () => {
    expect(bucketOf("15m", "Europe/Athens")).toEqual({ every: "PT15M" })
    expect(bucketOf("day", "Europe/Athens")).toEqual({
      calendar: "day",
      timezone: "Europe/Athens",
    })
    expect(bucketOf("week")).toEqual({ calendar: "week", weekStart: "monday" })
  })

  it("reads days, weeks and months where each value was recorded", () => {
    // No zone: the reader's places only a value written without an offset.
    expect(bucketOf("day", "Europe/Athens", true)).toEqual({
      calendar: "day",
      fallbackTimezone: "Europe/Athens",
    })
    expect(bucketOf("week", "Europe/Athens", true)).toEqual({
      calendar: "week",
      weekStart: "monday",
      fallbackTimezone: "Europe/Athens",
    })
    // An hour is an instant's, wherever it was lived.
    expect(bucketOf("hour", "Europe/Athens", true)).toEqual({
      calendar: "hour",
      timezone: "Europe/Athens",
    })
    expect(bucketOf("15m", "Europe/Athens", true)).toEqual({ every: "PT15M" })
  })

  it("reads whole local days at any offset, and only for day buckets", () => {
    const week = {
      start: new Date(2026, 8, 19).getTime(),
      end: new Date(2026, 8, 26).getTime(),
    }
    // The server answers those dates at whatever offset each was lived.
    expect(wholeLocalDates(week, "day", true)).toBe(true)
    expect(wholeLocalDates(week, "day", false)).toBe(false)
    expect(wholeLocalDates(week, "week", true)).toBe(false)
    const hours = { ...week, start: new Date(2026, 8, 19, 6).getTime() }
    expect(wholeLocalDates(hours, "day", true)).toBe(false)
  })

  it("keys a recorded-local row by the date its own start names", () => {
    // 23:30 in London is 01:30 in Athens, and still the 20th.
    expect(localKey("2026-09-20T23:30:00+01:00", "day")).toBe("2026-09-20")
    expect(localKey("2026-09-24T00:00:00-05:00", "week")).toBe("2026-09-21")
    expect(localKey("2026-09-21T00:00:00+01:00", "week")).toBe("2026-09-21")
    expect(localKey("2026-09-27T22:00:00+01:00", "week")).toBe("2026-09-21")
    expect(localKey("2026-09-30T23:00:00+01:00", "month")).toBe("2026-09-01")
    expect(localKey("not a time", "day")).toBeUndefined()
    // Its slot is that date on the reader's axis, midnight to midnight.
    expect(localSlot("2026-09-20", "day")).toEqual({
      start: new Date(2026, 8, 20).getTime(),
      end: new Date(2026, 8, 21).getTime(),
    })
    expect(localSlot("2026-09-21", "week").end).toBe(
      new Date(2026, 8, 28).getTime()
    )
    expect(localSlot("2026-12-01", "month").end).toBe(
      new Date(2027, 0, 1).getTime()
    )
  })

  it("says a recorded-local row's dates and the offsets it was lived at", () => {
    expect(
      localWindowWords("2026-09-20T00:00:00+03:00", "2026-09-21T00:00:00+01:00")
    ).toBe("Sep 20, 2026")
    expect(
      localWindowWords("2026-09-21T00:00:00+01:00", "2026-09-28T00:00:00+03:00")
    ).toBe("Sep 21 to Sep 27, 2026")
    // A range that cuts the day still names it.
    expect(
      localWindowWords("2026-09-18T13:00:00+03:00", "2026-09-19T00:00:00+03:00")
    ).toBe("Sep 18, 2026")
    expect(
      offsetWords("2026-09-20T00:00:00+03:00", "2026-09-21T00:00:00+01:00")
    ).toBe("+03:00 to +01:00")
    expect(offsetWords("2026-09-21T00:00:00Z", "2026-09-22T00:00:00Z")).toBe(
      "+00:00"
    )
  })

  it("reads the reader's zone, UTC where the runtime names none", () => {
    expect(readerTimezone()).toBe("Europe/Athens")
    // Without a zone a calendar bucket would be the recorded-local one,
    // while the range stays in the browser's offset.
    const spy = vi
      .spyOn(Intl.DateTimeFormat.prototype, "resolvedOptions")
      .mockReturnValue({} as Intl.ResolvedDateTimeFormatOptions)
    try {
      expect(readerTimezone()).toBe("UTC")
    } finally {
      spy.mockRestore()
    }
  })

  it("ticks the axis at the size's boundaries across the whole range", () => {
    const range = {
      start: new Date(2026, 9, 1, 13, 20).getTime(),
      end: new Date(2026, 9, 4).getTime(),
    }
    const days = axisTicks(range, "day").map((t) => new Date(t).getDate())
    expect(days).toEqual([2, 3])
    const hours = axisTicks(range, "hour")
    expect(new Date(hours[0]).getHours()).toBe(14)
    expect(hours).toHaveLength(58)
    // 2026-10-05 is a Monday.
    const weeks = axisTicks(
      { start: range.start, end: range.end + 9 * DAY },
      "week"
    )
    expect(weeks.map((t) => new Date(t).getDay())).toEqual([1, 1])
    expect(new Date(weeks[0]).getDate()).toBe(5)
  })

  it("ticks every hour of a day the clocks go back, the repeated one twice", () => {
    // The suite runs in Europe/Athens (vite.config.ts): on 2026-10-25 the
    // clocks go from 04:00 back to 03:00, so the day holds 25 hours.
    const day = dayRange("2026-10-25", "2026-10-25")!
    expect(day.end - day.start).toBe(25 * 3_600_000)
    const hours = axisTicks(day, "hour")
    expect(hours).toHaveLength(25)
    expect(hours.map((t) => isoWithOffset(t).slice(11))).toContain(
      "03:00:00+03:00"
    )
    expect(hours.map((t) => isoWithOffset(t).slice(11))).toContain(
      "03:00:00+02:00"
    )
    expect(axisTicks(day, "15m")).toHaveLength(100)
    // And the day the clocks go forward holds 23.
    expect(
      axisTicks(dayRange("2026-03-29", "2026-03-29")!, "hour")
    ).toHaveLength(23)
    // Day ticks stay at midnight on either side of the change.
    const days = axisTicks(dayRange("2026-10-24", "2026-10-27")!, "day")
    expect(days.map((t) => isoWithOffset(t))).toEqual([
      "2026-10-24T00:00:00+03:00",
      "2026-10-25T00:00:00+03:00",
      "2026-10-26T00:00:00+02:00",
      "2026-10-27T00:00:00+02:00",
    ])
  })

  it("labels buckets as finely as their size", () => {
    const t = new Date(2026, 8, 3, 6, 30).getTime()
    expect(bucketWords(t, "hour")).toBe("Sep 3, 06:30")
    expect(bucketWords(t, "hour", t - 3_600_000)).toBe("06:30")
    expect(bucketWords(t, "day")).toBe("Sep 3")
    expect(bucketWords(new Date(2026, 0, 1).getTime(), "month", 0)).toBe(
      "Jan 2026"
    )
  })

  it("counts groups times buckets against the budget", () => {
    const week = { start: 0, end: 7 * DAY }
    expect(bucketEstimate(week, "15m", 3)).toBe(672 * 3)
  })
})

describe("durations", () => {
  it("reads ISO 8601 durations and says them in a few words", () => {
    expect(durationMs("PT15M")).toBe(900_000)
    expect(durationMs("P6DT22H10M")).toBe((6 * 24 + 22) * 3_600_000 + 600_000)
    expect(durationMs("P1M")).toBeUndefined()
    expect(coverageWords("P6DT22H10M")).toBe("6d 22h 10m")
    expect(coverageWords("PT59M58S")).toBe("59m 58s")
  })
})

describe("the covered time", () => {
  it("reads to the minute from an hour up, to the second under it", () => {
    expect(coveredWords("PT22H25M17S")).toBe("22h 25m")
    expect(coveredWords("PT7H10M40S")).toBe("7h 11m")
    expect(coveredWords("PT8M12S")).toBe("8m 12s")
    expect(coveredWords("soon")).toBe("soon")
  })
})

describe("allRange", () => {
  it("runs from the earliest point's day through today", () => {
    const now = new Date(2026, 9, 23, 15, 30).getTime()
    expect(allRange(new Date(2024, 1, 3, 7, 15).getTime(), now)).toEqual({
      start: new Date(2024, 1, 3).getTime(),
      end: new Date(2026, 9, 24).getTime(),
    })
  })
})

describe("firstDayStart", () => {
  it("keeps a point's recorded date where the reader's date is later", () => {
    // 23:30 on October 1 at -07:00 is October 2 in UTC and east of it.
    expect(firstDayStart("2026-10-01T23:30:00-07:00")).toBe(
      new Date(2026, 9, 1).getTime()
    )
  })

  it("reaches the date a later point of the stream may record further west", () => {
    // The stream's earliest instant is 00:30 UTC on October 2; a later one,
    // 00:45 UTC, was written at -01:00 and records October 1.
    expect(firstDayStart("2026-10-02T00:30:00Z")).toBeLessThanOrEqual(
      new Date(2026, 9, 1).getTime()
    )
  })

  it("never starts after the point's instant", () => {
    for (const iso of [
      "2026-10-02T00:30:00+09:00",
      "2026-10-01T23:30:00-07:00",
      "2026-10-01T12:00:00Z",
    ]) {
      expect(firstDayStart(iso)).toBeLessThanOrEqual(Date.parse(iso))
    }
  })
})
