/** Dates the way a person says them: "Today, 10:00", "Tomorrow", "Fri",
 * "26 Sep", "26 Sep 2025". The stored value always rides the title. */

const DAY = 86_400_000

function startOfDay(t: number): number {
  const d = new Date(t)
  d.setHours(0, 0, 0, 0)
  return d.getTime()
}

/** The day, relative where that is shorter: Today, Tomorrow, Yesterday, a
 * weekday within the coming week, else the date (the year only once it
 * differs). */
export function friendlyDay(iso: string, now = Date.now()): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return iso
  const days = Math.round((startOfDay(t) - startOfDay(now)) / DAY)
  if (days === 0) return "Today"
  if (days === 1) return "Tomorrow"
  if (days === -1) return "Yesterday"
  const d = new Date(t)
  if (days > 1 && days < 7) {
    return d.toLocaleDateString("en-GB", { weekday: "short" })
  }
  const sameYear = d.getFullYear() === new Date(now).getFullYear()
  return d.toLocaleDateString("en-GB", {
    day: "numeric",
    month: "short",
    ...(sameYear ? {} : { year: "numeric" }),
  })
}

/** A bare `date` (`2026-10-03`) is a calendar day, not an instant: parsed as
 * UTC midnight it would read as the day before west of Greenwich. Anything
 * else is read as a stamp. */
export function friendlyCalendarDay(value: string, now = Date.now()): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value)
  if (!m) return friendlyDay(value, now)
  const local = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]))
  return friendlyDay(local.toISOString(), now)
}

/** The day and, when the value carries one that is not midnight, the time. */
export function friendlyDateTime(iso: string, now = Date.now()): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return iso
  const d = new Date(t)
  const day = friendlyDay(iso, now)
  if (d.getHours() === 0 && d.getMinutes() === 0) return day
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${day}, ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** "3 min ago", "2h ago", "yesterday", "4 days ago", then the date. */
export function ago(iso: string, now = Date.now()): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return iso
  const minutes = Math.round((now - t) / 60_000)
  if (minutes < 1) return "just now"
  if (minutes < 60) return `${minutes} min ago`
  const hours = Math.round(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.round(hours / 24)
  if (days === 1) return "yesterday"
  if (days < 14) return `${days} days ago`
  return friendlyDay(iso, now)
}

/** A stored instant as a `datetime-local` input's value, in local time. */
export function toLocalInput(iso: string): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return ""
  const d = new Date(t)
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** A `datetime-local` input's value back as an RFC 3339 instant, or "" when
 * it is blank or not a time. */
export function fromLocalInput(local: string): string {
  if (!local) return ""
  const t = new Date(local).getTime()
  return Number.isNaN(t) ? "" : new Date(t).toISOString().replace(".000Z", "Z")
}
