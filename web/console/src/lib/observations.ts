/** The observations pages' discovery, without React: which kinds hold
 * measured values (those binding core's `observed` trait), what a metric's
 * record and form let a reader ask for, and the streams the latest read
 * answers. Its siblings hold the rest: `observations-time.ts` (ranges,
 * buckets, durations), `observations-reads.ts` (the reads),
 * `observations-answer.ts` (an answer as buckets and series),
 * `observations-trace.ts` (a trace's values) and `timeline-address.ts` (the
 * choices in the address). Everything is a pure function of the registry,
 * the metric records, the URL state and the answer. */

import type {
  AggregateReducer,
  AggregateResult,
  AggregateRow,
  AggregateValue,
  KindInfo,
  SubstrateRecord,
} from "@/lib/api/types"
import { readReference } from "@/lib/api/types"
import { declaredProperties } from "@/lib/definition"
import { recordTitle } from "@/lib/format"
import { daysEnding, type TimeRange } from "@/lib/observations-time"
import { splitRecordPath } from "@/lib/record-path"
import { FORM_WORDS, OBSERVED_TRAIT_IDENTITY } from "@/lib/observed-form"
import { kindHasTrait } from "@/lib/traits"
import { formatQuantity } from "@/lib/units"

// ── discovery ──────────────────────────────────────────────────────────────

export { OBSERVED_TRAIT_IDENTITY }

/** `single` holds one value per record (an observation), `packed` many over
 * a fixed period (a trace). */
export type ObservedVariant = "single" | "packed"

/** The kinds binding the `observed` trait, by reference. Never a fixed
 * authority: an imported sample lands under the repository's own. */
export function observedKinds(kinds: readonly KindInfo[]): KindInfo[] {
  return kinds
    .filter((k) => kindHasTrait(k, OBSERVED_TRAIT_IDENTITY))
    .sort((a, b) => a.identity.localeCompare(b.identity))
}

/** Which layout a kind binding the trait uses: a kind declaring `values`
 * packs them. Undefined for a kind that does not bind the trait. */
export function observedVariant(
  kind: KindInfo | undefined
): ObservedVariant | undefined {
  if (!kind || !kindHasTrait(kind, OBSERVED_TRAIT_IDENTITY)) return undefined
  return declaredProperties(kind).some((p) => p.name === "values")
    ? "packed"
    : "single"
}

/** The properties each layout's section on a record page shows
 * (`components/observations/observed-section.tsx`), by the heading it shows
 * them under: the page folds them away from its property sheet. */
export const OBSERVED_SHOWN: Record<
  ObservedVariant,
  { names: readonly string[]; where: string }
> = {
  single: {
    where: "Measurement",
    names: [
      "value",
      "code",
      "metric",
      "source",
      "about",
      "form",
      "statistic",
      "period",
      "at",
      "endsAt",
      "revision",
      "origin",
    ],
  },
  packed: {
    where: "Readings",
    names: [
      "values",
      "offsets",
      "codes",
      "stats",
      "unit",
      "every",
      "at",
      "endsAt",
      "utcOffsetStart",
      "utcOffsetEnd",
    ],
  },
}

/** The kinds the trait's references are pinned to, read off the observed
 * kinds' own declarations: the first pin found for each. `about` is usually
 * pinned to `any` (it may point at any record), which names no kind, so it
 * is usually absent. */
export interface ObservedPins {
  metric?: string
  source?: string
  about?: string
}

export function observedPins(observed: readonly KindInfo[]): ObservedPins {
  const out: ObservedPins = {}
  for (const kind of observed) {
    for (const p of declaredProperties(kind)) {
      if (p.kind !== "reference" || !p.to || p.to === "any") continue
      if (p.name === "metric" || p.name === "source" || p.name === "about") {
        out[p.name] ??= p.to
      }
    }
  }
  return out
}

/** A group value as a record path: as given where it already is one, else
 * the id under the kind its reference is pinned to. Undefined where neither
 * says what kind the record is. */
export function refPathOf(
  value: string | undefined,
  pin: string | undefined
): string | undefined {
  if (!value) return undefined
  if (splitRecordPath(value)) return value
  return pin ? `${pin}/${value}` : undefined
}

/** Whether two spellings name one record: equal, or one is a record path
 * whose id is the other (the answer names a record by id, a stored
 * reference by its path). */
export function sameRef(a: string | undefined, b: string | undefined): boolean {
  if (!a || !b) return false
  if (a === b) return true
  return splitRecordPath(a)?.id === b || splitRecordPath(b)?.id === a
}

/** The id a group value or a stored reference names: a path's id, or the
 * value itself where it is no path. */
export function refIdOf(value: string): string {
  return splitRecordPath(value)?.id ?? value
}

/** The values that are set, once each, in the order they first come. */
export function distinct(values: readonly (string | undefined)[]): string[] {
  return [...new Set(values.filter((v): v is string => Boolean(v)))]
}

// ── metrics and forms ──────────────────────────────────────────────────────

/** The four forms a value is stored in (decision 0156). */
export type Form = "gauge" | "amount" | "counter" | "summary"

/** How a reader reads a metric: its form, or `coded` for a categorical
 * gauge or amount (a code instead of a number). */
export type ReadingShape = Form | "coded"

const FORMS: readonly Form[] = ["gauge", "amount", "counter", "summary"]

export function isForm(value: unknown): value is Form {
  return FORMS.includes(value as Form)
}

/** Everyday words for each shape, the forms' as every surface reads them
 * (`FORM_WORDS`); the stored word sits beside them in technical mode. */
export const SHAPE_WORDS: Record<ReadingShape, string> = {
  ...FORM_WORDS,
  coded: "Category",
}

/** What one metric record says that a reader uses: its name, its default
 * form and unit, and the codes a categorical metric admits. The engine
 * reads none of it (decision 0156); every value carries its own form and
 * unit, so these are defaults for display. */
export interface MetricInfo {
  id: string
  path: string
  name?: string
  form?: Form
  unit?: string
  /** The measure every value's unit belongs to (`mass`, `rate`). */
  measures?: string
  codeSet?: string[]
  /** The decimal places a reader is shown. */
  displayPrecision?: number
}

/** A metric record read as `MetricInfo`. A `codeSet` entry may be a bare
 * code or an object naming one under `code`. */
export function metricInfoOf(record: SubstrateRecord): MetricInfo {
  const p = record.properties ?? {}
  const codeSet = Array.isArray(p.codeSet)
    ? p.codeSet.flatMap((c) =>
        typeof c === "string"
          ? [c]
          : c && typeof c === "object" && typeof c.code === "string"
            ? [c.code as string]
            : []
      )
    : undefined
  const name =
    (typeof p.name === "string" && p.name) || recordTitle(p) || undefined
  return {
    id: record.id,
    path: `${record.kind}/${record.id}`,
    name,
    form: isForm(p.form) ? p.form : undefined,
    unit: typeof p.unit === "string" && p.unit ? p.unit : undefined,
    measures:
      typeof p.measures === "string" && p.measures ? p.measures : undefined,
    codeSet: codeSet?.length ? codeSet : undefined,
    ...(typeof p.displayPrecision === "number" &&
    Number.isInteger(p.displayPrecision)
      ? { displayPrecision: p.displayPrecision }
      : {}),
  }
}

/** The places a computed value is shown to where the metric names none. */
export const DEFAULT_PRECISION = 2

/** Decimal digits cut to `precision` places for display, rounded half
 * away from zero on the digits themselves (a binary float turns `1.005`
 * into `1.00`); digits with no more places than that come back as they
 * are, so a stored `72.40` keeps its zero, and a value rounded to zero has
 * no sign. The exact digits stay on the wire, in the copied table and on
 * hover. */
export function roundDigits(value: string, precision: number): string {
  const m = /^(-?)(\d+)\.(\d+)$/.exec(value)
  if (!m || m[3].length <= precision) return value
  const [, sign, whole, places] = m
  const digits = (whole + places.slice(0, precision)).split("").map(Number)
  if (Number(places[precision]) >= 5) {
    let i = digits.length - 1
    while (i >= 0 && digits[i] === 9) digits[i--] = 0
    if (i < 0) digits.unshift(1)
    else digits[i] += 1
  }
  const text = digits.join("")
  const cut = text.length - precision
  const out = precision ? `${text.slice(0, cut)}.${text.slice(cut)}` : text
  return sign && /[1-9]/.test(out) ? `-${out}` : out
}

/** A category's code as words: `rem` is "Rem", `not-detected` "Not
 * detected". */
export function codeWords(code: string): string {
  const words = code.replaceAll(/[_-]+/g, " ").trim() || code
  return words.charAt(0).toUpperCase() + words.slice(1)
}

/** A reading as text, for a tooltip line or a copied table: the digits and
 * the unit's print symbol, the digits alone where no unit came. */
export function readingText(
  value: string | null | undefined,
  unit?: string,
  precision?: number
): string {
  if (value === null || value === undefined) return "no answer"
  const shown = precision === undefined ? value : roundDigits(value, precision)
  return (unit && formatQuantity({ value: shown, unit })) || shown
}

/** A metric id read as words when no record names it, its last segment
 * as a code is: `health/heart_rate` is "Heart rate". */
export function metricWords(id: string): string {
  return codeWords(id.slice(id.lastIndexOf("/") + 1) || id)
}

/** What a stream's newest point says of its form: the latest read's row
 * `form`, and a `code` where its `last` answered a code. */
export interface StreamForm {
  form?: string
  code?: string
}

/** The form a metric's values are stored in: the stream's own, since every
 * value carries its form and the index reads that, else the metric record's
 * default. */
export function formOf(
  metric: MetricInfo | undefined,
  stream?: StreamForm
): Form | undefined {
  return isForm(stream?.form) ? stream.form : metric?.form
}

/** How a metric is read: coded where it lists codes or its newest value is
 * a code, else its form, a gauge where nothing says. */
export function shapeOf(
  metric: MetricInfo | undefined,
  stream?: StreamForm
): ReadingShape {
  if (metric?.codeSet?.length || stream?.code) return "coded"
  return formOf(metric, stream) ?? "gauge"
}

/** The reducers a shape may ask for, the default first, each one its form
 * takes (docs/api.md, "Reducers, by form"). A coded gauge answers time per
 * code, a coded amount its total per code. */
export function reducerOptions(shape: ReadingShape, form?: Form): string[] {
  switch (shape) {
    case "gauge":
      return ["avg", "twa", "min", "max", "first", "last", "count", "stddev"]
    case "amount":
      return ["sum", "count", "min", "max"]
    case "counter":
      return ["increase", "rate", "last"]
    case "coded":
      return form === "amount" ? ["sum", "count"] : ["timeInState", "count"]
    case "summary":
      return ["last"]
  }
}

/** The reducers a vendor summary takes (docs/api.md, "Reducers, by
 * form"). A read that may serve a bucket from a summary is held to these
 * besides its form's own. */
export const SUMMARY_REDUCERS: readonly string[] = [
  "count",
  "first",
  "last",
  "sum",
  "min",
  "max",
  "avg",
]

export const REDUCER_WORDS: Record<string, string> = {
  avg: "Average",
  twa: "Time-weighted average",
  min: "Lowest",
  max: "Highest",
  first: "First",
  last: "Latest",
  count: "Count",
  stddev: "Standard deviation",
  sum: "Total",
  increase: "Increase",
  rate: "Rate",
  timeInState: "Time in each category",
  percentile: "Percentile",
  durationIn: "Time in range",
  circularMean: "Circular average",
}

/** A summary's `statistic` in words: a reducer's word where it is one
 * ("Average"), and the two a reducer never is. */
export function statisticWords(statistic: string): string {
  if (statistic === "vendor") return "The vendor's own"
  if (statistic === "median") return "Median"
  return REDUCER_WORDS[statistic] ?? codeWords(statistic)
}

/** A reducer's name: the word, or the key of a structured one. */
export function reducerName(reducer: AggregateReducer): string {
  if (typeof reducer === "string") return reducer
  return Object.keys(reducer)[0] ?? ""
}

/** A reducer as one string, its argument included: `avg`,
 * `durationIn:95,114`, `percentile:0.05`. Two reducers with one key are the
 * same column. */
export function reducerKey(reducer: AggregateReducer): string {
  if (typeof reducer === "string") return reducer
  const [name, arg] = Object.entries(reducer)[0] ?? ["", undefined]
  return `${name}:${Array.isArray(arg) ? arg.join(",") : String(arg)}`
}

/** A reducer in words: "Average", "Time in range 95 to 114". */
export function reducerLabel(reducer: AggregateReducer): string {
  const name = reducerName(reducer)
  const word = REDUCER_WORDS[name] ?? name
  if (typeof reducer === "string") return word
  const arg = Object.values(reducer)[0]
  if (name === "durationIn" && Array.isArray(arg)) {
    return `${word} ${arg[0]} to ${arg[1]}`
  }
  if (name === "percentile" && typeof arg === "number") {
    return `${word} ${Math.round(arg * 100)}`
  }
  return word
}

// ── the latest read: streams ───────────────────────────────────────────────

/** One stream: a metric written by one source about one thing on one of
 * the source's channels, with its newest value. `metric`, `source` and
 * `about` are the group values the answer carries, record paths; `channel`
 * is the source's own word for a second measurement of one metric (a
 * WHOOP cycle's heart rate beside a workout's). */
export interface Stream extends StreamForm {
  key: string
  metric: string
  source?: string
  about?: string
  channel?: string
  /** The newest value's digits; null where it is a code or has none. */
  value: string | null
  unit?: string
  /** The newest point's time, the row's `start`. */
  at: string
  /** Where the newest point covers a window (an amount, a summary), its
   * end, the row's `end`. */
  end?: string
  /** Every point the stream holds. */
  count?: number
  coverage?: string
}

/** The key one stream is told apart by, in the URL and the table. */
export function streamKey(
  metric: string,
  source?: string,
  about?: string,
  channel?: string
): string {
  return [metric, source ?? "", about ?? "", channel ?? ""].join("\n")
}

/** A series value told apart by its channel too: a source's two channels
 * of one metric are two series. */
export function channelKey(value: string, channel?: string): string {
  return channel ? `${value}\u0001${channel}` : value
}

/** The timeline's address for one record of an observed kind: its stream,
 * and a window around its time (the trace's own period, or the 30 days up
 * to a single value), as the URL keys the timeline reads. Undefined for a
 * record naming no metric or no time. */
export function recordTimelineSearch(
  properties: Record<string, unknown>
): Record<string, string> | undefined {
  const ref = (key: string) => readReference(properties[key])?.path
  const metric = ref("metric")
  const at = typeof properties.at === "string" ? Date.parse(properties.at) : NaN
  if (!metric || Number.isNaN(at)) return undefined
  const endsAt =
    typeof properties.endsAt === "string" ? Date.parse(properties.endsAt) : NaN
  const source = ref("source")
  const about = ref("about")
  const channel =
    typeof properties.channel === "string" && properties.channel
      ? properties.channel
      : undefined
  const window: TimeRange =
    !Number.isNaN(endsAt) && endsAt > at
      ? { start: at, end: endsAt }
      : daysEnding(at, 30)
  // Record paths, the spelling the latest read's groups carry.
  return {
    ...streamSearch({ metric, source, about, channel }),
    range: "custom",
    start: new Date(window.start).toISOString(),
    end: new Date(window.end).toISOString(),
  }
}

/** The timeline's address for one stream: its metric, source and thing, as
 * the URL keys the timeline reads. */
export function streamSearch(stream: {
  metric: string
  source?: string
  about?: string
  channel?: string
}): Record<string, string> {
  const out: Record<string, string> = { metric: stream.metric }
  if (stream.source) out.source = stream.source
  if (stream.about) out.about = stream.about
  if (stream.channel) out.channel = stream.channel
  return out
}

/** Whether a stream's newest point is one window of an amount or a
 * summary, which a reader may take for a day's total. */
export function windowed(stream: Pick<Stream, "form" | "end">): boolean {
  return (
    Boolean(stream.end) &&
    (stream.form === "amount" || stream.form === "summary")
  )
}

/** The timeline's address for a stream's row in the streams table: an
 * amount opens on today's total, a day bucket over today, since its newest
 * point is one window of it. */
export function rowSearch(
  stream: Pick<Stream, "metric" | "source" | "about" | "channel" | "form">
): Record<string, string> {
  const out = streamSearch(stream)
  return stream.form === "amount"
    ? { ...out, range: "today", bucket: "day" }
    : out
}

/** The `last` value of a row, or its first value where none says `last`. */
function lastValue(row: AggregateRow): AggregateValue | undefined {
  const values = row.values ?? []
  return values.find((v) => reducerName(v.reducer) === "last") ?? values[0]
}

/** The latest read's rows as streams, newest first. A row with no metric in
 * its group answers no stream and is dropped. */
export function streamsOf(
  result: Pick<AggregateResult, "rows"> | undefined
): Stream[] {
  const out: Stream[] = []
  for (const row of result?.rows ?? []) {
    const metric = row.group?.metric
    if (!metric) continue
    const value = lastValue(row)
    out.push({
      key: streamKey(
        metric,
        row.group?.source,
        row.group?.about,
        row.group?.channel
      ),
      metric,
      source: row.group?.source,
      about: row.group?.about,
      ...(row.group?.channel ? { channel: row.group.channel } : {}),
      value: value?.value ?? null,
      unit: value?.unit,
      // A coded `last` answers its code, with `value` null.
      ...(value?.code ? { code: value.code } : {}),
      ...(row.form ? { form: row.form } : {}),
      at: row.start,
      ...(row.end && row.end !== row.start ? { end: row.end } : {}),
      count: row.count,
      coverage: row.coverage,
    })
  }
  return out.sort((a, b) => Date.parse(b.at) - Date.parse(a.at))
}
