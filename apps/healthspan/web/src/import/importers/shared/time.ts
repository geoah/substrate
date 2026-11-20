// A provider's times: a wall-clock time in a named zone made an instant with
// the zone's offset at that moment, ISO 8601 text, dates, and whole seconds
// since the epoch for a provider's revision
// (apps/healthspan/docs/importers/00-shared.md, Time).
//
// An ambiguous wall-clock time (the hour a zone repeats in autumn) takes the
// earlier offset, and one in the hour a zone skips in spring takes the offset
// from before the change, as Python's zoneinfo does at fold 0; Temporal's
// `compatible` would move the second forward instead.

/** A time as a clock on the wall shows it, with no zone. */
export interface WallClock {
  year: number
  month: number
  day: number
  hour: number
  minute: number
  second: number
  /** 0 to 999999. */
  microsecond: number
}

const MINUTE = 60_000
const DAY = 86_400_000

function daysIn(year: number, month: number): number {
  return new Date(Date.UTC(year, month, 0)).getUTCDate()
}

/** A wall-clock time, or undefined where a field is out of range (the 30th
 * of February, the hour 24). Years run from 1000 to 9999. */
export function wallClock(
  year: number,
  month: number,
  day: number,
  hour = 0,
  minute = 0,
  second = 0,
  microsecond = 0
): WallClock | undefined {
  const all = [year, month, day, hour, minute, second, microsecond]
  if (!all.every(Number.isInteger)) return undefined
  if (year < 1000 || year > 9999 || month < 1 || month > 12) return undefined
  if (day < 1 || day > daysIn(year, month)) return undefined
  if (hour < 0 || hour > 23 || minute < 0 || minute > 59) return undefined
  if (second < 0 || second > 59) return undefined
  if (microsecond < 0 || microsecond > 999_999) return undefined
  return { year, month, day, hour, minute, second, microsecond }
}

/** The wall clock read as if it were UTC, in milliseconds, its
 * microseconds left out. */
function asUtc(w: WallClock): number {
  return Date.UTC(w.year, w.month - 1, w.day, w.hour, w.minute, w.second)
}

const FORMATS = new Map<string, Intl.DateTimeFormat | null>()

function formatFor(zone: string): Intl.DateTimeFormat | undefined {
  let f = FORMATS.get(zone)
  if (f === undefined) {
    try {
      f = new Intl.DateTimeFormat("en-US", {
        timeZone: zone,
        hourCycle: "h23",
        year: "numeric",
        month: "numeric",
        day: "numeric",
        hour: "numeric",
        minute: "numeric",
        second: "numeric",
      })
    } catch {
      f = null
    }
    FORMATS.set(zone, f)
  }
  return f ?? undefined
}

/** Whether this browser knows `zone` as an IANA time zone. */
export function knownZone(zone: string): boolean {
  return formatFor(zone) !== undefined
}

/** Minutes east of UTC that `zone` keeps at the instant `ms`, or undefined
 * for a zone this browser does not know. */
export function zoneOffset(zone: string, ms: number): number | undefined {
  const f = formatFor(zone)
  if (!f) return undefined
  const p: Record<string, number> = {}
  for (const part of f.formatToParts(ms))
    if (part.type !== "literal") p[part.type] = Number(part.value)
  const wall = Date.UTC(p.year, p.month - 1, p.day, p.hour, p.minute, p.second)
  const second = ms - (((ms % 1000) + 1000) % 1000)
  return Math.round((wall - second) / MINUTE)
}

/** The offset, in minutes east of UTC, a wall-clock time in `zone` takes:
 * the zone's offset at that moment; an ambiguous time takes the earlier
 * offset and a time in a spring-forward gap the offset from before the
 * change. Undefined for a zone this browser does not know. */
export function wallOffset(w: WallClock, zone: string): number | undefined {
  const t = asUtc(w)
  const before = zoneOffset(zone, t - DAY)
  const after = zoneOffset(zone, t + DAY)
  if (before === undefined || after === undefined) return undefined
  if (zoneOffset(zone, t - before * MINUTE) === before) return before
  if (zoneOffset(zone, t - after * MINUTE) === after) return after
  return before
}

const pad = (n: number, width = 2) => String(n).padStart(width, "0")

/** An offset as ISO 8601 writes it: `+01:00`, `-05:30`, `+00:00`. */
export function offsetText(minutes: number): string {
  const sign = minutes < 0 ? "-" : "+"
  const m = Math.abs(minutes)
  return `${sign}${pad(Math.floor(m / 60))}:${pad(m % 60)}`
}

/** A wall clock's date, `YYYY-MM-DD`. */
export function dateText(w: Pick<WallClock, "year" | "month" | "day">): string {
  return `${pad(w.year, 4)}-${pad(w.month)}-${pad(w.day)}`
}

/** A wall clock as ISO 8601, `YYYY-MM-DDTHH:MM:SS`, its microseconds as six
 * digits only where they are not zero, and the offset where one is given. */
export function isoText(w: WallClock, offset?: number): string {
  const time = `${pad(w.hour)}:${pad(w.minute)}:${pad(w.second)}`
  const frac = w.microsecond ? `.${pad(w.microsecond, 6)}` : ""
  const zone = offset === undefined ? "" : offsetText(offset)
  return `${dateText(w)}T${time}${frac}${zone}`
}

/** A wall-clock time in `zone` as an instant with the zone's offset at that
 * moment (`2026-07-01T09:30:00+01:00`), or undefined for a zone this
 * browser does not know. */
export function zoned(w: WallClock, zone: string): string | undefined {
  const offset = wallOffset(w, zone)
  return offset === undefined ? undefined : isoText(w, offset)
}

/** What a clock in `zone` shows at the instant `ms`, with the offset it
 * keeps then. */
export function wallAt(
  ms: number,
  zone: string
): { wall: WallClock; offset: number } | undefined {
  const offset = zoneOffset(zone, ms)
  if (offset === undefined) return undefined
  const d = new Date(ms + offset * MINUTE)
  const wall: WallClock = {
    year: d.getUTCFullYear(),
    month: d.getUTCMonth() + 1,
    day: d.getUTCDate(),
    hour: d.getUTCHours(),
    minute: d.getUTCMinutes(),
    second: d.getUTCSeconds(),
    microsecond: (((ms % 1000) + 1000) % 1000) * 1000,
  }
  return { wall, offset }
}

/** The instant `ms` as `zone` shows it, ISO 8601 with the offset
 * (`/Date(1767261600000)/` in London is `2026-01-01T10:00:00+00:00`). */
export function instantIn(ms: number, zone: string): string | undefined {
  const at = wallAt(ms, zone)
  return at && isoText(at.wall, at.offset)
}

const ISO =
  /^(\d{4})-(\d{2})-(\d{2})(?:[Tt ](\d{2}):(\d{2})(?::(\d{2})(?:[.,](\d+))?)?(Z|z|[+-]\d{2}(?::?\d{2})?)?)?$/

/** An ISO 8601 date or date and time, as Python's `fromisoformat` reads
 * one: its wall clock (fractions past six digits dropped) and its offset
 * in minutes where it names one (`Z` is zero). A date alone is midnight. */
export function parseIso(
  text: string
): { wall: WallClock; offset?: number } | undefined {
  const m = ISO.exec(text.trim())
  if (!m) return undefined
  const micro = m[7] ? Number(m[7].slice(0, 6).padEnd(6, "0")) : 0
  const wall = wallClock(
    Number(m[1]),
    Number(m[2]),
    Number(m[3]),
    m[4] === undefined ? 0 : Number(m[4]),
    m[5] === undefined ? 0 : Number(m[5]),
    m[6] === undefined ? 0 : Number(m[6]),
    micro
  )
  if (!wall) return undefined
  const z = m[8]
  if (!z) return { wall }
  if (z === "Z" || z === "z") return { wall, offset: 0 }
  const o = /^([+-])(\d{2}):?(\d{2})?$/.exec(z)
  if (!o) return undefined
  const minutes = Number(o[2]) * 60 + Number(o[3] ?? "0")
  if (Number(o[2]) > 23 || Number(o[3] ?? "0") > 59) return undefined
  return { wall, offset: o[1] === "-" ? -minutes : minutes }
}

/** A stamp's UTC offset in minutes, or undefined where it names none (or
 * is not ISO 8601). */
export function offsetMinutes(stamp: string): number | undefined {
  return parseIso(stamp)?.offset
}

/** Whole seconds since the epoch of an ISO 8601 stamp, truncated as
 * Python's `int(timestamp)` truncates; a stamp naming no offset is read in
 * `zone`. Undefined where the stamp is not ISO 8601 or the zone unknown. */
export function epoch(stamp: string, zone = "UTC"): number | undefined {
  const p = parseIso(stamp)
  if (!p) return undefined
  const offset = p.offset ?? wallOffset(p.wall, zone)
  if (offset === undefined) return undefined
  const base = asUtc(p.wall) / 1000 - offset * 60
  return p.wall.microsecond > 0 && base < 0 ? base + 1 : base
}

/** The instant of an ISO 8601 stamp in milliseconds, or undefined where it
 * names no offset. */
export function instantMs(stamp: string): number | undefined {
  const p = parseIso(stamp)
  if (!p || p.offset === undefined) return undefined
  return (
    asUtc(p.wall) - p.offset * MINUTE + Math.floor(p.wall.microsecond / 1000)
  )
}

/** The date an instant falls on in `zone`, `YYYY-MM-DD`; a stamp naming no
 * offset gives its own date. */
export function dateIn(stamp: string, zone: string): string | undefined {
  const p = parseIso(stamp)
  if (!p) return undefined
  if (p.offset === undefined) return dateText(p.wall)
  const at = wallAt(asUtc(p.wall) - p.offset * MINUTE, zone)
  return at && dateText(at.wall)
}

/** When a run of a provider's fetcher started, as epoch seconds: the
 * manifest's `startedAt`, else the newest of its `runs[].startedAt` (or
 * `runs[].started`). Undefined where the manifest names no start. */
export function runStarted(manifest: unknown): number | undefined {
  if (!manifest || typeof manifest !== "object") return undefined
  const m = manifest as { startedAt?: unknown; runs?: unknown }
  if (typeof m.startedAt === "string") return epoch(m.startedAt)
  if (!Array.isArray(m.runs)) return undefined
  let newest: number | undefined
  for (const run of m.runs as unknown[]) {
    if (!run || typeof run !== "object") continue
    const r = run as { startedAt?: unknown; started?: unknown }
    const s = typeof r.startedAt === "string" ? r.startedAt : r.started
    const t = typeof s === "string" ? epoch(s) : undefined
    if (t !== undefined && (newest === undefined || t > newest)) newest = t
  }
  return newest
}

/** A `YYYY-MM-DD` date `months` calendar months later (or earlier), its day
 * kept where the month has it and the month's last day where it has not
 * (31 January plus one month is 28 or 29 February). */
export function addMonths(date: string, months: number): string | undefined {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date)
  if (!m || !Number.isInteger(months)) return undefined
  const index = Number(m[1]) * 12 + Number(m[2]) - 1 + months
  const year = Math.floor(index / 12)
  const month = index - year * 12 + 1
  const day = Math.min(Number(m[3]), daysIn(year, month))
  const w = wallClock(year, month, day)
  return w && dateText(w)
}

const MONTHS = [
  "january",
  "february",
  "march",
  "april",
  "may",
  "june",
  "july",
  "august",
  "september",
  "october",
  "november",
  "december",
]

const FIELDS: Record<string, string> = {
  d: "(3[01]|[12]\\d|0[1-9]|[1-9]| [1-9])",
  m: "(1[0-2]|0[1-9]|[1-9])",
  Y: "(\\d{4})",
  y: "(\\d{2})",
  H: "(2[0-3]|[01]\\d|\\d)",
  I: "(1[0-2]|0[1-9]|[1-9])",
  M: "([0-5]\\d|\\d)",
  S: "([0-5]\\d|\\d)",
  f: "(\\d{1,6})",
  p: "(am|pm)",
  b: `(${MONTHS.map((n) => n.slice(0, 3)).join("|")})`,
  B: `(${MONTHS.join("|")})`,
}

/** A wall-clock time read by a `strptime` format, as Python reads one: the
 * directives `%d %m %Y %y %H %I %M %S %f %p %b %B` (month names in
 * English, any case), `%%`, a run of spaces matching any run of
 * whitespace, and the whole text matched. Undefined where it does not
 * match or names no real time. */
export function parseClock(
  text: string,
  format: string
): WallClock | undefined {
  const order: string[] = []
  let source = ""
  for (let i = 0; i < format.length; i++) {
    const c = format[i]
    if (c === "%" && i + 1 < format.length) {
      const d = format[++i]
      if (d === "%") source += "%"
      else if (FIELDS[d]) {
        source += FIELDS[d]
        order.push(d)
      } else return undefined
    } else if (/\s/.test(c)) {
      while (i + 1 < format.length && /\s/.test(format[i + 1])) i++
      source += "\\s+"
    } else source += c.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")
  }
  const m = new RegExp(`^${source}$`, "i").exec(text)
  if (!m) return undefined
  const v: Record<string, string> = {}
  order.forEach((d, i) => (v[d] = m[i + 1].trim().toLowerCase()))
  const month =
    v.m !== undefined
      ? Number(v.m)
      : v.b !== undefined
        ? MONTHS.findIndex((n) => n.startsWith(v.b)) + 1
        : v.B !== undefined
          ? MONTHS.indexOf(v.B) + 1
          : 1
  const year =
    v.Y !== undefined
      ? Number(v.Y)
      : v.y !== undefined
        ? Number(v.y) + (Number(v.y) < 69 ? 2000 : 1900)
        : 1900
  let hour = v.H !== undefined ? Number(v.H) : 0
  if (v.I !== undefined) {
    hour = Number(v.I) % 12
    if (v.p === "pm") hour += 12
  }
  return wallClock(
    year,
    month,
    v.d === undefined ? 1 : Number(v.d),
    hour,
    v.M === undefined ? 0 : Number(v.M),
    v.S === undefined ? 0 : Number(v.S),
    v.f === undefined ? 0 : Number(v.f.padEnd(6, "0"))
  )
}
