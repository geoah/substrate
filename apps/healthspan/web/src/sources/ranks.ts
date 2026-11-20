// Ranks per group (docs/plans/healthspan.md, Sources). A metric group's
// order is read from, and written to, each source's `priorities` for every
// metric of the group, 1 first; a session group's is the profile's order,
// then every other holder in the default order.

import type { SourceSummary } from "../data/sources"
import { recordOrder } from "../data/sourceOrder"
import { groupMetrics, inGroup, priorityPath, type MetricGroup } from "./groups"
import { defaultOrder } from "./list"
import { keepLinkedTogether, type SourceLink } from "./links"

/** The sources reporting any metric of the group. */
export function groupSources(
  group: MetricGroup,
  list: readonly SourceSummary[]
): SourceSummary[] {
  return list.filter((s) => s.metrics.some((m) => inGroup(group, m.metric)))
}

/** A source's rank in a group: its best priority for the group's
 * metrics; undefined where it ranks none of them. */
export function groupRank(
  group: MetricGroup,
  s: SourceSummary
): number | undefined {
  let best: number | undefined
  for (const [k, v] of Object.entries(s.priorities))
    if (inGroup(group, k) && (best === undefined || v < best)) best = v
  return best
}

/** A group's sources in their order: the ranked by rank, then the
 * unranked by class and name, as the read ranks an unranked source last. */
export function metricOrder(
  group: MetricGroup,
  list: readonly SourceSummary[]
): SourceSummary[] {
  return groupSources(group, list).sort((a, b) => {
    const ra = groupRank(group, a)
    const rb = groupRank(group, b)
    if (ra !== undefined && rb !== undefined && ra !== rb) return ra - rb
    if (ra !== undefined && rb === undefined) return -1
    if (ra === undefined && rb !== undefined) return 1
    return defaultOrder(a, b)
  })
}

/** What each source's `priorities` becomes for a new order of a group:
 * its rank (1 first) for every metric of the group, every other key kept,
 * written as full record paths. A source whose priorities would not change
 * is left out. */
export function priorityWrites(
  group: MetricGroup,
  order: readonly SourceSummary[],
  known: Iterable<string>
): { id: string; priorities: Record<string, number> }[] {
  const metrics = groupMetrics(group, [
    ...known,
    ...order.flatMap((s) => s.metrics.map((m) => m.metric)),
  ])
  const out: { id: string; priorities: Record<string, number> }[] = []
  order.forEach((s, i) => {
    const next: Record<string, number> = {}
    for (const [k, v] of Object.entries(s.priorities)) next[priorityPath(k)] = v
    let changed = false
    for (const m of metrics) {
      const key = priorityPath(m)
      if (next[key] !== i + 1) changed = true
      next[key] = i + 1
    }
    if (changed) out.push({ id: s.id, priorities: next })
  })
  return out
}

/** A session group's holders in the order its read names them: the
 * profile's order, then every other holder by class and name, each link's
 * sources together. */
export function sessionOrder(
  kind: "nights" | "workouts",
  owners: readonly string[],
  list: readonly SourceSummary[],
  links: readonly SourceLink[]
): SourceSummary[] {
  const holding = new Set(
    list
      .filter((s) => (kind === "nights" ? s.nights : s.workouts) > 0)
      .map((s) => s.id)
  )
  const ids = keepLinkedTogether(recordOrder(owners, list, holding), links)
  const byId = new Map(list.map((s) => [s.id, s]))
  return ids.flatMap((id) => {
    const s = byId.get(id)
    return s ? [s] : []
  })
}

/** An order with one entry moved from `from` to `to`. */
export function moved<T>(order: readonly T[], from: number, to: number): T[] {
  const out = [...order]
  if (from < 0 || from >= out.length) return out
  const [item] = out.splice(from, 1)
  out.splice(Math.max(0, Math.min(to, out.length)), 0, item)
  return out
}
