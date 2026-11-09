// A table's rows read through the owner's column roles and matched: each
// row's date, test, value, range and flag, its entry and its metric, why it
// is left out where it is, and whether the repository may hold it already.

import type { CatalogueEntry, MetricRecord } from "../catalogue/types"
import type { ColumnRole } from "./columns"
import { normalise } from "./columns"
import type { Table } from "./csv"
import {
  candidateMetrics,
  findEntry,
  linearCache,
  mayBeHeld,
  pickMetric,
  type Convert,
  type HeldResult,
} from "./match"
import {
  instantOf,
  normaliseUnit,
  parseDate,
  parseFlag,
  parseRange,
  parseValue,
  type Comparator,
  type DayOrder,
  type ParsedRange,
} from "./values"

/** One row of the file, read and matched. */
export interface ResolvedRow {
  /** Its line in the file, the headers being line 1. */
  line: number
  name?: string
  loinc?: string
  lab?: string
  accession?: string
  at?: string
  utcOffset?: number
  day?: string
  value?: string
  comparator?: Comparator
  unit?: string
  text?: string
  range: ParsedRange
  flag?: string
  entry?: CatalogueEntry
  metric?: MetricRecord
  /** Its value in its metric's unit, for the "may already be here"
   * compare. */
  inMetricUnit?: number
  /** Why it is left out; absent for a row that can be written. */
  problem?: string
  /** No entry or metric knows its name: the owner may pick one. */
  unmatched?: boolean
  /** The repository may hold it already: left unticked. */
  held?: boolean
}

/** The cell of the first column with `role`. */
function cell(
  row: string[],
  roles: ColumnRole[],
  role: ColumnRole
): string | undefined {
  const i = roles.indexOf(role)
  const v = i < 0 ? undefined : row[i]?.trim()
  return v ? v : undefined
}

/** The rows as values, before matching: what each cell says, and the
 * problems that need no metric to find. */
export function readRows(
  table: Table,
  roles: ColumnRole[],
  order: DayOrder,
  defaultLab: string
): ResolvedRow[] {
  return table.rows.map((r, i) => {
    const out: ResolvedRow = { line: i + 2, range: {} }
    out.name = cell(r, roles, "name")
    out.loinc = cell(r, roles, "loinc")
    out.lab = cell(r, roles, "lab") ?? (defaultLab.trim() || undefined)
    out.accession = cell(r, roles, "accession")
    const date = cell(r, roles, "date")
    const parsed = date ? parseDate(date, order) : undefined
    if (parsed) {
      const t = instantOf(parsed)
      out.at = t.at
      out.utcOffset = t.utcOffset
      out.day = t.day
    }
    const v = parseValue(cell(r, roles, "value") ?? "")
    if (v.kind === "number") {
      out.value = v.value
      out.comparator = v.comparator
      out.unit = normaliseUnit(cell(r, roles, "unit") ?? "") ?? v.unit
    } else if (v.kind === "text") {
      out.text = v.text
    }
    const range = cell(r, roles, "range")
    out.range = range ? parseRange(range) : {}
    const low = cell(r, roles, "low")
    const high = cell(r, roles, "high")
    if (low !== undefined || high !== undefined) {
      const lo = low === undefined ? undefined : parseRange(`>=${low}`).low
      const hi = high === undefined ? undefined : parseRange(`<=${high}`).high
      out.range = {
        ...(lo !== undefined ? { low: lo } : {}),
        ...(hi !== undefined ? { high: hi } : {}),
      }
    }
    out.flag = parseFlag(cell(r, roles, "flag") ?? "")
    if (!date) out.problem = "It has no date."
    else if (!parsed) out.problem = `Its date, ${date}, is not one this reads.`
    else if (!out.name && !out.loinc) out.problem = "It names no test."
    else if (v.kind === "empty") out.problem = "It has no value."
    else if (!out.lab) out.problem = "It names no lab."
    return out
  })
}

/** What matching reads besides the rows. */
export interface MatchInput {
  catalogue: readonly CatalogueEntry[]
  /** The repository's lab and body metrics. */
  metrics: readonly MetricRecord[]
  /** The owner's pick of a metric for a name nothing matched, by the
   * name, normalised. */
  picks: ReadonlyMap<string, string>
  convert: Convert
  /** The results the repository holds for these metrics over these days. */
  held: (metrics: string[], days: string[]) => Promise<HeldResult[]>
}

/** The key the owner's picks are kept under for a row's name. */
export function pickKey(row: { name?: string; loinc?: string }): string {
  return normalise(row.name ?? row.loinc ?? "")
}

/** Every row matched to an entry and a metric, the "may already be here"
 * mark set from one read of what the repository holds. */
export async function matchRows(
  rows: ResolvedRow[],
  input: MatchInput
): Promise<ResolvedRow[]> {
  const linear = linearCache(input.convert)
  const out = await Promise.all(
    rows.map(async (row): Promise<ResolvedRow> => {
      if (row.problem) return row
      const entry = findEntry(row, input.catalogue)
      const pickedId = input.picks.get(pickKey(row))
      const picked = pickedId
        ? input.metrics.find((m) => m.id === pickedId)
        : undefined
      const candidates = candidateMetrics(
        row,
        entry,
        input.catalogue,
        input.metrics,
        picked
      )
      if (!candidates.length)
        return {
          ...row,
          entry,
          unmatched: true,
          problem: entry
            ? `The repository has no metric for ${entry.name}.`
            : "Nothing in the catalogue or the repository knows this test.",
        }
      const pick = await pickMetric(
        { value: row.value, unit: row.unit, text: row.text !== undefined },
        candidates,
        linear
      )
      if ("reason" in pick)
        return { ...row, entry, problem: pick.reason, unmatched: !entry }
      return {
        ...row,
        entry,
        metric: pick.metric,
        unit: row.text !== undefined ? undefined : pick.unit,
        inMetricUnit: pick.inMetricUnit,
      }
    })
  )
  const matched = out.filter((r) => r.metric && r.day)
  if (!matched.length) return out
  const held = await input.held(
    [...new Set(matched.map((r) => r.metric!.id))],
    [...new Set(matched.map((r) => r.day!))]
  )
  if (!held.length) return out
  return Promise.all(
    out.map(async (r) => {
      if (!r.metric || !r.day) return r
      const dup = await mayBeHeld(
        {
          metric: r.metric,
          day: r.day,
          inMetricUnit: r.inMetricUnit,
          text: r.text,
        },
        held,
        linear
      )
      return dup ? { ...r, held: true } : r
    })
  )
}

/** Whether a row is ticked at first: one that can be written and is not
 * marked as maybe already here. */
export function tickedAtFirst(r: ResolvedRow): boolean {
  return !r.problem && !r.held
}
