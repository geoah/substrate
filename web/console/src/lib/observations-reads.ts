/** The aggregate reads the observations pages issue, without React: one
 * metric's read in a chart (`laneRead`), a chart's metrics as few requests
 * as hold them (`sharedReads`), discovery's (the latest read, the list of
 * metrics, the facet menus) and how discovery batches its metrics. The
 * read is docs/api.md's ("The aggregate read"); `lib/api/observations.ts`
 * sends it. */

import type { AggregateRead } from "@/lib/api/observations"
import {
  ApiError,
  type Aggregate,
  type AggregateBucket,
  type AggregateFrom,
  type AggregateGroupKey,
  type AggregateReducer,
  type RecordFilter,
} from "@/lib/api/types"
import {
  bandedReducer,
  OBSERVED_TRAIT_IDENTITY,
  reducerOf,
  type ReadingShape,
} from "@/lib/observations"
import {
  isoWithOffset,
  MAX_BUCKETS,
  MAX_BUCKETS_CAP,
  type TimeRange,
} from "@/lib/observations-time"

// ── the timeline's requests ────────────────────────────────────────────────

/** What a chart's series are told apart by, beside each metric's own. A
 * coded metric is always grouped by code (`laneGroupKey`), and no other
 * metric has codes, so code is no choice. */
export type GroupChoice = "metric" | "source" | "about"

export const GROUP_CHOICES: readonly { value: GroupChoice; label: string }[] = [
  { value: "metric", label: "Metric" },
  { value: "source", label: "Source" },
  { value: "about", label: "About" },
]

export const FROM_CHOICES: readonly { value: AggregateFrom; label: string }[] =
  [
    { value: "raw", label: "Raw" },
    { value: "summaries", label: "Summaries" },
    { value: "prefer-raw", label: "Prefer raw" },
  ]

/** What a read narrows its streams to: record paths of metrics, sources
 * and things observed, and channels. An empty list narrows nothing. A
 * `statistic` narrows the vendor summaries to it and keeps every other
 * form's values. */
export interface StreamNarrowing {
  metrics?: readonly string[]
  sources?: readonly string[]
  abouts?: readonly string[]
  channels?: readonly string[]
  statistic?: string
}

/** The observed filter, narrowed server-side by `properties`. */
function narrowedFilter(narrow: StreamNarrowing): RecordFilter {
  const properties: NonNullable<RecordFilter["properties"]> = {}
  if (narrow.metrics?.length) properties.metric = { in: [...narrow.metrics] }
  if (narrow.sources?.length) properties.source = { in: [...narrow.sources] }
  if (narrow.abouts?.length) properties.about = { in: [...narrow.abouts] }
  if (narrow.channels?.length) {
    properties.channel = { in: [...narrow.channels] }
  }
  if (narrow.statistic) properties.statistic = { eq: narrow.statistic }
  return {
    implements: OBSERVED_TRAIT_IDENTITY,
    ...(Object.keys(properties).length ? { properties } : {}),
  }
}

/** What one chart of the timeline asks for. */
export interface LaneSpec {
  /** The metric record's path. */
  metricPath: string
  shape: ReadingShape
  /** The reducer the reader picked, a key from `reducerOptions`
   * (`durationIn:95,114` for a structured one). */
  reducer: string
  /** The output unit; absent keeps the stored one. */
  unit?: string
  from: AggregateFrom
  range: TimeRange
  bucket: AggregateBucket
  group: GroupChoice
  /** Source record paths the read is narrowed to; empty for every source. */
  sources: string[]
  /** The sources the read takes, in the reader's order where `ranked`:
   * record paths or ids, which `sources.order` takes alike. */
  sourceOrder: string[]
  /** The reader ranked the sources (the timeline's Source filter), so the
   * read sends them as `sources.order`. Otherwise it sends none, and the
   * server ranks them by each source record's `priorities` for the
   * metric. */
  ranked?: boolean
  /** The read selects a stream naming no source too, which the priority
   * policy counts as one more source and ranks after every named one. The
   * read never sends an order beside it, because naming a source narrows
   * the read to the named ones. */
  sourceless?: boolean
  /** `about` values the read is narrowed to; empty for everything. */
  about: string[]
  /** Channels the read is narrowed to; absent or empty for every one. */
  channels?: string[]
  /** The one statistic of the vendor summaries the read takes, where they
   * keep several: a row reads one, and the server refuses two. */
  statistic?: string
  /** The buckets the read may make, counting every group, where that is
   * past the server's default (`MAX_BUCKETS`). */
  maxBuckets?: number
  /** The read takes streams on more than one channel (no channel counting
   * as one), so it groups by channel too and each is a series of its own:
   * a channel no `in` can name (none) is told apart this way. Grouped by
   * source, the read groups by channel anyway. */
  byChannel?: boolean
}

/** The reducers a lane asks for: the reader's, then (on a level read in its
 * own unit) the lowest and highest, which the chart draws as a band.
 * `timeInState` goes bare, which answers the time each code holds. */
export function laneReducers(spec: LaneSpec): AggregateReducer[] {
  const picked = reducerOf(spec.reducer)
  if (spec.shape !== "gauge" || !bandedReducer(spec.reducer)) return [picked]
  return [picked, ...["min", "max"].filter((r) => r !== spec.reducer)]
}

/** What a lane's chart groups its series by: a coded metric always by code
 * (a coded amount must be, and each code is a series of the stack);
 * otherwise the reader's choice, where it is not the metric itself (each
 * metric is its own series). Grouped by source, the read also groups by
 * channel (`laneRead`), so a source's two channels are two series. */
export function laneGroupKey(spec: LaneSpec): AggregateGroupKey | undefined {
  if (spec.shape === "coded") return "code"
  return spec.group === "metric" ? undefined : spec.group
}

/** What a lane's read groups by: by source, each source's channels apart;
 * otherwise its chart's key, and the channel where the read takes several. */
export function laneGroupBy(spec: LaneSpec): AggregateGroupKey[] {
  const groupKey = laneGroupKey(spec)
  if (groupKey === "source") return ["source", "channel"]
  return [
    ...(groupKey ? [groupKey] : []),
    ...(spec.byChannel ? ["channel" as const] : []),
  ]
}

/** The aggregate read one lane issues. Grouped by anything but source, two
 * or more sources (a stream naming none counts as one) are resolved by
 * priority, in the reader's order where the reader ranked them and in the
 * server's default otherwise, so one line is one thing: the default policy
 * answers a row per source, and a series draws one row per bucket. Grouped
 * by source, each keeps its own rows. */
export function laneRead(spec: LaneSpec): AggregateRead {
  const groupKey = laneGroupKey(spec)
  const groupBy = laneGroupBy(spec)
  const ranked = spec.sourceOrder.length + (spec.sourceless ? 1 : 0)
  const aggregate: Aggregate = {
    metric: { ref: spec.metricPath },
    from: spec.from,
    range: {
      gte: isoWithOffset(spec.range.start),
      lt: isoWithOffset(spec.range.end),
    },
    bucket: spec.bucket,
    ...(groupBy.length ? { groupBy } : {}),
    reducers: laneReducers(spec),
    ...(groupKey !== "source" && ranked > 1
      ? {
          sources: {
            policy: "priority",
            ...(spec.ranked ? { order: spec.sourceOrder } : {}),
          },
        }
      : {}),
    ...(spec.unit ? { unit: spec.unit } : {}),
    ...(spec.maxBuckets ? { budget: { maxBuckets: spec.maxBuckets } } : {}),
  }
  return {
    filter: narrowedFilter({
      sources: spec.sources,
      abouts: spec.about,
      channels: spec.channels,
      statistic: spec.statistic,
    }),
    aggregate,
  }
}

/** The most groups one read may ask for (`budget.maxGroups`'s cap). */
export const MAX_GROUPS_CAP = 2000

/** The server's other budgets (`substrate.AggregateMaxGroups`,
 * `AggregateMaxSamples` and its cap) and the most metrics one read names
 * (`AggregateMaxMetrics`). */
export const MAX_GROUPS = 200
export const MAX_SAMPLES = 1_000_000
export const MAX_SAMPLES_CAP = 20_000_000
export const MAX_METRICS = 16

// ── several metrics in one read ────────────────────────────────────────────

/** One lane's read, and the buckets it is expected to make counting every
 * group (`bucketEstimate`). */
export interface LaneReadPlan {
  read: AggregateRead
  buckets: number
}

/** One request of a chart, and the lanes it answers, by their place in the
 * list `sharedReads` was given. */
export interface SharedRead {
  read: AggregateRead
  lanes: number[]
}

/** The lanes' reads as few requests as hold them: lanes whose reads differ
 * only in their metric, unit, reducers, `from` and summary statistic share
 * one several-metric read (`aggregate.metrics`), in their order, up to
 * `MAX_METRICS` and while their buckets add up to at most the cap; a lane
 * alone keeps its own read. The read's filter, range, bucket, grouping and
 * sources are every lane's. Its budget is the lanes' added up (to each
 * cap), because the server holds the metrics to it together and each lane
 * keeps what its own read could spend. */
export function sharedReads(lanes: readonly LaneReadPlan[]): SharedRead[] {
  const open: { key: string; lanes: number[]; buckets: number }[] = []
  lanes.forEach((lane, at) => {
    const key = sharedKey(lane.read)
    const into =
      key === undefined
        ? undefined
        : open.find(
            (b) =>
              b.key === key &&
              b.lanes.length < MAX_METRICS &&
              b.buckets + lane.buckets <= MAX_BUCKETS_CAP
          )
    if (into) {
      into.lanes.push(at)
      into.buckets += lane.buckets
    } else {
      open.push({ key: key ?? "", lanes: [at], buckets: lane.buckets })
    }
  })
  return open.map((b) => ({
    read:
      b.lanes.length === 1
        ? lanes[b.lanes[0]].read
        : together(b.lanes.map((at) => lanes[at].read)),
    lanes: b.lanes,
  }))
}

/** The one statistic a lane's filter names (`{eq}`), which a several-metric
 * read names in the metric's entry instead. */
function statisticOf(filter: RecordFilter | undefined): string | undefined {
  const cond = filter?.properties?.statistic
  if (!cond || Object.keys(cond).length !== 1) return undefined
  return typeof cond.eq === "string" ? cond.eq : undefined
}

/** The filter without the statistic a metric's entry names instead. */
function sharedFilter(filter: RecordFilter | undefined): RecordFilter {
  const out: RecordFilter = { ...filter }
  if (statisticOf(filter) === undefined || !out.properties) return out
  const properties = { ...out.properties }
  delete properties.statistic
  if (Object.keys(properties).length) out.properties = properties
  else delete out.properties
  return out
}

/** What a lane's read shares with another's in one several-metric read:
 * all of it but its metric's own choices and its budget, as JSON with the
 * keys sorted. Undefined for a read that names no one metric. */
function sharedKey(read: AggregateRead): string | undefined {
  if (!read.aggregate.metric) return undefined
  const aggregate: Aggregate = { ...read.aggregate }
  for (const own of ["metric", "unit", "reducers", "from", "budget"] as const) {
    delete aggregate[own]
  }
  return sortedJSON({ filter: sharedFilter(read.filter), aggregate })
}

function sortedJSON(value: unknown): string {
  return JSON.stringify(value, (_, v: unknown) =>
    v && typeof v === "object" && !Array.isArray(v)
      ? Object.fromEntries(
          Object.entries(v as Record<string, unknown>).sort(([a], [b]) =>
            a < b ? -1 : a > b ? 1 : 0
          )
        )
      : v
  )
}

/** Two or more lanes' reads, which `sharedKey` found alike, as one read. */
function together(reads: readonly AggregateRead[]): AggregateRead {
  const [first] = reads
  const aggregate: Aggregate = {
    metrics: reads.map(({ filter, aggregate: a }) => {
      const statistic = statisticOf(filter)
      return {
        ref: a.metric!.ref,
        ...(a.unit ? { unit: a.unit } : {}),
        ...(a.reducers ? { reducers: a.reducers } : {}),
        ...(a.from ? { from: a.from } : {}),
        ...(statistic ? { statistic } : {}),
      }
    }),
    ...first.aggregate,
  }
  for (const own of ["metric", "unit", "reducers", "from"] as const) {
    delete aggregate[own]
  }
  const total = (
    key: "maxBuckets" | "maxSamples" | "maxGroups",
    otherwise: number,
    cap: number
  ) =>
    Math.min(
      cap,
      reads.reduce((n, r) => n + (r.aggregate.budget?.[key] ?? otherwise), 0)
    )
  aggregate.budget = {
    maxBuckets: total("maxBuckets", MAX_BUCKETS, MAX_BUCKETS_CAP),
    maxSamples: total("maxSamples", MAX_SAMPLES, MAX_SAMPLES_CAP),
    maxGroups: total("maxGroups", MAX_GROUPS, MAX_GROUPS_CAP),
  }
  return { filter: sharedFilter(first.filter), aggregate }
}

/** Whether a read was refused for making more groups than one read may. */
export function overGroupCap(error: Error | null | undefined): boolean {
  return (
    error instanceof ApiError &&
    error.status === 422 &&
    error.problems.some((p) => p.startsWith("aggregate.budget.maxGroups"))
  )
}

/** The keys discovery's latest read groups by: a stream's, and the
 * statistic, so a stream whose summaries keep several says which
 * (`streamsOf` folds them into one stream). */
export const LATEST_GROUP_BY: AggregateGroupKey[] = [
  "metric",
  "source",
  "about",
  "channel",
  "statistic",
]

/** The latest read: one row per stream and statistic, from the newest
 * point of each, narrowed to the given metrics, sources and things, at the
 * group cap. A read makes at most that many groups, so where one read of
 * every stream is refused for them, discovery asks a few metrics at a time
 * (`discoveryChunks`). */
export function latestRead(narrow: StreamNarrowing = {}): AggregateRead {
  return {
    filter: narrowedFilter(narrow),
    aggregate: {
      groupBy: [...LATEST_GROUP_BY],
      reducers: ["last"],
      budget: { maxGroups: MAX_GROUPS_CAP },
    },
  }
}

/** Discovery's first read: one row per metric with a point, narrowed to
 * the chosen sources and things, at the group cap, since every metric with
 * a point is one group and a catalogue is a few hundred. Its metrics are
 * what a picker offers and what `discoveryChunks` partitions. */
export function metricsRead(narrow: StreamNarrowing = {}): AggregateRead {
  return {
    filter: narrowedFilter({ sources: narrow.sources, abouts: narrow.abouts }),
    aggregate: {
      groupBy: ["metric"],
      reducers: ["last"],
      budget: { maxGroups: MAX_GROUPS_CAP },
    },
  }
}

/** A facet a page filters its streams by on the server, beside the metric. */
export type StreamFacet = "source" | "about"

/** A facet menu's read: one row per source, or per thing observed, with a
 * point under the page's other choices, at the group cap. The facet's own
 * choice is left out, so the menu still offers the values a reader can add
 * once one is chosen; a row with no value is the owner's. */
export function facetRead(
  facet: StreamFacet,
  narrow: StreamNarrowing = {}
): AggregateRead {
  return {
    filter: narrowedFilter({
      metrics: narrow.metrics,
      ...(facet === "source"
        ? { abouts: narrow.abouts }
        : { sources: narrow.sources }),
    }),
    aggregate: {
      groupBy: [facet],
      reducers: ["last"],
      budget: { maxGroups: MAX_GROUPS_CAP },
    },
  }
}

/** How many metrics one stream read names, and how many stream reads one
 * discovery makes at most: eight metrics of a few hundred streams each sit
 * inside a read's 2000 groups, and past 256 metrics each read names more. */
const METRICS_PER_READ = 8
export const MAX_STREAM_READS = 32

/** The metrics as the batches discovery reads their streams in: at most
 * `MAX_STREAM_READS` batches of one size, at least `METRICS_PER_READ`
 * metrics, the last holding what is left. */
export function discoveryChunks(metrics: readonly string[]): string[][] {
  const size = Math.max(
    METRICS_PER_READ,
    Math.ceil(metrics.length / MAX_STREAM_READS)
  )
  const out: string[][] = []
  for (let i = 0; i < metrics.length; i += size) {
    out.push(metrics.slice(i, i + size))
  }
  return out
}
