/** The A2UI v1.0 basic catalog's functions, as the renderer runs them: the
 * checks an input carries, the formats a text reads through, and the logic
 * that joins them. Each takes its args already resolved and answers
 * undefined where it cannot, so a bad arg blanks one value and nothing
 * else. `openUrl` acts only from a button (urls.ts): evaluated as a value
 * it does nothing. */

import { isRecord, toBool, toNumber, toText } from "./coerce"
import { getAt, resolvePath } from "./pointer"
import type { EvalContext } from "./values"

type Call = (name: string, args: unknown, ctx: EvalContext) => unknown

type Fn = (
  args: Record<string, unknown>,
  ctx: EvalContext,
  call: Call
) => unknown

function locale(ctx: EvalContext): string | undefined {
  return ctx.locale
}

// ── formatString ────────────────────────────────────────────────────────────

/** The index of the `}` that closes an expression opened before `from`, or
 * -1. Quotes count only inside a call's parentheses, where a string arg
 * may hold a brace. */
function closingBrace(text: string, from: number): number {
  let depth = 0
  let parens = 0
  let quote = ""
  for (let i = from; i < text.length; i++) {
    const c = text[i]
    if (quote) {
      if (c === "\\") i++
      else if (c === quote) quote = ""
      continue
    }
    if (parens > 0 && (c === "'" || c === '"')) quote = c
    else if (c === "(") parens++
    else if (c === ")") parens = Math.max(0, parens - 1)
    else if (c === "$" && text[i + 1] === "{") {
      depth++
      i++
    } else if (c === "}") {
      if (depth === 0) return i
      depth--
    }
  }
  return -1
}

/** A call's args split at their top-level commas. */
function splitArgs(text: string): string[] {
  const parts: string[] = []
  let depth = 0
  let quote = ""
  let start = 0
  for (let i = 0; i < text.length; i++) {
    const c = text[i]
    if (quote) {
      if (c === "\\") i++
      else if (c === quote) quote = ""
      continue
    }
    if (c === "'" || c === '"') quote = c
    else if (c === "(" || (c === "$" && text[i + 1] === "{")) depth++
    else if ((c === ")" || c === "}") && depth > 0) depth--
    else if (c === "," && depth === 0) {
      parts.push(text.slice(start, i))
      start = i + 1
    }
  }
  if (text.slice(start).trim()) parts.push(text.slice(start))
  return parts
}

function unquote(text: string): string {
  return text.slice(1, -1).replace(/\\(.)/g, "$1")
}

/** One arg's value: a quoted string, a number, a boolean, null, a nested
 * `${…}`, or else a path. */
function argValue(text: string, ctx: EvalContext, call: Call): unknown {
  const t = text.trim()
  if (
    t.length >= 2 &&
    (t[0] === "'" || t[0] === '"') &&
    t[t.length - 1] === t[0]
  )
    return unquote(t)
  if (t === "true") return true
  if (t === "false") return false
  if (t === "null") return null
  if (/^-?\d+(\.\d+)?$/.test(t)) return Number(t)
  if (t.startsWith("${") && closingBrace(t, 2) === t.length - 1)
    return expression(t.slice(2, -1), ctx, call)
  return getAt(ctx.model, resolvePath(t, ctx.scope))
}

const CALL = /^([A-Za-z_@][\w]*)\s*\(([\s\S]*)\)$/

/** One `${…}`: a call with named args, or a path. */
function expression(text: string, ctx: EvalContext, call: Call): unknown {
  const t = text.trim()
  const m = CALL.exec(t)
  if (m) {
    const args: Record<string, unknown> = {}
    for (const part of splitArgs(m[2])) {
      const colon = part.indexOf(":")
      if (colon < 0) continue
      args[part.slice(0, colon).trim()] = argValue(
        part.slice(colon + 1),
        ctx,
        call
      )
    }
    // The values are resolved already: a literal stays itself.
    return call(m[1], args, ctx)
  }
  return getAt(ctx.model, resolvePath(t, ctx.scope))
}

/** `formatString`: every `${expression}` replaced by its value as text;
 * `\${` is a literal `${`. */
export function interpolate(
  template: string,
  ctx: EvalContext,
  call: Call
): string {
  let out = ""
  let i = 0
  while (i < template.length) {
    if (template.startsWith("\\${", i)) {
      out += "${"
      i += 3
    } else if (template.startsWith("${", i)) {
      const end = closingBrace(template, i + 2)
      if (end < 0) {
        out += template.slice(i)
        break
      }
      out += toText(expression(template.slice(i + 2, end), ctx, call))
      i = end + 1
    } else {
      out += template[i]
      i++
    }
  }
  return out
}

// ── numbers ─────────────────────────────────────────────────────────────────

function numberOptions(
  args: Record<string, unknown>
): Intl.NumberFormatOptions {
  const decimals = toNumber(args.decimals)
  const options: Intl.NumberFormatOptions = {
    useGrouping: args.grouping !== false,
  }
  if (decimals !== undefined && decimals >= 0 && decimals <= 20) {
    options.minimumFractionDigits = decimals
    options.maximumFractionDigits = decimals
  }
  return options
}

// ── dates ───────────────────────────────────────────────────────────────────

/** A date from an ISO date (read as local midnight, so `2026-01-16` stays
 * the 16th west of Greenwich), an ISO date-time, a time of day (today at
 * it), or epoch milliseconds. */
export function parseDate(value: unknown): Date | undefined {
  if (typeof value === "number")
    return Number.isFinite(value) ? new Date(value) : undefined
  if (typeof value !== "string" || !value) return undefined
  const day = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value)
  if (day) return new Date(Number(day[1]), Number(day[2]) - 1, Number(day[3]))
  const time = /^(\d{2}):(\d{2})(?::(\d{2}))?$/.exec(value)
  if (time) {
    const d = new Date()
    d.setHours(Number(time[1]), Number(time[2]), Number(time[3] ?? 0), 0)
    return d
  }
  const at = Date.parse(value)
  return Number.isNaN(at) ? undefined : new Date(at)
}

const pad = (n: number, width = 2) => String(n).padStart(width, "0")

const DATE_TOKEN =
  /'(?:[^']|'')*'|yyyy|yy|MMMM|MMM|MM|M|dd|d|EEEE|EEE|EE|E|HH|H|hh|h|mm|ss|a/g

/** A date written in a Unicode TR35 pattern, the subset the catalog
 * documents; text in single quotes is literal. */
export function formatDatePattern(
  date: Date,
  pattern: string,
  lang?: string
): string {
  const name = (options: Intl.DateTimeFormatOptions) =>
    new Intl.DateTimeFormat(lang, options).format(date)
  const hour12 = date.getHours() % 12 || 12
  return pattern.replace(DATE_TOKEN, (token) => {
    switch (token) {
      case "yyyy":
        return String(date.getFullYear())
      case "yy":
        return pad(date.getFullYear() % 100)
      case "MMMM":
        return name({ month: "long" })
      case "MMM":
        return name({ month: "short" })
      case "MM":
        return pad(date.getMonth() + 1)
      case "M":
        return String(date.getMonth() + 1)
      case "dd":
        return pad(date.getDate())
      case "d":
        return String(date.getDate())
      case "EEEE":
        return name({ weekday: "long" })
      case "EEE":
      case "EE":
      case "E":
        return name({ weekday: "short" })
      case "HH":
        return pad(date.getHours())
      case "H":
        return String(date.getHours())
      case "hh":
        return pad(hour12)
      case "h":
        return String(hour12)
      case "mm":
        return pad(date.getMinutes())
      case "ss":
        return pad(date.getSeconds())
      case "a":
        return date.getHours() < 12 ? "AM" : "PM"
      default:
        return token.slice(1, -1).replace(/''/g, "'")
    }
  })
}

// ── checks ──────────────────────────────────────────────────────────────────

function isEmpty(value: unknown): boolean {
  return (
    value === undefined ||
    value === null ||
    (typeof value === "string" && value.trim() === "") ||
    (Array.isArray(value) && value.length === 0) ||
    (isRecord(value) && Object.keys(value).length === 0)
  )
}

const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

function withinRange(n: number, args: Record<string, unknown>): boolean {
  const min = toNumber(args.min)
  const max = toNumber(args.max)
  return (min === undefined || n >= min) && (max === undefined || n <= max)
}

function boolList(value: unknown): boolean[] {
  return Array.isArray(value) ? value.map(toBool) : []
}

// ── the catalog ─────────────────────────────────────────────────────────────

export const FUNCTIONS: Record<string, Fn> = {
  required: ({ value }) => !isEmpty(value),
  regex: ({ value, pattern }) => {
    if (typeof pattern !== "string") return false
    try {
      return new RegExp(pattern).test(toText(value))
    } catch {
      return false
    }
  },
  length: (args) => {
    const value = args.value
    const n = Array.isArray(value) ? value.length : toText(value).length
    return withinRange(n, args)
  },
  numeric: (args) => {
    const n = toNumber(args.value)
    return n !== undefined && withinRange(n, args)
  },
  email: ({ value }) => EMAIL.test(toText(value)),
  formatString: ({ value }, ctx, call) => interpolate(toText(value), ctx, call),
  formatNumber: (args, ctx) => {
    const n = toNumber(args.value)
    if (n === undefined) return undefined
    return new Intl.NumberFormat(locale(ctx), numberOptions(args)).format(n)
  },
  formatCurrency: (args, ctx) => {
    const n = toNumber(args.value)
    if (n === undefined || typeof args.currency !== "string") return undefined
    return new Intl.NumberFormat(locale(ctx), {
      ...numberOptions(args),
      style: "currency",
      currency: args.currency,
    }).format(n)
  },
  formatDate: ({ value, format }, ctx) => {
    const date = parseDate(value)
    if (!date) return undefined
    if (typeof format !== "string" || !format)
      return new Intl.DateTimeFormat(locale(ctx), {
        dateStyle: "medium",
      }).format(date)
    return formatDatePattern(date, format, locale(ctx))
  },
  pluralize: (args, ctx) => {
    const n = toNumber(args.value)
    if (n === undefined) return toText(args.other)
    if (n === 0 && args.zero !== undefined) return toText(args.zero)
    const category = new Intl.PluralRules(locale(ctx)).select(n)
    return toText(args[category] ?? args.other)
  },
  openUrl: () => undefined,
  and: ({ values }) => {
    const list = boolList(values)
    return list.length > 0 && list.every(Boolean)
  },
  or: ({ values }) => boolList(values).some(Boolean),
  not: ({ value }) => !toBool(value),
}
