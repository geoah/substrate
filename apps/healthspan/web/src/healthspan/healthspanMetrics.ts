// The Healthspan screens' names and series. The nine contributors take
// their display labels from here (the engine's labels are Title Case);
// the signal cards chart a daily series each, by route id
// (`#/healthspan/<id>`): its unit and how it prints. A vendor's own score
// keeps its vendor's name.

import { BODY_CARRY_FORWARD_DAYS, yearsAt } from "../engine/engine"
import { formatImpact, formatValue, tenths } from "../engine/format"
import type { Metric, MetricId, ValueFormat, Verdict } from "../engine/report"
import { CONTRIBUTOR_INPUTS, type Daily, type HealthspanSeries } from "./read"

/** The contributors' display labels, in the engine's order. */
export const CONTRIBUTOR_LABELS: Record<MetricId, string> = {
  sleepConsistency: "Sleep consistency",
  sleepHours: "Hours of sleep",
  zones13: "Zone 1-3 time a week",
  zones45: "Zone 4-5 time a week",
  strength: "Strength time a week",
  steps: "Steps a day",
  vo2max: "VO2 max",
  rhr: "Resting heart rate",
  leanMass: "Lean body mass",
}

/** The status word a contributor's finding shows when its 30-day average
 * would add years. */
export const CONTRIBUTOR_WORDS: Record<MetricId, string> = {
  sleepConsistency: "Inconsistent",
  sleepHours: "Short",
  zones13: "Little",
  zones45: "Little",
  strength: "Little",
  steps: "Few",
  vo2max: "Below target",
  rhr: "High",
  leanMass: "Below target",
}

export type SignalId =
  | "recovery"
  | "sleepPerformance"
  | "strain"
  | "hrv"
  | "hrvSdnn"
  | "respiratoryRate"
  | "spo2"
  | "skinTemperature"

export interface HealthspanMetric {
  id: SignalId
  label: string
  unit: string
  /** Decimals shown. */
  precision: number
  /** Where the numbers come from, in a sentence. */
  source: string
  daily: (d: HealthspanSeries) => Daily
  /** Draws the 30-day baseline: a nightly physiological signal. */
  hasBaseline: boolean
}

/** The signals, in page order. A signal no source reports is left out of
 * the page. */
export const HEALTHSPAN_METRICS: HealthspanMetric[] = [
  {
    id: "recovery",
    label: "Recovery",
    unit: "%",
    precision: 0,
    source: "WHOOP's recovery score, from the source that reports it.",
    daily: (d) => d.recovery,
    hasBaseline: false,
  },
  {
    id: "sleepPerformance",
    label: "Sleep performance",
    unit: "%",
    precision: 0,
    source:
      "WHOOP's sleep performance: sleep against the sleep it says was needed.",
    daily: (d) => d.sleepPerformance,
    hasBaseline: false,
  },
  {
    id: "strain",
    label: "Strain",
    unit: "",
    precision: 1,
    source: "WHOOP's day strain.",
    daily: (d) => d.strain,
    hasBaseline: false,
  },
  {
    id: "hrv",
    label: "HRV",
    unit: "ms",
    precision: 0,
    source:
      "Nightly heart rate variability as RMSSD, from the source your repository ranks first.",
    daily: (d) => d.hrv,
    hasBaseline: true,
  },
  {
    id: "hrvSdnn",
    label: "HRV (SDNN)",
    unit: "ms",
    precision: 0,
    source:
      "Heart rate variability as SDNN, the mean of each day's readings. A different measure from RMSSD; shown beside it, never merged.",
    daily: (d) => d.hrvSdnn,
    hasBaseline: true,
  },
  {
    id: "respiratoryRate",
    label: "Breathing rate",
    unit: "/min",
    precision: 1,
    source: "The source's nightly summary, during sleep.",
    daily: (d) => d.respiratoryRate,
    hasBaseline: true,
  },
  {
    id: "spo2",
    label: "Blood oxygen",
    unit: "%",
    precision: 1,
    source: "The source's nightly summary, during sleep.",
    daily: (d) => d.spo2,
    hasBaseline: false,
  },
  {
    id: "skinTemperature",
    label: "Skin temperature",
    unit: "°C",
    precision: 2,
    source: "The source's nightly summary, during sleep.",
    daily: (d) => d.skinTemperature,
    hasBaseline: true,
  },
]

export function healthspanMetric(id: string): HealthspanMetric | undefined {
  return HEALTHSPAN_METRICS.find((m) => m.id === id)
}

/** The 30-day baseline per day: mean and standard deviation of the 30 days
 * before it, with at least 14 of them holding a value. */
export function baselineSeries(
  m: Daily
): { at: string; value: number; low: number; high: number }[] {
  // One sliding pass: the window is the 30 days before each day, the day
  // itself left out, with a running sum and sum of squares.
  const DAY = 86_400_000
  const rows = [...m]
    .map(([d, v]) => ({ d, n: Date.parse(`${d}T00:00:00Z`) / DAY, v }))
    .sort((a, b) => a.n - b.n)
  const out: { at: string; value: number; low: number; high: number }[] = []
  let lo = 0
  let hi = 0
  let sum = 0
  let sq = 0
  for (const r of rows) {
    while (hi < rows.length && rows[hi].n < r.n) {
      sum += rows[hi].v
      sq += rows[hi].v ** 2
      hi++
    }
    while (lo < hi && rows[lo].n < r.n - 30) {
      sum -= rows[lo].v
      sq -= rows[lo].v ** 2
      lo++
    }
    const n = hi - lo
    if (n < 14) continue
    const mean = sum / n
    const sd = Math.sqrt(Math.max(0, sq / n - mean * mean))
    out.push({ at: r.d, value: mean, low: mean - sd, high: mean + sd })
  }
  return out
}

// The engine's prose is in Title Case headings with its own metric names
// inside sentences. The page speaks in its own labels.
const NAMES: [RegExp, string][] = [
  [/\bPace of Aging\b/g, "pace of aging"],
  [/\bSleep Consistency\b/g, "sleep consistency"],
  [/\bHours of Sleep\b/g, "hours of sleep"],
  [/\bTime in HR Zones 1-3\b/g, "zone 1-3 time"],
  [/\bTime in HR Zones 4-5\b/g, "zone 4-5 time"],
  [/\bStrength Activity Time\b/g, "strength time"],
  [/\bSteps\b/g, "steps"],
  [/VO₂ Max/g, "VO2 max"],
  [/\bResting Heart Rate\b/g, "resting heart rate"],
  [/\bRHR\b/g, "resting heart rate"],
  [/\bLean Body Mass\b/g, "lean body mass"],
]

/** The engine's sentences in the page's words: its metric names become the
 * page's labels and every sentence starts with a capital. */
export function pageText(s: string): string {
  let out = s
  for (const [re, to] of NAMES) out = out.replace(re, to)
  return out.replace(
    /(^|[.!?]\s+)([a-z])/g,
    (_, a: string, b: string) => a + b.toUpperCase()
  )
}

/** "Maintain Your Gains" to "Maintain your gains". */
export function sentenceCase(title: string): string {
  const t = pageText(title)
  return (
    t.charAt(0).toUpperCase() +
    t
      .slice(1)
      .replace(
        /\b([A-Z])([a-z])/g,
        (_, a: string, b: string) => a.toLowerCase() + b
      )
  )
}

/** A value's number and unit as this app prints them: the engine's format,
 * except VO2 max to one decimal (a target sits beside it, and a whole
 * number would print the value and the target alike) and steps in lower
 * case. */
export function valueParts(
  v: number,
  f: ValueFormat
): { num: string; unit: string } {
  if (f === "vo2") return { num: v.toFixed(1), unit: "ml/kg/min" }
  const p = formatValue(v, f)
  return f === "steps" ? { num: p.num, unit: "steps" } : p
}

/** A value as one string, "No data" for null. */
export function valueText(v: number | null, f: ValueFormat): string {
  if (v === null) return "No data"
  const { num, unit } = valueParts(v, f)
  return unit ? `${num} ${unit}` : num
}

/** Years with a sign, a real minus for negatives: "+0.3", "−0.5". */
export function impactText(years: number): string {
  return formatImpact(years).replace("-", "\u2212")
}

/** Pace with a real minus sign: "−0.4x". */
export function paceText(p: number): string {
  return `${p.toFixed(1).replace("-", "\u2212")}x`
}

/** The focus sentence in this app's value format. */
export function focusText(m: Metric): string {
  const impact = m.impact ?? 0
  const recent = m.impact30 ?? impact
  const head = `${valueText(m.thirtyDay, m.format)} this month, ${valueText(m.sixMonth, m.format)} over 6 months.`
  if (recent - impact >= 0.1)
    return `${head} Slipping: if it holds, it adds ${formatImpact(recent - impact).replace("+", "")} more years.`
  if (impact >= 0.05) return `${head} Below target, adding years now.`
  return head
}

/** A contributor's verdict for the page. Without data: say what would feed
 * it, over the look-back the contributor really has, or that no source
 * scores sleep consistency. */
export function contributorVerdict(m: Metric): Verdict {
  if (m.sixMonth === null || m.impact === null)
    return {
      title: "No data",
      body: m.source.startsWith("None of")
        ? `${m.source} It counts as 0 years.`
        : `${noDataText(m.id)}, so it counts as 0 years. It is fed by: ${CONTRIBUTOR_INPUTS[m.id].summary}.`,
    }
  return {
    title: sentenceCase(m.verdict.title),
    body: pageText(m.verdict.body),
  }
}

/** "Nothing in the last six months", or the last year for body readings,
 * which carry forward. */
export function noDataText(id: MetricId): string {
  return id === "vo2max" || id === "leanMass"
    ? `No reading in the last ${BODY_CARRY_FORWARD_DAYS} days`
    : "Nothing in the last six months"
}

/** The number and unit apart, for a chart's readout. */
export function chartFormat(f: ValueFormat): {
  unit: string
  value: (v: number) => string
} {
  return { unit: valueParts(1, f).unit, value: (v) => valueParts(v, f).num }
}

/** The decimals a step needs to print exactly: 2.5 needs 1, 0.25 needs 2. */
function decimalsOf(step: number): number {
  for (let d = 0; d < 4; d++) {
    const x = step * 10 ** d
    if (Math.abs(x - Math.round(x)) < 1e-6) return d
  }
  return 4
}

/** Axis ticks for a contributor. Steps print in thousands ("10K", "7.5K")
 * only when the step is a thousand or more, with the decimals the step
 * needs; a finer axis prints whole steps ("8,250"). Hours print "7:30";
 * the rest take the chart's default. */
export function tickFormat(
  f: ValueFormat
): ((v: number, step: number) => string) | undefined {
  if (f === "steps")
    return (v, step) =>
      step < 1000
        ? Math.round(v).toLocaleString("en-GB")
        : `${(v / 1000).toLocaleString("en-GB", {
            minimumFractionDigits: decimalsOf(step / 1000),
            maximumFractionDigits: decimalsOf(step / 1000),
          })}K`
  if (f === "hours") return (v) => formatValue(v, "hours").num
  return undefined
}

/** "5:00 h adds 2.3 years, 7:00 h adds nothing, 8:00 h removes 0.2 years". */
export function curveLine(m: Metric, target: number | undefined): string {
  const at = (v: number, text: string) => {
    const y = tenths(yearsAt(m.curve, v))
    const verb =
      y > 0
        ? `adds ${formatImpact(y).replace("+", "")} years`
        : y < 0
          ? `removes ${formatImpact(-y).replace("+", "")} years`
          : "adds nothing"
    return `${text} ${verb}`
  }
  const value = (v: number) => valueText(v, m.format)
  const parts = [at(m.scale.min, value(m.scale.min))]
  if (target !== undefined) parts.push(`${value(target)} adds nothing`)
  parts.push(at(m.scale.max, value(m.scale.max)))
  return `${parts.join(", ")}.`
}

/** The mean of a daily series over the `days` days ending `today`. */
export function meanOver(
  m: Daily,
  today: string,
  days: number
): number | undefined {
  const from = new Date(
    Date.parse(`${today}T00:00:00Z`) - (days - 1) * 86_400_000
  )
    .toISOString()
    .slice(0, 10)
  let sum = 0
  let n = 0
  for (const [d, v] of m)
    if (d >= from && d <= today) {
      sum += v
      n++
    }
  return n ? sum / n : undefined
}

/** The newest day of a series not after `today`, with its value. */
export function latest(
  m: Daily,
  today: string
): { day: string; value: number } | undefined {
  let best: string | undefined
  for (const d of m.keys()) if (d <= today && (!best || d > best)) best = d
  return best ? { day: best, value: m.get(best) as number } : undefined
}

/** The mean of the `days` days ending each day (that day included), from
 * the day the window holds 14 values: the baseline line under a daily
 * chart. One sliding pass over the sorted days; only days from `from` on
 * are returned (the window before `from` still counts). */
export function rollingMean(
  m: Daily,
  days = 30,
  from?: string
): { at: string; value: number }[] {
  const DAY = 86_400_000
  const start = from
    ? Date.parse(`${from}T00:00:00Z`) / DAY - days
    : Number.NEGATIVE_INFINITY
  const rows = [...m]
    .map(([d, v]) => ({ d, n: Date.parse(`${d}T00:00:00Z`) / DAY, v }))
    .filter((r) => r.n > start)
    .sort((a, b) => a.n - b.n)
  const out: { at: string; value: number }[] = []
  let lo = 0
  let sum = 0
  for (let hi = 0; hi < rows.length; hi++) {
    sum += rows[hi].v
    while (rows[lo].n <= rows[hi].n - days) sum -= rows[lo++].v
    const n = hi - lo + 1
    if (n >= 14 && (!from || rows[hi].d >= from))
      out.push({ at: rows[hi].d, value: sum / n })
  }
  return out
}

/** The sum of a daily series from this week's Monday to `today`. */
export function weekSoFar(m: Daily, today: string): number {
  const dow = (new Date(`${today}T00:00:00Z`).getUTCDay() + 6) % 7
  const monday = new Date(Date.parse(`${today}T00:00:00Z`) - dow * 86_400_000)
    .toISOString()
    .slice(0, 10)
  let sum = 0
  for (const [d, v] of m) if (d >= monday && d <= today) sum += v
  return sum
}
