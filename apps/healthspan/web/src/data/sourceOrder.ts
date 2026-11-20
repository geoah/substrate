// The order a session read (nights, workouts) names: the owner's order kept
// on the profile, then every other source holding such records in a
// default order (docs/plans/healthspan.md, Sources). The server ranks at
// most this many and takes an unnamed source last.

/** The record policy's cap on an order (internal/engine/aggregaterecords.go). */
export const RECORD_ORDER_CAP = 100

/** A source as the orders weigh it. */
export interface OrderSource {
  id: string
  class?: string
  name: string
}

const CLASS_ORDER = ["watch", "ring", "service", "app"]

function classRank(c: string | undefined): number {
  const i = c ? CLASS_ORDER.indexOf(c) : -1
  return i < 0 ? CLASS_ORDER.length : i
}

/** The owner's order first (the ones still holding records), then the
 * other holders by class (watch, ring, service, app, the rest) and name,
 * capped. */
export function recordOrder(
  owners: readonly string[],
  sources: readonly OrderSource[],
  holding: ReadonlySet<string>
): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const id of owners) {
    if (holding.has(id) && !seen.has(id)) {
      seen.add(id)
      out.push(id)
    }
  }
  const rest = sources
    .filter((s) => holding.has(s.id) && !seen.has(s.id))
    .sort(
      (a, b) =>
        classRank(a.class) - classRank(b.class) ||
        a.name.localeCompare(b.name) ||
        a.id.localeCompare(b.id)
    )
  for (const s of rest) {
    seen.add(s.id)
    out.push(s.id)
  }
  // A holder with no source record (written before its source) goes last.
  for (const id of [...holding].sort()) if (!seen.has(id)) out.push(id)
  return out.slice(0, RECORD_ORDER_CAP)
}
