/** The calendar popover's arithmetic, without React: a month as the weeks a
 * grid draws (Monday first, always six rows so the popover never changes
 * height), moving a day by the keys a grid answers to, and a typed time read
 * the loose ways people type one. Days are local calendar days held as
 * `YYYY-MM-DD`, the same spelling a `date` property stores. */

/** A local calendar day. */
export interface Day {
  year: number
  /** 0-11, as Date counts them. */
  month: number
  day: number
}

const pad = (n: number) => String(n).padStart(2, "0")

export function dayKey(d: Day): string {
  return `${d.year}-${pad(d.month + 1)}-${pad(d.day)}`
}

export function parseDay(key: string): Day | undefined {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(key)
  if (!m) return undefined
  return { year: Number(m[1]), month: Number(m[2]) - 1, day: Number(m[3]) }
}

export function dayOf(date: Date): Day {
  return {
    year: date.getFullYear(),
    month: date.getMonth(),
    day: date.getDate(),
  }
}

/** A day moved by `days`, across months and years. */
export function addDays(d: Day, days: number): Day {
  return dayOf(new Date(d.year, d.month, d.day + days))
}

/** The same day `months` later, clamped to the month's last day (31 Jan plus
 * one month is 28 or 29 Feb, never 3 Mar). */
export function addMonths(d: Day, months: number): Day {
  const first = new Date(d.year, d.month + months, 1)
  const last = new Date(first.getFullYear(), first.getMonth() + 1, 0).getDate()
  return {
    year: first.getFullYear(),
    month: first.getMonth(),
    day: Math.min(d.day, last),
  }
}

/** Monday = 0 … Sunday = 6. */
function weekday(d: Day): number {
  return (new Date(d.year, d.month, d.day).getDay() + 6) % 7
}

/** The six weeks a grid shows for a month: from the Monday on or before the
 * 1st, 42 days, the neighbours' days included. */
export function monthGrid(year: number, month: number): Day[][] {
  const first: Day = { year, month, day: 1 }
  const start = addDays(first, -weekday(first))
  const weeks: Day[][] = []
  for (let w = 0; w < 6; w++) {
    const week: Day[] = []
    for (let i = 0; i < 7; i++) week.push(addDays(start, w * 7 + i))
    weeks.push(week)
  }
  return weeks
}

/** Where a key moves the grid's focused day; undefined for a key it does not
 * answer. */
export function moveDay(d: Day, key: string, shift = false): Day | undefined {
  switch (key) {
    case "ArrowLeft":
      return addDays(d, -1)
    case "ArrowRight":
      return addDays(d, 1)
    case "ArrowUp":
      return addDays(d, -7)
    case "ArrowDown":
      return addDays(d, 7)
    case "Home":
      return addDays(d, -weekday(d))
    case "End":
      return addDays(d, 6 - weekday(d))
    case "PageUp":
      return addMonths(d, shift ? -12 : -1)
    case "PageDown":
      return addMonths(d, shift ? 12 : 1)
    default:
      return undefined
  }
}

/** A typed time as `HH:MM`: "9", "9:5", "0930", "9.30", "9pm", "12am".
 * Empty is midnight's absence, "", and anything else is undefined. */
export function parseTime(text: string): string | undefined {
  const t = text.trim().toLowerCase()
  if (!t) return ""
  const m = /^(\d{1,2})(?:[:.h]?(\d{2}))?\s*(am|pm)?$/.exec(t)
  if (!m) return undefined
  let hours = Number(m[1])
  const minutes = m[2] ? Number(m[2]) : 0
  if (m[3]) {
    if (hours < 1 || hours > 12) return undefined
    hours = (hours % 12) + (m[3] === "pm" ? 12 : 0)
  }
  if (hours > 23 || minutes > 59) return undefined
  return `${pad(hours)}:${pad(minutes)}`
}

/** "October 2026". */
export function monthTitle(year: number, month: number): string {
  return new Date(year, month, 1).toLocaleDateString("en-GB", {
    month: "long",
    year: "numeric",
  })
}

/** "Saturday 3 October 2026": a day button's accessible name. */
export function dayName(d: Day): string {
  return new Date(d.year, d.month, d.day).toLocaleDateString("en-GB", {
    weekday: "long",
    day: "numeric",
    month: "long",
    year: "numeric",
  })
}

/** Whether a device's pointer is a finger, where the native date control is
 * the better keyboard. */
export function prefersNativeDate(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia("(pointer: coarse)").matches
  )
}
