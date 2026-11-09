// The records an import writes, exactly as `labs/labreport` and
// `labs/labresult` declare them (kinds/substrate.reamde.dev/labs/), and
// how a row found at one of their ids is compared with what the plan
// writes there.

import type { SubstrateRecord } from "@substrate/client"
import { KIND, type ImportReport, type ImportRow } from "../kinds"
import { reportId, resultId } from "./ids"
import { offsetOf } from "./values"

const path = (kind: string, id: string) => `${kind}/${id}`

/** The HL7 code system a lab's H, L, A and N flags belong to. */
export const INTERPRETATION =
  "http://terminology.hl7.org/CodeSystem/v3-ObservationInterpretation"

const FLAG_WORDS: Record<string, string> = {
  H: "High",
  L: "Low",
  HH: "Critically high",
  LL: "Critically low",
  A: "Abnormal",
  N: "Normal",
}

/** What the writing of one import needs besides its plan. */
export interface ImportTarget {
  /** The import's id. */
  id: string
  /** The lab's source id. */
  source: string
  /** The lab's name, written on each report. */
  lab: string
}

/** One report's properties. */
export function reportProps(
  t: ImportTarget,
  report: ImportReport
): Record<string, unknown> {
  const out: Record<string, unknown> = {
    at: report.takenAt,
    lab: t.lab,
    source: path(KIND.source, t.source),
    origin: path(KIND.import, t.id),
  }
  if (report.accession) out.accession = report.accession
  return out
}

/** One result's properties: a gauge held for no time, its value a
 * quantity with its comparator, or its words; the lab's range as two
 * quantities in the result's unit; the flag as the lab's interpretation. */
export function resultProps(
  t: ImportTarget,
  row: ImportRow
): Record<string, unknown> {
  const out: Record<string, unknown> = {
    metric: path(KIND.metric, row.metric),
    source: path(KIND.source, t.source),
    at: row.takenAt,
    utcOffset: offsetOf(row.takenAt),
    form: "gauge",
    hold: "PT0S",
    report: path(KIND.labreport, reportId(t.id, row.report)),
    origin: path(KIND.import, t.id),
  }
  if (row.text !== undefined) out.text = row.text
  else if (row.value !== undefined && row.unit)
    out.value = row.comparator
      ? { value: row.value, unit: row.unit, comparator: row.comparator }
      : { value: row.value, unit: row.unit }
  if (row.name) out.coding = { display: row.name }
  if (row.low !== undefined || row.high !== undefined) {
    const range: Record<string, unknown> = {}
    if (row.low !== undefined && row.unit)
      range.low = { value: row.low, unit: row.unit }
    if (row.high !== undefined && row.unit)
      range.high = { value: row.high, unit: row.unit }
    if (Object.keys(range).length) out.referenceRanges = [range]
  }
  if (row.flag) {
    const word = FLAG_WORDS[row.flag]
    out.interpretation = [
      word
        ? { system: INTERPRETATION, code: row.flag, display: word }
        : { code: row.flag },
    ]
  }
  return out
}

/** The ids one import writes, results first then reports, as Undo deletes
 * them (a report's deletion cascades to its results, so the results go
 * first and each is accounted for). */
export function plannedIds(
  importId: string,
  plan: { rows: ImportRow[]; reports: ImportReport[] }
): { kind: string; id: string }[] {
  return [
    ...plan.rows.map((r) => ({
      kind: KIND.labresult,
      id: resultId(importId, r.key),
    })),
    ...plan.reports.map((r) => ({
      kind: KIND.labreport,
      id: reportId(importId, r.key),
    })),
  ]
}

/** A reference as served (`{ref}`) or written (the path). */
export function refPath(v: unknown): string | undefined {
  if (typeof v === "string") return v
  if (v && typeof v === "object" && "ref" in v) {
    const r = v.ref
    return typeof r === "string" ? r : undefined
  }
  return undefined
}

function sameInstant(a: unknown, b: unknown): boolean {
  return (
    typeof a === "string" &&
    typeof b === "string" &&
    Date.parse(a) === Date.parse(b)
  )
}

/** A unit's identity: the names one unit goes by (its id, its code, an
 * alias) give one key. */
export type UnitKey = (name: string) => string

const spelled: UnitKey = (name) => name

function sameQuantity(a: unknown, b: unknown, unitKey: UnitKey): boolean {
  if (!a || !b || typeof a !== "object" || typeof b !== "object") return a === b
  const x = a as { value?: unknown; comparator?: unknown; unit?: unknown }
  const y = b as { value?: unknown; comparator?: unknown; unit?: unknown }
  const ux = typeof x.unit === "string" ? unitKey(x.unit) : undefined
  const uy = typeof y.unit === "string" ? unitKey(y.unit) : undefined
  return (
    ux === uy &&
    Number(x.value) === Number(y.value) &&
    (x.comparator ?? undefined) === (y.comparator ?? undefined)
  )
}

/** Whether a row found at an id the plan writes is what the plan writes
 * there: the same import, metric or report, time, and value or words. The
 * server stores a unit under its id and keeps the digits as written, so a
 * value is the same number in the same unit, the unit compared through
 * `unitKey` (`%` and `percent` are one unit; `g/dL` and `g/L` are not). */
export function sameAsPlanned(
  kind: string,
  sent: Record<string, unknown>,
  found: SubstrateRecord,
  unitKey: UnitKey = spelled
): boolean {
  const p = found.properties
  if (refPath(p.origin) !== sent.origin) return false
  if (!sameInstant(p.at, sent.at)) return false
  if (kind === KIND.labreport)
    return (p.accession ?? undefined) === (sent.accession ?? undefined)
  if (refPath(p.metric) !== sent.metric) return false
  if (refPath(p.report) !== sent.report) return false
  if (sent.text !== undefined) return p.text === sent.text
  return sameQuantity(p.value, sent.value, unitKey)
}
