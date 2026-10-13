/** The observations pages, without React: which kinds hold measured values
 * (those binding core's `observed` trait), what a metric's form lets a reader
 * ask for, the range and bucket a timeline reads, the aggregate request each
 * chart issues, and the rows of an answer cut into buckets and series.
 *
 * The aggregate read is docs/api.md's ("The aggregate read"), mirrored in
 * `lib/api/types.ts`; the request builder lives in `lib/api/observations.ts`.
 * Everything here is a pure function of the registry, the metric records,
 * the URL state and the answer. */

import type {
  Aggregate,
  AggregateBucket,
  AggregateGroup,
  AggregateReducer,
  AggregateResult,
  AggregateRow,
  AggregateValue,
  KindInfo,
  RecordFilter,
  SubstrateRecord,
} from "@/lib/api/types"
import { declaredProperties } from "@/lib/definition"
import { recordTitle } from "@/lib/format"
import { splitRecordPath } from "@/lib/record-path"
import { OBSERVED_TRAIT_IDENTITY, kindHasTrait } from "@/lib/sync"
import { formatQuantity } from "@/lib/units"

/** Where an aggregate reads from: raw points, vendor `summary` points, or
 * raw where a bucket is covered and the summary otherwise. */
export type AggregateFrom = "raw" | "summaries" | "prefer-raw"

/** A stream key a result may be grouped by. */
export type AggregateGroupKey =
  "source" | "about" | "code" | "metric" | "kind" | "channel"

// ── discovery ──────────────────────────────────────────────────────────────

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

/** Everyday words for each shape; the stored word sits beside them in
 * technical mode. */
export const SHAPE_WORDS: Record<ReadingShape, string> = {
  gauge: "Level",
  amount: "Amount",
  counter: "Running total",
  summary: "Vendor summary",
  coded: "Category",
}

/** What one metric record says that a reader uses: its name, its default
 * form and unit, the codes a categorical metric admits and the reducers it
 * recommends. The engine reads none of it (decision 0156); every value
 * carries its own form and unit, so these are defaults for display. */
export interface MetricInfo {
  id: string
  path: string
  name?: string
  form?: Form
  unit?: string
  /** The measure every value's unit belongs to (`mass`, `rate`). */
  measures?: string
  codeSet?: string[]
  reducers?: string[]
  /** The decimal places a reader is shown. */
  displayPrecision?: number
}

function strings(value: unknown): string[] | undefined {
  if (!Array.isArray(value)) return undefined
  const out = value.filter((v): v is string => typeof v === "string")
  return out.length ? out : undefined
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
    reducers: strings(p.reducers),
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

/** A metric id read as words when no record names it:
 * `health/heart_rate` is "Heart rate". */
export function metricWords(id: string): string {
  const last = id.slice(id.lastIndexOf("/") + 1).replaceAll(/[_-]+/g, " ")
  const words = last.trim() || id
  return words.charAt(0).toUpperCase() + words.slice(1)
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
  const ref = (key: string) => {
    const v = properties[key]
    if (typeof v === "string") return v || undefined
    if (
      v &&
      typeof v === "object" &&
      typeof (v as { ref?: unknown }).ref === "string"
    ) {
      return (v as { ref: string }).ref
    }
    return undefined
  }
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
      : (() => {
          const d = new Date(at)
          const y = d.getFullYear()
          const m = d.getMonth()
          const day = d.getDate()
          return {
            start: new Date(y, m, day - 29).getTime(),
            end: new Date(y, m, day + 1).getTime(),
          }
        })()
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
      count: row.count,
      coverage: row.coverage,
    })
  }
  return out.sort((a, b) => Date.parse(b.at) - Date.parse(a.at))
}

// ── ranges ─────────────────────────────────────────────────────────────────

export type RangePreset = "today" | "7d" | "30d" | "6m" | "1y"

export const RANGE_PRESETS: readonly { value: RangePreset; label: string }[] = [
  { value: "today", label: "Today" },
  { value: "7d", label: "Last 7 days" },
  { value: "30d", label: "Last 30 days" },
  { value: "6m", label: "Last 6 months" },
  { value: "1y", label: "Last year" },
]

export function isRangePreset(value: unknown): value is RangePreset {
  return RANGE_PRESETS.some((p) => p.value === value)
}

/** A half-open window `[start, end)` in epoch milliseconds. */
export interface TimeRange {
  start: number
  end: number
}

function startOfDay(t: number): Date {
  const d = new Date(t)
  return new Date(d.getFullYear(), d.getMonth(), d.getDate())
}

/** A preset as the local days it covers, ending with today:
 * `[the first day's midnight, tomorrow's midnight)`. */
export function presetRange(preset: RangePreset, now = Date.now()): TimeRange {
  const today = startOfDay(now)
  const y = today.getFullYear()
  const m = today.getMonth()
  const d = today.getDate()
  const end = new Date(y, m, d + 1).getTime()
  const start =
    preset === "today"
      ? today
      : preset === "7d"
        ? new Date(y, m, d - 6)
        : preset === "30d"
          ? new Date(y, m, d - 29)
          : preset === "6m"
            ? new Date(y, m - 6, d + 1)
            : new Date(y - 1, m, d + 1)
  return { start: start.getTime(), end }
}

/** Two local calendar days (`YYYY-MM-DD`) as the window from the first's
 * midnight to the midnight after the second. Undefined for a day that does
 * not parse or an end before the start. */
export function dayRange(first: string, last: string): TimeRange | undefined {
  const a = /^(\d{4})-(\d{2})-(\d{2})$/.exec(first)
  const b = /^(\d{4})-(\d{2})-(\d{2})$/.exec(last)
  if (!a || !b) return undefined
  const start = new Date(+a[1], +a[2] - 1, +a[3]).getTime()
  const end = new Date(+b[1], +b[2] - 1, +b[3] + 1).getTime()
  return end > start ? { start, end } : undefined
}

const pad = (n: number) => String(n).padStart(2, "0")

/** An instant as local wall time with its offset, the spelling the
 * aggregate's `range` reads: `2026-09-01T00:00:00+03:00`. Milliseconds are
 * kept where the instant has them (`00:00:00.123+03:00`): a record's window
 * shorter than a second would otherwise read as an empty range. */
export function isoWithOffset(t: number): string {
  const d = new Date(t)
  const off = -d.getTimezoneOffset()
  const sign = off >= 0 ? "+" : "-"
  const abs = Math.abs(off)
  const ms = d.getMilliseconds()
  return (
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}` +
    `T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}` +
    (ms ? `.${String(ms).padStart(3, "0")}` : "") +
    `${sign}${pad(Math.floor(abs / 60))}:${pad(abs % 60)}`
  )
}

/** A local day key, `YYYY-MM-DD`. */
export function dayKeyOf(t: number): string {
  const d = new Date(t)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

const MONTHS = [
  "Jan",
  "Feb",
  "Mar",
  "Apr",
  "May",
  "Jun",
  "Jul",
  "Aug",
  "Sep",
  "Oct",
  "Nov",
  "Dec",
]

function isMidnight(t: number): boolean {
  const d = new Date(t)
  return d.getHours() === 0 && d.getMinutes() === 0 && d.getSeconds() === 0
}

/** A local day in words: `Sep 3`, or `Sep 3, 2026` with its year. */
export function dayWords(t: number, withYear = false): string {
  const d = new Date(t)
  return `${MONTHS[d.getMonth()]} ${d.getDate()}${withYear ? `, ${d.getFullYear()}` : ""}`
}

/** A local clock time, `06:30`. */
export function clockWords(t: number): string {
  const d = new Date(t)
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** A window in words: whole days as `Sep 1 to Sep 30, 2026` (the end is
 * the last day it holds), anything finer as its two instants,
 * `Oct 7, 06:00 to 09:00`. */
export function rangeWords(range: TimeRange): string {
  if (isMidnight(range.start) && isMidnight(range.end)) {
    const last = new Date(range.end)
    last.setDate(last.getDate() - 1)
    const lastAt = last.getTime()
    if (dayKeyOf(range.start) === dayKeyOf(lastAt)) {
      return dayWords(range.start, true)
    }
    const sameYear = new Date(range.start).getFullYear() === last.getFullYear()
    return `${dayWords(range.start, !sameYear)} to ${dayWords(lastAt, true)}`
  }
  const sameDay = dayKeyOf(range.start) === dayKeyOf(range.end - 1)
  const from = `${dayWords(range.start, true)}, ${clockWords(range.start)}`
  const to = sameDay
    ? clockWords(range.end)
    : `${dayWords(range.end, true)}, ${clockWords(range.end)}`
  return `${from} to ${to}`
}

/** A bucket's start as an axis or table label, as fine as its size:
 * `06:30` (with the day where it changes), `Sep 3`, `Sep 2026`. */
export function bucketWords(
  t: number,
  size: BucketSize,
  previous?: number
): string {
  const d = new Date(t)
  if (size === "month") {
    return d.getMonth() === 0 || previous === undefined
      ? `${MONTHS[d.getMonth()]} ${d.getFullYear()}`
      : MONTHS[d.getMonth()]
  }
  if (size === "day" || size === "week") return dayWords(t)
  const clock = clockWords(t)
  if (previous !== undefined && dayKeyOf(previous) === dayKeyOf(t)) return clock
  return clock === "00:00" ? dayWords(t) : `${dayWords(t)}, ${clock}`
}

/** The size's boundaries inside a range, local time: every quarter hour,
 * hour, midnight, Monday or first of the month from the first one at or
 * after the range's start. The x axis labels these, whether or not a bucket
 * holds anything there. At most `limit` of them.
 *
 * Quarter hours and hours step by elapsed time, so a day the clocks go back
 * ticks its repeated hour twice. Days, weeks and months are each worked out
 * from the calendar date, so a day whose midnight the clocks skip starts at
 * its first instant and the next day at midnight again. */
export function axisTicks(
  range: TimeRange,
  size: BucketSize,
  limit = 2000
): number[] {
  const out: number[] = []
  if (size === "15m" || size === "hour") {
    const d = new Date(range.start)
    d.setSeconds(0, 0)
    const step = size === "15m" ? 15 : 60
    const minutes = Math.ceil(d.getMinutes() / step) * step
    let t = d.getTime() + (minutes - d.getMinutes()) * 60_000
    for (; t < range.end && out.length < limit; t += step * 60_000) {
      if (t >= range.start) out.push(t)
    }
    return out
  }
  const s = new Date(range.start)
  let y = s.getFullYear()
  let m = s.getMonth()
  let day = s.getDate()
  if (new Date(y, m, day).getTime() < range.start) day += 1
  if (size === "week") day += (8 - new Date(y, m, day).getDay()) % 7
  if (size === "month") {
    const first = new Date(y, m, day)
    y = first.getFullYear()
    m = first.getMonth() + (first.getDate() > 1 ? 1 : 0)
    day = 1
  }
  for (let k = 0; out.length < limit; k++) {
    const t = (
      size === "day"
        ? new Date(y, m, day + k)
        : size === "week"
          ? new Date(y, m, day + 7 * k)
          : new Date(y, m + k, 1)
    ).getTime()
    if (t >= range.end) break
    out.push(t)
  }
  return out
}

/** A bucket's window in words, for a tooltip or a table row: a day as
 * `Sep 3, 2026`, a week as `Sep 1 to Sep 7, 2026`, finer windows by their
 * clocks. */
export function windowWords(start: number, end: number): string {
  return rangeWords({ start, end })
}

// ── buckets ────────────────────────────────────────────────────────────────

export type BucketSize = "15m" | "hour" | "day" | "week" | "month"
export type BucketChoice = "auto" | BucketSize

export const BUCKET_CHOICES: readonly { value: BucketChoice; label: string }[] =
  [
    { value: "auto", label: "Automatic" },
    { value: "15m", label: "15 min" },
    { value: "hour", label: "Hour" },
    { value: "day", label: "Day" },
    { value: "week", label: "Week" },
    { value: "month", label: "Month" },
  ]

/** Each size in words, for the automatic choice's label: "Automatic: day". */
export const BUCKET_SIZE_WORDS: Record<BucketSize, string> = {
  "15m": "15 min",
  hour: "hour",
  day: "day",
  week: "week",
  month: "month",
}

const HOUR_MS = 3_600_000
const DAY_MS = 24 * HOUR_MS

/** Each size's length, a month counted as 30 days: for estimates only. */
export const BUCKET_MS: Record<BucketSize, number> = {
  "15m": 15 * 60_000,
  hour: HOUR_MS,
  day: DAY_MS,
  week: 7 * DAY_MS,
  month: 30 * DAY_MS,
}

/** The size a range reads at when nobody picked one: under 200 buckets
 * for every preset (96 for a day, 168 for a week, 30 for a month, 27 and 53
 * weeks for six months and a year). */
export function autoBucket(range: TimeRange): BucketSize {
  const span = range.end - range.start
  if (span <= DAY_MS + HOUR_MS) return "15m"
  if (span <= 8 * DAY_MS) return "hour"
  if (span <= 62 * DAY_MS) return "day"
  if (span <= 400 * DAY_MS) return "week"
  return "month"
}

export function bucketSizeOf(
  choice: BucketChoice,
  range: TimeRange
): BucketSize {
  return choice === "auto" ? autoBucket(range) : choice
}

/** About how many buckets a read makes: the range's buckets times its
 * groups, which is what the request's budget counts (`maxBuckets`). */
export function bucketEstimate(
  range: TimeRange,
  size: BucketSize,
  groups = 1
): number {
  return Math.ceil((range.end - range.start) / BUCKET_MS[size]) * groups
}

/** The default `maxBuckets` a read is held to; past it the server refuses
 * with a `422` before reading anything. */
export const MAX_BUCKETS = 1440

/** A size as the aggregate's time mode: 15 minutes is a physical window on
 * UTC instants, the rest calendar windows in the zone given, weeks from
 * Monday. With no zone, a calendar window is the recorded-local one (each
 * point's own offset). */
export function bucketOf(size: BucketSize, timezone?: string): AggregateBucket {
  const zone = timezone ? { timezone } : {}
  switch (size) {
    case "15m":
      return { every: "PT15M" }
    case "hour":
      return { calendar: "hour", ...zone }
    case "day":
      return { calendar: "day", ...zone }
    case "week":
      return { calendar: "week", weekStart: "monday", ...zone }
    case "month":
      return { calendar: "month", ...zone }
  }
}

/** The reader's time zone, for calendar buckets: the runtime's, else UTC.
 * Never none, since a calendar bucket without a zone is the recorded-local
 * calendar while the range stays in the browser's offset. */
export function readerTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC"
  } catch {
    return "UTC"
  }
}

// ── the timeline's requests ────────────────────────────────────────────────

export type GroupChoice = "metric" | "source" | "about" | "code"

export const GROUP_CHOICES: readonly { value: GroupChoice; label: string }[] = [
  { value: "metric", label: "Metric" },
  { value: "source", label: "Source" },
  { value: "about", label: "About" },
  { value: "code", label: "Category" },
]

export const FROM_CHOICES: readonly { value: AggregateFrom; label: string }[] =
  [
    { value: "raw", label: "Raw" },
    { value: "summaries", label: "Summaries" },
    { value: "prefer-raw", label: "Prefer raw" },
  ]

/** One aggregate read: the records filter and the `aggregate` parameter,
 * which `lib/api/observations.ts` encodes onto the records route. */
export interface AggregateRead {
  filter?: RecordFilter
  aggregate: Aggregate
}

/** What one chart of the timeline asks for. */
export interface LaneSpec {
  /** The metric record's path. */
  metricPath: string
  shape: ReadingShape
  /** The reducer the reader picked, a word from `reducerOptions`. */
  reducer: string
  /** The output unit; absent keeps the stored one. */
  unit?: string
  from: AggregateFrom
  range: TimeRange
  bucket: AggregateBucket
  group: GroupChoice
  /** Source record paths the read is narrowed to; empty for every source. */
  sources: string[]
  /** Sources in the reader's order, for the priority policy: record paths
   * or ids, which `sources.order` takes alike. */
  sourceOrder: string[]
  /** The read selects a stream naming no source too. `sources.order` cannot
   * name it, so the priority policy ranks it after every named source. */
  sourceless?: boolean
  /** `about` values the read is narrowed to; empty for everything. */
  about: string[]
  /** Channels the read is narrowed to; absent or empty for every one. */
  channels?: string[]
}

/** The reducers a lane asks for: the reader's, then (on a level) the
 * lowest and highest, which the chart draws as a band. `timeInState` goes
 * bare, which answers the time each code holds. */
export function laneReducers(spec: LaneSpec): AggregateReducer[] {
  const picked: AggregateReducer = spec.reducer
  if (spec.shape !== "gauge") return [picked]
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

/** The aggregate read one lane issues. Grouped by anything but source, two
 * or more sources (a stream naming none counts as one) are resolved by
 * priority in the reader's order, so one line is one thing: the default
 * policy answers a row per source, and a series draws one row per bucket.
 * Grouped by source, each keeps its own rows. */
export function laneRead(spec: LaneSpec): AggregateRead {
  const groupKey = laneGroupKey(spec)
  const ranked = spec.sourceOrder.length + (spec.sourceless ? 1 : 0)
  const properties: NonNullable<RecordFilter["properties"]> = {}
  if (spec.sources.length) properties.source = { in: spec.sources }
  if (spec.about.length) properties.about = { in: spec.about }
  if (spec.channels?.length) properties.channel = { in: spec.channels }
  const aggregate: Aggregate = {
    metric: { ref: spec.metricPath },
    from: spec.from,
    range: {
      gte: isoWithOffset(spec.range.start),
      lt: isoWithOffset(spec.range.end),
    },
    bucket: spec.bucket,
    ...(groupKey
      ? { groupBy: groupKey === "source" ? ["source", "channel"] : [groupKey] }
      : {}),
    reducers: laneReducers(spec),
    ...(groupKey !== "source" && ranked > 1
      ? { sources: { policy: "priority", order: spec.sourceOrder } }
      : {}),
    ...(spec.unit ? { unit: spec.unit } : {}),
  }
  return {
    filter: {
      implements: OBSERVED_TRAIT_IDENTITY,
      ...(Object.keys(properties).length ? { properties } : {}),
    },
    aggregate,
  }
}

/** What a page narrows its streams to: record paths of metrics, sources
 * and things observed. An empty list narrows nothing. */
export interface StreamNarrowing {
  metrics?: readonly string[]
  sources?: readonly string[]
  abouts?: readonly string[]
}

/** The observed filter, narrowed server-side by `properties`. */
function narrowedFilter(narrow: StreamNarrowing): RecordFilter {
  const properties: NonNullable<RecordFilter["properties"]> = {}
  if (narrow.metrics?.length) properties.metric = { in: [...narrow.metrics] }
  if (narrow.sources?.length) properties.source = { in: [...narrow.sources] }
  if (narrow.abouts?.length) properties.about = { in: [...narrow.abouts] }
  return {
    implements: OBSERVED_TRAIT_IDENTITY,
    ...(Object.keys(properties).length ? { properties } : {}),
  }
}

/** The most groups one read may ask for (`budget.maxGroups`'s cap). */
export const MAX_GROUPS_CAP = 2000

/** The latest read: one row per stream, from the newest point of each,
 * narrowed to the given metrics, sources and things, at the group cap. A
 * read makes at most that many groups, which is why discovery asks for its
 * streams a few metrics at a time (`discoveryChunks`). */
export function latestRead(narrow: StreamNarrowing = {}): AggregateRead {
  return {
    filter: narrowedFilter(narrow),
    aggregate: {
      groupBy: ["metric", "source", "about", "channel"],
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
const MAX_STREAM_READS = 32

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

// ── the answer, cut into buckets and series ────────────────────────────────

/** A decimal string as a number to plot; null for no answer. */
export function numberOf(value: string | null | undefined): number | null {
  if (value === null || value === undefined || value === "") return null
  const n = Number(value)
  return Number.isFinite(n) ? n : null
}

/** One bucket of the answer: its window and the rows that fell in it. */
export interface Bucket {
  start: number
  end: number
  startIso: string
  endIso: string
  rows: AggregateRow[]
}

/** One series: the rows one group value answers, a point per bucket. */
export interface Series {
  key: string
  /** The group value, or undefined for the one series of an ungrouped lane. */
  value?: string
  /** The source's channel, where the answer is grouped by one. */
  channel?: string
  points: SeriesPoint[]
  /** Rows of this series that met a bucket already holding one, which the
   * series does not draw. A read the console plans answers none. */
  dropped: number
}

export interface SeriesPoint {
  bucket: number
  row?: AggregateRow
  /** The picked reducer's value. */
  value: number | null
  /** The band: the lowest and highest, on a level. */
  low: number | null
  high: number | null
}

/** A row's value for one reducer, by key. A rolling value (one carrying
 * `rolling` and `of`) is never the reducer's own: the timeline asks for
 * none, and one would otherwise shadow the column it rolls over.
 *
 * A bare `timeInState` is asked for once and answered once per state, each
 * value naming its state (`{"timeInState": ["rem"]}`, `code: "rem"`), so the
 * bare key resolves to the value for the row's own code. */
export function rowValue(
  row: AggregateRow | undefined,
  key: string
): AggregateValue | undefined {
  const own = (row?.values ?? []).filter((v) => v.rolling === undefined)
  const exact = own.find((v) => reducerKey(v.reducer) === key)
  if (exact || key !== "timeInState") return exact
  const code = row?.group?.code
  if (code === undefined) return undefined
  return own.find(
    (v) =>
      reducerName(v.reducer) === "timeInState" &&
      reducerKey(v.reducer) === `timeInState:${code}`
  )
}

/** The empty buckets between windows that do not meet: an answer without
 * `fill` omits the buckets nothing fell in, and a line drawn from one
 * returned window to the next would bridge the time between them. Each
 * stretch of missing time becomes one bucket holding no rows, so every
 * series breaks there. */
function withGaps(buckets: Bucket[]): Bucket[] {
  const out: Bucket[] = []
  for (const bucket of buckets) {
    const previous = out.at(-1)
    if (previous && bucket.start > previous.end) {
      out.push({
        start: previous.end,
        end: bucket.start,
        startIso: previous.endIso,
        endIso: bucket.startIso,
        rows: [],
      })
    }
    out.push(bucket)
  }
  return out
}

/** The windows of one or more answers as one bucket axis: every distinct
 * window, oldest first, with an empty bucket standing in for each stretch
 * no answer covered. Several reads over one range and one bucket share
 * their windows, so a chart that composes them draws one axis; each
 * bucket holds every row that fell in it, from any answer. */
export function bucketsOf(
  answers: readonly (readonly AggregateRow[])[]
): Bucket[] {
  const byWindow = new Map<string, Bucket>()
  for (const rows of answers) {
    for (const row of rows) {
      const start = Date.parse(row.start)
      const end = Date.parse(row.end)
      const id = `${start}\n${end}`
      let bucket = byWindow.get(id)
      if (!bucket) {
        bucket = { start, end, startIso: row.start, endIso: row.end, rows: [] }
        byWindow.set(id, bucket)
      }
      bucket.rows.push(row)
    }
  }
  return withGaps([...byWindow.values()].sort((a, b) => a.start - b.start))
}

/** One answer's series on a bucket axis (`bucketsOf`): one per value of the
 * group key, in first-seen order, each holding one point per bucket. A
 * bucket the answer has no row in is a point with no value, so a line
 * breaks there. */
export function seriesOn(
  buckets: readonly Bucket[],
  rows: readonly AggregateRow[],
  pickedKey: string,
  groupKey?: AggregateGroupKey
): Series[] {
  const at = new Map<string, number>()
  buckets.forEach((b, i) => at.set(`${b.start}\n${b.end}`, i))
  const keys: string[] = []
  const channels = new Map<string, string | undefined>()
  const byKey = new Map<string, Map<number, AggregateRow>>()
  const dropped = new Map<string, number>()
  for (const row of rows) {
    const value = groupValue(row.group, groupKey) ?? ""
    const channel = groupKey ? row.group?.channel || undefined : undefined
    const key = channelKey(value, channel)
    if (!byKey.has(key)) {
      keys.push(key)
      channels.set(key, channel)
      byKey.set(key, new Map())
    }
    const i = at.get(`${Date.parse(row.start)}\n${Date.parse(row.end)}`)
    const held = byKey.get(key)!
    if (i === undefined) continue
    if (held.has(i)) dropped.set(key, (dropped.get(key) ?? 0) + 1)
    else held.set(i, row)
  }
  return keys.map((key) => {
    const held = byKey.get(key)!
    const channel = channels.get(key)
    const value = channel ? key.slice(0, key.indexOf("\u0001")) : key
    return {
      key,
      value: groupKey ? value || undefined : undefined,
      ...(channel ? { channel } : {}),
      dropped: dropped.get(key) ?? 0,
      points: buckets.map((_, i) => {
        const row = held.get(i)
        const off = row?.unavailable === true
        return {
          bucket: i,
          row,
          value: off ? null : numberOf(rowValue(row, pickedKey)?.value),
          low: off ? null : numberOf(rowValue(row, "min")?.value),
          high: off ? null : numberOf(rowValue(row, "max")?.value),
        }
      }),
    }
  })
}

/** The rows as buckets (distinct windows, oldest first, with an empty one
 * standing in for each stretch the answer skipped) and series (one per
 * value of the group key, in first-seen order), every series holding one
 * point per bucket. */
export function seriesOf(
  rows: readonly AggregateRow[],
  pickedKey: string,
  groupKey?: AggregateGroupKey
): { buckets: Bucket[]; series: Series[] } {
  const buckets = bucketsOf([rows])
  return { buckets, series: seriesOn(buckets, rows, pickedKey, groupKey) }
}

function groupValue(
  group: AggregateGroup | undefined,
  key: AggregateGroupKey | undefined
): string | undefined {
  return key ? group?.[key] : undefined
}

/** The unit an answer is in: the picked reducer's, else the result's. */
export function answerUnit(
  result: AggregateResult | undefined,
  pickedKey: string
): string | undefined {
  for (const row of result?.rows ?? []) {
    const unit = rowValue(row, pickedKey)?.unit
    if (unit) return unit
  }
  return result?.unit
}

/** The sources that fed a row and have a name: the server answers `""`
 * for a stream that names no source. */
export function namedSources(row: AggregateRow): string[] {
  return (row.sources ?? []).filter(Boolean)
}

/** The reducer columns an answer carries, in first-seen order. */
export function answerReducers(
  rows: readonly AggregateRow[]
): AggregateReducer[] {
  const seen = new Map<string, AggregateReducer>()
  for (const row of rows) {
    for (const v of row.values ?? []) {
      if (v.rolling !== undefined) continue
      const key = reducerKey(v.reducer)
      if (!seen.has(key)) seen.set(key, v.reducer)
    }
  }
  return [...seen.values()]
}

/** An answer as tab-separated text, the rollup table's copy: a header
 * line, then one line per row with the wire's own values (instants,
 * digits, codes and durations as served). */
export function rollupText(
  rows: readonly AggregateRow[],
  reducers: readonly AggregateReducer[],
  groupKey?: AggregateGroupKey
): string {
  const channels = rows.some((r) => r.group?.channel)
  const head = [
    "start",
    "end",
    ...(groupKey ? [groupKey] : []),
    ...(channels ? ["channel"] : []),
    ...reducers.map(reducerKey),
    "count",
    "coverage",
    "sources",
    "from",
    "synthetic",
    "unavailable",
  ]
  const lines = rows.map((row) => [
    row.start,
    row.end,
    ...(groupKey ? [row.group?.[groupKey] ?? ""] : []),
    ...(channels ? [row.group?.channel ?? ""] : []),
    ...reducers.map((r) => {
      const v = rowValue(row, reducerKey(r))
      if (v?.value == null) return v?.code ?? ""
      return `${v.value}${v.unit ? ` ${v.unit}` : ""}`
    }),
    row.count === undefined ? "" : String(row.count),
    row.coverage ?? "",
    (row.sources ?? []).join(","),
    row.from ?? "",
    row.synthetic ? "true" : "",
    row.unavailable ? "true" : "",
  ])
  return [head, ...lines].map((l) => l.join("\t")).join("\n")
}

// ── durations ──────────────────────────────────────────────────────────────

const DURATION =
  /^P(?:(\d+(?:\.\d+)?)W)?(?:(\d+(?:\.\d+)?)D)?(?:T(?:(\d+(?:\.\d+)?)H)?(?:(\d+(?:\.\d+)?)M)?(?:(\d+(?:\.\d+)?)S)?)?$/

/** An ISO 8601 duration of weeks, days, hours, minutes and seconds in
 * milliseconds; undefined for anything else (years and months have no fixed
 * length). */
export function durationMs(text: string | undefined): number | undefined {
  if (!text) return undefined
  const m = DURATION.exec(text)
  if (!m || text === "P" || text.endsWith("T")) return undefined
  const [w, d, h, min, s] = m.slice(1).map((x) => (x ? Number(x) : 0))
  return Math.round((((w * 7 + d) * 24 + h) * 60 + min) * 60_000 + s * 1000)
}

/** A length of time in a few words: `6d 22h 10m`, `59m 58s`, `0s`. */
export function durationWords(ms: number): string {
  const parts: string[] = []
  let rest = Math.round(ms / 1000)
  const days = Math.floor(rest / 86_400)
  rest -= days * 86_400
  const hours = Math.floor(rest / 3600)
  rest -= hours * 3600
  const minutes = Math.floor(rest / 60)
  const seconds = rest - minutes * 60
  if (days) parts.push(`${days}d`)
  if (hours) parts.push(`${hours}h`)
  if (minutes) parts.push(`${minutes}m`)
  if (seconds && !days) parts.push(`${seconds}s`)
  return parts.length ? parts.slice(0, 3).join(" ") : "0s"
}

/** A stored duration in words, or the stored text where it does not parse. */
export function coverageWords(text: string | undefined): string {
  const ms = durationMs(text)
  return ms === undefined ? (text ?? "") : durationWords(ms)
}

// ── a trace's own values ───────────────────────────────────────────────────

/** One value of a trace, placed in time. */
export interface TracePoint {
  at: number
  value: string | null
  code?: string | null
}

/** A trace's values in time order: each at `at` plus its offset in
 * milliseconds, or plus `every` times its position. `values` and `codes`
 * line up with the offsets; whichever the trace holds is read. A trace
 * whose spacing does not parse answers nothing. */
export function tracePoints(properties: Record<string, unknown>): TracePoint[] {
  const at = typeof properties.at === "string" ? Date.parse(properties.at) : NaN
  if (Number.isNaN(at)) return []
  const values = Array.isArray(properties.values) ? properties.values : null
  const codes = Array.isArray(properties.codes) ? properties.codes : null
  const length = values?.length ?? codes?.length ?? 0
  const offsets = Array.isArray(properties.offsets)
    ? properties.offsets.map(Number)
    : null
  const every = durationMs(
    typeof properties.every === "string" ? properties.every : undefined
  )
  if (!offsets && every === undefined) return []
  const out: TracePoint[] = []
  for (let i = 0; i < length; i++) {
    const offset = offsets ? offsets[i] : every! * i
    if (!Number.isFinite(offset)) continue
    const raw = values?.[i]
    out.push({
      at: at + offset,
      value:
        typeof raw === "string"
          ? raw
          : typeof raw === "number"
            ? `${raw}`
            : null,
      ...(codes
        ? { code: typeof codes[i] === "string" ? codes[i] : null }
        : {}),
    })
  }
  return out
}

/** A trace's points with a break (`null`) wherever two neighbours sit
 * further apart than the trace's `hold`: past it a level is not known
 * (decision 0158), so a line must not slope across the gap. No hold, no
 * breaks. */
export function breakAtGaps(
  points: readonly TracePoint[],
  holdMs: number | undefined
): (TracePoint | null)[] {
  if (holdMs === undefined) return [...points]
  const out: (TracePoint | null)[] = []
  points.forEach((p, i) => {
    if (i > 0 && p.at - points[i - 1].at > holdMs) out.push(null)
    out.push(p)
  })
  return out
}

/** An instant to the second, for a value inside a trace: `Oct 7, 06:30:01`. */
export function secondWords(t: number): string {
  const d = new Date(t)
  return `${dayWords(t)}, ${clockWords(t)}:${pad(d.getSeconds())}`
}

/** A trace's managed `stats`, read defensively: the engine writes them. */
export interface TraceStats {
  count?: number
  min?: string
  max?: string
  mean?: number
  m2?: number
  first?: { at: string; value: string }
  last?: { at: string; value: string }
  coverage?: string
}

function point(raw: unknown): { at: string; value: string } | undefined {
  if (!raw || typeof raw !== "object") return undefined
  const p = raw as Record<string, unknown>
  return typeof p.at === "string" &&
    (typeof p.value === "string" || typeof p.value === "number")
    ? { at: p.at, value: String(p.value) }
    : undefined
}

export function traceStats(raw: unknown): TraceStats | undefined {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return undefined
  const s = raw as Record<string, unknown>
  const num = (v: unknown) =>
    typeof v === "number" && Number.isFinite(v) ? v : undefined
  const dec = (v: unknown) =>
    typeof v === "string" ? v : typeof v === "number" ? String(v) : undefined
  return {
    count: num(s.count),
    min: dec(s.min),
    max: dec(s.max),
    mean: num(s.mean),
    m2: num(s.m2),
    first: point(s.first),
    last: point(s.last),
    coverage: typeof s.coverage === "string" ? s.coverage : undefined,
  }
}

/** The standard deviation `m2` holds: the variance is `m2 / count`
 * (docs/observations.md, "Stats"), the population's. */
export function stddevOf(stats: TraceStats): number | undefined {
  if (stats.m2 === undefined || !stats.count) return undefined
  return Math.sqrt(stats.m2 / stats.count)
}

/** A plotted number as digits, at most `digits` after the point, trailing
 * zeros dropped: `131.6`, `0.25`, `1812`. */
export function plainNumber(n: number, digits = 2): string {
  return Number(n.toFixed(digits)).toString()
}

// ── per-metric choices in the address ──────────────────────────────────────

/** Per-metric choices (a reducer, a unit) as URL tokens `<metric>~<value>`,
 * the metric URI-encoded since an id may hold a `~`. */
export function readChoices(
  tokens: readonly string[] | null
): Map<string, string> {
  const out = new Map<string, string>()
  for (const token of tokens ?? []) {
    const at = token.lastIndexOf("~")
    if (at <= 0) continue
    try {
      out.set(decodeURIComponent(token.slice(0, at)), token.slice(at + 1))
    } catch {
      // A hand-edited token that does not decode names no metric.
    }
  }
  return out
}

/** The tokens with one metric's choice set, or cleared with "". */
export function writeChoice(
  tokens: readonly string[] | null,
  metric: string,
  value: string
): string[] {
  const choices = readChoices(tokens)
  if (value) choices.set(metric, value)
  else choices.delete(metric)
  return [...choices].map(([m, v]) => `${encodeURIComponent(m)}~${v}`)
}
