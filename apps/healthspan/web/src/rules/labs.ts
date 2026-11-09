// The lab rules. Each is a pure function of results; `evaluateLabs` runs
// them on the newest result of every series and keeps each series'
// strongest finding.
//
// 1. Out of lab range (danger): the lab's interpretation flag, else the
//    value against the lab's own bands.
// 2. Off target (warning): outside the catalogue's green band.
// 3. Overdue (info): an important entry last tested longer ago than its
//    cadence.
// 4. Trend: the regression, percent of the mean per month. It colours the
//    trend chip and never makes a finding.

import {
  DEFAULT_CADENCE_MONTHS,
  forSex,
  greenBands,
  targetBand,
  targetSpan,
  type CatalogueEntry,
  type TargetBand,
} from "../catalogue"
import type { Sex } from "../kinds"
import { resultText } from "../labs/result"
import type { Band, LabIndex, LabResult, Series, Subject } from "../labs/types"
import {
  channelLabel,
  formatDate,
  formatValue,
  monthsBetween,
  rangeText,
} from "../lib/format"
import { routeHash } from "../route"
import { sortFindings, type Finding } from "./types"

const HIGH = new Set(["H", "HH", "HU"])
const LOW = new Set(["L", "LL", "LU"])
const ABNORMAL = new Set(["A", "AA", ...HIGH, ...LOW])

export interface OutOfRange {
  word: "High" | "Low" | "Abnormal"
  /** How far past the violated bound, in widths of the band (or of the
   * bound itself for a one-sided band). Zero for a flag with no number. */
  distance: number
  /** The band that decided it, when a band did. */
  band?: Band
}

/** How a result stands against the lab's own ranges: out (rule 1 fires),
 * in, or uncertain, for a bound such as `<5` against a range starting at 2
 * that could lie either side. */
export type RangeStatus =
  | ({ status: "out" } & OutOfRange)
  | { status: "in"; band?: Band }
  | { status: "uncertain"; band: Band }

function width(b: Band): number {
  if (b.low !== undefined && b.high !== undefined && b.high > b.low)
    return b.high - b.low
  return Math.abs(b.high ?? b.low ?? 1) || 1
}

/** The lab's normal ranges: the bands with no tone or an explicit `good`
 * one (the kind reads an absent tone as good). */
export function normalBands(r: Pick<LabResult, "ranges">): Band[] {
  return r.ranges.filter(
    (b) =>
      (!b.tone || b.tone === "good") &&
      (b.low !== undefined || b.high !== undefined)
  )
}

/** Units whose values can fall below zero: scores (a BMD T-score of -1.4),
 * plain ratios and numbers, angles and temperatures. A result in any other
 * unit is an amount, a concentration, a count or a rate, which the metric's
 * unit says cannot be negative. A result with no unit is treated as
 * signed. */
const SIGNED_UNITS = new Set(["score", "1", "deg", "Cel", "[degF]"])

export function canBeNegative(unit: string | undefined): boolean {
  return !unit || SIGNED_UNITS.has(unit)
}

/** The values a result can be. A number is a point; a comparator result is
 * the interval it denotes: `<1` is everything below 1 and `>5` everything
 * above 5. A `<` or `<=` result starts at minus infinity, or at 0 when its
 * unit says it cannot be negative (`<0.5 mg/L` is 0 up to 0.5); a `>` or
 * `>=` result runs to plus infinity. */
export interface Interval {
  lo: number
  loIn: boolean
  hi: number
  hiIn: boolean
}

export function resultInterval(
  value: number,
  comparator?: string,
  unit?: string
): Interval {
  const floor = canBeNegative(unit) || value < 0 ? Number.NEGATIVE_INFINITY : 0
  switch (comparator) {
    case "<":
      return { lo: floor, loIn: true, hi: value, hiIn: false }
    case "<=":
      return { lo: floor, loIn: true, hi: value, hiIn: true }
    case ">":
      return {
        lo: value,
        loIn: false,
        hi: Number.POSITIVE_INFINITY,
        hiIn: false,
      }
    case ">=":
      return {
        lo: value,
        loIn: true,
        hi: Number.POSITIVE_INFINITY,
        hiIn: false,
      }
    default:
      return { lo: value, loIn: true, hi: value, hiIn: true }
  }
}

/** A band's bounds, inclusive at both ends. */
function bandBounds(b: Band): { lo: number; hi: number } {
  return {
    lo: b.low ?? Number.NEGATIVE_INFINITY,
    hi: b.high ?? Number.POSITIVE_INFINITY,
  }
}

/** Every value of the interval lies in the band. */
export function within(i: Interval, b: Band): boolean {
  const { lo, hi } = bandBounds(b)
  return i.lo >= lo && i.hi <= hi
}

/** Which side of the band every value of the interval lies on, if all lie
 * outside it. */
export function outside(i: Interval, b: Band): "above" | "below" | undefined {
  const { lo, hi } = bandBounds(b)
  if (i.hi < lo || (i.hi === lo && !i.hiIn)) return "below"
  if (i.lo > hi || (i.lo === hi && !i.loIn)) return "above"
  return undefined
}

function past(v: number, b: Band, side: "above" | "below"): number {
  const d = side === "above" ? v - (b.high as number) : (b.low as number) - v
  return Math.max(0, d) / width(b)
}

/** Rule 1, in full. The interpretation decides when it is there; a coded
 * result without one is never out of range; otherwise the result is read
 * against the normal bands (inside any one is in range, outside all is out,
 * anything else uncertain) and then against the `poor` cut-offs. */
export function rangeStatus(r: LabResult): RangeStatus | undefined {
  const normal = normalBands(r)
  if (r.interpretation.length) {
    const codes = r.interpretation.map((i) => i.code.toUpperCase())
    if (codes.every((c) => c === "N")) return { status: "in", band: normal[0] }
    const bad = codes.find(
      (c) => ABNORMAL.has(c) || (r.value === undefined && c !== "N")
    )
    if (!bad) return { status: "in", band: normal[0] }
    const word = HIGH.has(bad) ? "High" : LOW.has(bad) ? "Low" : "Abnormal"
    const band = normal[0]
    const v = r.value
    let distance = 0
    if (band && v !== undefined) {
      if (band.high !== undefined && v > band.high)
        distance = past(v, band, "above")
      else if (band.low !== undefined && v < band.low)
        distance = past(v, band, "below")
    }
    return { status: "out", word, distance, band }
  }
  const v = r.value
  if (v === undefined) return undefined
  const i = resultInterval(v, r.comparator, r.unit)
  if (normal.length) {
    const home = normal.find((b) => within(i, b))
    if (home) return { status: "in", band: home }
    const sides = normal.map((b) => outside(i, b))
    if (sides.some((x) => x === undefined)) {
      const band = normal[sides.indexOf(undefined)]
      return { status: "uncertain", band }
    }
    // Outside every band: the first band names the side and the distance.
    const band = normal[0]
    const side = sides[0] as "above" | "below"
    return {
      status: "out",
      word: side === "above" ? "High" : "Low",
      distance: past(v, band, side),
      band,
    }
  }
  for (const b of r.ranges) {
    if (b.tone !== "poor") continue
    if (b.low !== undefined && b.high === undefined) {
      // At or above `low` is abnormal.
      const cut: Band = { low: b.low }
      if (within(i, cut))
        return {
          status: "out",
          word: "High",
          distance: (v - b.low) / width(b),
          band: b,
        }
      if (!outside(i, cut)) return { status: "uncertain", band: b }
    } else if (b.high !== undefined && b.low === undefined) {
      const cut: Band = { high: b.high }
      if (within(i, cut))
        return {
          status: "out",
          word: "Low",
          distance: (b.high - v) / width(b),
          band: b,
        }
      if (!outside(i, cut)) return { status: "uncertain", band: b }
    } else if (b.low !== undefined && b.high !== undefined) {
      if (within(i, b))
        return { status: "out", word: "Abnormal", distance: 0, band: b }
      if (!outside(i, b)) return { status: "uncertain", band: b }
    }
  }
  return undefined
}

/** Rule 1: out of the lab's range, only when certain. */
export function outOfRange(r: LabResult): OutOfRange | undefined {
  const s = rangeStatus(r)
  if (s?.status !== "out") return undefined
  return { word: s.word, distance: s.distance, band: s.band }
}

/** Rule 2: outside the catalogue's green bands, bounds inclusive. The band
 * the value lands in names it ("Borderline High"). A comparator result is
 * off target only when every value it can be is. */
export function offTarget(
  r: LabResult,
  entry: CatalogueEntry | undefined
): { label: string; distance: number } | undefined {
  if (r.value === undefined) return undefined
  const green = greenBands(entry)
  const span = targetSpan(entry)
  if (!green.length || !span) return undefined
  const v = r.value
  const i = resultInterval(v, r.comparator, r.unit)
  const asBand = (b: TargetBand): Band => ({ low: b.from, high: b.to })
  if (green.some((b) => !outside(i, asBand(b)))) return undefined
  const lo = span.from ?? Number.NEGATIVE_INFINITY
  const hi = span.to ?? Number.POSITIVE_INFINITY
  const size =
    Number.isFinite(hi - lo) && hi > lo
      ? hi - lo
      : Math.abs(Number.isFinite(hi) ? hi : lo) || 1
  const distance = v > hi ? (v - hi) / size : v < lo ? (lo - v) / size : 0
  // The band the result lands in names it; for a bound, the first band any
  // of its values reach.
  const named = r.comparator
    ? entry?.target?.find((b) => b.tone !== "success" && !outside(i, asBand(b)))
    : targetBand(entry, v)
  return { label: named?.label ?? "Off target", distance }
}

/** The flag a result carries on the lab screens: the lab's own word, the
 * uncertain case, or off target, named and toned by the catalogue band the
 * value lands in ("High" in a danger band is `danger`, "Borderline High" in
 * a warning band is `warning`; "Off target" outside any named band is
 * `warning`), so a marker's ring matches the zone it sits in. `withTarget`
 * is false where the values are in another unit than the catalogue's
 * target. */
export function resultFlag(
  r: LabResult,
  entry: CatalogueEntry | undefined,
  withTarget = true
): { word: string; tone: "danger" | "warning" | "neutral" } | undefined {
  const s = rangeStatus(r)
  if (s?.status === "out") return { word: s.word, tone: "danger" }
  const off = withTarget ? offTarget(r, entry) : undefined
  if (off) {
    const band = entry?.target?.find(
      (b) => b.tone !== "success" && b.label === off.label
    )
    return {
      word: off.label,
      tone: band?.tone === "danger" ? "danger" : "warning",
    }
  }
  if (s?.status === "uncertain") return { word: "Uncertain", tone: "neutral" }
  return undefined
}

/** Rule 3: an important entry with at least two results whose newest is
 * older than its cadence. Returns the months since. */
export function overdue(
  results: readonly Pick<LabResult, "at">[],
  entry: CatalogueEntry | undefined,
  today: Date
): number | undefined {
  if (!entry?.important || results.length < 2) return undefined
  const newest = results[results.length - 1]
  const months = monthsBetween(newest.at, today)
  return months >= (entry.cadenceMonths ?? DEFAULT_CADENCE_MONTHS)
    ? months
    : undefined
}

const MONTH_MS = 30.4375 * 86_400_000

/** Below this rate, in percent of the mean per month, a trend is flat. */
export const FLAT_RATE = 0.5

/** Fewest results a trend is fitted to. */
export const TREND_MIN_POINTS = 3

export type TrendDirection = "up" | "down" | "flat"

export interface TrendFit {
  /** Slope over the absolute mean, in percent per month (30.4375 days). */
  ratePerMonth: number
  direction: TrendDirection
  /** How many results the line was fitted to. */
  n: number
  /** The fitted line at the first and the last result's time: the
   * trendline's endpoints. */
  from: { at: string; value: number }
  to: { at: string; value: number }
}

/** Rule 4: the least-squares line of value over time in months through
 * `points` (oldest first, the numeric results the chart draws). Flat while
 * the rate, rounded to the one decimal it prints with, is 0.5 percent a
 * month or less. Undefined with fewer than three points, all on one
 * instant, or a mean of zero. */
export function trendFit(
  points: { at: string; value: number }[]
): TrendFit | undefined {
  if (points.length < TREND_MIN_POINTS) return undefined
  const xs = points.map((p) => Date.parse(p.at) / MONTH_MS)
  const ys = points.map((p) => p.value)
  const n = xs.length
  const mx = xs.reduce((a, b) => a + b, 0) / n
  const my = ys.reduce((a, b) => a + b, 0) / n
  let num = 0
  let den = 0
  for (let i = 0; i < n; i++) {
    num += (xs[i] - mx) * (ys[i] - my)
    den += (xs[i] - mx) ** 2
  }
  if (den === 0 || my === 0) return undefined
  const slope = num / den
  const ratePerMonth = (slope / Math.abs(my)) * 100
  // Decided on the rate as it prints (one decimal), so the arrow and the
  // number always agree: 0.47 prints "0.5" and is flat, as is -0.51.
  const shown = Math.round(Math.abs(ratePerMonth) * 10) / 10
  const direction: TrendDirection =
    shown <= FLAT_RATE ? "flat" : ratePerMonth > 0 ? "up" : "down"
  const at = (x: number) => my + slope * (x - mx)
  const first = points[0]
  const last = points[n - 1]
  return {
    ratePerMonth,
    direction,
    n,
    from: { at: first.at, value: at(xs[0]) },
    to: { at: last.at, value: at(xs[n - 1]) },
  }
}

/** The trend's colour: `success` when it moves the better way for the
 * entry's `trendGoal`, `danger` the worse way, `neutral` when flat or the
 * catalogue sets no goal. */
export function trendTone(
  fit: Pick<TrendFit, "direction"> | undefined,
  entry: CatalogueEntry | undefined
): "success" | "danger" | "neutral" {
  if (!fit || fit.direction === "flat" || !entry?.trendGoal) return "neutral"
  return (fit.direction === "up") === (entry.trendGoal === "higher")
    ? "success"
    : "danger"
}

/** The statuses the Labs filter reads. */
export type LabStatus = "range" | "target" | "overdue" | "ok"

/** The statuses that hold on one series, each read on its newest result
 * independently: `range` when rule 1 fires, `target` when rule 2 fires
 * (whether or not rule 1 does), `overdue` when rule 3 fires, and `ok` when
 * the newest result has a lab range or a catalogue target and neither rule
 * 1 nor rule 2 fires. */
export function seriesStatuses(
  s: Pick<Series, "results">,
  entry: CatalogueEntry | undefined,
  today: Date
): Set<LabStatus> {
  const out = new Set<LabStatus>()
  const newest = s.results[s.results.length - 1]
  if (!newest) return out
  if (rangeStatus(newest)?.status === "out") out.add("range")
  if (offTarget(newest, entry)) out.add("target")
  if (overdue(s.results, entry, today) !== undefined) out.add("overdue")
  // In range only means something when there is a range or a target.
  const judged = rangeStatus(newest) !== undefined || !!entry?.target?.length
  if (judged && !out.has("range") && !out.has("target")) out.add("ok")
  return out
}

/** A series' name: its subject's, with the channel for a regional one. */
export function seriesName(
  subject: Pick<Subject, "name"> | undefined,
  s: Pick<Series, "code" | "channel">
): string {
  const name = subject?.name ?? s.code
  return s.channel ? `${name}, ${channelLabel(s.channel).toLowerCase()}` : name
}

/** Where a series opens: its subject's history, on its channel. */
export function seriesHref(s: Pick<Series, "code" | "channel">): string {
  return routeHash({
    screen: "lab",
    entry: s.code,
    query: s.channel ? `channel=${encodeURIComponent(s.channel)}` : "",
  })
}

/** The finding for one series, by the strongest rule that fires. `entry`
 * is the series' entry as it applies to the owner (`forSex`). */
export function seriesFinding(
  index: Pick<LabIndex, "subjects" | "sources">,
  s: Series,
  entry: CatalogueEntry | undefined,
  today: Date
): Finding | undefined {
  const newest = s.results[s.results.length - 1]
  if (!newest) return undefined
  const subject = index.subjects.get(s.code)
  const precision = subject?.precision
  const title = seriesName(subject, s)
  const shown = resultText(newest, precision, subject?.unit)
  const lab =
    (newest.source && index.sources.get(newest.source)?.name) ?? "the lab"
  const when = formatDate(newest.at)
  const base = {
    section: "labs" as const,
    subject: s.key,
    title,
    since: newest.at,
    href: seriesHref(s),
    important: !!entry?.important,
  }

  const out = outOfRange(newest)
  if (out) {
    const range = out.band
      ? rangeText(out.band.low, out.band.high, precision)
      : ""
    const side =
      out.word === "High" ? "above" : out.word === "Low" ? "below" : "outside"
    const b = out.band
    const limit =
      b && b.low !== undefined && b.high !== undefined
        ? `range of ${range}`
        : b?.high !== undefined
          ? `limit of ${formatValue(b.high, precision)}`
          : b?.low !== undefined
            ? `limit of ${formatValue(b.low, precision)}`
            : ""
    const detail = limit
      ? `${shown} on ${when}, ${side} ${lab}'s ${limit}`
      : `${shown} on ${when}, flagged ${out.word.toLowerCase()} by ${lab}`
    return {
      ...base,
      detail,
      tone: "danger",
      word: out.word,
      rank: 1,
      distance: out.distance,
    }
  }
  const off = offTarget(newest, entry)
  if (off) {
    const span = targetSpan(entry)
    const target = rangeText(span?.from, span?.to, precision)
    return {
      ...base,
      detail: `${shown} on ${when}, outside the target of ${target}${off.label === "Off target" ? "" : ` (${off.label.toLowerCase()})`}`,
      tone: "warning",
      word: "Off target",
      rank: 2,
      distance: off.distance,
    }
  }
  const months = overdue(s.results, entry, today)
  if (months !== undefined) {
    const ago =
      months >= 24 ? `${Math.floor(months / 12)} years` : `${months} months`
    return {
      ...base,
      detail: `Last tested ${ago} ago, on ${when}`,
      tone: "info",
      word: "Overdue",
      rank: 3,
    }
  }
  return undefined
}

/** Every lab finding, ordered: rule, then important first, then distance.
 * The targets are the ones for `sex`. */
export function evaluateLabs(
  index: Pick<LabIndex, "subjects" | "sources" | "series">,
  today: Date,
  sex?: Sex
): Finding[] {
  const out: Finding[] = []
  for (const s of index.series.values()) {
    const entry = index.subjects.get(s.code)?.entry
    const f = seriesFinding(
      index,
      s,
      entry ? forSex(entry, sex) : undefined,
      today
    )
    if (f) out.push(f)
  }
  return sortFindings(out)
}
