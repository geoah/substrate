/** The observations pages' discovery: the kinds binding the observed
 * trait, what a metric's record and form let a reader ask for, and the
 * streams the latest read answers. */

import { describe, expect, it } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import { FORM_REDUCERS } from "@/lib/aggregate-rules"
import {
  boundWords,
  metricKindOf,
  MENU_LEAVES_OUT,
  NONE_TOKEN,
  reducerName,
  summaryReducers,
  defaultStatistic,
  formOf,
  metricInfoOf,
  observedKinds,
  observedPins,
  observedVariant,
  judgementWords,
  rangeWords,
  readingText,
  recordTimelineSearch,
  resultReading,
  reducerKey,
  reducerChoiceWords,
  reducerLabel,
  reducerOf,
  reducerOptions,
  refPathOf,
  roundDigits,
  sameRef,
  shapeOf,
  statisticChoices,
  streamSearch,
  streamsOf,
} from "@/lib/observations"
import {
  BODY_MASS,
  GARMIN,
  HEART,
  MIA,
  WHOOP,
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
import { UnitGraph } from "@/lib/unit-graph"
import { editedTable, fixtureTable } from "@/lib/unit-graph.test-fixtures"
import { TABLE_UNITS, graphUnits, pendingUnits } from "@/lib/unit-reader"

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

  it("finds the metric kind by core's metric trait, a trait pin included", () => {
    expect(metricKindOf(KINDS, { metric: METRIC })).toBe(METRIC)
    // A pin of the trait itself names no kind: the one kind binding it.
    expect(
      metricKindOf(KINDS, { metric: "substrate.reamde.dev/core/metric" })
    ).toBe(METRIC)
    // Two metric kinds and no kind pin: no one answer.
    const other = { ...metricKind, identity: "home.example.org/climate/metric" }
    expect(metricKindOf([...KINDS, other], {})).toBeUndefined()
    expect(metricKindOf([...KINDS, other], { metric: METRIC })).toBe(METRIC)
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

  it("reads a metric's unit reference as the unit's id", () => {
    const record = METRICS.find((m) => m.id === "health/body_mass")!
    const named = (unit: unknown) =>
      metricInfoOf({ ...record, properties: { ...record.properties, unit } })
        .unit
    expect(named({ ref: "substrate.reamde.dev/core/unit/kg" })).toBe("kg")
    expect(named("substrate.reamde.dev/core/unit/1/min")).toBe("1/min")
    expect(named("kg")).toBe("kg")
    expect(named({ ref: 3 })).toBeUndefined()
  })

  it("offers the reducers each form allows, the default first", () => {
    // Every reducer docs/api.md's form table lists, as reducer keys.
    const heart = info("health/heart_rate")
    expect(reducerOptions(TABLE_UNITS, shapeOf(heart), "gauge", heart)).toEqual(
      [
        "avg",
        "min",
        "max",
        "first",
        "last",
        "count",
        "stddev",
        "percentile:0.5",
        "percentile:0.9",
      ]
    )
    // No metric is offered bands: which bands a reader wants is a record
    // view's to say, not the console's. An angle's mean is circular.
    expect(
      reducerOptions(TABLE_UNITS, "gauge", "gauge", info("health/heart_rate"))
    ).not.toContain("durationIn:95,114")
    expect(
      reducerOptions(TABLE_UNITS, "gauge", "gauge", { measures: "angle" })
    ).toContain("circularMean")
    expect(reducerOptions(TABLE_UNITS, "amount")).toEqual([
      "sum",
      "count",
      "avg",
      "min",
      "max",
      "first",
      "last",
      "stddev",
      "percentile:0.5",
      "percentile:0.9",
    ])
    expect(reducerOptions(TABLE_UNITS, "counter")).toEqual([
      "increase",
      "rate",
      "last",
      "first",
      "min",
      "max",
      "count",
    ])
    // A summary reads its one statistic: a vendor's averages first, a
    // total's sum first.
    expect(
      reducerOptions(TABLE_UNITS, "summary", "summary", undefined, "vendor")
    ).toEqual(["avg", "min", "max", "first", "last", "count"])
    expect(
      reducerOptions(TABLE_UNITS, "summary", "summary", undefined, "sum")[0]
    ).toBe("sum")
    // A coded gauge answers time per code; a coded amount takes no
    // timeInState (docs/api.md, "Reducers, by form").
    expect(reducerOptions(TABLE_UNITS, "coded", "gauge")).toEqual([
      "timeInState",
      "count",
    ])
    expect(shapeOf(info("health/sleep_stage"))).toBe("coded")
    expect(reducerOptions(TABLE_UNITS, "coded", "amount")).toEqual([
      "sum",
      "count",
      "min",
      "max",
    ])
  })

  it("offers the circular mean under the graph by the unit's turn", () => {
    // The server checks a turn on the read's unit, not a declared measure.
    const graph = graphUnits(new UnitGraph(fixtureTable))
    expect(reducerOptions(graph, "gauge", "gauge", { unit: "deg" })).toContain(
      "circularMean"
    )
    expect(
      reducerOptions(graph, "gauge", "gauge", { unit: "kg", measures: "angle" })
    ).not.toContain("circularMean")
    expect(
      reducerOptions(pendingUnits("loading"), "gauge", "gauge", { unit: "deg" })
    ).not.toContain("circularMean")
  })

  it("offers every reducer the server admits for a form, and no other", () => {
    // FORM_REDUCERS is the server's own table (aggregate.rules.json, which a
    // Go test writes), so a reducer the server adds or drops fails here
    // until a menu takes it or MENU_LEAVES_OUT says why not.
    const menus = {
      gauge: reducerOptions(TABLE_UNITS, "gauge", "gauge", {
        measures: "angle",
      }),
      amount: reducerOptions(TABLE_UNITS, "amount", "amount"),
      counter: reducerOptions(TABLE_UNITS, "counter", "counter"),
      "coded gauge": reducerOptions(TABLE_UNITS, "coded", "gauge"),
      "coded amount": reducerOptions(TABLE_UNITS, "coded", "amount"),
      summary: summaryReducers(undefined, "prefer-raw"),
    }
    for (const [form, menu] of Object.entries(menus)) {
      const names = new Set(menu.map((k) => reducerName(reducerOf(k))))
      const admitted = FORM_REDUCERS[form as keyof typeof FORM_REDUCERS]
      expect([...names].every((n) => admitted.includes(n))).toBe(true)
      expect(
        admitted.filter((n) => !(MENU_LEAVES_OUT[form] ?? []).includes(n))
      ).toEqual(expect.arrayContaining([...names]))
      expect(names.size).toBe(
        admitted.length - (MENU_LEAVES_OUT[form] ?? []).length
      )
    }
  })

  it("reads a reducer key as the aggregate asks for it", () => {
    expect(reducerOf("avg")).toBe("avg")
    expect(reducerOf("percentile:0.9")).toEqual({ percentile: 0.9 })
    expect(reducerOf("durationIn:114,133")).toEqual({ durationIn: [114, 133] })
    expect(reducerKey(reducerOf("durationIn:114,133"))).toBe(
      "durationIn:114,133"
    )
    expect(reducerChoiceWords("durationIn:114,133")).toBe(
      "Time in range 114 to 133"
    )
    expect(reducerChoiceWords("percentile:0.5")).toBe("Median")
    expect(reducerChoiceWords("percentile:0.9")).toBe("Percentile 90")
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
  })

  it("keeps a bound's comparator and says why a bounded row has no number", () => {
    expect(readingText(TABLE_UNITS, "0.01", "ng/mL", undefined, "<")).toBe(
      "<0.01 ng/mL"
    )
    expect(readingText(TABLE_UNITS, "1200", "{steps}", 0, ">=")).toBe(
      ">=1,200 steps"
    )
    const row = { count: 3, bounded: 1 }
    expect(boundWords(row, { value: null })).toBe(
      "1 of 3 values was written as a bound (such as <0.01), so this has no number"
    )
    // A number, a code or no bound at all needs no reason.
    expect(boundWords(row, { value: "4" })).toBeUndefined()
    expect(boundWords(row, { value: null, code: "deep" })).toBeUndefined()
    expect(boundWords({ count: 3 }, { value: null })).toBeUndefined()
  })

  it("rounds a computed value for display and keeps stored digits", () => {
    expect(roundDigits("63.1666666666667", 0)).toBe("63")
    expect(roundDigits("61.9372442", 2)).toBe("61.94")
    expect(roundDigits("72.40", 2)).toBe("72.40")
    expect(roundDigits("9812", 0)).toBe("9812")
    expect(readingText(TABLE_UNITS, "160.011509893784", "[lb_av]", 1)).toBe(
      "160.0 lb"
    )
    // A tooltip's stage minutes read as a sleep's page reads them.
    expect(readingText(TABLE_UNITS, "282.6", "min", 2)).toBe("4h 42m 36s")
    expect(readingText(TABLE_UNITS, "61200", "{steps}", 0)).toBe("61,200 steps")
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
    // The owner's stream names no thing, which the address says as `~none`:
    // left out, the timeline would read everyone's.
    expect(streamSearch(streams[1])).toEqual({
      metric: HEART,
      source: GARMIN,
      about: NONE_TOKEN,
      channel: "workout",
    })
  })

  it("folds a stream's statistics into one stream that names them", () => {
    // WHOOP's heart rate keeps each cycle's average and maximum: the latest
    // read groups by statistic and answers a row for each.
    const summary = (statistic: string, value: string, start: string) => ({
      ...LATEST.rows[0],
      start,
      end: start,
      form: "summary",
      group: { metric: HEART, source: WHOOP, statistic },
      values: [{ reducer: "last", value, unit: "/min" }],
      count: 30,
    })
    const streams = streamsOf({
      rows: [
        summary("max", "171", "2026-10-07T04:00:00Z"),
        summary("avg", "64", "2026-10-07T04:00:00Z"),
        LATEST.rows[0],
      ],
    })
    expect(streams).toHaveLength(2)
    expect(streams.find((s) => s.source === WHOOP)).toMatchObject({
      statistics: ["avg", "max"],
      // One instant: the default statistic's value.
      statistic: "avg",
      value: "64",
      count: 60,
    })
    expect(streams.find((s) => s.source === GARMIN)?.statistics).toBeUndefined()
  })

  it("reads the average by default, else the one statistic, else the vendor's", () => {
    expect(defaultStatistic(["max", "avg"])).toBe("avg")
    expect(defaultStatistic(["max"])).toBe("max")
    expect(defaultStatistic(["max", "vendor"])).toBe("vendor")
    expect(defaultStatistic(["min", "max"])).toBe("max")
    expect(defaultStatistic([])).toBeUndefined()
    expect(statisticChoices(["max", "vendor", "avg", "max"])).toEqual([
      "avg",
      "max",
      "vendor",
    ])
  })

  it("opens the timeline on a summary's own statistic", () => {
    const search = recordTimelineSearch({
      metric: { ref: HEART },
      source: { ref: WHOOP },
      statistic: "max",
      at: "2026-10-06T19:41:00Z",
      endsAt: "2026-10-07T19:38:00Z",
    })!
    expect(search.statistic).toBe(`${encodeURIComponent(HEART)}~max`)
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

describe("a lab result's flag and range in words", () => {
  const mg = (value: string) => ({ value, unit: "mg/dL" })
  it("says the lab's flag and its range's bounds", () => {
    const normal = [{ code: "N", display: "Normal" }]
    expect(judgementWords(TABLE_UNITS, normal, [{ high: mg("120") }])).toBe(
      "normal (under 120 mg/dL)"
    )
    expect(
      judgementWords(
        TABLE_UNITS,
        [{ code: "H" }],
        [{ low: mg("60"), high: mg("120") }]
      )
    ).toBe("high (60 to 120 mg/dL)")
    expect(judgementWords(TABLE_UNITS, [], [{ low: mg("40") }])).toBe(
      "range over 40 mg/dL"
    )
    expect(judgementWords(TABLE_UNITS, normal, undefined)).toBe("normal")
    expect(judgementWords(TABLE_UNITS, undefined, [{ text: "optimal" }])).toBe(
      "range optimal"
    )
    expect(judgementWords(TABLE_UNITS, undefined, undefined)).toBeUndefined()
  })

  it("says an HL7 flag's code in words, and keeps a lab's own spelling", () => {
    const words = (flag: Record<string, string>) =>
      judgementWords(TABLE_UNITS, [flag], undefined)
    expect(words({ code: "LL" })).toBe("critical low")
    expect(
      words({
        system:
          "http://terminology.hl7.org/CodeSystem/v3-ObservationInterpretation",
        code: "A",
      })
    ).toBe("abnormal")
    // Another system's code, and a code HL7 does not list, as written.
    expect(words({ system: "urn:lab:flags", code: "H" })).toBe("H")
    expect(words({ code: "X9" })).toBe("X9")
    // A display in sentence case reads lowered; any other as written.
    expect(words({ code: "H", display: "Above high normal" })).toBe(
      "above high normal"
    )
    expect(words({ code: "H", display: "HIGH" })).toBe("HIGH")
    expect(words({ code: "L", display: "pH Low" })).toBe("pH Low")
  })

  it("reads a range's small bounds to their significant digits", () => {
    const ng = (value: string) => ({ value, unit: "ng/mL" })
    expect(
      judgementWords(TABLE_UNITS, undefined, [{ high: ng("0.014") }])
    ).toBe("range under 0.014 ng/mL")
    // Both bounds are cut the same way.
    expect(
      judgementWords(TABLE_UNITS, undefined, [
        { low: mg("3.14159"), high: mg("5.6789") },
      ])
    ).toBe("range 3.14 to 5.68 mg/dL")
  })
})

describe("a result read against its range", () => {
  const q = (value: string, unit: string, comparator?: string) => ({
    value,
    unit,
    ...(comparator ? { comparator } : {}),
  })

  it("keeps a bound's comparator and the flag's tone", () => {
    expect(
      resultReading(TABLE_UNITS, {
        value: q("0.5", "ng/mL", "<"),
        interpretation: [{ code: "L" }],
      })
    ).toEqual({ value: 0.5, unit: "ng/mL", comparator: "<", tone: "poor" })
    expect(resultReading(TABLE_UNITS, { text: "trace" })).toBeUndefined()
  })

  it("reads a range as the lab's normal one unless it names a tone", () => {
    expect(
      resultReading(TABLE_UNITS, {
        value: q("2.8", "ng/mL"),
        referenceRanges: [{ high: q("3", "ng/mL") }, { low: q("9", "ng/mL") }],
      })?.range
    ).toEqual({ high: 3, label: "reference range", tone: "good" })
    expect(
      resultReading(TABLE_UNITS, {
        value: q("5", "ng/mL"),
        referenceRanges: [{ low: q("4", "ng/mL"), text: "high", tone: "poor" }],
      })?.range
    ).toEqual({ low: 4, label: "high", tone: "poor" })
  })

  it("converts a bound within its measure and drops a range of another", () => {
    expect(
      resultReading(TABLE_UNITS, {
        value: q("3.6", "mmol/L"),
        referenceRanges: [
          { low: q("2600", "umol/L"), high: q("3.4", "mmol/L") },
        ],
      })?.range
    ).toMatchObject({ low: 2.6, high: 3.4 })
    expect(
      resultReading(TABLE_UNITS, {
        value: q("3.6", "mmol/L"),
        referenceRanges: [{ high: q("60", "mg/dL") }],
      })?.range
    ).toBeUndefined()
  })

  it("reads a value into another unit of its measure, or not at all", () => {
    expect(
      resultReading(
        TABLE_UNITS,
        { value: q("450", "g"), measure: "mass" },
        "kg"
      )?.value
    ).toBeCloseTo(0.45, 6)
    expect(
      resultReading(TABLE_UNITS, { value: q("1.2", "ug/L") }, "ng/mL")?.value
    ).toBe(1.2)
    expect(
      resultReading(TABLE_UNITS, { value: q("92", "mg/dL") }, "mmol/L")
    ).toBeUndefined()
  })

  it("converts through the repository's graph where the server reads one", () => {
    // The repository's pound is 500 g; the table's is 453.59237 g.
    const edited = graphUnits(new UnitGraph(editedTable))
    const p = {
      value: q("1", "kg"),
      referenceRanges: [{ low: q("1", "sack25"), high: q("2", "sack25") }],
    }
    expect(resultReading(edited, p, "lb")).toMatchObject({
      value: 2,
      unit: "lb",
      range: { low: 50, high: 100 },
    })
    expect(resultReading(TABLE_UNITS, p, "[lb_av]")?.value).toBeCloseTo(
      2.20462,
      5
    )
    expect(
      resultReading(edited, { value: q("4", "sack25") }, "kg")?.value
    ).toBe(100)
    expect(rangeWords(edited, { low: q("1", "sack25") })).toBe("over 1 sack")
    expect(readingText(edited, "2", "sack25")).toBe("2 sack")
  })

  it("converts a difference through the graph by the row's delta", () => {
    const edited = graphUnits(new UnitGraph(editedTable))
    expect(
      resultReading(edited, { value: q("1", "Cel"), delta: true }, "degF")
        ?.value
    ).toBe(1.8)
    expect(
      resultReading(edited, { value: q("1", "Cel") }, "degF")?.value
    ).toBeCloseTo(33.8, 6)
  })

  it("converts nothing while the graph loads", () => {
    const loading = pendingUnits("loading")
    expect(
      resultReading(loading, { value: q("1", "kg") }, "[lb_av]")
    ).toBeUndefined()
    expect(resultReading(loading, { value: q("1", "kg") })?.value).toBe(1)
  })
})
