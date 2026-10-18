/** Record views (decision record 0166) without React: a
 * `substrate.reamde.dev/console/recordview` record read into the blocks a
 * record page draws above its generic sheet, the window a view binds on a
 * record by its properties' names, and the reads its chart and spans blocks
 * make over that window. Nothing here names a sample or a metric: every
 * kind, metric, reducer and code comes from the view record. A metric is
 * the full record path the view's reference holds, never an id resolved
 * against some metric kind: two metric kinds may hold one id. */

import type { AggregateRead } from "@/lib/api/observations"
import { CORE_AUTHORITY, CORE_PACKAGE } from "@/lib/api/http"
import type { ListParams } from "@/lib/api/records"
import {
  readReference,
  type AggregateReducer,
  type AggregateResult,
  type AggregateSources,
  type Cond,
  type KindInfo,
  type RecordFilter,
  type SubstrateRecord,
} from "@/lib/api/types"
import { ALL_REDUCERS } from "@/lib/aggregate-rules"
import { OBSERVED_TRAIT } from "@/lib/core-kinds"
import { splitRecordPath } from "@/lib/record-path"
import { isTimeUnit } from "@/lib/units"
import {
  addressValue,
  NONE,
  observedKinds,
  reducerKey,
  sameRef,
} from "@/lib/observations"
import {
  autoBucket,
  bucketOf,
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

/** Which of the record's own properties hold its window, who it is about
 * and what recorded it. */
export interface ViewWindow {
  start: string
  end: string
  about?: string
  source?: string
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

export type Block =
  | HeadingBlock
  | TextBlock
  | PropertiesBlock
  | RelatedBlock
  | ChartBlock
  | SpansBlock

/** The block types this console draws, in the order a notice names them. */
export const BLOCK_TYPES = [
  "heading",
  "text",
  "properties",
  "related",
  "chart",
  "spans",
] as const

/** One section: the block it holds, or why it cannot be drawn. */
export type Section = { block: Block } | { problem: string }

export interface RecordView {
  id: string
  name?: string
  intro?: string
  window?: ViewWindow
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
  const start = text(w?.start)
  const end = text(w?.end)
  return {
    id: record.id,
    name: text(p.name),
    intro: text(p.intro),
    window:
      start && end
        ? {
            start,
            end,
            ...(text(w?.about) ? { about: text(w?.about) } : {}),
            ...(text(w?.source) ? { source: text(w?.source) } : {}),
          }
        : undefined,
    fold: texts(p.fold),
    sections: bags(p.sections).map(sectionOf),
  }
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
    case "chart": {
      const named = Array.isArray(b.metrics) ? b.metrics : []
      if (!named.length) return { problem: "This chart names no metric." }
      const metrics: string[] = []
      for (const m of named) {
        const path = metricPathIn(m)
        if (!path) return { problem: `This chart ${notAMetric(m)}` }
        metrics.push(path)
      }
      const bucket = oneOf(b.bucket, CHART_BUCKETS, "auto")
      if (!bucket)
        return { problem: `This chart's bucket is ${said(b.bucket)}.` }
      const sources = oneOf(b.sources, SOURCE_POLICIES, "prefer")
      if (!sources)
        return { problem: `This chart's sources is ${said(b.sources)}.` }
      const reducers: ChartReducer[] = []
      for (const r of bags(b.reducers)) {
        const read = chartReducerOf(r)
        if (typeof read === "string") return { problem: read }
        reducers.push(read)
      }
      if (!reducers.length) return { problem: "This chart names no reducer." }
      return {
        block: {
          type,
          label: text(b.label),
          metrics,
          reducers,
          bucket,
          unit: text(b.unit),
          sources,
        },
      }
    }
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

/** What a view's chart and spans blocks read on one record: its window,
 * who it is about (absent where the view binds no `about` or the record
 * names nobody, which reads the owner's), what recorded it, and the
 * observed kinds the reads go through. */
export interface Bound {
  window: TimeRange
  about?: string
  source?: string
  observed: string[]
}

/** The view's window on a record: the instants its bound properties hold,
 * an end after the start, in a repository with an observed kind to read.
 * Undefined otherwise, and the chart and spans blocks then say they have
 * nothing to read. */
export function boundOf(
  view: RecordView,
  record: SubstrateRecord,
  kinds: readonly KindInfo[]
): Bound | undefined {
  const w = view.window
  if (!w) return undefined
  const p = record.properties ?? {}
  const at =
    typeof p[w.start] === "string" ? Date.parse(p[w.start] as string) : NaN
  const end =
    typeof p[w.end] === "string" ? Date.parse(p[w.end] as string) : NaN
  if (Number.isNaN(at) || Number.isNaN(end) || end <= at) return undefined
  const observed = observedKinds(kinds)
  if (!observed.length) return undefined
  return {
    window: { start: at, end },
    about: w.about ? readReference(p[w.about])?.path : undefined,
    source: w.source ? readReference(p[w.source])?.path : undefined,
    observed: observed.map((k) => k.identity),
  }
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

function filterOf(b: Bound, policy: SourcePolicy): RecordFilter {
  return {
    implements: OBSERVED_TRAIT,
    properties: narrowing(b, policy),
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

/** One metric of a chart over `window` (the record's, or a part zoomed
 * into): a line's buckets, or one row over the window where the chart's
 * bucket is `whole`. */
export function chartRead(
  b: Bound,
  chart: ChartBlock,
  metric: string,
  window: TimeRange,
  timezone: string
): AggregateRead {
  const sources = sourcesOf(b, chart.sources)
  return {
    filter: filterOf(b, chart.sources),
    aggregate: {
      metric: { ref: metric },
      from: "raw",
      range: rangeOf(window),
      ...(chart.bucket === "whole"
        ? {}
        : {
            bucket: bucketOf(chartBucketSize(chart.bucket, window), timezone),
          }),
      reducers: chart.reducers.map((r) => r.reducer),
      // A unit of duration beside a time in a range is the times' unit,
      // which the read takes as `durationUnit`; any other converts values.
      ...(chart.unit
        ? isTimeUnit(chart.unit) &&
          chart.reducers.some((r) =>
            reducerKey(r.reducer).startsWith("durationIn")
          )
          ? { durationUnit: chart.unit }
          : { unit: chart.unit }
        : {}),
      ...(sources ? { sources } : {}),
    },
  }
}

/** A coded metric's total per code over the whole window. */
export function spansTotalsRead(b: Bound, spans: SpansBlock): AggregateRead {
  const sources = sourcesOf(b, spans.sources)
  return {
    filter: filterOf(b, spans.sources),
    aggregate: {
      metric: { ref: spans.metric },
      from: "raw",
      range: rangeOf(b.window),
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
  return {
    kinds: b.observed,
    first: SPANS_PAGE,
    orderBy: "at:asc",
    filter: {
      properties: {
        metric: { eq: spans.metric },
        ...narrow,
        at: {
          gte: new Date(b.window.start).toISOString(),
          lt: new Date(b.window.end).toISOString(),
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
 * line charts and spans read, about who the record is about. */
export function viewTimelineSearch(
  b: Bound,
  view: RecordView
): Record<string, string> | undefined {
  const metrics = new Set<string>()
  for (const s of view.sections) {
    if (!("block" in s)) continue
    if (s.block.type === "chart" && s.block.bucket !== "whole") {
      for (const m of s.block.metrics) metrics.add(m)
    }
    if (s.block.type === "spans") metrics.add(s.block.metric)
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
