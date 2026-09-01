/** Display formatting for substrate data: relative timestamps for table
 * temporality, compact absolute stamps for detail contexts, and the flat
 * rendering of a property value into a table cell. */

import { readReference } from "@/lib/api/types"
import { splitRecordPath } from "@/lib/record-path"

const MINUTE = 60_000
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

/** `3m ago` / `2h ago` / `5d ago`, falling to a date once it stops being
 * recent. Future stamps read `in …` (calendar events legitimately are). */
export function relativeTime(iso: string, now = Date.now()): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return iso
  const delta = now - t
  const abs = Math.abs(delta)
  if (abs < MINUTE) return delta >= 0 ? "just now" : "in <1m"
  const phrase = (n: number, unit: string) =>
    delta >= 0 ? `${n}${unit} ago` : `in ${n}${unit}`
  if (abs < HOUR) return phrase(Math.floor(abs / MINUTE), "m")
  if (abs < DAY) return phrase(Math.floor(abs / HOUR), "h")
  if (abs < 14 * DAY) return phrase(Math.floor(abs / DAY), "d")
  return shortDate(iso)
}

/** `2026-08-04` — table-cell temporal voice, LOCAL calendar day (a stamp
 * late at night must not read as tomorrow). */
export function shortDate(iso: string): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return iso
  const d = new Date(t)
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
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

/** `Aug 6, 01:10` — the table datetime voice, local timezone; the year joins
 * only once it differs (`Aug 6 2025, 01:10`). Callers put the wire ISO on
 * `title` so hover always shows the original value. */
export function tableDateTime(iso: string, now = Date.now()): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return iso
  const d = new Date(t)
  const pad = (n: number) => String(n).padStart(2, "0")
  const day = `${MONTHS[d.getMonth()]} ${d.getDate()}`
  const year =
    d.getFullYear() === new Date(now).getFullYear() ? "" : ` ${d.getFullYear()}`
  return `${day}${year}, ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** `2026-08-04 13:04` — detail-context stamp, minutes are enough. */
export function shortDateTime(iso: string): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return iso
  const d = new Date(t)
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** `13:04` — time-of-day voice for the changelog's tight ranges; the date is
 * the surrounding context's job. Seconds join in when the caller orders
 * within a burst (`13:04:52`). */
export function shortTime(iso: string, withSeconds = false): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return iso
  const d = new Date(t)
  const pad = (n: number) => String(n).padStart(2, "0")
  const hm = `${pad(d.getHours())}:${pad(d.getMinutes())}`
  return withSeconds ? `${hm}:${pad(d.getSeconds())}` : hm
}

/** A folded group's span: `12:03–12:05`, collapsing to one stamp when the
 * burst fits inside a minute. */
export function timeRange(oldestISO: string, newestISO: string): string {
  const a = shortTime(oldestISO)
  const b = shortTime(newestISO)
  return a === b ? b : `${a}–${b}`
}

/** A stored reference — the referent's record PATH — read back as the id it
 * names, or "" when the value is not a path. A cell repeating the whole
 * `<kind>/<id>` says the column's own kind back at the reader, so the surfaces
 * that KNOW a property is a reference call this instead of `cellValue`: the
 * datatype is what tells a path from a string that merely has slashes in it. */
export function referenceID(value: unknown): string {
  // Either value shape: the flat path, or the `{ref, …}` object a reference
  // that declares link data stores.
  const held = readReference(value)
  return held ? (splitRecordPath(held.path)?.id ?? "") : ""
}

/** A reference PROPERTY's value in a cell: the id it names, and a repeated one
 * joined the way `cellValue` joins a list. */
export function referenceCell(value: unknown): string {
  if (Array.isArray(value)) {
    return value.map(referenceID).filter(Boolean).join(", ")
  }
  return referenceID(value)
}

/** One property value flattened into a cell: arrays join, objects summarize,
 * scalars pass through. The cell truncates; this only has to be honest. */
export function cellValue(value: unknown): string {
  if (value === null || value === undefined) return ""
  if (Array.isArray(value)) return value.map(cellValue).join(", ")
  if (typeof value === "object") {
    const keys = Object.keys(value as Record<string, unknown>)
    return keys.length ? `{${keys.join(", ")}}` : "{}"
  }
  return String(value)
}

/** The row's display name: the title property when a source set one, else
 * nothing — the caller falls back to the id in the data voice. */
export function recordTitle(properties: Record<string, unknown>): string {
  const title = properties.title
  return typeof title === "string" ? title : ""
}

/** Initials for an actor chip: `people.google.connectors.substrate.reamde.dev` → `PG`,
 * `owner` → `OW`. */
export function actorInitials(actor: string): string {
  const parts = actor.split(".").filter(Boolean)
  if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase()
  return actor.slice(0, 2).toUpperCase()
}

/** A connector actor's short voice: the first two labels
 * (`people.google.connectors.substrate.reamde.dev` → `people.google`); short names pass
 * through whole. */
export function actorShortName(actor: string): string {
  const parts = actor.split(".")
  return parts.length > 2 ? parts.slice(0, 2).join(".") : actor
}
