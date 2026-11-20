// A provider's numbers as text: a value keeps the digits the lab wrote, and
// whatever compares or scales one does so as a decimal, never a float, so
// `2.90` equals `2.9` and `3.0` times 10 is `30`
// (apps/healthspan/docs/importers/00-shared.md, Numbers).

import { BigDecimal, Option } from "effect"
import type { Comparator } from "../../values"

export type { Comparator } from "../../values"

// What Python's str.strip() strips: str.isspace's characters, which hold the
// information separators U+001C to U+001F and not U+FEFF, unlike
// String.prototype.trim.
const EDGE = String.raw`[\t\n\v\f\r \x1c-\x1f\x85\xa0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+`
const EDGES = new RegExp(`^${EDGE}|${EDGE}$`, "g")

/** `text` without the whitespace at either end, as the providers' readers
 * have always stripped it. */
export function strip(text: string): string {
  return text.replace(EDGES, "")
}

const SPLIT = /^\s*(<=|>=|≤|≥|<|>)\s*([0-9.]+)\s*$/

/** A result's comparator and digits (`< 5` is `<` and `5`; `≤` reads as
 * `<=`), or the whole text stripped, with no comparator, where it is not a
 * comparator before digits. */
export function splitComparator(text: string): {
  comparator?: Comparator
  value: string
} {
  const m = SPLIT.exec(text)
  if (!m) return { value: strip(text) }
  const c = m[1]
  const comparator: Comparator =
    c === "≤" ? "<=" : c === "≥" ? ">=" : (c as Comparator)
  return { comparator, value: m[2] }
}

/** A quantity as a record's property holds it: the digits as written and
 * a UCUM code, with a comparator for a bound (`<0.01`). */
export interface Quantity {
  value: string
  unit: string
  comparator?: Comparator
}

/** A quantity, its comparator left out where there is none. */
export function quantity(
  value: string,
  unit: string,
  comparator?: Comparator | null
): Quantity {
  return comparator ? { value, unit, comparator } : { value, unit }
}

const NUMBER = /^-?[0-9]+(\.[0-9]+)?$/

/** Whether `text` is a plain decimal number: digits with a point and digits
 * after it or not, a minus sign or not; no `+`, no exponent, no `.5`, no
 * comma. */
export function isNumber(text: string): boolean {
  return NUMBER.test(strip(text))
}

/** A number written with `,` for the decimal point and `.` between
 * thousands, as a decimal string: `12.345` is `12345`, `20,8` is `20.8`. */
export function commaDecimal(text: string): string {
  return strip(text).replace(/\./g, "").replace(",", ".")
}

/** A provider's number as decimal digits, or undefined where it is not a
 * plain number; `comma` reads `,` as the decimal point and `.` as a
 * thousands separator. */
export function numberText(
  text: string,
  options: { comma?: boolean } = {}
): string | undefined {
  const s = options.comma ? commaDecimal(text) : strip(text)
  return NUMBER.test(s) ? s : undefined
}

/** A plain number as an exact decimal, or undefined. */
export function toDecimal(text: string): BigDecimal.BigDecimal | undefined {
  const s = strip(text)
  return NUMBER.test(s)
    ? Option.getOrUndefined(BigDecimal.fromString(s))
    : undefined
}

/** How two plain numbers compare as decimals, or undefined where either is
 * not one. */
export function compareDecimals(a: string, b: string): -1 | 0 | 1 | undefined {
  const x = toDecimal(a)
  const y = toDecimal(b)
  return x && y ? BigDecimal.Order(x, y) : undefined
}

/** Whether two plain numbers are the same decimal (`2.90` and `2.9`). */
export function sameDecimal(a: string, b: string): boolean {
  return compareDecimals(a, b) === 0
}

/** `text` times `factor`, as fixed-point digits with no trailing zeros
 * (`3.0` times 10 is `30`, `0.50` times 1 is `0.5`), or undefined where
 * either is not a plain number. */
export function scaleDecimal(text: string, factor: string): string | undefined {
  const x = toDecimal(text)
  const f = toDecimal(factor)
  return x && f
    ? BigDecimal.format(BigDecimal.normalize(BigDecimal.multiply(x, f)))
    : undefined
}

/** A float as Python's `str` writes it, for a provider whose digits the
 * scripts took from a parsed float: `140.0` is `140.0`, `1e-05` is
 * `1e-05`, `0.0001` is `0.0001`. */
export function pythonFloat(n: number): string {
  if (Number.isNaN(n)) return "nan"
  if (!Number.isFinite(n)) return n > 0 ? "inf" : "-inf"
  if (n === 0) return Object.is(n, -0) ? "-0.0" : "0.0"
  const [mantissa, exp] = n.toExponential().split("e")
  const e = Number(exp)
  const sign = mantissa.startsWith("-") ? "-" : ""
  const digits = mantissa.replace(/^-/, "").replace(".", "")
  if (e < -4 || e >= 16) {
    const m = digits.length > 1 ? `${digits[0]}.${digits.slice(1)}` : digits
    const es = `${e < 0 ? "-" : "+"}${String(Math.abs(e)).padStart(2, "0")}`
    return `${sign}${m}e${es}`
  }
  if (e < 0) return `${sign}0.${"0".repeat(-e - 1)}${digits}`
  const whole = digits.slice(0, e + 1).padEnd(e + 1, "0")
  const frac = digits.slice(e + 1)
  return `${sign}${whole}.${frac || "0"}`
}
