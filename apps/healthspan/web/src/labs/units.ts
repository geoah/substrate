// How the labs convert a value: through the repository's unit graph
// (`GET /api/v1/units`, docs/units.md) for ordinary conversions, and
// through an analyte record's molar mass or formula between a mass and an
// amount concentration (or HbA1c's two scales), which no unit conversion
// joins. A unit the graph does not hold falls back to lib/units' table.

import { factor } from "../lib/units"

export interface GraphUnit {
  id: string
  symbol?: string
  /** The smallest id of its linear component: two units with one root
   * convert exactly. */
  root: string
  /** root = value x factor + offset. */
  factor: number
  offset: number
}

export interface UnitGraph {
  units: ReadonlyMap<string, GraphUnit>
  /** A unit as a lab writes it (its code or an alias) to its id. Symbols
   * are left out: annotated units share them (`mmol/L` for glucose). */
  names?: ReadonlyMap<string, string>
}

export const EMPTY_GRAPH: UnitGraph = { units: new Map() }

// The characters a spelling is normalized by, as the server's
// vocabulary.UnitSpellingKey does (internal/vocabulary/unitgraph.go): both
// micro signs, which look alike and are two code points, and the
// superscript digits.
const UNIT_CHARACTERS: Record<string, string> = {
  "\u00b5": "u", // MICRO SIGN
  "\u03bc": "u", // GREEK SMALL LETTER MU
  "\u00b2": "2",
  "\u00b3": "3",
}

/** Whether the text before a superscript run ends in a component that is
 * the bare number 10, whose power the run is (`10³/μL` is `10*3/uL`). */
function tenEnds(before: string[]): boolean {
  const n = before.length
  if (n < 2 || before[n - 2] !== "1" || before[n - 1] !== "0") return false
  return n === 2 || "./(".includes(before[n - 3])
}

// Go's strings.TrimSpace trims Unicode's White_Space set, which holds
// U+0085 and not U+FEFF; String.prototype.trim does the opposite on both.
const EDGE_SPACE = /^\p{White_Space}+|\p{White_Space}+$/gu

/** The one key the server matches a stored unit spelling by: trimmed, both
 * micro signs as `u`, the superscript digits as ASCII, and a superscript
 * power of a bare 10 as UCUM's `10*`. The server stores a spelling it
 * resolves this way normalized, so a row written `kg/m²` reads `kg/m2`. */
export function unitSpellingKey(s: string): string {
  const runes = [...s.replace(EDGE_SPACE, "")]
  let out = ""
  runes.forEach((r, i) => {
    const to = UNIT_CHARACTERS[r]
    if (to === undefined) {
      out += r
      return
    }
    if ((r === "\u00b2" || r === "\u00b3") && tenEnds(runes.slice(0, i)))
      out += "*"
    out += to
  })
  return out
}

/** The unit a written name stands for: its id, else the id whose code or
 * alias it is, each tried as written and then by its spelling key. */
function unitOf(graph: UnitGraph, name: string): GraphUnit | undefined {
  const byName = (n: string) =>
    graph.units.get(n) ?? graph.units.get(graph.names?.get(n) ?? "")
  return byName(name) ?? byName(unitSpellingKey(name))
}

/** The id of the unit a written name stands for, or undefined where the
 * graph holds none. */
export function unitId(graph: UnitGraph, name: string): string | undefined {
  return unitOf(graph, name)?.id
}

/** An exact rational as the server writes one, digits or a fraction. */
export function parseRational(v: unknown): number | undefined {
  if (typeof v === "number") return Number.isFinite(v) ? v : undefined
  if (typeof v !== "string" || !v.trim()) return undefined
  const slash = v.indexOf("/")
  if (slash > 0) {
    const n = Number(v.slice(0, slash))
    const d = Number(v.slice(slash + 1))
    return Number.isFinite(n) && Number.isFinite(d) && d !== 0
      ? n / d
      : undefined
  }
  const n = Number(v)
  return Number.isFinite(n) ? n : undefined
}

/** `GET /api/v1/units`'s answer as a graph; anything malformed left out. */
export function parseUnitGraph(answer: unknown): UnitGraph {
  const list =
    answer && typeof answer === "object" && "units" in answer
      ? answer.units
      : undefined
  const units = new Map<string, GraphUnit>()
  const names = new Map<string, string>()
  if (!Array.isArray(list)) return { units, names }
  for (const raw of list) {
    if (!raw || typeof raw !== "object") continue
    const u = raw as Record<string, unknown>
    const f = parseRational(u.factor)
    if (typeof u.id !== "string" || typeof u.root !== "string" || !f) continue
    units.set(u.id, {
      id: u.id,
      symbol: typeof u.symbol === "string" ? u.symbol : undefined,
      root: u.root,
      factor: f,
      offset: parseRational(u.offset) ?? 0,
    })
    for (const name of [
      u.id,
      u.code,
      ...(Array.isArray(u.aliases) ? (u.aliases as unknown[]) : []),
    ])
      if (typeof name === "string" && name)
        for (const n of new Set([name, unitSpellingKey(name)]))
          if (n && !names.has(n)) names.set(n, u.id)
  }
  return { units, names }
}

/** `value` in `from` expressed in `to`, or undefined when no ordinary
 * conversion joins them. Equal units convert as they are. */
export function convert(
  graph: UnitGraph,
  value: number,
  from: string | undefined,
  to: string | undefined
): number | undefined {
  if (!from || !to || from === to) return from === to ? value : undefined
  const a = unitOf(graph, from)
  const b = unitOf(graph, to)
  if (a && b)
    return a.root === b.root
      ? (value * a.factor + a.offset - b.offset) / b.factor
      : undefined
  const f = factor(from, to)
  return f === undefined ? undefined : value * f
}

export function convertible(
  graph: UnitGraph,
  from: string | undefined,
  to: string | undefined
): boolean {
  return convert(graph, 1, from, to) !== undefined
}

/** One conversion an analyte record names. */
export interface AnalyteConversion {
  /** The target metric's id. */
  target: string
  formula: string
  parameters: Record<string, number>
}

/** A `labs/analyte` record as the labs read it. */
export interface Analyte {
  id: string
  /** The metric it is about, by id. */
  metric: string
  /** Grams per mole. */
  molarMass?: number
  conversions: AnalyteConversion[]
}

/** `value` in `unit` (of the analyte's metric) converted through one of its
 * conversions into `targetUnit` (the target metric's unit, or one that
 * converts to it); undefined where the formula is unknown or a unit does
 * not fit it. A mass concentration goes through g/L and an amount one
 * through mol/L, so any unit of either converts. */
export function throughAnalyte(
  graph: UnitGraph,
  value: number,
  unit: string | undefined,
  analyte: Pick<Analyte, "molarMass">,
  conversion: Pick<AnalyteConversion, "formula" | "parameters">,
  targetUnit: string | undefined
): number | undefined {
  const p = conversion.parameters
  switch (conversion.formula) {
    case "molar-mass": {
      const mm = analyte.molarMass
      if (!mm) return undefined
      const mass = convert(graph, value, unit, "g/L")
      if (mass !== undefined)
        return convert(graph, mass / mm, "mol/L", targetUnit)
      const amount = convert(graph, value, unit, "mol/L")
      if (amount !== undefined)
        return convert(graph, amount * mm, "g/L", targetUnit)
      return undefined
    }
    case "ngsp-to-ifcc": {
      const ngsp = unit === "percent" ? value : undefined
      if (ngsp === undefined || !p.slope) return undefined
      const ifcc = (ngsp - (p.intercept ?? 0)) * p.slope
      return convert(graph, ifcc, "mmol/mol", targetUnit)
    }
    case "ifcc-to-ngsp": {
      const ifcc = unit === "mmol/mol" ? value : undefined
      if (ifcc === undefined || !p.slope) return undefined
      const ngsp = ifcc / p.slope + (p.intercept ?? 0)
      return convert(graph, ngsp, "percent", targetUnit)
    }
    default:
      return undefined
  }
}
