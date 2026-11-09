// The aggregate read's request and answer (docs/api.md, "The aggregate
// read"), typed after internal/substrate/aggregate.go. Every field the API
// takes is here and optional where the API's is; the read itself decides
// which combinations it admits, and refuses the rest with a 422 naming
// them.

/** A reducer: a bare name, or a one-key object naming one with its
 * argument. A value's `reducer` reads back as the request spelled it. */
export type AggregateReducer =
  | "count"
  | "sum"
  | "avg"
  | "min"
  | "max"
  | "first"
  | "last"
  | "stddev"
  | "increase"
  | "rate"
  | "circularMean"
  /** The time each code holds, one value per code the bucket holds. */
  | "timeInState"
  /** The value below which `percentile` of the values lie, 0 to 1. */
  | { percentile: number }
  /** The time a value in `[lo, hi)` holds, in the values' unit. */
  | { durationIn: readonly [number, number] }
  /** The time each named code holds; empty is every code. */
  | { timeInState: readonly string[] }

/** The reducers a rolling window takes. */
export type AggregateRollingReducer = "avg" | "sum" | "min" | "max" | "stddev"

/** Which points a read takes: raw gauges, amounts and counters, summary
 * points, or raw points where they cover 0.9 of a bucket and summaries
 * otherwise. */
export type AggregateFrom = "raw" | "summaries" | "prefer-raw"

/** The form of the points that fed a row. */
export type AggregateForm = "gauge" | "amount" | "counter" | "summary"

/** The stream keys a `groupBy` names; `statistic` on a read from summaries
 * or the latest read. */
export type AggregateGroupKey =
  "source" | "about" | "code" | "metric" | "channel" | "statistic"

/** The records filter beside an aggregate read. A metric read takes
 * `kinds`, `implements` and `eq` or `in` on the stream properties `source`,
 * `about`, `metric`, `channel`, `code` and `statistic`; a property read the
 * whole filter grammar. */
export interface AggregateFilter {
  kinds?: readonly string[]
  implements?: string
  properties?: Record<string, unknown>
  [arm: string]: unknown
}

/** One metric of a several-metric read: its record path, the unit its
 * values answer in and its durations' unit, its reducers and `from` (the
 * read's by default), and the one summary statistic its summaries are read
 * at. */
export interface AggregateMetricRead {
  ref: string
  unit?: string
  durationUnit?: string
  reducers?: readonly AggregateReducer[]
  from?: AggregateFrom
  statistic?: string
}

/** The records' source policy: every record (`separate`), or the
 * highest-ranked source's where records of several sources overlap
 * (`priority`, which names `property` and `order`). */
export interface AggregateRecordSources {
  policy?: "separate" | "priority"
  /** The values of `property`, first highest: a record path, or a bare id
   * the read completes. */
  order?: readonly string[]
  /** The records' declared property holding their source. */
  property?: string
  /** Declared properties that must also be equal for one record to drop
   * another. */
  match?: readonly string[]
}

/** A declared number, quantity, money or duration of the filtered kinds,
 * reduced instead of a metric, placed in time by `anchor` (`at` by
 * default). */
export interface AggregateProperty {
  name: string
  anchor?: string
  /** The declared int property holding each record's UTC offset, which
   * the recorded-local calendar places it by. */
  utcOffset?: string
  /** Declared string, enum or reference properties: a row per
   * combination of their values. */
  groupBy?: readonly string[]
  sources?: AggregateRecordSources
}

/** The half-open range `[gte, lt)`, two RFC 3339 instants; with
 * `recordedLocal`, whole recorded-local dates. */
export interface AggregateRange {
  gte: string
  lt: string
  recordedLocal?: boolean
}

/** The records time mode: each record the filter matches is one bucket,
 * its `[at, endsAt)`. */
export interface AggregateRecordsBucket {
  filter: AggregateFilter
  sources?: AggregateRecordSources
  /** The records' declared int properties holding their UTC offset where
   * they start and end; `end` is `start`'s where absent. */
  utcOffsets?: { start: string; end?: string }
}

/** One of the four time modes: `every` (from `origin`) on UTC instants;
 * `calendar` in a `timezone`; `calendar` alone, the recorded-local
 * calendar; `records`. In an answer, the bucket as the read applied it. */
export interface AggregateBucket {
  /** An ISO 8601 duration. */
  every?: string
  origin?: string
  calendar?: "hour" | "day" | "week" | "month"
  /** An IANA zone; absent beside `calendar`, the recorded-local calendar. */
  timezone?: string
  /** The zone the recorded-local calendar places a value written without
   * a UTC offset by. */
  fallbackTimezone?: string
  /** A day name, `monday` by default. */
  weekStart?: string
  /** Where a value with a span lands: the bucket its start or its last
   * instant lies in. Absent, the metric's `bucketPlace`, else `start`. */
  place?: "start" | "end"
  records?: AggregateRecordsBucket
}

/** The points' source policy: one row per source (`separate`), or an
 * ordered list resolved per stored interval (`priority`); with no
 * `order`, each source record's `priorities` for the metric rank them. */
export interface AggregateSources {
  policy?: "separate" | "priority"
  order?: readonly string[]
}

/** Each rolling reducer over a window of `buckets` buckets ending at the
 * row's bucket (`current`) or the one before it (`previous`). */
export interface AggregateRolling {
  buckets: number
  reducers?: readonly AggregateRollingReducer[]
  through?: "current" | "previous"
}

/** Bounds checked before the read runs; absent is the default. */
export interface AggregateBudget {
  maxBuckets?: number
  maxSamples?: number
  maxGroups?: number
}

/** The `aggregate` parameter: what to reduce (`metric`, `metrics` or
 * `property`, or the newest point of each group with `latest`), over which
 * range, in which buckets, with which reducers. */
export interface AggregateRequest {
  metric?: { ref: string }
  /** Up to 16 metrics over one range, bucket, groupBy and sources. */
  metrics?: readonly AggregateMetricRead[]
  property?: AggregateProperty
  /** Each group's newest point instead of buckets. */
  latest?: boolean
  from?: AggregateFrom
  range?: AggregateRange
  bucket?: AggregateBucket
  groupBy?: readonly AggregateGroupKey[]
  reducers?: readonly AggregateReducer[]
  sources?: AggregateSources
  /** The unit the values answer in, which converts to the metric's. */
  unit?: string
  /** The unit `durationIn` and `timeInState` answer their times in. */
  durationUnit?: string
  /** An ISO 8601 duration capping how long a gauge's value holds. */
  maxGap?: string
  fill?: "zero"
  rolling?: AggregateRolling
  budget?: AggregateBudget
}

/** The `downsample` parameter: at most `points` columns over the range,
 * each answering `first`, `last`, `min` and `max`. */
export interface AggregateDownsample {
  points: number
}

/** A row's group: one key per `groupBy` word, a record path for `source`,
 * `about` and `metric`; a money property's `currency`; on a property read,
 * each `property.groupBy` property's value. */
export interface AggregateGroup {
  source?: string
  about?: string
  code?: string
  metric?: string
  channel?: string
  statistic?: string
  currency?: string
  properties?: Record<string, string>
}

/** One reducer's answer for a row. */
export interface AggregateValue {
  reducer: AggregateReducer
  /** A decimal string, never rounded for display; null where the reducer
   * has no answer (an average of no points, a censored value, a coded
   * `first` or `last`, whose answer is `code`). */
  value: string | null
  /** The true number lies beyond `value`. */
  comparator?: "<" | "<=" | ">=" | ">"
  /** Absent for `count`. */
  unit?: string
  code?: string
  /** On a downsample, the instant the column's value was taken at. */
  at?: string
  /** A rolling value: the window's buckets, the reducer it rolls over, and
   * the buckets of the window that held a value. */
  rolling?: number
  of?: AggregateReducer
  n?: number
}

/** One bucket of one group. */
export interface AggregateRow {
  /** The bucket, each end with its own UTC offset. */
  start: string
  end: string
  /** Absent on a property read. */
  form?: AggregateForm
  group?: AggregateGroup
  /** The records mode's matched record, at the version the read saw. */
  record?: { ref: string; version: number }
  from?: "raw" | "summaries"
  /** The sources that fed the row, by record path. */
  sources?: string[]
  /** An ISO 8601 duration. */
  coverage?: string
  /** One per reducer in request order, then the rolling values. */
  values: AggregateValue[]
  count: number
  /** On the latest read, the instant of the group's earliest point. */
  first?: string
  offsetless?: number
  prorated?: number
  /** The values among `count` written with a comparator. */
  bounded?: number
  /** A row `fill` made. */
  synthetic?: boolean
  /** A records-mode row, whose window may overlap another's. */
  nonAdditive?: boolean
}

/** The answer of an aggregate read. */
export interface AggregateAnswer {
  /** What was read; neither on a several-metric read. */
  metric?: string
  property?: string
  /** The values' unit; absent on a several-metric read, whose `units`
   * names each metric's by its record path. */
  unit?: string
  units?: Record<string, string>
  bucket?: AggregateBucket
  from?: AggregateFrom
  /** False when any value came from a float64 reduction or a rounded
   * conversion. */
  exact: boolean
  /** Ordered by bucket, then group, then code. */
  rows: AggregateRow[]
  /** Every source that fed a row, by record path. */
  sources?: string[]
  /** Where the read was served in the change feed; zero under an app's
   * token, whose reads carry no position (as its lists carry no head). */
  head: { changelog: number; generation: string }
}

/** What an aggregate read sends beside its body. */
export interface AggregateOptions {
  filter?: AggregateFilter
  downsample?: AggregateDownsample
  signal?: AbortSignal
}
