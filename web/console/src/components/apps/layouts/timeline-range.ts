/** The timeline's chosen range: two LOCAL calendar days, inclusive, that
 * stand in for the view's `window` while a person is looking. The choice is
 * never written to the view record; it lives in the URL as `from` and `to`
 * (`YYYY-MM-DD`), so back, forward and a shared link all land on the same
 * days. Here: the day key the URL carries, the quick ranges the chips offer,
 * the window as a range (what the custom inputs prefill with), the bounds a
 * read sends, and the label the header shows. */

import { parseDuration } from "@/lib/apps/time"
import type { ViewSpec } from "@/lib/apps/spec"
import { DEFAULT_FUTURE, DEFAULT_PAST, dayKey } from "./timeline-window"

/** A local calendar day as `YYYY-MM-DD`, which sorts as it reads. */
export type DayKey = string

export interface TimelineRange {
  from: DayKey
  to: DayKey
}

const DAY_KEY = /^(\d{4})-(\d{2})-(\d{2})$/

/** The key as a local date, or nothing when it is not a real calendar day
 * (`2026-02-30` is refused, not rolled into March). */
function dateOf(key: DayKey): Date | undefined {
  const m = DAY_KEY.exec(key)
  if (!m) return undefined
  const [y, mo, d] = [Number(m[1]), Number(m[2]), Number(m[3])]
  const date = new Date(y, mo - 1, d)
  if (
    date.getFullYear() !== y ||
    date.getMonth() !== mo - 1 ||
    date.getDate() !== d
  ) {
    return undefined
  }
  return date
}

/** A URL value as a day key, or nothing: anything but a real
 * `YYYY-MM-DD` reads as absent, so a mangled link falls back to the window. */
export function parseDayKey(value: unknown): DayKey | undefined {
  if (typeof value !== "string") return undefined
  return dateOf(value.trim()) ? value.trim() : undefined
}

/** The local start of the day, in epoch milliseconds. */
export function startOfDay(key: DayKey): number {
  return (dateOf(key) ?? new Date(NaN)).getTime()
}

/** A key moved by whole days, months or years; a month step clamps to the
 * last day when the target month is shorter. */
function shift(
  key: DayKey,
  by: { days?: number; months?: number; years?: number }
): DayKey {
  const d = dateOf(key) ?? new Date()
  const day = d.getDate()
  d.setDate(1)
  d.setMonth(d.getMonth() + (by.months ?? 0) + 12 * (by.years ?? 0))
  const last = new Date(d.getFullYear(), d.getMonth() + 1, 0).getDate()
  d.setDate(Math.min(day, last) + (by.days ?? 0))
  return dayKey(d.getTime())
}

/** The four spans a chip offers, each computed from today. A week runs
 * Monday to Sunday. */
export type QuickRangeKey = "week" | "month" | "next3" | "past"

export interface QuickRange {
  key: QuickRangeKey
  label: string
  range: (now: number) => TimelineRange
}

export const QUICK_RANGES: readonly QuickRange[] = [
  {
    key: "week",
    label: "This week",
    range: (now) => {
      const today = dayKey(now)
      const weekday = (new Date(now).getDay() + 6) % 7
      const from = shift(today, { days: -weekday })
      return { from, to: shift(from, { days: 6 }) }
    },
  },
  {
    key: "month",
    label: "This month",
    range: (now) => {
      const d = new Date(now)
      const from = dayKey(new Date(d.getFullYear(), d.getMonth(), 1).getTime())
      const to = dayKey(
        new Date(d.getFullYear(), d.getMonth() + 1, 0).getTime()
      )
      return { from, to }
    },
  },
  {
    key: "next3",
    label: "Next 3 months",
    range: (now) => {
      const from = dayKey(now)
      return { from, to: shift(shift(from, { months: 3 }), { days: -1 }) }
    },
  },
  {
    key: "past",
    label: "Past month",
    range: (now) => {
      const to = dayKey(now)
      return { from: shift(to, { months: -1 }), to }
    },
  },
]

/** The quick range the active range IS, day for day, or nothing: a range
 * that matches none is the custom one. */
export function activeQuickRange(
  range: TimelineRange,
  now: number
): QuickRangeKey | undefined {
  return QUICK_RANGES.find((q) => {
    const r = q.range(now)
    return r.from === range.from && r.to === range.to
  })?.key
}

/** The view's window as a range of days: the day `now - past` falls on
 * through the day `now + future` falls on. What the custom inputs prefill
 * with when nothing has been chosen. */
export function windowRange(spec: ViewSpec, now: number): TimelineRange {
  const past =
    parseDuration(spec.window.past ?? DEFAULT_PAST) ??
    parseDuration(DEFAULT_PAST) ??
    0
  const future =
    parseDuration(spec.window.future ?? DEFAULT_FUTURE) ??
    parseDuration(DEFAULT_FUTURE) ??
    0
  return { from: dayKey(now - past), to: dayKey(now + future) }
}

/** The range the URL asks for, or nothing when it asks for none. A lone
 * `from` or `to` completes itself from the window's other end, and an
 * inverted pair is read the right way round. */
export function resolveRange(
  params: { from: DayKey | null; to: DayKey | null },
  window: TimelineRange
): TimelineRange | undefined {
  if (!params.from && !params.to) return undefined
  const from = params.from ?? window.from
  const to = params.to ?? window.to
  return from <= to ? { from, to } : { from: to, to: from }
}

/** What a read sends for the range: the local start of `from` and the local
 * start of the day after `to`, so `to` is included whole. */
export function rangeBounds(range: TimelineRange): { gte: string; lt: string } {
  return {
    gte: new Date(startOfDay(range.from)).toISOString(),
    lt: new Date(startOfDay(shift(range.to, { days: 1 }))).toISOString(),
  }
}

export function inRange(key: DayKey, range: TimelineRange): boolean {
  return key >= range.from && key <= range.to
}

const MONTHS = [
  "Jan",
  "Feb",
  "Mar",
  "Apr",
  "May",
  "Jun",
  "Jul",
  "Aug",
  "Sep",
  "Oct",
  "Nov",
  "Dec",
]

/** `12 Sep`, the year joining only once it is not this year. Spelled by
 * hand so the header reads the same in every locale. */
export function dayText(key: DayKey, now: number): string {
  const d = dateOf(key)
  if (!d) return key
  const year =
    d.getFullYear() === new Date(now).getFullYear() ? "" : ` ${d.getFullYear()}`
  return `${d.getDate()} ${MONTHS[d.getMonth()]}${year}`
}

/** `12 Sep – 12 Oct`; a one-day range is that day alone. */
export function rangeLabel(range: TimelineRange, now: number): string {
  if (range.from === range.to) return dayText(range.from, now)
  return `${dayText(range.from, now)} – ${dayText(range.to, now)}`
}
