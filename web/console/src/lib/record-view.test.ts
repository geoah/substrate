/** Record views without React: a recordview record read into blocks, the
 * sections the console cannot draw kept as problems, the window a view
 * binds on a record by its properties' names, and the reads its chart and
 * spans blocks make. The suite runs in Europe/Athens. */

import { describe, expect, it } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { METRIC_TRAIT, OBSERVED_TRAIT } from "@/lib/core-kinds"
import { isoWithOffset } from "@/lib/observations-time"
import {
  boundOf,
  chartRead,
  foldedBy,
  RECORD_VIEW_KIND,
  recordViewOf,
  recordViewsParams,
  sequenceOf,
  spanRows,
  spansListParams,
  spansOf,
  spansTotalsRead,
  viewTimelineSearch,
  viewsOf,
  type Bound,
  type ChartBlock,
  type SpansBlock,
} from "@/lib/record-view"
import {
  AUTHORITY,
  GARMIN,
  KINDS,
  METRIC,
  MIA,
} from "@/lib/observations.test-fixtures"
import { NONE_TOKEN } from "@/lib/observations"

const WORKOUT: KindInfo = {
  identity: `${AUTHORITY}/fitness/session`,
  name: "session",
  authority: AUTHORITY,
  package: "fitness",
  version: 1,
  source: "installed",
  description: "",
  definition: { authority: AUTHORITY, package: "fitness" },
}

function record(
  kind: string,
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  return {
    id,
    kind,
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-10-11T07:00:00Z",
    updatedAt: "2026-10-11T07:00:00Z",
  }
}

const WINDOW = { start: "begins", end: "ends", about: "who", source: "by" }

// A view names each metric by a reference to its record, as the engine
// stores one.
const PULSE = `${METRIC}/body/pulse`
const STAGE = `${METRIC}/body/stage`

function view(properties: Record<string, unknown>, id = "fitness/session") {
  return record(RECORD_VIEW_KIND, id, {
    appliesTo: { ref: `substrate.reamde.dev/core/kind/${WORKOUT.identity}` },
    ...properties,
  })
}

const HEART_CHART = {
  label: "Heart rate",
  metrics: [{ ref: PULSE }],
  reducers: [
    { reducer: "avg", label: "Average" },
    { reducer: "min" },
    { reducer: "max" },
  ],
}

const SESSION = record(WORKOUT.identity, "s1", {
  begins: "2026-10-11T05:25:00Z",
  ends: "2026-10-11T06:59:00Z",
  by: { ref: GARMIN },
})

describe("a record view read into blocks", () => {
  it("reads every block type, defaults filled", () => {
    const v = recordViewOf(
      view({
        name: "During this session",
        intro: "What was measured.",
        window: WINDOW,
        fold: ["distance", "distance", "pace"],
        sections: [
          { heading: { text: "Effort", note: "how hard" } },
          { text: { text: "Read **this** first." } },
          {
            properties: {
              label: "At a glance",
              list: [{ property: "distance", label: "Distance" }],
            },
          },
          { related: { property: "route" } },
          {
            related: {
              // Stored as a reference to the kind's declaration.
              kind: {
                ref: `substrate.reamde.dev/core/kind/${AUTHORITY}/fitness/lap`,
              },
              via: "session",
              show: "count",
            },
          },
          { chart: HEART_CHART },
          {
            chart: {
              metrics: [{ ref: PULSE }],
              bucket: "whole",
              unit: "min",
              sources: "priority",
              reducers: [
                { reducer: "durationIn", low: 95, high: 114, label: "Easy" },
                { reducer: "percentile", rank: 0.9 },
              ],
            },
          },
          { spans: { metric: { ref: STAGE }, codes: [{ code: "deep" }] } },
        ],
      })
    )
    expect(v).toMatchObject({
      id: "fitness/session",
      name: "During this session",
      intro: "What was measured.",
      window: WINDOW,
    })
    expect(v.sections.map((s) => ("block" in s ? s.block.type : s))).toEqual([
      "heading",
      "text",
      "properties",
      "related",
      "related",
      "chart",
      "chart",
      "spans",
    ])
    const [, , , outgoing, incoming, line, whole, spans] = v.sections.map(
      (s) => ("block" in s ? s.block : undefined)
    )
    expect(outgoing).toEqual({
      type: "related",
      label: undefined,
      property: "route",
      show: "list",
      limit: 10,
    })
    expect(incoming).toMatchObject({
      kind: `${AUTHORITY}/fitness/lap`,
      show: "count",
      via: "session",
    })
    expect(line).toMatchObject({
      bucket: "auto",
      sources: "prefer",
      reducers: [
        { reducer: "avg", key: "avg", label: "Average" },
        { reducer: "min", key: "min" },
        { reducer: "max", key: "max" },
      ],
    })
    expect((whole as ChartBlock).reducers).toEqual([
      {
        reducer: { durationIn: [95, 114] },
        key: "durationIn:95,114",
        label: "Easy",
      },
      { reducer: { percentile: 0.9 }, key: "percentile:0.9" },
    ])
    expect(spans).toMatchObject({ totals: true, sources: "only" })
    expect(foldedBy([v])).toEqual(["distance", "pace"])
  })

  it("keeps a section it cannot draw as the reason why", () => {
    const v = recordViewOf(
      view({
        sections: [
          {},
          { gauge: { text: "x" } },
          { heading: { text: "A" }, text: { text: "B" } },
          {
            chart: {
              metrics: [{ ref: PULSE }],
              reducers: [{ reducer: "median" }],
            },
          },
          {
            chart: {
              metrics: [{ ref: PULSE }],
              reducers: [{ reducer: "durationIn" }],
            },
          },
          {
            chart: {
              metrics: [{ ref: PULSE }],
              reducers: [{ reducer: "durationIn", low: 133, high: 114 }],
            },
          },
          {
            chart: {
              metrics: [{ ref: PULSE }],
              reducers: [{ reducer: "percentile" }],
            },
          },
          {
            chart: {
              metrics: [{ ref: PULSE }],
              bucket: "fortnight",
              reducers: [{ reducer: "avg" }],
            },
          },
          { related: { show: "list" } },
          // A bare id names no metric kind, and two may hold it.
          {
            chart: { metrics: ["body/pulse"], reducers: [{ reducer: "avg" }] },
          },
          { spans: { metric: "body/stage" } },
          { heading: { text: "Kept" } },
        ],
      })
    )
    expect(
      v.sections.map((s) => ("problem" in s ? s.problem : "drawn"))
    ).toEqual([
      "This section holds no block.",
      "This section holds a block this console does not draw: gauge.",
      "This section holds 2 blocks (heading, text); a section holds one.",
      "This chart names a reducer this console does not read: median.",
      "A durationIn reducer needs a low and a high bound.",
      "A durationIn reducer's low bound must be under its high one: 133 to 114.",
      "A percentile reducer needs a rank.",
      "This chart's bucket is fortnight.",
      "This related block names neither one outgoing reference property nor a kind and the property on it that points here.",
      "This chart names a metric by body/pulse, which is no metric record's path: a view names each metric by a reference to its record.",
      "This spans block names a metric by body/stage, which is no metric record's path: a view names each metric by a reference to its record.",
      "drawn",
    ])
  })

  it("orders a kind's views by id and asks for them by the kind's declaration", () => {
    expect(viewsOf([view({}, "b"), view({}, "a")]).map((v) => v.id)).toEqual([
      "a",
      "b",
    ])
    expect(recordViewsParams(WORKOUT.identity)).toEqual({
      kinds: [RECORD_VIEW_KIND],
      first: 20,
      filter: {
        properties: {
          appliesTo: {
            eq: `substrate.reamde.dev/core/kind/${WORKOUT.identity}`,
          },
        },
      },
    })
  })
})

describe("the window a view binds on a record", () => {
  const v = recordViewOf(view({ window: WINDOW }))

  it("reads the record's own properties by the names the view gives", () => {
    expect(boundOf(v, SESSION, KINDS)).toMatchObject({
      window: {
        start: Date.parse("2026-10-11T05:25:00Z"),
        end: Date.parse("2026-10-11T06:59:00Z"),
      },
      about: undefined,
      source: GARMIN,
    })
  })

  it("binds nothing without a window, an end after the start, or an observed kind", () => {
    expect(boundOf(recordViewOf(view({})), SESSION, KINDS)).toBeUndefined()
    const backwards = record(WORKOUT.identity, "s2", {
      begins: "2026-10-11T06:59:00Z",
      ends: "2026-10-11T05:25:00Z",
    })
    expect(boundOf(v, backwards, KINDS)).toBeUndefined()
    expect(boundOf(v, SESSION, [WORKOUT])).toBeUndefined()
  })
})

describe("the reads over the window", () => {
  const v = recordViewOf(
    view({
      window: WINDOW,
      sections: [
        { chart: HEART_CHART },
        {
          chart: {
            metrics: [{ ref: PULSE }],
            bucket: "whole",
            unit: "min",
            reducers: [{ reducer: "durationIn", low: 95, high: 114 }],
          },
        },
        { spans: { metric: { ref: STAGE } } },
      ],
    })
  )
  const bound = boundOf(v, SESSION, KINDS)!
  const [line, whole, spans] = v.sections.map((s) =>
    "block" in s ? s.block : undefined
  ) as [ChartBlock, ChartBlock, SpansBlock]

  it("reads a line over the window, the record's source first", () => {
    const read = chartRead(bound, line, PULSE, bound.window, "Europe/Athens")
    expect(read.filter).toEqual({
      implements: "substrate.reamde.dev/core/observed",
      properties: { about: { exists: false } },
    })
    expect(read.aggregate).toEqual({
      metric: { ref: PULSE },
      from: "raw",
      range: {
        gte: isoWithOffset(Date.parse("2026-10-11T05:25:00Z")),
        lt: isoWithOffset(Date.parse("2026-10-11T06:59:00Z")),
      },
      // 94 minutes, read by the minute.
      bucket: { every: "PT1M" },
      reducers: ["avg", "min", "max"],
      sources: { policy: "priority", order: [GARMIN] },
    })
  })

  it("reads a whole bucket as one row in the view's unit, about who the record names", () => {
    const about: Bound = { ...bound, about: MIA }
    const read = chartRead(about, whole, PULSE, bound.window, "UTC")
    expect(read.filter?.properties).toEqual({ about: { eq: MIA } })
    expect(read.aggregate.bucket).toBeUndefined()
    expect(read.aggregate.durationUnit).toBe("min")
    expect(read.aggregate.reducers).toEqual([{ durationIn: [95, 114] }])
  })

  it("reads the owner's observations where the view binds no about", () => {
    const own = recordViewOf(
      view({ window: { start: "begins", end: "ends", source: "by" } })
    )
    const b = boundOf(
      own,
      { ...SESSION, properties: { ...SESSION.properties, who: { ref: MIA } } },
      KINDS
    )!
    expect(b.about).toBeUndefined()
    expect(chartRead(b, whole, PULSE, b.window, "UTC").filter).toEqual({
      implements: "substrate.reamde.dev/core/observed",
      properties: { about: { exists: false } },
    })
  })

  it("reads spans from the record's own source alone, by code", () => {
    expect(spansTotalsRead(bound, spans)).toMatchObject({
      filter: {
        properties: { about: { exists: false }, source: { eq: GARMIN } },
      },
      aggregate: {
        metric: { ref: STAGE },
        groupBy: ["code"],
        reducers: ["sum"],
        unit: "min",
      },
    })
    expect(spansTotalsRead(bound, spans).aggregate.sources).toBeUndefined()
    // A record naming no source takes one source's spans, by priority.
    expect(
      spansTotalsRead({ ...bound, source: undefined }, spans).aggregate.sources
    ).toEqual({ policy: "priority" })
    expect(spansListParams(bound, spans)).toMatchObject({
      orderBy: "at:asc",
      filter: {
        properties: {
          metric: { eq: STAGE },
          source: { eq: GARMIN },
          at: {
            gte: "2026-10-11T05:25:00.000Z",
            lt: "2026-10-11T06:59:00.000Z",
          },
        },
      },
    })
  })

  // Before the views named their metrics in full, every view read its ids
  // under one repository-wide metric kind, so a new observed kind pinned to
  // another metric kind redirected every view to that kind's records.
  it("reads the metric the view names, whatever other metric kinds the repository holds", () => {
    const other = `${AUTHORITY}/aardvark/metric`
    const aardvark = (name: string, definition: Record<string, unknown>) => ({
      ...WORKOUT,
      identity: `${AUTHORITY}/aardvark/${name}`,
      name,
      package: "aardvark",
      definition: { authority: AUTHORITY, package: "aardvark", ...definition },
    })
    const kinds: KindInfo[] = [
      aardvark("metric", { traits: [METRIC_TRAIT] }),
      aardvark("reading", {
        traits: [`${OBSERVED_TRAIT}(single)`],
        properties: { metric: { type: "reference", kind: other } },
      }),
      ...KINDS,
    ]
    const b = boundOf(v, SESSION, kinds)!
    expect(
      chartRead(b, line, line.metrics[0], b.window, "UTC").aggregate.metric
    ).toEqual({ ref: PULSE })
    expect(spansTotalsRead(b, spans).aggregate.metric).toEqual({ ref: STAGE })
  })

  it("opens the timeline over the window with every line and spans metric", () => {
    expect(viewTimelineSearch(bound, v)).toEqual({
      metric: `${PULSE},${STAGE}`,
      // The owner's, as the record names nobody: never everyone's.
      about: NONE_TOKEN,
      range: "custom",
      start: "2026-10-11T05:25:00.000Z",
      end: "2026-10-11T06:59:00.000Z",
    })
  })
})

describe("a coded metric's spans", () => {
  const span = (code: string, at: string, endsAt: string) =>
    record(`${AUTHORITY}/observations/observation`, `${code}/${at}`, {
      code,
      at,
      endsAt,
    })

  it("draws a sequence where the spans follow one another, rows in the view's order", () => {
    const spans = spansOf([
      span("light", "2026-10-06T23:20:00+03:00", "2026-10-07T00:05:00+03:00"),
      span("awake", "2026-10-06T23:10:00+03:00", "2026-10-06T23:20:00+03:00"),
      span("deep", "2026-10-07T00:05:00+03:00", "2026-10-07T01:00:00+03:00"),
      span("rem", "2026-10-07T01:00:00+03:00", "2026-10-07T01:30:00+03:00"),
      span("odd", "2026-10-07T01:30:00+03:00", "2026-10-07T01:40:00+03:00"),
      { ...span("rem", "", ""), properties: { code: "rem" } },
    ])
    expect(spans.map((s) => s.code)).toEqual([
      "awake",
      "light",
      "deep",
      "rem",
      "odd",
    ])
    expect(sequenceOf(spans)).toHaveLength(5)
    // The view's order, then a code it does not list.
    expect(
      spanRows(spans, ["awake", "in_bed", "rem", "light", "deep"])
    ).toEqual(["awake", "rem", "light", "deep", "odd"])
  })

  it("draws none where each span covers the whole window", () => {
    const whole = ["awake", "light"].map((code) =>
      span(code, "2026-10-06T23:10:00+03:00", "2026-10-07T06:20:00+03:00")
    )
    expect(sequenceOf(spansOf(whole))).toBeUndefined()
  })
})
