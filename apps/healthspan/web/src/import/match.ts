// A file's row to a catalogue entry, then to a metric of the repository
// (docs/plans/healthspan.md, Import, and The catalogue: Importing): the
// entry by a LOINC code where the row has one, else by its names; its
// metrics are those holding one of its codes, then the metric it names;
// the metric is the first whose unit the row's unit converts to. A row no
// entry knows is matched to a metric by the same rules, so a file reads
// against a catalogue still being filled; one nothing knows waits for the
// owner's pick of a metric.

import { matchEntry, metricsOf } from "../catalogue/resolve"
import type { CatalogueEntry, MetricRecord } from "../catalogue/types"
import { normalise } from "./columns"

const LOINC = "http://loinc.org"

/** The LOINC codes a metric holds. */
export function loincOf(m: MetricRecord): string[] {
  return m.codes
    .filter((c) => c.system === LOINC || /loinc/i.test(c.system))
    .map((c) => c.code)
}

/** A metric's name without a trailing parenthesis naming its form
 * (`LDL cholesterol (mass)`). */
function stem(name: string): string {
  return normalise(name.replace(/\s*\([^)]*\)\s*$/, ""))
}

/** The entry a row names: by its LOINC code, else by the entry's name, an
 * alias or its own key, compared without case or punctuation (the
 * catalogue's own match). */
export function findEntry(
  row: { name?: string; loinc?: string },
  catalogue: readonly CatalogueEntry[]
): CatalogueEntry | undefined {
  return matchEntry(catalogue, row)
}

/** An entry's metrics in the repository, as the catalogue claims them (a
 * metric belongs to the first entry holding one of its codes): the metric
 * the entry names first, then in the order of the entry's codes, the
 * display form's first. */
export function entryMetrics(
  entry: CatalogueEntry,
  catalogue: readonly CatalogueEntry[],
  metrics: readonly MetricRecord[]
): MetricRecord[] {
  const rank = (m: MetricRecord) => {
    if (m.id === entry.metric) return -1
    const at = loincOf(m)
      .map((c) => entry.loinc.indexOf(c))
      .filter((i) => i >= 0)
    return at.length ? Math.min(...at) : entry.loinc.length
  }
  return metricsOf(entry, catalogue, metrics).sort((a, b) => rank(a) - rank(b))
}

/** The metrics a row may be written under, best first: the owner's pick
 * alone; else its entry's, the one holding the row's own code first; else
 * the metrics holding its code, then those of its name in any form. */
export function candidateMetrics(
  row: { name?: string; loinc?: string },
  entry: CatalogueEntry | undefined,
  catalogue: readonly CatalogueEntry[],
  metrics: readonly MetricRecord[],
  picked?: MetricRecord
): MetricRecord[] {
  if (picked) return [picked]
  const code = row.loinc?.trim()
  const byCode = code ? metrics.filter((m) => loincOf(m).includes(code)) : []
  if (entry) {
    const own = entryMetrics(entry, catalogue, metrics)
    return [...byCode.filter((m) => own.includes(m)), ...own].filter(
      (m, i, all) => all.indexOf(m) === i
    )
  }
  const n = row.name ? normalise(row.name) : ""
  const byName = n
    ? metrics.filter((m) => normalise(m.name) === n || stem(m.name) === n)
    : []
  return [...byCode, ...byName].filter((m, i, all) => all.indexOf(m) === i)
}

/** How a value in one unit reads in another: `to = offset + factor ×
 * from`, or undefined where no conversion joins them. */
export interface Linear {
  factor: number
  offset: number
}

/** One conversion through the repository's unit graph: the value in
 * `to`, or undefined where no route joins the units or a spelling is
 * unknown. */
export type Convert = (
  value: string,
  from: string,
  to: string
) => Promise<string | undefined>

/** A converter that asks once per pair of units, from two points (0 and
 * 1), which a linear conversion is fixed by. */
export function linearCache(
  convert: Convert
): (from: string, to: string) => Promise<Linear | undefined> {
  const seen = new Map<string, Promise<Linear | undefined>>()
  return (from, to) => {
    if (from === to) return Promise.resolve({ factor: 1, offset: 0 })
    const key = `${from}\u0000${to}`
    let p = seen.get(key)
    if (!p) {
      p = Promise.all([convert("0", from, to), convert("1", from, to)]).then(
        ([zero, one]) =>
          zero === undefined || one === undefined
            ? undefined
            : {
                offset: Number(zero),
                factor: Number(one) - Number(zero),
              }
      )
      seen.set(key, p)
    }
    return p
  }
}

/** Which metric a row is written under, by its unit. */
export type MetricPick =
  | {
      metric: MetricRecord
      /** The unit written: the row's, else the metric's for a row that
       * gives none. */
      unit?: string
      /** The value in the metric's unit, for the "may already be here"
       * compare. */
      inMetricUnit?: number
    }
  | { reason: string }

const DIMENSIONLESS = new Set(["1", "{ratio}", "ratio", "{index}"])

export async function pickMetric(
  row: { value?: string; unit?: string; text?: boolean },
  candidates: MetricRecord[],
  linear: (from: string, to: string) => Promise<Linear | undefined>
): Promise<MetricPick> {
  if (!candidates.length)
    return { reason: "No metric of the repository matches its name." }
  if (row.text) return { metric: candidates[0] }
  for (const m of candidates) {
    if (!row.unit) {
      if (!m.unit || DIMENSIONLESS.has(m.unit))
        return {
          metric: m,
          unit: m.unit ?? "1",
          inMetricUnit: Number(row.value),
        }
      continue
    }
    if (!m.unit) return { metric: m, unit: row.unit }
    const map = await linear(row.unit, m.unit)
    if (map)
      return {
        metric: m,
        unit: row.unit,
        inMetricUnit: map.offset + map.factor * Number(row.value),
      }
  }
  if (!row.unit)
    return { reason: "It gives no unit, and its metrics need one." }
  const units = [...new Set(candidates.map((m) => m.unit).filter(Boolean))]
  return {
    reason: `Its unit, ${row.unit}, converts to none of its metrics' (${units.join(", ")}).`,
  }
}

/** A result the repository already holds, as the duplicate check reads
 * it. */
export interface HeldResult {
  /** The metric's id. */
  metric: string
  /** The local day it was taken on. */
  day: string
  value?: number
  unit?: string
  text?: string
}

/** Whether a row may already be in the repository: a result of the same
 * metric on the same day whose value equals it at the metric's display
 * precision (two decimals where the metric names none), from any source.
 * A result of the analyte's other form is found through its twin, which
 * the repository's conversion writes under this metric. */
export async function mayBeHeld(
  row: {
    metric: MetricRecord
    day: string
    inMetricUnit?: number
    text?: string
  },
  held: readonly HeldResult[],
  linear: (from: string, to: string) => Promise<Linear | undefined>
): Promise<boolean> {
  const p = row.metric.precision ?? 2
  const round = (x: number) => Math.round(x * 10 ** p) / 10 ** p
  for (const h of held) {
    if (h.metric !== row.metric.id || h.day !== row.day) continue
    if (row.text !== undefined) {
      if (h.text && normalise(h.text) === normalise(row.text)) return true
      continue
    }
    if (h.value === undefined || row.inMetricUnit === undefined) continue
    let v = h.value
    const to = row.metric.unit
    if (h.unit && to && h.unit !== to) {
      const map = await linear(h.unit, to)
      if (!map) continue
      v = map.offset + map.factor * v
    }
    if (round(v) === round(row.inMetricUnit)) return true
  }
  return false
}
