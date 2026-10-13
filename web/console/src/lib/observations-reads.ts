/** The aggregate reads the observations pages issue, without React: one
 * chart's read (`laneRead`), discovery's (the latest read, the list of
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
import { isoWithOffset, type TimeRange } from "@/lib/observations-time"

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
   * policy counts as one more source. No record ranks it, so with no
   * `sources.order` the server places it among the sources no record
   * ranks, by kind and then source path: before a named source of its
   * kind. The read never sends an order beside it, because naming a source
   * narrows the read to the named ones. */
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
export const STREAM_GROUPS: AggregateGroupKey[] = [
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
      groupBy: [...STREAM_GROUPS],
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
