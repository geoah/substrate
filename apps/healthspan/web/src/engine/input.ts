// The engine's input: the sleeps, workouts and readings the shared kinds
// hold, parsed but not yet placed on days (healthspan/fromShared.ts builds
// it from records). Times are RFC 3339 instants; offsets are minutes east
// of UTC.

/** One `health/sleep` record. */
export interface SleepSession {
  at: string
  endsAt: string
  /** Where it ended; the newest row's offset when the record has none. */
  offsetEnd?: number
  /** The source's id, after `observations/source/`. */
  source?: string
  nap: boolean
  /** Light, deep and REM together, in ms. Undefined where the record
   * carries no stage total, which leaves the sleep uncounted. */
  asleepMs?: number
  /** The source's own sleep consistency score, percent. */
  consistency?: number
}

/** One `health/workout` record. */
export interface WorkoutSession {
  at: string
  endsAt?: string
  source?: string
  /** Where it began; the newest row's offset when the record has none. */
  offsetStart?: number
  /** The workout kind's shared code (`strength`, `running`). */
  sport?: string
  /** The sport as the source named it (`weightlifting`). */
  sportSource?: string
  /** Time in each heart rate zone in ms, by zone name (`zone1`). */
  zones?: Record<string, number>
  strain?: number
  averageHeartRate?: number
}

/** One resting heart rate summary: the span it measures and its value. */
export interface RestingSummary {
  at: string
  endsAt?: string
  /** Its UTC offset; the newest row's when it has none. */
  offset?: number
  source?: string
  /** Beats per minute. */
  value: number
}

/** A dated scalar: a VO2 max reading, a scan's lean body mass. */
export interface Sample {
  at: string
  value: number
  source?: string
}

export interface EngineInput {
  /** Every sleep the night order keeps, counted or not. */
  sleeps: SleepSession[]
  /** Every sleep of every source, before the night order: a resting heart
   * rate summary's night is its own source's sleep, whichever source the
   * night order prefers. Absent, `sleeps`. */
  restingSleeps?: SleepSession[]
  /** Every workout the workout order keeps, counted or not. */
  workouts: WorkoutSession[]
  /** The summaries the priority policy keeps. */
  rhr: RestingSummary[]
  /** Daily step totals by local date (`YYYY-MM-DD`). */
  steps: ReadonlyMap<string, number>
  /** VO2 max readings, ml/kg/min. */
  vo2: Sample[]
  /** Lean body mass, percent of body weight, one per scan. */
  lean: Sample[]
}

export const EMPTY_INPUT: EngineInput = {
  sleeps: [],
  workouts: [],
  rhr: [],
  steps: new Map(),
  vo2: [],
  lean: [],
}
