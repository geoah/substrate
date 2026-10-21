/** Record views (decision records 0166 and 0175) without React: a
 * `substrate.reamde.dev/console/recordview` record read into the blocks a
 * record page draws above its generic sheet, the window a view binds on a
 * record by its properties' names, and the reads its blocks make over that
 * window. Nothing here names a sample or a metric: every kind, metric,
 * reducer, band and code comes from the view record. A metric is the full
 * record path the view's reference holds, never an id resolved against some
 * metric kind: two metric kinds may hold one id. */

import type { AggregateRead } from "@/lib/api/observations"
import { CORE_AUTHORITY, CORE_PACKAGE } from "@/lib/api/http"
import type { ListParams } from "@/lib/api/records"
import {
  readReference,
  type Aggregate,
  type AggregateMetricRead,
  type AggregateReducer,
  type AggregateResult,
  type AggregateRow,
  type AggregateSources,
  type Cond,
  type KindInfo,
  type RecordFilter,
  type SubstrateRecord,
} from "@/lib/api/types"
import {
  ALL_REDUCERS,
  MAX_BUCKETS,
  MAX_BUCKETS_CAP,
  MAX_METRICS,
} from "@/lib/aggregate-rules"
import { OBSERVED_TRAIT } from "@/lib/core-kinds"
import { splitRecordPath } from "@/lib/record-path"
import { groupDigits, isTimeUnit, roundDigits } from "@/lib/units"
import {
  addressValue,
  bandedReducer,
  DEFAULT_PRECISION,
  NONE,
  observedKinds,
  readingText,
  reducerKey,
  sameRef,
  type MetricInfo,
} from "@/lib/observations"
import { numberOf, rowValue, type Bucket } from "@/lib/observations-answer"
import {
  autoBucket,
  bucketEstimate,
  bucketOf,
  dayWords,
  durationMs,
  durationWords,
  isoWithOffset,
  type BucketSize,
  type TimeRange,
} from "@/lib/observations-time"

/** The kind a record view is a record of. */
export const RECORD_VIEW_KIND = `${CORE_AUTHORITY}/console/recordview`

/** The kind a kind's declaration is a record of: a view's `appliesTo`
 * points at one. */
const KIND_DECLARATION = `${CORE_PACKAGE}/kind`

/** The record path of a kind's declaration, which a view's `appliesTo`
 * names. */
export function kindDeclarationPath(identity: string): string {
  return `${KIND_DECLARATION}/${identity}`
}

/** The list read of the views of one kind. */
export function recordViewsParams(identity: string): ListParams {
  return {
    kinds: [RECORD_VIEW_KIND],
    first: 20,
    filter: {
      properties: { appliesTo: { eq: kindDeclarationPath(identity) } },
    },
  }
}

// ── tones and bands ───────────────────────────────────────────────────────

/** How a band is coloured and worded. A tone is never the only signal: a
 * word sits beside it. */
export type Tone = "good" | "fair" | "poor" | "none"

/** Which direction of a change is good. */
export type Better = "higher" | "lower" | "none"

/** Where a read takes its values. */
export type ReadFrom = "raw" | "summaries" | "prefer-raw"

/** A labelled value range, `low <= value < high`, an absent bound open; a
 * band naming a metric applies to that metric alone. */
export interface Band {
  low?: number
  high?: number
  label?: string
  tone: Tone
  metric?: string
}

const TONES: readonly Tone[] = ["good", "fair", "poor", "none"]
const BETTERS: readonly Better[] = ["higher", "lower", "none"]
const READ_FROM: readonly ReadFrom[] = ["raw", "summaries", "prefer-raw"]

/** The status token a tone's mark wears. */
export function toneColor(tone: Tone): string {
  switch (tone) {
    case "good":
      return "var(--ok)"
    case "fair":
      return "var(--warning)"
    case "poor":
      return "var(--destructive)"
    case "none":
      return "var(--faint)"
  }
}

/** The colour a tone's words wear: the mark's, but a toneless word the
 * faint text token, since the faint mark token is too light for text. */
export function toneText(tone: Tone): string {
  return tone === "none" ? "var(--faint-text)" : toneColor(tone)
}

/** The soft status token a tone's wash wears. */
export function toneSoft(tone: Tone): string {
  switch (tone) {
    case "good":
      return "var(--ok-soft)"
    case "fair":
      return "var(--warn-soft)"
    case "poor":
      return "var(--bad-soft)"
    case "none":
      return "var(--hover)"
  }
}

// ── the view as the console reads it ──────────────────────────────────────

/** Which of the record's own properties hold its window, who it is about
 * and what recorded it; or, for a record with no window of its own, the
 * last `last` of time. */
export interface ViewWindow {
  start?: string
  end?: string
  /** A record whose end is unset is read up to now. */
  open: boolean
  /** An ISO 8601 duration: the window ends now and is this long. */
  last?: string
  about?: string
  /** The record itself is who the observations are about. */
  aboutSelf: boolean
  source?: string
  /** The record itself is the source that recorded them. */
  sourceSelf: boolean
}

/** How a read takes the record's own source: first then the sources'
 * priorities, alone, or the priorities alone. */
export type SourcePolicy = "prefer" | "only" | "priority"

export type ChartBucket = "auto" | "whole" | "minute" | "hour" | "day"

/** One reducer of a chart, as the aggregate read takes it, its key in an
 * answer (`reducerKey`) and what it is called. */
export interface ChartReducer {
  reducer: AggregateReducer
  key: string
  label?: string
}

export interface HeadingBlock {
  type: "heading"
  text: string
  note?: string
}

export interface TextBlock {
  type: "text"
  text: string
}

export interface PropertiesBlock {
  type: "properties"
  label?: string
  list: { property: string; label?: string }[]
}

export interface RelatedBlock {
  type: "related"
  label?: string
  /** An outgoing reference property of the record. */
  property?: string
  /** A kind whose records reference this one, through `via`. */
  kind?: string
  via?: string
  show: "list" | "count"
  limit: number
}

export interface ChartBlock {
  type: "chart"
  label?: string
  /** The metric records' paths. */
  metrics: string[]
  reducers: ChartReducer[]
  bucket: ChartBucket
  unit?: string
  sources: SourcePolicy
  from: ReadFrom
  statistic?: string
  /** The channel kept; `none` keeps the streams naming no channel. */
  channel?: string
  bands: Band[]
  /** Whether a chart none of whose metrics was measured over the window
   * says so or is left out. */
  empty: "say" | "hide"
}

export interface SpansBlock {
  type: "spans"
  label?: string
  /** The metric record's path. */
  metric: string
  codes: { code: string; label?: string }[]
  totals: boolean
  sources: SourcePolicy
}

/** One tile: one reducer of one metric over the window, or one property of
 * the record. `metric` is the metric record's path; an item naming
 * `metricProperty` reads the path the record's reference holds
 * (`itemMetric`). */
export interface StatItem {
  metric?: string
  metricProperty?: string
  /** A property of the record; a dotted path reads into a keyed map. */
  property?: string
  reducer: AggregateReducer
  /** The reducer's key in an answer (`reducerKey`). */
  key: string
  from: ReadFrom
  statistic?: string
  code?: string
  unit?: string
  label?: string
  show: "number" | "dial" | "bar"
  min?: number
  max?: number
  better: Better
  precision?: number
}

export interface StatsBlock {
  type: "stats"
  label?: string
  /** Days before the window each metric item is compared against; 0 none. */
  baseline: number
  channel?: string
  sources: SourcePolicy
  bands: Band[]
  items: StatItem[]
  /** Whether a block none of whose items holds a value says so or is
   * left out. */
  empty: "say" | "hide"
}

export interface TrendItem {
  metric?: string
  metricProperty?: string
  reducer: AggregateReducer
  key: string
  from: ReadFrom
  statistic?: string
  code?: string
  unit?: string
  label?: string
  show: "auto" | "line" | "bars"
  better: Better
}

export interface TrendBlock {
  type: "trend"
  label?: string
  /** Buckets the strip covers, the record's last. */
  span: number
  bucket: "day" | "week" | "month"
  /** Buckets of the trailing average, through the bucket before; 0 none. */
  rolling: number
  channel?: string
  sources: SourcePolicy
  bands: Band[]
  items: TrendItem[]
  /** Whether a block none of whose strips holds a value says so or is
   * left out. */
  empty: "say" | "hide"
}

export interface TableBlock {
  type: "table"
  label?: string
  /** The listed kind's identity. */
  kind: string
  via?: string
  /** Properties this record and the listed records hold alike: a
   * view's `sharing`, or its older one-name spelling `sameAs`. Empty where
   * the table lists by `via` or `inWindow`. */
  sharing: string[]
  inWindow: boolean
  columns: string[]
  orderBy: string
  limit: number
  chart: boolean
  /** `rows` draws a table; `tiles` draws each result as a tile. */
  show: "rows" | "tiles"
  nest?: string
  empty: "say" | "hide"
}

export interface LaneRow {
  /** A kind's identity, whose records are the row's spans. */
  kind?: string
  via?: string
  /** A coded metric's record path. */
  metric?: string
  label?: string
  spanTitle?: string
  limit: number
}

export interface LanesBlock {
  type: "lanes"
  label?: string
  sources: SourcePolicy
  rows: LaneRow[]
}

export interface BreakdownBlock {
  type: "breakdown"
  label?: string
  property?: string
  keys: { key: string; label?: string }[]
  items: { property: string; label?: string }[]
  total?: string
}

export type Block =
  | HeadingBlock
  | TextBlock
  | PropertiesBlock
  | RelatedBlock
  | ChartBlock
  | SpansBlock
  | StatsBlock
  | TrendBlock
  | TableBlock
  | LanesBlock
  | BreakdownBlock

/** The block types the grammar declares, in the order a notice names them. */
export const BLOCK_TYPES = [
  "heading",
  "text",
  "properties",
  "related",
  "chart",
  "spans",
  "stats",
  "trend",
  "table",
  "lanes",
  "breakdown",
] as const

/** One section: the block it holds, or why it cannot be drawn. */
export type Section = { block: Block } | { problem: string }

export interface RecordView {
  id: string
  name?: string
  intro?: string
  window?: ViewWindow
  /** Why the window cannot be bound as written, which the blocks that read
   * it say instead of reading. */
  windowProblem?: string
  fold: string[]
  sections: Section[]
}

/** The reducers a chart may name: the aggregate read's (`ALL_REDUCERS`,
 * the server's own table), less `timeInState`, which a spans block reads
 * instead. */
const CHART_REDUCERS = new Set(
  [...ALL_REDUCERS].filter((r) => r !== "timeInState")
)

const CHART_BUCKETS: readonly ChartBucket[] = [
  "auto",
  "whole",
  "minute",
  "hour",
  "day",
]
const SOURCE_POLICIES: readonly SourcePolicy[] = ["prefer", "only", "priority"]

type Bag = Record<string, unknown>

function isBag(v: unknown): v is Bag {
  return typeof v === "object" && v !== null && !Array.isArray(v)
}

function text(v: unknown): string | undefined {
  return typeof v === "string" && v ? v : undefined
}

function texts(v: unknown): string[] {
  return Array.isArray(v)
    ? v.flatMap((x) => (text(x) ? [x as string] : []))
    : []
}

function bags(v: unknown): Bag[] {
  return Array.isArray(v) ? v.filter(isBag) : []
}

function num(v: unknown): number | undefined {
  return typeof v === "number" && Number.isFinite(v) ? v : undefined
}

function given(v: unknown): boolean {
  return v !== undefined && v !== null
}

/** An integer setting clamped to its bounds, the fallback where unset. */
function clampInt(v: unknown, lo: number, hi: number, fallback: number) {
  const n = num(v)
  return n === undefined ? fallback : Math.min(hi, Math.max(lo, Math.round(n)))
}

function oneOf<T extends string>(
  v: unknown,
  allowed: readonly T[],
  fallback: T
): T | undefined {
  if (v === undefined || v === null) return fallback
  return allowed.includes(v as T) ? (v as T) : undefined
}

/** A record view record as the console draws it. A section holding no
 * block, two blocks, or a block this console does not know or cannot read
 * is kept as a problem, so the page says so where the section would be. */
export function recordViewOf(record: SubstrateRecord): RecordView {
  const p = record.properties ?? {}
  const w = isBag(p.window) ? p.window : undefined
  const window: ViewWindow | undefined = w
    ? {
        ...(text(w.start) ? { start: text(w.start) } : {}),
        ...(text(w.end) ? { end: text(w.end) } : {}),
        open: w.open === true,
        ...(text(w.last) ? { last: text(w.last) } : {}),
        ...(text(w.about) ? { about: text(w.about) } : {}),
        aboutSelf: w.aboutSelf === true,
        ...(text(w.source) ? { source: text(w.source) } : {}),
        sourceSelf: w.sourceSelf === true,
      }
    : undefined
  const windowProblem = window ? windowProblemOf(window) : undefined
  return {
    id: record.id,
    name: text(p.name),
    intro: text(p.intro),
    window,
    ...(windowProblem ? { windowProblem } : {}),
    fold: texts(p.fold),
    sections: bags(p.sections).map(sectionOf),
  }
}

/** The window rules the grammar cannot state. */
function windowProblemOf(w: ViewWindow): string | undefined {
  if (w.aboutSelf && w.about) {
    return "This view's window names both about and aboutSelf; it names one."
  }
  if (w.sourceSelf && w.source) {
    return "This view's window names both source and sourceSelf; it names one."
  }
  if (w.last && w.start) {
    return "This view's window names both last and start; it names one."
  }
  if (w.last && durationMs(w.last) === undefined) {
    return `This view's window's last is ${w.last}, which is no duration of weeks, days, hours, minutes or seconds.`
  }
  return undefined
}

function sectionOf(raw: Bag): Section {
  const set = Object.keys(raw).filter(
    (k) => raw[k] !== undefined && raw[k] !== null
  )
  const known = set.filter((k) =>
    (BLOCK_TYPES as readonly string[]).includes(k)
  )
  if (!set.length) return { problem: "This section holds no block." }
  if (!known.length) {
    return {
      problem: `This section holds a block this console does not draw: ${set.join(", ")}.`,
    }
  }
  if (known.length > 1) {
    return {
      problem: `This section holds ${known.length} blocks (${known.join(", ")}); a section holds one.`,
    }
  }
  const type = known[0] as Block["type"]
  const body = raw[type]
  if (!isBag(body)) return { problem: `This ${type} block is not an object.` }
  return blockOf(type, body)
}

function blockOf(type: Block["type"], b: Bag): Section {
  switch (type) {
    case "heading": {
      const t = text(b.text)
      if (!t) return { problem: "This heading has no text." }
      return { block: { type, text: t, note: text(b.note) } }
    }
    case "text": {
      const t = text(b.text)
      if (!t) return { problem: "This text block has no text." }
      return { block: { type, text: t } }
    }
    case "properties": {
      const list = bags(b.list).flatMap((item) => {
        const property = text(item.property)
        return property ? [{ property, label: text(item.label) }] : []
      })
      if (!list.length) {
        return { problem: "This properties block names no property." }
      }
      return { block: { type, label: text(b.label), list } }
    }
    case "related": {
      const property = text(b.property)
      const kind = kindOfDeclaration(b.kind)
      const via = text(b.via)
      const show = oneOf(b.show, ["list", "count"] as const, "list")
      if (!show)
        return { problem: `This related block's show is ${said(b.show)}.` }
      if (property ? kind || via : !(kind && via)) {
        return {
          problem:
            "This related block names neither one outgoing reference property nor a kind and the property on it that points here.",
        }
      }
      const limit =
        typeof b.limit === "number" && b.limit >= 1
          ? Math.min(100, b.limit)
          : 10
      return {
        block: {
          type,
          label: text(b.label),
          ...(property ? { property } : { kind, via }),
          show,
          limit,
        },
      }
    }
    case "chart":
      return chartOf(b)
    case "spans": {
      if (b.metric === undefined || b.metric === null) {
        return { problem: "This spans block names no metric." }
      }
      const metric = metricPathIn(b.metric)
      if (!metric)
        return { problem: `This spans block ${notAMetric(b.metric)}` }
      const sources = oneOf(b.sources, SOURCE_POLICIES, "only")
      if (!sources) {
        return { problem: `This spans block's sources is ${said(b.sources)}.` }
      }
      const codes = bags(b.codes).flatMap((c) => {
        const code = text(c.code)
        return code ? [{ code, label: text(c.label) }] : []
      })
      return {
        block: {
          type,
          label: text(b.label),
          metric,
          codes,
          totals: b.totals !== false,
          sources,
        },
      }
    }
    case "stats":
      return statsOf(b)
    case "trend":
      return trendOf(b)
    case "table":
      return tableOf(b)
    case "lanes":
      return lanesOf(b)
    case "breakdown":
      return breakdownOf(b)
  }
}

function chartOf(b: Bag): Section {
  const named = Array.isArray(b.metrics) ? b.metrics : []
  if (!named.length) return { problem: "This chart names no metric." }
  const metrics: string[] = []
  for (const m of named) {
    const path = metricPathIn(m)
    if (!path) return { problem: `This chart ${notAMetric(m)}` }
    metrics.push(path)
  }
  const bucket = oneOf(b.bucket, CHART_BUCKETS, "auto")
  if (!bucket) return { problem: `This chart's bucket is ${said(b.bucket)}.` }
  const sources = oneOf(b.sources, SOURCE_POLICIES, "prefer")
  if (!sources)
    return { problem: `This chart's sources is ${said(b.sources)}.` }
  const from = oneOf(b.from, READ_FROM, "raw")
  if (!from) return { problem: fromProblem("This chart", b.from) }
  const rawStatistic = statisticProblem("This chart", from, b.statistic)
  if (rawStatistic) return { problem: rawStatistic }
  const bands = bandsOf(b.bands, "This chart")
  if (typeof bands === "string") return { problem: bands }
  const empty = oneOf(b.empty, ["say", "hide"] as const, "say")
  if (!empty) return { problem: `This chart's empty is ${said(b.empty)}.` }
  const reducers: ChartReducer[] = []
  for (const r of bags(b.reducers)) {
    const read = chartReducerOf(r)
    if (typeof read === "string") return { problem: read }
    reducers.push(read)
  }
  if (!reducers.length) return { problem: "This chart names no reducer." }
  return {
    block: {
      type: "chart",
      label: text(b.label),
      metrics,
      reducers,
      bucket,
      unit: text(b.unit),
      sources,
      from,
      ...(text(b.statistic) ? { statistic: text(b.statistic) } : {}),
      ...(text(b.channel) ? { channel: text(b.channel) } : {}),
      bands,
      empty,
    },
  }
}

/** Why a read names a statistic it cannot keep: a statistic narrows a
 * metric's summaries, and a raw read holds none. Undefined where it can. */
function statisticProblem(
  what: string,
  from: ReadFrom,
  statistic: unknown
): string | undefined {
  const named = text(statistic)
  if (!named || from !== "raw") return undefined
  return `${what} keeps the statistic ${named} and reads from raw; a statistic narrows a metric's summaries, so read from summaries or prefer-raw.`
}

function fromProblem(what: string, v: unknown): string {
  return `${what} reads from ${said(v)}; from is raw, summaries or prefer-raw.`
}

/** A block's bands, or why one cannot be read. */
function bandsOf(v: unknown, what: string): Band[] | string {
  const out: Band[] = []
  for (const raw of bags(v)) {
    const tone = oneOf(raw.tone, TONES, "none")
    if (!tone) {
      return `${what} names a band whose tone is ${said(raw.tone)}; a tone is good, fair, poor or none.`
    }
    let metric: string | undefined
    if (given(raw.metric)) {
      metric = metricPathIn(raw.metric)
      if (!metric) return `${what} has a band that ${notAMetric(raw.metric)}`
    }
    const low = num(raw.low)
    const high = num(raw.high)
    if (low !== undefined && high !== undefined && low >= high) {
      return `${what} has a band whose low bound is not under its high one: ${low} to ${high}.`
    }
    out.push({
      ...(low !== undefined ? { low } : {}),
      ...(high !== undefined ? { high } : {}),
      ...(text(raw.label) ? { label: text(raw.label) } : {}),
      tone,
      ...(metric ? { metric } : {}),
    })
  }
  return out
}

/** The fields a stats item and a trend item share, or why one cannot be
 * read. `what` names the block in a problem. */
function readItemOf(
  r: Bag,
  what: string,
  admitsProperty: boolean
):
  | {
      metric?: string
      metricProperty?: string
      property?: string
      reducer: AggregateReducer
      key: string
      from: ReadFrom
      statistic?: string
      code?: string
      unit?: string
      label?: string
      better: Better
    }
  | string {
  let metric: string | undefined
  if (given(r.metric)) {
    metric = metricPathIn(r.metric)
    if (!metric) return `${what} has an item that ${notAMetric(r.metric)}`
  }
  const metricProperty = text(r.metricProperty)
  const property = admitsProperty ? text(r.property) : undefined
  const named = [metric, metricProperty, property].filter(Boolean).length
  if (named !== 1) {
    return admitsProperty
      ? `${what} has an item naming ${named} of metric, metricProperty and property; an item names exactly one.`
      : `${what} has an item naming ${named} of metric and metricProperty; an item names exactly one.`
  }
  const from = oneOf(r.from, READ_FROM, "raw")
  if (!from) return fromProblem(`${what}'s item`, r.from)
  const rawStatistic = statisticProblem(`${what}'s item`, from, r.statistic)
  if (rawStatistic) return rawStatistic
  const better = oneOf(r.better, BETTERS, "none")
  if (!better) {
    return `${what} has an item whose better is ${said(r.better)}; better is higher, lower or none.`
  }
  const code = text(r.code)
  const name = text(r.reducer) ?? "last"
  if (!ALL_REDUCERS.has(name)) {
    return `${what} names a reducer this console does not read: ${name}.`
  }
  let reducer: AggregateReducer = name
  if (name === "durationIn") {
    const low = num(r.low)
    const high = num(r.high)
    if (low === undefined || high === undefined) {
      return "A durationIn reducer needs a low and a high bound."
    }
    if (low >= high) {
      return `A durationIn reducer's low bound must be under its high one: ${low} to ${high}.`
    }
    reducer = { durationIn: [low, high] }
  } else if (name === "percentile") {
    const rank = num(r.rank)
    if (rank === undefined) return "A percentile reducer needs a rank."
    reducer = { percentile: rank }
  } else if (name === "timeInState") {
    if (!code) return "A timeInState reducer needs a code."
    reducer = { timeInState: [code] }
  }
  return {
    ...(metric ? { metric } : {}),
    ...(metricProperty ? { metricProperty } : {}),
    ...(property ? { property } : {}),
    reducer,
    key: reducerKey(reducer),
    from,
    ...(text(r.statistic) ? { statistic: text(r.statistic) } : {}),
    ...(code ? { code } : {}),
    ...(text(r.unit) ? { unit: text(r.unit) } : {}),
    ...(text(r.label) ? { label: text(r.label) } : {}),
    better,
  }
}

function statsOf(b: Bag): Section {
  const what = "This stats block"
  const sources = oneOf(b.sources, SOURCE_POLICIES, "prefer")
  if (!sources) return { problem: `${what}'s sources is ${said(b.sources)}.` }
  const empty = oneOf(b.empty, ["say", "hide"] as const, "say")
  if (!empty) return { problem: `${what}'s empty is ${said(b.empty)}.` }
  const bands = bandsOf(b.bands, what)
  if (typeof bands === "string") return { problem: bands }
  const items: StatItem[] = []
  for (const r of bags(b.items)) {
    const read = readItemOf(r, what, true)
    if (typeof read === "string") return { problem: read }
    const show = oneOf(r.show, ["number", "dial", "bar"] as const, "number")
    if (!show) {
      return {
        problem: `${what} has an item whose show is ${said(r.show)}; show is number, dial or bar.`,
      }
    }
    const min = num(r.min)
    const max = num(r.max)
    const precision = num(r.precision)
    items.push({
      ...read,
      show,
      ...(min !== undefined ? { min } : {}),
      ...(max !== undefined ? { max } : {}),
      ...(precision !== undefined
        ? { precision: clampInt(precision, 0, 12, 0) }
        : {}),
    })
  }
  if (!items.length) return { problem: `${what} names no item.` }
  return {
    block: {
      type: "stats",
      label: text(b.label),
      baseline: clampInt(b.baseline, 0, 400, 0),
      ...(text(b.channel) ? { channel: text(b.channel) } : {}),
      sources,
      bands,
      items,
      empty,
    },
  }
}

function trendOf(b: Bag): Section {
  const what = "This trend block"
  const span = given(b.span) ? num(b.span) : 30
  if (span === undefined || span < 2) {
    return {
      problem: `${what}'s span is ${said(b.span)}; a span covers at least 2 buckets.`,
    }
  }
  const bucket = oneOf(b.bucket, ["day", "week", "month"] as const, "day")
  if (!bucket) {
    return {
      problem: `${what}'s bucket is ${said(b.bucket)}; a bucket is day, week or month.`,
    }
  }
  const sources = oneOf(b.sources, SOURCE_POLICIES, "prefer")
  if (!sources) return { problem: `${what}'s sources is ${said(b.sources)}.` }
  const empty = oneOf(b.empty, ["say", "hide"] as const, "say")
  if (!empty) return { problem: `${what}'s empty is ${said(b.empty)}.` }
  const bands = bandsOf(b.bands, what)
  if (typeof bands === "string") return { problem: bands }
  const items: TrendItem[] = []
  for (const r of bags(b.items)) {
    const read = readItemOf(r, what, false)
    if (typeof read === "string") return { problem: read }
    const show = oneOf(r.show, ["auto", "line", "bars"] as const, "auto")
    if (!show) {
      return {
        problem: `${what} has an item whose show is ${said(r.show)}; show is auto, line or bars.`,
      }
    }
    // One read holds a metric once, so two items of one metric must read
    // it alike and differ in their reducer alone.
    const twin = items.find((i) => i.metric && i.metric === read.metric)
    if (twin && entrySpec(twin) !== entrySpec(read)) {
      return {
        problem: `${what} reads ${read.metric} twice from different points; a trend reads each metric one way.`,
      }
    }
    items.push({ ...read, show })
  }
  if (!items.length) return { problem: `${what} names no item.` }
  return {
    block: {
      type: "trend",
      label: text(b.label),
      span: Math.min(400, Math.round(span)),
      bucket,
      rolling: clampInt(b.rolling, 0, 400, 0),
      ...(text(b.channel) ? { channel: text(b.channel) } : {}),
      sources,
      bands,
      items,
      empty,
    },
  }
}

function tableOf(b: Bag): Section {
  const what = "This table"
  const kind = kindOfDeclaration(b.kind)
  if (!kind) return { problem: `${what} names no kind.` }
  const via = text(b.via)
  const sameAs = text(b.sameAs)
  const listed = texts(b.sharing)
  if (sameAs && listed.length) {
    return {
      problem: `${what} names both sharing and its older spelling sameAs; it names one.`,
    }
  }
  // `sameAs` is the one-name spelling a view written before `sharing`
  // holds: it reads as a list of one.
  const sharing = sameAs ? [sameAs] : [...new Set(listed)]
  const inWindow = b.inWindow === true
  const named = [via, sharing.length > 0, inWindow].filter(Boolean).length
  if (named !== 1) {
    return {
      problem: `${what} names ${named} of via, sharing and inWindow; a table names exactly one.`,
    }
  }
  const empty = oneOf(b.empty, ["say", "hide"] as const, "say")
  if (!empty) return { problem: `${what}'s empty is ${said(b.empty)}.` }
  const show = oneOf(b.show, ["rows", "tiles"] as const, "rows")
  if (!show) return { problem: `${what}'s show is ${said(b.show)}.` }
  return {
    block: {
      type: "table",
      label: text(b.label),
      kind,
      ...(via ? { via } : {}),
      sharing,
      inWindow,
      columns: texts(b.columns),
      orderBy: text(b.orderBy) ?? "at:desc",
      limit: clampInt(b.limit, 1, 100, 25),
      chart: b.chart === true,
      show,
      ...(text(b.nest) ? { nest: text(b.nest) } : {}),
      empty,
    },
  }
}

function lanesOf(b: Bag): Section {
  const what = "This lanes block"
  const sources = oneOf(b.sources, SOURCE_POLICIES, "only")
  if (!sources) return { problem: `${what}'s sources is ${said(b.sources)}.` }
  const rows: LaneRow[] = []
  for (const r of bags(b.rows)) {
    const kind = given(r.kind) ? kindOfDeclaration(r.kind) : undefined
    let metric: string | undefined
    if (given(r.metric)) {
      metric = metricPathIn(r.metric)
      if (!metric)
        return { problem: `${what} has a row that ${notAMetric(r.metric)}` }
    }
    if (Boolean(kind) === Boolean(metric)) {
      return {
        problem: `${what} has a row naming ${kind ? "both" : "neither"} a kind and a metric; a row names one.`,
      }
    }
    const via = text(r.via)
    if (via && !kind) {
      return { problem: `${what} has a row naming via without a kind.` }
    }
    rows.push({
      ...(kind ? { kind } : {}),
      ...(via ? { via } : {}),
      ...(metric ? { metric } : {}),
      ...(text(r.label) ? { label: text(r.label) } : {}),
      ...(text(r.spanTitle) ? { spanTitle: text(r.spanTitle) } : {}),
      limit: clampInt(r.limit, 1, 500, 100),
    })
  }
  if (!rows.length) return { problem: `${what} names no row.` }
  return { block: { type: "lanes", label: text(b.label), sources, rows } }
}

function breakdownOf(b: Bag): Section {
  const what = "This breakdown"
  const property = text(b.property)
  const items = bags(b.items).flatMap((i) => {
    const p = text(i.property)
    return p
      ? [{ property: p, ...(text(i.label) ? { label: text(i.label) } : {}) }]
      : []
  })
  if (property ? items.length : !items.length) {
    return {
      problem: `${what} names ${property ? "both a property and items" : "neither a property nor items"}; it names one.`,
    }
  }
  const keys = bags(b.keys).flatMap((k) => {
    const key = text(k.key)
    return key
      ? [{ key, ...(text(k.label) ? { label: text(k.label) } : {}) }]
      : []
  })
  return {
    block: {
      type: "breakdown",
      label: text(b.label),
      ...(property ? { property } : {}),
      keys,
      items,
      ...(text(b.total) ? { total: text(b.total) } : {}),
    },
  }
}

/** The kind identity a reference to its declaration names: the stored
 * `{ref}` or the bare path. A bare word is stored as the id of a
 * declaration nobody made, and reads as that word. */
function kindOfDeclaration(v: unknown): string | undefined {
  const path = readReference(v)?.path ?? text(v)
  if (!path) return undefined
  const prefix = `${KIND_DECLARATION}/`
  return path.startsWith(prefix) ? path.slice(prefix.length) : path
}

/** The record path a view's metric reference holds: the stored `{ref}` or
 * the bare path. Undefined for anything that names no record, a bare id
 * among them, since an id alone does not say which metric kind holds it. */
function metricPathIn(v: unknown): string | undefined {
  const path = readReference(v)?.path ?? text(v)
  return path && splitRecordPath(path) ? path : undefined
}

function notAMetric(v: unknown): string {
  return `names a metric by ${said(readReference(v)?.path ?? v)}, which is no metric record's path: a view names each metric by a reference to its record.`
}

/** One declared reducer as the read takes it, or why it cannot be read. */
function chartReducerOf(r: Bag): ChartReducer | string {
  const name = text(r.reducer)
  const label = text(r.label)
  if (!name || !CHART_REDUCERS.has(name)) {
    return `This chart names a reducer this console does not read: ${name ?? "none"}.`
  }
  let reducer: AggregateReducer = name
  if (name === "durationIn") {
    if (typeof r.low !== "number" || typeof r.high !== "number") {
      return "A durationIn reducer needs a low and a high bound."
    }
    if (r.low >= r.high) {
      return `A durationIn reducer's low bound must be under its high one: ${r.low} to ${r.high}.`
    }
    reducer = { durationIn: [r.low, r.high] }
  } else if (name === "percentile") {
    if (typeof r.rank !== "number") return "A percentile reducer needs a rank."
    reducer = { percentile: r.rank }
  }
  return { reducer, key: reducerKey(reducer), ...(label ? { label } : {}) }
}

/** The properties the views fold away from the generic sheet. */
export function foldedBy(views: readonly RecordView[]): string[] {
  return [...new Set(views.flatMap((v) => v.fold))]
}

/** The views in the order a page draws them: by id. */
export function viewsOf(records: readonly SubstrateRecord[]): RecordView[] {
  return records.map(recordViewOf).sort((a, b) => a.id.localeCompare(b.id))
}

// ── the window a view binds on one record ─────────────────────────────────

/** What a view's blocks read on one record: its window (absent where the
 * record holds none the view binds), whether that window ends at the time
 * of reading, who it is about (absent where the view binds no `about` or
 * the record names nobody, which reads the owner's), what recorded it, and
 * the observed kinds the reads go through. */
export interface Bound {
  window?: TimeRange
  /** The window ends at the time of reading: "so far". */
  open: boolean
  about?: string
  source?: string
  observed: string[]
  /** The window is the view's `last`, ending now. */
  lastWindow?: boolean
}

/** Whether a Bound is a record's own window still running: an open record
 * read up to now, which holds part of what its whole window will. A view's
 * `last` ends now too, but it is a whole stretch of time. */
export function partialWindow(b: Bound): boolean {
  return b.open && !b.lastWindow
}

/** Whether a view binds a window at all: a start or a `last`. */
export function viewNeedsWindow(view: RecordView): boolean {
  return Boolean(view.window?.start || view.window?.last)
}

/** Whether a block reads the record's window. */
export function readsWindow(block: Block): boolean {
  switch (block.type) {
    case "chart":
    case "spans":
    case "lanes":
    case "trend":
      return true
    case "table":
      return block.inWindow
    case "stats":
      return block.items.some((i) => i.metric || i.metricProperty)
    default:
      return false
  }
}

function instant(v: unknown): number | undefined {
  if (typeof v !== "string") return undefined
  const t = Date.parse(v)
  return Number.isNaN(t) ? undefined : t
}

/** The view's window on a record. Undefined only where no kind of the
 * repository binds core `observed`; otherwise a Bound, whose `window` is
 * absent where the record holds none the view binds: no start, an end at
 * or before the start, or no end on a view that is not `open`. */
export function boundOf(
  view: RecordView,
  record: SubstrateRecord,
  kinds: readonly KindInfo[],
  now = Date.now()
): Bound | undefined {
  const observed = observedKinds(kinds)
  if (!observed.length) return undefined
  const w = view.window
  const p = record.properties ?? {}
  const self = `${record.kind}/${record.id}`
  const about = w?.aboutSelf
    ? self
    : w?.about
      ? readReference(p[w.about])?.path
      : undefined
  const source = w?.sourceSelf
    ? self
    : w?.source
      ? readReference(p[w.source])?.path
      : undefined
  let window: TimeRange | undefined
  let open = false
  let lastWindow = false
  const lastMs = w?.last ? durationMs(w.last) : undefined
  if (lastMs) {
    window = { start: now - lastMs, end: now }
    open = true
    lastWindow = true
  } else if (w?.start) {
    const start = instant(p[w.start])
    const ended = w.end ? instant(p[w.end]) : undefined
    const end = ended ?? (w.open ? now : undefined)
    if (start !== undefined && end !== undefined && end > start) {
      window = { start, end }
      open = w.open && ended === undefined
    }
  }
  return {
    ...(window ? { window } : {}),
    open,
    about,
    source,
    observed: observed.map((k) => k.identity),
    ...(lastWindow ? { lastWindow } : {}),
  }
}

/** The window of a Bound a block reads, which its caller checked. */
function windowOf(b: Bound): TimeRange {
  if (!b.window) throw new Error("This read needs a bound window.")
  return b.window
}

function rangeOf(window: TimeRange) {
  return { gte: isoWithOffset(window.start), lt: isoWithOffset(window.end) }
}

/** The narrowing every read takes: who the record is about (a named
 * record, or the owner's, which name nobody), and the record's own source
 * where the policy reads it alone. Never everyone's: a repository may hold
 * another person's observations beside the owner's. */
function narrowing(b: Bound, policy: SourcePolicy): Record<string, Cond> {
  return {
    about: b.about ? { eq: b.about } : { exists: false },
    ...(policy === "only" && b.source ? { source: { eq: b.source } } : {}),
  }
}

/** The channel a block keeps: `none` the streams naming none. */
function channelCond(channel: string | undefined): Record<string, Cond> {
  if (!channel) return {}
  return { channel: channel === "none" ? { exists: false } : { eq: channel } }
}

/** Where sources overlap, which one answers. `prefer` puts the record's own
 * source first and the rest by priority; `only` narrows to it in the
 * filter and needs no order, except where the record names none. */
function sourcesOf(
  b: Bound,
  policy: SourcePolicy
): AggregateSources | undefined {
  if (policy === "only" && b.source) return undefined
  return {
    policy: "priority",
    ...(policy === "prefer" && b.source ? { order: [b.source] } : {}),
  }
}

function filterOf(
  b: Bound,
  policy: SourcePolicy,
  channel?: string
): RecordFilter {
  return {
    implements: OBSERVED_TRAIT,
    properties: { ...narrowing(b, policy), ...channelCond(channel) },
  }
}

/** One metric of a several-metric read. A unit of duration beside a time
 * in a range or a state is the times' unit (`durationUnit`); any other
 * converts the values. */
export function metricEntry(
  ref: string,
  reducers: readonly AggregateReducer[],
  from: ReadFrom,
  statistic?: string,
  unit?: string
): AggregateMetricRead {
  const timed = reducers.some((r) => {
    const k = reducerKey(r)
    return k.startsWith("durationIn") || k.startsWith("timeInState")
  })
  return {
    ref,
    reducers: [...reducers],
    from,
    ...(statistic ? { statistic } : {}),
    ...(unit
      ? isTimeUnit(unit) && timed
        ? { durationUnit: unit }
        : { unit }
      : {}),
  }
}

/** The one builder every block's several-metric read goes through: the
 * observed filter with the block's narrowing and channel, the entries, the
 * Bound's window as the range unless `extra` names another, and the
 * sources order the policy asks for. */
export function metricsReadOf(
  b: Bound,
  policy: SourcePolicy,
  channel: string | undefined,
  entries: readonly AggregateMetricRead[],
  extra: Partial<Aggregate> = {}
): AggregateRead {
  const sources = sourcesOf(b, policy)
  return {
    filter: filterOf(b, policy, channel),
    aggregate: {
      metrics: [...entries],
      ...(b.window ? { range: rangeOf(b.window) } : {}),
      ...extra,
      ...(sources ? { sources } : {}),
    },
  }
}

const BUCKET_SIZES: Record<
  Exclude<ChartBucket, "auto" | "whole">,
  BucketSize
> = {
  minute: "1m",
  hour: "hour",
  day: "day",
}

/** The bucket size a line chart reads `window` at. */
export function chartBucketSize(
  bucket: ChartBucket,
  window: TimeRange
): BucketSize {
  return bucket === "auto" || bucket === "whole"
    ? autoBucket(window)
    : BUCKET_SIZES[bucket]
}

/** Every metric of a chart over `window` (the record's, or a part zoomed
 * into) in as few reads as the server admits: a line's buckets, or one row
 * per metric over the window where the chart's bucket is `whole`. Each row
 * names its metric in `group.metric`. A read names at most `MAX_METRICS`
 * metrics, and the server holds a read's series times buckets to its
 * budget together, so where the metrics' buckets pass the default the read
 * asks for the cap (the estimate is a guess), and holds no more metrics
 * than fit under it. `readOf[i]` is the index of the read metric i is in. */
export function chartReads(
  b: Bound,
  chart: ChartBlock,
  window: TimeRange,
  timezone: string
): { reads: AggregateRead[]; readOf: number[] } {
  const reducers = chart.reducers.map((r) => r.reducer)
  const size =
    chart.bucket === "whole" ? undefined : chartBucketSize(chart.bucket, window)
  // One more bucket than the range spans: a range off the buckets' edges
  // touches one at each end.
  const perMetric = size ? bucketEstimate(window, size) + 1 : 1
  const most = Math.max(
    1,
    Math.min(MAX_METRICS, Math.floor(MAX_BUCKETS_CAP / perMetric))
  )
  const reads: AggregateRead[] = []
  const readOf: number[] = []
  for (let at = 0; at < chart.metrics.length; at += most) {
    const metrics = chart.metrics.slice(at, at + most)
    metrics.forEach(() => readOf.push(reads.length))
    reads.push(
      metricsReadOf(
        b,
        chart.sources,
        chart.channel,
        metrics.map((m) =>
          metricEntry(m, reducers, chart.from, chart.statistic, chart.unit)
        ),
        {
          range: rangeOf(window),
          ...(size ? { bucket: bucketOf(size, timezone) } : {}),
          ...(perMetric * metrics.length > MAX_BUCKETS
            ? { budget: { maxBuckets: MAX_BUCKETS_CAP } }
            : {}),
        }
      )
    )
  }
  return { reads, readOf }
}

/** The reducer a chart's line draws: the first in the metric's own unit,
 * else the first named. */
export function chartLineReducer(
  reducers: readonly ChartReducer[]
): ChartReducer {
  return reducers.find((r) => bandedReducer(r.key)) ?? reducers[0]
}

/** One metric's rows of a chart's batched answer. A row naming no metric
 * is the one metric's of a read of one. */
export function chartMetricRows(
  block: Pick<ChartBlock, "metrics">,
  metric: string,
  answer: AggregateResult | undefined
): AggregateRow[] {
  const alone = block.metrics.length === 1
  return (answer?.rows ?? []).filter((r) =>
    r.group?.metric ? sameRef(r.group.metric, metric) : alone
  )
}

/** Whether a chart's answer holds a value of one metric: a line's reducer
 * in some bucket, or for a `whole` chart any of its reducers. A chart
 * under `empty: hide` none of whose metrics was measured is left out. */
export function chartMeasured(
  block: Pick<ChartBlock, "metrics" | "reducers" | "bucket">,
  metric: string,
  answer: AggregateResult | undefined
): boolean {
  const keys =
    block.bucket === "whole"
      ? block.reducers.map((r) => r.key)
      : [chartLineReducer(block.reducers).key]
  return chartMetricRows(block, metric, answer).some((row) =>
    keys.some((k) => {
      const v = rowValue(row, k)
      return Boolean(v) && v!.value !== null
    })
  )
}

/** Whether an item naming a code reads grouped by code: every coded
 * reducer but a time in a state, which names its code in the reducer and
 * answers it in a bucket that held none of the state (as 0) only where the
 * read is not split by code. */
export function readsByCode(item: {
  code?: string
  reducer: AggregateReducer
}): boolean {
  return (
    Boolean(item.code) && !reducerKey(item.reducer).startsWith("timeInState")
  )
}

// ── stats ─────────────────────────────────────────────────────────────────

/** The metric record path an item reads: its own `metric`, or the path
 * the record's `metricProperty` reference holds. */
export function itemMetric(
  item: Pick<StatItem, "metric" | "metricProperty">,
  record?: SubstrateRecord
): string | undefined {
  if (item.metric) return item.metric
  if (!item.metricProperty || !record) return undefined
  return metricPathIn(record.properties?.[item.metricProperty])
}

/** How an item's metric is read, apart from its reducer: two items alike
 * here share one entry. */
function entrySpec(item: {
  from: ReadFrom
  statistic?: string
  unit?: string
}): string {
  return JSON.stringify([item.from, item.statistic ?? "", item.unit ?? ""])
}

/** The reads of a stats block. A read names a metric once, so the k-th
 * distinct way an item reads a metric goes to the k-th read of its family;
 * items that read a metric alike share its entry, each adding its reducer.
 * Items naming a code read in reads of their own, grouped by code
 * (`readsByCode`; a time in a state reads with the plain ones). Plain
 * reads come first in `value`, then the coded ones; `itemRead[i]` is the
 * index in `value` of the read item i is answered by, -1 for a property
 * item or one whose metric `record` does not name. With `baseline > 0`,
 * each value read's entries are read again by the day over the `baseline`
 * local days before the window (`baselineRange`, which ends a day later for
 * a metric placing a span by its end); `itemBaseline[i]` is the index in
 * `baseline` of item i's read, -1 where it has none, as over a record's own
 * window still running for a value that grows over it (`partialItem`), and
 * `itemDays[i]` the days of that read item i compares with.
 * Nothing is read without a window. */
export function statsReads(
  b: Bound,
  block: StatsBlock,
  timezone: string,
  // The metric records: a coded metric's codes size its baseline's budget.
  infos: ReadonlyMap<string, MetricInfo> = new Map(),
  record?: SubstrateRecord
): {
  value: AggregateRead[]
  baseline: AggregateRead[]
  itemRead: number[]
  itemBaseline: number[]
  itemDays: (TimeRange | undefined)[]
} {
  const itemRead = block.items.map(() => -1)
  if (!b.window) {
    return {
      value: [],
      baseline: [],
      itemRead,
      itemBaseline: [...itemRead],
      itemDays: block.items.map(() => undefined),
    }
  }
  type Entry = { ref: string; spec: StatItem; reducers: AggregateReducer[] }
  const families: Record<"plain" | "coded", Entry[][]> = {
    plain: [],
    coded: [],
  }
  const placed: { family: "plain" | "coded"; read: number }[] = []
  block.items.forEach((item, i) => {
    const ref = itemMetric(item, record)
    if (!ref || item.property) return
    const family = readsByCode(item) ? "coded" : "plain"
    const reads = families[family]
    let at = reads.findIndex((entries) =>
      entries.some(
        (e) => e.ref === ref && entrySpec(e.spec) === entrySpec(item)
      )
    )
    if (at >= 0) {
      const entry = reads[at].find((e) => e.ref === ref)!
      if (!entry.reducers.some((r) => reducerKey(r) === item.key)) {
        entry.reducers.push(item.reducer)
      }
    } else {
      at = reads.findIndex((entries) => !entries.some((e) => e.ref === ref))
      if (at < 0) {
        reads.push([])
        at = reads.length - 1
      }
      reads[at].push({ ref, spec: item, reducers: [item.reducer] })
    }
    placed[i] = { family, read: at }
  })
  const plainCount = families.plain.length
  placed.forEach((p, i) => {
    if (p) itemRead[i] = p.family === "plain" ? p.read : plainCount + p.read
  })
  const reads = [
    ...families.plain.map((entries) => ({ entries, coded: false })),
    ...families.coded.map((entries) => ({ entries, coded: true })),
  ]
  const entriesOf = (entries: Entry[], base = false) =>
    entries.map((e) =>
      metricEntry(
        e.ref,
        base ? baselineReducers(e.reducers, infos.get(e.ref)) : e.reducers,
        e.spec.from,
        e.spec.statistic,
        e.spec.unit
      )
    )
  const value = reads.map(({ entries, coded }) =>
    metricsReadOf(b, block.sources, block.channel, entriesOf(entries), {
      ...(coded ? { groupBy: ["code"] } : {}),
    })
  )
  // One baseline read per value read over the days its entries compare
  // with. A metric placing a span by its end lands a night on the day it
  // ends, so its days can end a day later than a start-placed metric's:
  // the read covers both, and each item keeps its own days of it.
  const baseline: AggregateRead[] = []
  const baselineAt: number[] = reads.map(() => -1)
  const daysOf = new Map<boolean, TimeRange>()
  if (block.baseline > 0) {
    for (const end of [false, true]) {
      // A record's own window still running holds part of a day, or part
      // of a cycle: against whole days before it, a value that grows over
      // the window would mislead. A night's summary placed by its end is
      // whole once the night ends, so it is compared.
      if (!end && partialWindow(b)) continue
      daysOf.set(end, baselineRange(b, block.baseline, end))
    }
    reads.forEach(({ entries, coded }, r) => {
      const ranges = [
        ...new Set(entries.map((e) => placesByEnd(infos, e.ref))),
      ].flatMap((end) => daysOf.get(end) ?? [])
      if (!ranges.length) return
      const span = {
        start: Math.min(...ranges.map((x) => x.start)),
        end: Math.max(...ranges.map((x) => x.end)),
      }
      const days = Math.round((span.end - span.start) / 86_400_000)
      // The server holds every entry's days, times its codes where grouped
      // by code, to the read's budget together: past the default, the read
      // asks for what its entries need, with a day more for a zone that is
      // not the browser's.
      const series = entries.reduce(
        (n, e) =>
          n + (coded ? Math.max(1, infos.get(e.ref)?.codeSet?.length ?? 1) : 1),
        0
      )
      const buckets = (days + 1) * series
      baselineAt[r] = baseline.length
      baseline.push(
        metricsReadOf(
          b,
          block.sources,
          block.channel,
          entriesOf(entries, true),
          {
            range: rangeOf(span),
            bucket: bucketOf("day", timezone),
            ...(coded ? { groupBy: ["code"] } : {}),
            ...(buckets > MAX_BUCKETS
              ? { budget: { maxBuckets: Math.min(MAX_BUCKETS_CAP, buckets) } }
              : {}),
          }
        )
      )
    })
  }
  const itemBaseline = block.items.map((item, i) => {
    const ref = itemMetric(item, record)
    const days = ref ? daysOf.get(placesByEnd(infos, ref)) : undefined
    return itemRead[i] < 0 || !days ? -1 : baselineAt[itemRead[i]]
  })
  const itemDays = block.items.map((item) => {
    const ref = itemMetric(item, record)
    return ref ? daysOf.get(placesByEnd(infos, ref)) : undefined
  })
  return { value, baseline, itemRead, itemBaseline, itemDays }
}

/** The rows of a baseline answer whose day lies in `days`, an item's own
 * (`statsReads`'s `itemDays`): a row's middle decides, so a day bucketed
 * in a zone not the browser's still falls on one side. */
export function rowsInDays<T extends { start: string; end: string }>(
  rows: readonly T[],
  days?: TimeRange
): T[] {
  if (!days) return [...rows]
  return rows.filter((row) => {
    const mid = (Date.parse(row.start) + Date.parse(row.end)) / 2
    return mid >= days.start && mid < days.end
  })
}

/** Whether a metric's record places a value's span at its end. */
function placesByEnd(
  infos: ReadonlyMap<string, MetricInfo>,
  ref: string
): boolean {
  return infos.get(ref)?.bucketPlace === "end"
}

/** Whether a stats item's value is part of what its window will hold: a
 * record's own window still running (`partialWindow`), read from a metric
 * that does not place a span by its end. A night's summary placed by its
 * end (a recovery, last night's HRV) is whole once the night ends. */
export function partialItem(b: Bound, info?: MetricInfo): boolean {
  return partialWindow(b) && info?.bucketPlace !== "end"
}

/** The local days a stats baseline reads: `days` days ending at the start
 * of the day the window's own value lands on. A start-placed metric lands
 * on the window's first day, as does a view's `last` window. A metric
 * placed by its end lands on the day its span ends: the window's last day,
 * or for a window that runs past it (a cycle opening with a night), the
 * day the night opening it ends, taken as the window's start plus 12
 * hours. A sleep from 23:50 lands on the next day, so the night before it
 * is among the days it compares with. */
function baselineRange(b: Bound, days: number, end: boolean): TimeRange {
  const w = b.window!
  const at =
    end && !b.lastWindow
      ? Math.min(w.end - 1, w.start + 12 * 3_600_000)
      : w.start
  const first = new Date(at)
  const dayStart = new Date(
    first.getFullYear(),
    first.getMonth(),
    first.getDate()
  )
  const from = new Date(
    first.getFullYear(),
    first.getMonth(),
    first.getDate() - days
  )
  return { start: from.getTime(), end: dayStart.getTime() }
}

/** The reducer a stats item's baseline reads per day. A gauge's first or
 * last reading of a day is whatever the instrument said at one arbitrary
 * instant (a room's light at midnight), so a gauge read by `first` or
 * `last` is compared against each day's average; every other reducer, and
 * any reducer of an amount, a counter, a summary or a category, reads
 * per day as the item reads its window (a sum a day, a day's highest).
 * The form is the metric record's. */
export function baselineReducer(
  reducer: AggregateReducer,
  info?: Pick<MetricInfo, "form" | "codeSet">
): AggregateReducer {
  if (info?.form !== "gauge" || info.codeSet?.length) return reducer
  const key = reducerKey(reducer)
  return key === "first" || key === "last" ? "avg" : reducer
}

/** `baselineReducer` over an entry's reducers, each once. */
function baselineReducers(
  reducers: readonly AggregateReducer[],
  info?: MetricInfo
): AggregateReducer[] {
  const out: AggregateReducer[] = []
  for (const r of reducers) {
    const mapped = baselineReducer(r, info)
    if (!out.some((o) => reducerKey(o) === reducerKey(mapped))) out.push(mapped)
  }
  return out
}

/** The average, lowest and highest of the daily values one reducer
 * answered in a baseline's rows, and how many days held one. Undefined
 * where none did. */
export function baselineOf(
  rows: readonly AggregateRow[],
  key: string
): { avg: number; low: number; high: number; days: number } | undefined {
  const values = rows.flatMap((r) => {
    const n = numberOf(rowValue(r, key)?.value)
    return n === null ? [] : [n]
  })
  if (!values.length) return undefined
  return {
    avg: values.reduce((a, v) => a + v, 0) / values.length,
    low: Math.min(...values),
    high: Math.max(...values),
    days: values.length,
  }
}

/** Reducers whose answer grows with the window: a sum, a count, the time
 * spent in a state or a range. */
const ADDITIVE = new Set(["sum", "count", "timeInState", "durationIn"])

/** A baseline as the window reads it. A daily baseline answers one day, so
 * a view's `last` window of several days read by an additive reducer (the
 * time a door stood open over the last week) is compared with that many
 * average days, not with one. A record's own window is never scaled: an
 * open one runs to now, which is no length the value was measured over (a
 * cycle whose provider stopped syncing). Any other reducer, and a window of
 * a day or less, keeps the daily figures. `span` in the answer is the
 * number of days scaled by, 1 where nothing was. */
export function scaleBaseline(
  base: { avg: number; low: number; high: number; days: number },
  reducer: AggregateReducer,
  window?: TimeRange,
  lastWindow = false
): { avg: number; low: number; high: number; days: number; span: number } {
  const name = reducerKey(reducer).split(":")[0] ?? ""
  const span =
    window && lastWindow
      ? Math.max(1, Math.round((window.end - window.start) / 86_400_000))
      : 1
  if (!ADDITIVE.has(name) || span <= 1) return { ...base, span: 1 }
  return {
    avg: base.avg * span,
    low: base.low * span,
    high: base.high * span,
    days: base.days,
    span,
  }
}

// ── trend ─────────────────────────────────────────────────────────────────

export type CalendarBucket = "day" | "week" | "month"

/** The local start of the calendar bucket `t` lies in: midnight, the
 * Monday of its week, the first of its month. */
export function bucketStart(t: number, bucket: CalendarBucket): number {
  const d = new Date(t)
  switch (bucket) {
    case "day":
      return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
    case "week":
      return new Date(
        d.getFullYear(),
        d.getMonth(),
        d.getDate() - ((d.getDay() + 6) % 7)
      ).getTime()
    case "month":
      return new Date(d.getFullYear(), d.getMonth(), 1).getTime()
  }
}

/** The start of the bucket `n` buckets after the one starting at `t` (a
 * bucket start), in local calendar arithmetic, so a day across a change of
 * offset is 23 or 25 hours. */
export function shiftBucket(
  t: number,
  bucket: CalendarBucket,
  n: number
): number {
  const d = new Date(t)
  const [y, m, day] = [d.getFullYear(), d.getMonth(), d.getDate()]
  switch (bucket) {
    case "day":
      return new Date(y, m, day + n).getTime()
    case "week":
      return new Date(y, m, day + 7 * n).getTime()
    case "month":
      return new Date(y, m + n, 1).getTime()
  }
}

/** Every calendar bucket from the one holding `range.start` up to
 * `range.end`, empty of rows: a strip's whole axis, so a bucket with no
 * value still shows. */
export function calendarGrid(
  range: TimeRange,
  bucket: CalendarBucket
): Bucket[] {
  const out: Bucket[] = []
  for (
    let t = bucketStart(range.start, bucket);
    t < range.end;
    t = shiftBucket(t, bucket, 1)
  ) {
    const end = shiftBucket(t, bucket, 1)
    out.push({
      start: t,
      end,
      startIso: isoWithOffset(t),
      endIso: isoWithOffset(end),
      rows: [],
    })
  }
  return out
}

/** The instant whose bucket holds the record on a strip: the window's last
 * instant for a `last` window or a metric placing a span at its end, else
 * its start. Undefined without a window. */
export function trendAnchor(
  b: Bound,
  bucketPlace?: "start" | "end"
): number | undefined {
  if (!b.window) return undefined
  return b.lastWindow || bucketPlace === "end"
    ? b.window.end - 1
    : b.window.start
}

/** The range a trend reads: `span` buckets ending with the latest bucket an
 * item's anchor lies in, starting `span - 1` buckets before the earliest. */
export function trendRange(
  b: Bound,
  block: TrendBlock,
  infos: ReadonlyMap<string, MetricInfo> = new Map(),
  record?: SubstrateRecord
): TimeRange | undefined {
  const anchors = block.items.flatMap((item) => {
    const ref = itemMetric(item, record)
    const a = trendAnchor(b, ref ? infos.get(ref)?.bucketPlace : undefined)
    return a === undefined ? [] : [a]
  })
  if (!anchors.length) {
    const a = trendAnchor(b)
    if (a === undefined) return undefined
    anchors.push(a)
  }
  const earliest = bucketStart(Math.min(...anchors), block.bucket)
  const latest = bucketStart(Math.max(...anchors), block.bucket)
  return {
    start: shiftBucket(earliest, block.bucket, -(block.span - 1)),
    end: shiftBucket(latest, block.bucket, 1),
  }
}

/** A trend block's one read: every item's metric by the calendar bucket
 * over `trendRange`, with the trailing average through the bucket before
 * where the block asks for one. Undefined without a window. */
export function trendRead(
  b: Bound,
  block: TrendBlock,
  timezone: string,
  infos: ReadonlyMap<string, MetricInfo> = new Map(),
  record?: SubstrateRecord
): AggregateRead | undefined {
  const range = trendRange(b, block, infos, record)
  if (!range) return undefined
  const entries: {
    ref: string
    item: TrendItem
    reducers: AggregateReducer[]
  }[] = []
  for (const item of block.items) {
    const ref = itemMetric(item, record)
    if (!ref) continue
    const held = entries.find((e) => e.ref === ref)
    if (held) {
      if (!held.reducers.some((r) => reducerKey(r) === item.key)) {
        held.reducers.push(item.reducer)
      }
      continue
    }
    entries.push({ ref, item, reducers: [item.reducer] })
  }
  if (!entries.length) return undefined
  const buckets = block.span * entries.length
  const coded = block.items.some(readsByCode)
  return metricsReadOf(
    b,
    block.sources,
    block.channel,
    entries.map((e) =>
      metricEntry(e.ref, e.reducers, e.item.from, e.item.statistic, e.item.unit)
    ),
    {
      range: rangeOf(range),
      bucket: bucketOf(block.bucket, timezone),
      ...(coded ? { groupBy: ["code"] } : {}),
      ...(block.rolling > 0
        ? {
            rolling: {
              buckets: block.rolling,
              reducers: ["avg"],
              through: "previous",
            },
          }
        : {}),
      ...(buckets > MAX_BUCKETS ? { budget: { maxBuckets: buckets } } : {}),
    }
  )
}

// ── table ─────────────────────────────────────────────────────────────────

/** A table block's list read, or why it cannot be read. `about` narrows
 * the list only where the listed kind declares an `about` property, and
 * then never to everyone's. */
export function tableParams(
  b: Bound | undefined,
  block: TableBlock,
  record: SubstrateRecord,
  listedKind?: KindInfo
): ListParams | { problem: string } {
  const properties: Record<string, Cond> = {}
  if (block.via) {
    properties[block.via] = { eq: `${record.kind}/${record.id}` }
  } else if (block.sharing.length) {
    // A property this record holds narrows to its value; one it does not
    // hold narrows to the records that hold none either, so a blood test
    // (no channel) lists blood tests and a trunk result lists the trunk's.
    let held = 0
    for (const name of block.sharing) {
      const value = sharedValue(record.properties?.[name])
      if (value === undefined) {
        properties[name] = { exists: false }
      } else {
        properties[name] = { eq: value }
        held++
      }
    }
    if (!held) {
      return {
        problem: `This table lists the records sharing this record's ${listWords(block.sharing)}, and this record holds ${block.sharing.length > 1 ? "none of them" : "none"}.`,
      }
    }
  } else if (block.inWindow) {
    if (!b?.window) {
      return {
        problem:
          "This table lists the records inside the view's window, and this record holds no window.",
      }
    }
    properties.at = {
      gte: new Date(b.window.start).toISOString(),
      lt: new Date(b.window.end).toISOString(),
    }
  }
  const declared = listedKind?.definition?.properties as
    Record<string, unknown> | undefined
  // Narrowed only where `about` is a reference, the observed contract's
  // "who this is about": a kind whose `about` means something else is
  // listed whole.
  const about =
    declared && Object.hasOwn(declared, "about") ? declared.about : undefined
  if (about === "reference" || (isBag(about) && about.type === "reference")) {
    properties.about = b?.about ? { eq: b.about } : { exists: false }
  }
  return {
    kinds: [block.kind],
    first: block.limit,
    orderBy: block.orderBy,
    filter: { properties },
  }
}

/** The most earlier results a table's tiles read to find each result's
 * previous one. */
export const TILE_HISTORY = 100

/** The read a table's tiles compare against: the listed kind's results of
 * the same metrics, before the earliest listed result, newest first, about
 * whom the table's own read is. Undefined where no listed result names a
 * metric and a time. */
export function tileHistoryParams(
  table: ListParams,
  listed: readonly SubstrateRecord[]
): ListParams | undefined {
  const metrics = new Set<string>()
  let earliest: number | undefined
  for (const r of listed) {
    const p = r.properties ?? {}
    const metric = readReference(p.metric)?.path
    const at = instant(p.at)
    if (!metric || at === undefined) continue
    metrics.add(metric)
    earliest = earliest === undefined ? at : Math.min(earliest, at)
  }
  if (!metrics.size || earliest === undefined) return undefined
  const about = table.filter?.properties?.about
  return {
    kinds: table.kinds,
    first: TILE_HISTORY,
    orderBy: "at:desc",
    filter: {
      properties: {
        metric: { in: [...metrics].sort() },
        at: { lt: new Date(earliest).toISOString() },
        ...(about ? { about } : {}),
      },
    },
  }
}

/** A record's value as a list filter compares it: a reference's path, a
 * string, a number or a boolean. Undefined for anything else, which the
 * table reads as holding none. */
function sharedValue(v: unknown): string | undefined {
  return (
    readReference(v)?.path ??
    (typeof v === "number" || typeof v === "boolean" ? String(v) : undefined)
  )
}

/** Names in a sentence: `metric`, `metric and channel`, `a, b and c`. */
function listWords(names: readonly string[]): string {
  if (names.length < 2) return names.join("")
  return `${names.slice(0, -1).join(", ")} and ${names[names.length - 1]}`
}

// ── breakdown and values of the record ────────────────────────────────────

/** The value at a property of the record, or at a dotted path into a keyed
 * map or object it holds. */
export function valueAt(record: SubstrateRecord, path: string): unknown {
  let at: unknown = record.properties ?? {}
  for (const part of path.split(".")) {
    if (!isBag(at)) return undefined
    at = at[part]
  }
  return at
}

const DECIMAL = /^[+-]?\d+(\.\d+)?$/

/** The datatypes a table cell holds a fraction in. */
const FRACTIONAL = new Set(["float", "float64", "double", "number", "decimal"])

/** How many fraction digits a table cell shows of a fractional property,
 * which declares no display precision of its own. */
export const CELL_FRACTION_DIGITS = 2

/** A fractional property's value as a table cell shows it: at most
 * `CELL_FRACTION_DIGITS` fraction digits, rounded on the digits, with the
 * zeros the rounding leaves dropped (`8.3985` is `8.4`), and the digits
 * grouped. An integer, and a value already that short, keep their
 * digits. Undefined for any other datatype or value, which the cell draws
 * as declared. */
export function cellFraction(
  datatype: string,
  value: unknown
): string | undefined {
  if (!FRACTIONAL.has(datatype)) return undefined
  let digits: string
  if (typeof value === "number" && Number.isFinite(value)) {
    digits = String(value)
    if (!DECIMAL.test(digits)) digits = value.toFixed(CELL_FRACTION_DIGITS)
  } else if (typeof value === "string" && DECIMAL.test(value)) {
    digits = value
  } else {
    return undefined
  }
  digits = digits.replace(/^\+/, "")
  let shown = roundDigits(digits, CELL_FRACTION_DIGITS)
  if (shown !== digits && shown.includes(".")) {
    shown = shown.replace(/0+$/, "").replace(/\.$/, "")
  }
  return groupDigits(shown)
}

/** A value a bar or a tile can draw: a number, a decimal, an ISO duration (in
 * milliseconds) or a quantity, with its text. Undefined for anything
 * else. */
export function amountOf(
  v: unknown,
  precision = DEFAULT_PRECISION
): { amount: number; text: string } | undefined {
  if (typeof v === "number" && Number.isFinite(v)) {
    return { amount: v, text: readingText(String(v), undefined, precision) }
  }
  if (typeof v === "string") {
    // A decimal travels as a string.
    if (DECIMAL.test(v)) {
      return { amount: Number(v), text: readingText(v, undefined, precision) }
    }
    const ms = durationMs(v)
    if (ms !== undefined) return { amount: ms, text: durationWords(ms) }
    return undefined
  }
  if (isBag(v) && given(v.value) && typeof v.unit === "string") {
    const n = Number(v.value)
    if (!Number.isFinite(n)) return undefined
    return { amount: n, text: readingText(String(v.value), v.unit, precision) }
  }
  return undefined
}

export interface BreakdownItem {
  key: string
  name: string
  amount: number | null
  text: string
  /** The bar's share of the total, 0 to 1. */
  share?: number
}

/** A breakdown's bars from the record: one per key of the keyed property
 * (listed keys first, in order, the rest after) or one per listed scalar
 * property, each with its share of `total` or of the bars' sum. A value
 * that is no number, duration or quantity is left out and named in
 * `skipped`. */
export function breakdownItems(
  block: BreakdownBlock,
  record: SubstrateRecord
): { items: BreakdownItem[]; skipped: string[] } {
  const named: { key: string; name: string; value: unknown }[] = []
  if (block.property) {
    const map = valueAt(record, block.property)
    if (isBag(map)) {
      const listed = block.keys.filter((k) => Object.hasOwn(map, k.key))
      for (const k of listed) {
        named.push({ key: k.key, name: k.label ?? k.key, value: map[k.key] })
      }
      for (const key of Object.keys(map)) {
        if (!block.keys.some((k) => k.key === key)) {
          named.push({ key, name: key, value: map[key] })
        }
      }
    }
  } else {
    for (const i of block.items) {
      const value = valueAt(record, i.property)
      if (value === undefined || value === null) continue
      named.push({ key: i.property, name: i.label ?? i.property, value })
    }
  }
  const skipped: string[] = []
  const items: BreakdownItem[] = []
  for (const n of named) {
    const a = amountOf(n.value)
    if (!a) {
      skipped.push(n.key)
      continue
    }
    items.push({ key: n.key, name: n.name, amount: a.amount, text: a.text })
  }
  const total = block.total
    ? amountOf(valueAt(record, block.total))?.amount
    : items.reduce((s, i) => s + (i.amount ?? 0), 0)
  if (total && total > 0) {
    for (const i of items) {
      if (i.amount !== null) i.share = i.amount / total
    }
  }
  return { items, skipped }
}

/** The first band in order whose bounds hold `value` and that applies to
 * `metric` (a band naming no metric applies to all). */
export function bandOf(
  value: number,
  bands: readonly Band[],
  metric?: string
): Band | undefined {
  return bands.find(
    (band) =>
      (band.low ?? -Infinity) <= value &&
      value < (band.high ?? Infinity) &&
      (!band.metric || sameRef(band.metric, metric))
  )
}

/** A value's change against a baseline's average, in words, and the tone
 * `better` gives it: "+4 ms (+6%) vs the 30-day average of 61 ms". A value
 * in percent changes by points, with no relative part: "+9 points vs the
 * 30-day average of 42 %". No tone where `better` is none or nothing
 * changed. */
export function deltaWords(
  value: number,
  avg: number,
  better: Better,
  unit?: string,
  precision = 0,
  days?: number
): { text: string; tone: Tone } {
  const change = value - avg
  const round = (n: number) => Number(n.toFixed(precision))
  const shown = round(change)
  const sign = shown > 0 ? "+" : shown < 0 ? "-" : ""
  const fmt = (n: number) => readingText(String(round(n)), unit, precision)
  const average = days ? `the ${days}-day average` : "the average"
  const percent = unit === "%"
  const pct =
    !percent && avg !== 0
      ? Math.round((change / Math.abs(avg)) * 100)
      : undefined
  const pctText =
    pct === undefined
      ? ""
      : ` (${pct > 0 ? "+" : pct < 0 ? "-" : ""}${Math.abs(pct)}%)`
  const changed = percent
    ? `${readingText(String(round(Math.abs(change))), undefined, precision)} ${Math.abs(shown) === 1 ? "point" : "points"}`
    : fmt(Math.abs(change))
  const text = `${sign}${changed}${pctText} vs ${average} of ${fmt(avg)}`
  const tone: Tone =
    better === "none" || shown === 0
      ? "none"
      : shown > 0 === (better === "higher")
        ? "good"
        : "poor"
  return { text, tone }
}

/** A result's change since the previous result of its analyte, in words:
 * "+0.23 mmol/L (+6%) since Oct 15, 2025". A value in percent changes by
 * points, with no relative part. It carries no tone: a view names no better
 * direction for a lab's analyte. */
export function changeSinceWords(
  value: number,
  previous: number,
  previousAt: number,
  unit?: string,
  precision = DEFAULT_PRECISION
): string {
  const since = `since ${dayWords(previousAt, true)}`
  const round = (n: number) => Number(n.toFixed(precision))
  const change = value - previous
  const shown = round(change)
  if (shown === 0) return `No change ${since}`
  const sign = shown > 0 ? "+" : "-"
  const size = String(round(Math.abs(change)))
  if (unit === "%") {
    const points = Math.abs(shown) === 1 ? "point" : "points"
    return `${sign}${readingText(size, undefined, precision)} ${points} ${since}`
  }
  const pct =
    previous !== 0 ? Math.round((change / Math.abs(previous)) * 100) : 0
  const pctText = pct ? ` (${pct > 0 ? "+" : "-"}${Math.abs(pct)}%)` : ""
  return `${sign}${readingText(size, unit, precision)}${pctText} ${since}`
}

// ── spans ─────────────────────────────────────────────────────────────────

/** A coded metric's total per code over the whole window. */
export function spansTotalsRead(b: Bound, spans: SpansBlock): AggregateRead {
  const sources = sourcesOf(b, spans.sources)
  return {
    filter: filterOf(b, spans.sources),
    aggregate: {
      metric: { ref: spans.metric },
      from: "raw",
      range: rangeOf(windowOf(b)),
      groupBy: ["code"],
      reducers: ["sum"],
      unit: "min",
      ...(sources ? { sources } : {}),
    },
  }
}

/** How many spans one list reads; past it the block says how many it
 * drew of how many. */
export const SPANS_PAGE = 500

/** Whether a spans block's list is narrowed to one source by its policy
 * alone: `only`, on a record naming its source. Otherwise the list follows
 * the totals' read (`spansSource`). */
export function spansNarrowed(b: Bound, spans: SpansBlock): boolean {
  return spans.sources === "only" && Boolean(b.source)
}

/** The one source a spans list draws where the policy does not narrow it
 * (`spansNarrowed`), read off the totals the policy answered, so the
 * spans and the totals beside them show one source: the record's own under
 * `prefer` where it fed them, else the source that fed the most of them.
 * `""` is the streams that name no source. Undefined where the totals name
 * none, and the list then reads what the narrowing takes. */
export function spansSource(
  b: Bound,
  spans: SpansBlock,
  totals: AggregateResult | undefined
): string | undefined {
  if (spansNarrowed(b, spans)) return undefined
  const fed = new Map<string, number>()
  for (const row of totals?.rows ?? []) {
    for (const s of row.sources ?? []) fed.set(s, (fed.get(s) ?? 0) + 1)
  }
  if (!fed.size) return undefined
  if (spans.sources === "prefer" && b.source) {
    const own = [...fed.keys()].find((s) => sameRef(s, b.source))
    if (own !== undefined) return own
  }
  return [...fed].sort((x, y) => y[1] - x[1] || x[0].localeCompare(y[0]))[0][0]
}

/** How many sources fed the totals of a spans block, where its list does
 * not follow the policy alone: more than one, and the spans drawn are one
 * of them. */
export function spansSourceCount(totals: AggregateResult | undefined): number {
  return new Set((totals?.rows ?? []).flatMap((r) => r.sources ?? [])).size
}

/** The list of a coded metric's spans that start inside the window,
 * oldest first: the first `SPANS_PAGE` of them, and a cursor where more
 * follow. It is a window read (`at` bounded on both ends), which the server
 * refuses to count. `source` narrows it to one source (`spansSource`), `""`
 * to the streams naming none. */
export function spansListParams(
  b: Bound,
  spans: SpansBlock,
  source?: string
): ListParams {
  const narrow = narrowing(b, spans.sources)
  if (source !== undefined) {
    narrow.source = source ? { eq: source } : { exists: false }
  }
  const window = windowOf(b)
  return {
    kinds: b.observed,
    first: SPANS_PAGE,
    orderBy: "at:asc",
    filter: {
      properties: {
        metric: { eq: spans.metric },
        ...narrow,
        at: {
          gte: new Date(window.start).toISOString(),
          lt: new Date(window.end).toISOString(),
        },
      },
    },
  }
}

/** One span of a coded metric: its code and its instants. */
export interface Span {
  code: string
  start: number
  end: number
}

/** Coded records as spans, oldest first: each naming a code over a span
 * that ends after it starts. */
export function spansOf(records: readonly SubstrateRecord[]): Span[] {
  const out: Span[] = []
  for (const r of records) {
    const p = r.properties ?? {}
    const start = typeof p.at === "string" ? Date.parse(p.at) : NaN
    const end = typeof p.endsAt === "string" ? Date.parse(p.endsAt) : NaN
    if (typeof p.code !== "string" || !p.code) continue
    if (Number.isNaN(start) || Number.isNaN(end) || end <= start) continue
    out.push({ code: p.code, start, end })
  }
  return out.sort((a, b) => a.start - b.start)
}

/** The spans as a sequence, where they are one: two or more that never
 * overlap, so each instant holds one code. A source that writes one span
 * per code over the whole window gives totals and no order, and draws
 * none. */
export function sequenceOf(spans: readonly Span[]): Span[] | undefined {
  if (spans.length < 2) return undefined
  for (let i = 1; i < spans.length; i++) {
    if (spans[i].start < spans[i - 1].end) return undefined
  }
  return [...spans]
}

/** The rows of a spans block, top to bottom: the codes the spans hold, in
 * the block's order, and a code it does not list after them by name. */
export function spanRows(
  spans: readonly Span[],
  order: readonly string[]
): string[] {
  const codes = [...new Set(spans.map((s) => s.code))]
  const rank = (c: string) => {
    const at = order.indexOf(c)
    return at < 0 ? order.length : at
  }
  return codes.sort((a, b) => rank(a) - rank(b) || a.localeCompare(b))
}

/** The timeline over the record's window, with every metric the view's
 * line charts, spans, stats and trends read, about who the record is
 * about. Undefined without a window or a metric. */
export function viewTimelineSearch(
  b: Bound,
  view: RecordView,
  record?: SubstrateRecord
): Record<string, string> | undefined {
  if (!b.window) return undefined
  const metrics = new Set<string>()
  for (const s of view.sections) {
    if (!("block" in s)) continue
    const block = s.block
    if (block.type === "chart" && block.bucket !== "whole") {
      for (const m of block.metrics) metrics.add(m)
    }
    if (block.type === "spans") metrics.add(block.metric)
    if (block.type === "stats" || block.type === "trend") {
      for (const item of block.items) {
        const m = itemMetric(item, record)
        if (m) metrics.add(m)
      }
    }
  }
  if (!metrics.size) return undefined
  return {
    metric: [...metrics].join(","),
    // The owner's where the record names nobody, never everyone's.
    about: addressValue(b.about ?? NONE),
    range: "custom",
    start: new Date(b.window.start).toISOString(),
    end: new Date(b.window.end).toISOString(),
  }
}

/** A declared value as a problem names it: a string as written, anything
 * else as JSON. */
function said(v: unknown): string {
  if (v === undefined) return "absent"
  return typeof v === "string" ? v : JSON.stringify(v)
}
