// The sources of measured values and the imports (docs/plans/healthspan.md,
// Sources; Import). The contract getting started and the age score read
// sources through; the sources workstream implements it, live and demo.

import { pending, settled, type Read } from "./read"

/** One `observations/source` record and what it holds. */
export interface SourceSummary {
  /** The id after `observations/source/`. */
  id: string
  name: string
  class?: string
  /** The metrics it reports, by metric id, with its newest value's time. */
  metrics: { metric: string; latest?: string }[]
  /** How many `health/sleep` and `health/workout` records it holds. */
  nights: number
  workouts: number
  /** For a lab: how many reports. */
  reports: number
  /** Its rank per metric, keyed by the metric's id, 1 first. */
  priorities: Record<string, number>
}

export interface SourcesData {
  /** Every source, in no particular order. */
  useSourceList(): Read<SourceSummary[]>
}

export const liveSources: SourcesData = {
  useSourceList: () => pending(),
}

export const demoSources: SourcesData = {
  useSourceList: () => settled([]),
}
