/** The `quantity` datatype, client-side: the engine's unit table, the
 * substrate's own checks on a stored value (`internal/engine/quantity.go`
 * coerceQuantity), and the text an editor holds.
 *
 * A quantity value is `{value: "72.5", unit: "kg"}`, with `comparator: "<"`
 * beside them where the property declares `comparator: true`. The value is
 * exact digits written as a string, as a decimal is; the unit is a
 * case-sensitive UCUM code, stored as the code whatever alias was typed.
 *
 * - `formatQuantityText` is the EXACT text an editor holds, `72.5 kg`,
 *   `37 Cel`, and `parseQuantityText` reads it back to the same value.
 * - `formatQuantity` is the display, the server's title form: `37 °C`.
 *
 * The table is `units.table.json`, which a Go test writes from the engine's
 * own table (`TestConsoleUnitsMatchTheEngine`), so it is imported and never
 * edited. It carries the print rules too, so `unitSymbol` renders a compound
 * the table does not list (`GJ`, `kcal/(kg.h)`) as the engine's titles do,
 * held to the engine's own answers by `units.prints.json`. The console does
 * not judge such a code: whether it belongs to a measure is the server's. */

import { durationWords } from "@/lib/observations-time"

import table from "./units.table.json"

export interface Measure {
  name: string
  /** The code every unit of the measure converts to. */
  base: string
  /** How the measure converts: `ratio`, `affine`, `difference`, `count`,
   * `logarithmic` or `arbitrary`. */
  scale: string
  /** The unit a reader is shown by default. */
  display: string
  dimension: string
  /** The listed codes, in the table's order. */
  units: string[]
  /** The codes in classes the server converts a value between: two codes
   * convert exactly when one class holds both. */
  converts: string[][]
  /** Each code's exact map to the base, `base = value * factor + offset`,
   * as rationals (`1/1000`); a code no rational factor reaches is absent. */
  factors?: Partial<Record<string, { factor: string; offset?: string }>>
  note?: string
}

export type Comparator = "<" | "<=" | ">=" | ">"

export interface Quantity {
  value: string
  unit: string
  comparator?: Comparator
}

/** What a quantity property declares beyond its datatype, as `PropSpec`
 * carries it. */
export interface QuantityDeclaration {
  measures?: string
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

/** The picker's units under one measure; `measure` is absent where the
 * declaration lists units and names no measure. */
export interface UnitGroup {
  measure?: string
  units: string[]
}

export const MEASURES: readonly Measure[] = table.measures
export const COMPARATORS: readonly Comparator[] =
  table.comparators as Comparator[]

const SYMBOLS: Record<string, string> = table.symbols
const PRIMARY: Record<string, string> = table.primary
const ALIASES: Record<string, string> = table.aliases
/** The spellings the engine refuses by name (`count`). */
const REFUSED = new Set<string>(table.refused)
/** The print symbols a person may type for a listed code. None of them is a
 * code or an alias itself, so reading one never turns a stored code into
 * another (`pt` stays the picotonne). */
const SYMBOL_CODES: Record<string, string> = table.symbolCodes

const BY_NAME = new Map(MEASURES.map((m) => [m.name, m]))

/** The measures listing each code, in table order. */
const LISTED_IN = new Map<string, string[]>()
for (const m of MEASURES) {
  for (const code of m.units) {
    LISTED_IN.set(code, [...(LISTED_IN.get(code) ?? []), m.name])
  }
}

/** The worked value, as the text an editor takes. */
export const QUANTITY_TEXT_EXAMPLE = "72.5 kg"

/** A measure by its name, or undefined. */
export function lookupMeasure(name: string | undefined): Measure | undefined {
  return name ? BY_NAME.get(name) : undefined
}

/** Whether the table lists a code under any measure. */
export function isListedUnit(code: string): boolean {
  return Object.hasOwn(SYMBOLS, code)
}

/** A rational the table writes as `n/d` or `n`, as a number. */
function rational(text: string | undefined): number {
  if (!text) return 0
  const [n, d] = text.split("/")
  return Number(n) / (d === undefined ? 1 : Number(d))
}

/** A float's digits at twelve significant places, which clears a binary
 * float's noise: `7099.999999999999` is `7100`, `0.1 + 0.2` is `0.3`. */
export function floatDigits(v: number): string {
  return String(Number(v.toPrecision(12)))
}

/** A value in one code as the same quantity in another, for a chart or a
 * comparison: the two codes must share a converts class of one measure
 * (`measure` where the value names one, else the code's own,
 * `measureOf`), and the answer is a float, never digits to store.
 * Undefined where the two do not convert (mg/dL and mmol/L are two
 * measures). */
export function convertUnit(
  value: number,
  from: string,
  to: string,
  measure?: string
): number | undefined {
  if (from === to) return value
  const m = lookupMeasure(measure ?? measureOf(from))
  if (!m?.converts.some((c) => c.includes(from) && c.includes(to))) {
    return undefined
  }
  const f = m.factors?.[from]
  const t = m.factors?.[to]
  if (!f || !t) return undefined
  const base = value * rational(f.factor) + rational(f.offset)
  // Twelve significant places clear a binary float's noise: 23.4 kg is
  // 23400 g, not 23399.999999999996.
  return Number(
    ((base - rational(t.offset)) / rational(t.factor)).toPrecision(12)
  )
}

/** A measure's name as a heading: `temperature-difference` is "Temperature
 * difference". */
export function measureLabel(name: string): string {
  const words = name.replaceAll("-", " ")
  return words.charAt(0).toUpperCase() + words.slice(1)
}

/** The measure a listed code belongs to when nothing declares one: the one
 * measure listing it, or for a code two measures list (`Cel`, `1`) the one
 * the engine reads it as. An unlisted code is the server's to place. */
export function measureOf(code: string): string | undefined {
  if (Object.hasOwn(PRIMARY, code)) return PRIMARY[code]
  const names = LISTED_IN.get(code)
  return names?.length === 1 ? names[0] : undefined
}

const ANGLES = new Set(["deg", "'", "''"])
const SUPERSCRIPT: Record<string, string> = {
  "0": "⁰",
  "1": "¹",
  "2": "²",
  "3": "³",
  "4": "⁴",
  "5": "⁵",
  "6": "⁶",
  "7": "⁷",
  "8": "⁸",
  "9": "⁹",
  "-": "⁻",
}

const PRINT = table.print
const PREFIX_PRINTS: Record<string, string> = PRINT.prefixes
/** The prefixes longest first, so `da` is tried before `d`. */
const PREFIXES = Object.keys(PREFIX_PRINTS).sort((a, b) => b.length - a.length)
const ATOM_PRINTS: Record<string, string> = PRINT.atoms
const PRINTS: Record<string, string> = PRINT.prints
const WHOLES = new Set(PRINT.wholes)
const IDENTITY_NOTES = new Set(PRINT.annotations)
const POWERS_OF_TEN = new Set(["10*", "10^"])

type PrintNode =
  | { kind: "unit"; prefix: string; atom: string; exp?: number; note?: string }
  | { kind: "number"; digits: string; note?: string }
  | { kind: "note"; text: string }
  | { kind: "group"; term: PrintTerm }

interface PrintTerm {
  reciprocal: boolean
  parts: PrintNode[]
  ops: string[]
}

/** A code read into the components the engine's print renderer walks
 * (`internal/vocabulary/unitsgrammar.go`), or undefined where an atom is not
 * one the table carries. It holds nothing to the grammar's bounds: a stored
 * code already passed them. */
function readPrintTerm(code: string): PrintTerm | undefined {
  let i = 0
  const annotation = (): string | undefined => {
    const end = code.indexOf("}", i + 1)
    if (end < 0) return undefined
    const text = code.slice(i + 1, end)
    i = end + 1
    return text
  }
  const term = (): PrintTerm | undefined => {
    const t: PrintTerm = { reciprocal: false, parts: [], ops: [] }
    for (;;) {
      const part = component()
      if (!part) return undefined
      t.parts.push(part)
      const c = code[i]
      if (c !== "." && c !== "/") return t
      t.ops.push(c)
      i++
    }
  }
  const component = (): PrintNode | undefined => {
    const c = code[i]
    if (c === "(") {
      i++
      const inner = term()
      if (!inner || code[i] !== ")") return undefined
      i++
      return { kind: "group", term: inner }
    }
    if (c === "{") {
      const text = annotation()
      return text === undefined ? undefined : { kind: "note", text }
    }
    const start = i
    let depth = 0
    while (i < code.length) {
      const ch = code[i]
      if (depth === 0 && ".()/{}".includes(ch)) break
      if (ch === "[") depth++
      else if (ch === "]") depth--
      i++
    }
    const symbol = code.slice(start, i)
    if (!symbol || depth !== 0) return undefined
    let note: string | undefined
    if (code[i] === "{") {
      note = annotation()
      if (note === undefined) return undefined
    }
    if (/^\d+$/.test(symbol)) return { kind: "number", digits: symbol, note }
    const powered = /^(.*?[^\d+-])([+-]?\d+)$/.exec(symbol)
    const head = powered ? powered[1] : symbol
    const exp = powered ? Number(powered[2]) : undefined
    if (Object.hasOwn(ATOM_PRINTS, head)) {
      return { kind: "unit", prefix: "", atom: head, exp, note }
    }
    for (const prefix of PREFIXES) {
      const atom = head.slice(prefix.length)
      if (head.startsWith(prefix) && atom && Object.hasOwn(ATOM_PRINTS, atom)) {
        return { kind: "unit", prefix, atom, exp, note }
      }
    }
    return undefined
  }
  if (code[0] === "/") i++
  const t = term()
  if (!t || i !== code.length) return undefined
  t.reciprocal = code[0] === "/"
  return t
}

function renderPrintTerm(t: PrintTerm): string {
  let out = t.reciprocal ? "/" : ""
  t.parts.forEach((part, n) => {
    if (n > 0) out += t.ops[n - 1] === "." ? "·" : "/"
    switch (part.kind) {
      case "unit":
        out += PREFIX_PRINTS[part.prefix] ?? ""
        out += ATOM_PRINTS[part.atom]
        if (part.exp !== undefined && part.exp !== 1) {
          out += [...String(part.exp)].map((c) => SUPERSCRIPT[c]).join("")
        }
        if (
          part.note !== undefined &&
          (POWERS_OF_TEN.has(part.atom) || IDENTITY_NOTES.has(part.note))
        ) {
          out += ` ${part.note}`
        }
        break
      case "number":
        out += part.digits
        if (part.note !== undefined) out += ` ${part.note}`
        break
      case "note":
        out += part.text
        break
      case "group":
        out += `(${renderPrintTerm(part.term)})`
        break
    }
  })
  return out
}

/** A code's print symbol, as the server's titles print it (`Unit.Print`):
 * `°C` for Cel, `μg` for ug, `kWh` for kW.h, `kvar` for kV.A{reactive},
 * `beats/min` for {beats}/min, `μL` for uL, nothing for `1`. A code naming an
 * atom the table does not carry prints as itself, and so does one whose
 * print is an alias of another code or a refused spelling (`{count}/min`,
 * never `count/min`, which reads as `/min`). */
export function unitSymbol(code: string): string {
  const symbol = printSymbol(code)
  const spellsAnother =
    (Object.hasOwn(ALIASES, symbol) && ALIASES[symbol] !== code) ||
    REFUSED.has(symbol)
  return spellsAnother ? code : symbol
}

function printSymbol(code: string): string {
  if (Object.hasOwn(PRINTS, code)) return PRINTS[code]
  if (Object.hasOwn(SYMBOLS, code)) return SYMBOLS[code]
  const term = readPrintTerm(code)
  if (!term) return code
  const first = term.parts[0]
  if (!term.reciprocal && first?.kind === "unit" && first.prefix) {
    const whole = code.slice(first.prefix.length)
    if (WHOLES.has(whole)) return PREFIX_PRINTS[first.prefix] + PRINTS[whole]
  }
  return renderPrintTerm(term)
}

/** A unit as a picker offers it: the symbol a reader knows, with the code
 * beside it where the two differ and `code` asks for it (an editor, which
 * writes the code, or Technical details): `°C (Cel)`, `lb ([lb_av])`. */
export function unitLabel(code: string, withCode = true): string {
  const symbol = unitSymbol(code)
  if (!symbol) return withCode ? `no unit (${code})` : "no unit"
  return symbol === code || !withCode ? symbol : `${symbol} (${code})`
}

/** The units a declaration admits, as the picker groups them: the listed
 * `units:`, else the declared measure's units, else every measure's, each
 * code under the measure it reads as undeclared so none is offered twice. */
export function unitsFor(spec: QuantityDeclaration = {}): UnitGroup[] {
  if (spec.units?.length) {
    return [{ measure: spec.measures, units: [...spec.units] }]
  }
  const declared = lookupMeasure(spec.measures)
  if (declared) return [{ measure: declared.name, units: [...declared.units] }]
  return MEASURES.map((m) => ({
    measure: m.name,
    units: m.units.filter((code) => measureOf(code) === m.name),
  })).filter((g) => g.units.length)
}

const MICRO = /[µμ]/g
const SUPERSCRIPT_DIGITS: Record<string, string> = { "²": "2", "³": "3" }

/** An input spelling as the engine stores it (`CanonicalUnit`): an alias
 * (`°C`, `bpm`, `mmHg`) to its code, both micro signs to `u` and the
 * superscript digits to ASCII (`μg/m³` is `ug/m3`), a superscript after a
 * bare 10 as its power (`10³/μL` is `10*3/uL`). */
function engineUnit(input: string): string {
  const s = input.trim()
  if (Object.hasOwn(ALIASES, s)) return ALIASES[s]
  const normalized = s
    .replace(MICRO, "u")
    .replace(/(^|[./(])10(?=[²³])/g, (_, before: string) => `${before}10*`)
    .replace(/[²³]/g, (c) => SUPERSCRIPT_DIGITS[c])
  return Object.hasOwn(ALIASES, normalized) ? ALIASES[normalized] : normalized
}

/** What a person types as the code a quantity stores: the engine's own
 * reading (aliases, micro signs, superscripts), and then a listed unit's
 * print symbol (`mph`, `ft³`) as its code where no code and no alias spells
 * it. The second step is the console's alone, so a value it sends always
 * carries a code; the server reads codes and aliases only. Anything else
 * comes back as typed, for the server to judge. */
export function canonicalUnit(input: string): string {
  const code = engineUnit(input)
  if (Object.hasOwn(SYMBOLS, code)) return code
  const typed = input.trim()
  return Object.hasOwn(SYMBOL_CODES, typed) ? SYMBOL_CODES[typed] : code
}

const DECIMAL = /^[+-]?\d+(\.\d+)?$/
const DECIMAL_EXPONENT = /^[+-]?\d*\.?\d+[eE][+-]?\d+$/

const MAX_DECIMAL_DIGITS: number = table.maxDecimalDigits

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

function orList(names: string[]): string {
  return names.length < 2
    ? (names[0] ?? "")
    : `${names.slice(0, -1).join(", ")} or ${names.at(-1)}`
}

const KEYS = new Set(["value", "unit", "comparator"])

/** The substrate's refusal of a quantity value, word for word where it can
 * be, or undefined when the value is admissible. A unit the table lists is
 * held to the declared `measures:` and `units:`; one it does not list is the
 * server's to place. A bound is checked only where the value is in the
 * bound's own unit: across units the comparison needs the engine's exact
 * conversion, which the server holds. */
export function checkQuantity(
  value: unknown,
  spec: QuantityDeclaration = {}
): string | undefined {
  if (!isRecord(value)) {
    return 'a quantity value is an object {value: "72.5", unit: "kg"}'
  }
  for (const key of Object.keys(value)) {
    if (!KEYS.has(key)) {
      return `a quantity value holds value, unit and comparator, and "${key}" is none of them`
    }
  }
  const raw = value.value
  if (raw === undefined || raw === null) {
    return 'a quantity value needs value, its digits as a string ("72.5")'
  }
  if (typeof raw !== "string") {
    return 'value is written as a string ("72.5"): a bare JSON number rides float64 and may already be rounded'
  }
  const decimal = canonicalDecimal(raw)
  if (decimal.error) return `value: ${decimal.error}`
  const unit = typeof value.unit === "string" ? engineUnit(value.unit) : ""
  if (!unit) return "a quantity value needs unit, a UCUM code (kg, Cel, mmol/L)"
  const listed = LISTED_IN.get(unit)
  if (listed && spec.measures && lookupMeasure(spec.measures)) {
    if (!listed.includes(spec.measures)) {
      return `unit: ${unit} measures ${orList(listed)}, not ${spec.measures}`
    }
  }
  if (spec.units?.length && !spec.units.includes(unit)) {
    return `unit: ${unit} is not one of the declared units ${spec.units.join(", ")}`
  }
  const comparator = value.comparator
  if (comparator !== undefined && comparator !== null) {
    if (!spec.comparator) {
      return "comparator: this property states values exactly; it admits a comparator only where it declares `comparator: true`"
    }
    if (!COMPARATORS.includes(comparator as Comparator)) {
      return `comparator is one of ${COMPARATORS.join(", ")}`
    }
  }
  const digits = decimal.digits!
  const min = spec.minQuantity
  if (min && min.unit === unit && compareDecimal(digits, min.value) < 0) {
    return `must be >= ${formatQuantity(min)}`
  }
  const max = spec.maxQuantity
  if (max && max.unit === unit && compareDecimal(digits, max.value) > 0) {
    return `must be <= ${formatQuantity(max)}`
  }
  return undefined
}

/** Whether a value holds the stored shape. A declaration's narrowing is not
 * part of the shape; `checkQuantity` holds that. */
export function isQuantity(value: unknown): value is Quantity {
  return (
    isRecord(value) && checkQuantity(value, { comparator: true }) === undefined
  )
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

/** A stored value as a title shows it: the comparator, the digits as stored
 * and the unit's print symbol, `72.5 kg`, `37 °C`, `<0.5 ng/mL`, `8000 steps`;
 * an angle sits against the number (`90°`) and `1` prints the digits alone
 * (`vocabulary.FormatQuantity`). Not a quantity: "". */
export function formatQuantity(value: unknown): string {
  if (!isQuantity(value)) return ""
  return withSymbol(`${value.comparator ?? ""}${value.value}`, value.unit)
}

/** Digits already written for a reader, and the unit's print symbol: an
 * angle against the number, no unit's symbol nothing at all. */
export function withSymbol(head: string, unit: string): string {
  const symbol = unitSymbol(unit)
  if (!symbol) return head
  if (ANGLES.has(unit)) return `${head}${symbol}`
  return `${head} ${symbol}`
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

/** A stored value as a reader sees it outside Technical details: the
 * digits as `displayDigits` gives them, with the unit's print symbol,
 * `97.18 %`, `18,194.61 m`, and a time of a minute or more in words
 * (`timeWords`); the stored digits stay on hover. A value with a
 * comparator is a bound, `<0.005 ng/mL`, and keeps its every digit. Not a
 * quantity: "". */
export function displayQuantity(
  value: unknown,
  places: number = DISPLAY_PLACES
): string {
  if (!isQuantity(value)) return ""
  if (value.comparator) {
    return withSymbol(
      `${value.comparator}${groupDigits(value.value)}`,
      value.unit
    )
  }
  const time = timeWords(value.value, value.unit)
  if (time) return time
  return withSymbol(displayDigits(value.value, places), value.unit)
}

/** A stored value as the exact text an editor holds, the unit as its code so
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
 * decimal grammar and the unit is read through `canonical`, the table's
 * `canonicalUnit` unless a reader names its own (lib/unit-reader.ts). */
export function parseQuantityText(
  text: string,
  canonical: (input: string) => string = canonicalUnit
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
      error: `"${written}" is not a unit: a unit is one code without spaces (kg, [lb_av], mmol/L)`,
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

/** The worked value for a declaration, in the YAML a document carries: the
 * declared default, else the unit a blank value opens on. */
export function quantityExample(spec: QuantityDeclaration = {}): string {
  const held = readQuantity(spec.default)
  const unit = held?.unit ?? (openingUnit(spec, "") || "kg")
  const yaml = /^[A-Za-z0-9./]+$/.test(unit) ? unit : JSON.stringify(unit)
  return `{value: "${held?.value ?? "72.5"}", unit: ${yaml}}`
}

/** The worked value for a declaration, as the text an editor takes. */
export function quantityTextExample(spec: QuantityDeclaration = {}): string {
  const held = readQuantity(spec.default)
  if (held) return formatQuantityText(held)
  const unit = openingUnit(spec, "")
  return unit ? `72.5 ${unit}` : QUANTITY_TEXT_EXAMPLE
}

/** The unit a blank value opens on: the declared default's, else the
 * declared measure's display unit where the declaration admits it, else the
 * unit picked last where the declaration admits it, else the first listed
 * unit; "" where nothing says. */
export function openingUnit(
  spec: QuantityDeclaration = {},
  last: string = lastUnit() ?? ""
): string {
  const held = readQuantity(spec.default)
  if (held) return held.unit
  const admitted = unitsFor(spec).flatMap((g) => g.units)
  const display = lookupMeasure(spec.measures)?.display
  if (display && admitted.includes(display)) return display
  if (last && admitted.includes(last)) return last
  return spec.units?.[0] ?? ""
}

const LAST_UNIT = "substrate.console.lastUnit"

/** The unit this reader picked last, which a blank quantity input opens on
 * where its declaration says nothing closer; `known` holds it to the units
 * the reader in use holds, the table's unless a reader names its own. */
export function lastUnit(
  known: (unit: string) => boolean = isListedUnit
): string | undefined {
  try {
    const code = localStorage.getItem(LAST_UNIT) ?? ""
    return known(code) ? code : undefined
  } catch {
    return undefined
  }
}

export function rememberUnit(
  code: string,
  known: (unit: string) => boolean = isListedUnit
): void {
  try {
    if (known(code)) localStorage.setItem(LAST_UNIT, code)
  } catch {
    // Storage denied: the next blank input opens on the declaration alone.
  }
}
