// Which source a result came from, as the charts show it: one marker shape
// per source, the same on every card and on a history screen; a legend per
// metric with counts; the "Latest:" line naming the newest result's source.
//
// Nothing here names a source. The caller passes the sources in the order
// the shapes go to (`sourcesByCount` gives the Labs order) and each one's
// name, from its `observations/source` record.

import { formatDate } from "./format"
import type { TrendMarker } from "./trendMath"

/** The order sources receive the shapes. */
export const MARKER_ORDER: TrendMarker[] = [
  "circle",
  "square",
  "diamond",
  "triangle",
  "cross",
]

/** The key and name of results that name no source. */
export const UNKNOWN_SOURCE = ""
const UNKNOWN_NAME = "Unknown source"

/** Each source's record by id, as the caller read them: only `name` is
 * used. A source with no record shows its id. */
export type SourceNames = ReadonlyMap<string, { name?: string }>

/** A result as these helpers read it: when it is and which source wrote
 * it. */
export interface SourcedResult {
  at: string
  source?: string
}

export interface SourceMark {
  /** The source id, or `UNKNOWN_SOURCE`. */
  id: string
  name: string
  /** The count the caller ranked it by (all-time results on Labs); 0 when
   * it gave none. */
  count: number
  marker: TrendMarker
}

/** A source in the caller's order. */
export interface RankedSource {
  id: string
  count?: number
}

function sourceName(names: SourceNames, id: string | undefined): string {
  if (!id) return UNKNOWN_NAME
  return names.get(id)?.name || id
}

/** Every source with results, by result count, most first (ties by name):
 * the order the Labs screen gives the shapes in. `results` are all of them,
 * derived twins left out by the caller. */
export function sourcesByCount(
  results: readonly Pick<SourcedResult, "source">[],
  names: SourceNames
): RankedSource[] {
  const counts = new Map<string, number>()
  for (const r of results) {
    const id = r.source ?? UNKNOWN_SOURCE
    counts.set(id, (counts.get(id) ?? 0) + 1)
  }
  return [...counts]
    .map(([id, count]) => ({ id, count, name: sourceName(names, id) }))
    .sort((a, b) => b.count - a.count || a.name.localeCompare(b.name))
    .map(({ id, count }) => ({ id, count }))
}

/** Each source in `order` with its marker, by its place: circle, square,
 * diamond, triangle, cross; the sixth and later share cross. A repeated id
 * keeps its first place. Iteration order is `order`. */
export function sourceMarkers(
  order: readonly RankedSource[],
  names: SourceNames
): Map<string, SourceMark> {
  const out = new Map<string, SourceMark>()
  for (const s of order) {
    if (out.has(s.id)) continue
    out.set(s.id, {
      id: s.id,
      name: sourceName(names, s.id),
      count: s.count ?? 0,
      marker: MARKER_ORDER[Math.min(out.size, MARKER_ORDER.length - 1)],
    })
  }
  return out
}

/** The mark of a result's source; a source not in the order gets cross. */
export function markOf(
  markers: Map<string, SourceMark>,
  r: Pick<SourcedResult, "source">
): SourceMark {
  const id = r.source ?? UNKNOWN_SOURCE
  return (
    markers.get(id) ?? {
      id,
      name: id || UNKNOWN_NAME,
      count: 0,
      marker: "cross",
    }
  )
}

/** "6 results", "1 result". */
export function resultCount(n: number): string {
  return `${n.toLocaleString("en-GB")} ${n === 1 ? "result" : "results"}`
}

export interface LegendItem {
  /** The source id; matches the chart point's `series`. */
  key: string
  /** "Lab A, 6 results", or "Lab A, 4 of 6 results" when some fall
   * outside the range. */
  label: string
  name: string
  /** All of this source's results. */
  count: number
  /** Those inside the range (equal to `count` without a range). */
  shown: number
  marker: TrendMarker
}

/** The legend of one metric's results: every source present, in the
 * markers' order, with how many of its results `inRange` keeps when that
 * is not all of them ("Lab A, 4 of 6 results", "0 of 2 results"). */
export function seriesLegend<R extends Pick<SourcedResult, "source">>(
  results: readonly R[],
  markers: Map<string, SourceMark>,
  inRange: (r: R) => boolean = () => true
): LegendItem[] {
  const counts = new Map<string, number>()
  const shown = new Map<string, number>()
  for (const r of results) {
    const id = r.source ?? UNKNOWN_SOURCE
    counts.set(id, (counts.get(id) ?? 0) + 1)
    if (inRange(r)) shown.set(id, (shown.get(id) ?? 0) + 1)
  }
  const order = [...markers.keys()]
  const rank = (id: string) => {
    const i = order.indexOf(id)
    return i < 0 ? order.length : i
  }
  return [...counts]
    .sort((a, b) => rank(a[0]) - rank(b[0]) || a[0].localeCompare(b[0]))
    .map(([id, count]) => {
      const m = markOf(markers, { source: id || undefined })
      const n = shown.get(id) ?? 0
      return {
        key: id,
        name: m.name,
        count,
        shown: n,
        marker: m.marker,
        label:
          n === count
            ? `${m.name}, ${resultCount(count)}`
            : `${m.name}, ${n.toLocaleString("en-GB")} of ${resultCount(count)}`,
      }
    })
}

export interface LatestLine {
  at: string
  /** "20 Oct 2026". */
  date: string
  /** The newest result's source name, when it names one. */
  source?: string
  /** "Latest: 20 Oct 2026, Lab B". */
  text: string
}

/** The "Latest:" line of a card: the newest result's date and source.
 * `results` are in time order, oldest first. */
export function latestLine(
  names: SourceNames,
  results: readonly SourcedResult[]
): LatestLine | undefined {
  const newest = results[results.length - 1]
  if (!newest) return undefined
  const date = formatDate(newest.at)
  const source = newest.source ? sourceName(names, newest.source) : undefined
  return {
    at: newest.at,
    date,
    source,
    text: `Latest: ${date}${source ? `, ${source}` : ""}`,
  }
}

/** A Source filter's options: every named source in the markers' order, as
 * "Lab A (435)" with its count. */
export function sourceOptions(
  markers: Map<string, SourceMark>
): { value: string; label: string }[] {
  return [...markers.values()]
    .filter((m) => m.id !== UNKNOWN_SOURCE)
    .map((m) => ({
      value: m.id,
      label: `${m.name} (${m.count.toLocaleString("en-GB")})`,
    }))
}
