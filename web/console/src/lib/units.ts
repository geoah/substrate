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
 * edited. The console cannot parse UCUM: the grammar admits compounds the
 * table does not list (`GJ`, `kcal/(kg.h)`), and those are shown as written
 * and left to the server to judge. */

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

export const MEASURES: readonly Measure[] = table.measures as Measure[]
export const COMPARATORS: readonly Comparator[] =
  table.comparators as Comparator[]

const SYMBOLS: Record<string, string> = table.symbols
const PRIMARY: Record<string, string> = table.primary
const ALIASES: Record<string, string> = table.aliases

const BY_NAME = new Map(MEASURES.map((m) => [m.name, m]))

/** The measures listing each code, in table order. */
const LISTED_IN = new Map<string, string[]>()
for (const m of MEASURES) {
  for (const code of m.units) {
    LISTED_IN.set(code, [...(LISTED_IN.get(code) ?? []), m.name])
  }
}

/** A print symbol to the one listed code it prints for. A symbol two codes
 * share, or one that is itself a listed code, reads as nothing here. */
const BY_SYMBOL = new Map<string, string>()
{
  const shared = new Set<string>()
  for (const [code, symbol] of Object.entries(SYMBOLS)) {
    if (!symbol || symbol === code) continue
    if (BY_SYMBOL.has(symbol)) shared.add(symbol)
    BY_SYMBOL.set(symbol, code)
  }
  for (const symbol of shared) BY_SYMBOL.delete(symbol)
  for (const symbol of [...BY_SYMBOL.keys()]) {
    if (Object.hasOwn(SYMBOLS, symbol)) BY_SYMBOL.delete(symbol)
  }
}

/** The worked value, in the YAML a document carries. */
export const QUANTITY_EXAMPLE = '{value: "72.5", unit: kg}'

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

/** One component of a compound code as the engine prints it, or undefined
 * where only the grammar could say. An annotation alone prints as its text
 * (`{beats}`), one beside a listed unit is dropped (`mL{total}`), digits stay
 * digits, and a listed unit raised to a power wears it in superscript. */
function componentSymbol(part: string): string | undefined {
  if (/^\d+$/.test(part)) return part
  if (Object.hasOwn(SYMBOLS, part)) return SYMBOLS[part]
  const note = /^\{([^{}]*)\}$/.exec(part)
  if (note) return note[1]
  const annotated = /^(.+?)\{[^{}]*\}$/.exec(part)
  if (annotated && Object.hasOwn(SYMBOLS, annotated[1])) {
    return SYMBOLS[annotated[1]]
  }
  const powered = /^(.*[^\d-])(-?\d+)$/.exec(part)
  if (powered && Object.hasOwn(SYMBOLS, powered[1])) {
    const exp = [...powered[2]].map((c) => SUPERSCRIPT[c]).join("")
    return `${SYMBOLS[powered[1]]}${exp}`
  }
  return undefined
}

/** A code's print symbol, as the server's titles print it: `°C` for Cel, `μg`
 * for ug, `kWh` for kW.h, `beats/min` for {beats}/min, nothing for `1`. A code
 * the console cannot read (a group, a power of ten) prints as itself. */
export function unitSymbol(code: string): string {
  if (Object.hasOwn(SYMBOLS, code)) return SYMBOLS[code]
  // Split on the top-level `.` and `/`, never inside brackets or braces.
  const parts: string[] = []
  const ops: string[] = []
  let depth = 0
  let start = 0
  for (let i = 0; i < code.length; i++) {
    const c = code[i]
    if (c === "(") return code
    if (c === "[" || c === "{") depth++
    else if (c === "]" || c === "}") depth--
    else if (depth === 0 && (c === "." || c === "/")) {
      if (i === 0 && c === "/") {
        ops.push("/")
        start = 1
        continue
      }
      parts.push(code.slice(start, i))
      ops.push(c)
      start = i + 1
    }
  }
  parts.push(code.slice(start))
  const printed = parts.map(componentSymbol)
  if (printed.some((p) => p === undefined || p === "")) return code
  const lead = code.startsWith("/") ? "/" : ""
  const rest = lead ? ops.slice(1) : ops
  return (
    lead +
    printed
      .map((p, i) => (i === 0 ? p : `${rest[i - 1] === "." ? "·" : "/"}${p}`))
      .join("")
  )
}

/** A unit as the picker offers it: the symbol a reader knows, with the code
 * beside it where the two differ (`°C (Cel)`, `lb ([lb_av])`). */
export function unitLabel(code: string): string {
  const symbol = unitSymbol(code)
  if (!symbol) return `no unit (${code})`
  return symbol === code ? code : `${symbol} (${code})`
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
 * superscript digits to ASCII (`μg/m³` is `ug/m3`). */
function engineUnit(input: string): string {
  const s = input.trim()
  if (Object.hasOwn(ALIASES, s)) return ALIASES[s]
  const normalized = s
    .replace(MICRO, "u")
    .replace(/[²³]/g, (c) => SUPERSCRIPT_DIGITS[c])
  return Object.hasOwn(ALIASES, normalized) ? ALIASES[normalized] : normalized
}

/** What a person types as the code a quantity stores: the engine's own
 * reading (aliases, micro signs, superscripts), and then a listed unit's
 * print symbol (`lb`, `mph`, `ft³`) as its code. The second step is the
 * console's alone, so a value it sends always carries a code; the server
 * reads codes and aliases only. Anything else comes back as typed, for the
 * server to judge. */
export function canonicalUnit(input: string): string {
  const code = engineUnit(input)
  if (Object.hasOwn(SYMBOLS, code)) return code
  return BY_SYMBOL.get(input.trim()) ?? code
}

const DECIMAL = /^[+-]?\d+(\.\d+)?$/
const DECIMAL_EXPONENT = /^[+-]?\d*\.?\d+[eE][+-]?\d+$/

/** One authored decimal held to the grammar (an optional sign, digits, an
 * optional fraction) in its canonical spelling: no `+`, no leading zeros, no
 * negative zero, and the scale kept, so "19.90" stays "19.90"
 * (`vocabulary.CanonicalDecimal`). */
export function canonicalDecimal(text: string): {
  digits?: string
  error?: string
} {
  if (!DECIMAL.test(text)) {
    return {
      error: DECIMAL_EXPONENT.test(text)
        ? "a decimal writes its digits out, without an exponent"
        : 'expected a decimal ("19.99"): an optional sign, digits, an optional fraction',
    }
  }
  const neg = text.startsWith("-")
  const [whole, frac] = text.replace(/^[+-]/, "").split(".")
  const out = `${whole.replace(/^0+/, "") || "0"}${frac === undefined ? "" : `.${frac}`}`
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
  const head = `${value.comparator ?? ""}${value.value}`
  const symbol = unitSymbol(value.unit)
  if (!symbol) return head
  if (ANGLES.has(value.unit)) return `${head}${symbol}`
  return `${head} ${symbol}`
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
 * decimal grammar and the unit is read through `canonicalUnit`. */
export function parseQuantityText(text: string): {
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
  const unit = canonicalUnit(written)
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

/** The unit a stored value is in, or undefined. */
export function quantityUnit(value: unknown): string | undefined {
  return isQuantity(value) ? value.unit : undefined
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
 * where its declaration says nothing closer. */
export function lastUnit(): string | undefined {
  try {
    const code = localStorage.getItem(LAST_UNIT) ?? ""
    return isListedUnit(code) ? code : undefined
  } catch {
    return undefined
  }
}

export function rememberUnit(code: string): void {
  try {
    if (isListedUnit(code)) localStorage.setItem(LAST_UNIT, code)
  } catch {
    // Storage denied: the next blank input opens on the declaration alone.
  }
}
