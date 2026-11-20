// A lab's bands as a result's reference ranges, the flag a band or a range
// gives a value, the HL7 interpretation a flag is written as, and the coded
// words a result can be (apps/healthspan/docs/importers/00-shared.md,
// Ranges and flags).

import type { UnitGraph } from "../../../labs/units"
import { INTERPRETATION } from "../../records"
import {
  compareDecimals,
  isNumber,
  quantity,
  scaleDecimal,
  strip,
  type Quantity,
} from "./numbers"
import { fixedFactor, knownUnit, ucum } from "./units"

export { INTERPRETATION } from "../../records"

/** A band as a provider reports it, before it is a reference range. */
export interface Band {
  low?: string | null
  high?: string | null
  /** The provider's word for the band (`lab`, `Optimal`, `HIGH`). */
  lab?: string | null
  /** The band's word where it has no label of its own: `normal`. */
  word?: string
  /** What sets a provider's own range apart from the lab's ("Numan
   * clinical range"); such a band sorts last. */
  detail?: string
  /** The coded result the band expects (`negative`). */
  expected?: string | null
  /** The band's unit as printed, where it can differ from the value's. */
  unit?: string | null
  /** How the provider colours the band; a band set where every band has a
   * tone is read as toned bands. */
  tone?: "good" | "fair" | "poor"
  /** Whether the value fell in it, as the provider says. */
  matched?: boolean
  /** For a one-sided band: whether the bound is inclusive (`<=`, `>=`). */
  inclusive?: boolean
  /** A coded band's criterion as printed (`Negative`, `Trace`). */
  criterion?: string
  /** The panel that printed the band. */
  panel?: string
  /** False for a band another panel printed beside the test's own. */
  primary?: boolean
}

/** One of a result's `referenceRanges`, as `labresult` declares it. */
export interface ReferenceRange {
  low?: Quantity
  high?: Quantity
  text?: string
  /** Absent reads as good. */
  tone?: "fair" | "poor"
}

/** A lab's word for a band as the range's word, and the detail it keeps. */
export const BAND_WORDS: Readonly<Record<string, readonly [string, string?]>> =
  {
    lab: ["normal"],
    normal: ["normal"],
    satisfactory: ["normal"],
    sufficiency: ["normal"],
    "low risk": ["normal", "low risk"],
    optimal: ["optimal"],
    desirable: ["optimal"],
    high: ["high"],
    "very high": ["high", "very high"],
    overweight: ["high", "overweight"],
    obese: ["high", "obese"],
    low: ["low"],
    deficient: ["low", "deficient"],
    underweight: ["low", "underweight"],
    borderline: ["borderline"],
  }

function bandWord(band: Band): { word: string; detail?: string } {
  const label = strip(band.lab ?? "").toLowerCase()
  if (!label) return { word: band.word ?? "normal" }
  const known = BAND_WORDS[label]
  return known
    ? { word: known[0], detail: known[1] }
    : { word: "normal", detail: label }
}

/** A band's text: its word, then what it expects, the word's detail, the
 * band's own detail and (for another panel's band) the panel, lower case:
 * `normal`, `low: deficient`, `normal: Numan clinical range`. */
export function bandText(band: Band, panel = false): string {
  const { word, detail } = bandWord(band)
  const details = [band.expected, detail, band.detail].filter(
    (d): d is string => !!d
  )
  if (panel && band.panel) details.push(band.panel.toLowerCase())
  return details.length ? `${word}: ${details.join(", ")}` : word
}

/** A one-sided band's bound in words, its strictness kept, which the
 * range's quantities cannot hold: `under 25`, `at most 25`, `over 1.7`,
 * `at least 1.7`. Undefined for a band with no one-sided bound. */
export function cutoffText(band: Band): string | undefined {
  if (band.inclusive === undefined) return undefined
  if (band.high !== null && band.high !== undefined)
    return `${band.inclusive ? "at most" : "under"} ${band.high}`
  if (band.low !== null && band.low !== undefined)
    return `${band.inclusive ? "at least" : "over"} ${band.low}`
  return undefined
}

/** A toned band's text: the provider's own word, lower case and not mapped
 * (`optimal`, `moderately raised`), then its criterion, its cut-off in
 * words and, for another panel's band, the panel: `deficiency: under 25`,
 * `risk: over 1.7, metabolic syndrome`. */
export function tonedBandText(band: Band, panel = false): string {
  const details: string[] = []
  if (band.criterion) details.push(band.criterion.toLowerCase())
  const cutoff = cutoffText(band)
  if (cutoff) details.push(cutoff)
  if (panel && band.panel) details.push(band.panel.toLowerCase())
  const word = (band.lab || "range").toLowerCase()
  return details.length ? `${word}: ${details.join(", ")}` : word
}

/** How the bounds of a value's ranges are written. */
export interface RangeUnits {
  /** The value's unit as a known UCUM code; absent for words or for a
   * unit kept as received, whose ranges keep their words alone. */
  ucum?: string
  /** The analyte, for the unit table's overrides. */
  analyte?: string
  /** The repository's unit graph: a band's own unit counts as known only
   * where it holds the unit. */
  units: UnitGraph
}

/** Bounds as JSON with sorted keys, which two bands are compared by. */
function boundKey(r: ReferenceRange): string {
  return JSON.stringify([r.high ?? null, r.low ?? null])
}

function bound(
  value: string | null | undefined,
  unit: string | undefined,
  factor?: string
): Quantity | undefined {
  if (value === null || value === undefined || value === "" || !unit)
    return undefined
  if (!isNumber(value)) return undefined
  const v = factor === undefined ? value : scaleDecimal(value, factor)
  return v === undefined ? undefined : quantity(v, unit)
}

/** Whether a band says anything in words: a label, a word of its own, a
 * detail or the coded result it expects. */
function hasWords(band: Band): boolean {
  return !!(band.lab || band.word || band.detail || band.expected)
}

/** The ranges of bands with no tone, in a reader's order: the normal or
 * optimal band first, a provider's own range last. A band whose own unit
 * no fixed factor moves into the value's is left out, with a note; a band
 * with no bound written and no words is dropped, and one with words alone
 * (beside a value kept as received or written as words) keeps them. */
export function plainRanges(
  bands: readonly Band[],
  units: RangeUnits
): { ranges: ReferenceRange[]; notes: string[] } {
  const notes: string[] = []
  const seen = new Set<string>()
  const ranked: { rank: number; range: ReferenceRange }[] = []
  for (const band of bands) {
    const unit = units.ucum
    let factor: string | undefined
    const hasBound = !!(band.low || band.high)
    if (hasBound && unit && band.unit) {
      const own = ucum(band.unit, units.analyte) ?? band.unit
      if (own !== unit) {
        factor = fixedFactor(own, unit)
        if (factor === undefined) {
          notes.push(`the range in ${band.unit} has no fixed factor to ${unit}`)
          continue
        }
      }
    }
    const range: ReferenceRange = {}
    const low = bound(band.low, unit, factor)
    const high = bound(band.high, unit, factor)
    if (low) range.low = low
    if (high) range.high = high
    if (!low && !high && !hasWords(band)) continue
    const key = `${boundKey(range)}\u0000${bandText(band)}`
    if (seen.has(key)) continue
    seen.add(key)
    const secondary = band.primary === false
    range.text = bandText(band, secondary)
    const { word } = bandWord(band)
    if (word === "high" || word === "low") range.tone = "poor"
    const rank = band.detail
      ? 3
      : secondary
        ? 2
        : word === "normal" || word === "optimal"
          ? 0
          : 1
    ranked.push({ rank, range })
  }
  return {
    ranges: ranked
      .map((r, i) => ({ ...r, i }))
      .sort((a, b) => a.rank - b.rank || a.i - b.i)
      .map((r) => r.range),
    notes,
  }
}

const TONE_RANK = { good: 1, fair: 2, poor: 3 } as const

/** The ranges of toned bands, each a band's bounds, text and tone (good
 * left out), in a reader's order: the good band the value fell in, the
 * other good bands, fair, poor, then other panels' bands in the same order.
 * A band several panels print is kept once. */
export function tonedRanges(
  bands: readonly Band[],
  units: RangeUnits
): ReferenceRange[] {
  const seen = new Set<string>()
  const ranked: { rank: number; range: ReferenceRange }[] = []
  for (const band of bands) {
    let unit = units.ucum
    if (!unit && band.unit)
      unit = knownUnit(band.unit, units.analyte, units.units)?.ucum
    const range: ReferenceRange = {}
    const low = bound(band.low, unit)
    const high = bound(band.high, unit)
    if (low) range.low = low
    if (high) range.high = high
    const tone = band.tone ?? "poor"
    const key = `${boundKey(range)}\u0000${tonedBandText(band)}\u0000${tone}`
    if (seen.has(key)) continue
    seen.add(key)
    const secondary = band.primary === false
    range.text = tonedBandText(band, secondary)
    if (tone !== "good") range.tone = tone
    const own = tone === "good" && band.matched && !secondary
    ranked.push({
      rank: (own ? 0 : TONE_RANK[tone]) + (secondary ? 4 : 0),
      range,
    })
  }
  return ranked
    .map((r, i) => ({ ...r, i }))
    .sort((a, b) => a.rank - b.rank || a.i - b.i)
    .map((r) => r.range)
}

/** A value's reference ranges from its bands: toned ranges where every
 * band has a tone, else plain ones; null where none is left. */
export function referenceRanges(
  bands: readonly Band[],
  units: RangeUnits
): { ranges: ReferenceRange[] | null; notes: string[] } {
  if (bands.length && bands.every((b) => b.tone)) {
    const ranges = tonedRanges(bands, units)
    return { ranges: ranges.length ? ranges : null, notes: [] }
  }
  const { ranges, notes } = plainRanges(bands, units)
  return { ranges: ranges.length ? ranges : null, notes }
}

// ---- flags ----

/** A lab's flag as the word a reader sees. */
export const FLAG_WORDS: Readonly<Record<string, string>> = {
  N: "Normal",
  H: "High",
  L: "Low",
  A: "Abnormal",
  HH: "Critically high",
  LL: "Critically low",
}

/** A flag as a result's `interpretation`, the CSV import's shape: the HL7
 * code with its word, the provider's own words where they are longer than
 * the letter. A flag HL7 does not hold is kept as its code alone. Null for
 * no flag. */
export function interpretation(
  flag: string | null | undefined,
  text?: string | null
): { system?: string; code: string; display?: string }[] | null {
  if (!flag) return null
  const word = FLAG_WORDS[flag]
  const display = text && text.length > 1 ? text : word
  if (!word) return [display ? { code: flag, display } : { code: flag }]
  return [{ system: INTERPRETATION, code: flag, display: display ?? word }]
}

/** The flag a value's place among toned bands gives: none where the
 * provider marks no band as the value's, `N` for a good band, `H` for a
 * band at or above every good band's top, `L` for one at or below every
 * good band's bottom, else `A`. */
export function flagOfTonedBands(
  bands: readonly Band[]
): "N" | "H" | "L" | "A" | undefined {
  const hit = bands.find((b) => b.matched)
  if (!hit) return undefined
  if (hit.tone === "good") return "N"
  const goods = bands.filter((b) => b.tone === "good")
  const numbers = (xs: (string | null | undefined)[]) =>
    xs.filter((x): x is string => !!x && isNumber(x))
  const highs = numbers(goods.map((b) => b.high))
  const lows = numbers(goods.map((b) => b.low))
  const top = highs.reduce<string | undefined>(
    (m, x) => (m === undefined || compareDecimals(x, m) === 1 ? x : m),
    undefined
  )
  const bottom = lows.reduce<string | undefined>(
    (m, x) => (m === undefined || compareDecimals(x, m) === -1 ? x : m),
    undefined
  )
  if (hit.low && isNumber(hit.low) && top !== undefined)
    if ((compareDecimals(hit.low, top) ?? -1) >= 0) return "H"
  if (hit.high && isNumber(hit.high) && bottom !== undefined)
    if ((compareDecimals(hit.high, bottom) ?? 1) <= 0) return "L"
  return "A"
}

/** The flag a value gets against a range, compared as decimals, bounds
 * inclusive: `L` below the low bound, `H` above the high one, else `N`.
 * Undefined where the value or a bound given is not a number. */
export function flagOfRange(
  value: string,
  low?: string | null,
  high?: string | null
): "N" | "H" | "L" | undefined {
  if (!isNumber(value)) return undefined
  if (low) {
    const c = compareDecimals(value, low)
    if (c === undefined) return undefined
    if (c < 0) return "L"
  }
  if (high) {
    const c = compareDecimals(value, high)
    if (c === undefined) return undefined
    if (c > 0) return "H"
  }
  return "N"
}

// ---- coded results ----

export const SNOMED = "http://snomed.info/sct"

/** The words a result can be that are written as a code: the index key
 * `code` and, where SNOMED CT has a qualifier for them, its concept. */
export const CODED_WORDS: Readonly<
  Record<string, { code: string; snomed?: string }>
> = {
  negative: { code: "negative", snomed: "260385009" },
  "not detected": { code: "not-detected", snomed: "260415000" },
  "no flag": { code: "none" },
}

/** A result in words as a coded result (`Negative`, `Not detected`), its
 * words kept as received, or undefined for words that are not a code. */
export function codedResult(text: string):
  | {
      code: string
      codedValue: { system?: string; code?: string; display: string }
    }
  | undefined {
  const c = CODED_WORDS[strip(text).toLowerCase()]
  if (!c) return undefined
  return {
    code: c.code,
    codedValue: c.snomed
      ? { system: SNOMED, code: c.snomed, display: text }
      : { display: text },
  }
}
