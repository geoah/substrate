/** An aggregate answer as a chart and a table read it: one bucket axis,
 * a series per group value, a row's value per reducer, the columns and the
 * copy. */

import { describe, expect, it } from "vitest"

import type { AggregateRow } from "@/lib/api/types"
import { channelKey, reducerKey, streamsOf } from "@/lib/observations"
import {
  answerReducers,
  bucketsOf,
  rollupText,
  rowValue,
  seriesOn,
} from "@/lib/observations-answer"
import {
  GARMIN,
  HEART,
  SLEEP,
  STEPS,
  WEIGHT,
  WHOOP,
  WITHINGS,
  ZONES,
  seriesOf,
} from "@/lib/observations.test-fixtures"

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

  it("counts the rows a series cannot draw where two share its bucket", () => {
    // Two sources kept apart in a read grouped by nothing.
    const day = (source: string, value: string) => ({
      start: "2026-10-01T00:00:00+03:00",
      end: "2026-10-02T00:00:00+03:00",
      sources: [source],
      values: [{ reducer: "avg", value, unit: "kg" }],
      count: 1,
      bounded: 0,
      synthetic: false,
      unavailable: false,
    })
    const rows = [day("", "80"), day(WITHINGS, "70")]
    const [series] = seriesOn(bucketsOf([rows]), rows, "avg")
    expect(series.points.map((p) => p.value)).toEqual([80])
    expect(series.dropped).toBe(1)
    expect(
      seriesOn(bucketsOf([rows.slice(1)]), rows.slice(1), "avg")[0].dropped
    ).toBe(0)
  })

  it("reads a row answered with no values as one with none", () => {
    // A coded gauge's state that held no time: the engine writes the row's
    // empty list of values as null.
    const window = {
      start: "2026-10-01T00:00:00+03:00",
      end: "2026-10-02T00:00:00+03:00",
      count: 1,
      bounded: 0,
      synthetic: false,
      unavailable: false,
    }
    const rows: AggregateRow[] = [
      { ...window, group: { code: "open" }, values: null },
      {
        ...window,
        group: { code: "closed" },
        values: [
          {
            reducer: { timeInState: ["closed"] },
            value: "86400",
            unit: "s",
            code: "closed",
          },
        ],
      },
    ]
    const buckets = bucketsOf([rows])
    expect(
      seriesOn(buckets, rows, "timeInState", "code").map((s) => [
        s.value,
        s.points[0].value,
      ])
    ).toEqual([
      ["open", null],
      ["closed", 86400],
    ])
    expect(answerReducers(rows).map(reducerKey)).toEqual(["timeInState:closed"])
    expect(rollupText(rows, ["timeInState"], "code")).toContain("\topen\t\t1\t")
    const [stream] = streamsOf({
      rows: [{ ...rows[0], group: { metric: HEART, source: GARMIN } }],
    })
    expect(stream).toMatchObject({ metric: HEART, value: null })
  })

  it("skips a rolling value where it reads a reducer's own", () => {
    const rolled = {
      ...WEIGHT.rows[0],
      values: [
        { reducer: "avg", value: "72.9", unit: "kg", rolling: 4, of: "avg" },
        ...(WEIGHT.rows[0].values ?? []),
      ],
    }
    expect(rowValue(rolled, "avg")?.value).toBe("72.6")
    expect(answerReducers([rolled]).map(reducerKey)).toEqual([
      "avg",
      "min",
      "max",
    ])
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
