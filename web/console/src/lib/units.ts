/** The `quantity` datatype, client-side: the shape of a stored value, the
 * decimal grammar its digits follow, and the text an editor holds. Units are
 * the repository's own records (decision 0186): everything that resolves,
 * prints or converts a unit reads them through a `UnitReader`
 * (lib/unit-reader.ts) over the repository's graph (lib/unit-graph.ts).
 *
 * A quantity value is `{value: "72.5", unit: "kg"}`, with `comparator: "<"`
 * beside them where the property declares `comparator: true`. The value is
 * exact digits written as a string, as a decimal is; the unit is the id of
 * one of the repository's unit records, which a write stores whatever
 * spelling was typed.
 *
 * - `formatQuantityText` is the EXACT text an editor holds, `72.5 kg`,
 *   `37 Cel`, and `parseQuantityText` reads it back to the same value.
 * - A reader's `format` is the display, the server's title form: `37 °C`. */

import { durationWords } from "@/lib/observations-time"

export type Comparator = "<" | "<=" | ">=" | ">"

export interface Quantity {
  value: string
  unit: string
  comparator?: Comparator
}

/** What a quantity property declares beyond its datatype, as `PropSpec`
 * carries it. */
export interface QuantityDeclaration {
  /** The declared `unit:` (decision 0186): the unit every value's unit
   * converts to, read through the repository's unit graph. */
  unit?: string
  /** The declared `delta: true`: every value is a difference. */
  delta?: boolean
  units?: string[]
  comparator?: boolean
  minQuantity?: Quantity
  maxQuantity?: Quantity
  default?: unknown
}

/** The picker's units under one heading: the name of the linear
 * component's root unit, absent where the declaration lists its units. */
export interface UnitGroup {
  heading?: string
  units: string[]
}

/** The comparators a quantity value may carry (`vocabulary.Comparators`). */
export const COMPARATORS: readonly Comparator[] = ["<", "<=", ">=", ">"]

/** The most digits a decimal holds (`vocabulary.MaxDecimalDigits`). */
const MAX_DECIMAL_DIGITS = 1000

/** The worked value, as the text an editor takes. */
export const QUANTITY_TEXT_EXAMPLE = "72.5 kg"

/** A float's digits at twelve significant places, which clears a binary
 * float's noise: `7099.999999999999` is `7100`, `0.1 + 0.2` is `0.3`. */
export function floatDigits(v: number): string {
  return String(Number(v.toPrecision(12)))
}

const DECIMAL = /^[+-]?\d+(\.\d+)?$/
const DECIMAL_EXPONENT = /^[+-]?\d*\.?\d+[eE][+-]?\d+$/

/** One authored decimal held to the grammar (an optional sign, digits, an
 * optional fraction) and to the engine's digit bound, in its canonical
 * spelling: no `+`, no leading zeros, no negative zero, and the scale kept,
 * so "19.90" stays "19.90" (`vocabulary.CanonicalDecimal`). */
export function canonicalDecimal(text: string): {
  digits?: string
  error?: string
} {
  const tooLong = {
    error: `a decimal holds at most ${MAX_DECIMAL_DIGITS} digits`,
  }
  if (text.length > MAX_DECIMAL_DIGITS + 2) return tooLong
  if (!DECIMAL.test(text)) {
    return {
      error: DECIMAL_EXPONENT.test(text)
        ? "a decimal writes its digits out, without an exponent"
        : 'expected a decimal ("19.99"): an optional sign, digits, an optional fraction',
    }
  }
  const neg = text.startsWith("-")
  const [whole, frac] = text.replace(/^[+-]/, "").split(".")
  const integer = whole.replace(/^0+/, "") || "0"
  if (integer.length + (frac?.length ?? 0) > MAX_DECIMAL_DIGITS) return tooLong
  const out = `${integer}${frac === undefined ? "" : `.${frac}`}`
  return { digits: neg && /[1-9]/.test(out) ? `-${out}` : out }
}

/** The order of two canonical decimals, exactly: negative, zero or
 * positive as `a` is below, equal to or above `b`. */
export function compareDecimal(a: string, b: string): number {
  const sign = (s: string) => (s.startsWith("-") ? -1 : /[1-9]/.test(s) ? 1 : 0)
  const sa = sign(a)
  const sb = sign(b)
  if (sa !== sb) return sa - sb
  const [ia, fa = ""] = a.replace(/^-/, "").split(".")
  const [ib, fb = ""] = b.replace(/^-/, "").split(".")
  let c = ia.length - ib.length || (ia < ib ? -1 : ia > ib ? 1 : 0)
  if (!c) {
    const n = Math.max(fa.length, fb.length)
    const x = fa.padEnd(n, "0")
    const y = fb.padEnd(n, "0")
    c = x < y ? -1 : x > y ? 1 : 0
  }
  return sa < 0 ? -c : c
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

const KEYS = new Set(["value", "unit", "comparator"])

/** Whether a value holds the stored shape: digits as a decimal string, a
 * non-empty unit, no key but value, unit and comparator, and a comparator
 * from the four. Whether the unit is one the repository holds, and what a
 * declaration narrows, is a reader's `check`. */
export function isQuantity(value: unknown): value is Quantity {
  if (!isRecord(value)) return false
  if (!Object.keys(value).every((key) => KEYS.has(key))) return false
  if (typeof value.value !== "string" || canonicalDecimal(value.value).error) {
    return false
  }
  if (typeof value.unit !== "string" || !value.unit.trim()) return false
  const c = value.comparator
  return c === undefined || c === null || COMPARATORS.includes(c as Comparator)
}

/** A declared bound or default read off the declaration: a quantity value
 * whose digits may have been authored as a number. */
export function readQuantity(raw: unknown): Quantity | undefined {
  if (!isRecord(raw) || typeof raw.unit !== "string" || !raw.unit) {
    return undefined
  }
  const written =
    typeof raw.value === "number" && Number.isFinite(raw.value)
      ? String(raw.value)
      : raw.value
  if (typeof written !== "string") return undefined
  const { digits } = canonicalDecimal(written)
  return digits === undefined ? undefined : { value: digits, unit: raw.unit }
}

/** Each time unit a reader takes in hours and minutes, by its length. A
 * millisecond is not among them: a heart's variability reads in ms. */
const TIME_UNITS: Record<string, number> = {
  s: 1000,
  min: 60_000,
  h: 3_600_000,
  d: 86_400_000,
}

/** A time of a minute or more in a time unit, in words, the way a
 * duration property reads (`4h 42m 36s` for `282.6 min`); undefined for
 * any other unit, a negative time, or one under a minute, which reads as
 * its digits (`0.35 s`). */
export function timeWords(digits: string, unit: string): string | undefined {
  if (!isTimeUnit(unit)) return undefined
  const per = TIME_UNITS[unit]
  const ms = Number(digits) * per
  if (!Number.isFinite(ms) || ms < 60_000) return undefined
  return durationWords(ms)
}

/** Whether a unit's times read in hours and minutes (`timeWords`). */
export function isTimeUnit(unit: string): boolean {
  return Object.hasOwn(TIME_UNITS, unit)
}

/** Decimal digits with the whole part in groups of three, as a count is
 * shown: `18194.61` is `18,194.61`, `-1234` is `-1,234`. Digits of any
 * other shape come back as they are. */
export function groupDigits(digits: string): string {
  const m = /^(-?)(\d+)(\.\d+)?$/.exec(digits)
  if (!m) return digits
  const [, sign, whole, rest = ""] = m
  return `${sign}${whole.replace(/\B(?=(\d{3})+(?!\d))/g, ",")}${rest}`
}

/** Decimal digits cut to `precision` places for display, rounded half
 * away from zero on the digits themselves (a binary float turns `1.005`
 * into `1.00`); digits with no more places than that come back as they
 * are, so a stored `72.40` keeps its zero, and a value rounded to zero has
 * no sign. The exact digits stay on the wire, in the copied table and on
 * hover. */
export function roundDigits(value: string, precision: number): string {
  const m = /^(-?)(\d+)\.(\d+)$/.exec(value)
  if (!m || m[3].length <= precision) return value
  const [, sign, whole, places] = m
  const digits = (whole + places.slice(0, precision)).split("").map(Number)
  if (Number(places[precision]) >= 5) {
    let i = digits.length - 1
    while (i >= 0 && digits[i] === 9) digits[i--] = 0
    if (i < 0) digits.unshift(1)
    else digits[i] += 1
  }
  const text = digits.join("")
  const cut = text.length - precision
  const out = precision ? `${text.slice(0, cut)}.${text.slice(cut)}` : text
  return sign && /[1-9]/.test(out) ? `-${out}` : out
}

/** The places a quantity is shown to outside Technical details where
 * nothing declares its own (a kind's quantity property declares none; a
 * metric's `displayPrecision` does). */
export const DISPLAY_PLACES = 2

/** Decimal digits as a reader sees them outside Technical details: cut to
 * `places`, grouped in threes. A value under one keeps its first two
 * significant digits however many places that takes, so `0.004` and
 * `0.014` never read as `0.00` and `0.01`. */
export function displayDigits(
  digits: string,
  places: number = DISPLAY_PLACES
): string {
  const lead = /^-?0\.(0*)[1-9]/.exec(digits)
  const shown = lead ? Math.max(places, lead[1].length + 2) : places
  return groupDigits(roundDigits(digits, shown))
}

/** A stored value as the exact text an editor holds, the unit as its id so
 * it reads back to the same value: `72.5 kg`, `<0.5 ng/mL`, `37 Cel`. Not a
 * quantity: "". */
export function formatQuantityText(value: unknown): string {
  if (!isQuantity(value)) return ""
  return `${value.comparator ?? ""}${value.value} ${value.unit}`
}

const LEADING_COMPARATOR = /^(<=|>=|≤|≥|<|>)\s*/
const NUMBER = /^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?/

/** Read the text an editor holds, or a person types, back into the value:
 * `72.5 kg`, `<0.5 ng/mL`, `37 °C`, `62 bpm`. The digits are held to the
 * decimal grammar and the unit is read through `canonical`, a reader's
 * spelling-to-id (lib/unit-reader.ts). */
export function parseQuantityText(
  text: string,
  canonical: (input: string) => string
): {
  value?: Quantity
  error?: string
} {
  let rest = text.trim()
  if (!rest) {
    return { error: `expected a number and a unit (${QUANTITY_TEXT_EXAMPLE})` }
  }
  let comparator: Comparator | undefined
  const cmp = LEADING_COMPARATOR.exec(rest)
  if (cmp) {
    comparator = (
      cmp[1] === "≤" ? "<=" : cmp[1] === "≥" ? ">=" : cmp[1]
    ) as Comparator
    rest = rest.slice(cmp[0].length)
  }
  const number = NUMBER.exec(rest)
  if (!number) {
    return {
      error: `expected a number before the unit (${QUANTITY_TEXT_EXAMPLE})`,
    }
  }
  const after = rest.slice(number[0].length)
  if (/^[\d.,]/.test(after)) return { error: "expected a number like 72.5" }
  const decimal = canonicalDecimal(number[0])
  if (decimal.error) return { error: decimal.error }
  const written = after.trim()
  if (!written) return { error: "choose a unit" }
  const unit = canonical(written)
  if (/\s/.test(unit)) {
    return {
      error: `"${written}" is not a unit: a unit is one spelling without spaces (kg, lb, mmol/L)`,
    }
  }
  return {
    value: {
      value: decimal.digits!,
      unit,
      ...(comparator ? { comparator } : {}),
    },
  }
}

const LAST_UNIT = "substrate.console.lastUnit"

/** The unit this reader picked last, which a blank quantity input opens on
 * where its declaration says nothing closer; `known` holds it to the units
 * the reader in use holds. */
export function lastUnit(known: (unit: string) => boolean): string | undefined {
  try {
    const code = localStorage.getItem(LAST_UNIT) ?? ""
    return known(code) ? code : undefined
  } catch {
    return undefined
  }
}

export function rememberUnit(
  code: string,
  known: (unit: string) => boolean
): void {
  try {
    if (known(code)) localStorage.setItem(LAST_UNIT, code)
  } catch {
    // Storage denied: the next blank input opens on the declaration alone.
  }
}
