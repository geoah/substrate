/** Fixtures for the observations suites, shaped like the wire contract the
 * observations brief pins and built from the proposal's worked examples
 * (docs/plans/observations/proposal/02*.md): the registry an imported
 * `observations` sample leaves, its metric records, a latest read of six
 * streams, and four answers (weekly heart rate zones, daily steps with
 * `from` per row, a weight trend, sleep stages grouped by code). Only test
 * suites import this file. */

import { fireEvent, screen, waitFor } from "@testing-library/react"
import { expect } from "vitest"

import type {
  Aggregate,
  AggregateGroupKey,
  AggregateResult,
  AggregateRow,
  KindInfo,
  RecordFilter,
  SubstrateRecord,
} from "@/lib/api/types"
import {
  CORE_PACKAGE,
  KIND_KIND,
  METRIC_TRAIT,
  OBSERVED_TRAIT,
} from "@/lib/core-kinds"
import {
  bucketsOf,
  seriesOn,
  type Bucket,
  type Series,
} from "@/lib/observations-answer"

export const AUTHORITY = "ada.example.com"
export const OBSERVATION = `${AUTHORITY}/observations/observation`
export const TRACE = `${AUTHORITY}/observations/trace`
export const METRIC = `${AUTHORITY}/observations/metric`
export const SOURCE = `${AUTHORITY}/observations/source`

function kind(
  name: string,
  traits: string[],
  properties: Record<string, unknown>
): KindInfo {
  return {
    identity: `${AUTHORITY}/observations/${name}`,
    name,
    authority: AUTHORITY,
    package: "observations",
    version: 1,
    source: "installed",
    description: "",
    definition: {
      authority: AUTHORITY,
      package: "observations",
      traits,
      properties,
    },
  }
}

const shared = {
  metric: { type: "reference", kind: METRIC },
  source: { type: "reference", kind: SOURCE },
  about: { type: "reference", kind: "any" },
  form: { type: "string" },
  at: { type: "datetime" },
  endsAt: { type: "datetime" },
  revision: { type: "int" },
}

export const observationKind = kind(
  "observation",
  [`${OBSERVED_TRAIT}(single)`],
  {
    ...shared,
    value: { type: "quantity" },
    code: { type: "string" },
    statistic: { type: "string" },
    period: { type: "string" },
    origin: { type: "reference" },
  }
)

export const traceKind = kind("trace", [`${OBSERVED_TRAIT}(packed)`], {
  ...shared,
  unit: { type: "string" },
  every: { type: "duration" },
  offsets: { type: "int", repeated: true },
  values: { type: "decimal", repeated: true },
  codes: { type: "string", repeated: true },
  stats: { type: "json", managed: true },
})

export const metricKind = kind("metric", [METRIC_TRAIT], {
  name: { type: "string" },
  form: { type: "string" },
  unit: { type: "string" },
  codeSet: { type: "string", repeated: true },
})

export const sourceKind = kind("source", [`${CORE_PACKAGE}/source`], {
  name: { type: "string" },
})

export const KINDS: KindInfo[] = [
  observationKind,
  traceKind,
  metricKind,
  sourceKind,
]

function record(
  kindRef: string,
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  return {
    id,
    kind: kindRef,
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-10-07T04:12:31Z",
    updatedAt: "2026-10-07T04:12:31Z",
  }
}

export const METRICS: SubstrateRecord[] = [
  record(METRIC, "health/heart_rate", {
    title: "Heart rate",
    name: "Heart rate",
    form: "gauge",
    unit: "/min",
    category: "heart",
  }),
  record(METRIC, "health/steps", {
    title: "Steps",
    name: "Steps",
    form: "amount",
    unit: "{steps}",
  }),
  record(METRIC, "health/body_mass", {
    title: "Weight",
    name: "Weight",
    form: "gauge",
    unit: "kg",
  }),
  record(METRIC, "health/sleep_stage", {
    title: "Sleep stage",
    name: "Sleep stage",
    form: "amount",
    unit: "min",
    codeSet: ["rem", "deep", "light", "awake"],
    bucketPlace: "end",
  }),
  record(METRIC, "health/whoop_recovery", {
    title: "Recovery",
    name: "Recovery",
    form: "summary",
    unit: "%",
    bucketPlace: "end",
  }),
  record(METRIC, "health/energy_expended", {
    title: "Energy expended",
    name: "Energy expended",
    form: "amount",
    unit: "kJ",
  }),
]

export const SOURCES: SubstrateRecord[] = [
  // Ranked first for heart rate, though its path sorts after the watch's.
  record(SOURCE, "garmin-forerunner-265", {
    title: "Garmin Forerunner 265",
    priorities: { [`${METRIC}/health/heart_rate`]: 1 },
  }),
  record(SOURCE, "apple-watch-s10", { title: "Apple Watch S10" }),
  record(SOURCE, "withings-body-plus", { title: "Withings Body+" }),
  record(SOURCE, "whoop-4-0", { title: "WHOOP 4.0" }),
]

const HEAD = { changelog: 2171234, generation: "7f3a0c2e9b1d4e6f" }

/** A row as the server writes it: `count` always present, `bounded` and
 * `synthetic` here as a reader may meet them. */
function row(
  r: Partial<AggregateRow> & Pick<AggregateRow, "start" | "end" | "values">
): AggregateRow {
  return { count: 0, bounded: 0, synthetic: false, ...r }
}

/** Record paths, the spelling every group value and `sources` carry. */
export const HEART = `${METRIC}/health/heart_rate`
export const STEPS_METRIC = `${METRIC}/health/steps`
export const BODY_MASS = `${METRIC}/health/body_mass`
export const SLEEP_STAGE = `${METRIC}/health/sleep_stage`
export const RECOVERY = `${METRIC}/health/whoop_recovery`
export const GARMIN = `${SOURCE}/garmin-forerunner-265`
export const APPLE = `${SOURCE}/apple-watch-s10`
export const WITHINGS = `${SOURCE}/withings-body-plus`
export const WHOOP = `${SOURCE}/whoop-4-0`
export const MIA = "ada.example.com/people/person/mia"

/** The latest read: one row per stream, its `start` the newest point's
 * time and its `count` the stream's points. */
export const LATEST: AggregateResult = {
  exact: true,
  head: HEAD,
  rows: [
    row({
      start: "2026-10-07T06:59:58Z",
      end: "2026-10-07T06:59:58Z",
      form: "gauge",
      group: { metric: HEART, source: GARMIN },
      values: [{ reducer: "last", value: "96", unit: "/min" }],
      count: 1812,
    }),
    row({
      start: "2026-10-07T05:00:00Z",
      end: "2026-10-07T05:00:00Z",
      form: "gauge",
      group: { metric: HEART, source: APPLE },
      values: [{ reducer: "last", value: "61", unit: "/min" }],
      count: 310,
    }),
    row({
      start: "2026-10-07T06:45:00Z",
      end: "2026-10-07T07:00:00Z",
      form: "amount",
      group: { metric: STEPS_METRIC, source: GARMIN },
      values: [{ reducer: "last", value: "412", unit: "{steps}" }],
      count: 96,
    }),
    row({
      start: "2026-10-07T03:41:12Z",
      end: "2026-10-07T03:41:12Z",
      form: "gauge",
      group: { metric: BODY_MASS, source: WITHINGS },
      values: [{ reducer: "last", value: "72.40", unit: "kg" }],
      count: 9,
    }),
    row({
      start: "2026-10-06T19:41:00Z",
      end: "2026-10-07T19:38:00Z",
      form: "summary",
      group: { metric: RECOVERY, source: WHOOP },
      values: [{ reducer: "last", value: "67", unit: "%" }],
      count: 1,
    }),
    row({
      start: "2026-10-06T23:10:00Z",
      end: "2026-10-07T06:20:00Z",
      form: "amount",
      group: { metric: SLEEP_STAGE, source: WHOOP, about: MIA },
      // A coded `last` answers its code, with `value` null.
      values: [{ reducer: "last", value: null, code: "awake" }],
      count: 4,
    }),
  ],
}

export const LEO = "ada.example.com/people/person/leo"

/** The latest read with two people's heart rate beside the owner's: Mia's
 * from the Garmin and Leo's from the Apple Watch, so the About facet has
 * two records to choose between. */
export const LATEST_PEOPLE: AggregateResult = {
  ...LATEST,
  rows: [
    ...LATEST.rows,
    row({
      start: "2026-10-07T06:30:00Z",
      end: "2026-10-07T06:30:00Z",
      form: "gauge",
      group: { metric: HEART, source: GARMIN, about: MIA },
      values: [{ reducer: "last", value: "88", unit: "/min" }],
      count: 40,
    }),
    row({
      start: "2026-10-07T06:20:00Z",
      end: "2026-10-07T06:20:00Z",
      form: "gauge",
      group: { metric: HEART, source: APPLE, about: LEO },
      values: [{ reducer: "last", value: "71", unit: "/min" }],
      count: 25,
    }),
  ],
}

/** The latest read with WHOOP's heart rate twice: on no channel (its
 * cycles) and on its `workout` channel. */
export const LATEST_CHANNELS: AggregateResult = {
  ...LATEST,
  rows: [
    ...LATEST.rows,
    row({
      start: "2026-10-07T05:00:00Z",
      end: "2026-10-07T05:00:00Z",
      form: "gauge",
      group: { metric: HEART, source: WHOOP },
      values: [{ reducer: "last", value: "58", unit: "/min" }],
      count: 300,
    }),
    row({
      start: "2026-10-06T18:00:00Z",
      end: "2026-10-06T18:00:00Z",
      form: "gauge",
      group: { metric: HEART, source: WHOOP, channel: "workout" },
      values: [{ reducer: "last", value: "141", unit: "/min" }],
      count: 120,
    }),
  ],
}

/** Heart rate zones, weekly, five `durationIn` reducers in minutes. */
export const ZONES: AggregateResult = {
  metric: HEART,
  unit: "/min",
  bucket: { calendar: "week", weekStart: "monday", timezone: "Europe/Athens" },
  from: "raw",
  exact: false,
  head: HEAD,
  sources: [GARMIN, APPLE],
  rows: [
    row({
      start: "2026-04-06T00:00:00+03:00",
      end: "2026-04-13T00:00:00+03:00",
      form: "gauge",
      from: "raw",
      sources: [GARMIN],
      coverage: "P6DT22H10M",
      values: [
        { reducer: { durationIn: [95, 114] }, value: "612.4", unit: "min" },
        { reducer: { durationIn: [114, 133] }, value: "171", unit: "min" },
        { reducer: { durationIn: [133, 152] }, value: "88.5", unit: "min" },
        { reducer: { durationIn: [152, 171] }, value: "21", unit: "min" },
        { reducer: { durationIn: [171, 220] }, value: "2.5", unit: "min" },
      ],
      count: 24810,
    }),
    row({
      start: "2026-04-13T00:00:00+03:00",
      end: "2026-04-20T00:00:00+03:00",
      form: "gauge",
      from: "raw",
      sources: [GARMIN, APPLE],
      coverage: "P6DT20H",
      values: [
        { reducer: { durationIn: [95, 114] }, value: "540", unit: "min" },
        { reducer: { durationIn: [114, 133] }, value: "201.5", unit: "min" },
        { reducer: { durationIn: [133, 152] }, value: "60", unit: "min" },
        { reducer: { durationIn: [152, 171] }, value: "12", unit: "min" },
        { reducer: { durationIn: [171, 220] }, value: "0", unit: "min" },
      ],
      count: 22102,
    }),
  ],
}

/** Daily steps with `prefer-raw`: raw where a day is covered, Garmin's
 * daily summary where it is not (that row's form is `summary`). */
export const STEPS: AggregateResult = {
  metric: STEPS_METRIC,
  unit: "{steps}",
  bucket: { calendar: "day" },
  from: "prefer-raw",
  exact: false,
  head: HEAD,
  rows: [
    row({
      start: "2026-10-04T00:00:00+03:00",
      end: "2026-10-05T00:00:00+03:00",
      form: "amount",
      from: "raw",
      sources: [GARMIN],
      coverage: "PT23H40M",
      values: [{ reducer: "sum", value: "9812", unit: "{steps}" }],
      count: 96,
    }),
    row({
      start: "2026-10-05T00:00:00+03:00",
      end: "2026-10-06T00:00:00+03:00",
      form: "summary",
      from: "summaries",
      sources: [GARMIN],
      coverage: "P1D",
      values: [{ reducer: "sum", value: "7004", unit: "{steps}" }],
      count: 1,
    }),
    row({
      start: "2026-10-06T00:00:00+03:00",
      end: "2026-10-07T00:00:00+03:00",
      form: "amount",
      from: "raw",
      sources: [GARMIN, APPLE],
      coverage: "PT24H",
      values: [{ reducer: "sum", value: "12450", unit: "{steps}" }],
      count: 96,
    }),
  ],
}

/** The weight trend, weekly: the mean with its lowest and highest; the
 * middle week holds the previous weighing with no point of its own
 * (`count` 0), and a `fill` row would be `synthetic`. */
export const WEIGHT: AggregateResult = {
  metric: BODY_MASS,
  unit: "kg",
  bucket: { calendar: "week", weekStart: "monday", timezone: "Europe/Athens" },
  from: "raw",
  exact: false,
  head: HEAD,
  rows: [
    row({
      start: "2026-09-21T00:00:00+03:00",
      end: "2026-09-28T00:00:00+03:00",
      form: "gauge",
      from: "raw",
      sources: [WITHINGS],
      values: [
        { reducer: "avg", value: "72.6", unit: "kg" },
        { reducer: "min", value: "72.1", unit: "kg" },
        { reducer: "max", value: "73", unit: "kg" },
      ],
      count: 6,
    }),
    row({
      start: "2026-09-28T00:00:00+03:00",
      end: "2026-10-05T00:00:00+03:00",
      synthetic: true,
      values: [
        { reducer: "avg", value: null, unit: "kg" },
        { reducer: "min", value: null, unit: "kg" },
        { reducer: "max", value: null, unit: "kg" },
      ],
    }),
    row({
      start: "2026-10-05T00:00:00+03:00",
      end: "2026-10-12T00:00:00+03:00",
      form: "gauge",
      from: "raw",
      sources: [WITHINGS],
      values: [
        { reducer: "avg", value: "72.3", unit: "kg" },
        { reducer: "min", value: "72.1", unit: "kg" },
        { reducer: "max", value: "72.4", unit: "kg" },
      ],
      count: 3,
    }),
  ],
}

/** Daily heart rate in the recorded-local calendar across the persona's
 * London trip (decision 0161): Athens at +03:00 until the flight on Sep 20,
 * London at +01:00 until the night of Sep 24, home again from Sep 25. Each
 * row starts at its own first point's offset and ends at its last one's,
 * so the flight day runs 26 hours and Sep 24 and Sep 25 overlap in time.
 * Sep 18 and Sep 26 lie outside a range of the dates between, which the
 * chart leaves out by their dates. */
export const LONDON_TRIP: AggregateResult = {
  metric: HEART,
  unit: "/min",
  bucket: { calendar: "day", fallbackTimezone: "Europe/Athens" },
  from: "raw",
  exact: false,
  head: HEAD,
  sources: [GARMIN],
  rows: (
    [
      ["2026-09-18T13:00:00+03:00", "2026-09-19T00:00:00+03:00", "61", 132],
      ["2026-09-19T00:00:00+03:00", "2026-09-20T00:00:00+03:00", "64", 288],
      ["2026-09-20T00:00:00+03:00", "2026-09-21T00:00:00+01:00", "71", 312],
      ["2026-09-21T00:00:00+01:00", "2026-09-22T00:00:00+01:00", "66", 288],
      ["2026-09-22T00:00:00+01:00", "2026-09-23T00:00:00+01:00", "65", 288],
      ["2026-09-23T00:00:00+01:00", "2026-09-24T00:00:00+01:00", "67", 288],
      ["2026-09-24T00:00:00+01:00", "2026-09-25T00:00:00+01:00", "69", 282],
      ["2026-09-25T00:00:00+03:00", "2026-09-26T00:00:00+03:00", "63", 288],
      ["2026-09-26T00:00:00+03:00", "2026-09-26T15:00:00+03:00", "58", 180],
    ] as const
  ).map(([start, end, value, count]) =>
    row({
      start,
      end,
      form: "gauge",
      from: "raw",
      sources: [GARMIN],
      values: [
        { reducer: "avg", value, unit: "/min" },
        { reducer: "min", value: "48", unit: "/min" },
        { reducer: "max", value: "131", unit: "/min" },
      ],
      count,
    })
  ),
}

/** Sleep stages per night, a coded amount grouped by code, each night the
 * sleep record's own span. */
export const SLEEP: AggregateResult = {
  metric: SLEEP_STAGE,
  unit: "min",
  from: "raw",
  exact: false,
  head: HEAD,
  sources: [WHOOP],
  rows: [
    ["rem", "94"],
    ["deep", "71"],
    ["light", "211"],
    ["awake", "38"],
  ].map(([code, value]) =>
    row({
      start: "2026-10-06T23:10:00+03:00",
      end: "2026-10-07T06:20:00+03:00",
      form: "amount",
      group: { code },
      record: {
        ref: `${AUTHORITY}/health/sleep/whoop/ada/e3b0c442/sleep`,
        version: 2,
      },
      nonAdditive: true,
      from: "raw",
      sources: [WHOOP],
      values: [{ reducer: "sum", value, unit: "min" }],
      count: 1,
    })
  ),
}

/** One hour of heart rate as a trace, irregular offsets, a run starting at
 * 06:30 (proposal 02b). */
export const HEART_TRACE: SubstrateRecord = record(
  TRACE,
  "applehealth/iphone/garmin-forerunner-265/heart_rate/2026-10-07T06",
  {
    metric: { ref: `${METRIC}/health/heart_rate` },
    source: { ref: `${SOURCE}/garmin-forerunner-265` },
    form: "gauge",
    unit: "/min",
    hold: "PT4M",
    at: "2026-10-07T06:00:00Z",
    endsAt: "2026-10-07T07:00:00Z",
    offsets: [0, 118000, 241000, 362000, 1800000, 1801000, 1802000],
    values: ["58", "57", "59", "61", "112", "118", "124"],
    revision: 2,
    stats: {
      count: 1812,
      min: "55",
      max: "171",
      mean: 131.6,
      m2: 2203418.2,
      first: { at: "2026-10-07T06:00:00Z", value: "58" },
      last: { at: "2026-10-07T06:59:58Z", value: "96" },
      coverage: "PT59M58S",
    },
  }
)

/** One weighing, a single observation (proposal 02c). */
export const WEIGHING: SubstrateRecord = record(
  OBSERVATION,
  "withings/ada/5f3a91/weight",
  {
    metric: { ref: `${METRIC}/health/body_mass` },
    source: { ref: `${SOURCE}/withings-body-plus` },
    form: "gauge",
    value: { value: "72.40", unit: "kg" },
    at: "2026-10-07T06:41:12+03:00",
    revision: 1,
    origin: {
      ref: "providers.substrate.reamde.dev/withings/measuregrp/5f3a91",
    },
  }
)

/** One aggregate read as the server saw it. */
export interface SeenRead {
  filter: RecordFilter
  aggregate: Aggregate
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status })
}

/** A latest read answered from `latest` the way the server answers it:
 * narrowed by the filter's `metric`, `source` and `about` lists, and one
 * row per value (the newest) where it groups by one of the three alone; a
 * row without that value is the group with none. */
export function latestAnswer(
  read: SeenRead,
  latest: AggregateResult = LATEST
): AggregateResult {
  const props = (read.filter.properties ?? {}) as Record<
    string,
    { in?: string[] }
  >
  const keep = (name: "metric" | "source" | "about", value?: string) =>
    !props[name]?.in || (value !== undefined && props[name].in.includes(value))
  const rows = latest.rows.filter(
    (r) =>
      keep("metric", r.group?.metric) &&
      keep("source", r.group?.source) &&
      keep("about", r.group?.about)
  )
  const groupBy = read.aggregate.groupBy ?? []
  const [by] = groupBy
  if (
    groupBy.length !== 1 ||
    (by !== "metric" && by !== "source" && by !== "about")
  ) {
    return { ...latest, rows }
  }
  const newest = new Map<string, AggregateRow>()
  for (const r of rows) {
    const value = r.group?.[by]
    const held = newest.get(value ?? "")
    if (!held || Date.parse(r.start) > Date.parse(held.start)) {
      newest.set(value ?? "", { ...r, group: value ? { [by]: value } : {} })
    }
  }
  return { ...latest, rows: [...newest.values()] }
}

/** Each metric's own read in a read the server saw: the read itself, or
 * for a several-metric read (`aggregate.metrics`) per entry the read that
 * metric alone makes, as docs/api.md defines an entry: its unit, its
 * reducers and `from` (else the read's), its statistic as
 * `filter.properties.statistic`, and the read's every other option but its
 * budget. The console sends none; another client may. */
export function perMetric(read: SeenRead): SeenRead[] {
  const { metrics, ...rest } = read.aggregate
  if (!metrics) return [read]
  return metrics.map((m) => {
    const aggregate: Aggregate = { ...rest, metric: { ref: m.ref } }
    delete aggregate.budget
    if (m.unit) aggregate.unit = m.unit
    if (m.reducers) aggregate.reducers = m.reducers
    if (m.from) aggregate.from = m.from
    const properties = { ...read.filter.properties }
    if (m.statistic) properties.statistic = { eq: m.statistic }
    return {
      filter: Object.keys(properties).length
        ? { ...read.filter, properties }
        : read.filter,
      aggregate,
    }
  })
}

/** A several-metric read's answer as the server writes it, from each
 * metric's own answer: their rows, each naming its metric in
 * `group.metric`, ordered by bucket and then metric, `units` naming each
 * metric's unit, and no `metric` or `unit`. The first refusal of any
 * metric refuses the read. */
function together(
  read: SeenRead,
  answer: (read: SeenRead) => AggregateResult | Response
): AggregateResult | Response {
  const parts = perMetric(read).map((one) => ({
    ref: one.aggregate.metric!.ref,
    out: answer(one),
  }))
  const refused = parts.find((p) => p.out instanceof Response)
  if (refused) return refused.out
  const answers = parts as { ref: string; out: AggregateResult }[]
  const [first] = answers
  const result: AggregateResult = {
    ...first.out,
    units: Object.fromEntries(answers.map((p) => [p.ref, p.out.unit ?? ""])),
    exact: answers.every((p) => p.out.exact),
    rows: answers
      .flatMap(({ ref, out }) =>
        out.rows.map((row) => ({
          ...row,
          group: { ...row.group, metric: ref },
        }))
      )
      .sort(
        (a, b) =>
          Date.parse(a.start) - Date.parse(b.start) ||
          (a.group.metric < b.group.metric ? -1 : 1)
      ),
  }
  delete result.metric
  delete result.unit
  return result
}

/** A stand-in for the substrate's records route: the registry (`kinds`),
 * the metric and source records (with `filter.ids` honoured, as the batched
 * title read asks), and every aggregate read answered by `answer`. A
 * several-metric read is answered from each metric's own read
 * (`perMetric`). `sent` keeps every aggregate read as it was sent, and
 * `seen` each metric's own read of them, in order. */
export function observationsServer(
  answer: (read: SeenRead) => AggregateResult | Response,
  kinds: KindInfo[] = KINDS,
  stored: SubstrateRecord[] = [...METRICS, ...SOURCES]
): {
  fetch: (input: RequestInfo | URL) => Promise<Response>
  seen: SeenRead[]
  sent: SeenRead[]
} {
  const seen: SeenRead[] = []
  const sent: SeenRead[] = []
  const respond = (input: RequestInfo | URL): Response => {
    const url = new URL(urlOf(input), "http://test")
    const filter = JSON.parse(
      url.searchParams.get("filter") ?? "{}"
    ) as RecordFilter
    const aggregate = url.searchParams.get("aggregate")
    if (aggregate) {
      const read = { filter, aggregate: JSON.parse(aggregate) as Aggregate }
      sent.push(read)
      seen.push(...perMetric(read))
      const out = read.aggregate.metrics ? together(read, answer) : answer(read)
      return out instanceof Response ? out : json(out)
    }
    if (filter.kinds?.includes(KIND_KIND)) {
      return json({ records: kinds, head: 0, generation: "g" })
    }
    const records = stored.filter(
      (r) =>
        (!filter.kinds || filter.kinds.includes(r.kind)) &&
        (!filter.ids || filter.ids.includes(r.id))
    )
    return json({ records, head: 0, generation: "g" })
  }
  const fetch = (input: RequestInfo | URL) => Promise.resolve(respond(input))
  return { fetch, seen, sent }
}

/** The server's 503 for a read it could not run just then. */
export function unavailable(): Response {
  return new Response(
    JSON.stringify({
      error: { code: "unavailable", message: "the database is restarting" },
    }),
    { status: 503, headers: { "Content-Type": "application/json" } }
  )
}

/** A stand-in server's `fetch` for a page under test: where `kindsFail`,
 * the registry's own read answers 503 (`unavailable`) and every other read
 * goes to the server. */
export function pageFetch(
  server: { fetch: (input: RequestInfo | URL) => Promise<Response> },
  kindsFail = false
): (input: RequestInfo | URL) => Promise<Response> {
  return (input) =>
    kindsFail && urlOf(input).includes("core%2Fkind")
      ? Promise.resolve(unavailable())
      : server.fetch(input)
}

/** Picks one value of a facet from the filter bar's "Add filter", then
 * closes the menu. */
export async function addFilter(facet: string, value: RegExp) {
  fireEvent.click(screen.getByRole("button", { name: /Add filter/ }))
  fireEvent.click(await screen.findByRole("option", { name: facet }))
  fireEvent.click(await screen.findByRole("option", { name: value }))
  fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" })
  await waitFor(() =>
    expect(screen.queryByRole("option", { name: value })).toBe(null)
  )
}

/** One answer alone as buckets and series, the way a chart of one read
 * lays it out (`bucketsOf`, then `seriesOn`). */
export function seriesOf(
  rows: readonly AggregateRow[],
  pickedKey: string,
  groupKey?: AggregateGroupKey
): { buckets: Bucket[]; series: Series[] } {
  const buckets = bucketsOf([rows])
  return { buckets, series: seriesOn(buckets, rows, pickedKey, groupKey) }
}

/** The URL a `fetch` was called with, whichever form it took. */
function urlOf(input: RequestInfo | URL): string {
  if (typeof input === "string") return input
  return input instanceof URL ? input.href : input.url
}
