/** Record views without React: a recordview record read into blocks, the
 * sections the console cannot draw kept as problems, the window a view
 * binds on a record by its properties' names, and the reads its chart and
 * spans blocks make. The suite runs in Europe/Athens. */

import { describe, expect, it } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { METRIC_TRAIT, OBSERVED_TRAIT } from "@/lib/core-kinds"
import { isoWithOffset, type TimeRange } from "@/lib/observations-time"
import {
  amountOf,
  bandOf,
  baselineOf,
  scaleBaseline,
  baselineReducer,
  boundOf,
  breakdownItems,
  calendarGrid,
  cellFraction,
  chartReads,
  deltaWords,
  foldedBy,
  RECORD_VIEW_KIND,
  recordViewOf,
  recordViewsParams,
  sequenceOf,
  spanRows,
  spansListParams,
  spansOf,
  spansTotalsRead,
  statsReads,
  tableParams,
  toneColor,
  toneText,
  toneSoft,
  trendAnchor,
  trendRead,
  valueAt,
  viewNeedsWindow,
  readsWindow,
  viewTimelineSearch,
  viewsOf,
  type Band,
  type Block,
  type BreakdownBlock,
  type Bound,
  type ChartBlock,
  type SpansBlock,
  type StatsBlock,
  type TableBlock,
  type TrendBlock,
} from "@/lib/record-view"
import type { MetricInfo } from "@/lib/observations"
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

  it("binds no window without one, or an end after the start, and nothing without an observed kind", () => {
    expect(
      boundOf(recordViewOf(view({})), SESSION, KINDS)?.window
    ).toBeUndefined()
    const backwards = record(WORKOUT.identity, "s2", {
      begins: "2026-10-11T06:59:00Z",
      ends: "2026-10-11T05:25:00Z",
    })
    expect(boundOf(v, backwards, KINDS)).toMatchObject({ open: false })
    expect(boundOf(v, backwards, KINDS)?.window).toBeUndefined()
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
    const read = chartReads(bound, line, bound.window!, "Europe/Athens")
      .reads[0]
    expect(read.filter).toEqual({
      implements: "substrate.reamde.dev/core/observed",
      properties: { about: { exists: false } },
    })
    expect(read.aggregate).toEqual({
      metrics: [{ ref: PULSE, reducers: ["avg", "min", "max"], from: "raw" }],
      range: {
        gte: isoWithOffset(Date.parse("2026-10-11T05:25:00Z")),
        lt: isoWithOffset(Date.parse("2026-10-11T06:59:00Z")),
      },
      // 94 minutes, read by the minute.
      bucket: { every: "PT1M" },
      sources: { policy: "priority", order: [GARMIN] },
    })
  })

  it("reads a whole bucket as one row in the view's unit, about who the record names", () => {
    const about: Bound = { ...bound, about: MIA }
    const read = chartReads(about, whole, bound.window!, "UTC").reads[0]
    expect(read.filter?.properties).toEqual({ about: { eq: MIA } })
    expect(read.aggregate.bucket).toBeUndefined()
    expect(read.aggregate.reducers).toBeUndefined()
    expect(read.aggregate.metrics).toEqual([
      {
        ref: PULSE,
        reducers: [{ durationIn: [95, 114] }],
        from: "raw",
        durationUnit: "min",
      },
    ])
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
    expect(chartReads(b, whole, b.window!, "UTC").reads[0].filter).toEqual({
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
      chartReads(b, line, b.window!, "UTC").reads[0].aggregate.metrics?.map(
        (m) => m.ref
      )
    ).toEqual([PULSE])
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

// ── the blocks of version 2 ────────────────────────────────────────────────

const STEPS = `${METRIC}/body/steps`
const ROOM = record(`${AUTHORITY}/home/room`, "kitchen", { name: "Kitchen" })

function sections(...raw: Record<string, unknown>[]) {
  return recordViewOf(view({ window: WINDOW, sections: raw })).sections
}

function blockIn(raw: Record<string, unknown>): Block {
  const [s] = sections(raw)
  if (!("block" in s)) throw new Error(s.problem)
  return s.block
}

function problemIn(raw: Record<string, unknown>): string {
  const [s] = sections(raw)
  if ("block" in s) throw new Error(`admitted ${s.block.type}`)
  return s.problem
}

describe("the blocks of version 2 read into blocks", () => {
  it("reads every new block, defaults filled", () => {
    expect(
      blockIn({
        stats: {
          items: [{ metric: { ref: PULSE } }, { property: "scores.rest" }],
        },
      })
    ).toEqual({
      type: "stats",
      label: undefined,
      baseline: 0,
      sources: "prefer",
      bands: [],
      empty: "say",
      items: [
        {
          metric: PULSE,
          reducer: "last",
          key: "last",
          from: "raw",
          better: "none",
          show: "number",
        },
        {
          property: "scores.rest",
          reducer: "last",
          key: "last",
          from: "raw",
          better: "none",
          show: "number",
        },
      ],
    })
    expect(
      blockIn({
        trend: { items: [{ metric: { ref: PULSE }, reducer: "avg" }] },
      })
    ).toMatchObject({
      type: "trend",
      span: 30,
      bucket: "day",
      rolling: 0,
      sources: "prefer",
      bands: [],
      empty: "say",
      items: [{ metric: PULSE, reducer: "avg", show: "auto", from: "raw" }],
    })
    expect(
      blockIn({
        table: {
          kind: { ref: `substrate.reamde.dev/core/kind/${WORKOUT.identity}` },
          via: "plan",
          limit: 500,
        },
      })
    ).toEqual({
      type: "table",
      label: undefined,
      kind: WORKOUT.identity,
      via: "plan",
      inWindow: false,
      columns: [],
      orderBy: "at:desc",
      limit: 100,
      chart: false,
      empty: "say",
    })
    expect(
      blockIn({
        lanes: {
          rows: [
            { kind: WORKOUT.identity, spanTitle: "sport" },
            { metric: { ref: STAGE } },
          ],
        },
      })
    ).toEqual({
      type: "lanes",
      label: undefined,
      sources: "only",
      rows: [
        { kind: WORKOUT.identity, spanTitle: "sport", limit: 100 },
        { metric: STAGE, limit: 100 },
      ],
    })
    expect(
      blockIn({ breakdown: { items: [{ property: "deep" }], total: "inBed" } })
    ).toEqual({
      type: "breakdown",
      label: undefined,
      keys: [],
      items: [{ property: "deep" }],
      total: "inBed",
    })
    expect(
      blockIn({
        chart: {
          ...HEART_CHART,
          from: "summaries",
          statistic: "vendor",
          channel: "none",
          bands: [{ low: 50, high: 60, tone: "good", label: "Calm" }],
        },
      })
    ).toMatchObject({
      from: "summaries",
      statistic: "vendor",
      channel: "none",
      bands: [{ low: 50, high: 60, tone: "good", label: "Calm" }],
    })
  })

  it("reads a timeInState item's code into its reducer, and keeps a coded sum bare", () => {
    const b = blockIn({
      stats: {
        items: [
          { metric: { ref: STAGE }, reducer: "timeInState", code: "rem" },
          { metric: { ref: STAGE }, reducer: "sum", code: "deep" },
        ],
      },
    }) as StatsBlock
    expect(b.items.map((i) => [i.reducer, i.key, i.code])).toEqual([
      [{ timeInState: ["rem"] }, "timeInState:rem", "rem"],
      ["sum", "sum", "deep"],
    ])
  })

  it("keeps each rule the grammar cannot state as a problem naming it", () => {
    expect(
      problemIn({
        stats: { items: [{ metric: { ref: PULSE }, property: "rest" }] },
      })
    ).toContain("names exactly one")
    expect(
      problemIn({
        table: { kind: WORKOUT.identity, via: "plan", sameAs: "metric" },
      })
    ).toContain("a table names exactly one")
    expect(
      problemIn({
        breakdown: { property: "zones", items: [{ property: "deep" }] },
      })
    ).toContain("both a property and items")
    expect(problemIn({ breakdown: { label: "x" } })).toContain(
      "neither a property nor items"
    )
    expect(
      problemIn({
        stats: { items: [{ metric: { ref: STAGE }, reducer: "timeInState" }] },
      })
    ).toBe("A timeInState reducer needs a code.")
    expect(
      problemIn({ trend: { span: 1, items: [{ metric: { ref: PULSE } }] } })
    ).toContain("span")
    expect(
      problemIn({
        chart: { ...HEART_CHART, from: "cooked" },
      })
    ).toContain("from is raw, summaries or prefer-raw")
    expect(
      problemIn({
        stats: { items: [{ metric: { ref: PULSE }, from: "cooked" }] },
      })
    ).toContain("cooked")
    expect(
      problemIn({
        stats: {
          bands: [{ tone: "great" }],
          items: [{ metric: { ref: PULSE } }],
        },
      })
    ).toContain("tone is great")
    expect(
      problemIn({ stats: { items: [{ metric: "body/pulse" }] } })
    ).toContain("no metric record's path")
    expect(
      problemIn({
        stats: { items: [{ metric: { ref: PULSE }, reducer: "mode" }] },
      })
    ).toContain("does not read: mode")
    expect(
      problemIn({
        stats: { items: [{ metric: { ref: PULSE }, reducer: "percentile" }] },
      })
    ).toBe("A percentile reducer needs a rank.")
    expect(
      problemIn({
        lanes: { rows: [{ kind: WORKOUT.identity, metric: { ref: STAGE } }] },
      })
    ).toContain("both a kind and a metric")
    expect(
      problemIn({
        trend: {
          items: [
            { metric: { ref: PULSE } },
            { metric: { ref: PULSE }, from: "summaries" },
          ],
        },
      })
    ).toContain("twice")
    expect(problemIn({ stats: { label: "Nothing" } })).toBe(
      "This stats block names no item."
    )
    expect(
      problemIn({
        stats: { empty: "fade", items: [{ metric: { ref: PULSE } }] },
      })
    ).toBe("This stats block's empty is fade.")
    expect(
      problemIn({
        trend: { empty: "fade", items: [{ metric: { ref: PULSE } }] },
      })
    ).toBe("This trend block's empty is fade.")
  })

  it("reads a stats or trend block's empty, say where absent", () => {
    const item = { metric: { ref: PULSE } }
    expect(blockIn({ stats: { empty: "hide", items: [item] } })).toMatchObject({
      empty: "hide",
    })
    expect(blockIn({ trend: { empty: "hide", items: [item] } })).toMatchObject({
      empty: "hide",
    })
    expect(blockIn({ trend: { items: [item] } })).toMatchObject({
      empty: "say",
    })
  })

  it("names a window broken as written", () => {
    expect(
      recordViewOf(view({ window: { start: "begins", last: "P1D" } }))
        .windowProblem
    ).toContain("both last and start")
    expect(
      recordViewOf(view({ window: { about: "who", aboutSelf: true } }))
        .windowProblem
    ).toContain("both about and aboutSelf")
    expect(recordViewOf(view({ window: WINDOW })).windowProblem).toBeUndefined()
  })

  it("says which blocks read the window", () => {
    const [stats, props, table, inWindow] = sections(
      { stats: { items: [{ metric: { ref: PULSE } }] } },
      { stats: { items: [{ property: "rest" }] } },
      { table: { kind: WORKOUT.identity, via: "plan" } },
      { table: { kind: WORKOUT.identity, inWindow: true } }
    ).map((s) => ("block" in s ? s.block : undefined)) as Block[]
    expect([stats, props, table, inWindow].map(readsWindow)).toEqual([
      true,
      false,
      false,
      true,
    ])
    expect(
      viewNeedsWindow(recordViewOf(view({ window: { about: "who" } })))
    ).toBe(false)
    expect(
      viewNeedsWindow(recordViewOf(view({ window: { last: "P7D" } })))
    ).toBe(true)
  })
})

describe("the window of version 2", () => {
  const NOW = Date.parse("2026-10-11T12:00:00Z")

  it("reads an open record with no end up to now, so far", () => {
    const v = recordViewOf(
      view({ window: { start: "begins", end: "ends", open: true } })
    )
    const running = record(WORKOUT.identity, "s3", {
      begins: "2026-10-11T05:25:00Z",
    })
    expect(boundOf(v, running, KINDS, NOW)).toMatchObject({
      window: { start: Date.parse("2026-10-11T05:25:00Z"), end: NOW },
      open: true,
    })
    // An end the record holds closes it.
    expect(boundOf(v, SESSION, KINDS, NOW)).toMatchObject({
      window: { end: Date.parse("2026-10-11T06:59:00Z") },
      open: false,
    })
  })

  it("gives a record with no window of its own the last of time, about itself", () => {
    const v = recordViewOf(view({ window: { last: "P1D", aboutSelf: true } }))
    expect(boundOf(v, ROOM, KINDS, NOW)).toEqual({
      window: { start: NOW - 86_400_000, end: NOW },
      open: true,
      lastWindow: true,
      about: `${AUTHORITY}/home/room/kitchen`,
      source: undefined,
      observed: expect.any(Array),
    })
  })

  it("names the record itself as the source under sourceSelf", () => {
    const v = recordViewOf(view({ window: { last: "PT6H", sourceSelf: true } }))
    expect(boundOf(v, ROOM, KINDS, NOW)?.source).toBe(
      `${AUTHORITY}/home/room/kitchen`
    )
  })

  it("binds who a record is about with no window where the view names about alone", () => {
    const v = recordViewOf(view({ window: { about: "who" } }))
    const report = record(WORKOUT.identity, "r1", { who: { ref: MIA } })
    const b = boundOf(v, report, KINDS, NOW)!
    expect(b.window).toBeUndefined()
    expect(b.about).toBe(MIA)
    expect(viewTimelineSearch(b, v)).toBeUndefined()
  })
})

describe("the reads of version 2", () => {
  const v = recordViewOf(view({ window: WINDOW }))
  const bound = boundOf(v, SESSION, KINDS)!
  const athens = "Europe/Athens"

  it("reads every metric of a chart in one read, each entry with its own reducers, points and unit", () => {
    const chart = blockIn({
      chart: {
        metrics: [{ ref: PULSE }, { ref: STEPS }],
        reducers: [{ reducer: "avg" }, { reducer: "max" }],
        bucket: "hour",
        from: "summaries",
        statistic: "vendor",
        unit: "/min",
        channel: "none",
      },
    }) as ChartBlock
    expect(chartReads(bound, chart, bound.window!, athens).reads).toEqual([
      {
        filter: {
          implements: OBSERVED_TRAIT,
          properties: { about: { exists: false }, channel: { exists: false } },
        },
        aggregate: {
          metrics: [
            {
              ref: PULSE,
              reducers: ["avg", "max"],
              from: "summaries",
              statistic: "vendor",
              unit: "/min",
            },
            {
              ref: STEPS,
              reducers: ["avg", "max"],
              from: "summaries",
              statistic: "vendor",
              unit: "/min",
            },
          ],
          range: {
            gte: "2026-10-11T08:25:00+03:00",
            lt: "2026-10-11T09:59:00+03:00",
          },
          bucket: { calendar: "hour", timezone: athens },
          sources: { policy: "priority", order: [GARMIN] },
        },
      },
    ])
    const kept = { ...chart, channel: "wrist" }
    expect(
      chartReads(bound, kept, bound.window!, athens).reads[0].filter?.properties
        ?.channel
    ).toEqual({ eq: "wrist" })
  })

  // A read names at most 16 metrics, and the server holds its series times
  // buckets to the budget together: a chart that drew one read per metric
  // must not be refused whole for its count.
  it("splits a chart past 16 metrics into reads of 16, and raises a read's budget past the default", () => {
    const refs = Array.from({ length: 17 }, (_, i) => `${METRIC}/body/m${i}`)
    const chart = (n: number, bucket: string) =>
      blockIn({
        chart: {
          metrics: refs.slice(0, n).map((ref) => ({ ref })),
          reducers: [{ reducer: "avg" }],
          bucket,
        },
      }) as ChartBlock
    const many = chartReads(bound, chart(17, "auto"), bound.window!, athens)
    expect(many.reads.map((r) => r.aggregate.metrics?.length)).toEqual([16, 1])
    expect(many.readOf).toEqual([...Array(16).fill(0), 1])
    // 94 minutes by the minute is about 95 buckets a metric: 16 make 1,520.
    expect(many.reads[0].aggregate.budget).toEqual({ maxBuckets: 10000 })
    expect(many.reads[1].aggregate.budget).toBeUndefined()
    const few = chartReads(bound, chart(3, "auto"), bound.window!, athens)
    expect(few.reads).toHaveLength(1)
    expect(few.reads[0].aggregate.budget).toBeUndefined()
    const week: TimeRange = {
      start: Date.parse("2026-10-04T00:00:00Z"),
      end: Date.parse("2026-10-11T00:00:00Z"),
    }
    // Nine metrics by the hour over a week: 9 times 169 is 1,521.
    const hourly = chartReads(bound, chart(9, "hour"), week, athens)
    expect(hourly.reads).toHaveLength(1)
    expect(hourly.reads[0].aggregate.budget).toEqual({ maxBuckets: 10000 })
    // A day by the minute is 1,441 buckets a metric: six fit the cap.
    const day: TimeRange = { start: week.start, end: week.start + 86_400_000 }
    const minutes = chartReads(bound, chart(13, "minute"), day, athens)
    expect(minutes.reads.map((r) => r.aggregate.metrics?.length)).toEqual([
      6, 6, 1,
    ])
  })

  it("refuses a statistic on a raw read", () => {
    expect(
      problemIn({
        chart: {
          metrics: [{ ref: PULSE }],
          reducers: [{ reducer: "avg" }],
          statistic: "vendor",
        },
      })
    ).toBe(
      "This chart keeps the statistic vendor and reads from raw; a statistic narrows a metric's summaries, so read from summaries or prefer-raw."
    )
    expect(
      problemIn({
        stats: { items: [{ metric: { ref: PULSE }, statistic: "avg" }] },
      })
    ).toContain("keeps the statistic avg and reads from raw")
    expect(
      blockIn({
        stats: {
          items: [
            { metric: { ref: PULSE }, from: "prefer-raw", statistic: "avg" },
          ],
        },
      })
    ).toMatchObject({ type: "stats" })
  })

  const stats = (items: Record<string, unknown>[], baseline = 30) =>
    blockIn({ stats: { baseline, items } }) as StatsBlock

  it("reads three metrics in one value read and one baseline read over the 30 local days before the window's day", () => {
    const block = stats([
      { metric: { ref: PULSE }, from: "summaries", statistic: "vendor" },
      { metric: { ref: STEPS }, reducer: "sum" },
      { metric: { ref: STAGE }, reducer: "count" },
      { property: "rest" },
    ])
    const reads = statsReads(bound, block, athens)
    expect(reads.itemRead).toEqual([0, 0, 0, -1])
    expect(reads.value).toHaveLength(1)
    expect(reads.value[0].aggregate).toEqual({
      metrics: [
        {
          ref: PULSE,
          reducers: ["last"],
          from: "summaries",
          statistic: "vendor",
        },
        { ref: STEPS, reducers: ["sum"], from: "raw" },
        { ref: STAGE, reducers: ["count"], from: "raw" },
      ],
      range: {
        gte: "2026-10-11T08:25:00+03:00",
        lt: "2026-10-11T09:59:00+03:00",
      },
      sources: { policy: "priority", order: [GARMIN] },
    })
    expect(reads.baseline).toHaveLength(1)
    expect(reads.baseline[0].aggregate).toMatchObject({
      metrics: reads.value[0].aggregate.metrics,
      range: {
        gte: "2026-09-11T00:00:00+03:00",
        lt: "2026-10-11T00:00:00+03:00",
      },
      bucket: { calendar: "day", timezone: athens },
    })
    expect(
      statsReads(bound, { ...block, baseline: 0 }, athens).baseline
    ).toEqual([])
  })

  it("splits a metric read two ways into two reads, and shares an entry read alike", () => {
    const twice = statsReads(
      bound,
      stats([
        { metric: { ref: PULSE }, from: "summaries", statistic: "avg" },
        { metric: { ref: PULSE }, from: "summaries", statistic: "max" },
        { metric: { ref: STEPS }, reducer: "sum" },
      ]),
      athens
    )
    expect(twice.itemRead).toEqual([0, 1, 0])
    expect(
      twice.value.map((r) => r.aggregate.metrics?.map((m) => m.ref))
    ).toEqual([[PULSE, STEPS], [PULSE]])
    expect(twice.baseline).toHaveLength(2)
    const alike = statsReads(
      bound,
      stats([
        { metric: { ref: PULSE }, reducer: "avg" },
        { metric: { ref: PULSE }, reducer: "max" },
      ]),
      athens
    )
    expect(alike.itemRead).toEqual([0, 0])
    expect(alike.value[0].aggregate.metrics).toEqual([
      { ref: PULSE, reducers: ["avg", "max"], from: "raw" },
    ])
  })

  it("reads a time in a state with the plain items, ungrouped, a time unit as the times' unit", () => {
    const reads = statsReads(
      bound,
      stats([
        { metric: { ref: PULSE } },
        {
          metric: { ref: STAGE },
          reducer: "timeInState",
          code: "rem",
          unit: "h",
        },
      ]),
      athens
    )
    // Grouped by code, the server answers no row for a state a bucket held
    // none of: a week the door stayed shut would read as not measured, and
    // its baseline would skip every day without the state.
    expect(reads.itemRead).toEqual([0, 0])
    expect(reads.value).toHaveLength(1)
    expect(reads.value[0].aggregate.metrics?.[1]).toEqual({
      ref: STAGE,
      reducers: [{ timeInState: ["rem"] }],
      from: "raw",
      durationUnit: "h",
    })
    expect(reads.value[0].aggregate.groupBy).toBeUndefined()
    expect(reads.baseline[0].aggregate.groupBy).toBeUndefined()
  })

  it("reads any other coded item in a read of its own, grouped by code", () => {
    const reads = statsReads(
      bound,
      stats(
        [
          { metric: { ref: PULSE } },
          { metric: { ref: STAGE }, reducer: "sum", code: "rem" },
        ],
        0
      ),
      athens
    )
    expect(reads.itemRead).toEqual([0, 1])
    expect(reads.value[1].aggregate).toMatchObject({
      metrics: [{ ref: STAGE, reducers: ["sum"] }],
      groupBy: ["code"],
    })
    expect(reads.value[0].aggregate.groupBy).toBeUndefined()
  })

  // The server holds a read's series times buckets to its budget together.
  it("raises a baseline read's budget where its days pass the default", () => {
    const four = [PULSE, STEPS, STAGE, `${METRIC}/body/mass`].map((ref) => ({
      metric: { ref },
    }))
    const year = statsReads(bound, stats(four, 365), athens)
    expect(year.baseline[0].aggregate.budget).toEqual({ maxBuckets: 1464 })
    expect(
      statsReads(bound, stats(four, 30), athens).baseline[0].aggregate.budget
    ).toBeUndefined()
    const coded = statsReads(
      bound,
      stats([{ metric: { ref: STAGE }, reducer: "sum", code: "rem" }], 365),
      athens,
      new Map([
        [
          STAGE,
          {
            id: "stage",
            path: STAGE,
            codeSet: ["awake", "light", "deep", "rem"],
          },
        ],
      ])
    )
    expect(coded.baseline[0].aggregate.budget).toEqual({ maxBuckets: 1464 })
  })

  it("reads the metric a record's reference names", () => {
    const result = record(WORKOUT.identity, "r2", {
      begins: "2026-10-11T05:25:00Z",
      ends: "2026-10-11T06:59:00Z",
      measures: { ref: PULSE },
    })
    const reads = statsReads(
      boundOf(v, result, KINDS)!,
      stats([{ metricProperty: "measures" }], 0),
      athens,
      new Map(),
      result
    )
    expect(reads.value[0].aggregate.metrics?.[0].ref).toBe(PULSE)
  })

  // A gauge's last reading of a day is one arbitrary instant (a room's
  // light at midnight), so its baseline is the day's average; an amount
  // keeps its sum a day, and a gauge's lowest and highest stay so.
  it("reads a gauge's first or last against each day's average, and any other reducer as the item reads it", () => {
    const infos = new Map<string, MetricInfo>([
      [PULSE, { id: "pulse", path: PULSE, form: "gauge" }],
      [STEPS, { id: "steps", path: STEPS, form: "amount" }],
      [
        STAGE,
        {
          id: "stage",
          path: STAGE,
          form: "gauge",
          codeSet: ["awake", "light", "deep", "rem"],
        },
      ],
    ])
    const reads = statsReads(
      bound,
      stats([
        { metric: { ref: PULSE } },
        { metric: { ref: PULSE }, reducer: "first" },
        { metric: { ref: PULSE }, reducer: "max" },
        { metric: { ref: PULSE }, reducer: "min" },
        { metric: { ref: STEPS }, reducer: "sum" },
        { metric: { ref: STEPS }, reducer: "last" },
      ]),
      athens,
      infos
    )
    expect(reads.value[0].aggregate.metrics).toEqual([
      { ref: PULSE, reducers: ["last", "first", "max", "min"], from: "raw" },
      { ref: STEPS, reducers: ["sum", "last"], from: "raw" },
    ])
    expect(reads.baseline[0].aggregate.metrics).toEqual([
      { ref: PULSE, reducers: ["avg", "max", "min"], from: "raw" },
      { ref: STEPS, reducers: ["sum", "last"], from: "raw" },
    ])
    expect(baselineReducer("last", infos.get(PULSE))).toBe("avg")
    expect(baselineReducer("first", infos.get(PULSE))).toBe("avg")
    expect(baselineReducer("max", infos.get(PULSE))).toBe("max")
    expect(baselineReducer({ percentile: 0.9 }, infos.get(PULSE))).toEqual({
      percentile: 0.9,
    })
    expect(baselineReducer("last", infos.get(STEPS))).toBe("last")
    // A category, and a metric whose form is not known, keep the reducer.
    expect(baselineReducer("last", infos.get(STAGE))).toBe("last")
    expect(baselineReducer("last", undefined)).toBe("last")
  })

  it("reads nothing without a window", () => {
    const b: Bound = { ...bound, window: undefined }
    expect(statsReads(b, stats([{ metric: { ref: PULSE } }]), athens)).toEqual({
      value: [],
      baseline: [],
      itemRead: [-1],
    })
  })

  it("reads a trend over span buckets ending with the record's, the rolling average through the bucket before", () => {
    const block = blockIn({
      trend: {
        span: 30,
        rolling: 7,
        items: [
          { metric: { ref: PULSE }, from: "summaries", statistic: "vendor" },
          { metric: { ref: STEPS }, reducer: "sum" },
        ],
      },
    }) as TrendBlock
    expect(trendRead(bound, block, athens)).toEqual({
      filter: {
        implements: OBSERVED_TRAIT,
        properties: { about: { exists: false } },
      },
      aggregate: {
        metrics: [
          {
            ref: PULSE,
            reducers: ["last"],
            from: "summaries",
            statistic: "vendor",
          },
          { ref: STEPS, reducers: ["sum"], from: "raw" },
        ],
        range: {
          gte: "2026-09-12T00:00:00+03:00",
          lt: "2026-10-12T00:00:00+03:00",
        },
        bucket: { calendar: "day", timezone: athens },
        rolling: { buckets: 7, reducers: ["avg"], through: "previous" },
        sources: { policy: "priority", order: [GARMIN] },
      },
    })
    // Past the default budget, the read asks for the buckets it needs.
    const long = {
      ...block,
      span: 400,
      items: [...block.items, ...block.items],
    }
    expect(trendRead(bound, long, athens)?.aggregate.budget).toBeUndefined()
    const wide = blockIn({
      trend: {
        span: 400,
        items: [
          { metric: { ref: PULSE } },
          { metric: { ref: STEPS } },
          { metric: { ref: STAGE } },
          { metric: { ref: `${METRIC}/body/weight` } },
        ],
      },
    }) as TrendBlock
    expect(trendRead(bound, wide, athens)?.aggregate.budget).toEqual({
      maxBuckets: 1600,
    })
  })

  it("anchors a trend at the window's end for a last window and for a metric placing a span at its end", () => {
    const night: Bound = {
      ...bound,
      window: {
        start: Date.parse("2026-10-10T20:30:00Z"),
        end: Date.parse("2026-10-11T04:00:00Z"),
      },
    }
    expect(trendAnchor(night)).toBe(night.window!.start)
    expect(trendAnchor(night, "end")).toBe(night.window!.end - 1)
    expect(trendAnchor({ ...night, lastWindow: true })).toBe(
      night.window!.end - 1
    )
    const block = blockIn({
      trend: { span: 7, items: [{ metric: { ref: STAGE }, reducer: "sum" }] },
    }) as TrendBlock
    const ends = new Map<string, MetricInfo>([
      [STAGE, { id: "body/stage", path: STAGE, bucketPlace: "end" }],
    ])
    // The night starts on the 10th (23:30 local) and ends on the 11th.
    expect(trendRead(night, block, athens)?.aggregate.range).toEqual({
      gte: "2026-10-04T00:00:00+03:00",
      lt: "2026-10-11T00:00:00+03:00",
    })
    expect(trendRead(night, block, athens, ends)?.aggregate.range).toEqual({
      gte: "2026-10-05T00:00:00+03:00",
      lt: "2026-10-12T00:00:00+03:00",
    })
  })

  it("lays out a strip's whole calendar, a day across the change of offset 25 hours", () => {
    const days = calendarGrid(
      {
        start: Date.parse("2026-10-22T10:00:00+03:00"),
        end: Date.parse("2026-10-28T00:00:00+02:00"),
      },
      "day"
    )
    expect(days.map((d) => d.startIso)).toEqual([
      "2026-10-22T00:00:00+03:00",
      "2026-10-23T00:00:00+03:00",
      "2026-10-24T00:00:00+03:00",
      "2026-10-25T00:00:00+03:00",
      "2026-10-26T00:00:00+02:00",
      "2026-10-27T00:00:00+02:00",
    ])
    expect((days[3].end - days[3].start) / 3_600_000).toBe(25)
    expect(days.every((d) => d.rows.length === 0)).toBe(true)
    const weeks = calendarGrid(
      {
        start: Date.parse("2026-10-14T12:00:00+03:00"),
        end: Date.parse("2026-11-03T00:00:00+02:00"),
      },
      "week"
    )
    expect(weeks.map((w) => w.startIso)).toEqual([
      "2026-10-12T00:00:00+03:00",
      "2026-10-19T00:00:00+03:00",
      "2026-10-26T00:00:00+02:00",
      "2026-11-02T00:00:00+02:00",
    ])
    const months = calendarGrid(
      {
        start: Date.parse("2026-01-15T00:00:00+02:00"),
        end: Date.parse("2026-03-01T00:00:00+02:00"),
      },
      "month"
    )
    expect(months.map((m) => m.startIso)).toEqual([
      "2026-01-01T00:00:00+02:00",
      "2026-02-01T00:00:00+02:00",
    ])
  })

  const table = (raw: Record<string, unknown>) =>
    blockIn({ table: { kind: WORKOUT.identity, ...raw } }) as TableBlock
  const withAbout: KindInfo = {
    ...WORKOUT,
    definition: {
      ...WORKOUT.definition,
      properties: { about: { type: "reference" } },
    },
  }

  it("lists the records pointing here, sharing a value, or starting in the window", () => {
    expect(tableParams(bound, table({ via: "plan" }), SESSION)).toEqual({
      kinds: [WORKOUT.identity],
      first: 25,
      orderBy: "at:desc",
      filter: {
        properties: { plan: { eq: `${WORKOUT.identity}/s1` } },
      },
    })
    expect(
      tableParams(bound, table({ sameAs: "by", orderBy: "at:asc" }), SESSION)
    ).toMatchObject({
      orderBy: "at:asc",
      filter: { properties: { by: { eq: GARMIN } } },
    })
    expect(tableParams(bound, table({ sameAs: "lab" }), SESSION)).toEqual({
      problem:
        "This table lists the records sharing this record's lab, and this record holds none.",
    })
    expect(
      tableParams(bound, table({ inWindow: true }), SESSION)
    ).toMatchObject({
      filter: {
        properties: {
          at: {
            gte: "2026-10-11T05:25:00.000Z",
            lt: "2026-10-11T06:59:00.000Z",
          },
        },
      },
    })
    expect(
      tableParams(undefined, table({ inWindow: true }), SESSION)
    ).toHaveProperty("problem")
  })

  it("narrows a table by who it is about only where the listed kind declares about", () => {
    expect(
      tableParams(bound, table({ via: "plan" }), SESSION, WORKOUT)
    ).not.toHaveProperty("filter.properties.about")
    expect(
      tableParams(bound, table({ via: "plan" }), SESSION, withAbout)
    ).toHaveProperty("filter.properties.about", { exists: false })
    expect(
      tableParams(
        { ...bound, about: MIA },
        table({ via: "plan" }),
        SESSION,
        withAbout
      )
    ).toHaveProperty("filter.properties.about", { eq: MIA })
  })

  it("breaks a keyed map of durations into bars, listed keys first, each a share of the total", () => {
    const workout = record(WORKOUT.identity, "w1", {
      duration: "PT1H",
      zones: { zone1: "PT10M", zone2: "PT20M", odd: "fast" },
    })
    const block = blockIn({
      breakdown: {
        property: "zones",
        keys: [{ key: "zone2", label: "Zone 2" }],
        total: "duration",
      },
    }) as BreakdownBlock
    expect(breakdownItems(block, workout)).toEqual({
      items: [
        {
          key: "zone2",
          name: "Zone 2",
          amount: 1_200_000,
          text: "20m",
          share: 1 / 3,
        },
        {
          key: "zone1",
          name: "zone1",
          amount: 600_000,
          text: "10m",
          share: 1 / 6,
        },
      ],
      skipped: ["odd"],
    })
  })

  // A decimal property travels as a string.
  it("reads a decimal string as a number", () => {
    expect(amountOf("11.9")).toEqual({ amount: 11.9, text: "11.9" })
    expect(amountOf("-2")).toMatchObject({ amount: -2 })
    expect(amountOf("PT20M")).toEqual({ amount: 1_200_000, text: "20m" })
    expect(amountOf("high")).toBeUndefined()
  })

  it("breaks quantity properties into bars, each a share of their sum", () => {
    const body = record(WORKOUT.identity, "b1", {
      fat: { value: "20", unit: "kg" },
      lean: { value: "60", unit: "kg" },
    })
    const block = blockIn({
      breakdown: {
        items: [{ property: "fat", label: "Fat" }, { property: "lean" }],
      },
    }) as BreakdownBlock
    const { items, skipped } = breakdownItems(block, body)
    expect(skipped).toEqual([])
    expect(items.map((i) => [i.name, i.amount, i.share])).toEqual([
      ["Fat", 20, 0.25],
      ["lean", 60, 0.75],
    ])
    expect(items[0].text).toContain("20")
    expect(items[0].text).toContain("kg")
  })

  it("reads a dotted path into a keyed map", () => {
    const sleep = record(WORKOUT.identity, "n1", {
      scores: { consistency: 81 },
    })
    expect(valueAt(sleep, "scores.consistency")).toBe(81)
    expect(valueAt(sleep, "scores.missing")).toBeUndefined()
    expect(valueAt(sleep, "nothing.here")).toBeUndefined()
  })

  it("finds a value's band, an absent bound open, a metric's band for that metric alone", () => {
    const bands: Band[] = [
      { metric: STEPS, high: 1000, tone: "poor", label: "Idle" },
      { high: 34, tone: "poor", label: "Red" },
      { low: 34, high: 67, tone: "fair", label: "Yellow" },
      { low: 67, tone: "good", label: "Green" },
    ]
    expect(bandOf(10, bands, PULSE)?.label).toBe("Red")
    expect(bandOf(10, bands, STEPS)?.label).toBe("Idle")
    expect(bandOf(34, bands, PULSE)?.label).toBe("Yellow")
    expect(bandOf(500, bands, PULSE)?.label).toBe("Green")
    expect(bandOf(5, [{ metric: STEPS, tone: "good" }], PULSE)).toBeUndefined()
  })

  it("says a change against the average, toned by which direction is better", () => {
    expect(deltaWords(65, 61, "higher", "ms", 0, 30)).toEqual({
      text: "+4 ms (+7%) vs the 30-day average of 61 ms",
      tone: "good",
    })
    expect(deltaWords(65, 61, "lower", "ms").tone).toBe("poor")
    expect(deltaWords(57, 61, "lower", "ms").text).toBe(
      "-4 ms (-7%) vs the average of 61 ms"
    )
    expect(deltaWords(57, 61, "lower").tone).toBe("good")
    expect(deltaWords(65, 61, "none").tone).toBe("none")
    expect(deltaWords(61, 61, "higher").tone).toBe("none")
  })

  // A relative change of a percentage reads as a second percentage.
  it("says a change of a value in percent in points, with no relative part", () => {
    expect(deltaWords(54, 45, "higher", "%", 0, 10)).toEqual({
      text: "+9 points vs the 10-day average of 45 %",
      tone: "good",
    })
    expect(deltaWords(41, 42, "none", "%").text).toBe(
      "-1 point vs the average of 42 %"
    )
    expect(deltaWords(42.25, 42, "none", "%", 1).text).toBe(
      "+0.3 points vs the average of 42 %"
    )
  })

  // A float or decimal declares no display precision; a cell shows two
  // fraction digits at most.
  it("shows a fractional property in a table cell to two fraction digits at most", () => {
    expect(cellFraction("float", 8.3985)).toBe("8.4")
    expect(cellFraction("float", 8.3949)).toBe("8.39")
    expect(cellFraction("float", 12)).toBe("12")
    expect(cellFraction("float", 2.5)).toBe("2.5")
    expect(cellFraction("float", -0.004)).toBe("0")
    expect(cellFraction("float", 12345.678)).toBe("12,345.68")
    expect(cellFraction("decimal", "19.999")).toBe("20")
    expect(cellFraction("decimal", "72.40")).toBe("72.40")
    expect(cellFraction("int", 8)).toBeUndefined()
    expect(cellFraction("string", "8.3985")).toBeUndefined()
    expect(cellFraction("float", "n/a")).toBeUndefined()
  })

  it("takes a baseline's average, lowest and highest of the days that held a value", () => {
    const day = (value: string | null) => ({
      start: "2026-10-01T00:00:00+03:00",
      end: "2026-10-02T00:00:00+03:00",
      values: [{ reducer: "avg", value }],
      count: 1,
    })
    expect(
      baselineOf([day("60"), day("70"), day(null), day("50")], "avg")
    ).toEqual({
      avg: 60,
      low: 50,
      high: 70,
      days: 3,
    })
    expect(baselineOf([day(null)], "avg")).toBeUndefined()
  })

  it("colours a tone with the status tokens", () => {
    expect(toneColor("good")).toBe("var(--ok)")
    expect(toneColor("fair")).toBe("var(--warning)")
    expect(toneColor("poor")).toBe("var(--destructive)")
    expect(toneColor("none")).toBe("var(--faint)")
    // A toneless word is text: the faint mark token is too light for it.
    expect(toneText("none")).toBe("var(--faint-text)")
    expect(toneText("poor")).toBe("var(--destructive)")
    expect(toneSoft("poor")).toBe("var(--bad-soft)")
    expect(toneSoft("none")).toBe("var(--hover)")
  })

  it("opens the timeline with the metrics the stats and trends read", () => {
    const withTiles = recordViewOf(
      view({
        window: WINDOW,
        sections: [
          { stats: { items: [{ metric: { ref: STEPS } }, { property: "x" }] } },
          { trend: { items: [{ metric: { ref: PULSE } }] } },
        ],
      })
    )
    expect(viewTimelineSearch(bound, withTiles)?.metric).toBe(
      `${STEPS},${PULSE}`
    )
  })
})

describe("scaleBaseline", () => {
  const daily = { avg: 900, low: 300, high: 2400, days: 23 }
  const day = 86_400_000
  const week = { start: 0, end: 7 * day }

  it("compares a week of time in a state with seven average days", () => {
    expect(scaleBaseline(daily, "timeInState", week)).toEqual({
      avg: 6300,
      low: 2100,
      high: 16800,
      days: 23,
      span: 7,
    })
  })

  it("scales a sum and a time in a range, never an average", () => {
    expect(scaleBaseline(daily, "sum", week).span).toBe(7)
    expect(scaleBaseline(daily, { durationIn: [95, 114] }, week).span).toBe(7)
    expect(scaleBaseline(daily, "avg", week)).toEqual({ ...daily, span: 1 })
  })

  it("keeps the daily figures for a window of a day or less", () => {
    expect(scaleBaseline(daily, "sum", { start: 0, end: day })).toEqual({
      ...daily,
      span: 1,
    })
    expect(
      scaleBaseline(daily, "sum", { start: 0, end: 8 * 3_600_000 }).span
    ).toBe(1)
    expect(scaleBaseline(daily, "sum")).toEqual({ ...daily, span: 1 })
  })
})
