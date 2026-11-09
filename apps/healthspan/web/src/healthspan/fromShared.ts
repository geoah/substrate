// The engine's input from the shared kinds (docs/plans/healthspan.md, The
// age score from the shared kinds): `health/sleep` and `health/workout`
// records kept by the app's own source orders, resting heart rate
// summaries kept by the repository's ranking, daily steps and VO2 max from
// the aggregate read, lean body mass from body composition lab results.
// Records in, engine input out, with no I/O, so a script can build the
// input from what it read and run the engine on it.

import type { AggregateAnswer } from "@substrate/client"
import type { SubstrateRecord } from "@substrate/react"
import type {
  EngineInput,
  RestingSummary,
  Sample,
  SleepSession,
  WorkoutSession,
} from "../engine/input"
import { localDay } from "../engine/engine"
import { factor } from "../lib/units"
import {
  dailyOf,
  durationMs,
  int,
  metricId,
  num,
  rowDay,
  rowMetric,
  rowValue,
  sourceId,
  str,
  unitOf,
} from "./records"

// ---- the record priority policy ---------------------------------------------

/** What the priority policy weighs of a record: its span and its source. */
export interface Weighed {
  at?: string
  endsAt?: string
  source?: string
}

/** The records the priority policy keeps, as internal/engine's
 * aggregaterecords.go keeps them: by rank (the order's index, a source it
 * does not name last), a record whose [at, endsAt) overlaps a kept record
 * of a higher rank is dropped whole, so a dropped record drops nothing.
 * Records of one rank never drop each other. A record with no `endsAt`
 * after its `at` is an instant: a span holding it, or a kept instant at the
 * same `at`, drops it, and it drops only an instant. One with no `at` is
 * kept. Input order is kept. */
export function keepBySource<T extends Weighed>(
  items: readonly T[],
  order: readonly string[]
): T[] {
  const rank = new Map<string, number>()
  order.forEach((id, i) => {
    if (!rank.has(id)) rank.set(id, i)
  })
  const rankOf = (s?: string) =>
    s !== undefined && rank.has(s) ? (rank.get(s) as number) : order.length
  const recs = items.map((it) => {
    const start = it.at ? Date.parse(it.at) : Number.NaN
    const end = it.endsAt ? Date.parse(it.endsAt) : Number.NaN
    const placed = Number.isFinite(start)
    const instant = !(Number.isFinite(end) && end > start)
    return {
      placed,
      instant,
      start,
      end: instant ? start + 0.5 : end,
      rank: rankOf(it.source),
    }
  })
  const byRank = recs
    .map((_, i) => i)
    .sort((a, b) => recs[a].rank - recs[b].rank)
  const keep = new Array<boolean>(recs.length).fill(false)
  const covers: [number, number][] = []
  const instants = new Set<number>()
  const overlaps = (a: number, b: number) =>
    covers.some(([s, e]) => s < b && a < e)
  for (let lo = 0; lo < byRank.length;) {
    let hi = lo
    while (
      hi < byRank.length &&
      recs[byRank[hi]].rank === recs[byRank[lo]].rank
    )
      hi++
    const group = byRank.slice(lo, hi)
    for (const i of group) {
      const r = recs[i]
      keep[i] =
        !r.placed ||
        (!overlaps(r.start, r.end) && !(r.instant && instants.has(r.start)))
    }
    for (const i of group) {
      const r = recs[i]
      if (!keep[i] || !r.placed) continue
      if (r.instant) instants.add(r.start)
      else covers.push([r.start, r.end])
    }
    lo = hi
  }
  return items.filter((_, i) => keep[i])
}

// ---- records -----------------------------------------------------------------

/** A `health/sleep` record as the engine reads it. Its stage totals are
 * light, deep and REM; a record with none of them is not counted. */
export function parseSleep(r: SubstrateRecord): SleepSession | undefined {
  const at = str(r, "at")
  const endsAt = str(r, "endsAt")
  if (!at || !endsAt) return undefined
  const stages = ["light", "deep", "rem"]
    .map((k) => durationMs(r.properties[k]))
    .filter((v): v is number => v !== undefined)
  const scores = r.properties.scores
  const consistency =
    scores && typeof scores === "object"
      ? num((scores as Record<string, unknown>).sleepConsistency)
      : undefined
  return {
    at,
    endsAt,
    offsetEnd: int(r, "utcOffsetEnd") ?? int(r, "utcOffsetStart"),
    source: sourceId(r.properties.source),
    nap: r.properties.isNap === true,
    asleepMs: stages.length ? stages.reduce((a, b) => a + b, 0) : undefined,
    consistency,
  }
}

/** A `health/workout` record as the engine reads it. */
export function parseWorkout(r: SubstrateRecord): WorkoutSession | undefined {
  const at = str(r, "at")
  if (!at) return undefined
  const raw = r.properties.zoneDurations
  const zones: Record<string, number> = {}
  if (raw && typeof raw === "object")
    for (const [k, v] of Object.entries(raw as Record<string, unknown>)) {
      const ms = durationMs(v)
      if (ms !== undefined) zones[k] = ms
    }
  return {
    at,
    endsAt: str(r, "endsAt"),
    source: sourceId(r.properties.source),
    offsetStart: int(r, "utcOffsetStart") ?? int(r, "utcOffsetEnd"),
    sport: str(r, "sport"),
    sportSource: str(r, "sportSource"),
    zones: Object.keys(zones).length ? zones : undefined,
    strain: num(r.properties.strain),
    averageHeartRate: num(r.properties.averageHeartRate),
  }
}

/** A resting heart rate summary (an `observations/observation`). */
export function parseResting(r: SubstrateRecord): RestingSummary | undefined {
  const at = str(r, "at")
  const value = num(r.properties.value)
  if (!at || value === undefined) return undefined
  return {
    at,
    endsAt: str(r, "endsAt"),
    offset: int(r, "utcOffset"),
    source: sourceId(r.properties.source),
    value,
  }
}

// ---- resting heart rate under the repository's ranking -----------------------

export const RESTING_METRIC = "health/resting_heart_rate"

/** The recorded-local date a summary's last instant lies on, at its own
 * offset: where a day read with `place: "end"` puts it (a span ending at
 * midnight on the day before). */
export function placeEndDay(r: RestingSummary): string {
  const a = Date.parse(r.at)
  const e = r.endsAt ? Date.parse(r.endsAt) : a
  const last = e > a ? e - 1 : a
  return new Date(localDay(last, r.offset ?? 0) * 86_400_000)
    .toISOString()
    .slice(0, 10)
}

/** The summaries the priority policy keeps: on each date, those of the
 * sources a day read of the metric under `priority` (with `place: "end"`)
 * says fed that date's row. The server applies each source's `priorities`
 * and the `sourcelink`s, so this follows the repository's ranking. */
export function restingKept(
  summaries: readonly RestingSummary[],
  days: Pick<AggregateAnswer, "rows">,
  metric = RESTING_METRIC
): RestingSummary[] {
  const winners = new Map<string, Set<string>>()
  for (const row of days.rows) {
    if (rowMetric(row) !== undefined && rowMetric(row) !== metric) continue
    if (rowValue(row) === undefined) continue
    const ids = (row.sources ?? [])
      .map(sourceId)
      .filter((s): s is string => !!s)
    const set = winners.get(rowDay(row)) ?? new Set<string>()
    for (const id of ids) set.add(id)
    winners.set(rowDay(row), set)
  }
  return summaries.filter(
    (s) => !!s.source && !!winners.get(placeEndDay(s))?.has(s.source)
  )
}

// ---- VO2 max -----------------------------------------------------------------

/** One reading a day from a day read of `health/vo2_max` (`last`), at noon
 * UTC of its date so any offset within twelve hours keeps it there, with
 * the source that fed it. */
export function vo2Readings(answer: Pick<AggregateAnswer, "rows">): Sample[] {
  const out: Sample[] = []
  for (const row of answer.rows) {
    const v = rowValue(row)
    if (v === undefined) continue
    out.push({
      at: `${rowDay(row)}T12:00:00Z`,
      value: v,
      source: sourceId(row.sources?.[0]),
    })
  }
  return out.sort((a, b) => a.at.localeCompare(b.at))
}

// ---- body composition ------------------------------------------------------------

/** What a body composition result is, whatever metric it is under. */
export type BodyRole =
  "fatFreeMass" | "bodyMass" | "bodyFat" | "leanMass" | "visceralFat"

/** The shared catalogue's body metrics. */
export const BODY_METRICS: Record<BodyRole, string> = {
  fatFreeMass: "health/fat_free_mass",
  bodyMass: "health/body_mass",
  bodyFat: "health/body_fat_fraction",
  leanMass: "health/lean_mass",
  visceralFat: "health/visceral_fat_rating",
}

const ROLE_BY_NAME = new Map(
  (Object.entries(BODY_METRICS) as [BodyRole, string][]).map(([role, id]) => [
    id.slice(id.indexOf("/") + 1),
    role,
  ])
)

/** A metric's role: the catalogue's own, one `roles` names (an owner's
 * metric of the same name), else by its id's last part (`x/fat_free_mass`). */
export function bodyRoleOf(
  metric: string,
  roles?: ReadonlyMap<string, BodyRole>
): BodyRole | undefined {
  return (
    roles?.get(metric) ??
    ROLE_BY_NAME.get(metric.slice(metric.lastIndexOf("/") + 1))
  )
}

/** A `labs/labresult` as the body readings use it. */
export interface LabValue {
  metric: string
  at: string
  value: number
  unit?: string
  source?: string
  /** A regional result (an arm, a leg); the whole body has none. */
  channel?: string
  /** The report it is part of: one scan. */
  report?: string
}

export function parseLabValue(r: SubstrateRecord): LabValue | undefined {
  const metric = metricId(r.properties.metric)
  const at = str(r, "at")
  const value = num(r.properties.value)
  if (!metric || !at || value === undefined) return undefined
  const report = r.properties.report
  return {
    metric,
    at,
    value,
    unit: unitOf(r.properties.value),
    source: sourceId(r.properties.source),
    channel: str(r, "channel") ?? str(r, "bodySite"),
    report:
      typeof report === "string"
        ? report
        : report && typeof report === "object" && "ref" in report
          ? String(report.ref)
          : undefined,
  }
}

/** Lean body mass as the age score means it: fat-free mass (all but fat,
 * bone included) as a percent of body weight, from one scan. */
export interface LeanReading {
  /** Percent of body weight. */
  value: number
  at: string
  source?: string
  /** Fat-free mass over the same scan's weight, or 100 less its body fat. */
  basis: "fatFreeMass" | "bodyFat"
}

/** A body fat result in percent, whether written as percent or fraction. */
export function fatPercent(r: Pick<LabValue, "value" | "unit">): number {
  const f = r.unit ? factor(r.unit, "percent") : undefined
  if (f !== undefined && r.unit) return r.value * f
  return r.value > 1 ? r.value : r.value * 100
}

function kg(r: Pick<LabValue, "value" | "unit">): number | undefined {
  const f = factor(r.unit ?? "kg", "kg")
  return f === undefined ? undefined : r.value * f
}

/** One scan's lean body mass: its fat-free mass over the weight it took,
 * else 100 less its body fat. Undefined without either. */
export function scanLean(
  scan: ReadonlyMap<BodyRole, LabValue>
): LeanReading | undefined {
  const ffm = scan.get("fatFreeMass")
  const mass = scan.get("bodyMass")
  const fat = scan.get("bodyFat")
  if (ffm && mass) {
    const f = kg(ffm)
    const m = kg(mass)
    if (f !== undefined && m !== undefined && m > 0 && f > 0 && f <= m)
      return {
        value: (100 * f) / m,
        at: ffm.at,
        source: ffm.source,
        basis: "fatFreeMass",
      }
  }
  if (fat) {
    const pct = fatPercent(fat)
    if (pct > 0 && pct < 100)
      return {
        value: 100 - pct,
        at: fat.at,
        source: fat.source,
        basis: "bodyFat",
      }
  }
  return undefined
}

/** Lean body mass per scan day. A scan is one report, or one source's
 * results on one date where they name no report. Lean mass (soft tissue
 * without bone) is not the model's and is not read; regional results are
 * left out. On a day with two scans the source `order` ranks first wins.
 * Oldest first. */
export function leanReadings(
  results: readonly LabValue[],
  roles?: ReadonlyMap<string, BodyRole>,
  order: readonly string[] = []
): LeanReading[] {
  const scans = new Map<string, Map<BodyRole, LabValue>>()
  for (const r of results) {
    const role = bodyRoleOf(r.metric, roles)
    if (!role || role === "leanMass" || role === "visceralFat" || r.channel)
      continue
    const key = r.report ?? `${r.source ?? ""}|${r.at.slice(0, 10)}`
    const scan = scans.get(key) ?? new Map<BodyRole, LabValue>()
    scan.set(role, r)
    scans.set(key, scan)
  }
  const rank = (s?: string) => {
    const i = s ? order.indexOf(s) : -1
    return i < 0 ? order.length : i
  }
  const byDay = new Map<string, LeanReading>()
  for (const scan of scans.values()) {
    const l = scanLean(scan)
    if (!l) continue
    const day = l.at.slice(0, 10)
    const cur = byDay.get(day)
    if (
      !cur ||
      rank(l.source) < rank(cur.source) ||
      (rank(l.source) === rank(cur.source) &&
        (l.source ?? "") < (cur.source ?? ""))
    )
      byDay.set(day, l)
  }
  return [...byDay.values()].sort((a, b) => a.at.localeCompare(b.at))
}

// ---- the engine's input ------------------------------------------------------------

/** What the engine reads, as the server answers it. */
export interface SharedRecords {
  /** `health/sleep` records over the window, every source's. */
  sleeps: readonly SubstrateRecord[]
  /** `health/workout` records over the window, every source's. */
  workouts: readonly SubstrateRecord[]
  /** `health/resting_heart_rate` summaries over the window. */
  rhr: readonly SubstrateRecord[]
  /** A day read of the summaries under `priority` (`place: "end"`): which
   * source the repository ranks first on each date. Without it every
   * summary is kept. */
  rhrDays?: Pick<AggregateAnswer, "rows">
  /** A day read of `health/steps` (`sum`, priority), or the daily totals. */
  steps: Pick<AggregateAnswer, "rows"> | ReadonlyMap<string, number>
  /** A day read of `health/vo2_max` (`last`, priority), or the readings. */
  vo2: Pick<AggregateAnswer, "rows"> | readonly Sample[]
  /** `labs/labresult` records of the body metrics. */
  lean: readonly SubstrateRecord[]
  /** Source ids, first highest: the order sleeps are kept in. */
  nightOrder: readonly string[]
  /** The same for workouts. */
  workoutOrder: readonly string[]
  /** Owner metrics of a body metric's name, by id. */
  bodyRoles?: ReadonlyMap<string, BodyRole>
  /** Source ids, first highest: which scan a day keeps. */
  bodyOrder?: readonly string[]
}

const defined = <T>(x: T | undefined): x is T => x !== undefined

/** The engine's input from the shared kinds. */
export function inputFromShared(r: SharedRecords): EngineInput {
  const rhr = r.rhr.map(parseResting).filter(defined)
  return {
    sleeps: keepBySource(
      r.sleeps.map(parseSleep).filter(defined),
      r.nightOrder
    ),
    workouts: keepBySource(
      r.workouts.map(parseWorkout).filter(defined),
      r.workoutOrder
    ),
    rhr: r.rhrDays ? restingKept(rhr, r.rhrDays) : rhr,
    steps:
      r.steps instanceof Map ? r.steps : dailyOf(r.steps as AggregateAnswer),
    vo2: Array.isArray(r.vo2)
      ? [...(r.vo2 as Sample[])]
      : vo2Readings(r.vo2 as AggregateAnswer),
    lean: leanReadings(
      r.lean.map(parseLabValue).filter(defined),
      r.bodyRoles,
      r.bodyOrder
    ).map(({ at, value, source }) => ({ at, value, source })),
  }
}
