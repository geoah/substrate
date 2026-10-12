/** Several metrics in one chart: which unit each metric's answer reads in,
 * which axis each takes, how a list of metrics splits into charts of two
 * units, what a refused third unit says, how a metric whose streams keep
 * vendor summaries is read, and which colour each series wears. */

import { describe, expect, it } from "vitest"

import {
  presetRange,
  streamsOf,
  type MetricInfo,
  type Stream,
} from "@/lib/observations"
import {
  BODY_MASS,
  GARMIN,
  HEART,
  LATEST,
  METRIC,
  RECOVERY,
  SLEEP_STAGE,
  SOURCE,
  STEPS_METRIC,
  WHOOP,
} from "@/lib/observations.test-fixtures"
import {
  COUNT_UNIT,
  answerUnitOf,
  assignAxes,
  fromOf,
  leftOutWords,
  markOf,
  planPlot,
  seriesColors,
  seriesKey,
  splitCharts,
  unitWords,
  universeOf,
  type PlotScope,
  type PlotSource,
} from "./overlay"

const INFOS: Record<string, MetricInfo> = {
  [HEART]: {
    id: "health/heart_rate",
    path: HEART,
    name: "Heart rate",
    form: "gauge",
    unit: "/min",
  },
  [STEPS_METRIC]: {
    id: "health/steps",
    path: STEPS_METRIC,
    name: "Steps",
    form: "amount",
    unit: "{steps}",
  },
  [BODY_MASS]: {
    id: "health/body_mass",
    path: BODY_MASS,
    name: "Weight",
    form: "gauge",
    unit: "kg",
  },
  [SLEEP_STAGE]: {
    id: "health/sleep_stage",
    path: SLEEP_STAGE,
    name: "Sleep stage",
    form: "amount",
    unit: "min",
    codeSet: ["rem", "deep", "light", "awake"],
  },
  [RECOVERY]: {
    id: "health/whoop_recovery",
    path: RECOVERY,
    name: "Recovery",
    form: "summary",
    unit: "%",
  },
}

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
    expect(universeOf(sleep, ["awake", "nap", "rem"])).toEqual([
      "rem",
      "deep",
      "light",
      "awake",
      "nap",
    ])
  })

  it("greys a series past the eighth colour", () => {
    const colors = seriesColors(Array.from({ length: 9 }, (_, i) => `s${i}`))
    expect(colors.get("s7")).toBe("var(--series-8)")
    expect(colors.get("s8")).toBe("var(--faint)")
  })
})
