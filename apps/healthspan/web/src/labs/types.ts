// What the lab screens and rules read: the repository's lab results folded
// into subjects (a catalogue entry, or a metric no entry claims), one point
// per specimen, in the subject's display unit.

import type { CatalogueEntry, MetricRecord, UnitForm } from "../catalogue"
import type { Analyte, UnitGraph } from "./units"

export interface Band {
  low?: number
  high?: number
  text?: string
  /** Absent is the lab's normal range; `poor` a one-sided cut-off; `fair`
   * a borderline band. */
  tone?: string
}

export interface LabResult {
  id: string
  /** The metric record's id. */
  metric: string
  source?: string
  report?: string
  at: string
  /** In `unit`. */
  value?: number
  unit?: string
  comparator?: string
  coded?: { code?: string; display?: string }
  text?: string
  /** A titre: `1:80` is `{numerator: 1, denominator: 80}`. */
  ratio?: { numerator: number; denominator: number }
  /** Why there is no result: `not-performed`, `cancelled`. */
  absentReason?: string
  /** The result as received when it could not be normalized. */
  sourceValue?: string
  sourceUnit?: string
  /** In `unit`. */
  ranges: Band[]
  interpretation: { code: string; display?: string }[]
  channel?: string
  fasting?: boolean
  derivedFrom?: string[]
}

export type MetricInfo = MetricRecord

export interface SourceInfo {
  id: string
  name: string
  class?: string
}

export interface StoredFile {
  digest: string
  mediaType?: string
  name?: string
  size?: number
}

export interface LabReport {
  id: string
  at: string
  lab?: string
  accession?: string
  issuedAt?: string
  status?: string
  /** The owner's check of a report read from a document. */
  review?: string
  source?: string
  /** The original document, which an app's token cannot read. */
  file?: StoredFile
}

/** What one card and one history screen show: a catalogue entry, or a metric
 * with results that no entry claims. */
export interface Subject {
  /** The entry's code, or the metric's id for a metric no entry claims
   * (which holds a slash, so the two never meet). */
  code: string
  entry?: CatalogueEntry
  name: string
  /** The unit its points are in. */
  unit?: string
  precision?: number
  description?: string
  category?: string
  /** Its metrics in the repository, in forms: the display form first. */
  forms: UnitForm[]
}

/** How a specimen's shown result came to be in the display unit. */
export type ShownVia = "original" | "twin" | "converted" | "apart"

/** One specimen: a result with its derived twins and any result naming the
 * same report (or the same accession from the same lab) for the same
 * subject. Its point is `shown`. */
export interface Specimen {
  key: string
  subject: string
  channel?: string
  /** The result the point shows: in the subject's display unit unless
   * `via` is `apart`, where it keeps its own. */
  shown: LabResult
  via: ShownVia
  /** Every result in it, as received. */
  members: LabResult[]
}

/** One series: a subject's specimens, or one channel's. */
export interface Series {
  /** The subject's code, or `<code>#<channel>`. */
  key: string
  code: string
  channel?: string
  /** The shown results of the specimens in the display unit, oldest first. */
  results: LabResult[]
  /** The specimens behind `results`, in the same order. */
  specimens: Specimen[]
  /** Specimens no conversion brings into the display unit, oldest first:
   * shown apart, each in its own unit. */
  apart: Specimen[]
}

export interface LabIndex {
  metrics: Map<string, MetricInfo>
  sources: Map<string, SourceInfo>
  reports: Map<string, LabReport>
  analytes: Map<string, Analyte>
  subjects: Map<string, Subject>
  series: Map<string, Series>
  /** Every specimen, oldest first: one per point, twins folded in. */
  specimens: Specimen[]
  /** Every specimen's shown result, oldest first. */
  results: LabResult[]
  /** The unit graph the values were converted through. */
  graph: UnitGraph
}

export function seriesKey(code: string, channel?: string): string {
  return channel ? `${code}#${channel}` : code
}
