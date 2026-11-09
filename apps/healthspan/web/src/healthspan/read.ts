// What the Healthspan screens read, as the server answers it, and the one
// transform the live read and the demo both run on it (docs/plans/
// healthspan.md, The age score from the shared kinds). Sessions are listed
// and kept by the app's own source orders; resting heart rate, steps, VO2
// max and the vendor signals are day reads under the priority policy with
// no order, in the recorded-local calendar, so the repository's ranking
// decides; body composition comes from lab results.

import type { AggregateAnswer } from "@substrate/client"
import type { SubstrateRecord } from "@substrate/react"
import { recordOrder, type OrderSource } from "../data/sourceOrder"
import { LOCK_WINDOW_NIGHTS, SIX_MONTHS } from "../engine/engine"
import type { EngineInput, Sample } from "../engine/input"
import type { MetricId } from "../engine/report"
import { KIND } from "../kinds"
import { factor } from "../lib/units"
import {
  BODY_METRICS,
  bodyRoleOf,
  inputFromShared,
  leanReadings,
  parseLabValue,
  vo2Readings,
  type BodyRole,
  type LabValue,
  type LeanReading,
} from "./fromShared"
import { dailyOf, METRIC_PREFIX, num, rowValue, sourceId, str } from "./records"

/** One value per local date, `YYYY-MM-DD`. */
export type Daily = Map<string, number>

const DAY = 86_400_000

/** How far back the sessions and the day reads reach: the 182 days of the
 * oldest snapshot the page shows, the 182 before it, and its gate. */
export const ENGINE_WINDOW_DAYS = 2 * SIX_MONTHS + LOCK_WINDOW_NIGHTS + 7

/** How far back the VO2 max read reaches: two years, so a reading can
 * carry forward into the oldest snapshot. */
export const VO2_LOOKBACK_DAYS = 730

/** The signals' day read: each vendor summary on the date its night or
 * day ended, the source the repository ranks first. */
export const SIGNAL_METRICS = [
  "whoop/recovery",
  "whoop/strain",
  "whoop/sleep_performance",
  "health/resting_heart_rate",
  "health/hrv_rmssd",
  "health/respiratory_rate",
  "health/spo2",
  "health/skin_temperature",
] as const

/** A raw gauge, averaged per date (a gauge's instants refuse `place`). */
export const HRV_SDNN_METRIC = "health/hrv_sdnn"

/** Everything the screens read, as answered. */
export interface HealthspanRaw {
  /** `observations/source` records. */
  sources: readonly SubstrateRecord[]
  /** `health/sleep` and `health/workout` records over the window. */
  sessions: readonly SubstrateRecord[]
  /** Resting heart rate summaries over the window. */
  rhr: readonly SubstrateRecord[]
  /** The signals' day read (`SIGNAL_METRICS`, `last`, `place: "end"`). */
  signals: AggregateAnswer
  /** `health/hrv_sdnn`, `avg` per date. */
  hrvSdnn: AggregateAnswer
  /** `health/steps`, `sum` per date. */
  steps: AggregateAnswer
  /** `health/vo2_max`, `last` per date, two years. */
  vo2: AggregateAnswer
  /** `observations/metric` records of category `body`. */
  metrics: readonly SubstrateRecord[]
  /** `labs/labresult` records of the body metrics. */
  body: readonly SubstrateRecord[]
  /** The latest `health/height` of each source. */
  height?: AggregateAnswer
}

/** A source as the screens name it. */
export interface SourceInfo extends OrderSource {
  /** Its rank per metric id (`health/steps`), 1 first. */
  priorities: Record<string, number>
}

/** A body card's reading in its display unit. */
export interface BodyReading {
  at: string
  value: number
  source?: string
}

export type BodyCardRole = "bodyMass" | "bodyFat" | "visceralFat"

/** Everything the cards, the rules and the engine read, by date or by
 * reading. */
export interface HealthspanSeries {
  input: EngineInput
  steps: Daily
  recovery: Daily
  strain: Daily
  sleepPerformance: Daily
  /** The ranked source's resting heart rate on the date its night or day
   * ended. */
  rhr: Daily
  hrv: Daily
  hrvSdnn: Daily
  respiratoryRate: Daily
  spo2: Daily
  skinTemperature: Daily
  /** One reading a date, oldest first. */
  vo2: Sample[]
  /** One per scan day, oldest first. */
  lean: LeanReading[]
  /** Weight (kg), body fat (%) and visceral fat rating, every source's,
   * oldest first. */
  body: Record<BodyCardRole, BodyReading[]>
  /** Metres, the newest any source reports. */
  heightM?: number
  /** Every source record by id. */
  sources: Map<string, SourceInfo>
  /** The sources the steps read says fed a day, most days first. */
  stepSources: string[]
  /** The orders the sessions were kept in, names first highest. */
  nightOrder: string[]
  workoutOrder: string[]
}

/** The first date of the engine's window and the day after `today`, as
 * dates and as instants. */
export function windowOf(
  today: Date,
  days = ENGINE_WINDOW_DAYS
): { gte: string; lt: string; from: string } {
  const end = new Date(today.getTime() + DAY)
  const start = new Date(today.getTime() - (days - 1) * DAY)
  const date = (d: Date) =>
    `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`
  return {
    gte: date(start),
    lt: date(end),
    // A day early, so every offset's first night is in.
    from: new Date(start.getTime() - DAY).toISOString(),
  }
}

/** A source record as the screens use it. */
export function sourceInfo(r: SubstrateRecord): SourceInfo {
  const raw = r.properties.priorities
  const priorities: Record<string, number> = {}
  if (raw && typeof raw === "object")
    for (const [k, v] of Object.entries(raw as Record<string, unknown>)) {
      const n = num(v)
      if (n === undefined) continue
      priorities[
        k.startsWith(METRIC_PREFIX) ? k.slice(METRIC_PREFIX.length) : k
      ] = n
    }
  return {
    id: r.id,
    name: str(r, "name") ?? r.id,
    class: str(r, "class"),
    priorities,
  }
}

/** The sources ranked for some metrics, by their best rank among them
 * (1 first), unranked after, then by id. */
export function rankedFor(
  sources: Iterable<SourceInfo>,
  metrics: readonly string[]
): string[] {
  const best = (s: SourceInfo) =>
    Math.min(...metrics.map((m) => s.priorities[m] ?? Number.POSITIVE_INFINITY))
  return [...sources]
    .filter((s) => Number.isFinite(best(s)))
    .sort((a, b) => best(a) - best(b) || a.id.localeCompare(b.id))
    .map((s) => s.id)
}

/** Owner metrics of category `body` named as a catalogue body metric. */
export function bodyRoles(
  metrics: readonly SubstrateRecord[]
): Map<string, BodyRole> {
  const byName = new Map<string, BodyRole>()
  for (const r of metrics) {
    const role = (Object.entries(BODY_METRICS) as [BodyRole, string][]).find(
      ([, id]) => id === r.id
    )?.[0]
    const name = str(r, "name")?.toLowerCase()
    if (role && name) byName.set(name, role)
  }
  const out = new Map<string, BodyRole>()
  for (const r of metrics) {
    const name = str(r, "name")?.toLowerCase()
    const role = name ? byName.get(name) : undefined
    if (role && !Object.values(BODY_METRICS).includes(r.id)) out.set(r.id, role)
  }
  return out
}

/** The catalogue's body metrics and the owner's own of the same name, by
 * id, for the lab result read. */
export function bodyMetricIds(metrics: readonly SubstrateRecord[]): string[] {
  return [
    ...new Set([...Object.values(BODY_METRICS), ...bodyRoles(metrics).keys()]),
  ]
}

/** A body result in its card's unit: kg, percent, the rating as written. */
function display(r: LabValue, role: BodyCardRole): number | undefined {
  if (role === "bodyMass") {
    const f = factor(r.unit ?? "kg", "kg")
    return f === undefined ? undefined : r.value * f
  }
  if (role === "bodyFat") {
    const f = r.unit ? factor(r.unit, "percent") : undefined
    return f !== undefined ? r.value * f : r.value > 1 ? r.value : r.value * 100
  }
  return r.value
}

function bodyReadings(
  results: readonly LabValue[],
  roles: ReadonlyMap<string, BodyRole>
): Record<BodyCardRole, BodyReading[]> {
  const out: Record<BodyCardRole, BodyReading[]> = {
    bodyMass: [],
    bodyFat: [],
    visceralFat: [],
  }
  for (const r of results) {
    if (r.channel) continue
    const role = bodyRoleOf(r.metric, roles)
    if (role !== "bodyMass" && role !== "bodyFat" && role !== "visceralFat")
      continue
    const value = display(r, role)
    if (value !== undefined)
      out[role].push({ at: r.at, value, source: r.source })
  }
  for (const list of Object.values(out))
    list.sort((a, b) => a.at.localeCompare(b.at))
  return out
}

/** The newest height any source reports, in metres. */
function heightOf(answer: AggregateAnswer | undefined): number | undefined {
  let best: { at: string; value: number } | undefined
  for (const row of answer?.rows ?? []) {
    const v = rowValue(row)
    if (v === undefined || !(v > 0.5 && v < 2.6)) continue
    if (!best || row.start > best.at) best = { at: row.start, value: v }
  }
  return best?.value
}

/** The source orders of the sessions: the owner's, then every other
 * holder in the default order (data/sourceOrder.ts). */
export function sessionOrders(
  sessions: readonly SubstrateRecord[],
  sources: readonly OrderSource[],
  owner: {
    nightSources?: readonly string[]
    workoutSources?: readonly string[]
  }
): { night: string[]; workout: string[] } {
  const holding = (kind: string) =>
    new Set(
      sessions
        .filter((r) => r.kind === kind)
        .map((r) => sourceId(r.properties.source))
        .filter((s): s is string => !!s)
    )
  return {
    night: recordOrder(owner.nightSources ?? [], sources, holding(KIND.sleep)),
    workout: recordOrder(
      owner.workoutSources ?? [],
      sources,
      holding(KIND.workout)
    ),
  }
}

/** The screens' series from what was read. `owner` holds the profile's
 * night and workout orders. */
export function toHealthspan(
  raw: HealthspanRaw,
  owner: {
    nightSources?: readonly string[]
    workoutSources?: readonly string[]
  }
): HealthspanSeries {
  const sources = new Map(raw.sources.map((r) => [r.id, sourceInfo(r)]))
  const orders = sessionOrders(raw.sessions, [...sources.values()], owner)
  const roles = bodyRoles(raw.metrics)
  const labs = raw.body
    .map(parseLabValue)
    .filter((x): x is LabValue => x !== undefined)
  const bodyOrder = rankedFor(sources.values(), [
    BODY_METRICS.fatFreeMass,
    BODY_METRICS.bodyMass,
    BODY_METRICS.bodyFat,
  ])
  const input = inputFromShared({
    sleeps: raw.sessions.filter((r) => r.kind === KIND.sleep),
    workouts: raw.sessions.filter((r) => r.kind === KIND.workout),
    rhr: raw.rhr,
    rhrDays: raw.signals,
    steps: raw.steps,
    vo2: raw.vo2,
    lean: raw.body,
    nightOrder: orders.night,
    workoutOrder: orders.workout,
    bodyRoles: roles,
    bodyOrder,
  })
  const of = (m: string) => dailyOf(raw.signals, m)
  return {
    input,
    steps: new Map(input.steps),
    recovery: of("whoop/recovery"),
    strain: of("whoop/strain"),
    sleepPerformance: of("whoop/sleep_performance"),
    rhr: of("health/resting_heart_rate"),
    hrv: of("health/hrv_rmssd"),
    hrvSdnn: dailyOf(raw.hrvSdnn),
    respiratoryRate: of("health/respiratory_rate"),
    spo2: of("health/spo2"),
    skinTemperature: of("health/skin_temperature"),
    vo2: vo2Readings(raw.vo2),
    lean: leanReadings(labs, roles, bodyOrder),
    body: bodyReadings(labs, roles),
    heightM: heightOf(raw.height),
    sources,
    stepSources: fedBy(raw.steps),
    nightOrder: orders.night,
    workoutOrder: orders.workout,
  }
}

/** The sources a day read's rows name, most rows first. */
function fedBy(answer: AggregateAnswer): string[] {
  const n = new Map<string, number>()
  for (const row of answer.rows)
    for (const s of row.sources ?? []) {
      const id = sourceId(s)
      if (id) n.set(id, (n.get(id) ?? 0) + 1)
    }
  return [...n]
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .map(([id]) => id)
}

/** A source's name, or its id where no record names it. */
export function sourceName(
  sources: ReadonlyMap<string, { name: string }>,
  id: string | undefined
): string {
  if (!id) return "an unnamed source"
  return sources.get(id)?.name ?? id
}

/** What feeds one contributor: the kinds it reads and a sentence for the
 * page. */
export interface ContributorInput {
  metrics: string[]
  kinds: string[]
  summary: string
}

export const CONTRIBUTOR_INPUTS: Record<MetricId, ContributorInput> = {
  sleepConsistency: {
    metrics: [],
    kinds: [KIND.sleep],
    summary:
      "Sleeps with stage totals: the source's consistency score of each main sleep",
  },
  sleepHours: {
    metrics: [],
    kinds: [KIND.sleep],
    summary:
      "Sleeps with stage totals: light, deep and REM time each night, naps added",
  },
  zones13: {
    metrics: [],
    kinds: [KIND.workout],
    summary:
      "Workouts with a measure of effort: zone 1 to 3 minutes over the days worn, times 7",
  },
  zones45: {
    metrics: [],
    kinds: [KIND.workout],
    summary:
      "Workouts with a measure of effort: zone 4 and 5 minutes over the days worn, times 7",
  },
  strength: {
    metrics: [],
    kinds: [KIND.workout],
    summary:
      "Strength workouts with a measure of effort: minutes over the days worn, times 7",
  },
  steps: {
    metrics: ["health/steps"],
    kinds: [KIND.observation, KIND.trace],
    summary:
      "Daily step totals, from the source your repository ranks first each day",
  },
  vo2max: {
    metrics: ["health/vo2_max"],
    kinds: [KIND.observation],
    summary:
      "VO2 max readings of the last two years, from the source your repository ranks first each day",
  },
  rhr: {
    metrics: ["health/resting_heart_rate"],
    kinds: [KIND.observation],
    summary:
      "Resting heart rate summaries, each on the night of the sleep it measures",
  },
  leanMass: {
    metrics: [
      BODY_METRICS.fatFreeMass,
      BODY_METRICS.bodyMass,
      BODY_METRICS.bodyFat,
    ],
    kinds: [KIND.labresult],
    summary:
      "Body composition results: fat-free mass over the same scan's weight, or 100 less its body fat",
  },
}

/** The newest reading of a list kept oldest first. */
export function newest<T>(list: readonly T[]): T | undefined {
  return list[list.length - 1]
}
