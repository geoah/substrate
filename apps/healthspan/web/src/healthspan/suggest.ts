// One suggestion per Healthspan card: a
// sentence under 90 characters chosen from a table by the card's current
// band and its trend, with a basis sentence of the numbers behind it.
// General wellness wording only: never a diagnosis, a medicine or a dose;
// only breathing rate and blood oxygen mention a GP, and only when their
// finding has fired.

import type { Sex } from "../engine/curves"
import { ADDS_YEARS, isSlipping, SLIPPING } from "../engine/focus"
import type { Metric, MetricId } from "../engine/report"
import { valueText, type SignalId } from "./healthspanMetrics"
import {
  bmiBand,
  bodyFatBand,
  RECOVERY_BANDS,
  signed,
  strainBand,
} from "./scales"

export interface Suggestion {
  /** One sentence, under 90 characters. */
  text: string
  /** The numbers behind it, one sentence. */
  basis: string
}

/** The limit every suggestion stays under. */
export const SUGGESTION_MAX = 90

export type Trend = "better" | "worse" | "steady"

const fmt = (v: number, d = 0) =>
  v.toLocaleString("en-GB", {
    minimumFractionDigits: d,
    maximumFractionDigits: d,
  })

/** Adds `tail` when the whole stays under the limit. */
function withTail(text: string, tail: string): string {
  return `${text}${tail}`.length < SUGGESTION_MAX ? `${text}${tail}` : text
}

/** A contributor's trend, read by the years as the Slipping badge is:
 * worse when the 30-day impact is `SLIPPING` years or more above the
 * 6-month one (`isSlipping`), better when as far below it, else steady. So
 * the badge, the note and the info say one thing */
export function contributorTrend(m: Metric): Trend {
  if (m.impact === null || m.impact30 === null) return "steady"
  if (isSlipping(m)) return "worse"
  if (m.impact - m.impact30 >= SLIPPING) return "better"
  return "steady"
}

/** Which way the 30-day average moved against the 6-month one, by value
 * (steady within 2 percent): for a card without a target, where years
 * would be a stand-in person's. */
export function valueChange(m: Metric): "rose" | "fell" | "held" {
  if (m.sixMonth === null || m.thirtyDay === null || m.sixMonth === 0)
    return "held"
  const r = (m.thirtyDay - m.sixMonth) / Math.abs(m.sixMonth)
  return Math.abs(r) <= 0.02 ? "held" : r > 0 ? "rose" : "fell"
}

const trendWord: Record<Trend, string> = {
  better: "improving",
  worse: "slipping",
  steady: "steady",
}

/** The trend as the info and the basis say it: "slipping", "improving" or
 * "steady" with a target, "rose this month" without one. */
export function trendWords(m: Metric, hasTargets: boolean): string {
  return hasTargets
    ? trendWord[contributorTrend(m)]
    : `${valueChange(m)} this month`
}

export interface ContributorInput {
  metric: Metric
  /** The card's label ("Hours of sleep"). */
  label: string
  /** False without a birth date and sex: no target and no years. */
  hasTargets: boolean
  /** The value that adds no years; undefined without a profile. */
  target?: number
  /** VO2 max with a single reading in the record. */
  oneReading?: boolean
  /** Resting heart rate's "Elevated" finding fired. */
  elevated?: boolean
  /** Lean body mass's scan is stale or missing. */
  stale?: boolean
}

type Rule = (adds: boolean, trend: Trend) => string

const CONTRIBUTOR_RULES: Record<MetricId, Rule> = {
  sleepConsistency: (adds, t) =>
    adds
      ? t === "worse"
        ? "Bed and wake times are drifting; pick one wake time and keep it all week."
        : "Consistency is under 70%; the same wake time at weekends is the lever."
      : t === "worse"
        ? "Consistency slipped this month; hold your usual wake time."
        : "Consistency is helping; keep the same wake time.",
  sleepHours: (adds, t) =>
    adds
      ? t === "worse"
        ? "Sleep is short and getting shorter; move bedtime earlier by 30 minutes."
        : "Under 7 hours a night adds years; protect a 7 to 8 hour window."
      : t === "worse"
        ? "Nights are getting shorter this month; guard your bedtime."
        : "Sleep length is where it helps; keep it.",
  zones13: (adds, t) =>
    adds
      ? "Easy aerobic time is under target; add two 30-minute brisk walks or rides a week."
      : t === "worse"
        ? "Easy aerobic time fell this month; keep the weekly volume up."
        : "Zone 1 to 3 time is helping; keep the weekly volume.",
  zones45: (adds, t) =>
    adds
      ? "Hard efforts are under target; one short interval session a week covers it."
      : t === "worse"
        ? "Hard efforts fell this month; keep one interval session a week."
        : "Hard-effort time is on track.",
  strength: (adds, t) =>
    adds
      ? "Strength time is under 40 minutes a week; two 20-minute sessions reach the target."
      : t === "worse"
        ? "Strength time fell this month; keep two sessions a week."
        : "Strength time is helping; keep two sessions a week.",
  steps: (adds, t) =>
    adds
      ? "Steps are under target; a 20-minute walk adds about 2,000."
      : t === "worse"
        ? "Steps fell this month; keep a daily walk."
        : "Steps are on track.",
  vo2max: (adds) =>
    adds
      ? "VO2 max is under target for your age; zone 2 and some intervals raise it over months."
      : "VO2 max is at or above target; keep the aerobic volume.",
  rhr: (adds) =>
    adds
      ? "Resting heart rate is above target; sleep, fewer late drinks and aerobic work lower it."
      : "Resting heart rate is at or below target.",
  leanMass: (adds) =>
    adds
      ? "Lean mass is under target; protein with each meal and strength work preserve it."
      : "Lean mass is at target.",
}

export function contributorSuggestion(i: ContributorInput): Suggestion {
  const m = i.metric
  const trend = contributorTrend(m)
  const adds = i.hasTargets && m.impact30 !== null && m.impact30 >= ADDS_YEARS
  const thirty = valueText(m.thirtyDay, m.format)
  const six = valueText(m.sixMonth, m.format)
  const target =
    i.hasTargets && i.target !== undefined
      ? `, against a target of ${valueText(i.target, m.format)}`
      : ""
  const basis =
    m.thirtyDay === null
      ? "No data in the window."
      : `30-day ${thirty}${target}; 6-month ${six}; ${trendWords(m, i.hasTargets)}.`
  if (m.id === "vo2max" && i.oneReading)
    return {
      text: "One reading only; more readings from your source would show a trend.",
      basis: `One reading in the record (${thirty}); the averages carry it forward.`,
    }
  if (m.id === "rhr" && i.elevated && !adds)
    return {
      text: "Resting heart rate is up this week; take an easier day and watch sleep.",
      basis: `The last 7 nights sit more than one deviation above the 30-night baseline; ${basis}`,
    }
  if (m.id === "leanMass" && (i.stale || m.sixMonth === null))
    return {
      text: "A new body composition scan would refresh this.",
      basis: "The newest scan is older than a year, or there is none.",
    }
  // Without a target, only the direction: no years, no target (item 1).
  if (!i.hasTargets)
    return {
      text: `${i.label} ${valueChange(m)} this month; add your birth date and sex for a target.`,
      basis,
    }
  return { text: CONTRIBUTOR_RULES[m.id](adds, trend), basis }
}

// ---- signals ---------------------------------------------------------------------

export type { SignalId }

export interface SignalInput {
  id: SignalId
  /** The newest day's or night's value. */
  latest?: number
  /** 30-day and 6-month means. */
  m30?: number
  m182?: number
  /** The 7-night mean. */
  m7?: number
  /** The 30-night baseline before the newest night. */
  baseline?: { mean: number; sd: number }
  /** The card's rule fired (Depressed, Up, Low, Off). */
  finding?: boolean
  /** The newest reading's date ("5 Oct 2026") when it is older than the
   * staleness limit: the note says it is old, never a current verdict. */
  staleSince?: string
  /** Strain: the newest recovery, and yesterday's strain. */
  recovery?: number
  strainYesterday?: number
}

export function signalSuggestion(i: SignalInput): Suggestion {
  const none = {
    text: "No recent readings to judge.",
    basis: "Nothing in the last 182 days.",
  }
  if (i.latest === undefined && i.id !== "strain") return none
  if (i.staleSince)
    return {
      text: `The newest reading is from ${i.staleSince}; a new night from your source brings it up to date.`,
      basis: `Nothing newer than ${i.staleSince}, so no verdict on today.`,
    }
  const v = i.latest as number
  switch (i.id) {
    case "recovery": {
      const band = RECOVERY_BANDS.find((b) => v <= b.to) ?? RECOVERY_BANDS[2]
      const text =
        band.tone === "danger"
          ? "Red recovery today; keep strain light and prioritise sleep."
          : band.tone === "warning"
            ? "Yellow recovery; a moderate day (strain 10 to 13) fits."
            : "Green recovery; room for a high-strain day if you want it."
      const falling =
        i.m30 !== undefined && i.m182 !== undefined && i.m30 < i.m182 * 0.98
      return {
        text: falling ? withTail(text, " Trending down this month.") : text,
        basis: `Latest ${fmt(v)}%; 30-day ${fmt(i.m30 ?? v)}%, 6-month ${fmt(i.m182 ?? v)}%.`,
      }
    }
    // WHOOP's score combines consistency, sufficiency (hours against need),
    // stress and efficiency, so the advice names the habits, not the hours.
    case "sleepPerformance":
      return {
        text:
          v < 70
            ? "Poor sleep performance; a regular, earlier bedtime helps most."
            : v < 85
              ? "Sufficient sleep performance; regular times and more time in bed lift it."
              : "Optimal sleep performance; keep the routine.",
        basis: `Last night ${fmt(v)}%; 30-day ${fmt(i.m30 ?? v)}%. WHOOP: under 70 poor, 70 to 84 sufficient, 85 and above optimal.`,
      }
    case "strain": {
      const rec = i.recovery
      let text =
        rec === undefined
          ? "Match strain to recovery: Light on red days, High on green ones."
          : rec < 34
            ? "Recovery is red; a Light day (strain under 10) is the match."
            : rec < 67
              ? "Recovery is yellow; aim for Moderate (10 to 13)."
              : "Recovery is green; High (14 to 17) is available."
      if (
        i.strainYesterday !== undefined &&
        i.strainYesterday >= 18 &&
        rec !== undefined
      )
        text = `Yesterday was All Out; ${text.charAt(0).toLowerCase()}${text.slice(1)}`
      return {
        text,
        basis: `Latest strain ${i.latest === undefined ? "none" : fmt(i.latest, 1)} (${i.latest === undefined ? "no band" : strainBand(i.latest).word.split(":")[0]}); recovery ${rec === undefined ? "unknown" : `${fmt(rec)}%`}.`,
      }
    }
    case "hrv":
    case "hrvSdnn": {
      const b = i.baseline
      if (!b || !(b.sd > 0))
        return {
          text: "HRV needs 14 nights for a baseline.",
          basis: `Latest ${fmt(v)} ms.`,
        }
      const zLast = (v - b.mean) / b.sd
      const z7 = i.m7 === undefined ? 0 : (i.m7 - b.mean) / b.sd
      const text =
        i.finding || (zLast < -1 && z7 < -1)
          ? "HRV is below your baseline this week; ease training and protect sleep."
          : zLast < -1
            ? "Last night's HRV was below your usual; nothing to act on from one night."
            : zLast > 1
              ? "HRV is above your baseline; a good week to train."
              : "HRV is within your usual range."
      return {
        text,
        basis: `Last night ${fmt(v)} ms, 7-night ${fmt(i.m7 ?? v)} ms, baseline ${fmt(b.mean)} ms (SD ${fmt(b.sd)}).`,
      }
    }
    case "respiratoryRate": {
      const base = i.baseline?.mean
      // A GP only when the app's rule has fired.
      return {
        text: i.finding
          ? "Breathing rate is up on your baseline; if it stays up for nights, mention it to a GP."
          : v > 20
            ? "Breathing rate was over 20 last night; one night is not a pattern."
            : v < 12
              ? "Breathing rate was under 12 last night; one night is not a pattern."
              : "Breathing rate is in your usual range.",
        basis: `Last night ${fmt(v, 1)}/min${base === undefined ? "" : `, baseline ${fmt(base, 1)}/min`}; adult resting range 12 to 20.`,
      }
    }
    case "spo2":
      return {
        text: i.finding
          ? "Blood oxygen dipped under 95%; if it repeats, mention it to a GP."
          : v < 95
            ? "Blood oxygen was under 95% last night; one night is not a pattern."
            : "Blood oxygen is in the normal range.",
        basis: `Last night ${fmt(v, 1)}%; the normal range is 95 to 100%.`,
      }
    case "skinTemperature": {
      const d = i.baseline ? v - i.baseline.mean : 0
      return {
        text:
          i.finding || Math.abs(d) > 0.5
            ? "Skin temperature was off your baseline last night; rest and watch how you feel."
            : "Skin temperature is at your baseline.",
        basis: i.baseline
          ? `Last night ${signed(d, 2, " °C")} from the 30-night baseline of ${fmt(i.baseline.mean, 2)} °C.`
          : `Last night ${fmt(v, 2)} °C; no baseline yet.`,
      }
    }
  }
}

// ---- body ------------------------------------------------------------------------

export interface BodyInput {
  metric:
    | "health/body_mass"
    | "health/body_fat_fraction"
    | "health/visceral_fat_rating"
  latest?: number
  previous?: { value: number; date: string }
  heightM?: number
  sex?: Sex
  age?: number
}

export function bodySuggestion(i: BodyInput): Suggestion {
  if (i.latest === undefined)
    return {
      text: "No readings yet; a scan or clinic visit adds one.",
      basis: "No readings.",
    }
  const v = i.latest
  if (i.metric === "health/body_mass") {
    if (!i.heightM)
      return {
        text: "A height reading from any source puts this on the BMI scale.",
        basis: `Latest ${fmt(v, 1)} kg; no height reading.`,
      }
    const bmi = v / (i.heightM * i.heightM)
    const b = bmiBand(bmi)
    let text =
      b.word === "underweight"
        ? "Weight is under the healthy BMI range."
        : b.word === "healthy weight"
          ? "Weight is in the healthy BMI range."
          : "Weight is above the healthy BMI range; small sustained changes count most."
    if (i.previous && i.previous.value > 0) {
      const pct = ((v - i.previous.value) / i.previous.value) * 100
      if (Math.abs(pct) > 2)
        text = withTail(
          text,
          ` It ${pct > 0 ? "rose" : "fell"} ${fmt(Math.abs(pct), 1)}% since ${i.previous.date}.`
        )
    }
    return {
      text,
      basis: `Latest ${fmt(v, 1)} kg at ${fmt(i.heightM, 2)} m is BMI ${fmt(bmi, 1)} (${b.word}).`,
    }
  }
  if (i.metric === "health/body_fat_fraction") {
    if (!i.sex || i.age === undefined)
      return {
        text: "Add your birth date and sex to see your healthy range.",
        basis: `Latest ${fmt(v, 1)}%.`,
      }
    const b = bodyFatBand(v, i.sex, i.age)
    return {
      text:
        b.word === "below the healthy range"
          ? "Body fat is under the healthy range for your age and sex."
          : b.word === "healthy"
            ? "Body fat is in the healthy range for your age and sex."
            : b.word === "overweight"
              ? "Body fat is above the healthy range for your age and sex."
              : "Body fat is well above the healthy range for your age and sex.",
      basis: `Latest ${fmt(v, 1)}% for a ${i.sex === "male" ? "man" : "woman"} of ${fmt(Math.floor(i.age))} (${b.word}; Gallagher 2000).`,
    }
  }
  return {
    text:
      v <= 12
        ? "Visceral fat rating is in the scale's healthy range."
        : "Visceral fat rating is in the scale's excess range; aerobic activity and diet lower it.",
    basis: `Latest rating ${fmt(v)}; on Tanita's rating 1 to 12 is healthy, 13 and over excess.`,
  }
}
