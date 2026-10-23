/** Time on the observations pages, without React: the ranges a timeline
 * reads (presets, custom days), instants and windows in words, the axis's
 * ticks, the bucket sizes and the aggregate's time mode for each, the
 * recorded-local calendar's days, and ISO 8601 durations. Local time is the
 * browser's; a calendar bucket names the reader's zone, or reads a day, week
 * or month where each value was recorded. */

import type { AggregateBucket } from "@/lib/api/types"
import { MAX_BUCKETS_CAP } from "@/lib/aggregate-rules"

// ── ranges ─────────────────────────────────────────────────────────────────

export type RangePreset = "today" | "7d" | "30d" | "6m" | "1y"

export const RANGE_PRESETS: readonly { value: RangePreset; label: string }[] = [
  { value: "today", label: "Today" },
  { value: "7d", label: "Last 7 days" },
  { value: "30d", label: "Last 30 days" },
  { value: "6m", label: "Last 6 months" },
  { value: "1y", label: "Last year" },
]

export function isRangePreset(value: unknown): value is RangePreset {
  return RANGE_PRESETS.some((p) => p.value === value)
}

/** A range the picker offers: a preset, or `all`, from the earliest point
 * the chosen streams hold to today. */
export type RangeKey = RangePreset | "all"

export const ALL_RANGE: { value: "all"; label: string } = {
  value: "all",
  label: "All",
}

/** Every day from the one holding `earliest` through today. */
export function allRange(earliest: number, now = Date.now()): TimeRange {
  const first = new Date(Math.min(earliest, now))
  return {
    start: new Date(
      first.getFullYear(),
      first.getMonth(),
      first.getDate()
    ).getTime(),
    end: daysEnding(now, 1).end,
  }
}

/** The shortest preset whose window reaches back to `t`, a stream's newest
 * value; undefined where even the last year does not. */
export function presetReaching(
  t: number,
  now = Date.now()
): RangePreset | undefined {
  return RANGE_PRESETS.map((p) => p.value).find(
    (p) => presetRange(p, now).start <= t
  )
}

/** A half-open window `[start, end)` in epoch milliseconds. */
export interface TimeRange {
  start: number
  end: number
}

/** The `days` local days ending with the one holding `t`: from the first
 * one's start to the start of the day after `t`'s. */
export function daysEnding(t: number, days: number): TimeRange {
  const d = new Date(t)
  const y = d.getFullYear()
  const m = d.getMonth()
  const day = d.getDate()
  return {
    start: new Date(y, m, day - days + 1).getTime(),
    end: new Date(y, m, day + 1).getTime(),
  }
}

/** A preset as the local days it covers, ending with today:
 * `[the first day's midnight, tomorrow's midnight)`. */
export function presetRange(preset: RangePreset, now = Date.now()): TimeRange {
  if (preset === "today") return daysEnding(now, 1)
  if (preset === "7d") return daysEnding(now, 7)
  if (preset === "30d") return daysEnding(now, 30)
  const today = new Date(now)
  const y = today.getFullYear()
  const m = today.getMonth()
  const d = today.getDate()
  const start =
    preset === "6m" ? new Date(y, m - 6, d + 1) : new Date(y - 1, m, d + 1)
  return { start: start.getTime(), end: daysEnding(now, 1).end }
}

/** Two local calendar days (`YYYY-MM-DD`) as the window from the first's
 * midnight to the midnight after the second. Undefined for a day that does
 * not parse or an end before the start. */
export function dayRange(first: string, last: string): TimeRange | undefined {
  const a = /^(\d{4})-(\d{2})-(\d{2})$/.exec(first)
  const b = /^(\d{4})-(\d{2})-(\d{2})$/.exec(last)
  if (!a || !b) return undefined
  const start = new Date(+a[1], +a[2] - 1, +a[3]).getTime()
  const end = new Date(+b[1], +b[2] - 1, +b[3] + 1).getTime()
  return end > start ? { start, end } : undefined
}

const pad = (n: number) => String(n).padStart(2, "0")

/** An instant as local wall time with its offset, the spelling the
 * aggregate's `range` reads: `2026-09-01T00:00:00+03:00`. Milliseconds are
 * kept where the instant has them (`00:00:00.123+03:00`): a record's window
 * shorter than a second would otherwise read as an empty range. */
export function isoWithOffset(t: number): string {
  const d = new Date(t)
  const off = -d.getTimezoneOffset()
  const sign = off >= 0 ? "+" : "-"
  const abs = Math.abs(off)
  const ms = d.getMilliseconds()
  return (
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}` +
    `T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}` +
    (ms ? `.${String(ms).padStart(3, "0")}` : "") +
    `${sign}${pad(Math.floor(abs / 60))}:${pad(abs % 60)}`
  )
}

/** A local day key, `YYYY-MM-DD`. */
export function dayKeyOf(t: number): string {
  const d = new Date(t)
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

function isMidnight(t: number): boolean {
  const d = new Date(t)
  return d.getHours() === 0 && d.getMinutes() === 0 && d.getSeconds() === 0
}

/** A local day in words: `Sep 3`, or `Sep 3, 2026` with its year. */
export function dayWords(t: number, withYear = false): string {
  const d = new Date(t)
  return `${MONTHS[d.getMonth()]} ${d.getDate()}${withYear ? `, ${d.getFullYear()}` : ""}`
}

/** A local clock time, `06:30`. */
export function clockWords(t: number): string {
  const d = new Date(t)
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** A window in words: whole days as `Sep 1 to Sep 30, 2026` (the end is
 * the last day it holds), anything finer as its two instants,
 * `Oct 7, 06:00 to 09:00`. */
export function rangeWords(range: TimeRange): string {
  if (isMidnight(range.start) && isMidnight(range.end)) {
    const last = new Date(range.end)
    last.setDate(last.getDate() - 1)
    const lastAt = last.getTime()
    if (dayKeyOf(range.start) === dayKeyOf(lastAt)) {
      return dayWords(range.start, true)
    }
    const sameYear = new Date(range.start).getFullYear() === last.getFullYear()
    return `${dayWords(range.start, !sameYear)} to ${dayWords(lastAt, true)}`
  }
  const sameDay = dayKeyOf(range.start) === dayKeyOf(range.end - 1)
  const from = `${dayWords(range.start, true)}, ${clockWords(range.start)}`
  const to = sameDay
    ? clockWords(range.end)
    : `${dayWords(range.end, true)}, ${clockWords(range.end)}`
  return `${from} to ${to}`
}

/** A bucket's start as an axis or table label, as fine as its size:
 * `06:30` (with the day where it changes), `Sep 3`, `Sep 2026`. */
export function bucketWords(
  t: number,
  size: BucketSize,
  previous?: number
): string {
  const d = new Date(t)
  if (size === "month") {
    return d.getMonth() === 0 || previous === undefined
      ? `${MONTHS[d.getMonth()]} ${d.getFullYear()}`
      : MONTHS[d.getMonth()]
  }
  if (size === "day" || size === "week") return dayWords(t)
  const clock = clockWords(t)
  if (previous !== undefined && dayKeyOf(previous) === dayKeyOf(t)) return clock
  return clock === "00:00" ? dayWords(t) : `${dayWords(t)}, ${clock}`
}

/** Each size finer than a day, in minutes: these step by elapsed time. */
const STEP_MINUTES: Partial<Record<BucketSize, number>> = {
  "1m": 1,
  "5m": 5,
  "15m": 15,
  hour: 60,
}

/** The size's boundaries inside a range, local time: every minute, five
 * minutes, quarter hour, hour, midnight, Monday or first of the month from
 * the first one at or after the range's start. The x axis labels these,
 * whether or not a bucket holds anything there. At most `limit` of them:
 * the default is twice the buckets any read may make (`MAX_BUCKETS_CAP`),
 * so a range a chart can draw is never cut short.
 *
 * Sizes finer than a day step by elapsed time, so a day the clocks go back
 * ticks its repeated hour twice. Days, weeks and months are each worked out
 * from the calendar date, so a day whose midnight the clocks skip starts at
 * its first instant and the next day at midnight again. */
export function axisTicks(
  range: TimeRange,
  size: BucketSize,
  limit = 2 * MAX_BUCKETS_CAP
): number[] {
  const out: number[] = []
  const step = STEP_MINUTES[size]
  if (step) {
    const d = new Date(range.start)
    d.setSeconds(0, 0)
    const minutes = Math.ceil(d.getMinutes() / step) * step
    let t = d.getTime() + (minutes - d.getMinutes()) * 60_000
    for (; t < range.end && out.length < limit; t += step * 60_000) {
      if (t >= range.start) out.push(t)
    }
    return out
  }
  const s = new Date(range.start)
  let y = s.getFullYear()
  let m = s.getMonth()
  let day = s.getDate()
  if (new Date(y, m, day).getTime() < range.start) day += 1
  if (size === "week") day += (8 - new Date(y, m, day).getDay()) % 7
  if (size === "month") {
    const first = new Date(y, m, day)
    y = first.getFullYear()
    m = first.getMonth() + (first.getDate() > 1 ? 1 : 0)
    day = 1
  }
  for (let k = 0; out.length < limit; k++) {
    const t = (
      size === "day"
        ? new Date(y, m, day + k)
        : size === "week"
          ? new Date(y, m, day + 7 * k)
          : new Date(y, m + k, 1)
    ).getTime()
    if (t >= range.end) break
    out.push(t)
  }
  return out
}

/** The round clock steps, in minutes, a finer-than-a-day axis labels at. */
const LABEL_MINUTES = [1, 2, 5, 10, 15, 30, 60, 120, 180, 240, 360, 720, 1440]

/** Which of an axis's ticks (`axisTicks`) carry a label, where a label has
 * room for one in every `every` ticks: under a day, the ticks on the
 * smallest round clock step that leaves that room (every 6 hours on 00, 06,
 * 12 and 18, never every 5 from the first), and where that room passes a
 * day, every few midnights from the first; for days, weeks and months,
 * every `every`-th from the first. */
export function labelledTicks(
  marks: readonly number[],
  size: BucketSize,
  every: number
): number[] {
  const minutes = STEP_MINUTES[size]
  if (!minutes) return marks.flatMap((_, i) => (i % every ? [] : [i]))
  const room = minutes * every
  const step = LABEL_MINUTES.find((m) => m >= room) ?? 1440
  const days = Math.ceil(room / 1440)
  let midnights = 0
  return marks.flatMap((t, i) => {
    const d = new Date(t)
    const clock = d.getHours() * 60 + d.getMinutes()
    if (clock % step) return []
    if (step < 1440) return [i]
    return midnights++ % days ? [] : [i]
  })
}

/** A bucket's window in words, for a tooltip or a table row: a day as
 * `Sep 3, 2026`, a week as `Sep 1 to Sep 7, 2026`, finer windows by their
 * clocks. */
export function windowWords(start: number, end: number): string {
  return rangeWords({ start, end })
}

/** A point's window, short, for a table cell: whole days as their days
 * (`Oct 11`, `Oct 6 to Oct 7`), a window inside one day as its clocks
 * (`Oct 7, 09:45 to 10:00`), anything else as its two instants. The year is
 * left to the cell's hover. */
export function spanWords(start: number, end: number): string {
  if (isMidnight(start) && isMidnight(end)) {
    const last = new Date(end)
    last.setDate(last.getDate() - 1)
    return dayKeyOf(start) === dayKeyOf(last.getTime())
      ? dayWords(start)
      : `${dayWords(start)} to ${dayWords(last.getTime())}`
  }
  const from = `${dayWords(start)}, ${clockWords(start)}`
  return dayKeyOf(start) === dayKeyOf(end - 1)
    ? `${from} to ${clockWords(end)}`
    : `${from} to ${dayWords(end)}, ${clockWords(end)}`
}

/** An instant to the second, for a value inside a trace: `Oct 7, 06:30:01`. */
export function secondWords(t: number): string {
  const d = new Date(t)
  return `${dayWords(t)}, ${clockWords(t)}:${pad(d.getSeconds())}`
}

// ── buckets ────────────────────────────────────────────────────────────────

export type BucketSize = "1m" | "5m" | "15m" | "hour" | "day" | "week" | "month"
export type BucketChoice = "auto" | BucketSize

export const BUCKET_CHOICES: readonly { value: BucketChoice; label: string }[] =
  [
    { value: "auto", label: "Automatic" },
    { value: "1m", label: "1 min" },
    { value: "5m", label: "5 min" },
    { value: "15m", label: "15 min" },
    { value: "hour", label: "Hour" },
    { value: "day", label: "Day" },
    { value: "week", label: "Week" },
    { value: "month", label: "Month" },
  ]

/** Each size in words, for the automatic choice's label: "Automatic: day". */
export const BUCKET_SIZE_WORDS: Record<BucketSize, string> = {
  "1m": "1 min",
  "5m": "5 min",
  "15m": "15 min",
  hour: "hour",
  day: "day",
  week: "week",
  month: "month",
}

const HOUR_MS = 3_600_000
const DAY_MS = 24 * HOUR_MS

/** Each size's length, a month counted as 30 days: for estimates only. */
export const BUCKET_MS: Record<BucketSize, number> = {
  "1m": 60_000,
  "5m": 5 * 60_000,
  "15m": 15 * 60_000,
  hour: HOUR_MS,
  day: DAY_MS,
  week: 7 * DAY_MS,
  month: 30 * DAY_MS,
}

/** The size a range reads at when nobody picked one: under 200 buckets
 * for every preset (96 for a day, 168 for a week, 30 for a month, 27 and 53
 * weeks for six months and a year), and minutes for a window of a few
 * hours, such as a workout's trace (at most 120 of one minute, 96 of
 * five). */
export function autoBucket(range: TimeRange): BucketSize {
  const span = range.end - range.start
  if (span <= 2 * HOUR_MS) return "1m"
  if (span <= 8 * HOUR_MS) return "5m"
  if (span <= DAY_MS + HOUR_MS) return "15m"
  if (span <= 8 * DAY_MS) return "hour"
  if (span <= 62 * DAY_MS) return "day"
  if (span <= 400 * DAY_MS) return "week"
  return "month"
}

export function bucketSizeOf(
  choice: BucketChoice,
  range: TimeRange
): BucketSize {
  return choice === "auto" ? autoBucket(range) : choice
}

/** The size a chart drawing a metric placed by its end (`bucketPlace`)
 * reads at when nobody picked one: a day where the range's own size would
 * be an hour. Such a metric's spans run long (a night's time in each stage,
 * written as one span over the whole night), and an amount counts whole in
 * one bucket, so an hour would hold the whole night; a day, placed by its
 * end, holds one. */
export function endPlacedBucket(
  choice: BucketChoice,
  size: BucketSize
): BucketSize {
  return choice === "auto" && size === "hour" ? "day" : size
}

/** About how many buckets a read makes: the range's buckets times its
 * groups, which is what the request's budget counts (`maxBuckets`). */
export function bucketEstimate(
  range: TimeRange,
  size: BucketSize,
  groups = 1
): number {
  return Math.ceil((range.end - range.start) / BUCKET_MS[size]) * groups
}

/** Whether a size is a calendar window of a day or more: where its days
 * start is the reader's choice (`DayBoundary`). Finer sizes are instants. */
export function dayBucket(size: BucketSize): boolean {
  return size === "day" || size === "week" || size === "month"
}

/** Where a day, a week or a month starts: `local` is the recorded-local
 * calendar, each value on the date its own UTC offset names, so a day spent
 * in London is London's day wherever the reader is; `zone` is the reader's
 * time zone. */
export type DayBoundary = "local" | "zone"

export const DAY_BOUNDARIES: readonly DayBoundary[] = ["local", "zone"]

/** A size as the aggregate's time mode: a size in minutes is a physical
 * window on UTC instants, the rest calendar windows in the zone given,
 * weeks from Monday. A day, week or month read `local` is the recorded-local
 * calendar instead (no `timezone`): the zone given places only a value
 * written without a UTC offset (`fallbackTimezone`), which the server
 * otherwise refuses. */
export function bucketOf(
  size: BucketSize,
  timezone?: string,
  local = false
): AggregateBucket {
  const zone =
    local && dayBucket(size)
      ? timezone
        ? { fallbackTimezone: timezone }
        : {}
      : timezone
        ? { timezone }
        : {}
  switch (size) {
    case "1m":
      return { every: "PT1M" }
    case "5m":
      return { every: "PT5M" }
    case "15m":
      return { every: "PT15M" }
    case "hour":
      return { calendar: "hour", ...zone }
    case "day":
      return { calendar: "day", ...zone }
    case "week":
      return { calendar: "week", weekStart: "monday", ...zone }
    case "month":
      return { calendar: "month", ...zone }
  }
}

// ── recorded-local days ────────────────────────────────────────────────────

/** Whether a recorded-local read of `range` asks for whole dates
 * (`range.recordedLocal`): a day bucket over a range from one local
 * midnight to another. The server then answers every date in it, at
 * whatever offset each was lived, and none outside. Weeks and months keep
 * the range's own instants, since a partial one would take hours of a date
 * the range leaves out. */
export function wholeLocalDates(
  range: TimeRange,
  size: BucketSize,
  local: boolean
): boolean {
  return (
    local && size === "day" && isMidnight(range.start) && isMidnight(range.end)
  )
}

/** The calendar date an instant's text names, before its offset is applied:
 * `2026-09-20T23:30:00+01:00` is 2026-09-20 whatever the reader's zone. */
function wallDate(iso: string): [number, number, number] | undefined {
  const m = /^(\d{4})-(\d{2})-(\d{2})T/.exec(iso)
  return m ? [+m[1], +m[2], +m[3]] : undefined
}

function dateKey(y: number, m: number, d: number): string {
  return `${y}-${pad(m)}-${pad(d)}`
}

/** The recorded-local bucket a row's own `start` names, by its calendar
 * date and never by its instant: the date for a day, that week's Monday for
 * a week, the first for a month (`YYYY-MM-DD`). Undefined where the text
 * names no date. */
export function localKey(start: string, size: BucketSize): string | undefined {
  const date = wallDate(start)
  if (!date) return undefined
  const [y, m, d] = date
  if (size === "month") return dateKey(y, m, 1)
  if (size !== "week") return dateKey(y, m, d)
  const t = Date.UTC(y, m - 1, d)
  const back = (new Date(t).getUTCDay() + 6) % 7
  const monday = new Date(t - back * DAY_MS)
  return dateKey(
    monday.getUTCFullYear(),
    monday.getUTCMonth() + 1,
    monday.getUTCDate()
  )
}

/** Where a recorded-local bucket sits on the reader's axis: its date's
 * midnight to the next bucket's, in the reader's zone, so every date is one
 * slot and the axis's day ticks fall between them. */
export function localSlot(key: string, size: BucketSize): TimeRange {
  const [y, m, d] = key.split("-").map(Number)
  const end =
    size === "month"
      ? new Date(y, m, 1)
      : new Date(y, m - 1, d + (size === "week" ? 7 : 1))
  return { start: new Date(y, m - 1, d).getTime(), end: end.getTime() }
}

/** A recorded-local row's days in words, from the dates its own `start` and
 * `end` name: `Sep 20, 2026`, or `Sep 14 to Sep 20, 2026` for a week. An end
 * at midnight ends the day before. */
export function localWindowWords(start: string, end: string): string {
  const a = wallDate(start)
  const b = wallDate(end)
  if (!a || !b) return windowWords(Date.parse(start), Date.parse(end))
  const first = new Date(a[0], a[1] - 1, a[2])
  const midnight = /T00:00(?::00(?:\.0+)?)?(?:Z|[+-])/.test(end)
  const last = new Date(b[0], b[1] - 1, b[2] - (midnight ? 1 : 0))
  const lastAt = Math.max(last.getTime(), first.getTime())
  const after = new Date(lastAt)
  after.setDate(after.getDate() + 1)
  return rangeWords({ start: first.getTime(), end: after.getTime() })
}

/** The UTC offset an instant's text carries: `+01:00`, `+00:00` for `Z`. */
export function offsetOf(iso: string): string | undefined {
  const m = /(Z|[+-]\d{2}:\d{2})$/.exec(iso)
  if (!m) return undefined
  return m[1] === "Z" || m[1] === "-00:00" ? "+00:00" : m[1]
}

/** Whether every row was lived at the reader's own UTC offsets, at its
 * start and at its end, so the recorded-local calendar and the reader's
 * zone put it on one date. */
export function atReaderOffsets(
  rows: readonly { start: string; end: string }[]
): boolean {
  return rows.every((r) =>
    [r.start, r.end].every(
      (t) => offsetOf(t) === offsetOf(isoWithOffset(Date.parse(t)))
    )
  )
}

/** A row's UTC offsets in words: `+01:00`, or `+03:00 to +01:00` across a
 * change (a flight on the day). */
export function offsetWords(start: string, end: string): string {
  const a = offsetOf(start)
  const b = offsetOf(end)
  if (!a || !b) return a ?? b ?? ""
  return a === b ? a : `${a} to ${b}`
}

/** The reader's time zone, for calendar buckets: the runtime's, else UTC.
 * Never none, since a calendar bucket without a zone is the recorded-local
 * calendar while the range stays in the browser's offset. */
export function readerTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC"
  } catch {
    return "UTC"
  }
}

// ── durations ──────────────────────────────────────────────────────────────

const DURATION =
  /^P(?:(\d+(?:\.\d+)?)W)?(?:(\d+(?:\.\d+)?)D)?(?:T(?:(\d+(?:\.\d+)?)H)?(?:(\d+(?:\.\d+)?)M)?(?:(\d+(?:\.\d+)?)S)?)?$/

/** An ISO 8601 duration of weeks, days, hours, minutes and seconds in
 * milliseconds; undefined for anything else (years and months have no fixed
 * length). */
export function durationMs(text: string | undefined): number | undefined {
  if (!text) return undefined
  const m = DURATION.exec(text)
  if (!m || text === "P" || text.endsWith("T")) return undefined
  const [w, d, h, min, s] = m.slice(1).map((x) => (x ? Number(x) : 0))
  return Math.round((((w * 7 + d) * 24 + h) * 60 + min) * 60_000 + s * 1000)
}

/** A length of time in a few words: `6d 22h 10m`, `59m 58s`, `0s`. */
export function durationWords(ms: number): string {
  const parts: string[] = []
  let rest = Math.round(ms / 1000)
  const days = Math.floor(rest / 86_400)
  rest -= days * 86_400
  const hours = Math.floor(rest / 3600)
  rest -= hours * 3600
  const minutes = Math.floor(rest / 60)
  const seconds = rest - minutes * 60
  if (days) parts.push(`${days}d`)
  if (hours) parts.push(`${hours}h`)
  if (minutes) parts.push(`${minutes}m`)
  if (seconds && !days) parts.push(`${seconds}s`)
  return parts.length ? parts.slice(0, 3).join(" ") : "0s"
}

/** A stored duration in words, or the stored text where it does not parse. */
export function coverageWords(text: string | undefined): string {
  const ms = durationMs(text)
  return ms === undefined ? (text ?? "") : durationWords(ms)
}

/** The time a bucket's values cover, in words: to the minute from an hour
 * up (`22h 25m`, never `22h 25m 17s`), to the second under it; the stored
 * text where it does not parse. */
export function coveredWords(text: string | undefined): string {
  const ms = durationMs(text)
  if (ms === undefined) return text ?? ""
  return durationWords(ms >= 3_600_000 ? Math.round(ms / 60_000) * 60_000 : ms)
}
