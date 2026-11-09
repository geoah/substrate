// Which of the repository's metrics a provider's analyte is written under
// (apps/healthspan/docs/importers/00-shared.md, Metrics): the catalogue
// entry its table names, or the one its LOINC code or its name finds; then
// that entry's metric whose unit takes the value's (catalogue/resolve). A
// measure outside the catalogue names its shared metric. A value whose unit
// the repository does not know, or that no metric of the analyte takes, is
// kept as received under the analyte's metric, with the reason.

import {
  entryByCode,
  entryMetrics,
  LOINC_SYSTEM,
  matchEntry,
  metricForUnit,
  type CatalogueEntry,
  type MetricRecord,
} from "../../../catalogue"
import { convertible, type UnitGraph } from "../../../labs/units"
import { knownUnit, type KnownUnit } from "./units"

/** How a provider's table names an analyte. */
export interface AnalyteRef {
  /** The catalogue entry's code (`ldl_cholesterol`, `vitamin_b12`). */
  entry?: string
  /** A shared metric's id (`health/fat_mass`), for a measure the catalogue
   * has no entry for, or to take that metric before the entry's. */
  metric?: string
  /** A LOINC code the provider sends. */
  loinc?: string
  /** The provider's name for it, matched to an entry's name or aliases. */
  name?: string
}

/** What resolving an analyte reads from the repository. */
export interface MetricContext {
  metrics: readonly MetricRecord[]
  units: UnitGraph
}

/** The catalogue entry an analyte names: by its code, else by its LOINC
 * code or its name. */
export function analyteEntry(a: AnalyteRef): CatalogueEntry | undefined {
  if (a.entry) return entryByCode(a.entry)
  return a.loinc || a.name
    ? matchEntry({ loinc: a.loinc, name: a.name })
    : undefined
}

/** The key the unit table's overrides name an analyte by: its catalogue
 * entry's code, else its shared metric's id after the package. */
export function analyteKey(a: AnalyteRef): string | undefined {
  const entry = analyteEntry(a)
  if (entry) return entry.code
  return a.metric?.slice(a.metric.indexOf("/") + 1) || undefined
}

/** How an analyte's value is written. */
export type MetricChoice =
  /** As a quantity in `unit.ucum`, under `metric`. */
  | { kind: "value"; metric: MetricRecord; unit: KnownUnit }
  /** As words, under `metric`. */
  | { kind: "words"; metric: MetricRecord }
  /** As received (`sourceValue`, `sourceUnit`), under `metric`, with the
   * reason as the result's `normalizationFailure` and a sentence. */
  | {
      kind: "asReceived"
      metric: MetricRecord
      failure: "unknown-unit" | "unconvertible-unit"
      why: string
    }
  /** Not at all: the repository has no metric for it. */
  | { kind: "none"; why: string }

function label(a: AnalyteRef, entry: CatalogueEntry | undefined): string {
  return entry?.name ?? a.name ?? a.metric ?? a.loinc ?? "it"
}

/** The analyte's metrics in the repository, best first: the shared metric
 * its table names, then its entry's (as the catalogue claims them), then
 * any other holding its LOINC code. */
export function analyteMetrics(
  a: AnalyteRef,
  ctx: MetricContext
): MetricRecord[] {
  const entry = analyteEntry(a)
  const named = a.metric ? ctx.metrics.filter((m) => m.id === a.metric) : []
  const own = entry ? entryMetrics(entry, ctx.metrics) : []
  const coded = a.loinc
    ? ctx.metrics.filter((m) =>
        m.codes.some((c) => c.system === LOINC_SYSTEM && c.code === a.loinc)
      )
    : []
  return [...named, ...own, ...coded].filter(
    (m, i, all) => all.indexOf(m) === i
  )
}

/** Which metric an analyte's value is written under, and how: a number in
 * `unit` (as printed) or, with `unit` undefined and `numeric` false, words.
 * The shared metric the table names takes the value where its unit
 * converts; else the entry's metric of the value's unit form (display form
 * first). Words, and a value kept as received, go under the named metric,
 * else the entry's display form. */
export function chooseMetric(
  a: AnalyteRef,
  value: { numeric: boolean; unit?: string | null },
  ctx: MetricContext
): MetricChoice {
  const entry = analyteEntry(a)
  const candidates = analyteMetrics(a, ctx)
  if (!candidates.length)
    return {
      kind: "none",
      why: `the repository has no metric for ${label(a, entry)}`,
    }
  const conv = (from: string, to: string) => convertible(ctx.units, from, to)
  const named = a.metric ? candidates.find((m) => m.id === a.metric) : undefined
  const fallback =
    named ??
    (entry ? metricForUnit(entry, undefined, ctx.metrics, conv) : undefined) ??
    candidates[0]
  if (!value.numeric) return { kind: "words", metric: fallback }
  const unit = knownUnit(value.unit, analyteKey(a), ctx.units)
  if (!unit)
    return {
      kind: "asReceived",
      metric: fallback,
      failure: "unknown-unit",
      why: `its unit, ${value.unit || "none"}, is not one the repository knows`,
    }
  const takes = (m: MetricRecord) =>
    !m.unit || m.unit === unit.id || conv(unit.id, m.unit)
  const metric =
    (named && takes(named) ? named : undefined) ??
    (entry ? metricForUnit(entry, unit.id, ctx.metrics, conv) : undefined) ??
    candidates.find(takes)
  if (!metric)
    return {
      kind: "asReceived",
      metric: fallback,
      failure: "unconvertible-unit",
      why: `its unit, ${unit.ucum}, converts to none of the units of ${label(a, entry)}'s metrics`,
    }
  return { kind: "value", metric, unit }
}
