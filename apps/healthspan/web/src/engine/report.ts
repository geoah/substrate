// What the screen renders: one weekly snapshot per Sunday, each with the nine
// contributors. The engine builds it; the components only read it.

export type SectionId = "sleep" | "strain" | "fitness"

export type MetricId =
  | "sleepConsistency"
  | "sleepHours"
  | "zones13"
  | "zones45"
  | "strength"
  | "steps"
  | "vo2max"
  | "rhr"
  | "leanMass"

/** How a value prints: `68%`, `7:03 h`, `8,443 Steps`, `45 ml/kg/min`. */
export type ValueFormat =
  "percent" | "percent1" | "hours" | "steps" | "vo2" | "bpm"

export interface Scale {
  min: number
  max: number
  minLabel: string
  maxLabel: string
  /** The years each bar segment would add at its midpoint, low end first.
   * Drives the segment colours, so a contributor that never subtracts
   * (lean body mass) never shows green. */
  segments: number[]
}

export interface Verdict {
  title: string
  body: string
}

export interface MetricPoint {
  /** The snapshot's Sunday, `YYYY-MM-DD`. */
  date: string
  sixMonth: number | null
  thirtyDay: number | null
  impact: number | null
}

export interface Metric {
  id: MetricId
  section: SectionId
  label: string
  format: ValueFormat
  /** 6-month average; null when the window holds no data. */
  sixMonth: number | null
  /** 30-day average. */
  thirtyDay: number | null
  /** Years added (+) or removed (−) by the 6-month average. */
  impact: number | null
  /** Years the 30-day average would add or remove. */
  impact30: number | null
  /** Years the 6-month average adds; zero when it removes years. */
  opportunity: number
  scale: Scale
  verdict: Verdict
  /** [value, years] pairs over the bar's range widened to cover both
   * averages, for what-ifs. */
  curve: [number, number][]
  /** Where the value came from, in a sentence. */
  source: string
  history: MetricPoint[]
}

export interface Snapshot {
  /** Sunday the snapshot is computed on, `YYYY-MM-DD`. */
  date: string
  /** The week it is shown for: `date` to the Saturday after (the data
   * is everything before `date`). */
  weekStart: string
  weekEnd: string
  age: number
  /** The age score: age plus the nine impacts. */
  ageScore: number
  /** ageScore − age; negative is younger. */
  delta: number
  pace: number
  /** Fewer than 90 days of data before this snapshot. */
  calibrating: boolean
  /** Fewer than 21 resting heart rate nights in the 31 before it. */
  locked: boolean
  /** Contributors with data, out of nine. The rest count as zero. */
  contributors: number
  coach: Verdict
  metrics: Metric[]
}

export interface Report {
  /** Oldest first; the last one is the current week. */
  snapshots: Snapshot[]
  /** Days until the next Sunday update, 1 to 7. */
  nextUpdateDays: number
  /** Set when the latest Sunday lacked 21 resting heart rate nights in
   * 31: the age score stops updating and the newest snapshot is older than
   * this week. */
  paused?: { recoveries: number }
}

export const SECTIONS: { id: SectionId; title: string }[] = [
  { id: "sleep", title: "Sleep" },
  { id: "strain", title: "Strain" },
  { id: "fitness", title: "Fitness" },
]
