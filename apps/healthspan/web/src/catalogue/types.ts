// The lab catalogue's shape (docs/plans/healthspan.md, The catalogue): one
// entry per biomarker, keyed by a code of its own and carrying LOINC codes
// and names, never an owner's metric id. The labs workstream fills the
// entries (catalogue/index.ts) and resolves them against the repository's
// metric records; the import matches a file's tests to them.

import type { Sex } from "../kinds"

export interface ContentSource {
  title: string
  url: string
}

export interface TargetBand {
  from?: number
  to?: number
  tone: "success" | "warning" | "danger"
  label: string
}

export interface CatalogueEntry {
  /** The entry's own key, stable, used in routes (`#/labs/<code>`). */
  code: string
  name: string
  /** Other names a lab prints for it, for matching an import's rows. */
  aliases: string[]
  /** The LOINC codes of every unit form it is reported in, the display
   * form's first. */
  loinc: string[]
  /** The unit record id (`mmol/L`, `1e9/L`, `percent`) it is shown and
   * targeted in. */
  displayUnit: string
  /** The shared catalogue's metric of its display form (`labs/ldl_cholesterol`,
   * `health/body_mass`), where one exists: a fallback after the codes. */
  metric?: string
  /** Decimals its values print with, the display metric's. */
  precision?: number
  panel?: string
  important?: boolean
  trendGoal?: "higher" | "lower"
  /** Months after which an important result is overdue. Default 12. */
  cadenceMonths?: number
  /** Target bands in the display unit; the `success` ones are the target.
   * They hold for either sex unless `targetFor` names other bands for one. */
  target?: TargetBand[]
  /** Bands for one sex where they differ from `target`; an empty list for a
   * sex the target does not hold for, whose results the lab's own range
   * alone judges. */
  targetFor?: Partial<Record<Sex, TargetBand[]>>
  /** The one sex the entry is for (PSA): for the other it is never
   * important and has no target. */
  onlyFor?: Sex
  description?: string
  /** A sentence per status word, keyed by the word in lower case. */
  assessments?: Record<string, string>
  why?: string
  moves?: string
  sources?: ContentSource[]
}

/** A metric record of the repository, as the catalogue matches it. */
export interface MetricRecord {
  /** The id after `observations/metric/` (`labs/ldl_cholesterol`). */
  id: string
  name: string
  /** The unit record's id (`mmol/L`). */
  unit?: string
  codes: { system: string; code: string }[]
  category?: string
  precision?: number
  description?: string
}
