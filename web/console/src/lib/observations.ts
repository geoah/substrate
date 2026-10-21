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
  AggregateFrom,
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
import {
  ADDITIVE_STATISTICS,
  DEFAULT_ROLLUPS,
  FORM_REDUCERS,
  type ReducerTable,
} from "@/lib/aggregate-rules"
import { METRIC_TRAIT, OBSERVED_TRAIT } from "@/lib/core-kinds"
import { FORM_WORDS } from "@/lib/observed-form"
import { listWords } from "@/lib/tools"
import { kindHasTrait } from "@/lib/traits"
import {
  convertUnit,
  displayDigits,
  displayQuantity,
  formatQuantity,
  groupDigits,
  isQuantity,
  roundDigits,
  timeWords,
  withSymbol,
} from "@/lib/units"

// ── discovery ──────────────────────────────────────────────────────────────

/** `single` holds one value per record (an observation), `packed` many over
 * a fixed period (a trace). */
export type ObservedVariant = "single" | "packed"

/** The kinds binding the `observed` trait, by reference. Never a fixed
 * authority: an imported sample lands under the repository's own. */
export function observedKinds(kinds: readonly KindInfo[]): KindInfo[] {
  return kinds
    .filter((k) => kindHasTrait(k, OBSERVED_TRAIT))
    .sort((a, b) => a.identity.localeCompare(b.identity))
}

/** Which layout a kind binding the trait uses: a kind declaring `values`
 * packs them. Undefined for a kind that does not bind the trait. */
export function observedVariant(
  kind: KindInfo | undefined
): ObservedVariant | undefined {
  if (!kind || !kindHasTrait(kind, OBSERVED_TRAIT)) return undefined
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
      "interpretation",
      "referenceRanges",
      "measure",
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
      "utcOffset",
      "measure",
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

/** The kinds binding core's `metric` trait, by reference. */
export function metricKinds(kinds: readonly KindInfo[]): KindInfo[] {
  return kinds
    .filter((k) => kindHasTrait(k, METRIC_TRAIT))
    .sort((a, b) => a.identity.localeCompare(b.identity))
}

/** The kind a repository's metric records are of: the observed kinds'
 * `metric` pin where it names a kind binding core's `metric` trait, else
 * the one kind binding it. An observed kind may pin the trait itself, which
 * names no kind; with several metric kinds and no kind pin there is no one
 * answer, and it is undefined. */
export function metricKindOf(
  kinds: readonly KindInfo[],
  pins: Pick<ObservedPins, "metric">
): string | undefined {
  const bound = metricKinds(kinds)
  if (pins.metric && bound.some((k) => k.identity === pins.metric)) {
    return pins.metric
  }
  return bound.length === 1 ? bound[0].identity : undefined
}

/** The value a narrowing holds for the streams that name no source, no
 * thing (the owner's) or no channel. Absent is not every value: a link to
 * one stream names the key it leaves out as this, so the read takes that
 * stream alone and not every source's or everyone's. It holds a NUL, which
 * no stored string can (Postgres refuses one in text and in jsonb), so a
 * channel spelled any way at all is never taken for it. The address spells
 * it `NONE_TOKEN` (`addressValue`). */
export const NONE = "\u0000none"

/** How the timeline's address spells `NONE`. */
export const NONE_TOKEN = "~none"

/** A stream key's value (a source, a thing or a channel) as the timeline's
 * address spells it: `NONE` as `~none`, and a value starting with `~`
 * behind one more `~`, so a channel literally named `~none` stays that
 * channel. */
export function addressValue(value: string): string {
  if (value === NONE) return NONE_TOKEN
  return value.startsWith("~") ? `~${value}` : value
}

/** The value one token of the address names, `addressValue` undone. */
export function valueOfAddress(token: string): string {
  if (token === NONE_TOKEN) return NONE
  return token.startsWith("~~") ? token.slice(1) : token
}

/** A group value as a record path: as given where it already is one, else
 * the id under the kind its reference is pinned to. Undefined where neither
 * says what kind the record is. `NONE` is no record and stays as it is. */
export function refPathOf(
  value: string | undefined,
  pin: string | undefined
): string | undefined {
  if (!value) return undefined
  if (value === NONE) return NONE
  if (splitRecordPath(value)) return value
  return pin ? `${pin}/${value}` : undefined
}

/** Whether two spellings name one record: equal, or one is a record path
 * whose id is the other (the answer names a record by id, a stored
 * reference by its path). */
export function sameRef(a: string | undefined, b: string | undefined): boolean {
  if (!a || !b) return false
  if (a === NONE || b === NONE) return a === b
  if (a === b) return true
  return splitRecordPath(a)?.id === b || splitRecordPath(b)?.id === a
}

/** Whether a stream's value for a key (its source, thing or channel, absent
 * where it names none) is one a narrowing chose: `NONE` chooses the absent
 * value, anything else a record by path or id. */
export function chosenValue(
  value: string | undefined,
  chosen: string
): boolean {
  return chosen === NONE ? !value : sameRef(value, chosen)
}

/** Whether two spellings name one record where a bare id is read under
 * `pin` alone: both resolve to a record path (`refPathOf`) and the paths
 * are equal. Unlike `sameRef`, an id never matches a path of another kind,
 * so a metric is told apart from another metric kind's record holding its
 * id. */
export function samePinned(
  a: string | undefined,
  b: string | undefined,
  pin: string | undefined
): boolean {
  if (!a || !b) return false
  return (refPathOf(a, pin) ?? a) === (refPathOf(b, pin) ?? b)
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
  /** Where a read by the day or longer places a value's span: `end` lands
   * a night on the day it ends. Absent is the start. */
  bucketPlace?: "start" | "end"
}

/** A metric record read as `MetricInfo`. A `codeSet` entry may be a bare
 * code or an object naming one under `code`. */
export function metricInfoOf(record: SubstrateRecord): MetricInfo {
  const p = record.properties ?? {}
  const entries: unknown[] | undefined = Array.isArray(p.codeSet)
    ? p.codeSet
    : undefined
  const codeSet = entries?.flatMap((c) =>
    typeof c === "string"
      ? [c]
      : c && typeof c === "object" && "code" in c && typeof c.code === "string"
        ? [c.code]
        : []
  )
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
    ...(p.bucketPlace === "start" || p.bucketPlace === "end"
      ? { bucketPlace: p.bucketPlace }
      : {}),
    ...(typeof p.displayPrecision === "number" &&
    Number.isInteger(p.displayPrecision)
      ? { displayPrecision: p.displayPrecision }
      : {}),
  }
}

/** The tiers a metric's stored `rollups` keeps, in words. */
export function rollupWords(value: unknown): string {
  if (!Array.isArray(value)) {
    return `${listWords([...DEFAULT_ROLLUPS])}, the default`
  }
  const tiers = value.filter((t): t is string => typeof t === "string")
  return tiers.length
    ? listWords(tiers)
    : "none: every read of it takes raw points"
}

/** The places a computed value is shown to where the metric names none. */
export const DEFAULT_PRECISION = 2

/** Decimal digits cut to places for display: `lib/units.ts` holds the
 * rule, since every quantity on a page reads through it. */
export { roundDigits }

/** A category's code as words: `rem` is "Rem", `not-detected` "Not
 * detected". */
export function codeWords(code: string): string {
  const words = code.replaceAll(/[_-]+/g, " ").trim() || code
  return words.charAt(0).toUpperCase() + words.slice(1)
}

/** A reading as text, for a tooltip line or a table cell: the digits in
 * groups of three and the unit's print symbol, the digits alone where no
 * unit came, a time of a minute or more in words (`4h 42m 36s`), and "no
 * reading" where there is none. A bound keeps its comparator in front
 * (`<0.01 ng/mL`): the true value lies beyond the digits. */
export function readingText(
  value: string | null | undefined,
  unit?: string,
  precision?: number,
  comparator?: string
): string {
  if (value === null || value === undefined) return "no reading"
  const bound = comparator ?? ""
  const time = unit ? timeWords(value, unit) : undefined
  if (time) return `${bound}${time}`
  const shown = precision === undefined ? value : roundDigits(value, precision)
  const digits = `${bound}${groupDigits(shown)}`
  return unit && formatQuantity({ value: shown, unit })
    ? withSymbol(digits, unit)
    : digits
}

/** Why a row's reducer answers no number although the row holds values:
 * some were written as a bound (`<0.01`), and a reducer that needs every
 * number (a total, an average, a spread) cannot say one. Undefined where
 * that is not why. */
export function boundWords(
  row: Pick<AggregateRow, "bounded" | "count">,
  value: Pick<AggregateValue, "value" | "code"> | undefined
): string | undefined {
  if (!row.bounded || value?.value != null || value?.code) return undefined
  return `${row.bounded.toLocaleString("en")} of ${row.count.toLocaleString("en")} values ${row.bounded === 1 ? "was" : "were"} written as a bound (such as <0.01), so this has no number`
}

/** HL7 v3 ObservationInterpretation codes in words, after that code
 * system's own displays, for a flag that carries no `display`. */
const INTERPRETATION_WORDS: Record<string, string> = {
  N: "normal",
  A: "abnormal",
  AA: "critical abnormal",
  H: "high",
  HH: "critical high",
  HU: "significantly high",
  L: "low",
  LL: "critical low",
  LU: "significantly low",
  POS: "positive",
  NEG: "negative",
  DET: "detected",
  ND: "not detected",
  IND: "indeterminate",
}

const INTERPRETATION_SYSTEM =
  "http://terminology.hl7.org/CodeSystem/v3-ObservationInterpretation"

/** One flag in words: its `display`, lowered where it is in sentence case
 * (`Normal`, `Above range`) and kept as written otherwise (`HIGH`, `pH
 * low`); else its code, in words where it is an HL7 v3 code and the flag
 * names that system or none; else the code as written. */
function oneFlagWords(flag: Record<string, unknown>): string | undefined {
  const display = text(flag.display)
  if (display) {
    return /^[A-Z][^A-Z]*$/.test(display)
      ? display.charAt(0).toLowerCase() + display.slice(1)
      : display
  }
  const code = text(flag.code)
  if (!code) return undefined
  const system = text(flag.system)
  const hl7 = !system || system === INTERPRETATION_SYSTEM
  return hl7 && Object.hasOwn(INTERPRETATION_WORDS, code)
    ? INTERPRETATION_WORDS[code]
    : code
}

/** A result's flag and reference range as a reader says them beside its
 * value: `normal (under 120 mg/dL)`, `high (60 to 120 mg/dL)`, `range over
 * 40 mg/dL`. The lab's own words for the flag (`flagWords`) and the first
 * range's bounds; undefined where the record has neither. */
export function judgementWords(
  interpretation: unknown,
  referenceRanges: unknown
): string | undefined {
  const range = firstRange(referenceRanges)
  const bounds = range ? rangeWords(range) : undefined
  const flag = flagWords(interpretation) ?? ""
  if (flag && bounds) return `${flag} (${bounds})`
  if (flag) return flag
  return bounds ? `range ${bounds}` : undefined
}

/** The reference range a result is read against: the first the lab
 * printed. The one place that would choose by `appliesTo` and `age`. */
export function firstRange(
  referenceRanges: unknown
): Record<string, unknown> | undefined {
  const ranges: unknown[] = Array.isArray(referenceRanges)
    ? referenceRanges
    : []
  return isBag(ranges[0]) ? ranges[0] : undefined
}

/** How a reference range is drawn and worded: a lab's normal range is
 * `good`; a cut-off past which a value is abnormal is `poor`. */
export type RangeTone = "good" | "fair" | "poor" | "none"

const RANGE_TONES: readonly RangeTone[] = ["good", "fair", "poor", "none"]

/** A numeric result as a chart or a bar draws it. */
export interface ResultReading {
  value: number
  unit?: string
  /** A bound such as `<` in `<0.5 ng/mL`: the true value lies past it. */
  comparator?: string
  /** The tone the lab's flags give the value (`interpretationTone`). */
  tone: "good" | "poor" | "none"
  /** The first reference range, its bounds in the value's unit; absent
   * where the result has none, or a bound is in a unit the value's does
   * not convert to (mg/dL against mmol/L). */
  range?: { low?: number; high?: number; label: string; tone: RangeTone }
}

/** A result's numeric value and its first reference range, read against
 * one value-against-range rule wherever a reading is drawn. With `unit`,
 * the value and its bounds are converted into it within their measure;
 * undefined where the result holds no number, or one that does not convert
 * into `unit`. A range's `tone` is absent on a lab's normal range, which
 * reads as `good`. */
export function resultReading(
  p: Record<string, unknown>,
  unit?: string
): ResultReading | undefined {
  const v = isBag(p.value) ? p.value : undefined
  if (!v || (typeof v.value !== "string" && typeof v.value !== "number")) {
    return undefined
  }
  const raw = Number(v.value)
  if (!Number.isFinite(raw)) return undefined
  const own = text(v.unit)
  const measure = text(p.measure)
  const into = unit ?? own
  const value =
    own === into
      ? raw
      : own && into
        ? convertUnit(raw, own, into, measure)
        : undefined
  if (value === undefined) return undefined
  const comparator = text(v.comparator)
  return {
    value,
    ...(into ? { unit: into } : {}),
    ...(comparator ? { comparator } : {}),
    tone: interpretationTone(p.interpretation),
    ...rangeIn(firstRange(p.referenceRanges), into, measure),
  }
}

/** The bar a result's value draws against its own first reference range
 * (`resultReading`): only where the value is a plain number, not a bound
 * such as `<0.5`, and every bound the range gives converts into the
 * value's unit (another measure is said in words, never drawn). The range
 * wears its own tone, a lab's normal range the good one, and a one-sided
 * range of values that are never negative starts at zero, so a value
 * under a high bound alone sits where it is, not at the padded end. */
export function resultBar(p: Record<string, unknown>):
  | {
      value: number
      unit?: string
      band: { low?: number; high?: number; label: string; tone: RangeTone }
      tone: "good" | "poor" | "none"
      min?: number
    }
  | undefined {
  const reading = resultReading(p)
  if (!reading?.range || reading.comparator) return undefined
  const { value, unit, range, tone } = reading
  // Only a one-sided range ("under 3") starts at zero: a two-sided one
  // keeps its band wide enough to read, its bounds clear of each other.
  const oneSided = (range.low === undefined) !== (range.high === undefined)
  const nonNegative = [value, range.low, range.high].every(
    (v) => v === undefined || v >= 0
  )
  return {
    value,
    unit,
    band: {
      ...(range.low !== undefined ? { low: range.low } : {}),
      ...(range.high !== undefined ? { high: range.high } : {}),
      label: range.label,
      tone: range.tone,
    },
    tone,
    ...(oneSided && nonNegative ? { min: 0 } : {}),
  }
}

/** A range's bounds in `unit`, where every bound it gives converts. */
function rangeIn(
  range: Record<string, unknown> | undefined,
  unit: string | undefined,
  measure: string | undefined
): Pick<ResultReading, "range"> {
  if (!range) return {}
  const bound = (q: unknown): number | null | undefined => {
    if (q === undefined || q === null) return undefined
    // A bound written as a bound (`<5`) has no place on a scale.
    if (!isBag(q) || q.comparator) return null
    if (typeof q.value !== "string" && typeof q.value !== "number") return null
    const n = Number(q.value)
    const from = text(q.unit)
    if (!Number.isFinite(n)) return null
    if (from === unit) return n
    if (!from || !unit) return null
    return convertUnit(n, from, unit, measure) ?? null
  }
  const low = bound(range.low)
  const high = bound(range.high)
  if (low === null || high === null) return {}
  if (low === undefined && high === undefined) return {}
  const tone = RANGE_TONES.includes(range.tone as RangeTone)
    ? (range.tone as RangeTone)
    : "good"
  return {
    range: {
      ...(low !== undefined ? { low } : {}),
      ...(high !== undefined ? { high } : {}),
      label: text(range.text) ?? "reference range",
      tone,
    },
  }
}

/** A result's flags in words, joined (`high, critical high`): each flag's
 * `display`, else its code (`oneFlagWords`). Undefined where it has none. */
export function flagWords(interpretation: unknown): string | undefined {
  const words = (Array.isArray(interpretation) ? interpretation : []).flatMap(
    (f) => {
      const word = isBag(f) ? oneFlagWords(f) : undefined
      return word ? [word] : []
    }
  )
  return words.length ? words.join(", ") : undefined
}

/** The HL7 v3 interpretation codes that read as out of range. */
const POOR_CODES = new Set(["H", "L", "A", "HH", "LL", "AA", "HU", "LU"])

/** The tone a result's flags give it: `good` for a normal code, `poor` for
 * a code out of range, `none` otherwise. Only a flag naming the HL7 v3
 * system, or none, is read; another system's codes mean something else.
 * A poor flag wins over a normal one. */
export function interpretationTone(
  interpretation: unknown
): "good" | "poor" | "none" {
  let tone: "good" | "poor" | "none" = "none"
  for (const f of Array.isArray(interpretation) ? interpretation : []) {
    if (!isBag(f)) continue
    const system = text(f.system)
    if (system && system !== INTERPRETATION_SYSTEM) continue
    const code = text(f.code)
    if (!code) continue
    if (POOR_CODES.has(code)) return "poor"
    if (code === "N") tone = "good"
  }
  return tone
}

/** A reference range in words: `60 to 120 mg/dL`, `under 120 mg/dL`,
 * `over 40 mg/dL`, else its text. */
export function rangeWords(range: Record<string, unknown>): string | undefined {
  const low = isQuantity(range.low) ? range.low : undefined
  const high = isQuantity(range.high) ? range.high : undefined
  if (low && high) {
    return low.unit === high.unit && !low.comparator
      ? `${displayDigits(low.value)} to ${displayQuantity(high)}`
      : `${displayQuantity(low)} to ${displayQuantity(high)}`
  }
  if (high) return `under ${displayQuantity(high)}`
  if (low) return `over ${displayQuantity(low)}`
  return text(range.text)
}

function isBag(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

function text(value: unknown): string | undefined {
  return typeof value === "string" && value ? value : undefined
}

/** A metric id read as words when no record names it, its last segment
 * as a code is: `body/resting_rate` is "Resting rate". */
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

/** Where a shape's reducers are in the server's table (`FORM_REDUCERS`):
 * a category reads under its form's coded entry. */
function tableOf(shape: ReadingShape, form?: Form): ReducerTable {
  if (shape === "coded")
    return form === "amount" ? "coded amount" : "coded gauge"
  return shape
}

/** The menu each shape offers, the default first, as reducer keys
 * (`reducerKey`). Its order is the console's; which reducers it holds is
 * the server's (`reducerOptions` keeps only those the form admits). A
 * percentile is offered as the median and the 90th; a `durationIn` band
 * only where an address names one (`reducerOf`), since no metric says
 * which; a category's first and last answer codes, which no chart draws,
 * so they are left out. `MENU_LEAVES_OUT` names those choices, and a test
 * holds every other reducer the server admits to a place here. */
const MENU_ORDER: Record<
  Exclude<ReducerTable, "summary">,
  readonly string[]
> = {
  gauge: [
    "avg",
    "min",
    "max",
    "first",
    "last",
    "count",
    "stddev",
    "percentile:0.5",
    "percentile:0.9",
    "circularMean",
  ],
  amount: [
    "sum",
    "count",
    "avg",
    "min",
    "max",
    "first",
    "last",
    "stddev",
    "percentile:0.5",
    "percentile:0.9",
  ],
  counter: ["increase", "rate", "last", "first", "min", "max", "count"],
  "coded amount": ["sum", "count", "min", "max"],
  "coded gauge": ["timeInState", "count"],
}

/** The reducers the server admits for a form that its menu leaves out, and
 * why: a test holds the menus to the server's table with these apart. */
export const MENU_LEAVES_OUT: Readonly<Record<string, readonly string[]>> = {
  gauge: ["durationIn"],
  "coded gauge": ["first", "last"],
}

/** The reducers a shape may ask for, the default first: its menu
 * (`MENU_ORDER`) less any reducer the server does not admit for the form
 * (`FORM_REDUCERS`). A level's circular mean is offered where it is an
 * angle. A summary takes what `summaryReducers` lists. */
export function reducerOptions(
  shape: ReadingShape,
  form?: Form,
  metric?: Pick<MetricInfo, "measures">,
  statistic?: string
): string[] {
  const table = tableOf(shape, form)
  if (table === "summary") return summaryReducers(statistic, "summaries")
  const admitted = FORM_REDUCERS[table]
  return MENU_ORDER[table].filter(
    (key) =>
      admitted.includes(reducerName(reducerOf(key))) &&
      (key !== "circularMean" || metric?.measures === "angle")
  )
}

/** Whether a summary's statistic adds up (`ADDITIVE_STATISTICS`). */
export function additive(statistic: string | undefined): boolean {
  return statistic !== undefined && ADDITIVE_STATISTICS.includes(statistic)
}

/** The reducers a vendor summary takes (docs/api.md, "Reducers, by form"):
 * those the server admits for a summary (`FORM_REDUCERS.summary`) over the
 * one statistic a row reads, `sum` only where that statistic adds up (the
 * default then). Under `prefer-raw` a bucket served by a summary of another
 * statistic answers `sum` null instead of a refusal, so `sum` is taken there
 * too, and an unknown statistic offers `sum` last and leaves the server to
 * say. */
export function summaryReducers(
  statistic: string | undefined,
  from: AggregateFrom
): string[] {
  const shared = SUMMARY_ORDER.filter(
    (r) => r !== "sum" && FORM_REDUCERS.summary.includes(r)
  )
  if (additive(statistic)) return ["sum", ...shared]
  return statistic === undefined || from === "prefer-raw"
    ? [...shared, "sum"]
    : shared
}

/** A summary's menu order. */
const SUMMARY_ORDER = ["sum", "avg", "min", "max", "first", "last", "count"]

/** The reducers that read raw points alone: no summary takes them, so a
 * read asking for one leaves the summaries out (`from: raw`). */
export function rawOnly(key: string): boolean {
  return !summaryReducers(undefined, "prefer-raw").includes(
    reducerName(reducerOf(key))
  )
}

/** The reducers whose values are in the metric's own unit, which a level's
 * lowest-to-highest band reads beside: never a count, a spread, a time in
 * a range or an angle's mean. */
export function bandedReducer(key: string): boolean {
  const name = reducerName(reducerOf(key))
  return ["avg", "min", "max", "first", "last", "percentile"].includes(name)
}

/** A reducer key (`reducerKey`) as the aggregate reads it: `avg` as is,
 * `percentile:0.9` as `{"percentile": 0.9}`, `durationIn:95,114` as
 * `{"durationIn": [95, 114]}`. */
export function reducerOf(key: string): AggregateReducer {
  const at = key.indexOf(":")
  if (at < 0) return key
  const name = key.slice(0, at)
  const arg = key.slice(at + 1)
  if (name === "percentile") return { percentile: Number(arg) }
  if (name === "durationIn") return { durationIn: arg.split(",").map(Number) }
  if (name === "timeInState") return { timeInState: arg.split(",") }
  return key
}

/** A reducer key in a menu's words: the 50th percentile as the median,
 * every other as `reducerLabel` says it ("Time in range 95 to 114"). */
export function reducerChoiceWords(key: string): string {
  if (key === "percentile:0.5") return "Median"
  return reducerLabel(reducerOf(key))
}

export const REDUCER_WORDS: Record<string, string> = {
  avg: "Average",
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

/** The statistics a stream's vendor summaries keep, in words for its
 * Reading cell ("average, highest"), the default first; undefined where it
 * keeps none or only the vendor's own value, which needs no name. */
export function keptStatisticsWords(
  statistics: readonly string[] | undefined
): string | undefined {
  const kept = statisticChoices(statistics ?? [])
  if (!kept.length || (kept.length === 1 && kept[0] === "vendor")) {
    return undefined
  }
  return kept.map((s) => lowerFirstWord(statisticWords(s))).join(", ")
}

/** A value's statistic as a label before it ("Average 74 /min"); none for
 * the vendor's own value, which is the value itself. */
export function statisticLabel(
  statistic: string | undefined
): string | undefined {
  if (!statistic || statistic === "vendor") return undefined
  return statisticWords(statistic)
}

function lowerFirstWord(text: string): string {
  return text.charAt(0).toLowerCase() + text.slice(1)
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
  const entries: [string, unknown][] = Object.entries(reducer)
  const [name, arg] = entries[0] ?? ["", undefined]
  return `${name}:${Array.isArray(arg) ? arg.join(",") : String(arg)}`
}

/** A reducer in words: "Average", "Time in range 95 to 114". */
export function reducerLabel(reducer: AggregateReducer): string {
  const name = reducerName(reducer)
  const word = REDUCER_WORDS[name] ?? name
  if (typeof reducer === "string") return word
  const arg: unknown = Object.values(reducer)[0]
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
  /** Where the newest value is a bound (`<0.01`), its comparator. */
  comparator?: string
  unit?: string
  /** The newest point's time, the row's `start`. */
  at: string
  /** Where the newest point covers a window (an amount, a summary), its
   * end, the row's `end`. */
  end?: string
  /** Every point the stream holds. */
  count?: number
  coverage?: string
  /** The statistics its vendor summaries keep (`avg`, `max`, `vendor`),
   * where it keeps any. */
  statistics?: string[]
  /** The statistic the newest value is, where it is a vendor summary's. */
  statistic?: string
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
  // A summary opens on its own statistic.
  const statistic =
    typeof properties.statistic === "string" && properties.statistic
      ? properties.statistic
      : undefined
  // Record paths, the spelling the latest read's groups carry.
  return {
    ...streamSearch({ metric, source, about, channel }),
    ...(statistic
      ? { statistic: `${encodeURIComponent(metric)}~${statistic}` }
      : {}),
    range: "custom",
    start: new Date(window.start).toISOString(),
    end: new Date(window.end).toISOString(),
  }
}

/** The timeline's address for one stream: its metric, source, thing and
 * channel, as the URL keys the timeline reads. A key the stream names no
 * value for is `~none`, never left out: left out, the timeline would read
 * every source's, everyone's or every channel's values of the metric. */
export function streamSearch(stream: {
  metric: string
  source?: string
  about?: string
  channel?: string
}): Record<string, string> {
  return {
    metric: stream.metric,
    source: addressValue(stream.source || NONE),
    about: addressValue(stream.about || NONE),
    channel: addressValue(stream.channel || NONE),
  }
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

/** The statistic a read of several takes until the reader picks: the
 * average where the summaries keep one, the one they keep, else the
 * vendor's own, else the first by name. */
export function defaultStatistic(
  statistics: readonly string[]
): string | undefined {
  if (statistics.includes("avg")) return "avg"
  if (statistics.length === 1) return statistics[0]
  if (statistics.includes("vendor")) return "vendor"
  return [...statistics].sort()[0]
}

/** The statistics as a menu lists them: the default first, the rest by
 * name. */
export function statisticChoices(statistics: readonly string[]): string[] {
  const first = defaultStatistic(statistics)
  if (first === undefined) return []
  return [first, ...[...new Set(statistics)].filter((s) => s !== first).sort()]
}

/** The latest read's rows as streams, newest first. A row with no metric in
 * its group answers no stream and is dropped. A stream whose summaries keep
 * several statistics answers a row per statistic (the read groups by
 * `statistic`); they are one stream, its newest point the newest row's (the
 * default statistic's at one instant), its points all of theirs. */
export function streamsOf(
  result: Pick<AggregateResult, "rows"> | undefined
): Stream[] {
  const byKey = new Map<string, { stream: Stream; statistic?: string }>()
  for (const row of result?.rows ?? []) {
    const metric = row.group?.metric
    if (!metric) continue
    const value = lastValue(row)
    const key = streamKey(
      metric,
      row.group?.source,
      row.group?.about,
      row.group?.channel
    )
    const statistic = row.group?.statistic || undefined
    const stream: Stream = {
      key,
      metric,
      source: row.group?.source,
      about: row.group?.about,
      ...(row.group?.channel ? { channel: row.group.channel } : {}),
      value: value?.value ?? null,
      ...(value?.comparator ? { comparator: value.comparator } : {}),
      unit: value?.unit,
      // A coded `last` answers its code, with `value` null.
      ...(value?.code ? { code: value.code } : {}),
      ...(row.form ? { form: row.form } : {}),
      at: row.start,
      ...(row.end && row.end !== row.start ? { end: row.end } : {}),
      count: row.count,
      coverage: row.coverage,
      ...(statistic ? { statistics: [statistic], statistic } : {}),
    }
    const held = byKey.get(key)
    if (!held) {
      byKey.set(key, { stream, statistic })
      continue
    }
    const statistics = [
      ...new Set([
        ...(held.stream.statistics ?? []),
        ...(statistic ? [statistic] : []),
      ]),
    ].sort()
    const count =
      held.stream.count === undefined && stream.count === undefined
        ? undefined
        : (held.stream.count ?? 0) + (stream.count ?? 0)
    const gap = Date.parse(stream.at) - Date.parse(held.stream.at)
    const newer =
      gap > 0 ||
      (gap === 0 &&
        statistic !== undefined &&
        statistic === defaultStatistic(statistics))
    const next = newer ? { stream, statistic } : held
    byKey.set(key, {
      statistic: next.statistic,
      stream: {
        ...next.stream,
        count,
        ...(statistics.length ? { statistics } : {}),
      },
    })
  }
  return [...byKey.values()]
    .map((h) => h.stream)
    .sort((a, b) => Date.parse(b.at) - Date.parse(a.at))
}
