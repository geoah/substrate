/** Time and sections as the kit's lists read them, ported from proposal A's
 * `lib/apps/time.ts`, `lib/apps/group.ts` and the timeline's day fold. The
 * six relative buckets a datetime groups into, the phrase a row shows ("in 3
 * days", "2 days ago"), the local calendar day a timeline sections by, and
 * the grouping a `List` does over a property or a function. Calendar days
 * are LOCAL: a task due late tonight is still "today" here. Pure, so the
 * grouping is a unit test rather than a render. */

import type { SubstrateRecord } from "@/lib/api/types"

const MINUTE = 60_000
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

export type Bucket =
  "overdue" | "today" | "tomorrow" | "week" | "later" | "undated"

/** The buckets in the order a list sections them. */
export const BUCKETS: readonly { key: Bucket; label: string }[] = [
  { key: "overdue", label: "Overdue" },
  { key: "today", label: "Today" },
  { key: "tomorrow", label: "Tomorrow" },
  { key: "week", label: "This week" },
  { key: "later", label: "Later" },
  { key: "undated", label: "Undated" },
]

export function startOfDay(t: number): number {
  const d = new Date(t)
  d.setHours(0, 0, 0, 0)
  return d.getTime()
}

/** An ISO instant as epoch milliseconds, or nothing when the value is not
 * one. */
export function instantOf(value: unknown): number | undefined {
  if (typeof value !== "string" || !value) return undefined
  const t = Date.parse(value)
  return Number.isNaN(t) ? undefined : t
}

/** Which bucket an instant falls in. Anything already past is overdue, even
 * earlier today; "this week" is the six days after tomorrow. */
export function bucketOf(value: unknown, now = Date.now()): Bucket {
  const t = instantOf(value)
  if (t === undefined) return "undated"
  if (t < now) return "overdue"
  const days = Math.floor((startOfDay(t) - startOfDay(now)) / DAY)
  if (days <= 0) return "today"
  if (days === 1) return "tomorrow"
  if (days < 7) return "week"
  return "later"
}

const relative = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" })

/** "in 3 days" / "2 days ago" / "tomorrow" / "in 2 hours" / "just now": day
 * granularity across calendar days, hours and minutes inside today. */
export function relativeDay(iso: string, now = Date.now()): string {
  const t = instantOf(iso)
  if (t === undefined) return iso
  const days = Math.round((startOfDay(t) - startOfDay(now)) / DAY)
  if (days !== 0) return relative.format(days, "day")
  const delta = t - now
  const abs = Math.abs(delta)
  if (abs < MINUTE) return "just now"
  if (abs < HOUR) return relative.format(Math.round(delta / MINUTE), "minute")
  return relative.format(Math.round(delta / HOUR), "hour")
}

const pad = (n: number) => String(n).padStart(2, "0")

/** The local calendar day an instant falls on, as `YYYY-MM-DD`, which sorts
 * as it reads. */
export function dayKey(t: number): string {
  const d = new Date(t)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

/** The heading a day gets: Today, Tomorrow and Yesterday by name, every
 * other day its weekday and date, the year joining only once it differs. */
export function dayLabel(key: string, now = Date.now()): string {
  const [y, m, d] = key.split("-").map(Number)
  const day = new Date(y, m - 1, d).getTime()
  const days = Math.round((day - startOfDay(now)) / DAY)
  if (days === 0) return "Today"
  if (days === 1) return "Tomorrow"
  if (days === -1) return "Yesterday"
  const sameYear = y === new Date(now).getFullYear()
  return new Intl.DateTimeFormat(undefined, {
    weekday: "long",
    month: "short",
    day: "numeric",
    ...(sameYear ? {} : { year: "numeric" }),
  }).format(day)
}

/** `13:04`: the time of day, the date being the day header's job. */
export function shortTime(t: number): string {
  const d = new Date(t)
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** `09:00–09:30` for a range, `09:00` for a point; a range that ends on
 * another day names that day instead of its time. */
export function timeSpan(at: number, endsAt?: number): string {
  const start = shortTime(at)
  if (endsAt === undefined) return start
  const end = dayKey(at) === dayKey(endsAt) ? shortTime(endsAt) : dayKey(endsAt)
  return `${start}–${end}`
}

export interface TimelineRow<T extends SubstrateRecord = SubstrateRecord> {
  record: T
  /** The instant of the record's bound point, in epoch milliseconds. */
  at: number
  /** The end of a range, when the record carries `endsAt`. */
  endsAt?: number
}

/** A record's instant: `at` for a kind bound on the shared point or a range,
 * else `dueAt`, the fan-out having read each kind on its own point. A record
 * with neither is dropped: a timeline has no "Undated". */
export function timelineRows<T extends SubstrateRecord>(
  records: readonly T[]
): TimelineRow<T>[] {
  const rows: TimelineRow<T>[] = []
  for (const record of records) {
    const at = instantOf(record.properties.at ?? record.properties.dueAt)
    if (at === undefined) continue
    rows.push({ record, at, endsAt: instantOf(record.properties.endsAt) })
  }
  return rows.sort(
    (a, b) => a.at - b.at || titleOf(a.record).localeCompare(titleOf(b.record))
  )
}

export interface TimelineDay<T extends SubstrateRecord = SubstrateRecord> {
  key: string
  rows: TimelineRow<T>[]
}

/** Rows in instant order folded into their local days, empty days absent by
 * construction. */
export function groupByDay<T extends SubstrateRecord>(
  rows: readonly TimelineRow<T>[]
): TimelineDay<T>[] {
  const days: TimelineDay<T>[] = []
  for (const row of rows) {
    const key = dayKey(row.at)
    const last = days[days.length - 1]
    if (last && last.key === key) last.rows.push(row)
    else days.push({ key, rows: [row] })
  }
  return days
}

/** A record's heading: the server's title, else `name`, else `summary`,
 * else its id. */
export function titleOf(record: SubstrateRecord): string {
  for (const key of ["title", "name", "summary"]) {
    const value = record.properties[key]
    if (typeof value === "string" && value.trim()) return value.trim()
  }
  return record.id
}

/** Up to two initials: the first letter of the first two words. */
export function initialsOf(name: string): string {
  return name
    .trim()
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((w) => w.charAt(0).toUpperCase())
    .join("")
}

/** The section for every key outside A–Z: a digit, a symbol, another script,
 * or no title at all. Last in an index, like an address book. */
export const OTHER_SECTION = "#"

export const INDEX_SECTIONS: readonly string[] = [
  ..."ABCDEFGHIJKLMNOPQRSTUVWXYZ",
  OTHER_SECTION,
]

/** The index section a heading files under: its first letter with any
 * accent stripped, upper-cased, when that is A–Z; otherwise "#". */
export function sectionOf(title: string): string {
  const first = title
    .trim()
    .normalize("NFD")
    .replace(/[\u0300-\u036f]/g, "")
    .charAt(0)
    .toUpperCase()
  return /^[A-Z]$/.test(first) ? first : OTHER_SECTION
}

export interface RecordGroup<T extends SubstrateRecord = SubstrateRecord> {
  key: string
  label: string
  records: T[]
}

/** What a `List` needs of a declared property to section by it: the
 * datatype, and for a state or an enum the declared order and labels. An
 * SDK `PropSpec` satisfies it; the kit never imports the SDK's own. */
export interface PropertyHint {
  kind: string
  states?: readonly string[]
  values?: readonly { value: string; label?: string }[]
}

/** The key rows with no value group under; sorts last. */
const NONE = " none"

const ISO_DAY = /^\d{4}-\d{2}-\d{2}(?:[T\s]|$)/

function looksLikeInstant(value: unknown): boolean {
  return (
    typeof value === "string" &&
    ISO_DAY.test(value) &&
    instantOf(value) !== undefined
  )
}

/** Whether a property sections into the six buckets: the declaration says
 * `datetime`, or, with no declaration to ask, every present value reads as
 * an ISO instant (or none is present and the name reads as a moment). */
function isTemporal(
  records: readonly SubstrateRecord[],
  property: string,
  hint?: PropertyHint
): boolean {
  if (hint) return hint.kind === "datetime"
  let present = 0
  for (const record of records) {
    const value = record.properties[property]
    if (value === undefined || value === null || value === "") continue
    if (!looksLikeInstant(value)) return false
    present++
  }
  return present > 0 || /(?:At|Date|On)$/.test(property)
}

function isIndexKey(key: string): boolean {
  return key.length === 1
}

/** Index order: A–Z by locale, then everything else, "#" last. */
function compareIndexKeys(a: string, b: string): number {
  const la = /^[A-Z]$/i.test(a)
  const lb = /^[A-Z]$/i.test(b)
  if (la !== lb) return la ? -1 : 1
  if (a === OTHER_SECTION) return 1
  if (b === OTHER_SECTION) return -1
  return a.localeCompare(b)
}

/** Rows sectioned by `by`. A property name that is a datetime buckets
 * Overdue to Undated; a state or an enum groups by value in declared order
 * with the declaration's labels, the valueless last under "None"; a
 * function groups by what it returns, in the order the rows arrive, except
 * that single-character keys (an A–Z index) sort as an index. */
export function groupRecords<T extends SubstrateRecord>(
  records: readonly T[],
  by: string | ((record: T) => string),
  hint?: PropertyHint,
  now = Date.now()
): RecordGroup<T>[] {
  const groups = new Map<string, RecordGroup<T>>()
  const push = (key: string, label: string, record: T) => {
    const group = groups.get(key) ?? { key, label, records: [] }
    group.records.push(record)
    groups.set(key, group)
  }

  if (typeof by === "function") {
    for (const record of records) {
      const key = String(by(record) ?? "") || OTHER_SECTION
      push(key, key, record)
    }
    const out = [...groups.values()]
    if (out.every((g) => isIndexKey(g.key))) {
      out.sort((a, b) => compareIndexKeys(a.key, b.key))
    }
    return out
  }

  if (isTemporal(records, by, hint)) {
    for (const record of records) {
      const bucket = bucketOf(record.properties[by], now)
      const label = BUCKETS.find((b) => b.key === bucket)?.label ?? bucket
      push(bucket, label, record)
    }
    return BUCKETS.map((b) => groups.get(b.key)).filter(
      (g): g is RecordGroup<T> => Boolean(g)
    )
  }

  const declaredOrder =
    hint?.kind === "state"
      ? [...(hint.states ?? [])]
      : (hint?.values?.map((v) => v.value) ?? [])
  const labelFor = (raw: string) =>
    hint?.values?.find((v) => v.value === raw)?.label || raw

  for (const record of records) {
    const value = record.properties[by]
    const first = Array.isArray(value) ? value[0] : value
    if (first === undefined || first === null || first === "") {
      push(NONE, "None", record)
      continue
    }
    const raw =
      typeof first === "object" && first !== null && "ref" in first
        ? String((first as { ref: unknown }).ref)
        : String(first)
    push(raw, labelFor(raw), record)
  }

  return [...groups.values()].sort((a, b) => {
    if (a.key === NONE) return 1
    if (b.key === NONE) return -1
    if (!declaredOrder.length) return 0
    const ia = declaredOrder.indexOf(a.key)
    const ib = declaredOrder.indexOf(b.key)
    return (ia < 0 ? 1e9 : ia) - (ib < 0 ? 1e9 : ib)
  })
}
