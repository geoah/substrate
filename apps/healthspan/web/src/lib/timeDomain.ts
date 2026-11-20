// The shared time axis of a page of charts: every chart on the page spans
// the same dates, so a metric with one year of results in a five-year view
// shows four years of nothing, labelled.

import { formatMonth } from "./format"

export type RangeId = "1Y" | "2Y" | "5Y" | "All"

export const RANGES: { id: RangeId; label: string; months?: number }[] = [
  { id: "1Y", label: "1 year", months: 12 },
  { id: "2Y", label: "2 years", months: 24 },
  { id: "5Y", label: "5 years", months: 60 },
  { id: "All", label: "All time" },
]

export function isRangeId(v: string | null | undefined): v is RangeId {
  return RANGES.some((r) => r.id === v)
}

/** The first instant a range admits, or undefined for all time. */
export function rangeStart(range: RangeId, today: Date): Date | undefined {
  const months = RANGES.find((r) => r.id === range)?.months
  if (!months) return undefined
  const d = new Date(today)
  d.setMonth(d.getMonth() - months)
  return d
}

export interface TimeDomain {
  start: Date
  end: Date
}

const DAY = 86_400_000

/** Noon local on `today`'s day: the end of every chart's axis. */
export function axisEnd(today: Date): Date {
  const d = new Date(today)
  d.setHours(12, 0, 0, 0)
  return d
}

/** The page-wide domain: from the range's start (for All, the earliest of
 * `results`, so it stays put while filtering) to today. A list passes
 * every result it indexes; a history screen passes its metric's results,
 * so its All starts at the metric's first result. With no results, All
 * falls back to one year. */
export function pageDomain(
  range: RangeId,
  today: Date,
  results: readonly { at: string }[]
): TimeDomain {
  const end = axisEnd(today)
  const start = rangeStart(range, end)
  if (start) return { start, end }
  let first = Number.POSITIVE_INFINITY
  for (const r of results) {
    const t = Date.parse(r.at)
    if (t < first) first = t
  }
  if (!Number.isFinite(first))
    return { start: rangeStart("1Y", end) as Date, end }
  return { start: new Date(Math.min(first, end.getTime())), end }
}

/** Whether an instant lies inside the domain, bounds included. */
export function inDomain(at: string | number | Date, d: TimeDomain): boolean {
  const t = new Date(at).getTime()
  return t >= d.start.getTime() && t <= d.end.getTime()
}

export interface EmptySpan {
  from: Date
  to: Date
  /** "No results before Jul 2025". */
  label: string
}

/** The span before a metric's first result, hatched on its chart;
 * undefined when the first result is less than a day after the domain's
 * start. */
export function emptySpan(
  start: Date,
  firstAt: string | number | Date
): EmptySpan | undefined {
  const to = new Date(firstAt)
  if (to.getTime() - start.getTime() < DAY) return undefined
  return { from: start, to, label: `No results before ${formatMonth(to)}` }
}
