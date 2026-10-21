/** The repository's unit graph, client-side (decision 0186): the unit and
 * conversion records `GET /api/v1/units` answers, read into one value that
 * resolves a spelling to a unit, prints a unit's symbol, and converts between
 * two units of one linear component through their exact maps to its root.
 *
 * The server sends each unit with its root and its exact map to it
 * (`root = value * factor + offset`), so two units convert when they share a
 * root, by composing two maps, the way the engine does. Nothing here builds
 * a component: the server's answer is the graph. An expression conversion
 * joins two components for one value only, and only the server evaluates
 * it, so `convert` answers undefined there and a caller that needs it asks
 * `GET /api/v1/units/convert` (lib/api/units.ts).
 *
 * This is the console's second unit path: until the server's switch is on
 * (`units.graph` in discovery) lib/units.ts reads the engine's table, and
 * hooks/use-unit-reader.ts picks between the two readers of
 * lib/unit-reader.ts. */

import type {
  Quantity,
  QuantityDeclaration,
  UnitGroup,
  Comparator,
} from "@/lib/units"
import { canonicalDecimal } from "@/lib/units"

import type {
  RepositoryConversion,
  RepositoryUnit,
  UnitTable,
} from "./api/types"

/** An exact rational, numerator over a positive denominator. */
export interface Rational {
  n: bigint
  d: bigint
}

function gcd(a: bigint, b: bigint): bigint {
  let x = a < 0n ? -a : a
  let y = b < 0n ? -b : b
  while (y) [x, y] = [y, x % y]
  return x || 1n
}

function norm(n: bigint, d: bigint): Rational {
  if (d < 0n) [n, d] = [-n, -d]
  const g = gcd(n, d)
  return { n: n / g, d: d / g }
}

/** Digits or `n/d` (each part may carry decimals), as the server writes a
 * factor, an offset or a turn; undefined for anything else. */
export function parseRational(text: string | undefined): Rational | undefined {
  if (!text) return undefined
  const m = /^(-?\d+(?:\.\d+)?)(?:\/(\d+(?:\.\d+)?))?$/.exec(text.trim())
  if (!m) return undefined
  const part = (s: string): Rational => {
    const neg = s.startsWith("-")
    const [int, frac = ""] = s.replace("-", "").split(".")
    const n = BigInt(int + frac) * (neg ? -1n : 1n)
    return norm(n, 10n ** BigInt(frac.length))
  }
  const a = part(m[1])
  if (m[2] === undefined) return a
  const b = part(m[2])
  if (b.n === 0n) return undefined
  return norm(a.n * b.d, a.d * b.n)
}

const add = (a: Rational, b: Rational) => norm(a.n * b.d + b.n * a.d, a.d * b.d)
const sub = (a: Rational, b: Rational) => norm(a.n * b.d - b.n * a.d, a.d * b.d)
const mul = (a: Rational, b: Rational) => norm(a.n * b.n, a.d * b.d)
const div = (a: Rational, b: Rational) => norm(a.n * b.d, a.d * b.n)
const ZERO: Rational = { n: 0n, d: 1n }
const ONE: Rational = { n: 1n, d: 1n }

/** A rational as a float, at twelve significant places, which clears a
 * binary float's noise as lib/units.ts convertUnit does. */
export function rationalNumber(r: Rational): number {
  return Number((Number(r.n) / Number(r.d)).toPrecision(12))
}

/** -1, 0 or 1 as a is below, equal to or above b. */
export function compareRational(a: Rational, b: Rational): number {
  const x = a.n * b.d
  const y = b.n * a.d
  return x < y ? -1 : x > y ? 1 : 0
}

/** The spelling forms the server finds a unit under: as written, and with
 * both micro signs as `u` and the superscript digits as ASCII
 * (vocabulary.UnitSpellingKey). */
export function spellingKey(s: string): string {
  return s.trim().replace(/[µμ]/g, "u").replace(/²/g, "2").replace(/³/g, "3")
}

const ANGLE_SYMBOLS = new Set(["°", "′", "″", "'", "''"])

interface GraphUnit {
  record: RepositoryUnit
  factor: Rational
  offset: Rational
}

export class UnitGraph {
  readonly revision: number
  private readonly units = new Map<string, GraphUnit>()
  /** Spelling tiers, ids first, then codes, then aliases, each holding the
   * spelling as written and as its key. */
  private readonly tiers = [0, 1, 2].map(() => new Map<string, string>())
  private readonly byRoot = new Map<string, string[]>()
  readonly conversions: readonly RepositoryConversion[]

  constructor(table: Pick<UnitTable, "units" | "conversions" | "revision">) {
    this.revision = table.revision
    this.conversions = table.conversions
    for (const u of table.units) {
      this.units.set(u.id, {
        record: u,
        factor: parseRational(u.factor) ?? ONE,
        offset: parseRational(u.offset) ?? ZERO,
      })
      this.byRoot.set(u.root, [...(this.byRoot.get(u.root) ?? []), u.id])
      const index = (tier: number, s: string | undefined) => {
        if (!s) return
        for (const key of [s, spellingKey(s)]) {
          if (!this.tiers[tier].has(key)) this.tiers[tier].set(key, u.id)
        }
      }
      index(0, u.id)
      index(1, u.code)
      for (const a of u.aliases) index(2, a)
    }
    for (const members of this.byRoot.values()) members.sort()
  }

  /** The unit a writer's spelling names, as its id: an id, then a code,
   * then an alias, each also normalized. */
  resolve(spelling: string): string | undefined {
    const s = spelling.trim()
    for (const key of new Set([s, spellingKey(s)])) {
      for (const tier of this.tiers) {
        const id = tier.get(key)
        if (id !== undefined) return id
      }
    }
    return undefined
  }

  unit(id: string): RepositoryUnit | undefined {
    return this.units.get(id)?.record
  }

  /** Every unit id, in id order. */
  ids(): string[] {
    return [...this.units.keys()].sort()
  }

  root(id: string): string | undefined {
    return this.units.get(id)?.record.root
  }

  /** The units of a unit's linear component, in id order. */
  component(id: string): string[] {
    const root = this.root(id)
    return root === undefined ? [] : [...(this.byRoot.get(root) ?? [])]
  }

  /** What a title prints after a value in the unit: its symbol, the id for a
   * unit that declares none (the server sends the id then), the spelling
   * itself for one the graph does not hold. */
  symbol(spelling: string): string {
    const id = this.resolve(spelling)
    return id === undefined ? spelling : (this.unit(id)?.symbol ?? id)
  }

  /** The exact map between two units of one linear component,
   * `to = from * factor + offset`, a difference without the offset;
   * undefined where no linear route joins them. */
  linear(
    from: string,
    to: string,
    delta = false
  ): { factor: Rational; offset: Rational } | undefined {
    const f = this.units.get(this.resolve(from) ?? "")
    const t = this.units.get(this.resolve(to) ?? "")
    if (!f || !t || f.record.root !== t.record.root) return undefined
    const factor = div(f.factor, t.factor)
    const offset = delta ? ZERO : div(sub(f.offset, t.offset), t.factor)
    return { factor, offset }
  }

  /** Whether two spellings convert exactly, through linear conversions. */
  converts(from: string, to: string): boolean {
    return this.linear(from, to) !== undefined
  }

  /** A value exactly in another unit of its linear component; undefined
   * where none joins them. */
  convertExact(
    value: Rational,
    from: string,
    to: string,
    delta = false
  ): Rational | undefined {
    const m = this.linear(from, to, delta)
    return m ? add(mul(value, m.factor), m.offset) : undefined
  }

  /** A value in one unit as the same quantity in another, as a float for a
   * chart or a comparison; undefined across components, an expression route
   * included, which only the server evaluates. */
  convert(
    value: number,
    from: string,
    to: string,
    delta = false
  ): number | undefined {
    if (from === to) return value
    const r = parseRational(String(value))
    if (!r) {
      const m = this.linear(from, to, delta)
      if (!m) return undefined
      return Number(
        (
          value * rationalNumber(m.factor) +
          Number(m.offset.n) / Number(m.offset.d)
        ).toPrecision(12)
      )
    }
    const out = this.convertExact(r, from, to, delta)
    return out ? rationalNumber(out) : undefined
  }

  /** The id a person's spelling stores, or the spelling as typed for the
   * server to refuse. */
  canonical(input: string): string {
    return this.resolve(input) ?? input.trim()
  }

  /** A stored quantity as a title prints it: "72.5 kg", "90°". */
  formatQuantity(value: unknown): string {
    if (!isQuantityShape(value)) return ""
    const cmp = value.comparator ?? ""
    const symbol = this.symbol(value.unit)
    if (!symbol) return cmp + value.value
    if (ANGLE_SYMBOLS.has(symbol)) return cmp + value.value + symbol
    return `${cmp}${value.value} ${symbol}`
  }

  /** The units a declaration admits, as the picker groups them: the anchor
   * and its listed `units`, else the anchor's component, else every
   * component, each under its root's name. */
  unitsFor(spec: QuantityDeclaration = {}): UnitGroup[] {
    const anchor = spec.unit ? this.resolve(spec.unit) : undefined
    if (spec.units?.length) {
      const listed = spec.units.map((u) => this.resolve(u) ?? u)
      const all = anchor
        ? [anchor, ...listed.filter((u) => u !== anchor)]
        : listed
      return [{ units: all }]
    }
    if (anchor)
      return [{ measure: this.label(anchor), units: this.component(anchor) }]
    const roots = [...this.byRoot.keys()].sort()
    return roots.map((root) => ({
      measure: this.label(root),
      units: [...(this.byRoot.get(root) ?? [])],
    }))
  }

  private label(root: string): string {
    const r = this.units.get(this.root(root) ?? root)?.record
    return r?.name || root
  }

  /** The units a reading in `stored` may be shown in: every unit of its
   * linear component, the stored one first; the stored spelling alone where
   * the graph does not hold it. */
  unitChoices(stored: string | undefined): string[] {
    if (!stored) return []
    const id = this.resolve(stored)
    if (id === undefined) return [stored]
    return [id, ...this.component(id).filter((u) => u !== id)]
  }

  /** The server's refusal of a quantity value under a declaration, word for
   * word where it can be, or undefined when the value is admissible: the
   * shape, then the unit through the graph, the anchor, the listed units
   * and the bounds, compared exactly. */
  checkQuantity(
    value: unknown,
    spec: QuantityDeclaration = {}
  ): string | undefined {
    if (!value || typeof value !== "object" || Array.isArray(value)) {
      return 'a quantity value is an object {value: "72.5", unit: "kg"}'
    }
    const v = value as Record<string, unknown>
    for (const key of Object.keys(v)) {
      if (key !== "value" && key !== "unit" && key !== "comparator") {
        return `a quantity value holds value, unit and comparator, and "${key}" is none of them`
      }
    }
    if (typeof v.value !== "string") {
      return 'value is written as a string ("72.5"): a bare JSON number rides float64 and may already be rounded'
    }
    const decimal = canonicalDecimal(v.value)
    if (decimal.error) return `value: ${decimal.error}`
    const written = typeof v.unit === "string" ? v.unit : ""
    if (!written)
      return "a quantity value needs unit, a unit of this repository (kg, Cel, mmol/L)"
    const id = this.resolve(written)
    if (id === undefined)
      return `unit: "${written}" names no unit of this repository`
    const anchor = spec.unit ? this.resolve(spec.unit) : undefined
    if (anchor !== undefined && !this.converts(id, anchor)) {
      return `unit: ${id} does not convert to ${anchor}, the declaration's unit`
    }
    if (spec.units?.length && id !== anchor) {
      const listed = spec.units.map((u) => this.resolve(u) ?? u)
      if (!listed.includes(id)) {
        const all = anchor ? [anchor, ...listed] : listed
        return `unit: ${id} is not one of the declared units ${all.join(", ")}`
      }
    }
    if (v.comparator !== undefined && v.comparator !== null) {
      if (!spec.comparator) {
        return "comparator: this property states values exactly; it admits a comparator only where it declares `comparator: true`"
      }
      if (
        !(["<", "<=", ">=", ">"] as Comparator[]).includes(
          v.comparator as Comparator
        )
      ) {
        return "comparator is one of <, <=, >=, >"
      }
    }
    const digits = parseRational(decimal.digits)!
    for (const [bound, below] of [
      [spec.minQuantity, true],
      [spec.maxQuantity, false],
    ] as const) {
      if (!bound) continue
      const b = parseRational(bound.value)
      const at = b && this.convertExact(digits, id, bound.unit, spec.delta)
      const text = this.formatQuantity(bound)
      if (!b || !at)
        return `cannot compare with ${below ? "min" : "max"} ${text}`
      const cmp = compareRational(at, b)
      if (below && cmp < 0) return `must be >= ${text}`
      if (!below && cmp > 0) return `must be <= ${text}`
    }
    return undefined
  }
}

function isQuantityShape(value: unknown): value is Quantity {
  if (!value || typeof value !== "object") return false
  const v = value as Record<string, unknown>
  return (
    typeof v.value === "string" &&
    v.value !== "" &&
    typeof v.unit === "string" &&
    v.unit !== ""
  )
}
