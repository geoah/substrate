// A MyChart portal's lab results: one report per result of type LAB, its
// components as results, a value MyChart files under two orders kept once
// (apps/healthspan/docs/importers/06-mychart.md, Lab results).

import {
  analyteEntry,
  dateIn,
  isNumber,
  jsonDocument,
  labRecords,
  parseClock,
  reportKey,
  splitComparator,
  strip,
  zoned,
  type AnalyteRef,
  type Band,
  type LabReportInput,
  type LabRow,
  type MetricContext,
} from "../shared"
import type {
  LeftOut,
  PlannedDocument,
  PlannedRecord,
  RecordKey,
} from "../types"
import { byCodePoint } from "./folder"
import type { Component, Result, TestResult } from "./schemas"

/** The coding system a component's common name is written under. */
export const CODING = "epic"

/** What a document of the portal is named after. */
export const PROVIDER = "MyChart"

/** A component's name as MyChart shows it, and the analyte it is written
 * under: a catalogue entry, or a shared metric for a measure the catalogue
 * has no entry for. A name not here is matched to the catalogue by its
 * name, then its common name. */
export const COMPONENTS: Readonly<Record<string, AnalyteRef>> = {
  "Alanine transaminase": { entry: "alt" },
  Albumin: { entry: "albumin" },
  "Alkaline phosphatase": { entry: "alp" },
  Basophils: { entry: "basophils" },
  "Bilirubin (total)": { entry: "bilirubin" },
  "C-reactive protein": { entry: "crp" },
  Calcium: { entry: "calcium" },
  "Calcium (albumin-adjusted)": { entry: "calcium_adjusted" },
  Cholesterol: { entry: "total_cholesterol" },
  "Cholesterol:HDL ratio": { entry: "cholesterol_hdl_ratio" },
  Creatinine: { entry: "creatinine" },
  Eosinophils: { entry: "eosinophils" },
  "Estimated GFR": { entry: "egfr" },
  "Free T4": { entry: "free_t4" },
  // a venous glucose with no fasting stated, not a fasting glucose
  Glucose: { metric: "health/blood_glucose" },
  HCT: { entry: "haematocrit" },
  "HDL Cholesterol": { entry: "hdl_cholesterol" },
  "Haemoglobin (g/L)": { entry: "haemoglobin" },
  "Haemoglobin A1c (mmol/mol)": { entry: "hba1c" },
  "LDL Cholesterol (Calculated)": { entry: "ldl_cholesterol" },
  Lymphocytes: { entry: "lymphocytes" },
  MCH: { entry: "mch" },
  "MCHC (g/L)": { entry: "mchc" },
  MCV: { entry: "mcv" },
  MPV: { entry: "mean_platelet_volume" },
  Monocytes: { entry: "monocytes" },
  Neutrophils: { entry: "neutrophils" },
  "Non-HDL Cholesterol": { entry: "non_hdl_cholesterol" },
  Phosphate: { entry: "phosphate" },
  "Platelet count": { entry: "platelets" },
  Potassium: { entry: "potassium" },
  RDW: { entry: "red_cell_distribution_width" },
  "Red cell count": { entry: "rbc" },
  "Serum folate": { entry: "folate" },
  Sodium: { entry: "sodium" },
  // also the one component of an order MyChart titles after thyroid
  // stimulating immunoglobulin: the lab reported a TSH there
  "Thyroid stimulating hormone": { entry: "tsh" },
  Triglyceride: { entry: "triglycerides" },
  "Vitamin B12": { entry: "vitamin_b12" },
  "White cell count": { entry: "wbc" },
  "XE  Nucleated RBCs abs": { entry: "nucleated_red_cells" },
  "Acute Kidney Injury Warning": { metric: "labs/aki_warning_stage" },
  "Prothrombin Time": { metric: "labs/prothrombin_time" },
  INR: { metric: "labs/inr" },
  APTT: { metric: "labs/aptt" },
  "APTT Ratio": { metric: "labs/aptt_ratio" },
  "D-Dimer": { metric: "labs/d_dimer" },
  ESR: { entry: "esr" },
  "Reticulocytes (abs)": { metric: "labs/reticulocytes" },
  "Reticulocyte count %": { metric: "labs/reticulocytes_fraction" },
  "Immature granulocyte count %": {
    metric: "labs/immature_granulocytes_fraction",
  },
}

/** MyChart's flag words as a lab's flag. */
const FLAGS: Readonly<Record<string, string>> = {
  High: "H",
  Low: "L",
  Normal: "N",
}

/** A component's analyte: the table's, else the catalogue entry its name
 * or its common name finds. */
export function analyteOf(
  name: string,
  commonName?: string | null
): AnalyteRef | undefined {
  const known = COMPONENTS[name]
  if (known) return known
  if (analyteEntry({ name })) return { name }
  if (commonName && analyteEntry({ name: commonName }))
    return { name: commonName }
  return undefined
}

/** One result of an order, with the order it came in and that order's
 * JSON as read. */
export interface ResultOf {
  item: TestResult
  raw: unknown
  result: Result
}

/** Every result of every order, in file name order, then sorted by its
 * sample time and its key as text (a result with no sample time first). */
export function sortResults(
  items: readonly { value: TestResult; raw: unknown }[]
): ResultOf[] {
  const out: ResultOf[] = []
  for (const { value, raw } of items)
    for (const result of value.results ?? [])
      out.push({ item: value, raw, result })
  return out
    .map((r, i) => ({ r, i }))
    .sort(
      (a, b) =>
        byCodePoint(
          a.r.result.orderMetadata.prioritizedInstantISO ?? "",
          b.r.result.orderMetadata.prioritizedInstantISO ?? ""
        ) ||
        byCodePoint(a.r.result.key, b.r.result.key) ||
        a.i - b.i
    )
    .map((x) => x.r)
}

/** A component's reference range as a band: both ends as the lab printed
 * them, else the one end of the formatted range (`<550` is a high end,
 * `>2.9` a low one). */
export function bandsOf(
  range:
    | {
        displayLow?: string | null
        displayHigh?: string | null
        formattedReferenceRange?: string | null
      }
    | null
    | undefined
): Band[] {
  let low: string | null = strip(range?.displayLow ?? "") || null
  let high: string | null = strip(range?.displayHigh ?? "") || null
  if (low === null && high === null) {
    const { comparator, value } = splitComparator(
      range?.formattedReferenceRange ?? ""
    )
    if (comparator === ">" || comparator === ">=") low = value
    else if (comparator === "<" || comparator === "<=") high = value
  }
  return low !== null || high !== null ? [{ low, high, lab: "lab" }] : []
}

/** What reading the lab results needs. */
export interface LabContext extends MetricContext {
  namespace: string
  source: RecordKey
  zone: string
  revision: number
  /** The hospital's name, the lab where MyChart names none. */
  hospital: string
}

export interface LabRead {
  records: PlannedRecord[]
  documents: PlannedDocument[]
  left: LeftOut[]
  /** The result keys whose reports the plan writes. */
  written: Set<string>
}

/** The portal's lab reports and results, in write order. */
export function readLabs(
  results: readonly ResultOf[],
  ctx: LabContext
): LabRead {
  const records: PlannedRecord[] = []
  const documents: PlannedDocument[] = []
  const left: LeftOut[] = []
  const written = new Set<string>()
  const unmapped = new Map<string, number>()
  const taken = new Set<string>()
  let comments = 0
  let repeated = 0
  for (const { item, raw, result } of results) {
    const meta = result.orderMetadata
    if (meta.resultType !== "LAB") continue
    const title = item.orderName || result.name || "A lab result"
    const at = meta.prioritizedInstantISO
    if (!at) {
      left.push({ what: title, why: "it has no sample time" })
      continue
    }
    const material =
      strip((meta.specimensDisplay ?? "").split("(")[0]).toLowerCase() || null
    const rows: LabRow[] = []
    for (const c of result.resultComponents ?? []) {
      const row = componentRow(c, result.key, at, material, {
        unmapped,
        taken,
        comment: () => comments++,
        repeat: () => repeated++,
      })
      if (row) rows.push(row)
    }
    if (!rows.length) continue
    let issuedAt: string | null = null
    if (meta.resultTimestampDisplay) {
      const w = parseClock(meta.resultTimestampDisplay, "%d %b %Y %H:%M")
      issuedAt = (w && zoned(w, ctx.zone)) ?? null
    }
    const doc = jsonDocument(raw, {
      provider: PROVIDER,
      kind: "lab report",
      date: dateIn(at, ctx.zone) ?? "",
    })
    const report: LabReportInput = {
      namespace: ctx.namespace,
      key: result.key,
      source: ctx.source,
      lab: meta.resultingLab?.name || ctx.hospital,
      accession: item.orderName || result.name || null,
      at,
      orderedAt: null,
      issuedAt,
      status: meta.resultStatus || null,
      file: doc.digest,
      revision: ctx.revision,
    }
    const out = labRecords(report, rows, ctx)
    left.push(...out.left)
    if (!out.records.length) continue
    records.push(...out.records)
    written.add(reportKey(report).key)
    if (!documents.some((d) => d.digest === doc.digest)) documents.push(doc)
  }
  for (const [name, n] of [...unmapped].sort(([a], [b]) => byCodePoint(a, b)))
    left.push({
      what: `${PROVIDER}'s ${name}`,
      why: `no metric for it (${n} value${n === 1 ? "" : "s"})`,
    })
  if (comments)
    left.push({
      what: `the lab's comments on ${comments} value${comments === 1 ? "" : "s"}`,
      why: "a lab result has no property for them; they stay in each report's attached JSON",
    })
  if (repeated)
    left.push({
      what: `${repeated} value${repeated === 1 ? "" : "s"} MyChart files under a second order`,
      why: "kept once, in the first",
    })
  return { records, documents, left, written }
}

/** One component as a result row, or none: a component with no name, one
 * no analyte is known for (counted), and a value already read under an
 * earlier order (the same name, sample time, value and unit) are left
 * out. */
function componentRow(
  c: Component,
  resultKey: string,
  at: string,
  material: string | null,
  tally: {
    unmapped: Map<string, number>
    taken: Set<string>
    comment: () => void
    repeat: () => void
  }
): LabRow | undefined {
  const info = c.componentInfo
  const res = c.componentResultInfo
  const name = info?.name
  if (!name) return undefined
  const analyte = analyteOf(name, info.commonName)
  if (!analyte) {
    tally.unmapped.set(name, (tally.unmapped.get(name) ?? 0) + 1)
    return undefined
  }
  if (c.componentComments?.hasContent) tally.comment()
  const shown = strip(res?.value ?? "")
  const same = JSON.stringify([name, at, shown, info.units ?? null])
  if (tally.taken.has(same)) {
    tally.repeat()
    return undefined
  }
  tally.taken.add(same)
  const split = splitComparator(shown)
  const numeric = isNumber(split.value)
  const word = res?.abnormalFlagCategoryValue ?? ""
  const flag = FLAGS[word]
  const key = `${resultKey}/${info.componentID || name}`
  return {
    key,
    analyte,
    coding: { system: CODING, code: info.commonName || name, display: name },
    value: numeric ? split.value : null,
    comparator: numeric ? (split.comparator ?? null) : null,
    text: numeric ? null : shown || null,
    unit: info.units || null,
    bands: bandsOf(res?.referenceRange),
    flag: flag ?? null,
    flagText: flag ? word : null,
    specimen: material ? { material } : null,
    upstreamId: key,
  }
}
