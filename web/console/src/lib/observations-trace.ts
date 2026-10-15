/** A trace's own values, without React: each value placed in time, a
 * level's breaks past its hold, and the managed `stats` the engine keeps on
 * it. */

import { durationMs } from "@/lib/observations-time"

// ── a trace's own values ───────────────────────────────────────────────────

/** One value of a trace, placed in time. */
export interface TracePoint {
  at: number
  value: string | null
  code?: string | null
}

/** A trace's values in time order: each at `at` plus its offset in
 * milliseconds, or plus `every` times its position. `values` and `codes`
 * line up with the offsets; whichever the trace holds is read. A trace
 * whose spacing does not parse answers nothing. */
export function tracePoints(properties: Record<string, unknown>): TracePoint[] {
  const at = typeof properties.at === "string" ? Date.parse(properties.at) : NaN
  if (Number.isNaN(at)) return []
  const values = Array.isArray(properties.values) ? properties.values : null
  const codes = Array.isArray(properties.codes) ? properties.codes : null
  const length = values?.length ?? codes?.length ?? 0
  const offsets = Array.isArray(properties.offsets)
    ? properties.offsets.map(Number)
    : null
  const every = durationMs(
    typeof properties.every === "string" ? properties.every : undefined
  )
  if (!offsets && every === undefined) return []
  const out: TracePoint[] = []
  for (let i = 0; i < length; i++) {
    const offset = offsets ? offsets[i] : every! * i
    if (!Number.isFinite(offset)) continue
    const raw = values?.[i]
    out.push({
      at: at + offset,
      value:
        typeof raw === "string"
          ? raw
          : typeof raw === "number"
            ? `${raw}`
            : null,
      ...(codes
        ? { code: typeof codes[i] === "string" ? codes[i] : null }
        : {}),
    })
  }
  return out
}

/** A trace's points with a break (`null`) wherever two neighbours sit
 * further apart than the trace's `hold`: past it a level is not known
 * (decision 0158), so a line must not slope across the gap. No hold, no
 * breaks. */
export function breakAtGaps(
  points: readonly TracePoint[],
  holdMs: number | undefined
): (TracePoint | null)[] {
  if (holdMs === undefined) return [...points]
  const out: (TracePoint | null)[] = []
  points.forEach((p, i) => {
    if (i > 0 && p.at - points[i - 1].at > holdMs) out.push(null)
    out.push(p)
  })
  return out
}

/** A trace's managed `stats`, read defensively: the engine writes them. */
export interface TraceStats {
  count?: number
  min?: string
  max?: string
  mean?: number
  m2?: number
  first?: { at: string; value: string }
  last?: { at: string; value: string }
  coverage?: string
}

function point(raw: unknown): { at: string; value: string } | undefined {
  if (!raw || typeof raw !== "object") return undefined
  const p = raw as Record<string, unknown>
  return typeof p.at === "string" &&
    (typeof p.value === "string" || typeof p.value === "number")
    ? { at: p.at, value: String(p.value) }
    : undefined
}

export function traceStats(raw: unknown): TraceStats | undefined {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return undefined
  const s = raw as Record<string, unknown>
  const num = (v: unknown) =>
    typeof v === "number" && Number.isFinite(v) ? v : undefined
  const dec = (v: unknown) =>
    typeof v === "string" ? v : typeof v === "number" ? String(v) : undefined
  return {
    count: num(s.count),
    min: dec(s.min),
    max: dec(s.max),
    mean: num(s.mean),
    m2: num(s.m2),
    first: point(s.first),
    last: point(s.last),
    coverage: typeof s.coverage === "string" ? s.coverage : undefined,
  }
}

/** The standard deviation `m2` holds: the variance is `m2 / count`
 * (docs/observations.md, "Stats"), the population's. */
export function stddevOf(stats: TraceStats): number | undefined {
  if (stats.m2 === undefined || !stats.count) return undefined
  return Math.sqrt(stats.m2 / stats.count)
}

/** A plotted number as digits, at most `digits` after the point, trailing
 * zeros dropped: `131.6`, `0.25`, `1812`. */
export function plainNumber(n: number, digits = 2): string {
  return Number(n.toFixed(digits)).toString()
}
