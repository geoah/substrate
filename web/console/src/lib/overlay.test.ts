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
  streamsOf,
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
    // A summary in range answers only what a summary takes: no twa, no
    // stddev, and an address asking for one reads the default.
    expect(planned.reducers).toEqual([
      "avg",
      "min",
      "max",
      "first",
      "last",
      "count",
    ])
    expect(
      planPlot(source([whoop, ...streams]), HEART, {
        ...SCOPE,
        streams: [whoop, garmin],
        reducer: "twa",
      }).reducer
    ).toBe("avg")
    // A raw read leaves the summaries out, and offers the level's every one.
    expect(
      planPlot(source([whoop, ...streams]), HEART, {
        ...SCOPE,
        streams: [whoop, garmin],
        from: "raw",
      }).reducers
    ).toContain("twa")
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
    })
    expect(planned.sourceOrder).toEqual([APPLE, GARMIN])
    expect(planned.read?.aggregate.sources).toEqual({
      policy: "priority",
      order: [APPLE, GARMIN],
    })
    // With none chosen, the newest source leads.
    expect(planPlot(source(), HEART, SCOPE).sourceOrder).toEqual([
      GARMIN,
      APPLE,
    ])
  })

  it("resolves a stream naming no source with the named ones, after them", () => {
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
    // One line: the scale first, then what names no source.
    expect(planned.read?.aggregate.groupBy).toBeUndefined()
    expect(planned.read?.aggregate.sources).toEqual({
      policy: "priority",
      order: [WITHINGS],
    })
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

  it("asks nothing past the bucket budget and says why", () => {
    const planned = planPlot(source(), HEART, {
      ...SCOPE,
      range: presetRange("1y", Date.parse("2026-10-07T12:00:00Z")),
      size: "15m",
    })
    expect(planned.read).toBeUndefined()
    expect(planned.blocked).toMatch(/more than the 1,440 one read may hold/)
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
    const planned = planPlot(source(streams), HEART, scope)
    expect(planned.read).toBeUndefined()
    expect(planned.blocked).toMatch(/about 1,512 buckets/)
    // Without the second channel it fits: 6 times 216 hours.
    expect(
      planPlot(source(streams.slice(0, 6)), HEART, scope).read
    ).toBeDefined()
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
      unavailable: false,
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
      unavailable: false,
    })
    const answer = [row(), row(MIA), row(LEO)]
    expect(plotRows(byAbout, answer)).toEqual([answer[0], answer[2]])
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

  it("greys a series past the eighth colour", () => {
    const colors = seriesColors(Array.from({ length: 9 }, (_, i) => `s${i}`))
    expect(colors.get("s7")).toBe("var(--series-8)")
    expect(colors.get("s8")).toBe("var(--faint)")
  })
})
