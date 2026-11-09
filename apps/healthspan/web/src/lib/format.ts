// How values, dates and spans print. Dates are British day-month-year in
// the reader's time zone, as the labs print them.

import { withUnit } from "./units"

const DAY = 86_400_000

/** A value at `precision` decimals (default: as many as it needs, up to
 * three), with its comparator: `<0.5`, `3.40`. */
export function formatValue(
  value: number | undefined,
  precision?: number,
  comparator?: string
): string {
  if (value === undefined || !Number.isFinite(value)) return "-"
  const p =
    precision ?? (Number.isInteger(value) ? 0 : Math.abs(value) < 10 ? 2 : 1)
  const s = value.toLocaleString("en-GB", {
    minimumFractionDigits: Math.min(p, 3),
    maximumFractionDigits: Math.min(p, 3),
  })
  return comparator ? `${comparator}${s}` : s
}

/** A range with inclusive bounds, as the rules read it: `7.0 to 12.0`, `up
 * to 3.0`, `90 or more`. Empty with neither bound. */
export function rangeText(
  low: number | undefined,
  high: number | undefined,
  precision?: number
): string {
  if (low !== undefined && high !== undefined)
    return `${formatValue(low, precision)} to ${formatValue(high, precision)}`
  if (high !== undefined) return `up to ${formatValue(high, precision)}`
  if (low !== undefined) return `${formatValue(low, precision)} or more`
  return ""
}

/** A range with its unit: `up to 3.00 mmol/L`, `7.0 to 12.0%`. */
export function rangeWithUnit(
  low: number | undefined,
  high: number | undefined,
  precision?: number,
  unit?: string
): string {
  if (low !== undefined && high !== undefined)
    return withUnit(
      `${formatValue(low, precision)} to ${formatValue(high, precision)}`,
      unit
    )
  if (high !== undefined)
    return `up to ${withUnit(formatValue(high, precision), unit)}`
  if (low !== undefined)
    return `${withUnit(formatValue(low, precision), unit)} or more`
  return ""
}

/** A trend rate without its sign, one decimal: `0.5%/month`. The arrow
 * beside it gives the direction. */
export function formatRate(ratePerMonth: number): string {
  return `${formatValue(Math.abs(ratePerMonth), 1)}%/month`
}

/** `12 Mar 2026`. */
export function formatDate(iso: string | number | Date): string {
  const d = iso instanceof Date ? iso : new Date(iso)
  return d.toLocaleDateString("en-GB", {
    day: "numeric",
    month: "short",
    year: "numeric",
  })
}

/** `24 Oct`: day and short month, for a date the context already places in
 * the current year ("night to 24 Oct"). */
export function formatShortDate(iso: string | number | Date): string {
  const d = iso instanceof Date ? iso : new Date(iso)
  return d.toLocaleDateString("en-GB", { day: "numeric", month: "short" })
}

/** `Mon 23 Aug 2027, 09:40`, or `to 10:00` added when an end is given on
 * the same day. In the reader's time zone. */
export function formatDateTime(iso: string, endIso?: string): string {
  const d = new Date(iso)
  const day = d.toLocaleDateString("en-GB", {
    weekday: "short",
    day: "numeric",
    month: "short",
    year: "numeric",
  })
  const time = (x: Date) =>
    x.toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit" })
  const end = endIso ? new Date(endIso) : undefined
  const sameDay = end && end.toDateString() === d.toDateString() && end > d
  return `${day}, ${time(d)}${sameDay ? ` to ${time(end)}` : ""}`
}

/** `183 kB`, `1.2 MB`. */
export function formatBytes(n: number): string {
  if (n < 1000) return `${n} B`
  if (n < 1_000_000) return `${Math.round(n / 1000)} kB`
  return `${(n / 1_000_000).toFixed(1)} MB`
}

/** `Mar 2026`. */
export function formatMonth(iso: string | number | Date): string {
  const d = iso instanceof Date ? iso : new Date(iso)
  return d.toLocaleDateString("en-GB", { month: "short", year: "numeric" })
}

/** Whole months between two instants, rounded down. */
export function monthsBetween(from: string | number | Date, to: Date): number {
  const a = new Date(from)
  let m =
    (to.getFullYear() - a.getFullYear()) * 12 + (to.getMonth() - a.getMonth())
  if (to.getDate() < a.getDate()) m--
  return Math.max(0, m)
}

/** `14 months ago`, `2 years ago`, `5 days ago`, `today`. */
export function ago(from: string | number | Date, to: Date): string {
  const days = Math.floor((to.getTime() - new Date(from).getTime()) / DAY)
  if (days < 1) return "today"
  if (days < 45) return days === 1 ? "yesterday" : `${days} days ago`
  const m = monthsBetween(from, to)
  if (m < 24) return `${m} months ago`
  return `${Math.floor(m / 12)} years ago`
}

export function capitalize(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1)
}

/** `left_arm` -> `Left arm`. */
export function channelLabel(channel: string): string {
  return capitalize(channel.replace(/_/g, " "))
}
