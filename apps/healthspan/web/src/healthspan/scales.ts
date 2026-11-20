// The scale of every Healthspan card: the bar's segments, each with a tone, a level and a label that
// states its range and meaning, plus the ticks and the end labels. The
// contributors' bars are cut at their target by `contributorScale`. Every
// other scale cites its source in lib/content/healthspan.ts.

import type { ZoneBarLevel, ZoneBarSegment, ZoneBarTick } from "../ui/ZoneBar"
import type { Sex } from "../engine/curves"
import { yearsAt } from "../engine/engine"
import { tenths } from "../engine/format"
import type { Metric } from "../engine/report"
import { valueText } from "./healthspanMetrics"

export type ScaleSegment = ZoneBarSegment

export interface Scale {
  min: number
  max: number
  segments: ScaleSegment[]
  /** A target or threshold tick, or a personal baseline (`variant:
   * "baseline"`, drawn hollow). */
  ticks: ZoneBarTick[]
  minLabel: string
  maxLabel: string
}

const lvl = (n: number): ZoneBarLevel =>
  Math.min(4, Math.max(1, Math.round(n))) as ZoneBarLevel

/** `count` equal segments over [min, max]; `at(lo, hi, i)` gives each. */
function segmentsOf(
  min: number,
  max: number,
  count: number,
  at: (lo: number, hi: number, i: number) => ScaleSegment
): ScaleSegment[] {
  const w = (max - min) / count
  return Array.from({ length: count }, (_, i) =>
    at(min + i * w, min + (i + 1) * w, i)
  )
}

const fmt = (v: number, d = 0) =>
  v.toLocaleString("en-GB", {
    minimumFractionDigits: d,
    maximumFractionDigits: d,
  })

/** "+0.20 °C" with a real minus sign. */
export function signed(v: number, d: number, unit = ""): string {
  const abs = fmt(Math.abs(v), d)
  // A value that rounds to zero has no sign: "0.00", never "−0.00".
  const sign = /^[0.,]+$/.test(abs) ? "" : v > 0 ? "+" : "−"
  const s = `${sign}${abs}`
  return unit ? `${s}${unit}` : s
}

// ---- recovery ------------------------------------------------------------

export const RECOVERY_BANDS = [
  { from: 1, to: 33, tone: "danger", word: "red: rest or active recovery" },
  {
    from: 34,
    to: 66,
    tone: "warning",
    word: "yellow: ready for moderate strain",
  },
  {
    from: 67,
    to: 100,
    tone: "success",
    word: "green: primed for a high-strain day",
  },
] as const

/** WHOOP's bands: red 1 to 33, yellow 34 to 66, green 67 to 100, in 33
 * segments of three. */
export function recoveryScale(): Scale {
  return {
    min: 1,
    max: 100,
    segments: segmentsOf(1, 100, 33, (lo, _hi, i) => {
      const from = Math.round(lo)
      const to = i === 32 ? 100 : from + 2
      const b =
        RECOVERY_BANDS.find((x) => from >= x.from && from <= x.to) ??
        RECOVERY_BANDS[2]
      return {
        tone: b.tone,
        level: 3,
        label: `Recovery ${from} to ${to}%, ${b.word}`,
      }
    }),
    ticks: [],
    minLabel: "1%",
    maxLabel: "100%",
  }
}

// ---- sleep performance -----------------------------------------------------

/** WHOOP's bands: 85% and above, 70 to 84%, under 70%; the words are this
 * app's. */
export function sleepPerformanceScale(): Scale {
  return {
    min: 0,
    max: 100,
    segments: segmentsOf(0, 100, 20, (lo) => {
      const band =
        lo >= 85
          ? { tone: "success" as const, word: "85% and above (optimal)" }
          : lo >= 70
            ? { tone: "warning" as const, word: "70 to 84% (sufficient)" }
            : { tone: "danger" as const, word: "under 70% (poor)" }
      return {
        tone: band.tone,
        level: 3,
        label: `Sleep performance ${fmt(lo)} to ${fmt(lo >= 95 ? 100 : lo + 4)}%, ${band.word}`,
      }
    }),
    ticks: [],
    minLabel: "0%",
    maxLabel: "100%",
  }
}

// ---- strain -------------------------------------------------------------------

export const STRAIN_BANDS = [
  { from: 0, to: 9, level: 1, word: "Light: minimal stress" },
  { from: 10, to: 13, level: 2, word: "Moderate: maintains fitness" },
  { from: 14, to: 17, level: 3, word: "High: builds fitness" },
  { from: 18, to: 21, level: 4, word: "All out: hard to recover from" },
] as const

export function strainBand(v: number) {
  const n = Math.floor(v)
  return STRAIN_BANDS.find((b) => n >= b.from && n <= b.to) ?? STRAIN_BANDS[3]
}

/** WHOOP's 0 to 21 scale in four categories, accent levels: no good or bad
 * tone, since strain is load, not an outcome. */
export function strainScale(): Scale {
  return {
    min: 0,
    max: 21,
    segments: segmentsOf(0, 21, 21, (lo) => {
      const b = strainBand(lo)
      return {
        tone: "accent",
        level: b.level,
        label: `Strain ${fmt(lo)} to ${fmt(lo + 1)}, ${b.word}`,
      }
    }),
    ticks: [],
    minLabel: "0",
    maxLabel: "21",
  }
}

// ---- personal (baseline-relative) scales ------------------------------------------

/** HRV against the 30-night baseline: three standard deviations either
 * side in half-SD segments; below one SD is the app's "Depressed" side.
 * Undefined without a baseline. */
export function hrvScale(
  mean: number | undefined,
  sd: number | undefined
): Scale | undefined {
  if (mean === undefined || !sd || !(sd > 0) || !(mean > 0)) return undefined
  // Three deviations either side, fewer (in half steps) when three below
  // would pass 0 ms, so the scale stays centred on the baseline.
  const k = Math.max(0.5, Math.min(3, Math.floor((mean / sd) * 2) / 2))
  const min = mean - k * sd
  const max = mean + k * sd
  const zero = (v: number) => Math.abs(v) + 0
  return {
    min,
    max,
    segments: segmentsOf(min, max, k * 4, (lo, hi, i) => {
      const z = -k + i * 0.5
      const zEnd = z + 0.5
      const away = z >= 0 ? z : -zEnd
      const words =
        z >= 0
          ? `${fmt(zero(z), 1)} to ${fmt(zero(zEnd), 1)} SD above your baseline`
          : `${fmt(zero(zEnd), 1)} to ${fmt(zero(z), 1)} SD below your baseline`
      const tone = zEnd <= -1 ? "warning" : z >= 1 ? "success" : "neutral"
      return {
        tone,
        level: tone === "neutral" ? undefined : lvl((away - 1) * 2 + 1),
        label: `${fmt(lo)} to ${fmt(hi)} ms, ${words}`,
      }
    }),
    ticks: [
      { at: mean, label: `baseline ${fmt(mean)} ms`, variant: "baseline" },
    ],
    minLabel: `${fmt(min)} ms`,
    maxLabel: `${fmt(max)} ms`,
  }
}

/** Breathing rate, 8 to 24 a minute: 12 to 20 is the adult resting range;
 * outside it the tone deepens away from it. A tick at the baseline. */
export function breathingScale(baselineMean?: number): Scale {
  return {
    min: 8,
    max: 24,
    segments: segmentsOf(8, 24, 16, (lo) => {
      // 12 and 20 are in the range, so the segments either side of it name
      // their ends as "under 12" and "over 20".
      if (lo < 12)
        return {
          tone: "warning",
          level: lvl(lo < 10 ? 4 : lo < 11 ? 3 : 2),
          label: `${fmt(lo)} to ${lo === 11 ? "under 12" : fmt(lo + 1)}/min, under the adult resting range`,
        }
      if (lo < 20)
        return {
          tone: "success",
          level: 3,
          label: `${fmt(lo)} to ${fmt(lo + 1)}/min, within the adult resting range`,
        }
      return {
        tone: "warning",
        level: lvl(lo < 21 ? 2 : lo < 22 ? 3 : 4),
        label: `${lo === 20 ? "over 20" : fmt(lo)} to ${fmt(lo + 1)}/min, above the adult resting range`,
      }
    }),
    ticks:
      baselineMean === undefined
        ? []
        : [
            {
              at: baselineMean,
              label: `baseline ${fmt(baselineMean, 1)}/min`,
              variant: "baseline",
            },
          ],
    minLabel: "8/min",
    maxLabel: "24/min",
  }
}

/** Blood oxygen, 90 to 100%: under 95 is low (the app's rule, 083). */
export function spo2Scale(): Scale {
  return {
    min: 90,
    max: 100,
    segments: segmentsOf(90, 100, 10, (lo) =>
      lo < 95
        ? {
            tone: "warning",
            level: lvl(lo < 92 ? 4 : lo < 93 ? 3 : lo < 94 ? 2 : 1),
            label: `${fmt(lo)} to ${fmt(lo + 1)}%, under 95% (low)`,
          }
        : {
            tone: "success",
            level: 3,
            label: `${fmt(lo)} to ${fmt(lo + 1)}%, the normal range`,
          }
    ),
    ticks: [{ at: 95, label: "95%" }],
    minLabel: "90%",
    maxLabel: "100%",
  }
}

/** Skin temperature as the deviation from the 30-night baseline, minus 1.5
 * to plus 1.5 °C in quarter degrees: usual within 0.25, deepening beyond. */
export function skinScale(baselineMean?: number): Scale {
  return {
    min: -1.5,
    max: 1.5,
    segments: segmentsOf(-1.5, 1.5, 12, (lo, hi) => {
      const away = Math.max(Math.abs(lo), Math.abs(hi))
      const range = `${signed(lo, 2)} to ${signed(hi, 2)} °C`
      if (away <= 0.25 + 1e-9)
        return { tone: "neutral", label: `${range}, your usual` }
      return {
        tone: "warning",
        level: lvl(
          away <= 0.5 + 1e-9
            ? 1
            : away <= 0.75 + 1e-9
              ? 2
              : away <= 1 + 1e-9
                ? 3
                : 4
        ),
        label: `${range}, ${lo >= 0 ? "above" : "below"} your baseline`,
      }
    }),
    ticks: [
      {
        at: 0,
        label:
          baselineMean === undefined
            ? "baseline"
            : `baseline ${fmt(baselineMean, 2)} °C`,
        variant: "baseline",
      },
    ],
    minLabel: "−1.5 °C",
    maxLabel: "+1.5 °C",
  }
}

// ---- body -------------------------------------------------------------------------

export const BMI_BANDS = [
  { below: 18.5, tone: "danger", level: 2, word: "underweight" },
  { below: 25, tone: "success", level: 3, word: "healthy weight" },
  { below: 30, tone: "warning", level: 2, word: "overweight" },
  { below: 35, tone: "danger", level: 3, word: "obese" },
  { below: Number.POSITIVE_INFINITY, tone: "danger", level: 4, word: "obese" },
] as const

export function bmiBand(bmi: number) {
  return BMI_BANDS.find((b) => bmi < b.below) ?? BMI_BANDS[4]
}

/** Weight on the BMI scale (15 to 40, one segment per BMI unit), in
 * kilograms at `heightM`. Undefined without a height. */
export function bmiScale(heightM: number | undefined): Scale | undefined {
  if (!heightM || !(heightM > 0.5)) return undefined
  const h2 = heightM * heightM
  const kg = (bmi: number) => bmi * h2
  return {
    min: kg(15),
    max: kg(40),
    // Half-unit segments, so 18.5, 25 and 30 fall on edges.
    segments: segmentsOf(15, 40, 50, (lo, hi) => {
      const b = bmiBand((lo + hi) / 2)
      return {
        tone: b.tone,
        level: b.level,
        label: `BMI ${fmt(lo, 1)} to ${fmt(hi, 1)}, ${fmt(kg(lo), 1)} to ${fmt(kg(hi), 1)} kg, ${b.word}`,
      }
    }),
    ticks: [18.5, 25, 30].map((bmi) => ({
      at: kg(bmi),
      label: `BMI ${fmt(bmi, bmi % 1 ? 1 : 0)}`,
    })),
    minLabel: `${fmt(kg(15))} kg`,
    maxLabel: `${fmt(kg(40))} kg`,
  }
}

/** Gallagher et al. 2000, Am J Clin Nutr 72:694-701, Table 4: the body fat
 * percent at the start of the healthy, overweight and obese bands, by sex
 * and age (20 to 39, 40 to 59, 60 to 79). */
export const BODY_FAT_BANDS: Record<
  Sex,
  { ages: [number, number]; cuts: [number, number, number] }[]
> = {
  male: [
    { ages: [20, 39], cuts: [8, 20, 25] },
    { ages: [40, 59], cuts: [11, 22, 28] },
    { ages: [60, 79], cuts: [13, 25, 30] },
  ],
  female: [
    { ages: [20, 39], cuts: [21, 33, 39] },
    { ages: [40, 59], cuts: [23, 34, 40] },
    { ages: [60, 79], cuts: [24, 36, 42] },
  ],
}

/** The band row for a sex and age; under 20 takes 20 to 39 and 80 and
 * over takes 60 to 79 (the table's ends). */
export function bodyFatCuts(sex: Sex, age: number): [number, number, number] {
  const rows = BODY_FAT_BANDS[sex]
  const row = age < 40 ? rows[0] : age < 60 ? rows[1] : rows[2]
  return row.cuts
}

export function bodyFatBand(pct: number, sex: Sex, age: number) {
  const [healthy, over, obese] = bodyFatCuts(sex, age)
  if (pct < healthy)
    return {
      tone: "danger" as const,
      level: 1 as const,
      word: "below the healthy range",
    }
  if (pct < over)
    return { tone: "success" as const, level: 3 as const, word: "healthy" }
  if (pct < obese)
    return { tone: "warning" as const, level: 2 as const, word: "overweight" }
  return { tone: "danger" as const, level: 3 as const, word: "obese" }
}

/** Body fat 5 to 45% in one-point segments, banded for the sex and age.
 * Undefined without both. */
export function bodyFatScale(
  sex: Sex | undefined,
  age: number | undefined
): Scale | undefined {
  if (!sex || age === undefined) return undefined
  const [healthy, over, obese] = bodyFatCuts(sex, age)
  return {
    min: 5,
    max: 45,
    segments: segmentsOf(5, 45, 40, (lo) => {
      const b = bodyFatBand(lo + 0.5, sex, age)
      return {
        tone: b.tone,
        level: b.level,
        label: `${fmt(lo)} to ${fmt(lo + 1)}%, ${b.word}`,
      }
    }),
    ticks: [healthy, over, obese].map((at) => ({ at, label: `${fmt(at)}%` })),
    minLabel: "5%",
    maxLabel: "45%",
  }
}

/** Tanita's visceral fat rating, 1 to 20 shown (the scale runs to 59): 1 to
 * 12 healthy, 13 and over excess. */
export function visceralScale(): Scale {
  return {
    min: 1,
    max: 20,
    segments: segmentsOf(1, 20, 19, (lo) => {
      const r = Math.round(lo)
      return r <= 12
        ? { tone: "success", level: 3, label: `Rating ${r}, healthy` }
        : { tone: "warning", level: 3, label: `Rating ${r}, excess` }
    }),
    ticks: [{ at: 13, label: "13" }],
    minLabel: "1",
    maxLabel: "20",
  }
}

// ---- contributors ---------------------------------------------------------------

/** Years within this of zero, over a whole segment, are "no effect"
 * (`NEUTRAL_YEARS` in segmentTones.ts). */
const NO_EFFECT = 0.025

/** Equal segments whose edges include the target: the engine's range in
 * the first count from 10 to 30 that puts the target on an edge, else the
 * range widened past `max` by under one segment. */
function gridAt(
  min: number,
  max: number,
  target: number
): { max: number; count: number } {
  for (let n = 10; n <= 30; n++) {
    const k = ((target - min) / (max - min)) * n
    if (Math.abs(k - Math.round(k)) < 1e-6) return { max, count: n }
  }
  let best = { max: Number.POSITIVE_INFINITY, count: 10 }
  for (let k = 1; k <= 30; k++) {
    const w = (target - min) / k
    if (!(w > 0)) break
    const n = Math.ceil((max - min) / w - 1e-9)
    if (n < 8 || n > 30) continue
    const end = min + n * w
    if (end < best.max) best = { max: end, count: n }
  }
  return Number.isFinite(best.max) ? best : { max, count: 10 }
}

/** The years over a segment, its low end first: "adds 0.7 to 0.1 years",
 * "adds up to 0.2 years", "removes 0.3 years". */
function yearsWords(a: number, b: number): string {
  const x = Math.abs(tenths(a)) + 0
  const y = Math.abs(tenths(b)) + 0
  const verb = a + b > 0 ? "adds" : "removes"
  if (x === y) return `${verb} ${x.toFixed(1)} years`
  if (Math.min(x, y) === 0)
    return `${verb} up to ${Math.max(x, y).toFixed(1)} years`
  return `${verb} ${x.toFixed(1)} to ${y.toFixed(1)} years`
}

/** A contributor's bar. With a target:
 * equal segments with the target on an edge, so no segment straddles it;
 * each labelled with the years at both its ends, grey only where the years
 * stay within 0.025 of zero over the whole segment, orange where it adds,
 * teal where it removes, deeper with the years at its midpoint. Without
 * one (no birth date and sex) the engine's range, grey, labels without
 * years: the curve would be a stand-in person's. */
export function contributorScale(m: Metric, target?: number): Scale {
  // Widen past the engine's end to hold both averages, so no marker
  // overflows the bar (an overflow pointer sits lower than the others).
  const reach = Math.max(
    m.scale.max,
    ...[m.sixMonth, m.thirtyDay].filter((v): v is number => v !== null)
  )
  const { min } = m.scale
  const text = (v: number) => valueText(v, m.format)
  if (target === undefined || !(target > min) || !(target < m.scale.max)) {
    const w = (m.scale.max - min) / m.scale.segments.length
    const n =
      m.scale.segments.length +
      (reach > m.scale.max ? Math.ceil((reach - m.scale.max) / w - 1e-9) : 0)
    const end = min + n * w
    return {
      min,
      max: end,
      segments: Array.from({ length: n }, (_, i) => ({
        tone: "neutral" as const,
        label:
          target === undefined
            ? `${text(min + i * w)} to ${text(min + (i + 1) * w)}, no target without your birth date and sex`
            : `${text(min + i * w)} to ${text(min + (i + 1) * w)}, ${yearsWords(yearsAt(m.curve, min + i * w), yearsAt(m.curve, min + (i + 1) * w))}`,
      })),
      ticks: [],
      minLabel: m.scale.minLabel,
      maxLabel: end === m.scale.max ? m.scale.maxLabel : text(end),
    }
  }
  const base = gridAt(min, m.scale.max, target)
  const w = (base.max - min) / base.count
  const extra = reach > base.max ? Math.ceil((reach - base.max) / w - 1e-9) : 0
  const grid = { max: base.max + extra * w, count: base.count + extra }
  const spans = Array.from({ length: grid.count }, (_, i) => {
    const lo = min + i * w
    const hi = lo + w
    const ys = [0, 0.25, 0.5, 0.75, 1].map((t) => yearsAt(m.curve, lo + t * w))
    return { lo, hi, ys, mid: ys[2] }
  })
  const worst = Math.max(0, ...spans.map((s) => s.mid))
  const best = Math.min(0, ...spans.map((s) => s.mid))
  const level = (r: number): ZoneBarLevel => lvl(Math.ceil(r * 4 - 1e-9))
  return {
    min,
    max: grid.max,
    segments: spans.map(({ lo, hi, ys, mid }) => {
      const range = `${text(lo)} to ${text(hi)}`
      if (ys.every((y) => Math.abs(y) < NO_EFFECT))
        return { tone: "neutral" as const, label: `${range}, no effect` }
      const adds = ys[0] + ys[4] > 0
      return {
        tone: adds ? ("warning" as const) : ("success" as const),
        level: adds
          ? level(Math.max(mid, 0) / (worst || 1))
          : level(Math.min(mid, 0) / (best || -1)),
        label: `${range}, ${yearsWords(ys[0], ys[4])}`,
      }
    }),
    ticks: [{ at: target, label: `target ${text(target)}` }],
    minLabel: m.scale.minLabel,
    maxLabel: grid.max === m.scale.max ? m.scale.maxLabel : text(grid.max),
  }
}
