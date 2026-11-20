// Years each contributor adds (+) or removes (−), from its average.
//
// The targets and the method (10 × ln of the mortality hazard ratio) are
// WHOOP's, from the Healthspan white paper. The curve shapes and overlap
// shrinkage are not published; these are fitted so that every (value, impact)
// pair visible in published screenshots of the app comes out at the
// displayed tenth of a year.

import type { MetricId } from "./report"

export type Sex = "male" | "female"

/** Nightly sleep consistency, percent, that adds no years. */
export const SLEEP_CONSISTENCY_NEUTRAL = 70
/** Hours asleep a night that add no years. */
export const SLEEP_HOURS_NEUTRAL = 7
/** Weekly strength minutes that add no years, and those that earn the full
 * credit. */
export const STRENGTH_NEUTRAL_MIN = 40
export const STRENGTH_FULL_CREDIT_MIN = 120

const clamp = (x: number, lo: number, hi: number) =>
  Math.min(hi, Math.max(lo, x))

/** Nightly sleep consistency, percent. Neutral at 70. */
export function sleepConsistencyYears(pct: number): number {
  return clamp(0.13 * (SLEEP_CONSISTENCY_NEUTRAL - pct), -2.5, 4)
}

/** Hours asleep a night. Short sleep costs about 1.1 years an hour; 7 to 9
 * hours earns a small credit; past 9 hours the white paper finds no effect. */
export function sleepHoursYears(h: number): number {
  if (h < SLEEP_HOURS_NEUTRAL)
    return 1.13 * Math.min(3, SLEEP_HOURS_NEUTRAL - h)
  if (h <= 8) return -0.2 * (h - SLEEP_HOURS_NEUTRAL)
  if (h <= 9) return -0.2
  return 0
}

/** Weekly minutes in zones 1-3 the age's target asks for: 100 when young,
 * 70 by 70. */
export function zones13Target(age: number): number {
  return clamp(100 - 0.75 * (age - 30), 70, 100)
}

const lnHrModerate = (min: number) =>
  -0.179 * Math.log(1 + Math.min(min, 600) / 50)

export function zones13Years(minPerWeek: number, age: number): number {
  return 5.8 * (lnHrModerate(minPerWeek) - lnHrModerate(zones13Target(age)))
}

export function zones45Target(age: number): number {
  return clamp(10 - 0.075 * (age - 30), 7, 10)
}

/** Vigorous minutes overlap heavily with zones 1-3 and VO2 max, so their own
 * weight is small: about a tenth of a year either way. */
export function zones45Years(minPerWeek: number, age: number): number {
  const t = zones45Target(age)
  if (minPerWeek < t) return 0.15 * (1 - minPerWeek / t)
  return -0.1 * Math.min(1, (minPerWeek - t) / 50)
}

/** Weekly strength minutes: neutral at 40, the full credit by 120. */
export function strengthYears(minPerWeek: number): number {
  const m = Math.max(0, minPerWeek)
  // Concave below 40: the owner's 0:36 shows +0.2, and none at all still
  // fits the white paper's average-adult anchors.
  const neutral = STRENGTH_NEUTRAL_MIN
  const full = STRENGTH_FULL_CREDIT_MIN
  if (m <= neutral) return 1.2 * ((neutral - m) / neutral) ** 0.75
  if (m <= full) return -1.6 * ((m - neutral) / (full - neutral))
  return -1.6
}

// Paluch 2022 quartile medians and hazard ratios.
const STEPS: [number, number][] = [
  [3553, 0],
  [5801, Math.log(0.6)],
  [7842, Math.log(0.55)],
  [10901, Math.log(0.47)],
]

// Below the lowest quartile the first segment's slope continues, to a cap.
const FIRST_SLOPE = STEPS[1][1] / (STEPS[1][0] - STEPS[0][0])

function lnHrSteps(s: number): number {
  if (s <= STEPS[0][0]) return Math.min(0.3, FIRST_SLOPE * (s - STEPS[0][0]))
  for (let i = 1; i < STEPS.length; i++) {
    const [x1, y1] = STEPS[i]
    if (s <= x1) {
      const [x0, y0] = STEPS[i - 1]
      return y0 + ((y1 - y0) * (s - x0)) / (x1 - x0)
    }
  }
  return STEPS[STEPS.length - 1][1]
}

export function stepsTarget(age: number): number {
  return age >= 60 ? 5600 : 8000
}

/** Daily steps, measured against 8,000 (5,600 from 60) by rescaling. */
export function stepsYears(steps: number, age: number): number {
  const scaled = steps * (8000 / stepsTarget(age))
  return 3.7 * (lnHrSteps(scaled) - lnHrSteps(8000))
}

// WHOOP white paper Fig. 4: minimum VO2 max by age 20, 25, ... 100.
const VO2: Record<Sex, number[]> = {
  male: [46, 46, 44, 42, 40, 37, 36, 34, 32, 30, 29, 27, 26, 25, 23, 22, 21],
  female: [40, 40, 38, 36, 34, 32, 30, 29, 27, 26, 25, 23, 22, 21, 20, 19, 18],
}

export function vo2Target(age: number, sex: Sex): number {
  const t = VO2[sex]
  const pos = clamp((age - 20) / 5, 0, t.length - 1)
  const i = Math.floor(pos)
  const f = pos - i
  return i + 1 < t.length ? t[i] + (t[i + 1] - t[i]) * f : t[i]
}

/** ml/kg/min. 13% lower mortality per MET (3.5 ml/kg/min) is about 0.4
 * years a point; the male slope is fitted to a screenshot (45 → +0.5), the
 * female one to the white paper's average-US-woman anchor. The credit for
 * exceeding the target is capped: nothing published constrains it. */
export function vo2Years(v: number, age: number, sex: Sex): number {
  // Short of the target costs more than the same distance past it earns:
  // 45 against 46 shows +0.5, 44 against 38.3 shows -1.7.
  const gap = clamp(vo2Target(age, sex) - v, -14, 14)
  const slope = gap > 0 ? (sex === "male" ? 0.45 : 0.3) : 0.3
  return Math.max(-3, slope * gap)
}

export function rhrTarget(sex: Sex): number {
  return sex === "male" ? 60 : 64
}

/** +9% mortality per 10 bpm, fitted steeper to a screenshot (69 → +1.0).
 * The credit below target is capped. */
export function rhrYears(bpm: number, sex: Sex): number {
  // 69 bpm shows +1.0 and 56 bpm shows -0.5 (male target 60): the credit
  // below the target is a little steeper than the cost above it.
  const gap = clamp(bpm - rhrTarget(sex), -30, 30)
  return Math.max(-1.5, (gap > 0 ? 0.11 : 0.125) * gap)
}

export function leanTarget(age: number, sex: Sex): number {
  return (sex === "male" ? 80 : 67) - 0.1 * Math.max(0, age - 30)
}

/** Lean body mass, percent of body weight. Only ever adds years. */
export function leanYears(pct: number, age: number, sex: Sex): number {
  return Math.max(0, 0.1 * (leanTarget(age, sex) - pct))
}

/** The value of each contributor that adds no years, in the units its bar
 * and averages use: percent, hours a night, hours a week (zones and
 * strength), steps a day, ml/kg/min, bpm, percent of body weight. */
export function targetFor(id: MetricId, age: number, sex: Sex): number {
  switch (id) {
    case "sleepConsistency":
      return SLEEP_CONSISTENCY_NEUTRAL
    case "sleepHours":
      return SLEEP_HOURS_NEUTRAL
    case "zones13":
      return zones13Target(age) / 60
    case "zones45":
      return zones45Target(age) / 60
    case "strength":
      return STRENGTH_NEUTRAL_MIN / 60
    case "steps":
      return stepsTarget(age)
    case "vo2max":
      return vo2Target(age, sex)
    case "rhr":
      return rhrTarget(sex)
    case "leanMass":
      return leanTarget(age, sex)
  }
}
