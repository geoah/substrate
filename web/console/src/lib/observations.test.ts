/** The observations pages' discovery: the kinds binding the observed
 * trait, what a metric's record and form let a reader ask for, and the
 * streams the latest read answers. */

import { describe, expect, it } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import {
  formOf,
  metricInfoOf,
  observedKinds,
  observedPins,
  observedVariant,
  readingText,
  recordTimelineSearch,
  reducerKey,
  reducerLabel,
  reducerOptions,
  refPathOf,
  roundDigits,
  sameRef,
  shapeOf,
  streamSearch,
  streamsOf,
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
  SOURCE,
  WEIGHING,
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

  it("rounds a computed value for display and keeps stored digits", () => {
    expect(roundDigits("63.1666666666667", 0)).toBe("63")
    expect(roundDigits("61.9372442", 2)).toBe("61.94")
    expect(roundDigits("72.40", 2)).toBe("72.40")
    expect(roundDigits("9812", 0)).toBe("9812")
    expect(readingText("160.011509893784", "[lb_av]", 1)).toBe("160.0 lb")
    // Half up on the digits themselves, which a binary float is not.
    expect(roundDigits("1.005", 2)).toBe("1.01")
    expect(roundDigits("2.675", 2)).toBe("2.68")
    expect(roundDigits("-1.005", 2)).toBe("-1.01")
    expect(roundDigits("9.995", 2)).toBe("10.00")
    expect(roundDigits("0.5", 0)).toBe("1")
    // Rounded to nothing, a value has no sign.
    expect(roundDigits("-0.004", 2)).toBe("0.00")
    expect(roundDigits("-0.4", 0)).toBe("0")
  })
})

describe("the latest read's streams", () => {
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
