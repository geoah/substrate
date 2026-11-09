// A reported Randox order's tests as rows: each test once, in the panel that
// first prints it, with the bands of every panel that prints it, the band
// its result fell in as its flag, and its specimen and fasting
// (apps/healthspan/docs/importers/02-randox.md, The results).

import {
  analyteKey,
  FLAG_WORDS,
  flagOfTonedBands,
  isNumber,
  parseIso,
  splitComparator,
  strip,
  type Band,
  type LabRow,
  type WallClock,
} from "../shared"
import type { RandoxReferenceRange } from "./files"
import { PERSONAL_PANEL, testAnalyte, URINE_TESTS } from "./tests"

/** The coding system a result's test name is written under. */
export const RANDOX_CODING = "randox-health"

const TONES: Readonly<Record<string, "good" | "fair" | "poor">> = {
  G: "good",
  A: "fair",
  R: "poor",
}

/** One-sided operators: the side the bound is on, and whether it is
 * inclusive. */
const ONE_SIDED: Readonly<Record<number, ["low" | "high", boolean]>> = {
  1: ["high", false],
  2: ["high", true],
  3: ["low", false],
  4: ["low", true],
}

const CODED = new Set(["negative", "not detected", "positive", "detected"])

/** A test's bands from its reference range, in the portal's `sortOrder`:
 * each with the portal's own label, its colour as a tone (red where it
 * has none the importer knows), whether the result fell in it, and the
 * range's unit; a number on each side between two bounds, one bound with
 * its strictness for a one-sided band, a criterion for a coded band (a
 * urine strip's `Trace`), the label alone for a band with no bounds. A
 * band with neither a bound nor a label is dropped. */
export function randoxBands(
  rr: RandoxReferenceRange | null | undefined
): Band[] {
  if (!rr) return []
  const out: Band[] = []
  const ranges = [...(rr.ranges ?? [])]
    .map((r, i) => ({ r, i }))
    .sort((a, b) => (a.r.sortOrder ?? 0) - (b.r.sortOrder ?? 0) || a.i - b.i)
    .map((x) => x.r)
  for (const r of ranges) {
    const word = strip(r.description ?? "")
    const v1 = strip(r.value1 ?? "")
    const v2 = strip(r.value2 ?? "")
    const symbol = strip(r.statusColourSymbol ?? "").toUpperCase()
    const base: Band = {
      lab: word,
      tone: TONES[symbol] ?? "poor",
      matched: !!r.matchedRange,
      unit: rr.unit ?? null,
    }
    const op = r.operator
    const side = op === null || op === undefined ? undefined : ONE_SIDED[op]
    if (op === 0 && isNumber(v1) && isNumber(v2))
      out.push({ ...base, low: v1, high: v2 })
    else if (side && isNumber(v1))
      out.push({
        ...base,
        low: side[0] === "low" ? v1 : null,
        high: side[0] === "high" ? v1 : null,
        inclusive: side[1],
      })
    else if (v1)
      out.push({
        ...base,
        criterion: v1,
        expected: CODED.has(v1.toLowerCase()) ? v1.toLowerCase() : null,
      })
    else if (word) out.push(base)
  }
  return out
}

/** One panel's printing of a test. */
export interface Occurrence {
  panel: string
  /** The result as displayed, stripped. */
  result: string
  /** The reference range's unit as printed. */
  unit?: string | null
  bands: Band[]
}

/** A row of the order, with the analyte it is compared by against the
 * body composition sheet. */
export interface RandoxRow extends LabRow {
  name: string
}

/** What a reported order's rows are made with. */
export interface OrderFacts {
  sample: string
  /** Whether the sample was taken fasting: the fast began at least eight
   * hours before the sample. */
  fasted: boolean
  /** The programme the order is part of, as the portal names it. */
  programme?: string | null
}

/** How a test was measured: at the clinic for the personal measurements,
 * else in the clinic under the order's programme. */
export function methodOf(personal: boolean, programme?: string | null) {
  if (personal) return "measured at the clinic"
  const p = strip(programme ?? "")
  return p ? `${p} programme, in clinic` : "in clinic"
}

/** The rows of one order: `tests` maps each test's name to every panel
 * that prints it, in report order; the first is the test's own panel and
 * the others add their bands, marked as another panel's. */
export function randoxRows(
  tests: ReadonlyMap<string, readonly Occurrence[]>,
  order: OrderFacts
): RandoxRow[] {
  const rows: RandoxRow[] = []
  for (const [name, occurrences] of tests) {
    const first = occurrences[0]
    const analyte = testAnalyte(name)
    const split = splitComparator(first.result)
    const numeric = isNumber(split.value)
    const bands: Band[] = occurrences.flatMap((o) =>
      o.bands.map((b) => ({
        ...b,
        panel: o.panel,
        primary: o.panel === first.panel,
      }))
    )
    const flag = flagOfTonedBands(bands.filter((b) => b.primary))
    const personal = first.panel === PERSONAL_PANEL
    const material =
      name.includes("(Urine)") || URINE_TESTS.has(name)
        ? "urine"
        : personal
          ? undefined
          : "blood"
    rows.push({
      name,
      key: `${order.sample}/${name}`,
      analyte,
      coding: { system: RANDOX_CODING, code: name, display: name },
      value: numeric ? split.value : null,
      comparator: numeric ? (split.comparator ?? null) : null,
      text: numeric ? null : first.result,
      unit: first.unit ?? null,
      bands,
      flag: flag ?? null,
      flagText: flag ? FLAG_WORDS[flag] : null,
      specimen: material ? { material, fasting: order.fasted } : null,
      method: methodOf(personal, order.programme),
      upstreamId: `${order.sample}/${name}`,
    })
  }
  return rows
}

/** A portal time, UK wall-clock with no offset and .NET's seven fraction
 * digits, as a wall clock: the text before its first `.`, read as ISO
 * 8601. Undefined where it is not one, or names an offset. */
export function portalTime(text: string): WallClock | undefined {
  const p = parseIso(text.split(".")[0])
  return p && p.offset === undefined ? p.wall : undefined
}

const naiveMs = (w: WallClock) =>
  Date.UTC(w.year, w.month - 1, w.day, w.hour, w.minute, w.second)

/** Whether a sample collected at `collected` was taken fasting from
 * `fastStart`: eight hours or more between the two wall clocks, read as
 * they are written (a clock change between them is not counted). */
export function fastedFor(collected: WallClock, fastStart: WallClock): boolean {
  return naiveMs(collected) - naiveMs(fastStart) >= 8 * 3_600_000
}

/** The analyte a row is compared by against the body composition sheet,
 * with its region. */
export function measureOf(row: LabRow): string | undefined {
  const a = analyteKey(row.analyte)
  return a && `${a}\u0000${row.channel ?? ""}`
}
