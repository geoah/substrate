/** Several metrics in one chart: which unit each metric's answer reads in,
 * which axis each takes, how a list of metrics splits into charts of two
 * units, what a refused third unit says, how a metric whose streams keep
 * vendor summaries is read, and which colour each series wears. */

import { describe, expect, it } from "vitest"
import { parseAllDocuments } from "yaml"

import catalogue from "../../../../samples/health/metrics.yaml?raw"

import {
  channelKey,
  metricInfoOf,
  NONE,
  streamSearch,
  streamsOf,
  valueOfAddress,
  type MetricInfo,
  type Stream,
} from "@/lib/observations"
import { presetRange } from "@/lib/observations-time"
import {
  APPLE,
  BODY_MASS,
  GARMIN,
  HEART,
  LATEST,
  LEO,
  METRIC,
  MIA,
  RECOVERY,
  SLEEP_STAGE,
  SOURCE,
  STEPS_METRIC,
  WHOOP,
  WITHINGS,
} from "@/lib/observations.test-fixtures"
import {
  COUNT_UNIT,
  answerUnitOf,
  assignAxes,
  fromOf,
  leftOutWords,
  markOf,
  mixedFrom,
  overfull,
  planPlot,
  plotRows,
  seriesColors,
  seriesKey,
  splitCharts,
  unitWords,
  universeOf,
  type PlotScope,
  type PlotSource,
} from "./overlay"
import { selectionByMetric } from "./stream-table"

/** The shipped health catalogue's metric records, read as the console reads
 * a repository's, under the fixtures' metric kind. */
const INFOS: Record<string, MetricInfo> = Object.fromEntries(
  parseAllDocuments(catalogue).flatMap((doc) => {
    const d = doc.toJS() as {
      metadata?: { id?: string }
      data?: { properties?: Record<string, unknown> }
    } | null
    const id = d?.metadata?.id
    if (!id || !d?.data?.properties) return []
    const info = metricInfoOf({
      id,
      kind: METRIC,
      properties: d.data.properties,
      labels: {},
      version: 1,
      createdAt: "2026-10-07T04:12:31Z",
      updatedAt: "2026-10-07T04:12:31Z",
    })
    return [[info.path, info]]
  })
)

function source(streams: Stream[] = streamsOf(LATEST)): PlotSource {
  return {
    pins: { metric: METRIC, source: SOURCE },
    streams,
    metricOf: (v) => INFOS[v] ?? INFOS[`${METRIC}/${v}`],
    metricName: (v) => (INFOS[v] ?? INFOS[`${METRIC}/${v}`])?.name ?? v,
    metricPath: (v) => (INFOS[v] ?? INFOS[`${METRIC}/${v}`])?.path,
  }
}

const SCOPE: PlotScope = {
  range: presetRange("7d", Date.parse("2026-10-07T12:00:00Z")),
  size: "hour",
  group: "metric",
  sources: [],
  abouts: [],
}

const plot = (name: string, unit: string) => ({ name, unit })

describe("the unit each answer reads in", () => {
  it("is the output unit, else the stored one, for a level or a total", () => {
    expect(answerUnitOf("avg", "/min")).toBe("/min")
    expect(answerUnitOf("sum", "{steps}")).toBe("{steps}")
    expect(answerUnitOf("avg", undefined)).toBe("")
  })

  it("is no unit for a count, whatever it counts", () => {
    expect(answerUnitOf("count", "/min")).toBe(COUNT_UNIT)
    expect(answerUnitOf("count", "kg")).toBe(answerUnitOf("count", "/min"))
    expect(unitWords(COUNT_UNIT)).toBe("count")
  })

  it("is a duration for time in each category, the unit over seconds for a rate", () => {
    expect(answerUnitOf("timeInState", undefined)).toBe("s")
    expect(answerUnitOf("timeInState", "min")).toBe("min")
    expect(answerUnitOf("rate", "kWh")).toBe("kWh/s")
    expect(answerUnitOf("circularMean", "deg")).toBe("deg")
  })

  it("draws a level as a line, an amount or a count as bars, a category stacked", () => {
    expect(markOf("gauge", "avg")).toBe("line")
    expect(markOf("gauge", "count")).toBe("bars")
    expect(markOf("amount", "max")).toBe("bars")
    expect(markOf("coded", "timeInState")).toBe("stack")
    expect(markOf("counter", "rate")).toBe("line")
  })
})

describe("axes", () => {
  it("puts one unit on the left and a second on the right", () => {
    const heart = plot("Heart rate", "/min")
    const rest = plot("Resting heart rate", "/min")
    const hrv = plot("HRV", "ms")
    const { axes, leftOut } = assignAxes([heart, hrv, rest])
    expect(axes.map((a) => [a.side, a.unit, a.plots])).toEqual([
      ["left", "/min", [heart, rest]],
      ["right", "ms", [hrv]],
    ])
    expect(leftOut).toEqual([])
  })

  it("leaves a third unit out and says what to deselect", () => {
    const plan = assignAxes([
      plot("Heart rate", "/min"),
      plot("HRV", "ms"),
      plot("Weight", "kg"),
    ])
    expect(plan.leftOut.map((p) => p.name)).toEqual(["Weight"])
    expect(leftOutWords(plan)).toBe(
      "Up to two units in one chart: Weight (kg) is left out. Deselect it, or deselect Heart rate (/min) or HRV (ms) to draw it here."
    )
  })

  it("names every metric left out, unit by unit", () => {
    const plan = assignAxes([
      plot("Steps", "{steps}"),
      plot("Heart rate", "/min"),
      plot("Weight", "kg"),
      plot("Body fat", "%"),
      plot("Muscle", "kg"),
    ])
    expect(leftOutWords(plan)).toBe(
      "Up to two units in one chart: Weight and Muscle (kg) and Body fat (%) are left out. Deselect them, or deselect Steps (steps) or Heart rate (/min) to draw them here."
    )
  })

  it("says nothing while two units hold every metric", () => {
    expect(
      leftOutWords(assignAxes([plot("A", "kg"), plot("B", "kg")]))
    ).toBeUndefined()
  })

  it("splits into as few charts as hold the metrics two units each", () => {
    const heart = plot("Heart rate", "/min")
    const hrv = plot("HRV", "ms")
    const steps = plot("Steps", "{steps}")
    const rest = plot("Resting heart rate", "/min")
    const weight = plot("Weight", "kg")
    expect(splitCharts([heart, hrv, rest])).toEqual([[heart, hrv, rest]])
    expect(splitCharts([heart, hrv, steps, rest, weight])).toEqual([
      [heart, hrv, rest],
      [steps, weight],
    ])
    expect(splitCharts([heart, hrv, steps])).toEqual([[heart, hrv], [steps]])
    expect(splitCharts([])).toEqual([])
  })
})

describe("where a metric reads from", () => {
  const stream = (form: string) => ({ form })

  it("reads raw points where no stream keeps summaries", () => {
    expect(fromOf([stream("gauge"), stream("gauge")])).toEqual({ from: "raw" })
  })

  it("reads raw where it covers a bucket and the summary otherwise where some do", () => {
    expect(fromOf([stream("gauge"), stream("summary")])).toEqual({
      from: "prefer-raw",
      reason: "some-summaries",
    })
  })

  it("reads summaries where every stream keeps them", () => {
    expect(fromOf([stream("summary")])).toEqual({
      from: "summaries",
      reason: "summaries-only",
    })
  })

  it("plans a selection with a summary stream as prefer-raw, in the measured form", () => {
    const streams = streamsOf(LATEST)
    const garmin = streams.find(
      (s) => s.metric === HEART && s.source === GARMIN
    )!
    const whoop: Stream = {
      ...garmin,
      key: "whoop-heart",
      source: WHOOP,
      form: "summary",
      // Newer than the Garmin's: still the Garmin says how it is measured.
      at: "2026-10-08T00:00:00Z",
    }
    const planned = planPlot(source([whoop, ...streams]), HEART, {
      ...SCOPE,
      streams: [whoop, garmin],
    })
    expect(planned.shape).toBe("gauge")
    expect(planned.reducer).toBe("avg")
    expect(planned.from).toBe("prefer-raw")
    expect(planned.fromReason).toBe("some-summaries")
    expect(planned.summarySources).toEqual([WHOOP])
    expect(planned.read?.aggregate.from).toBe("prefer-raw")
    // What a summary takes comes first; the rest read raw points alone.
    expect(planned.reducerMenu.slice(0, 7)).toEqual([
      {
        value: "avg",
        label: "Average",
        hint: "Each reading weighed by how long it held",
      },
      { value: "min", label: "Lowest" },
      { value: "max", label: "Highest" },
      { value: "first", label: "First" },
      { value: "last", label: "Latest" },
      { value: "count", label: "Count" },
      {
        value: "stddev",
        label: "Standard deviation",
        raw: true,
        hint: "Raw points only",
      },
    ])
    // No band is offered unasked: which bands a reader wants is a record
    // view's to say.
    expect(planned.reducers).not.toContain("durationIn:114,133")
  })

  it("reads raw points alone for a reducer no summary takes, or says why it cannot", () => {
    const streams = streamsOf(LATEST)
    const garmin = streams.find(
      (s) => s.metric === HEART && s.source === GARMIN
    )!
    const whoop: Stream = {
      ...garmin,
      key: "whoop-heart",
      source: WHOOP,
      form: "summary",
    }
    const obs = source([whoop, ...streams])
    const scope = { ...SCOPE, streams: [whoop, garmin] }
    // A band an address asks for is read: picked from the automatic read,
    // time in a range reads raw points and says so; WHOOP's summaries take
    // no durationIn.
    const zone = planPlot(obs, HEART, {
      ...scope,
      reducer: "durationIn:114,133",
    })
    expect(zone).toMatchObject({
      reducer: "durationIn:114,133",
      from: "raw",
      fromReason: "raw-only",
      mark: "bars",
      band: false,
      unit: "min",
      units: ["min", "s", "h"],
    })
    expect(zone.read?.aggregate).toMatchObject({
      from: "raw",
      reducers: [{ durationIn: [114, 133] }],
      durationUnit: "min",
    })
    // Where the reader picked prefer-raw, it is offered disabled with the
    // reason, and an address asking for it reads the default.
    const held = planPlot(obs, HEART, {
      ...scope,
      from: "prefer-raw",
      reducer: "stddev",
    })
    expect(held.reducer).toBe("avg")
    expect(held.from).toBe("prefer-raw")
    expect(held.reducerMenu.find((o) => o.value === "stddev")).toMatchObject({
      disabled: true,
    })
    // A raw read leaves the summaries out, and offers the level's every one,
    // none disabled and the average saying it is weighed by time.
    const raw = planPlot(obs, HEART, { ...scope, from: "raw" })
    expect(raw.reducers).toContain("stddev")
    expect(raw.reducerMenu.every((o) => !o.disabled && !o.raw)).toBe(true)
    expect(
      raw.reducerMenu.filter((o) => o.hint).map((o) => [o.value, o.hint])
    ).toEqual([["avg", "Each reading weighed by how long it held"]])
  })

  it("reads one statistic of summaries that keep several, the average by default", () => {
    const streams = streamsOf(LATEST)
    const garmin = streams.find(
      (s) => s.metric === HEART && s.source === GARMIN
    )!
    const whoop: Stream = {
      ...garmin,
      key: "whoop-heart",
      source: WHOOP,
      form: "summary",
      statistics: ["avg", "max"],
    }
    const obs = source([whoop, ...streams])
    const scope = { ...SCOPE, streams: [whoop, garmin] }
    const planned = planPlot(obs, HEART, scope)
    expect(planned.statistics).toEqual(["avg", "max"])
    expect(planned.statistic).toBe("avg")
    expect(planned.read?.filter?.properties?.statistic).toEqual({ eq: "avg" })
    expect(
      planPlot(obs, HEART, { ...scope, statistic: "max" }).read?.filter
        ?.properties?.statistic
    ).toEqual({ eq: "max" })
    // A raw read takes no summary, so it names no statistic.
    const raw = planPlot(obs, HEART, { ...scope, from: "raw" })
    expect(raw.statistic).toBeUndefined()
    expect(raw.read?.filter?.properties).toBeUndefined()
    // A source whose summaries keep another statistic is left out, and the
    // chart says so; one statistic alone needs no narrowing.
    const garminDaily: Stream = {
      ...whoop,
      key: "garmin-daily",
      source: GARMIN,
      statistics: ["vendor"],
    }
    const mixed = planPlot(source([whoop, garminDaily, ...streams]), HEART, {
      ...SCOPE,
      streams: [whoop, garminDaily, garmin],
    })
    expect(mixed.statistics).toEqual(["avg", "max", "vendor"])
    expect(mixed.statisticLeftOut).toEqual([GARMIN])
    const one = planPlot(source([garminDaily, ...streams]), HEART, {
      ...SCOPE,
      streams: [garminDaily, garmin],
    })
    expect(one.statistics).toEqual(["vendor"])
    expect(one.statistic).toBeUndefined()
    expect(one.read?.filter?.properties).toBeUndefined()
  })

  it("reads one statistic of a stream that keeps raw points and summaries", () => {
    // Garmin's HRV keeps each night's average and maximum beside its
    // five-minute trace, all on one stream, and the trace's newest point is
    // newer than the night's summaries.
    const HRV = `${METRIC}/health/hrv_rmssd`
    const point = (
      start: string,
      value: string,
      form: string,
      statistic?: string
    ) => ({
      ...LATEST.rows[0],
      start,
      end: start,
      form,
      group: {
        metric: HRV,
        source: GARMIN,
        ...(statistic ? { statistic } : {}),
      },
      values: [{ reducer: "last", value, unit: "ms" }],
    })
    const [hrv, ...rest] = streamsOf({
      rows: [
        point("2026-10-06T22:00:00Z", "48", "summary", "avg"),
        point("2026-10-06T22:00:00Z", "71", "summary", "max"),
        point("2026-10-07T05:55:00Z", "52", "gauge"),
      ],
    })
    expect(rest).toEqual([])
    expect(hrv).toMatchObject({ form: "gauge", statistics: ["avg", "max"] })
    const obs = source([hrv])
    // Left automatic, it reads raw points where they cover a bucket and the
    // night's average otherwise, and offers the maximum.
    const auto = planPlot(obs, HRV, SCOPE)
    expect(auto).toMatchObject({
      shape: "gauge",
      from: "prefer-raw",
      fromReason: "some-summaries",
      summarySources: [GARMIN],
      statistics: ["avg", "max"],
      statistic: "avg",
    })
    expect(auto.read?.filter?.properties?.statistic).toEqual({ eq: "avg" })
    expect(auto.reducerMenu.find((o) => o.value === "stddev")).toMatchObject({
      raw: true,
    })
    // The reader's pick of where to read from names the statistic too.
    for (const from of ["summaries", "prefer-raw"] as const) {
      const read = planPlot(obs, HRV, { ...SCOPE, from, statistic: "max" }).read
      expect(read?.aggregate.from).toBe(from)
      expect(read?.filter?.properties?.statistic).toEqual({ eq: "max" })
    }
    const raw = planPlot(obs, HRV, { ...SCOPE, from: "raw" })
    expect(raw.statistic).toBeUndefined()
    expect(raw.read?.filter?.properties).toBeUndefined()
  })

  it("lets the reader's pick win", () => {
    const planned = planPlot(source(), RECOVERY, { ...SCOPE, from: "raw" })
    expect(planned.from).toBe("raw")
    expect(planned.fromReason).toBeUndefined()
    const auto = planPlot(source(), RECOVERY, SCOPE)
    expect(auto.from).toBe("summaries")
    expect(auto.fromReason).toBe("summaries-only")
  })
})

describe("one metric's plot", () => {
  it("reads in its unit, with its mark and band, through one aggregate read", () => {
    const heart = planPlot(source(), HEART, SCOPE)
    expect(heart).toMatchObject({
      name: "Heart rate",
      unit: "/min",
      mark: "line",
      band: true,
      reducer: "avg",
    })
    expect(heart.read?.aggregate.metric).toEqual({ ref: HEART })
    const steps = planPlot(source(), STEPS_METRIC, SCOPE)
    expect(steps).toMatchObject({ unit: "{steps}", mark: "bars", band: false })
  })

  it("reads days where each value was recorded unless the reader picks a zone", () => {
    const days = {
      ...SCOPE,
      size: "day" as const,
      timezone: "Europe/Athens",
      recordedLocal: true,
    }
    const local = planPlot(source(), HEART, days)
    expect(local.read?.aggregate.bucket).toEqual({
      calendar: "day",
      fallbackTimezone: "Europe/Athens",
    })
    // Whole local dates, which the server answers at any offset.
    expect(local.read?.aggregate.range).toEqual({
      gte: "2026-10-01",
      lt: "2026-10-08",
      recordedLocal: true,
    })
    expect(local.local).toEqual({ size: "day", range: SCOPE.range })
    const zone = planPlot(source(), HEART, { ...days, recordedLocal: false })
    expect(zone.read?.aggregate.bucket).toEqual({
      calendar: "day",
      timezone: "Europe/Athens",
    })
    expect(zone.spec?.range).toEqual(SCOPE.range)
    expect(zone.local).toBeUndefined()
    // An hour is the instant's, wherever it was lived.
    const hour = planPlot(source(), HEART, { ...days, size: "hour" })
    expect(hour.read?.aggregate.bucket).toEqual({
      calendar: "hour",
      timezone: "Europe/Athens",
    })
    expect(hour.local).toBeUndefined()
  })

  it("says where a category's buckets hold more time than they last", () => {
    const stage = { shape: "coded" as const, unit: "min", pickedKey: "sum" }
    const row = (start: string, end: string, code: string, min: string) => ({
      start,
      end,
      group: { code },
      values: [{ reducer: "sum", value: min, unit: "min" }],
      count: 1,
      bounded: 0,
      synthetic: false,
    })
    // Apple Health's night: each stage one span over the whole night, so
    // the hour it starts in holds 461 minutes.
    const hour = ["2026-10-07T23:00:00+03:00", "2026-10-08T00:00:00+03:00"]
    expect(
      overfull(stage, [
        row(hour[0], hour[1], "light", "282.6"),
        row(hour[0], hour[1], "deep", "79.8"),
        row(hour[0], hour[1], "rem", "98.8"),
      ])
    ).toBe(true)
    // The same night in a day fits.
    const day = ["2026-10-08T00:00:00+03:00", "2026-10-09T00:00:00+03:00"]
    expect(
      overfull(stage, [
        row(day[0], day[1], "light", "282.6"),
        row(day[0], day[1], "deep", "79.8"),
      ])
    ).toBe(false)
    // An hour of Garmin's segments holds at most the hour.
    expect(
      overfull(stage, [
        row(hour[0], hour[1], "light", "45"),
        row(hour[0], hour[1], "deep", "15"),
      ])
    ).toBe(false)
  })

  it("says where a line reads summaries in some buckets and raw points in others", () => {
    const row = (start: string, from: string, source?: string) => ({
      start,
      end: start,
      from,
      group: source ? { source } : {},
      values: [{ reducer: "avg", value: "80", unit: "ms" }],
      count: 1,
      bounded: 0,
      synthetic: false,
    })
    // Garmin's HRV: a nightly summary on Sep 19, its trace on Sep 18.
    expect(
      mixedFrom([
        row("2026-09-18T00:00:00+03:00", "raw"),
        row("2026-09-19T00:00:00+03:00", "summaries"),
      ])
    ).toBe(true)
    expect(
      mixedFrom([
        row("2026-09-18T00:00:00+03:00", "raw"),
        row("2026-09-19T00:00:00+03:00", "raw"),
      ])
    ).toBe(false)
    // Two series, each read one way, mix nothing.
    expect(
      mixedFrom([
        row("2026-09-18T00:00:00+03:00", "raw", GARMIN),
        row("2026-09-18T00:00:00+03:00", "summaries", WHOOP),
      ])
    ).toBe(false)
  })

  it("leaves where a night lands to the server", () => {
    // The server places a span by the metric's bucketPlace where the read
    // names no place, so every client gets the same days.
    for (const size of ["day", "week", "hour"] as const) {
      expect(
        planPlot(source(), SLEEP_STAGE, {
          ...SCOPE,
          size,
          timezone: "Europe/Athens",
          recordedLocal: true,
        }).read?.aggregate.bucket?.place
      ).toBeUndefined()
    }
  })

  it("moves its axis with the reducer and the unit picked", () => {
    expect(planPlot(source(), HEART, { ...SCOPE, reducer: "count" }).unit).toBe(
      COUNT_UNIT
    )
    expect(
      planPlot(source(), BODY_MASS, { ...SCOPE, unit: "[lb_av]" })
    ).toMatchObject({ unit: "[lb_av]", outputUnit: "[lb_av]" })
  })

  it("prefers the chosen sources in the order they were chosen", () => {
    // The Garmin wrote last; the reader chose the Apple Watch first.
    const planned = planPlot(source(), HEART, {
      ...SCOPE,
      sources: ["apple-watch-s10", GARMIN],
      sourcesRanked: true,
    })
    expect(planned.sourceOrder).toEqual([APPLE, GARMIN])
    expect(planned.ranked).toBe(true)
    expect(planned.read?.aggregate.sources).toEqual({
      policy: "priority",
      order: [APPLE, GARMIN],
    })
  })

  it("keeps the reader's order of the named sources beside a stream naming none", () => {
    // The reader put the Garmin first; the stored priorities need not. The
    // server ranks the typed values after both named sources.
    const heart = streamsOf(LATEST).find(
      (s) => s.metric === HEART && s.source === APPLE
    )!
    const typed: Stream = { ...heart, key: "typed-heart", source: undefined }
    const planned = planPlot(source([typed, ...streamsOf(LATEST)]), HEART, {
      ...SCOPE,
      sources: [GARMIN, APPLE, NONE],
      sourcesRanked: true,
    })
    expect(planned.sourceless).toBe(true)
    expect(planned.ranked).toBe(true)
    expect(planned.read?.filter?.properties?.source).toEqual({
      in: [GARMIN, APPLE, null],
    })
    expect(planned.read?.aggregate.sources).toEqual({
      policy: "priority",
      order: [GARMIN, APPLE],
    })
  })

  it("leaves the order to the sources' own priorities unless the reader ranked them", () => {
    // None chosen: no order, so the server ranks by each source record's
    // priorities for the metric.
    const none = planPlot(source(), HEART, SCOPE)
    expect(none.ranked).toBe(false)
    expect(none.sourceOrder).toEqual([GARMIN, APPLE])
    expect(none.read?.aggregate.sources).toEqual({ policy: "priority" })
    // A selection's sources narrow the read and rank nothing.
    const selection = planPlot(source(), HEART, {
      ...SCOPE,
      sources: [APPLE, GARMIN],
    })
    expect(selection.ranked).toBe(false)
    expect(selection.read?.aggregate.sources).toEqual({ policy: "priority" })
  })

  it("resolves a stream naming no source with the named ones by priority", () => {
    const streams = streamsOf(LATEST)
    const scale = streams.find((s) => s.metric === BODY_MASS)!
    const typed: Stream = {
      ...scale,
      key: "typed-weight",
      source: undefined,
      at: "2026-10-07T08:00:00Z",
    }
    const planned = planPlot(source([typed, ...streams]), BODY_MASS, SCOPE)
    expect(planned.sourceless).toBe(true)
    // One line, resolved by the server with no order: the typed values
    // rank after the scale's.
    expect(planned.read?.aggregate.groupBy).toBeUndefined()
    expect(planned.read?.aggregate.sources).toEqual({ policy: "priority" })
    expect(planned.sourceOrder).toEqual([WITHINGS])
    // A chosen source leaves the unnamed stream out of the read.
    const narrowed = planPlot(source([typed, ...streams]), BODY_MASS, {
      ...SCOPE,
      sources: [WITHINGS],
    })
    expect(narrowed.sourceless).toBe(false)
    expect(narrowed.read?.aggregate.sources).toBeUndefined()
  })

  it("asks for the unit its axis reads in where the metric names none", () => {
    // Sleep stage declares no unit; the server would answer in its first
    // stream's, which need not be the newest.
    const stage = (src: string, unit: string, at: string): Stream => ({
      key: `${src}-stage`,
      metric: SLEEP_STAGE,
      source: src,
      value: "30",
      unit,
      code: "deep",
      form: "amount",
      at,
    })
    const garmin = stage(GARMIN, "s", "2026-10-07T06:00:00Z")
    const apple = stage(APPLE, "min", "2026-10-06T06:00:00Z")
    const obs = source([garmin, apple])
    const panel = { ...SCOPE, streams: [garmin, apple] }
    const planned = planPlot(obs, SLEEP_STAGE, panel)
    expect(planned).toMatchObject({ stored: "s", unit: "s" })
    expect(planned.read?.aggregate.unit).toBe("s")
    // Picking the unit it reads in still names it; another moves the axis.
    expect(
      planPlot(obs, SLEEP_STAGE, { ...panel, unit: "s" }).read?.aggregate.unit
    ).toBe("s")
    expect(planPlot(obs, SLEEP_STAGE, { ...panel, unit: "min" })).toMatchObject(
      { unit: "min", read: { aggregate: { unit: "min" } } }
    )
    // Narrowed to one source, its stream's unit.
    expect(
      planPlot(obs, SLEEP_STAGE, { ...SCOPE, sources: [APPLE] })
    ).toMatchObject({ unit: "min", read: { aggregate: { unit: "min" } } })
    // A metric naming its unit is answered in it unasked.
    expect(planPlot(source(), HEART, SCOPE).read?.aggregate.unit).toBe(
      undefined
    )
  })

  it("narrows to the chosen channels", () => {
    const planned = planPlot(source(), HEART, {
      ...SCOPE,
      channels: ["workout"],
    })
    expect(planned.read?.filter?.properties).toEqual({
      channel: { in: ["workout"] },
    })
  })

  it("asks nothing past the bucket cap and says why", () => {
    const planned = planPlot(source(), HEART, {
      ...SCOPE,
      range: presetRange("1y", Date.parse("2026-10-07T12:00:00Z")),
      size: "15m",
    })
    expect(planned.read).toBeUndefined()
    expect(planned.blocked).toMatch(/more than the 10,000 one read may hold/)
  })

  it("raises the read's bucket budget past the default, up to the cap", () => {
    // 30 days at 15 minutes: 2,880 buckets, over the default 1,440.
    const month = presetRange("30d", Date.parse("2026-10-07T12:00:00Z"))
    const planned = planPlot(source(), HEART, {
      ...SCOPE,
      range: month,
      size: "15m",
    })
    expect(planned.blocked).toBeUndefined()
    expect(planned.read?.aggregate.budget).toEqual({ maxBuckets: 10000 })
    // Inside the default, the read names no budget.
    expect(planPlot(source(), HEART, SCOPE).read?.aggregate.budget).toBe(
      undefined
    )
  })

  it("counts each source's channels against the budget, as the read groups", () => {
    const [garmin] = streamsOf(LATEST)
    const heart = (src: string, channel?: string): Stream => ({
      ...garmin,
      key: `${src}-${channel ?? ""}`,
      source: `${SOURCE}/${src}`,
      ...(channel ? { channel } : {}),
    })
    // Six sources, seven series: WHOOP's cycle beside its workouts.
    const streams = [
      heart("a"),
      heart("b"),
      heart("c"),
      heart("d"),
      heart("e"),
      heart("whoop", "cycle"),
      heart("whoop", "workout"),
    ]
    const nine = {
      start: Date.parse("2026-10-03T00:00:00+03:00"),
      end: Date.parse("2026-10-12T00:00:00+03:00"),
    }
    const scope = { ...SCOPE, range: nine, group: "source" as const }
    // Seven series times 216 hours is 1,512, past the default: the read
    // raises its budget.
    const planned = planPlot(source(streams), HEART, scope)
    expect(planned.read?.aggregate.budget).toEqual({ maxBuckets: 10000 })
    // Without the second channel it fits: 6 times 216 hours.
    expect(
      planPlot(source(streams.slice(0, 6)), HEART, scope).read?.aggregate.budget
    ).toBeUndefined()
  })

  it("draws only the selected streams where the read takes others too", () => {
    const streams = streamsOf(LATEST)
    const garmin = streams.find(
      (s) => s.metric === HEART && s.source === GARMIN
    )!
    const apple = streams.find((s) => s.metric === HEART && s.source === APPLE)!
    const whoop = (channel?: string): Stream => ({
      ...garmin,
      key: `whoop-${channel ?? ""}`,
      source: WHOOP,
      ...(channel ? { channel } : {}),
    })
    const cycle = whoop()
    const workout = whoop("workout")
    // Apple Watch, and WHOOP's workouts: no `in` names WHOOP's stream on no
    // channel, so the read narrowed to the two sources takes it too.
    const obs = source([garmin, apple, cycle, workout])
    const scope: PlotScope = {
      ...SCOPE,
      group: "source",
      sources: [APPLE, WHOOP],
      streams: [apple, workout],
    }
    const bySource = planPlot(obs, HEART, scope)
    expect(bySource.only).toEqual([APPLE, channelKey(WHOOP, "workout")])
    expect(bySource.mixedIn).toEqual([])
    const row = (src: string, channel?: string) => ({
      start: "2026-10-01T00:00:00+03:00",
      end: "2026-10-02T00:00:00+03:00",
      group: { source: src, ...(channel ? { channel } : {}) },
      values: [{ reducer: "avg", value: "60", unit: "/min" }],
      count: 1,
      bounded: 0,
      synthetic: false,
    })
    const answer = [row(APPLE), row(WHOOP), row(WHOOP, "workout")]
    expect(plotRows(bySource, answer)).toEqual([answer[0], answer[2]])
    // Resolved into one line, the unselected stream joins it, and the plot
    // says which.
    const merged = planPlot(obs, HEART, { ...scope, group: "metric" })
    expect(merged.only).toBeUndefined()
    expect(merged.mixedIn).toEqual([cycle])
    expect(plotRows(merged, answer)).toBe(answer)
  })

  it("reads a selected stream naming no source beside a sourced one, and no other source", () => {
    const streams = streamsOf(LATEST)
    const scale = streams.find((s) => s.metric === BODY_MASS)!
    const typed: Stream = {
      ...scale,
      key: "typed-weight",
      source: undefined,
      at: "2026-10-06T07:00:00Z",
    }
    const other: Stream = { ...scale, key: "garmin-weight", source: GARMIN }
    const obs = source([typed, other, ...streams])
    // A null inside `in` names the stream with no source, so the read takes
    // the scale and the typed weigh-in, and not the watch's weight.
    const [entry] = selectionByMetric([scale, typed])
    expect(entry.sources).toEqual([WITHINGS, NONE])
    const scope: PlotScope = {
      ...SCOPE,
      size: "day",
      group: "source",
      sources: entry.sources,
      abouts: entry.abouts,
      channels: entry.channels,
      streams: entry.streams,
    }
    const bySource = planPlot(obs, BODY_MASS, scope)
    expect(bySource.sourceless).toBe(true)
    expect(bySource.read?.filter?.properties?.source).toEqual({
      in: [WITHINGS, null],
    })
    const row = (src?: string) => ({
      start: "2026-10-06T00:00:00+03:00",
      end: "2026-10-07T00:00:00+03:00",
      group: src ? { source: src } : {},
      values: [{ reducer: "avg", value: "72", unit: "kg" }],
      count: 1,
      bounded: 0,
      synthetic: false,
    })
    const answer = [row(WITHINGS), row()]
    // Grouped by source, both selected series draw.
    expect(plotRows(bySource, answer)).toEqual(answer)
    // Resolved into one line, no other source joins it, and the read sends
    // no order, since a selection ranks nothing.
    const merged = planPlot(obs, BODY_MASS, { ...scope, group: "metric" })
    expect(merged.sourceless).toBe(true)
    expect(merged.mixedIn).toEqual([])
    expect(merged.read?.aggregate.sources).toEqual({ policy: "priority" })
    expect(obs.streams).toContain(other)
  })

  // Opening one stream (the owner's steps from one source) read the
  // source's steps for everyone: the absent thing was read as every thing.
  it("reads one stream's values alone where it names no thing and no channel", () => {
    const garmin = streamsOf(LATEST).find(
      (s) => s.metric === HEART && s.source === GARMIN && !s.about
    )!
    const yours = { ...garmin, key: "yours" }
    const mias = { ...garmin, key: "mias", about: MIA }
    const workout = { ...garmin, key: "workout", channel: "workout" }
    const obs = source([yours, mias, workout])
    const search = streamSearch(yours)
    const plotted = planPlot(obs, HEART, {
      ...SCOPE,
      sources: [valueOfAddress(search.source)],
      abouts: [valueOfAddress(search.about)],
      channels: [valueOfAddress(search.channel)],
    })
    expect(plotted.read?.filter?.properties).toMatchObject({
      source: { in: [GARMIN] },
      about: { in: [null] },
      channel: { in: [null] },
    })
    expect(plotted.mixedIn).toEqual([])
    expect(plotted.blocked).toBeUndefined()
  })

  // `~none` stood for a stream naming no channel, so a channel literally
  // named `~none` read the unchannelled values instead.
  it("tells a channel named `~none` from no channel, through the address", () => {
    const garmin = streamsOf(LATEST).find(
      (s) => s.metric === HEART && s.source === GARMIN && !s.about
    )!
    const plain = { ...garmin, key: "plain" }
    const tilde = { ...garmin, key: "tilde", channel: "~none" }
    const obs = source([plain, tilde])
    const read = (stream: Stream) => {
      const search = streamSearch(stream)
      return planPlot(obs, HEART, {
        ...SCOPE,
        sources: [valueOfAddress(search.source)],
        abouts: [valueOfAddress(search.about)],
        channels: [valueOfAddress(search.channel)],
      })
    }
    expect(streamSearch(tilde).channel).toBe("~~none")
    expect(streamSearch(plain).channel).toBe("~none")
    const literal = read(tilde)
    expect(literal.read?.filter?.properties?.channel).toEqual({
      in: ["~none"],
    })
    expect(literal.mixedIn).toEqual([])
    expect(read(plain).read?.filter?.properties?.channel).toEqual({
      in: [null],
    })
  })

  it("draws only the selected things where the read, grouped by about, takes others too", () => {
    const garmin = streamsOf(LATEST).find(
      (s) => s.metric === HEART && s.source === GARMIN
    )!
    const stream = (src: string, about?: string): Stream => ({
      ...garmin,
      key: `${src}-${about ?? "owner"}`,
      source: src,
      about,
    })
    const yours = stream(GARMIN)
    const mia = stream(GARMIN, MIA)
    const leo = stream(APPLE, LEO)
    const appleYours = stream(APPLE)
    // Your Garmin and Leo's Apple Watch: you name no thing, so the read
    // narrows sources alone and takes Mia's Garmin and your Apple Watch too.
    const obs = source([yours, mia, leo, appleYours])
    const scope: PlotScope = {
      ...SCOPE,
      group: "about",
      sources: [GARMIN, APPLE],
      streams: [yours, leo],
    }
    const byAbout = planPlot(obs, HEART, scope)
    expect(byAbout.groupKey).toBe("about")
    expect(byAbout.only).toEqual(["", LEO])
    // Your Apple Watch joins your line; Mia's Garmin is a line of its own.
    expect(byAbout.mixedIn).toEqual([appleYours])
    const row = (about?: string) => ({
      start: "2026-10-01T00:00:00+03:00",
      end: "2026-10-02T00:00:00+03:00",
      group: about ? { about } : {},
      values: [{ reducer: "avg", value: "60", unit: "/min" }],
      count: 1,
      bounded: 0,
      synthetic: false,
    })
    const answer = [row(), row(MIA), row(LEO)]
    expect(plotRows(byAbout, answer)).toEqual([answer[0], answer[2]])
  })

  it("keeps a selected stream on no channel apart from the source's other channels", () => {
    const garmin = streamsOf(LATEST).find(
      (s) => s.metric === HEART && s.source === GARMIN
    )!
    const whoop = (channel?: string): Stream => ({
      ...garmin,
      key: `whoop-${channel ?? ""}`,
      source: WHOOP,
      ...(channel ? { channel } : {}),
    })
    const cycle = whoop()
    const workout = whoop("workout")
    // WHOOP's heart rate on no channel: no `in` names it, so the read
    // narrowed to WHOOP takes its workouts too, and groups by channel to
    // tell them apart.
    const obs = source([garmin, cycle, workout])
    const scope: PlotScope = {
      ...SCOPE,
      sources: [WHOOP],
      streams: [cycle],
    }
    const row = (channel?: string) => ({
      start: "2026-10-01T00:00:00+03:00",
      end: "2026-10-02T00:00:00+03:00",
      group: channel ? { channel } : {},
      values: [{ reducer: "avg", value: "60", unit: "/min" }],
      count: 1,
      bounded: 0,
      synthetic: false,
    })
    const answer = [row(), row("workout")]
    const merged = planPlot(obs, HEART, scope)
    expect(merged.read?.aggregate.groupBy).toEqual(["channel"])
    expect(merged.mixedIn).toEqual([])
    expect(plotRows(merged, answer)).toEqual([answer[0]])
    // Grouped by about, each thing's channels stay apart the same way.
    const byAbout = planPlot(obs, HEART, { ...scope, group: "about" })
    expect(byAbout.read?.aggregate.groupBy).toEqual(["about", "channel"])
    expect(plotRows(byAbout, answer)).toEqual([answer[0]])
    // Where every stream the read takes is on one channel, nothing splits.
    const alone = planPlot(source([garmin, workout]), HEART, {
      ...SCOPE,
      sources: [WHOOP],
      channels: ["workout"],
      streams: [workout],
    })
    expect(alone.read?.aggregate.groupBy).toBeUndefined()
  })

  it("keeps a series' colour whichever series a filter or a toggle hides", () => {
    const bySource = planPlot(source(), HEART, { ...SCOPE, group: "source" })
    expect(bySource.groupKey).toBe("source")
    const universe = universeOf(bySource, []).map((v) => seriesKey(HEART, v))
    const narrowed = planPlot(source(), HEART, {
      ...SCOPE,
      group: "source",
      sources: [GARMIN],
    })
    expect(universeOf(narrowed, []).map((v) => seriesKey(HEART, v))).toEqual(
      universe
    )
    const colors = seriesColors(universe)
    expect(colors.get(seriesKey(HEART, GARMIN))).toBe("var(--series-2)")
  })

  it("gives a selection its own palette, its first source the first colour", () => {
    const streams = streamsOf(LATEST)
    const garmin = streams.find(
      (s) => s.metric === HEART && s.source === GARMIN
    )!
    const planned = planPlot(source(), HEART, {
      ...SCOPE,
      group: "source",
      streams: [garmin],
    })
    const universe = universeOf(planned, []).map((v) => seriesKey(HEART, v))
    expect(universe).toEqual([seriesKey(HEART, GARMIN)])
    expect(seriesColors(universe).get(seriesKey(HEART, GARMIN))).toBe(
      "var(--series-1)"
    )
  })

  it("orders categories by the metric's own codes, then the rest", () => {
    const sleep = planPlot(source(), SLEEP_STAGE, SCOPE)
    expect(sleep.groupKey).toBe("code")
    const codes = INFOS[SLEEP_STAGE].codeSet!
    expect(codes).toContain("rem")
    expect(universeOf(sleep, ["nap", "rem"])).toEqual([...codes, "nap"])
  })

  it("gives a fixed key its slot and the rest the slots left free", () => {
    const colors = seriesColors(
      ["a", "b", "c"],
      new Map([
        ["rem", 0],
        ["deep", 2],
      ])
    )
    expect(colors.get("rem")).toBe("var(--series-1)")
    expect(colors.get("deep")).toBe("var(--series-3)")
    expect([colors.get("a"), colors.get("b"), colors.get("c")]).toEqual([
      "var(--series-2)",
      "var(--series-4)",
      "var(--series-5)",
    ])
  })

  it("greys a series past the eighth colour", () => {
    const colors = seriesColors(Array.from({ length: 9 }, (_, i) => `s${i}`))
    expect(colors.get("s7")).toBe("var(--series-8)")
    expect(colors.get("s8")).toBe("var(--faint)")
  })
})
