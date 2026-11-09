// The lab catalogue (docs/plans/healthspan.md, The catalogue): the entries,
// their panels and targets, and the resolvers the labs and the import read
// them through.

import type { Sex } from "../kinds"
import { LAB_CONTENT } from "./content"
import { ENTRY_LIST } from "./entries"
import {
  claimMetrics as claimAmong,
  matchEntry as matchAmong,
  metricForUnit as metricForUnitAmong,
  metricsOf,
  type Convertible,
} from "./resolve"
import type { CatalogueEntry, MetricRecord, TargetBand } from "./types"

export type {
  CatalogueEntry,
  ContentSource,
  MetricRecord,
  TargetBand,
} from "./types"
export {
  LOINC_SYSTEM,
  nameKey,
  unitForms,
  type Convertible,
  type UnitForm,
} from "./resolve"

export const DEFAULT_CADENCE_MONTHS = 12

/** Every entry, in panel order, with its content joined by code. */
export const CATALOGUE: readonly CatalogueEntry[] = ENTRY_LIST.map((e) => {
  const c = LAB_CONTENT[e.code]
  return c ? { ...e, why: c.why, moves: c.moves, sources: c.sources } : e
})

const BY_CODE = new Map(CATALOGUE.map((e) => [e.code, e]))

export function entryByCode(code: string): CatalogueEntry | undefined {
  return BY_CODE.get(code)
}

/** The catalogue's panels, in its order. */
export const PANELS: string[] = [
  ...new Set(CATALOGUE.flatMap((e) => (e.panel ? [e.panel] : []))),
]

/** Panel names for what sits outside the catalogue's panels, by the
 * metric's `category`. */
const CATEGORY_PANELS: Record<string, string> = {
  labs: "Other blood tests",
  body: "Body composition",
  urine: "Urine",
  scores: "Scores",
  heart: "Heart",
  "blood pressure": "Blood pressure",
  glucose: "Glucose",
  lifestyle: "Lifestyle",
}

/** An entry's panel, else its category's. */
export function panelFor(
  entry: CatalogueEntry | undefined,
  category: string | undefined
): string {
  if (entry?.panel) return entry.panel
  const c = category ?? "labs"
  return CATEGORY_PANELS[c] ?? c.charAt(0).toUpperCase() + c.slice(1)
}

/** Where an entry sits in the catalogue (its panel order), or undefined
 * outside it. */
const ORDER = new Map(CATALOGUE.map((e, i) => [e.code, i]))

export function catalogueOrder(code: string): number | undefined {
  return ORDER.get(code)
}

/** The entry as it applies to one sex: the other sex's entry of a one-sex
 * test (PSA) is neither important nor targeted, and a sex with bands of its
 * own takes them (none for an empty list). Unknown, the entry stands. */
export function forSex(
  entry: CatalogueEntry,
  sex: Sex | undefined
): CatalogueEntry {
  if (!sex) return entry
  if (entry.onlyFor && entry.onlyFor !== sex)
    return { ...entry, important: false, target: undefined }
  const own = entry.targetFor?.[sex]
  if (!own) return entry
  return { ...entry, target: own.length ? own : undefined }
}

/** Whether `value` lies in a target band. Both bounds are inclusive: MPV
 * 12.0 is inside 7.0 to 12.0. */
export function inTargetBand(b: TargetBand, value: number): boolean {
  return (
    (b.from === undefined || value >= b.from) &&
    (b.to === undefined || value <= b.to)
  )
}

/** The green bands, which together are the target. */
export function greenBands(entry: CatalogueEntry | undefined): TargetBand[] {
  return entry?.target?.filter((b) => b.tone === "success") ?? []
}

/** The target as one span: the lowest and highest bound of the green
 * bands, undefined on an open side. */
export function targetSpan(
  entry: CatalogueEntry | undefined
): { from?: number; to?: number } | undefined {
  const green = greenBands(entry)
  if (!green.length) return undefined
  const from = green.some((b) => b.from === undefined)
    ? undefined
    : Math.min(...green.map((b) => b.from as number))
  const to = green.some((b) => b.to === undefined)
    ? undefined
    : Math.max(...green.map((b) => b.to as number))
  return { from, to }
}

/** Where `value` lands in the target bands, or undefined with no target.
 * Bounds are inclusive, so a value on a shared edge lies in two bands; the
 * green band wins, so a value on the target's edge is on target. */
export function targetBand(
  entry: CatalogueEntry | undefined,
  value: number
): TargetBand | undefined {
  const bands = entry?.target ?? []
  return (
    bands.find((b) => b.tone === "success" && inTargetBand(b, value)) ??
    bands.find((b) => inTargetBand(b, value))
  )
}

/** Which way is better, as a card says it: from `trendGoal`, else "In range
 * is best" when the entry has a target, else nothing. */
export function directionPhrase(
  entry: CatalogueEntry | undefined
): "Lower is better" | "Higher is better" | "In range is best" | undefined {
  if (entry?.trendGoal === "lower") return "Lower is better"
  if (entry?.trendGoal === "higher") return "Higher is better"
  if (entry?.target?.length) return "In range is best"
  return undefined
}

/** The assessment sentence for a status word ("High"), if any. */
export function assessmentFor(
  entry: CatalogueEntry | undefined,
  word: string | undefined
): string | undefined {
  if (!word) return undefined
  return entry?.assessments?.[word.toLowerCase()]
}

/** Each claimed metric's entry, by metric id (by LOINC code first, then by
 * the entry's shared metric). */
export function claimMetrics(
  metrics: readonly MetricRecord[]
): Map<string, CatalogueEntry> {
  return claimAmong(CATALOGUE, metrics)
}

/** An entry's metrics among the repository's. */
export function entryMetrics(
  entry: CatalogueEntry,
  metrics: readonly MetricRecord[]
): MetricRecord[] {
  return metricsOf(entry, CATALOGUE, metrics)
}

/** The entry an import row names: by LOINC code, else by name or alias
 * (case, spaces and punctuation ignored). */
export function matchEntry(row: {
  loinc?: string
  name?: string
}): CatalogueEntry | undefined {
  return matchAmong(CATALOGUE, row)
}

/** The metric an import writes a row of `entry` in `unit` under: the
 * entry's metric of that unit, else one the unit converts to (display form
 * first). Undefined when none takes it. `convertible` answers whether two
 * units convert through ordinary conversions: the unit graph's
 * (labs/units.ts) or the server's `/api/v1/units/convert`. */
export function metricForUnit(
  entry: CatalogueEntry,
  unit: string | undefined,
  metrics: readonly MetricRecord[],
  convertible: Convertible
): MetricRecord | undefined {
  return metricForUnitAmong(entry, unit, metrics, convertible, CATALOGUE)
}
