/** The observations pages' model: discovery by trait, what a form lets a
 * reader ask for, ranges and buckets, the request each chart issues, and an
 * answer cut into buckets and series. */

import { describe, expect, it } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import {
  answerReducers,
  autoBucket,
  bucketsOf,
  channelKey,
  axisTicks,
  breakAtGaps,
  bucketEstimate,
  bucketOf,
  bucketWords,
  coverageWords,
  dayRange,
  durationMs,
  formOf,
  isoWithOffset,
  laneRead,
  discoveryChunks,
  facetRead,
  latestRead,
  metricInfoOf,
  observedKinds,
  observedPins,
  observedVariant,
  presetRange,
  rangeWords,
  readChoices,
  recordTimelineSearch,
  reducerKey,
  reducerLabel,
  reducerOptions,
  refPathOf,
  readingText,
  rollupText,
  roundDigits,
  rowValue,
  sameRef,
  seriesOf,
  seriesOn,
  shapeOf,
  stddevOf,
  streamSearch,
  streamsOf,
  tracePoints,
  traceStats,
  unitChoices,
  writeChoice,
  type LaneSpec,
} from "@/lib/observations"
import {
  BODY_MASS,
  GARMIN,
  HEART,
  MIA,
  RECOVERY,
  SLEEP_STAGE,
  STEPS_METRIC,
  HEART_TRACE,
  KINDS,
  LATEST,
  METRIC,
  METRICS,
  SLEEP,
  SOURCE,
  STEPS,
  WEIGHT,
  WEIGHING,
  WHOOP,
  ZONES,
  metricKind,
  observationKind,
  traceKind,
} from "@/lib/observations.test-fixtures"

const OBSERVED = "substrate.reamde.dev/core/observed"
const DAY = 86_400_000

describe("discovery by trait", () => {
  it("finds the kinds binding observed, whatever variant the binding names", () => {
    expect(observedKinds(KINDS).map((k) => k.identity)).toEqual([
      observationKind.identity,
      traceKind.identity,
    ])
  })

  it("finds a kind under any authority, never a fixed one", () => {
    const elsewhere: KindInfo = {
      ...observationKind,
      identity: "home.example.org/climate/reading",
      authority: "home.example.org",
      package: "climate",
      name: "reading",
    }
    expect(observedKinds([elsewhere, metricKind])).toEqual([elsewhere])
  })

  it("tells packed from single by whether the kind declares values", () => {
    expect(observedVariant(traceKind)).toBe("packed")
    expect(observedVariant(observationKind)).toBe("single")
    expect(observedVariant(metricKind)).toBeUndefined()
    const bare: KindInfo = {
      ...traceKind,
      definition: { ...traceKind.definition, traits: [OBSERVED] },
    }
    expect(observedVariant(bare)).toBe("packed")
  })

  it("reads the pins of metric and source off the declarations", () => {
    expect(observedPins(observedKinds(KINDS))).toEqual({
      metric: METRIC,
      source: SOURCE,
    })
  })

  it("spells a group value as a record path under its pin", () => {
    expect(refPathOf("health/heart_rate", METRIC)).toBe(
      `${METRIC}/health/heart_rate`
    )
    expect(refPathOf("ada.example.com/people/person/mia", undefined)).toBe(
      "ada.example.com/people/person/mia"
    )
    expect(refPathOf("mia", undefined)).toBeUndefined()
    expect(sameRef(`${SOURCE}/garmin`, "garmin")).toBe(true)
    expect(sameRef("garmin", "apple")).toBe(false)
  })
})

describe("forms and reducers", () => {
  const info = (id: string) => metricInfoOf(METRICS.find((m) => m.id === id)!)

  it("reads a metric record's name, form, unit and codes", () => {
    expect(info("health/sleep_stage")).toMatchObject({
      name: "Sleep stage",
      form: "amount",
      unit: "min",
      codeSet: ["rem", "deep", "light", "awake"],
      path: `${METRIC}/health/sleep_stage`,
    })
  })

  it("offers the reducers each form allows, the default first", () => {
    expect(reducerOptions(shapeOf(info("health/heart_rate")))).toEqual([
      "avg",
      "twa",
      "min",
      "max",
      "first",
      "last",
      "count",
      "stddev",
    ])
    expect(reducerOptions("amount")).toEqual(["sum", "count", "min", "max"])
    expect(reducerOptions("counter")).toEqual(["increase", "rate", "last"])
    expect(reducerOptions("summary")).toEqual(["last"])
    // A coded gauge answers time per code; a coded amount takes no
    // timeInState (docs/api.md, "Reducers, by form").
    expect(reducerOptions("coded", "gauge")).toEqual(["timeInState", "count"])
    expect(shapeOf(info("health/sleep_stage"))).toBe("coded")
    expect(reducerOptions("coded", "amount")).toEqual(["sum", "count"])
  })

  it("reads a stream's own form and code before the metric's default", () => {
    const heart = info("health/heart_rate")
    expect(shapeOf(heart, { form: "summary" })).toBe("summary")
    expect(shapeOf(heart, { form: "gauge", code: "open" })).toBe("coded")
    expect(formOf(heart, { form: "nonsense" })).toBe("gauge")
  })

  it("names structured reducers by their argument", () => {
    expect(reducerKey({ durationIn: [95, 114] })).toBe("durationIn:95,114")
    expect(reducerLabel({ durationIn: [95, 114] })).toBe(
      "Time in range 95 to 114"
    )
    expect(reducerLabel({ percentile: 0.05 })).toBe("Percentile 5")
    expect(reducerLabel("twa")).toBe("Time-weighted average")
  })

  it("narrows the output unit to the stored unit's measure", () => {
    expect(unitChoices("kg")).toContain("[lb_av]")
    expect(unitChoices("kg")).not.toContain("J")
    expect(unitChoices("/min")).toEqual(
      expect.arrayContaining(["/min", "/s", "/h"])
    )
    // An annotation the table does not list converts to nothing.
    expect(unitChoices("{steps}")).toEqual(["{steps}"])
    expect(unitChoices(undefined)).toEqual([])
    // A declared measure decides where the unit alone is ambiguous.
    expect(unitChoices("Cel", "temperature")).toContain("[degF]")
  })
})

describe("the latest read", () => {
  it("asks for the newest value of every stream, no range and no bucket", () => {
    expect(latestRead()).toEqual({
      filter: { implements: OBSERVED },
      aggregate: {
        groupBy: ["metric", "source", "about", "channel"],
        reducers: ["last"],
      },
    })
  })

  it("narrows on the server and reads its metrics in bounded batches", () => {
    expect(latestRead({ metrics: [HEART], sources: [GARMIN] }).filter).toEqual({
      implements: OBSERVED,
      properties: { metric: { in: [HEART] }, source: { in: [GARMIN] } },
    })
    const ids = (n: number) => Array.from({ length: n }, (_, i) => `m${i}`)
    expect(discoveryChunks(ids(20)).map((c) => c.length)).toEqual([8, 8, 4])
    const many = discoveryChunks(ids(1000))
    expect(many.length).toBeLessThanOrEqual(32)
    expect(many.flat()).toEqual(ids(1000))
    expect(discoveryChunks([])).toEqual([])
  })

  it("reads a facet's values under the other choices, never its own", () => {
    const narrow = { metrics: [HEART], sources: [GARMIN], abouts: [MIA] }
    expect(facetRead("source", narrow)).toEqual({
      filter: {
        implements: OBSERVED,
        properties: { metric: { in: [HEART] }, about: { in: [MIA] } },
      },
      aggregate: {
        groupBy: ["source"],
        reducers: ["last"],
        budget: { maxGroups: 2000 },
      },
    })
    expect(facetRead("about", narrow).filter?.properties).toEqual({
      metric: { in: [HEART] },
      source: { in: [GARMIN] },
    })
    expect(facetRead("about", narrow).aggregate.groupBy).toEqual(["about"])
  })

  it("reads six streams, newest first, with their form and code", () => {
    const streams = streamsOf(LATEST)
    expect(streams).toHaveLength(6)
    expect(streams[0]).toMatchObject({
      metric: HEART,
      source: GARMIN,
      value: "96",
      unit: "/min",
      form: "gauge",
      count: 1812,
    })
    expect(streams.map((s) => s.metric)).toEqual([
      HEART,
      STEPS_METRIC,
      HEART,
      BODY_MASS,
      SLEEP_STAGE,
      RECOVERY,
    ])
    // A coded `last` answers its code with `value` null.
    expect(streams.find((s) => s.metric === SLEEP_STAGE)).toMatchObject({
      about: MIA,
      value: null,
      code: "awake",
      form: "amount",
    })
  })

  it("tells a source's channels apart as two streams", () => {
    const row = LATEST.rows[0]
    const streams = streamsOf({
      rows: [
        { ...row, group: { ...row.group, channel: "cycle" } },
        { ...row, group: { ...row.group, channel: "workout" } },
      ],
    })
    expect(streams.map((s) => s.channel)).toEqual(["cycle", "workout"])
    expect(new Set(streams.map((s) => s.key)).size).toBe(2)
    expect(streamSearch(streams[1])).toEqual({
      metric: HEART,
      source: GARMIN,
      channel: "workout",
    })
  })
})

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
    for (const p of ["today", "7d", "30d", "6m", "1y"] as const) {
      const r = presetRange(p, now)
      expect(bucketEstimate(r, autoBucket(r))).toBeLessThan(200)
    }
    expect(bucketEstimate(presetRange("1y", now), "15m")).toBeGreaterThan(1440)
  })

  it("spells each size as the aggregate's time mode", () => {
    expect(bucketOf("15m", "Europe/Athens")).toEqual({ every: "PT15M" })
    expect(bucketOf("day", "Europe/Athens")).toEqual({
      calendar: "day",
      timezone: "Europe/Athens",
    })
    expect(bucketOf("week")).toEqual({ calendar: "week", weekStart: "monday" })
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

  it("rounds a computed value for display and keeps stored digits", () => {
    expect(roundDigits("63.1666666666667", 0)).toBe("63")
    expect(roundDigits("61.9372442", 2)).toBe("61.94")
    expect(roundDigits("72.40", 2)).toBe("72.40")
    expect(roundDigits("9812", 0)).toBe("9812")
    expect(readingText("160.011509893784", "[lb_av]", 1)).toBe("160.0 lb")
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
})

const spec = (over: Partial<LaneSpec> = {}): LaneSpec => ({
  metricPath: `${METRIC}/health/heart_rate`,
  shape: "gauge",
  reducer: "avg",
  from: "raw",
  range: {
    start: new Date(2026, 8, 1).getTime(),
    end: new Date(2026, 9, 1).getTime(),
  },
  bucket: { calendar: "day", timezone: "Europe/Athens" },
  group: "metric",
  sources: [],
  sourceOrder: ["garmin-forerunner-265"],
  about: [],
  ...over,
})

describe("one chart's request", () => {
  it("asks for the reducer and, on a level, the band around it", () => {
    const read = laneRead(spec())
    expect(read.filter).toEqual({ implements: OBSERVED })
    expect(read.aggregate).toMatchObject({
      metric: { ref: `${METRIC}/health/heart_rate` },
      from: "raw",
      bucket: { calendar: "day", timezone: "Europe/Athens" },
      reducers: ["avg", "min", "max"],
    })
    expect(read.aggregate.range?.gte).toMatch(/^2026-09-01T00:00:00/)
    expect(read.aggregate.groupBy).toBeUndefined()
    expect(read.aggregate.sources).toBeUndefined()
    expect(laneRead(spec({ reducer: "max" })).aggregate.reducers).toEqual([
      "max",
      "min",
    ])
  })

  it("groups by the reader's choice and resolves overlap by priority", () => {
    const bySource = laneRead(
      spec({ group: "source", sourceOrder: ["a", "b"] })
    ).aggregate
    // A source's channels stay apart: a WHOOP cycle beside its workouts.
    expect(bySource.groupBy).toEqual(["source", "channel"])
    expect(bySource.sources).toBeUndefined()
    const merged = laneRead(spec({ sourceOrder: ["a", "b"] })).aggregate
    expect(merged.sources).toEqual({ policy: "priority", order: ["a", "b"] })
  })

  it("narrows to the chosen channels", () => {
    const read = laneRead(spec({ channels: ["workout"] }))
    expect(read.filter?.properties).toEqual({ channel: { in: ["workout"] } })
    expect(laneRead(spec({ channels: [] })).filter?.properties).toBeUndefined()
  })

  it("narrows to the chosen sources and things by path", () => {
    const read = laneRead(
      spec({
        sources: [`${SOURCE}/garmin-forerunner-265`],
        about: ["ada.example.com/people/person/mia"],
      })
    )
    expect(read.filter?.properties).toEqual({
      source: { in: [`${SOURCE}/garmin-forerunner-265`] },
      about: { in: ["ada.example.com/people/person/mia"] },
    })
  })

  it("groups a coded metric by code and asks timeInState bare", () => {
    const read = laneRead(
      spec({ shape: "coded", reducer: "timeInState", group: "source" })
    ).aggregate
    expect(read.groupBy).toEqual(["code"])
    // Bare, it answers the time each code holds, each value with its code.
    expect(read.reducers).toEqual(["timeInState"])
  })

  it("skips a rolling value where it reads a reducer's own", () => {
    const rolled = {
      ...WEIGHT.rows[0],
      values: [
        { reducer: "avg", value: "72.9", unit: "kg", rolling: 4, of: "avg" },
        ...WEIGHT.rows[0].values,
      ],
    }
    expect(rowValue(rolled, "avg")?.value).toBe("72.6")
    expect(answerReducers([rolled]).map(reducerKey)).toEqual([
      "avg",
      "min",
      "max",
    ])
  })

  it("counts groups times buckets against the budget", () => {
    const week = { start: 0, end: 7 * DAY }
    expect(bucketEstimate(week, "15m", 3)).toBe(672 * 3)
  })

  it("sends the output unit only where one was picked", () => {
    expect(laneRead(spec()).aggregate.unit).toBeUndefined()
    expect(laneRead(spec({ unit: "/s" })).aggregate.unit).toBe("/s")
  })
})

describe("an answer as buckets and series", () => {
  it("lays several answers on one bucket axis, a gap where one is silent", () => {
    const day = (start: string, end: string, value: string, unit: string) => ({
      start,
      end,
      values: [{ reducer: "avg", value, unit }],
      count: 1,
      bounded: 0,
      synthetic: false,
      unavailable: false,
    })
    const heart = [
      day("2026-09-01T00:00:00Z", "2026-09-02T00:00:00Z", "61", "/min"),
      day("2026-09-02T00:00:00Z", "2026-09-03T00:00:00Z", "64", "/min"),
      day("2026-09-03T00:00:00Z", "2026-09-04T00:00:00Z", "60", "/min"),
    ]
    // HRV, spelled in another offset, with no reading on the second day.
    const hrv = [
      day("2026-09-01T03:00:00+03:00", "2026-09-02T03:00:00+03:00", "52", "ms"),
      day("2026-09-03T03:00:00+03:00", "2026-09-04T03:00:00+03:00", "58", "ms"),
    ]
    const buckets = bucketsOf([heart, hrv])
    expect(buckets).toHaveLength(3)
    expect(buckets[0].rows).toHaveLength(2)
    expect(seriesOn(buckets, hrv, "avg")[0].points.map((p) => p.value)).toEqual(
      [52, null, 58]
    )
    expect(
      seriesOn(buckets, heart, "avg")[0].points.map((p) => p.value)
    ).toEqual([61, 64, 60])
  })

  it("keeps a source's channels apart as two series", () => {
    const row = (channel: string, value: string) => ({
      start: "2026-09-01T00:00:00Z",
      end: "2026-09-02T00:00:00Z",
      group: { source: WHOOP, channel },
      values: [{ reducer: "avg", value, unit: "/min" }],
      count: 1,
      bounded: 0,
      synthetic: false,
      unavailable: false,
    })
    const { series } = seriesOf(
      [row("cycle", "58"), row("workout", "131")],
      "avg",
      "source"
    )
    expect(series.map((s) => [s.value, s.channel])).toEqual([
      [WHOOP, "cycle"],
      [WHOOP, "workout"],
    ])
    expect(series.map((s) => s.key)).toEqual([
      channelKey(WHOOP, "cycle"),
      channelKey(WHOOP, "workout"),
    ])
    expect(
      rollupText([row("cycle", "58")], ["avg"], "source").split("\n")
    ).toEqual([
      "start\tend\tsource\tchannel\tavg\tcount\tcoverage\tsources\tfrom\tsynthetic\tunavailable",
      `2026-09-01T00:00:00Z\t2026-09-02T00:00:00Z\t${WHOOP}\tcycle\t58 /min\t1\t\t\t\t\t`,
    ])
  })

  it("draws the weight trend with its band and a filled week as a gap", () => {
    const { buckets, series } = seriesOf(WEIGHT.rows, "avg")
    expect(buckets).toHaveLength(3)
    expect(series).toHaveLength(1)
    expect(series[0].points.map((p) => p.value)).toEqual([72.6, null, 72.3])
    expect(series[0].points[0]).toMatchObject({ low: 72.1, high: 73 })
  })

  it("cuts sleep stages into one series per code over one night", () => {
    const { buckets, series } = seriesOf(SLEEP.rows, "sum", "code")
    expect(buckets).toHaveLength(1)
    expect(series.map((s) => s.value)).toEqual([
      "rem",
      "deep",
      "light",
      "awake",
    ])
    expect(series.map((s) => s.points[0].value)).toEqual([94, 71, 211, 38])
  })

  it("breaks every series where the answer skips a bucket", () => {
    const hour = (
      start: string,
      end: string,
      source: string,
      value: string
    ) => ({
      start,
      end,
      group: { source },
      values: [{ reducer: "avg", value, unit: "kg" }],
      count: 1,
      bounded: 0,
      synthetic: false,
      unavailable: false,
    })
    // Two hours measured, two between them not: the answer omits them.
    const rows = [
      hour("2026-09-01T00:00:00Z", "2026-09-01T01:00:00Z", "a", "10"),
      hour("2026-09-01T00:00:00Z", "2026-09-01T01:00:00Z", "b", "11"),
      hour("2026-09-01T03:00:00Z", "2026-09-01T04:00:00Z", "a", "20"),
      hour("2026-09-01T03:00:00Z", "2026-09-01T04:00:00Z", "b", "21"),
    ]
    const { buckets, series } = seriesOf(rows, "avg", "source")
    expect(buckets.map((b) => [b.startIso, b.endIso])).toEqual([
      ["2026-09-01T00:00:00Z", "2026-09-01T01:00:00Z"],
      ["2026-09-01T01:00:00Z", "2026-09-01T03:00:00Z"],
      ["2026-09-01T03:00:00Z", "2026-09-01T04:00:00Z"],
    ])
    expect(buckets[1].rows).toEqual([])
    expect(series.map((s) => s.points.map((p) => p.value))).toEqual([
      [10, null, 20],
      [11, null, 21],
    ])
  })

  it("reads a bare timeInState from the value naming the row's state", () => {
    const rows = [
      {
        start: "2026-10-06T23:10:00+03:00",
        end: "2026-10-07T06:20:00+03:00",
        group: { code: "rem" },
        values: [
          {
            reducer: { timeInState: ["rem"] },
            value: "1800",
            unit: "s",
            code: "rem",
          },
        ],
        count: 1,
        bounded: 0,
        synthetic: false,
        unavailable: false,
      },
    ]
    const { series } = seriesOf(rows, "timeInState", "code")
    expect(series[0].points[0].value).toBe(1800)
    // Another state's value is not this row's.
    expect(
      seriesOf([{ ...rows[0], group: { code: "deep" } }], "timeInState", "code")
        .series[0].points[0].value
    ).toBe(null)
  })

  it("keeps each row's from, so prefer-raw is auditable", () => {
    const { series } = seriesOf(STEPS.rows, "sum")
    expect(series[0].points.map((p) => p.row?.from)).toEqual([
      "raw",
      "summaries",
      "raw",
    ])
  })

  it("lists the zones' reducers as the table's columns", () => {
    expect(answerReducers(ZONES.rows).map(reducerKey)).toEqual([
      "durationIn:95,114",
      "durationIn:114,133",
      "durationIn:133,152",
      "durationIn:152,171",
      "durationIn:171,220",
    ])
  })

  it("copies the table as tab-separated wire values", () => {
    const text = rollupText(STEPS.rows, [{ durationIn: [1, 2] }, "sum"])
    const [head, first] = text.split("\n")
    expect(head.split("\t")).toEqual([
      "start",
      "end",
      "durationIn:1,2",
      "sum",
      "count",
      "coverage",
      "sources",
      "from",
      "synthetic",
      "unavailable",
    ])
    expect(first.split("\t")).toEqual([
      "2026-10-04T00:00:00+03:00",
      "2026-10-05T00:00:00+03:00",
      "",
      "9812 {steps}",
      "96",
      "PT23H40M",
      GARMIN,
      "raw",
      "",
      "",
    ])
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

  it("opens the timeline on the record's stream and window", () => {
    const trace = recordTimelineSearch(HEART_TRACE.properties)!
    expect(trace).toMatchObject({
      metric: HEART,
      source: GARMIN,
      range: "custom",
      start: "2026-10-07T06:00:00.000Z",
      end: "2026-10-07T07:00:00.000Z",
    })
    const weighing = recordTimelineSearch(WEIGHING.properties)!
    expect(weighing.metric).toBe(BODY_MASS)
    expect(
      Date.parse(weighing.end) - Date.parse(weighing.start)
    ).toBeGreaterThan(29 * DAY)
  })
})

describe("per-metric choices in the address", () => {
  it("round-trips a metric id holding a slash and a tilde", () => {
    const tokens = writeChoice(null, "health/odd~id", "twa")
    expect(readChoices(tokens).get("health/odd~id")).toBe("twa")
    expect(writeChoice(tokens, "health/odd~id", "")).toEqual([])
  })
})
