// "How the age score works" as data. Every number here is read from the
// engine's exports or the snapshot, never typed, so the page cannot drift
// from the model it explains: the function, the windows and rules, one row
// per contributor with its target and the most it can add or remove, and
// which sources feed each contributor and in what order.

import { targetFor, type Sex } from "../engine/curves"
import {
  ageOn,
  BODY_CARRY_FORWARD_DAYS,
  buildSeries,
  CALIBRATION_DAYS,
  DEFS,
  LOCK_RECOVERIES,
  LOCK_WINDOW_NIGHTS,
  MIN_COVERED_DAYS,
  PACE_DAMPING,
  PACE_MAX,
  PACE_MIN,
  PROJECTION_YEARS,
  SIX_MONTHS,
  THIRTY,
} from "../engine/engine"
import { tenths } from "../engine/format"
import type { EngineInput } from "../engine/input"
import type { MetricId, Snapshot } from "../engine/report"
import { notInScoreAll } from "./content"
import { BODY_CARDS } from "./healthspanCards"
import {
  CONTRIBUTOR_LABELS,
  HEALTHSPAN_METRICS,
  impactText,
  paceText,
  valueText,
} from "./healthspanMetrics"
import { CONTRIBUTOR_INPUTS, sourceName, type HealthspanSeries } from "./read"

/** The engine's constants the explainer renders, by name. The test checks
 * each is the engine's own export. */
export const EXPLAINER_CONSTANTS = {
  SIX_MONTHS,
  THIRTY,
  MIN_COVERED_DAYS,
  CALIBRATION_DAYS,
  LOCK_RECOVERIES,
  LOCK_WINDOW_NIGHTS,
  PROJECTION_YEARS,
  PACE_DAMPING,
  PACE_MIN,
  PACE_MAX,
  BODY_CARRY_FORWARD_DAYS,
} as const

/** What a contributor's target depends on. */
const DEPENDS: Partial<Record<MetricId, "age" | "sex" | "age and sex">> = {
  zones13: "age",
  zones45: "age",
  steps: "age",
  vo2max: "age and sex",
  rhr: "sex",
  leanMass: "age and sex",
}

const NUMBERS = [
  "zero",
  "one",
  "two",
  "three",
  "four",
  "five",
  "six",
  "seven",
  "eight",
  "nine",
]
const word = (n: number) => NUMBERS[n] ?? String(n)

export interface ExplainerRow {
  id: MetricId
  /** The display label ("Hours of sleep"). */
  label: string
  /** The value that adds no years, in the metric's format ("7:00 h");
   * undefined when it needs an age or sex the profile lacks. */
  target?: string
  /** "for your age and sex" where the target depends on them. */
  targetNote?: string
  /** The most years the bar's range can add ("+3.4 years"), or "nothing". */
  addsAtMost?: string
  /** The most years it can remove ("-2.5 years"), or "nothing". */
  removesAtMost?: string
  /** Where its data comes from (`CONTRIBUTOR_INPUTS[id].summary`). */
  feedsFrom: string
}

export interface ExplainerItem {
  key: string
  value: string
}

export interface ExplainerSource {
  /** What the group covers ("Sleeps"). */
  data: string
  /** The contributors it feeds, by display label. */
  feeds: string[]
  /** How its sources are ranked, in a sentence. */
  ranking: string
  /** The sources that fed it, first highest, by name. */
  order: string[]
}

export interface Explainer {
  /** The function, as a paragraph. */
  function: string
  /** Windows and rules, as label and value. */
  rules: ExplainerItem[]
  /** One per contributor, in the engine's order. */
  rows: ExplainerRow[]
  /** One per group of sources the contributors read. */
  sources: ExplainerSource[]
}

/** The function paragraph. */
export function explainerFunction(): string {
  const n = word(DEFS.length)
  return `Age score = your age + the sum of ${n} impacts, one per contributor, each from its ${SIX_MONTHS}-day average. At its target a contributor adds 0 years; short of it, it adds years; past it, where the evidence says more keeps helping, it removes them. A contributor with no data counts as 0 and the hero says how many of ${n} have data.`
}

/** Days of data before a snapshot, for the calibration line. */
export function daysOfData(input: EngineInput, on: string): number {
  const first = buildSeries(input).firstDay
  if (!Number.isFinite(first)) return 0
  return Math.max(
    0,
    Math.round(Date.parse(`${on}T00:00:00Z`) / 86_400_000 - first)
  )
}

/** Windows and rules. `days` is the days of data behind the snapshot, for
 * the calibration line while it is calibrating. */
export function explainerRules(
  snapshot?: Pick<Snapshot, "calibrating">,
  days?: number
): ExplainerItem[] {
  return [
    {
      key: "What is not in the score",
      value: notInScoreAll(
        HEALTHSPAN_METRICS.map((m) => m.label),
        BODY_CARDS.map((m) => m.label)
      ),
    },
    { key: "Score window", value: `${SIX_MONTHS} days` },
    { key: "Pace window", value: `${THIRTY} days` },
    {
      key: "Pace of aging",
      value: `1 + ${PACE_DAMPING} × (sum of ${THIRTY}-day impacts minus sum of ${SIX_MONTHS}-day impacts) / ${PROJECTION_YEARS}, clamped to ${paceText(PACE_MIN)} to ${paceText(PACE_MAX)}`,
    },
    { key: "Updates", value: "Every Sunday" },
    {
      key: "Calibrating",
      value:
        snapshot?.calibrating && days !== undefined
          ? `Fewer than ${CALIBRATION_DAYS} days of data; ${days} of ${CALIBRATION_DAYS} days so far`
          : `Fewer than ${CALIBRATION_DAYS} days of data`,
    },
    {
      key: "Locked",
      value: `Fewer than ${LOCK_RECOVERIES} nights with a resting heart rate in the last ${LOCK_WINDOW_NIGHTS}`,
    },
    {
      key: "Weekly figures",
      value: `Minutes over the days worn, times 7, needing ${MIN_COVERED_DAYS} worn days`,
    },
    {
      key: "Body readings",
      value: `A VO2 max or body composition reading stands for up to ${BODY_CARRY_FORWARD_DAYS} days when a window has none`,
    },
  ]
}

const SAMPLES = 100

/** The most a contributor adds and removes over [lo, hi], in years. */
function extremes(
  years: (v: number) => number,
  lo: number,
  hi: number
): [number, number] {
  let most = 0
  let least = 0
  for (let i = 0; i <= SAMPLES; i++) {
    const y = years(lo + ((hi - lo) * i) / SAMPLES)
    most = Math.max(most, y)
    least = Math.min(least, y)
  }
  return [most, least]
}

const yearsText = (y: number) =>
  tenths(y) === 0 ? "nothing" : `${impactText(y)} years`

/** One row per contributor. Age is the snapshot's, else the profile's age
 * on `today`; sex the profile's. A row whose target needs a missing age or
 * sex has no target and no years. The range is the bar's: the snapshot's
 * scale where there is one, else the engine's default. */
export function explainerRows(
  snapshot: Pick<Snapshot, "age" | "metrics"> | undefined,
  profile: { birthDate?: string; sex?: Sex },
  today = new Date()
): ExplainerRow[] {
  const age =
    snapshot?.age ??
    (profile.birthDate
      ? ageOn(profile.birthDate, Math.floor(today.getTime() / 86_400_000))
      : undefined)
  const sex: Sex | undefined = profile.sex
  return DEFS.map((d) => {
    const depends = DEPENDS[d.id]
    const needsAge = depends === "age" || depends === "age and sex"
    const needsSex = depends === "sex" || depends === "age and sex"
    const known =
      !(needsAge && age === undefined) && !(needsSex && sex === undefined)
    const row: ExplainerRow = {
      id: d.id,
      label: CONTRIBUTOR_LABELS[d.id],
      feedsFrom: CONTRIBUTOR_INPUTS[d.id].summary,
    }
    if (!known) return row
    // Sex only matters to rows that need it; others take any.
    const st = { birthDate: "", sex: sex ?? "male" } as const
    const a = age ?? 0
    const scale = snapshot?.metrics.find((m) => m.id === d.id)?.scale
    const [most, least] = extremes(
      (v) => d.years(v, a, st),
      scale?.min ?? d.min,
      scale?.max ?? d.max
    )
    return {
      ...row,
      target: valueText(targetFor(d.id, a, st.sex), d.format),
      targetNote: depends ? `for your ${depends}` : undefined,
      addsAtMost: yearsText(most),
      removesAtMost: yearsText(least),
    }
  })
}

/** Which sources feed the contributors, by group: the nights and the
 * workouts in this app's own orders, the measurements and the scans by
 * the repository's ranking, each naming the sources that fed it. */
export function explainerSources(data?: HealthspanSeries): ExplainerSource[] {
  const names = (ids: Iterable<string | undefined>) => [
    ...new Set(
      [...ids]
        .filter((id): id is string => !!id)
        .map((id) => sourceName(data?.sources ?? new Map(), id))
    ),
  ]
  const feeds = (ids: MetricId[]) => ids.map((id) => CONTRIBUTOR_LABELS[id])
  const held = (ids: Iterable<string | undefined>) => new Set(ids)
  const sleeps = held((data?.input.sleeps ?? []).map((s) => s.source))
  const workouts = held((data?.input.workouts ?? []).map((w) => w.source))
  return [
    {
      data: "Sleeps",
      feeds: feeds(["sleepConsistency", "sleepHours"]),
      ranking:
        "This app's night order, kept in its profile: where two sources recorded one night, the night of the source ranked lower is left out whole.",
      order: names((data?.nightOrder ?? []).filter((id) => sleeps.has(id))),
    },
    {
      data: "Workouts",
      feeds: feeds(["zones13", "zones45", "strength"]),
      ranking:
        "This app's workout order, kept in its profile, in the same way.",
      order: names((data?.workoutOrder ?? []).filter((id) => workouts.has(id))),
    },
    {
      data: "Steps, VO2 max and resting heart rate",
      feeds: feeds(["steps", "vo2max", "rhr"]),
      ranking:
        "Your repository's ranking, each source's priority per metric, which every app reading these metrics follows.",
      order: names([
        ...(data?.stepSources ?? []),
        ...(data?.input.rhr ?? []).map((r) => r.source),
        ...(data?.input.vo2 ?? []).map((r) => r.source),
      ]),
    },
    {
      data: "Body composition results",
      feeds: feeds(["leanMass"]),
      ranking:
        "Your sources' priorities for body composition, where two scans fall on one day.",
      order: names((data?.lean ?? []).map((l) => l.source)),
    },
  ]
}

/** The whole explainer. */
export function explainer(
  snapshot: Snapshot | undefined,
  profile: { birthDate?: string; sex?: Sex },
  data?: HealthspanSeries,
  today = new Date()
): Explainer {
  const days =
    snapshot && data ? daysOfData(data.input, snapshot.date) : undefined
  return {
    function: explainerFunction(),
    rules: explainerRules(snapshot, days),
    rows: explainerRows(snapshot, profile, today),
    sources: explainerSources(data),
  }
}
