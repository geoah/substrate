// The groups sources are ranked in (docs/plans/healthspan.md, Sources):
// the metrics the screens read together, ranked by each source's
// `priorities` for every metric of the group, which every reader of those
// metrics follows; and the sessions, nights and workouts, whose order is
// this app's own, on its profile.

import { KIND } from "../kinds"

export interface MetricGroup {
  id: string
  label: string
  /** The group's metrics by id, where it names them. */
  metrics?: readonly string[]
  /** Every metric whose id starts so, where the group is a whole catalogue
   * (the lab values). */
  prefix?: string
}

export const METRIC_GROUPS: readonly MetricGroup[] = [
  {
    id: "heart",
    label: "Heart",
    metrics: [
      "health/resting_heart_rate",
      "health/hrv_rmssd",
      "health/hrv_sdnn",
    ],
  },
  {
    id: "activity",
    label: "Steps and distance",
    metrics: ["health/steps", "health/distance"],
  },
  { id: "vo2max", label: "VO2 max", metrics: ["health/vo2_max"] },
  {
    id: "body",
    label: "Body composition",
    metrics: [
      "health/body_mass",
      "health/body_fat_fraction",
      "health/fat_mass",
      "health/fat_free_mass",
      "health/lean_mass",
      "health/muscle_mass",
      "health/bone_mass",
      "health/body_water_fraction",
      "health/bmi",
      "health/visceral_fat_rating",
      "health/waist_circumference",
      "health/waist_hip_ratio",
    ],
  },
  { id: "labs", label: "Lab values", prefix: "labs/" },
]

export type SessionKind = "nights" | "workouts"

export const SESSION_GROUPS: readonly { id: SessionKind; label: string }[] = [
  { id: "nights", label: "Nights" },
  { id: "workouts", label: "Workouts" },
]

export function inGroup(group: MetricGroup, metric: string): boolean {
  return group.prefix
    ? metric.startsWith(group.prefix)
    : (group.metrics ?? []).includes(metric)
}

/** The group's metrics among `known` (the repository's metric records and
 * what its sources report): a priority may name only a metric that
 * exists. */
export function groupMetrics(
  group: MetricGroup,
  known: Iterable<string>
): string[] {
  const set = new Set<string>()
  for (const m of known) if (inGroup(group, m)) set.add(m)
  return [...set].sort()
}

/** A metric's full record path, the key a priority is written under. */
export function metricPath(id: string): string {
  return `${KIND.metric}/${id}`
}

/** A priority's key as the app reads it: a metric's id where the key is an
 * `observations/metric` path, else the key as written. */
export function priorityKey(key: string): string {
  const prefix = `${KIND.metric}/`
  return key.startsWith(prefix) ? key.slice(prefix.length) : key
}

/** A priority's key as it is written: the full record path. A key whose
 * first segment names an authority is one already. */
export function priorityPath(key: string): string {
  return key.split("/")[0]?.includes(".") ? key : metricPath(key)
}
