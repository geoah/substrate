// The cells of a lab file as values: a result with its comparator or its
// unit stuck on, a range in any of the forms labs print, a flag, a unit's
// spelling, and a date in the common formats with the day and month order
// read from the whole column.

export type Comparator = "<" | "<=" | ">" | ">="

export type ParsedValue =
  | { kind: "number"; value: string; comparator?: Comparator; unit?: string }
  | { kind: "text"; text: string }
  | { kind: "empty" }

export interface ParsedRange {
  low?: string
  high?: string
  /** A range that is not two numbers, such as Negative. */
  text?: string
}

const COMPARATOR = /^(<=|>=|≤|≥|=<|=>|<|>)\s*/
const NUMBER = /^[+-]?(?:\d+(?:[.,]\d+)*|[.,]\d+)$/

function comparatorOf(s: string): Comparator {
  switch (s) {
    case "<=":
    case "≤":
    case "=<":
      return "<="
    case ">=":
    case "≥":
    case "=>":
      return ">="
    case ">":
      return ">"
    default:
      return "<"
  }
}

/** A number's digits as a decimal string, or undefined: a decimal comma
 * read as a point, thousands separators dropped (`1,234.5`), a bare
 * fraction given its zero (`.5`). */
export function decimal(text: string): string | undefined {
  let s = text.trim().replace(/\s+/g, "")
  if (!NUMBER.test(s)) return undefined
  const sign = s.startsWith("-") ? "-" : ""
  s = s.replace(/^[+-]/, "")
  if (/^\d{1,3}(,\d{3})+(\.\d+)?$/.test(s)) s = s.replace(/,/g, "")
  else if (/^\d{1,3}(\.\d{3})+,\d+$/.test(s))
    s = s.replace(/\./g, "").replace(",", ".")
  else if (/^\d*,\d+$/.test(s)) s = s.replace(",", ".")
  else if (s.includes(",")) return undefined
  if ((s.match(/\./g) ?? []).length > 1) return undefined
  if (s.startsWith(".")) s = `0${s}`
  return sign + s
}

/** A result cell: a number (with its comparator, and its unit where the
 * cell carries one), else words. */
export function parseValue(cell: string): ParsedValue {
  const s = cell.trim()
  if (!s) return { kind: "empty" }
  const cmp = COMPARATOR.exec(s)
  const rest = cmp ? s.slice(cmp[0].length) : s
  const n = decimal(rest)
  if (n !== undefined)
    return {
      kind: "number",
      value: n,
      ...(cmp ? { comparator: comparatorOf(cmp[1]) } : {}),
    }
  const withUnit = /^([+-]?[\d.,]+)\s*([^\d\s.,].*)$/.exec(rest)
  if (withUnit) {
    const num = decimal(withUnit[1])
    if (num !== undefined)
      return {
        kind: "number",
        value: num,
        unit: normaliseUnit(withUnit[2]),
        ...(cmp ? { comparator: comparatorOf(cmp[1]) } : {}),
      }
  }
  return { kind: "text", text: s }
}

/** A range cell in any form a lab prints: `3.5-5.0`, `3.5 - 5.0`,
 * `3.5 to 5.0`, `-1.0 - 1.0`, `<5`, `≤ 5`, `up to 5`, `>60`, `60+`,
 * `60 or more`, with or without its unit after it; anything else is kept
 * as words. */
export function parseRange(cell: string): ParsedRange {
  let s = cell.trim()
  if (!s) return {}
  s = s.replace(/^[([]\s*|\s*[)\]]$/g, "")
  const num = String.raw`([+-]?(?:\d[\d.,]*|[.,]\d+))`
  const unit = String.raw`(?:\s*[^\d\s.,<>≤≥=+-][^\d]*)?`
  const between = new RegExp(
    `^${num}\\s*(?:-|–|—|to|\\.\\.)\\s*${num}${unit}$`,
    "i"
  ).exec(s)
  if (between) {
    const low = decimal(between[1])
    const high = decimal(between[2])
    if (low !== undefined && high !== undefined) return { low, high }
  }
  const below = new RegExp(
    `^(?:<=|<|≤|=<|up to|below|under|less than)\\s*${num}${unit}$`,
    "i"
  ).exec(s)
  if (below) {
    const high = decimal(below[1])
    if (high !== undefined) return { high }
  }
  const above = new RegExp(
    `^(?:>=|>|≥|=>|above|over|greater than|more than)\\s*${num}${unit}$`,
    "i"
  ).exec(s)
  if (above) {
    const low = decimal(above[1])
    if (low !== undefined) return { low }
  }
  const orMore = new RegExp(
    `^${num}${unit}\\s*(?:\\+|or more|or above)$`,
    "i"
  ).exec(s)
  if (orMore) {
    const low = decimal(orMore[1])
    if (low !== undefined) return { low }
  }
  const orLess = new RegExp(
    `^${num}${unit}\\s*(?:or less|or below)$`,
    "i"
  ).exec(s)
  if (orLess) {
    const high = decimal(orLess[1])
    if (high !== undefined) return { high }
  }
  return { text: s }
}

/** A flag as the lab's code: H, L, HH, LL, A or N for the words labs use
 * for them, anything else as written. Empty is no flag. */
export function parseFlag(cell: string): string | undefined {
  const s = cell.trim()
  if (!s) return undefined
  const n = s
    .toLowerCase()
    .replace(/[^a-z*↑↓+!<>]+/g, " ")
    .trim()
  if (
    ["hh", "critical high", "high critical", "panic high", "h*", "h!"].includes(
      n
    )
  )
    return "HH"
  if (
    ["ll", "critical low", "low critical", "panic low", "l*", "l!"].includes(n)
  )
    return "LL"
  if (["h", "high", "hi", "↑", "+", ">", "above"].includes(n)) return "H"
  if (["l", "low", "lo", "↓", "-", "<", "below"].includes(n)) return "L"
  if (["a", "abnormal", "abn", "*", "!"].includes(n)) return "A"
  if (["n", "normal", "nl", "within range", "in range"].includes(n)) return "N"
  return s.slice(0, 16)
}

/** A unit's spelling as the repository's unit records most likely know
 * it: micro as `u`, litres as `L`, international units in capitals. The
 * server reads codes and aliases too, so this only mends the commonest
 * slips; a spelling it still does not know is said when matching. */
export function normaliseUnit(cell: string): string | undefined {
  let s = cell.trim()
  if (!s) return undefined
  s = s.replace(/[µμ]/g, "u")
  s = s.replace(/\/([mdunpf]?)l$/i, (_, p: string) => `/${p.toLowerCase()}L`)
  s = s.replace(/^([mu]?)iu\b/i, (_, p: string) => `${p.toLowerCase()}IU`)
  s = s.replace(/^u\/L$/i, "U/L")
  return s
}

// ---- dates ----

export type DayOrder = "dmy" | "mdy"

export interface ParsedDate {
  y: number
  m: number
  d: number
  hh?: number
  mm?: number
  ss?: number
  /** Minutes east of UTC, where the cell says. */
  offset?: number
}

const MONTHS = [
  "jan",
  "feb",
  "mar",
  "apr",
  "may",
  "jun",
  "jul",
  "aug",
  "sep",
  "oct",
  "nov",
  "dec",
]

function month(word: string): number | undefined {
  const i = MONTHS.indexOf(word.slice(0, 3).toLowerCase())
  return i < 0 ? undefined : i + 1
}

const TIME = String.raw`(?:[T,\s]+(\d{1,2}):(\d{2})(?::(\d{2})(?:\.\d+)?)?\s*(am|pm)?\s*(Z|[+-]\d{2}:?\d{2})?)?`

function withTime(
  date: { y: number; m: number; d: number },
  t: (string | undefined)[]
): ParsedDate | undefined {
  const [h, mi, se, ampm, zone] = t
  const out: ParsedDate = { ...date }
  if (h !== undefined) {
    let hh = Number(h)
    if (ampm) {
      if (hh > 12) return undefined
      hh = (hh % 12) + (ampm.toLowerCase() === "pm" ? 12 : 0)
    }
    out.hh = hh
    out.mm = Number(mi)
    out.ss = se === undefined ? 0 : Number(se)
    if (out.hh > 23 || out.mm > 59 || out.ss > 59) return undefined
  }
  if (zone) {
    if (zone === "Z") out.offset = 0
    else {
      const z = /^([+-])(\d{2}):?(\d{2})$/.exec(zone)
      if (z)
        out.offset =
          (z[1] === "-" ? -1 : 1) * (Number(z[2]) * 60 + Number(z[3]))
    }
  }
  return valid(out) ? out : undefined
}

function valid(p: ParsedDate): boolean {
  if (p.m < 1 || p.m > 12 || p.d < 1 || p.y < 1900 || p.y > 2200) return false
  const days = new Date(Date.UTC(p.y, p.m, 0)).getUTCDate()
  return p.d <= days
}

function year(s: string): number {
  const n = Number(s)
  return s.length === 2 ? (n < 70 ? 2000 + n : 1900 + n) : n
}

/** The two numbers before the year in `12/03/2026` (or `.` or `-`), where
 * the cell has that form. */
function numericPair(cell: string): [number, number] | undefined {
  const m = /^(\d{1,2})[/.-](\d{1,2})[/.-](\d{4}|\d{2})\b/.exec(cell.trim())
  return m ? [Number(m[1]), Number(m[2])] : undefined
}

/** The day and month order a date column is written in: day first where
 * a first number passes 12, month first where a second does, undefined
 * where every date reads both ways (the owner is asked), day first where
 * no date has the numeric form at all. */
export function detectDayOrder(cells: string[]): DayOrder | undefined {
  let dmy = 0
  let mdy = 0
  let numeric = 0
  for (const c of cells) {
    const p = numericPair(c)
    if (!p) continue
    numeric++
    if (p[0] > 12 && p[1] <= 12) dmy++
    else if (p[1] > 12 && p[0] <= 12) mdy++
  }
  if (!numeric) return "dmy"
  if (!dmy && !mdy) return undefined
  return dmy >= mdy ? "dmy" : "mdy"
}

/** A date cell in the common formats: ISO (`2026-03-12`, with a time and
 * an offset or not), `2026/03/12`, `12/03/2026` in the column's order,
 * `12 Mar 2026`, `12-Mar-26`, `Mar 12, 2026`; a time after any of them. */
export function parseDate(
  cell: string,
  order: DayOrder = "dmy"
): ParsedDate | undefined {
  const s = cell.trim()
  if (!s) return undefined
  let m = new RegExp(`^(\\d{4})-(\\d{1,2})-(\\d{1,2})${TIME}$`, "i").exec(s)
  if (m)
    return withTime(
      { y: Number(m[1]), m: Number(m[2]), d: Number(m[3]) },
      m.slice(4)
    )
  m = new RegExp(`^(\\d{4})[/.](\\d{1,2})[/.](\\d{1,2})${TIME}$`, "i").exec(s)
  if (m)
    return withTime(
      { y: Number(m[1]), m: Number(m[2]), d: Number(m[3]) },
      m.slice(4)
    )
  m = new RegExp(
    `^(\\d{1,2})[/.-](\\d{1,2})[/.-](\\d{4}|\\d{2})${TIME}$`,
    "i"
  ).exec(s)
  if (m) {
    const a = Number(m[1])
    const b = Number(m[2])
    const [d, mo] = order === "dmy" ? [a, b] : [b, a]
    return withTime({ y: year(m[3]), m: mo, d }, m.slice(4))
  }
  m = new RegExp(
    `^(\\d{1,2})(?:st|nd|rd|th)?[\\s-]+([a-z]{3,})\\.?,?[\\s-]+(\\d{4}|\\d{2})${TIME}$`,
    "i"
  ).exec(s)
  if (m) {
    const mo = month(m[2])
    if (mo)
      return withTime({ y: year(m[3]), m: mo, d: Number(m[1]) }, m.slice(4))
  }
  m = new RegExp(
    `^([a-z]{3,})\\.?\\s+(\\d{1,2})(?:st|nd|rd|th)?,?\\s+(\\d{4})${TIME}$`,
    "i"
  ).exec(s)
  if (m) {
    const mo = month(m[1])
    if (mo)
      return withTime({ y: Number(m[3]), m: mo, d: Number(m[2]) }, m.slice(4))
  }
  return undefined
}

const pad = (n: number, w = 2) => String(Math.abs(n)).padStart(w, "0")

function offsetText(minutes: number): string {
  const sign = minutes < 0 ? "-" : "+"
  return `${sign}${pad(Math.floor(Math.abs(minutes) / 60))}:${pad(Math.abs(minutes) % 60)}`
}

/** When a dated cell was taken, as RFC 3339 with its offset, and that
 * offset in minutes east of UTC. A date with no time is placed at local
 * noon, so it stays on its day for a reader a few zones away; one with no
 * offset is in this browser's zone. */
export function instantOf(p: ParsedDate): {
  at: string
  utcOffset: number
  day: string
} {
  const hh = p.hh ?? 12
  const mm = p.mm ?? 0
  const ss = p.ss ?? 0
  const day = `${pad(p.y, 4)}-${pad(p.m)}-${pad(p.d)}`
  const offset =
    p.offset ?? -new Date(p.y, p.m - 1, p.d, hh, mm, ss).getTimezoneOffset()
  return {
    at: `${day}T${pad(hh)}:${pad(mm)}:${pad(ss)}${offsetText(offset)}`,
    utcOffset: offset,
    day,
  }
}

/** The local day (`YYYY-MM-DD`) an instant falls on in this browser. */
export function localDay(iso: string): string {
  const d = new Date(iso)
  return `${pad(d.getFullYear(), 4)}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

/** The offset an RFC 3339 instant was written at, in minutes east of
 * UTC, or this browser's for that instant where it names none. */
export function offsetOf(iso: string): number {
  const m = /([+-])(\d{2}):?(\d{2})$/.exec(iso)
  if (m) return (m[1] === "-" ? -1 : 1) * (Number(m[2]) * 60 + Number(m[3]))
  if (/Z$/i.test(iso)) return 0
  return -new Date(iso).getTimezoneOffset()
}
