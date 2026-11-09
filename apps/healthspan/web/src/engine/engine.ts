// The shared kinds in, weekly age score snapshots out
// (docs/plans/healthspan.md, The age score from the shared kinds).
//
// A snapshot is computed for each Sunday from the data before it: the
// 6-month (182-day) averages give the age score, the 30-day averages give
// the projection the pace of aging compares against.
//
// Days. A sleep belongs to a NIGHT, the local day before the day it ended
// on, so Saturday night's sleep, which ends on Sunday morning, falls in the
// week that ends on Saturday. A resting heart rate summary belongs to the
// night of the same source's sleep its span overlaps most, else to the day
// its span starts on when it starts at noon or later, else the day before.
// A workout belongs to its own local day. Every row is placed with its own
// UTC offset.

import * as C from "./curves"
import { tenths } from "./format"
import type {
  EngineInput,
  RestingSummary,
  Sample,
  SleepSession,
  WorkoutSession,
} from "./input"
import type {
  Metric,
  MetricId,
  MetricPoint,
  Report,
  Scale,
  SectionId,
  Snapshot,
  ValueFormat,
  Verdict,
} from "./report"

/** A value entered by hand, used from `asOf` onward in place of any
 * reading. */
export interface Manual {
  value: number
  /** `YYYY-MM-DD`. */
  asOf: string
}

export interface Settings {
  /** `YYYY-MM-DD`. */
  birthDate: string
  sex: C.Sex
  vo2Max?: Manual
  /** Lean body mass, percent of body weight. */
  leanMassPct?: Manual
}

const DAY = 86_400_000
/** Days in the window whose averages give the age score. */
export const SIX_MONTHS = 182
/** Days in the window whose averages give the pace of aging's projection. */
export const THIRTY = 30
/** A weekly figure needs at least a week of wear in its window. */
export const MIN_COVERED_DAYS = 7
/** A snapshot with fewer days of data than this before it is calibrating. */
export const CALIBRATION_DAYS = 90
/** Nights with a resting heart rate a snapshot needs in the nights before
 * it, or it does not update (21 in 31). */
export const LOCK_RECOVERIES = 21
export const LOCK_WINDOW_NIGHTS = 31
/** The projection the pace of aging compares against, in years: the
 * 30-day averages held this long. */
export const PROJECTION_YEARS = 0.5
/** The pace of aging is clamped to this range. */
export const PACE_MIN = -1
export const PACE_MAX = 3
/** A VO2 max or body composition reading stands for up to this many days
 * when a window holds none: a DXA scan is yearly, not daily. */
export const BODY_CARRY_FORWARD_DAYS = 365

/** The white paper's strength activities plus the support article's later
 * additions, as the band's API spells a sport (lower case, spaces and
 * dashes folded to underscores). A workout's `sportSource` is matched
 * against it. */
export const STRENGTH_SPORTS = new Set([
  "strength_trainer",
  "weightlifting",
  "powerlifting",
  "bodybuilding",
  "barre",
  "barre3",
  "pilates",
  "yoga",
  "hot_yoga",
  "sculpt_yoga",
  "functional_fitness",
  "barrys",
  "f45",
  "f45_training",
  "box_fitness",
  "hiit",
  "rucking",
  "solidcore",
  "babywearing",
  "baby_wearing",
  "toddlerwearing",
  "toddler_wearing",
])

/** The workout kind's shared codes that are strength work, whatever the
 * source named them. */
export const STRENGTH_CODES = new Set(["strength", "yoga", "pilates"])

/** Whether a source's own sport name is on the strength list, in any
 * spelling. */
export function isStrength(sport: string | undefined): boolean {
  if (!sport) return false
  return STRENGTH_SPORTS.has(
    sport
      .toLowerCase()
      .replace(/['’]/g, "")
      .replace(/[\s-]+/g, "_")
  )
}

/** A workout counts toward strength time when its shared code is a
 * strength one or its source's own name is on the strength list. */
export function isStrengthWorkout(
  w: Pick<WorkoutSession, "sport" | "sportSource">
): boolean {
  return (!!w.sport && STRENGTH_CODES.has(w.sport)) || isStrength(w.sportSource)
}

/** A sleep counts when it carries a stage total (light, deep or REM). */
export function countsSleep(s: Pick<SleepSession, "asleepMs">): boolean {
  return s.asleepMs !== undefined
}

/** A workout counts when it carries a measure of effort: its zone times,
 * a strain or an average heart rate. */
export function countsWorkout(
  w: Pick<WorkoutSession, "zones" | "strain" | "averageHeartRate">
): boolean {
  return (
    (!!w.zones && Object.keys(w.zones).length > 0) ||
    w.strain !== undefined ||
    w.averageHeartRate !== undefined
  )
}

// ---- local days -----------------------------------------------------------

/** `+02:00`, `-0700` or `Z` as minutes east of UTC. */
export function offsetMinutes(tz: string | undefined): number | undefined {
  if (tz === "Z") return 0
  const m = tz?.match(/^([+-])(\d{2}):?(\d{2})$/)
  if (!m) return undefined
  return (m[1] === "-" ? -1 : 1) * (Number(m[2]) * 60 + Number(m[3]))
}

/** Days since the epoch in local time at `offset` minutes from UTC. */
export function localDay(iso: string | number, offset: number): number {
  const t = typeof iso === "number" ? iso : Date.parse(iso)
  return Math.floor((t + offset * 60_000) / DAY)
}

export function isoDay(iso: string): number {
  return Math.floor(Date.parse(`${iso}T00:00:00Z`) / DAY)
}

export function dayIso(day: number): string {
  return new Date(day * DAY).toISOString().slice(0, 10)
}

/** A sleep's night: the local day before the day it ended on. */
export function sleepNight(s: SleepSession, fallbackOffset: number): number {
  return localDay(s.endsAt, s.offsetEnd ?? fallbackOffset) - 1
}

/** A resting heart rate summary's night: the night of the sleep of its
 * source (counted or not) its span overlaps most; with none, the local
 * day its span starts on when it starts at noon or later, else the day
 * before. An instant overlaps a sleep that holds it. */
export function restingNight(
  r: RestingSummary,
  sleeps: readonly SleepSession[],
  fallbackOffset: number
): number {
  const a = Date.parse(r.at)
  const end = r.endsAt ? Date.parse(r.endsAt) : a
  const b = end > a ? end : a + 1
  let best: SleepSession | undefined
  let most = 0
  for (const s of sleeps) {
    if ((s.source ?? "") !== (r.source ?? "")) continue
    const overlap =
      Math.min(b, Date.parse(s.endsAt)) - Math.max(a, Date.parse(s.at))
    if (overlap > most) {
      most = overlap
      best = s
    }
  }
  if (best) return sleepNight(best, fallbackOffset)
  const local = a + (r.offset ?? fallbackOffset) * 60_000
  const day = Math.floor(local / DAY)
  return local - day * DAY >= DAY / 2 ? day : day - 1
}

// ---- daily series -----------------------------------------------------------

/** One value per day; a window's average is the mean of its days. */
type Daily = Map<number, number>

export interface Series {
  sleepHours: Daily
  sleepConsistency: Daily
  rhr: Daily
  /** Minutes per day, summed over the day's workouts. */
  zones13: Daily
  zones45: Daily
  strength: Daily
  steps: Daily
  vo2: Daily
  lean: Daily
  /** Nights with a resting heart rate summary: the 21-in-31 gate. */
  recoveries: Set<number>
  /** Days worn (a counted sleep or a resting heart rate summary): weekly
   * figures are divided by these, not by the calendar. */
  worn: Set<number>
  /** Whether a counted sleep that is not a nap carries a consistency
   * score: without one there is no consistency contributor. */
  consistencyScored: boolean
  firstDay: number
  /** Offset of the newest row, for "today" and the Sunday boundary. */
  offset: number
}

function add(map: Daily, day: number, v: number) {
  map.set(day, (map.get(day) ?? 0) + v)
}

/** Daily values from samples: per source, averaged, then the largest
 * source wins a day. */
function daily(samples: readonly Sample[], offset: number): Daily {
  const bySource = new Map<string, Map<number, number[]>>()
  for (const s of samples) {
    if (!Number.isFinite(s.value)) continue
    const src = s.source ?? ""
    const days = bySource.get(src) ?? new Map<number, number[]>()
    const d = localDay(s.at, offset)
    days.set(d, [...(days.get(d) ?? []), s.value])
    bySource.set(src, days)
  }
  const out: Daily = new Map()
  for (const days of bySource.values()) {
    for (const [d, vs] of days) {
      const v = vs.reduce((a, b) => a + b, 0) / vs.length
      out.set(d, Math.max(out.get(d) ?? Number.NEGATIVE_INFINITY, v))
    }
  }
  return out
}

/** The offset of the newest sleep or resting heart rate summary. */
function newestOffset(data: EngineInput): number {
  let newest = ""
  let offset = 0
  for (const s of data.sleeps)
    if (s.offsetEnd !== undefined && s.endsAt > newest) {
      newest = s.endsAt
      offset = s.offsetEnd
    }
  for (const r of data.rhr) {
    const at = r.endsAt ?? r.at
    if (r.offset !== undefined && at > newest) {
      newest = at
      offset = r.offset
    }
  }
  return offset
}

export function buildSeries(data: EngineInput): Series {
  const offset = newestOffset(data)
  const s: Series = {
    sleepHours: new Map(),
    sleepConsistency: new Map(),
    rhr: new Map(),
    zones13: new Map(),
    zones45: new Map(),
    strength: new Map(),
    steps: new Map(),
    vo2: new Map(),
    lean: new Map(),
    recoveries: new Set(),
    worn: new Set(),
    consistencyScored: false,
    firstDay: Number.POSITIVE_INFINITY,
    offset,
  }
  const wore = (d: number) => {
    s.worn.add(d)
    s.firstDay = Math.min(s.firstDay, d)
  }

  for (const sl of data.sleeps) {
    if (!countsSleep(sl)) continue
    const night = sleepNight(sl, offset)
    const asleep = sl.asleepMs ?? 0
    // Naps count toward the night's hours but never toward consistency.
    if (asleep > 0) add(s.sleepHours, night, asleep / 3_600_000)
    if (!sl.nap && typeof sl.consistency === "number") {
      s.sleepConsistency.set(night, sl.consistency)
      s.consistencyScored = true
    }
    wore(night)
  }
  for (const r of data.rhr) {
    if (!Number.isFinite(r.value)) continue
    const night = restingNight(r, data.sleeps, offset)
    if (r.value > 0) s.rhr.set(night, r.value)
    s.recoveries.add(night)
    wore(night)
  }
  for (const w of data.workouts) {
    if (!countsWorkout(w)) continue
    const d = localDay(w.at, w.offsetStart ?? offset)
    const z = w.zones
    if (z) {
      add(
        s.zones13,
        d,
        ((z.zone1 ?? 0) + (z.zone2 ?? 0) + (z.zone3 ?? 0)) / 60_000
      )
      add(s.zones45, d, ((z.zone4 ?? 0) + (z.zone5 ?? 0)) / 60_000)
    }
    if (isStrengthWorkout(w) && w.endsAt) {
      const minutes = (Date.parse(w.endsAt) - Date.parse(w.at)) / 60_000
      if (minutes > 0) add(s.strength, d, minutes)
    }
  }
  for (const [day, v] of data.steps)
    if (Number.isFinite(v)) s.steps.set(isoDay(day), v)
  s.vo2 = daily(data.vo2, offset)
  s.lean = daily(data.lean, offset)
  return s
}

/** Mean of the days in [from, to) that have a value. */
function mean(map: Daily, from: number, to: number): number | null {
  let sum = 0
  let n = 0
  for (const [d, v] of map) {
    if (d >= from && d < to) {
      sum += v
      n++
    }
  }
  return n ? sum / n : null
}

/** Minutes per week over [from, to): the total over the days worn, times 7.
 * A window with less than a week of wear has no figure. */
function weekly(
  map: Daily,
  s: Series,
  from: number,
  to: number
): number | null {
  let worn = 0
  for (const d of s.worn) if (d >= from && d < to) worn++
  if (worn < MIN_COVERED_DAYS) return null
  let sum = 0
  for (const [d, v] of map) if (d >= from && d < to) sum += v
  return (sum / worn) * 7
}

/** The newest value in [to - days, to), or null. */
function newestWithin(map: Daily, to: number, days: number): number | null {
  let best: number | undefined
  for (const d of map.keys())
    if (d < to && d >= to - days && (best === undefined || d > best)) best = d
  return best === undefined ? null : (map.get(best) as number)
}

/** The value entered by hand when one is in force by the window's end: an
 * entry is the owner saying which number to trust. Otherwise the mean of
 * the readings in the window; a window without one takes the newest
 * reading up to `carryForwardDays` before its end. */
export function sampledOr(
  map: Daily,
  from: number,
  to: number,
  manual: Manual | undefined,
  carryForwardDays = 0
): number | null {
  if (manual && isoDay(manual.asOf) < to) return manual.value
  const m = mean(map, from, to)
  if (m !== null || carryForwardDays <= 0) return m
  return newestWithin(map, to, carryForwardDays)
}

// ---- metric definitions -----------------------------------------------------

export interface Def {
  id: MetricId
  section: SectionId
  label: string
  /** How the verdict sentence names it. */
  prose: string
  format: ValueFormat
  min: number
  max: number
  minLabel: string
  maxLabel: string
  /** A longer bar for a big average: strength runs to 2h, or to 4h when
   * the 6-month average is past `above`. */
  wide?: { above: number; max: number; maxLabel: string }
  /** Days a reading stands for when a window holds none (body readings). */
  carryForwardDays?: number
  /** Years for a value in display units. */
  years: (v: number, age: number, st: Settings) => number
  /** The average over [from, to), in display units. `carry` is the def's
   * own `carryForwardDays`, which the report passes in. */
  value: (
    s: Series,
    from: number,
    to: number,
    st: Settings,
    carry?: number
  ) => number | null
  source: (s: Series, st: Settings) => string
}

function hours(min: number | null): number | null {
  return min === null ? null : min / 60
}

export const DEFS: Def[] = [
  {
    id: "sleepConsistency",
    section: "sleep",
    label: "Sleep Consistency",
    prose: "daily Sleep Consistency",
    format: "percent",
    min: 40,
    max: 100,
    minLabel: "40%",
    maxLabel: "100%",
    years: (v) => C.sleepConsistencyYears(v),
    value: (s, a, b) => mean(s.sleepConsistency, a, b),
    source: (s) =>
      s.consistencyScored || !s.sleepHours.size
        ? "The sleep consistency score your sleep source gives each main sleep. Naps do not count."
        : "None of your sleep sources scores sleep consistency, so it has no value here; the score is the source's own and is not worked out another way.",
  },
  {
    id: "sleepHours",
    section: "sleep",
    label: "Hours of Sleep",
    prose: "nightly Hours of Sleep",
    format: "hours",
    min: 5,
    max: 8,
    minLabel: "5h",
    maxLabel: "8h",
    years: (v) => C.sleepHoursYears(v),
    value: (s, a, b) => mean(s.sleepHours, a, b),
    source: () =>
      "Light, deep and REM time of each sleep with stage totals, per night, with that night's naps added.",
  },
  {
    id: "zones13",
    section: "strain",
    label: "Time in HR Zones 1-3 (Weekly)",
    prose: "weekly Time in HR Zones 1-3",
    format: "hours",
    min: 0,
    max: 3,
    minLabel: "0h",
    maxLabel: "3h",
    years: (v, age) => C.zones13Years(v * 60, age),
    value: (s, a, b) => hours(weekly(s.zones13, s, a, b)),
    source: () =>
      "Zone 1, 2 and 3 time of workouts with a measure of effort, per week worn. All-day heart rate does not count.",
  },
  {
    id: "zones45",
    section: "strain",
    label: "Time in HR Zones 4-5 (Weekly)",
    prose: "weekly Time in HR Zones 4-5",
    format: "hours",
    min: 0,
    max: 1,
    minLabel: "0h",
    maxLabel: "1h",
    years: (v, age) => C.zones45Years(v * 60, age),
    value: (s, a, b) => hours(weekly(s.zones45, s, a, b)),
    source: () =>
      "Zone 4 and 5 time of workouts with a measure of effort, per week worn.",
  },
  {
    id: "strength",
    section: "strain",
    label: "Strength Activity Time (Weekly)",
    prose: "weekly Strength Activity Time",
    format: "hours",
    min: 0,
    max: 2,
    minLabel: "0h",
    maxLabel: "2h",
    wide: { above: 1.6, max: 4, maxLabel: "4h" },
    years: (v) => C.strengthYears(v * 60),
    value: (s, a, b) => hours(weekly(s.strength, s, a, b)),
    source: () =>
      "Duration of strength workouts with a measure of effort (strength training, pilates, yoga, functional fitness, HIIT and the rest of the white paper's list), per week worn.",
  },
  {
    id: "steps",
    section: "strain",
    label: "Steps",
    prose: "daily Steps",
    format: "steps",
    min: 0,
    max: 16000,
    minLabel: "0K",
    maxLabel: "16K",
    years: (v, age) => C.stepsYears(v, age),
    value: (s, a, b) => mean(s.steps, a, b),
    source: (s) =>
      s.steps.size
        ? "Daily step totals; where two sources count a day, the one your repository ranks first."
        : "No step data.",
  },
  {
    id: "vo2max",
    section: "fitness",
    label: "VO₂ Max",
    prose: "VO₂ Max",
    format: "vo2",
    min: 15,
    max: 70,
    minLabel: "15",
    maxLabel: "70",
    carryForwardDays: BODY_CARRY_FORWARD_DAYS,
    years: (v, age, st) => C.vo2Years(v, age, st.sex),
    value: (s, a, b, st, carry) => sampledOr(s.vo2, a, b, st.vo2Max, carry),
    source: (s, st) =>
      st.vo2Max
        ? `The VO₂ max entered by hand, in force from ${st.vo2Max.asOf}; it takes precedence over readings.`
        : s.vo2.size
          ? "VO₂ max readings; where two sources report a day, the one your repository ranks first."
          : "No VO₂ max reading in the last two years.",
  },
  {
    id: "rhr",
    section: "fitness",
    label: "RHR",
    prose: "Resting Heart Rate",
    format: "bpm",
    min: 40,
    max: 80,
    minLabel: "40bpm",
    maxLabel: "80bpm",
    years: (v, _a, st) => C.rhrYears(v, st.sex),
    value: (s, a, b) => mean(s.rhr, a, b),
    source: () =>
      "Resting heart rate summaries, each on the night of the sleep it measures.",
  },
  {
    id: "leanMass",
    section: "fitness",
    label: "Lean Body Mass",
    prose: "Lean Body Mass",
    format: "percent1",
    min: 60,
    max: 90,
    minLabel: "60%",
    maxLabel: "90%",
    carryForwardDays: BODY_CARRY_FORWARD_DAYS,
    years: (v, age, st) => C.leanYears(v, age, st.sex),
    value: (s, a, b, st, carry) =>
      sampledOr(s.lean, a, b, st.leanMassPct, carry),
    source: (s, st) =>
      st.leanMassPct
        ? `The lean body mass entered by hand, in force from ${st.leanMassPct.asOf}; it takes precedence over scans.`
        : s.lean.size
          ? "Fat-free mass as a percent of body weight, from body composition results."
          : "No body composition result in the last year.",
  },
]

const SEGMENTS = 10

function scaleFor(
  d: Def,
  age: number,
  st: Settings,
  six: number | null
): Scale {
  const wide = d.wide && six !== null && six > d.wide.above ? d.wide : undefined
  const max = wide?.max ?? d.max
  const w = (max - d.min) / SEGMENTS
  return {
    min: d.min,
    max,
    minLabel: d.minLabel,
    maxLabel: wide?.maxLabel ?? d.maxLabel,
    segments: Array.from({ length: SEGMENTS }, (_, i) =>
      d.years(d.min + (i + 0.5) * w, age, st)
    ),
  }
}

const CURVE_POINTS = 121

/** The curve over the bar's range, widened by a quarter of it on each side
 * and further if an average falls outside, never below zero. */
function curveFor(
  d: Def,
  age: number,
  st: Settings,
  values: (number | null)[]
): [number, number][] {
  const pad = (d.max - d.min) / 4
  const known = values.filter((v): v is number => v !== null)
  const lo = Math.max(0, Math.min(d.min - pad, ...known))
  const hi = Math.max(d.max + pad, ...known)
  return Array.from({ length: CURVE_POINTS }, (_, i) => {
    const v = lo + ((hi - lo) * i) / (CURVE_POINTS - 1)
    return [v, d.years(v, age, st)]
  })
}

/** Years at `v` on a sampled curve, linear between samples. */
export function yearsAt(curve: [number, number][], v: number): number {
  if (v <= curve[0][0]) return curve[0][1]
  for (let i = 1; i < curve.length; i++) {
    const [x1, y1] = curve[i]
    if (v <= x1) {
      const [x0, y0] = curve[i - 1]
      return y0 + ((y1 - y0) * (v - x0)) / (x1 - x0)
    }
  }
  return curve[curve.length - 1][1]
}

// ---- verdicts ---------------------------------------------------------------

function percentChange(from: number, to: number): string {
  if (from === 0) return ""
  return `${Math.abs(((to - from) / from) * 100).toFixed(1)}%`
}

export function verdictFor(
  d: Def,
  six: number | null,
  thirty: number | null,
  impact: number | null,
  impact30: number | null,
  source: string
): Verdict {
  if (six === null || impact === null) return { title: "No data", body: source }
  if (thirty === null || impact30 === null)
    return {
      title: "No recent data",
      body: `There is no ${d.prose} in the last 30 days.`,
    }
  const better = impact30 < impact - 0.05
  const worse = impact30 > impact + 0.05
  const change = percentChange(six, thirty)
  const moved =
    change && Math.abs(thirty - six) > 1e-9
      ? `Your ${d.prose} ${thirty > six ? "increased" : "decreased"} by ${change} this month from your 6-month average.`
      : `Your ${d.prose} this month matches your 6-month average.`
  if (better) {
    return {
      title:
        impact - impact30 >= 0.3 ? "Huge Improvement" : "Heading the Right Way",
      body: `${moved} Continue on this track to see your age score lower over time.`,
    }
  }
  if (worse) {
    return {
      title: impact30 - impact >= 0.3 ? "Needs Attention" : "Stay Focused",
      body: `${moved} If it holds for six months, it will add about ${tenths(impact30 - impact).toFixed(1)} years to your age score.`,
    }
  }
  if (impact > 0.05) {
    return {
      title: "Room to Improve",
      body: `${moved} It is still adding ${tenths(impact).toFixed(1)} years to your age score.`,
    }
  }
  return { title: "You're Right On Track", body: moved }
}

function short(label: string): string {
  return label.replace(/ \(Weekly\)$/, "")
}

function coachFor(
  snap: Omit<Snapshot, "coach">,
  prev: Snapshot | undefined
): Verdict {
  const known = snap.metrics.filter((m) => m.impact !== null)
  const younger = snap.delta < 0
  const change = prev ? snap.delta - prev.delta : 0
  const ageLine = !prev
    ? `Your age score is ${tenths(Math.abs(snap.delta)).toFixed(1)} years ${younger ? "younger" : "older"} than your age`
    : tenths(change) === 0
      ? "Your age score held steady this week"
      : `Your age score ${change < 0 ? "decreased" : "increased"} by ${tenths(Math.abs(change)).toFixed(1)} this week`
  const paceWord =
    snap.pace < 0
      ? "reversing"
      : snap.pace < 1
        ? "slow"
        : snap.pace === 1
          ? "steady"
          : "fast"

  // What moves Pace: the contributor whose last 30 days differ most from its
  // six months, in years.
  const shift = [...known]
    .filter((m) => m.impact30 !== null)
    .sort(
      (a, b) =>
        Math.abs((b.impact30 ?? 0) - (b.impact ?? 0)) -
        Math.abs((a.impact30 ?? 0) - (a.impact ?? 0))
    )[0]
  const shiftBy = shift ? (shift.impact30 ?? 0) - (shift.impact ?? 0) : 0
  const worst = [...known].sort((a, b) => (b.impact ?? 0) - (a.impact ?? 0))[0]
  let driver = ""
  if (shift && Math.abs(shiftBy) >= 0.1) {
    driver =
      shiftBy > 0
        ? ` ${short(shift.label)} has slipped this month and is pushing your Pace of Aging up the most.`
        : ` Your recent ${short(shift.label)} is doing the most to slow your Pace of Aging.`
  } else if (worst && (worst.impact ?? 0) > 0.05) {
    driver = ` ${short(worst.label)} is having the biggest impact on increasing your age score, so watch this trend over the coming weeks.`
  }

  if (snap.pace < 0.5 && younger)
    return {
      title: "As Good As It Gets",
      body: `${ageLine} and your Pace of Aging is ${paceWord}.${driver}`,
    }
  if (snap.pace <= 1)
    return {
      title: "Steady and Healthy",
      body: `${ageLine} and your Pace of Aging is ${paceWord}.${driver}`,
    }
  if (younger)
    return {
      title: "Maintain Your Gains",
      body: `${ageLine}, though you're still doing well overall. Your Pace of Aging is ${snap.pace.toFixed(1)}x.${driver}`,
    }
  return {
    title: "Stay Focused",
    body: `${ageLine} and your Pace of Aging is ${snap.pace.toFixed(1)}x, faster than the calendar.${driver}`,
  }
}

// ---- snapshots ----------------------------------------------------------------

export function ageOn(birthDate: string, day: number): number {
  return (day - isoDay(birthDate)) / 365.2425
}

/** How much of the white paper's projection the pace of aging shows. The
 * paper's definition (30-day averages held for six months) gives
 * 1 + 2 × (sum30 − sum6), which runs to 3.0x on real data where the vendor's
 * own app shows 1.8x. Fitted to 17 weekly paces read off that app's trend
 * chart: pace = 1 + 0.6 × (sum30 − sum6), so 0.3 of the paper's slope,
 * typical error 0.4x. */
export const PACE_DAMPING = 0.3

/** Pace of aging from the two sums of impacts (white paper p. 19,
 * damped). */
export function paceOf(sum6: number, sum30: number): number {
  const pace = 1 + (PACE_DAMPING * (sum30 - sum6)) / PROJECTION_YEARS
  return Math.min(PACE_MAX, Math.max(PACE_MIN, pace))
}

/** Every Sunday from the first with 21 resting heart rate nights in the 31
 * before it, up to the latest Sunday not after `today` (the member's local
 * date). */
export function computeReport(
  data: EngineInput,
  st: Settings,
  today = new Date()
): Report {
  const s = buildSeries(data)
  const todayDay = localDay(today.toISOString(), s.offset)
  const dow = new Date(todayDay * DAY).getUTCDay() // 0 = Sunday
  const lastSunday = todayDay - dow
  const nextUpdateDays = 7 - dow
  if (!Number.isFinite(s.firstDay)) return { snapshots: [], nextUpdateDays }

  let first = lastSunday
  while (first - 7 > s.firstDay) first -= 7

  const snapshots: Snapshot[] = []
  let paused: number | undefined
  const histories = new Map<MetricId, MetricPoint[]>(
    DEFS.map((d) => [d.id, []])
  )
  for (let sunday = first; sunday <= lastSunday; sunday += 7) {
    let recent = 0
    for (const d of s.recoveries)
      if (d >= sunday - LOCK_WINDOW_NIGHTS && d < sunday) recent++
    // Without 21 nights in 31 the age score does not update: the last
    // valid snapshot stands until coverage returns.
    const locked = recent < LOCK_RECOVERIES
    if (sunday === lastSunday) paused = locked ? recent : undefined
    if (locked) continue

    const age = ageOn(st.birthDate, sunday)
    const metrics: Metric[] = DEFS.map((d) => {
      const six = d.value(
        s,
        sunday - SIX_MONTHS,
        sunday,
        st,
        d.carryForwardDays
      )
      const thirty = d.value(s, sunday - THIRTY, sunday, st, d.carryForwardDays)
      const impact = six === null ? null : d.years(six, age, st)
      // The projection keeps today's targets: re-targeting at age + 0.5
      // would move Pace on a birthday with nothing else changed.
      const impact30 = thirty === null ? null : d.years(thirty, age, st)
      const point = {
        date: dayIso(sunday),
        sixMonth: six,
        thirtyDay: thirty,
        impact,
      }
      histories.get(d.id)?.push(point)
      const source = d.source(s, st)
      return {
        id: d.id,
        section: d.section,
        label: d.label,
        format: d.format,
        sixMonth: six,
        thirtyDay: thirty,
        impact,
        impact30,
        opportunity: impact === null ? 0 : Math.max(0, impact),
        scale: scaleFor(d, age, st, six),
        curve: curveFor(d, age, st, [six, thirty]),
        verdict: verdictFor(d, six, thirty, impact, impact30, source),
        source,
        history: [...(histories.get(d.id) ?? [])],
      }
    })
    const sum6 = metrics.reduce((a, m) => a + (m.impact ?? 0), 0)
    const sum30 = metrics.reduce((a, m) => a + (m.impact30 ?? m.impact ?? 0), 0)
    const ageScore = age + sum6
    const base = {
      date: dayIso(sunday),
      // A snapshot is labelled with the week that starts on its Sunday.
      weekStart: dayIso(sunday),
      weekEnd: dayIso(sunday + 6),
      age,
      ageScore,
      delta: sum6,
      pace: paceOf(sum6, sum30),
      calibrating: sunday - s.firstDay < CALIBRATION_DAYS,
      locked,
      contributors: metrics.filter((m) => m.impact !== null).length,
      metrics,
    }
    snapshots.push({
      ...base,
      coach: coachFor(base, snapshots[snapshots.length - 1]),
    })
  }
  return {
    snapshots,
    nextUpdateDays,
    paused: paused === undefined ? undefined : { recoveries: paused },
  }
}
