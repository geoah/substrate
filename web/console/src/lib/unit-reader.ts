/** The unit operations the console's components and calculations call, and
 * the three readers that answer them (decision 0186):
 *
 * - `TABLE_UNITS`, lib/units.ts's copy of the engine's table, on a server
 *   whose `units.graph` switch is off;
 * - `graphUnits(graph)`, the repository's own graph, on a server whose
 *   switch is on;
 * - `pendingUnits(status)`, on a server whose switch is on while its graph
 *   loads or after it failed. It answers no conversion and admits no value,
 *   and prints a unit as it is stored: on a graph-enabled server the table's
 *   answers are wrong answers (another factor, a missing unit), so none of
 *   them stands in for the graph.
 *
 * A lib function that converts, prints a symbol, lists choices or checks a
 * quantity takes a reader; the component above it takes one from `useUnits`
 * (hooks/use-units.ts). */

import { everydayUnits, everydayUnitsBy, unitChoices } from "@/lib/unit-choices"
import { UnitGraph } from "@/lib/unit-graph"
import {
  QUANTITY_TEXT_EXAMPLE,
  canonicalUnit,
  checkQuantity,
  convertUnit,
  displayDigits,
  displayQuantity,
  formatQuantity,
  formatQuantityText,
  groupDigits,
  isQuantity,
  lastUnit,
  measureOf,
  openingUnit,
  parseQuantityText,
  quantityExample,
  quantityTextExample,
  readQuantity,
  rememberUnit,
  timeWords,
  unitLabel,
  unitSymbol,
  unitsFor,
  withSymbol,
  DISPLAY_PLACES,
  type Quantity,
  type QuantityDeclaration,
  type UnitGroup,
} from "@/lib/units"

/** Which reader answers: the engine's table (`table`), the repository's
 * graph (`ready`), or neither yet (`loading`) or at all (`error`). */
export type UnitStatus = "table" | "loading" | "ready" | "error"

/** What a converted value is, beyond its unit: `delta` for a difference,
 * and the measure an observed row was stamped with before the switch,
 * which the table reads (`temperature-difference`). */
export interface UnitReading {
  delta?: boolean
  measure?: string
}

export interface UnitReader {
  status: UnitStatus
  /** Why the graph did not load, under `error`. */
  error?: Error
  graph?: UnitGraph
  /** The unit's print symbol, as the server's titles print it. */
  symbol(unit: string): string
  /** A unit as a picker offers it: its symbol, with the spelling a write
   * stores beside it where the two differ and `withCode` asks for it. */
  label(unit: string, withCode?: boolean): string
  /** The spelling a write stores for what a person typed. */
  canonical(input: string): string
  /** A value in one unit as a float in another; undefined where the two do
   * not convert. */
  convert(
    value: number,
    from: string,
    to: string,
    reading?: UnitReading
  ): number | undefined
  /** The units a declaration admits, grouped as the picker shows them. */
  unitsFor(spec?: QuantityDeclaration): UnitGroup[]
  /** The server's refusal of a quantity value, or undefined. */
  check(value: unknown, spec?: QuantityDeclaration): string | undefined
  /** The units a reading stored in `stored` may be shown in. */
  choices(stored: string | undefined, measures?: string): string[]
  /** The everyday ones of `units` for a reading stored in `stored`. */
  everyday(
    units: readonly string[],
    stored: string | undefined,
    picked?: string
  ): string[]
  /** A stored value as a title prints it (`37 °C`); "" for no quantity. */
  format(value: unknown): string
  /** A stored value as a reader sees it outside Technical details. */
  display(value: unknown, places?: number): string
  /** Digits already written for a reader, and the unit's symbol. */
  withSymbol(head: string, unit: string): string
  /** The editor text (`72.5 kg`) read back into a value. */
  parse(text: string): { value?: Quantity; error?: string }
  /** The unit a blank value of a declaration opens on, "" for none. */
  opening(spec?: QuantityDeclaration): string
  /** The worked value for a declaration, in YAML. */
  example(spec?: QuantityDeclaration): string
  /** The worked value for a declaration, as editor text. */
  textExample(spec?: QuantityDeclaration): string
  /** Keep the unit a person picked, which the next blank input opens on. */
  remember(unit: string): void
  /** Whether a value in the unit is an angle on a circle, which the server
   * averages with a circular mean: the table's `angle` measure, or under
   * the graph a unit of a component declaring a turn. */
  circular(unit: string): boolean
}

/** The engine's table, for a server whose switch is off. */
export const TABLE_UNITS: UnitReader = {
  status: "table",
  symbol: unitSymbol,
  label: unitLabel,
  canonical: canonicalUnit,
  // A row stamped before the switch names its measure; an unstamped
  // difference converts as the engine's ObservedMeasureFor reads it.
  convert: (value, from, to, reading) =>
    convertUnit(
      value,
      from,
      to,
      reading?.measure ??
        (reading?.delta && measureOf(from) === "temperature"
          ? "temperature-difference"
          : undefined)
    ),
  unitsFor,
  check: checkQuantity,
  choices: unitChoices,
  everyday: everydayUnits,
  format: formatQuantity,
  display: displayQuantity,
  withSymbol,
  parse: (text) => parseQuantityText(text),
  opening: (spec) => openingUnit(spec),
  example: quantityExample,
  textExample: quantityTextExample,
  remember: rememberUnit,
  circular: (unit) => measureOf(canonicalUnit(unit)) === "angle",
}

const ANGLE_SYMBOLS = new Set(["°", "′", "″", "'", "''"])

/** The reader over a graph, `status` naming which graph it is: the
 * repository's (`ready`) or the empty one a pending reader answers from. */
function overGraph(
  graph: UnitGraph,
  status: UnitStatus,
  error?: Error
): UnitReader {
  const attach = (head: string, unit: string): string => {
    const symbol = graph.symbol(unit)
    if (!symbol) return head
    return ANGLE_SYMBOLS.has(symbol) ? `${head}${symbol}` : `${head} ${symbol}`
  }
  const known = (unit: string) => graph.resolve(unit) !== undefined
  const opening = (spec: QuantityDeclaration = {}): string => {
    const held = readQuantity(spec.default)
    if (held) return graph.canonical(held.unit)
    const admitted = graph.unitsFor(spec).flatMap((g) => g.units)
    const anchor = spec.unit ? graph.resolve(spec.unit) : undefined
    if (anchor && admitted.includes(anchor)) return anchor
    const last = lastUnit(known)
    if (last && admitted.includes(last)) return last
    const first = spec.units?.[0]
    return first ? graph.canonical(first) : ""
  }
  return {
    status,
    ...(error ? { error } : {}),
    ...(status === "ready" ? { graph } : {}),
    symbol: (unit) => graph.symbol(unit),
    label: (unit, withCode = true) => {
      const symbol = graph.symbol(unit)
      if (!symbol) return withCode ? `no unit (${unit})` : "no unit"
      return symbol === unit || !withCode ? symbol : `${symbol} (${unit})`
    },
    canonical: (input) => graph.canonical(input),
    convert: (value, from, to, reading) =>
      graph.convert(value, from, to, reading?.delta ?? false),
    unitsFor: (spec) => graph.unitsFor(spec),
    check: (value, spec) => graph.checkQuantity(value, spec),
    choices: (stored) => graph.unitChoices(stored),
    everyday: (units, stored, picked) =>
      everydayUnitsBy(units, stored, picked, (u) => graph.canonical(u)),
    format: (value) => graph.formatQuantity(value),
    display: (value, places = DISPLAY_PLACES) => {
      if (!isQuantity(value)) return ""
      if (value.comparator) {
        return attach(
          `${value.comparator}${groupDigits(value.value)}`,
          value.unit
        )
      }
      const time = timeWords(value.value, value.unit)
      if (time) return time
      return attach(displayDigits(value.value, places), value.unit)
    },
    withSymbol: attach,
    parse: (text) => parseQuantityText(text, (s) => graph.canonical(s)),
    opening,
    example: (spec = {}) => {
      const held = readQuantity(spec.default)
      const unit = held?.unit ?? (opening(spec) || "kg")
      const yaml = /^[A-Za-z0-9./]+$/.test(unit) ? unit : JSON.stringify(unit)
      return `{value: "${held?.value ?? "72.5"}", unit: ${yaml}}`
    },
    textExample: (spec = {}) => {
      const held = readQuantity(spec.default)
      if (held) return formatQuantityText(held)
      const unit = opening(spec)
      return unit ? `72.5 ${unit}` : QUANTITY_TEXT_EXAMPLE
    },
    remember: (unit) => {
      if (known(unit)) rememberUnit(graph.canonical(unit), known)
    },
    circular: (unit) => {
      const id = graph.resolve(unit)
      return (
        id !== undefined &&
        graph.component(id).some((u) => graph.unit(u)?.turn !== undefined)
      )
    },
  }
}

/** The reader over the repository's graph, for a server whose switch is
 * on. */
export function graphUnits(graph: UnitGraph): UnitReader {
  return overGraph(graph, "ready")
}

const NO_UNITS = new UnitGraph({ units: [], conversions: [], revision: 0 })

/** The reader while a graph-enabled server's graph loads, or after it
 * failed: an empty graph, so a unit prints as stored, nothing converts but
 * a unit to itself, nothing is offered, and every value is refused with
 * the reason. */
export function pendingUnits(
  status: "loading" | "error",
  error?: Error
): UnitReader {
  const reason =
    status === "loading"
      ? "the repository's units are still loading"
      : `the repository's units did not load${error?.message ? `: ${error.message}` : ""}`
  return { ...overGraph(NO_UNITS, status, error), check: () => reason }
}

/** What a component shows in place of a unit answer it cannot give yet;
 * undefined where the reader answers. */
export function unitsUnavailable(units: UnitReader): string | undefined {
  if (units.status === "loading") return "The repository's units are loading"
  if (units.status === "error") {
    return `The repository's units did not load${units.error?.message ? `: ${units.error.message}` : ""}`
  }
  return undefined
}
