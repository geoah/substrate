// A provider's source, lab reports and lab results as planned records, with
// exactly the properties `observations/source`, `labs/labreport` and
// `labs/labresult` declare (apps/healthspan/docs/importers/00-shared.md, Lab
// records). Every property a builder maintains is present, null where the
// provider gives nothing, so a later export's rewrite clears what an
// earlier one set; the runner writes `origin` and the revision.

import type { LeftOut, PlannedRecord, RecordKey } from "../types"
import {
  chooseMetric,
  analyteKey,
  type AnalyteRef,
  type MetricContext,
} from "./metric"
import { docRef, KINDS, metricPath, recordKey, ref, sourceKey } from "./keys"
import {
  codedResult,
  interpretation,
  referenceRanges,
  type Band,
} from "./ranges"
import { quantity, type Comparator } from "./numbers"
import { offsetMinutes } from "./time"

export type SourceClass =
  | "watch"
  | "ring"
  | "scale"
  | "thermometer"
  | "meter"
  | "contact"
  | "camera"
  | "app"
  | "service"
  | "lab"
  | "manual"

/** A provider's own source: created where absent, never written again, so
 * what the owner renames or ranks stands. */
export function sourceRecord(s: {
  /** The provider's namespace (`numan`, `dxa/<centre>`). */
  namespace: string
  name: string
  class?: SourceClass
  manufacturer?: string
}): PlannedRecord {
  const properties: Record<string, unknown> = { name: s.name }
  if (s.class) properties.class = s.class
  if (s.manufacturer) properties.manufacturer = s.manufacturer
  return { ...sourceKey(s.namespace), mode: "create", properties }
}

/** What a lab report says, and where it lives. */
export interface LabReportInput {
  /** The provider's account (`numan`, `dxa/<centre>`). */
  namespace: string
  /** The report's key from the provider's data. */
  key: string
  /** The provider's source, as `sourceKey` names it. */
  source: RecordKey
  lab: string
  accession?: string | null
  /** When the specimen was taken, ISO 8601 with its offset. */
  at: string
  orderedAt?: string | null
  issuedAt?: string | null
  status?: string | null
  /** The digest of the report's document in the plan. */
  file?: string | null
  /** The provider's revision, seconds since the epoch. */
  revision: number
  /** Whether the owner is to check it: `review` is written `pending` when
   * the report is created, and never again. */
  review?: boolean
}

/** A lab report's key. */
export function reportKey(
  r: Pick<LabReportInput, "namespace" | "key">
): RecordKey {
  return recordKey(KINDS.labreport, r.namespace, r.key)
}

/** A lab report's planned record. */
export function labReport(r: LabReportInput): PlannedRecord {
  const properties: Record<string, unknown> = {
    lab: r.lab,
    accession: r.accession ?? null,
    at: r.at,
    orderedAt: r.orderedAt ?? null,
    issuedAt: r.issuedAt ?? null,
    status: r.status ?? null,
    file: r.file ? docRef(r.file) : null,
    source: ref(r.source),
  }
  const out: PlannedRecord = {
    ...reportKey(r),
    mode: "maintain",
    properties,
    revision: r.revision,
  }
  if (r.review) {
    properties.review = "pending"
    out.createOnly = ["review"]
  }
  return out
}

/** One value of a report, as a provider reads it. */
export interface LabRow {
  /** The result's key from the provider's data
   * (`<report key>/<analyte code>`). */
  key: string
  /** Which metric it is written under. */
  analyte: AnalyteRef
  /** The analyte's code as the provider sent it, as `coding`. */
  coding: { system: string; code: string; display?: string }
  /** The value's digits as written, where it is a number. */
  value?: string | null
  comparator?: Comparator | null
  /** The result in words, where it is not a number. */
  text?: string | null
  /** The unit as printed. */
  unit?: string | null
  bands?: readonly Band[]
  flag?: string | null
  /** The provider's own word for the flag, where it is longer than the
   * letter. */
  flagText?: string | null
  /** What was sampled; `fasting` only where the provider says. */
  specimen?: { material: string; fasting?: boolean } | null
  method?: string | null
  /** The body region of a regional result (`trunk`, `left_arm`). */
  channel?: string | null
  upstreamId?: string | null
  /** The page of the document it was read from. */
  page?: number | null
  /** The result's status as sent; `final` where the provider gives none. */
  status?: string | null
}

/** What a result builder makes of one row: its record, or none, and what
 * of it was left out or kept as received. */
export interface LabResultOutcome {
  record?: PlannedRecord
  left: LeftOut[]
}

/** A row's place, for what is left out of it. */
function where(report: LabReportInput, row: LabRow): string {
  const name = row.coding.display || row.coding.code
  return `${name}, ${report.accession || report.key}`
}

/** A lab result's planned record from its report and its row: its metric
 * chosen by its analyte and unit, its value a quantity in a known unit,
 * else kept as received, else words (a coded word as a code); its ranges
 * from its bands, its flag as the lab's interpretation, its specimen, its
 * page. A row with neither a value nor words, or whose analyte has no
 * metric in the repository, is left out. */
export function labResult(
  report: LabReportInput,
  row: LabRow,
  ctx: MetricContext
): LabResultOutcome {
  const left: LeftOut[] = []
  const what = where(report, row)
  const numeric =
    row.value !== null && row.value !== undefined && row.value !== ""
  const words = row.text ?? undefined
  if (!numeric && !words) return { left: [{ what, why: "it has no value" }] }
  const choice = chooseMetric(
    row.analyte,
    { numeric, unit: numeric ? row.unit : undefined },
    ctx
  )
  if (choice.kind === "none") return { left: [{ what, why: choice.why }] }
  const p: Record<string, unknown> = {
    metric: metricPath(choice.metric.id),
    source: ref(report.source),
    report: ref(reportKey(report)),
    form: "gauge",
    hold: "PT0S",
    at: report.at,
    utcOffset: offsetMinutes(report.at) ?? null,
    channel: row.channel ?? null,
    value: null,
    code: null,
    codedValue: null,
    text: null,
    sourceValue: null,
    sourceUnit: null,
    normalizationFailure: null,
    referenceRanges: null,
    interpretation: interpretation(row.flag, row.flagText),
    coding: { ...row.coding },
    upstreamId: row.upstreamId ?? null,
    specimen: null,
    method: row.method ?? null,
    status: row.status ?? "final",
    provenance: null,
  }
  let rangeUnit: string | undefined
  if (choice.kind === "value") {
    rangeUnit = choice.unit.ucum
    p.value = quantity(row.value!, choice.unit.ucum, row.comparator)
  } else if (choice.kind === "asReceived") {
    p.sourceValue = `${row.comparator ?? ""}${row.value}`
    p.sourceUnit = row.unit ?? null
    p.normalizationFailure = choice.failure
    left.push({ what, why: `its value is kept as received: ${choice.why}` })
  } else {
    const coded = codedResult(words!)
    if (coded) {
      p.code = coded.code
      p.codedValue = coded.codedValue
    } else p.text = words
  }
  const { ranges, notes } = referenceRanges(row.bands ?? [], {
    ucum: rangeUnit,
    analyte: analyteKey(row.analyte),
    units: ctx.units,
  })
  p.referenceRanges = ranges
  for (const n of notes) left.push({ what, why: `${n}, so it is left out` })
  if (row.specimen) {
    const s: Record<string, unknown> = {
      material: row.specimen.material,
      collectedAt: report.at,
    }
    if (row.specimen.fasting !== undefined) s.fasting = row.specimen.fasting
    p.specimen = s
  }
  if (row.page !== null && row.page !== undefined)
    p.provenance = { page: row.page }
  return {
    record: {
      ...recordKey(KINDS.labresult, report.namespace, row.key),
      mode: "maintain",
      properties: p,
      revision: report.revision,
    },
    left,
  }
}

/** A report and its results as records in write order (the report first),
 * or no records where no result is left: a report with nothing to show is
 * not written. A second row under a result key already taken is left out,
 * the first kept. */
export function labRecords(
  report: LabReportInput,
  rows: readonly LabRow[],
  ctx: MetricContext
): { records: PlannedRecord[]; left: LeftOut[] } {
  const results: PlannedRecord[] = []
  const left: LeftOut[] = []
  const taken = new Set<string>()
  for (const row of rows) {
    if (taken.has(row.key)) {
      left.push({
        what: where(report, row),
        why: "a second value under the same key; the first is kept",
      })
      continue
    }
    const out = labResult(report, row, ctx)
    left.push(...out.left)
    if (!out.record) continue
    taken.add(row.key)
    results.push(out.record)
  }
  return {
    records: results.length ? [labReport(report), ...results] : [],
    left,
  }
}
