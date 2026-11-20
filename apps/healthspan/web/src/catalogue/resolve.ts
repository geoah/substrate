// Which of the repository's metric records an entry reads, and in which unit
// forms (docs/plans/healthspan.md, Resolving an entry). An entry's metrics
// are those holding one of its LOINC codes, then the shared metric it names;
// a metric belongs to one entry, the first in catalogue order that claims
// it. They fall into unit forms: the ones whose unit converts to the display
// unit through ordinary conversions (the display form), and the others,
// which convert only through an analyte's molar mass or formula.

import type { CatalogueEntry, MetricRecord } from "./types"

export const LOINC_SYSTEM = "http://loinc.org"

/** Whether a value in `from` converts to `to` through ordinary (linear)
 * conversions; equal units do. */
export type Convertible = (from: string, to: string) => boolean

function loincCodes(m: MetricRecord): string[] {
  return m.codes.filter((c) => c.system === LOINC_SYSTEM).map((c) => c.code)
}

/** Each claimed metric's entry, by metric id: by code first, over every
 * entry in order, then by the shared metric id for the ones left. */
export function claimMetrics(
  entries: readonly CatalogueEntry[],
  metrics: readonly MetricRecord[]
): Map<string, CatalogueEntry> {
  const byCode = new Map<string, CatalogueEntry>()
  for (const e of entries)
    for (const code of e.loinc) if (!byCode.has(code)) byCode.set(code, e)
  const out = new Map<string, CatalogueEntry>()
  for (const m of metrics) {
    const e = loincCodes(m)
      .map((c) => byCode.get(c))
      .find((x): x is CatalogueEntry => !!x)
    if (e) out.set(m.id, e)
  }
  const byShared = new Map<string, CatalogueEntry>()
  for (const e of entries)
    if (e.metric && !byShared.has(e.metric)) byShared.set(e.metric, e)
  for (const m of metrics) {
    if (out.has(m.id)) continue
    const e = byShared.get(m.id)
    if (e) out.set(m.id, e)
  }
  return out
}

export interface UnitForm {
  /** The form's unit: the display unit for the display form, else its first
   * metric's. */
  unit?: string
  display: boolean
  metrics: MetricRecord[]
}

/** The unit forms of an entry's metrics, the display form first (present
 * even when empty), the others in the order their first metric came. */
export function unitForms(
  displayUnit: string,
  metrics: readonly MetricRecord[],
  convertible: Convertible
): UnitForm[] {
  const display: UnitForm = { unit: displayUnit, display: true, metrics: [] }
  const others: UnitForm[] = []
  for (const m of metrics) {
    if (m.unit && convertible(m.unit, displayUnit)) {
      display.metrics.push(m)
      continue
    }
    const home = others.find((f) =>
      f.unit && m.unit ? convertible(m.unit, f.unit) : f.unit === m.unit
    )
    if (home) home.metrics.push(m)
    else others.push({ unit: m.unit, display: false, metrics: [m] })
  }
  return [display, ...others]
}

/** The entry's metrics among `metrics`, as `claimMetrics` assigns them. */
export function metricsOf(
  entry: CatalogueEntry,
  entries: readonly CatalogueEntry[],
  metrics: readonly MetricRecord[]
): MetricRecord[] {
  const claims = claimMetrics(entries, metrics)
  return metrics.filter((m) => claims.get(m.id)?.code === entry.code)
}

/** The metric an import writes a row of `entry` under, given the row's
 * unit: one whose unit is the row's, else one the row's unit converts to;
 * the display form first and the shared metric first within it. A row with
 * no unit takes the display form's first. Undefined when no metric of the
 * entry takes the unit: the row is left out, with the reason. */
export function metricForUnit(
  entry: CatalogueEntry,
  unit: string | undefined,
  metrics: readonly MetricRecord[],
  convertible: Convertible,
  entries: readonly CatalogueEntry[]
): MetricRecord | undefined {
  const forms = unitForms(
    entry.displayUnit,
    metricsOf(entry, entries, metrics),
    convertible
  )
  const ordered = forms.flatMap((f) =>
    [...f.metrics].sort(
      (a, b) => Number(b.id === entry.metric) - Number(a.id === entry.metric)
    )
  )
  if (!unit) return forms[0].metrics.length ? ordered[0] : undefined
  return (
    ordered.find((m) => m.unit === unit) ??
    ordered.find((m) => !!m.unit && convertible(unit, m.unit))
  )
}

/** A name as rows are matched on: lower case, letters and digits only, so
 * "LDL Cholesterol", "ldl-cholesterol" and "LDL cholesterol" are one. */
export function nameKey(name: string): string {
  return name
    .normalize("NFKD")
    .toLowerCase()
    .replace(/[^a-z0-9]/g, "")
}

/** The entry a row names: by its LOINC code first, then by its name against
 * every entry's name and aliases. */
export function matchEntry(
  entries: readonly CatalogueEntry[],
  row: { loinc?: string; name?: string }
): CatalogueEntry | undefined {
  const code = row.loinc?.trim()
  if (code) {
    const e = entries.find((x) => x.loinc.includes(code))
    if (e) return e
  }
  const key = row.name ? nameKey(row.name) : ""
  if (!key) return undefined
  return entries.find(
    (e) =>
      nameKey(e.name) === key ||
      nameKey(e.code) === key ||
      e.aliases.some((a) => nameKey(a) === key)
  )
}
