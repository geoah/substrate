/** Fixtures for the observations suites, shaped like the wire contract the
 * observations brief pins and built from the proposal's worked examples
 * (docs/plans/observations/proposal/02*.md): the registry an imported
 * `observations` sample leaves, its metric records, a latest read of six
 * streams, and four answers (weekly heart rate zones, daily steps with
 * `from` per row, a weight trend, sleep stages grouped by code). Only test
 * suites import this file. */

import type {
  Aggregate,
  AggregateResult,
  KindInfo,
  RecordFilter,
  SubstrateRecord,
} from "@/lib/api/types"

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
  about: { type: "reference" },
  form: { type: "string" },
  at: { type: "datetime" },
  endsAt: { type: "datetime" },
  revision: { type: "int" },
}

export const observationKind = kind(
  "observation",
  ["substrate.reamde.dev/core/observed(single)"],
  {
    ...shared,
    value: { type: "quantity" },
    code: { type: "string" },
    statistic: { type: "string" },
    period: { type: "string" },
    origin: { type: "reference" },
  }
)

export const traceKind = kind(
  "trace",
  ["substrate.reamde.dev/core/observed(packed)"],
  {
    ...shared,
    unit: { type: "string" },
    every: { type: "duration" },
    offsets: { type: "int", repeated: true },
    values: { type: "decimal", repeated: true },
    codes: { type: "string", repeated: true },
    stats: { type: "json", managed: true },
  }
)

export const metricKind = kind("metric", [], {
  name: { type: "string" },
  form: { type: "string" },
  unit: { type: "string" },
  codeSet: { type: "string", repeated: true },
})

export const sourceKind = kind("source", [], { name: { type: "string" } })

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
  }),
  record(METRIC, "health/whoop_recovery", {
    title: "Recovery",
    name: "Recovery",
    form: "summary",
    unit: "%",
  }),
  record(METRIC, "health/energy_expended", {
    title: "Energy expended",
    name: "Energy expended",
    form: "amount",
    unit: "kJ",
  }),
]

export const SOURCES: SubstrateRecord[] = [
  record(SOURCE, "garmin-forerunner-265", { title: "Garmin Forerunner 265" }),
  record(SOURCE, "apple-watch-s10", { title: "Apple Watch S10" }),
  record(SOURCE, "withings-body-plus", { title: "Withings Body+" }),
  record(SOURCE, "whoop-4-0", { title: "WHOOP 4.0" }),
]

const HEAD = { changelog: 2171234, generation: 7 }

/** The latest read: one row per stream, newest first by `start`. */
export const LATEST: AggregateResult = {
  exact: true,
  head: HEAD,
  rows: [
    {
      start: "2026-10-07T06:59:58Z",
      end: "2026-10-07T06:59:58Z",
      group: {
        metric: "health/heart_rate",
        source: "garmin-forerunner-265",
      },
      values: [{ reducer: "last", value: "96", unit: "/min" }],
      count: 1812,
    },
    {
      start: "2026-10-07T05:00:00Z",
      end: "2026-10-07T05:00:00Z",
      group: { metric: "health/heart_rate", source: "apple-watch-s10" },
      values: [{ reducer: "last", value: "61", unit: "/min" }],
    },
    {
      start: "2026-10-07T06:45:00Z",
      end: "2026-10-07T07:00:00Z",
      group: { metric: "health/steps", source: "garmin-forerunner-265" },
      values: [{ reducer: "last", value: "412", unit: "{steps}" }],
      coverage: "PT15M",
    },
    {
      start: "2026-10-07T03:41:12Z",
      end: "2026-10-07T03:41:12Z",
      group: { metric: "health/body_mass", source: "withings-body-plus" },
      values: [{ reducer: "last", value: "72.40", unit: "kg" }],
    },
    {
      start: "2026-10-06T19:41:00Z",
      end: "2026-10-07T19:38:00Z",
      group: { metric: "health/whoop_recovery", source: "whoop-4-0" },
      values: [{ reducer: "last", value: "67", unit: "%" }],
    },
    {
      start: "2026-10-06T23:10:00Z",
      end: "2026-10-07T06:20:00Z",
      group: {
        metric: "health/sleep_stage",
        source: "whoop-4-0",
        about: "ada.example.com/people/person/mia",
      },
      values: [{ reducer: "last", value: null }],
    },
  ],
}

/** Heart rate zones, weekly, five `durationIn` reducers in minutes. */
export const ZONES: AggregateResult = {
  metric: `${METRIC}/health/heart_rate`,
  unit: "/min",
  bucket: { calendar: "week", weekStart: "monday", timezone: "Europe/Athens" },
  from: "raw",
  exact: true,
  head: HEAD,
  sources: ["garmin-forerunner-265", "apple-watch-s10"],
  rows: [
    {
      start: "2026-04-06T00:00:00+03:00",
      end: "2026-04-13T00:00:00+03:00",
      from: "raw",
      sources: ["garmin-forerunner-265"],
      coverage: "P6DT22H10M",
      values: [
        { reducer: { durationIn: [95, 114] }, value: "612.4", unit: "min" },
        { reducer: { durationIn: [114, 133] }, value: "171.0", unit: "min" },
        { reducer: { durationIn: [133, 152] }, value: "88.5", unit: "min" },
        { reducer: { durationIn: [152, 171] }, value: "21.0", unit: "min" },
        { reducer: { durationIn: [171, 220] }, value: "2.5", unit: "min" },
      ],
      count: 24810,
      bounded: 0,
      synthetic: false,
    },
    {
      start: "2026-04-13T00:00:00+03:00",
      end: "2026-04-20T00:00:00+03:00",
      from: "raw",
      sources: ["garmin-forerunner-265", "apple-watch-s10"],
      coverage: "P6DT20H",
      values: [
        { reducer: { durationIn: [95, 114] }, value: "540.0", unit: "min" },
        { reducer: { durationIn: [114, 133] }, value: "201.5", unit: "min" },
        { reducer: { durationIn: [133, 152] }, value: "60.0", unit: "min" },
        { reducer: { durationIn: [152, 171] }, value: "12.0", unit: "min" },
        { reducer: { durationIn: [171, 220] }, value: "0", unit: "min" },
      ],
      count: 22102,
    },
  ],
}

/** Daily steps with `prefer-raw`: raw where a day is covered, Garmin's
 * daily summary where it is not. */
export const STEPS: AggregateResult = {
  metric: `${METRIC}/health/steps`,
  unit: "{steps}",
  bucket: { calendar: "day" },
  from: "prefer-raw",
  exact: true,
  head: HEAD,
  rows: [
    {
      start: "2026-10-04T00:00:00+03:00",
      end: "2026-10-05T00:00:00+03:00",
      from: "raw",
      sources: ["garmin-forerunner-265"],
      coverage: "PT23H40M",
      values: [{ reducer: "sum", value: "9812", unit: "{steps}" }],
      count: 96,
    },
    {
      start: "2026-10-05T00:00:00+03:00",
      end: "2026-10-06T00:00:00+03:00",
      from: "summaries",
      sources: ["garmin-forerunner-265"],
      coverage: "PT9H",
      values: [{ reducer: "sum", value: "7004", unit: "{steps}" }],
      count: 1,
    },
    {
      start: "2026-10-06T00:00:00+03:00",
      end: "2026-10-07T00:00:00+03:00",
      from: "raw",
      sources: ["garmin-forerunner-265", "apple-watch-s10"],
      coverage: "PT24H",
      values: [{ reducer: "sum", value: "12450", unit: "{steps}" }],
      count: 96,
    },
  ],
}

/** The weight trend, weekly: the mean with its lowest and highest. */
export const WEIGHT: AggregateResult = {
  metric: `${METRIC}/health/body_mass`,
  unit: "kg",
  bucket: { calendar: "week", weekStart: "monday", timezone: "Europe/Athens" },
  from: "raw",
  exact: true,
  head: HEAD,
  rows: [
    {
      start: "2026-09-21T00:00:00+03:00",
      end: "2026-09-28T00:00:00+03:00",
      sources: ["withings-body-plus"],
      values: [
        { reducer: "avg", value: "72.6", unit: "kg" },
        { reducer: "min", value: "72.1", unit: "kg" },
        { reducer: "max", value: "73.0", unit: "kg" },
      ],
      count: 6,
    },
    {
      start: "2026-09-28T00:00:00+03:00",
      end: "2026-10-05T00:00:00+03:00",
      synthetic: true,
      values: [
        { reducer: "avg", value: null, unit: "kg" },
        { reducer: "min", value: null, unit: "kg" },
        { reducer: "max", value: null, unit: "kg" },
      ],
      count: 0,
    },
    {
      start: "2026-10-05T00:00:00+03:00",
      end: "2026-10-12T00:00:00+03:00",
      sources: ["withings-body-plus"],
      values: [
        { reducer: "avg", value: "72.3", unit: "kg" },
        { reducer: "min", value: "72.1", unit: "kg" },
        { reducer: "max", value: "72.4", unit: "kg" },
      ],
      count: 3,
    },
  ],
}

/** Sleep stages per night, grouped by code, each night the sleep record's
 * own span. */
export const SLEEP: AggregateResult = {
  metric: `${METRIC}/health/sleep_stage`,
  unit: "min",
  from: "raw",
  exact: true,
  head: HEAD,
  rows: [
    ["rem", "94"],
    ["deep", "71"],
    ["light", "211"],
    ["awake", "38"],
  ].map(([code, value]) => ({
    start: "2026-10-06T23:10:00+03:00",
    end: "2026-10-07T06:20:00+03:00",
    group: { code },
    record: {
      ref: `${AUTHORITY}/health/sleep/whoop/ada/e3b0c442/sleep`,
      version: 2,
    },
    sources: ["whoop-4-0"],
    values: [{ reducer: "sum", value, unit: "min" }],
    count: 1,
  })),
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

/** A stand-in for the substrate's records route: the registry (`kinds`),
 * the metric and source records (with `filter.ids` honoured, as the batched
 * title read asks), and every aggregate read answered by `answer` and kept
 * in `seen`, in order. */
export function observationsServer(
  answer: (read: SeenRead) => AggregateResult | Response,
  kinds: KindInfo[] = KINDS
): {
  fetch: (input: RequestInfo | URL) => Promise<Response>
  seen: SeenRead[]
} {
  const seen: SeenRead[] = []
  const fetch = async (input: RequestInfo | URL) => {
    const url = new URL(String(input), "http://test")
    const filter = JSON.parse(
      url.searchParams.get("filter") ?? "{}"
    ) as RecordFilter
    const aggregate = url.searchParams.get("aggregate")
    if (aggregate) {
      const read = { filter, aggregate: JSON.parse(aggregate) as Aggregate }
      seen.push(read)
      const out = answer(read)
      return out instanceof Response ? out : json(out)
    }
    if (filter.kinds?.includes("substrate.reamde.dev/core/kind")) {
      return json({ records: kinds, head: 0, generation: "g" })
    }
    const records = [...METRICS, ...SOURCES].filter(
      (r) =>
        (!filter.kinds || filter.kinds.includes(r.kind)) &&
        (!filter.ids || filter.ids.includes(r.id))
    )
    return json({ records, head: 0, generation: "g" })
  }
  return { fetch, seen }
}
