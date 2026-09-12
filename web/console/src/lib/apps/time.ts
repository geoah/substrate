/** Time as a list reads it: the six relative buckets a datetime groups into,
 * the phrase a cell shows ("in 3 days", "2 days ago"), and the ISO 8601
 * duration a timeline's `window` is written in. Calendar days are LOCAL: a
 * task due late tonight is still "today" here. */

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

function startOfDay(t: number): number {
  const d = new Date(t)
  d.setHours(0, 0, 0, 0)
  return d.getTime()
}

/** Which bucket an instant falls in. Anything already past is overdue, even
 * earlier today; "this week" is the six days after tomorrow. */
export function bucketOf(value: unknown, now = Date.now()): Bucket {
  if (typeof value !== "string" || !value) return "undated"
  const t = Date.parse(value)
  if (Number.isNaN(t)) return "undated"
  if (t < now) return "overdue"
  const today = startOfDay(now)
  const days = Math.floor((startOfDay(t) - today) / DAY)
  if (days <= 0) return "today"
  if (days === 1) return "tomorrow"
  if (days < 7) return "week"
  return "later"
}

const relative = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" })

/** "in 3 days" / "2 days ago" / "tomorrow" / "in 2 hours" / "just now": day
 * granularity across calendar days, hours and minutes inside today. */
export function relativeDay(iso: string, now = Date.now()): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return iso
  const days = Math.round((startOfDay(t) - startOfDay(now)) / DAY)
  if (days !== 0) return relative.format(days, "day")
  const delta = t - now
  const abs = Math.abs(delta)
  if (abs < MINUTE) return "just now"
  if (abs < HOUR) return relative.format(Math.round(delta / MINUTE), "minute")
  return relative.format(Math.round(delta / HOUR), "hour")
}

const DURATION =
  /^P(?:(\d+)Y)?(?:(\d+)M)?(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+(?:\.\d+)?)S)?)?$/

/** An ISO 8601 duration in milliseconds; months and years count as 30 and 365
 * days, a window being an approximate span around now. `undefined` when the
 * text is not a duration. */
export function parseDuration(text: string): number | undefined {
  const m = DURATION.exec(text.trim())
  if (!m || m[0] === "P" || m[0] === "PT") return undefined
  const n = (i: number) => (m[i] ? Number(m[i]) : 0)
  return (
    n(1) * 365 * DAY +
    n(2) * 30 * DAY +
    n(3) * 7 * DAY +
    n(4) * DAY +
    n(5) * HOUR +
    n(6) * MINUTE +
    n(7) * 1000
  )
}
