// The GP record's test results as lab reports and results: one report per
// test, one result per value whose term the table below names
// (apps/healthspan/docs/importers/05-nhs.md, Test results).

import {
  dateIn,
  jsonDocument,
  labRecords,
  pythonFloat,
  type AnalyteRef,
  type Band,
  type Comparator,
  type LabRow,
  type MetricContext,
} from "../shared"
import type {
  LeftOut,
  PlannedDocument,
  PlannedRecord,
  RecordKey,
} from "../types"
import type { TestResult, TestValue } from "./schema"

/** The coding system a GP record's test terms are written under. */
export const TEST_SYSTEM = "emis"

/** Each GP record term for a value, as the analyte it is written under: a
 * catalogue entry's code, or a shared metric for a measure the catalogue
 * has no entry for. A term not here is left out. */
export const NHS_TERMS: Readonly<Record<string, AnalyteRef>> = {
  "Body mass index": { entry: "bmi" },
  "Body weight": { entry: "body_mass" },
  "Standing height": { entry: "height" },
  "Total white cell count": { entry: "wbc" },
  "Red blood cell (RBC) count": { entry: "rbc" },
  "Haemoglobin estimation": { entry: "haemoglobin" },
  Haematocrit: { entry: "haematocrit" },
  "Mean corpuscular volume (MCV)": { entry: "mcv" },
  "Mean corpusc. haemoglobin(MCH)": { entry: "mch" },
  "Mean corpusc. Hb. conc. (MCHC)": { entry: "mchc" },
  "Platelet count": { entry: "platelets" },
  "Neutrophil count": { entry: "neutrophils" },
  "Lymphocyte count": { entry: "lymphocytes" },
  "Monocyte count": { entry: "monocytes" },
  "Eosinophil count": { entry: "eosinophils" },
  "Basophil count": { entry: "basophils" },
  "Serum ferritin": { entry: "ferritin" },
  "Serum vitamin B12": { entry: "vitamin_b12" },
  "Serum folate": { entry: "folate" },
  "Serum total 25-OH vit D level": { entry: "vitamin_d" },
  "Se prostate specific Ag level": { entry: "psa" },
  "Serum TSH level": { entry: "tsh" },
  "Serum free T4 level": { entry: "free_t4" },
  "Serum alkaline phosphatase": { entry: "alp" },
  "Serum albumin": { entry: "albumin" },
  "Serum adjusted calcium conc": { entry: "calcium_adjusted" },
  "Serum inorganic phosphate": { entry: "phosphate" },
  "Serum total bilirubin level": { entry: "bilirubin" },
  "Serum ALT level": { entry: "alt" },
  "AST serum level": { entry: "ast" },
  "Serum cholesterol": { entry: "total_cholesterol" },
  "Serum triglycerides": { entry: "triglycerides" },
  "Serum HDL cholesterol level": { entry: "hdl_cholesterol" },
  "Se non HDL cholesterol level": { entry: "non_hdl_cholesterol" },
  "Serum LDL cholesterol level": { entry: "ldl_cholesterol" },
  "Calculated LDL cholesterol lev": { entry: "ldl_cholesterol" },
  "Serum cholesterol/HDL ratio": { entry: "cholesterol_hdl_ratio" },
  "Serum sodium": { entry: "sodium" },
  "Serum potassium": { entry: "potassium" },
  "Serum urea level": { entry: "urea" },
  "Serum creatinine": { entry: "creatinine" },
  "eGFRcreat (CKD-EPI)/1.73 m*2": { entry: "egfr" },
  "HbA1c levl - IFCC standardised": { entry: "hba1c" },
  "Serum testosterone": { entry: "testosterone" },
  "QRISK2 cardiovascular disease 10 year risk score": {
    metric: "labs/cardiovascular_risk",
  },
  "QRISK3 cardiovascular disease 10 year risk calculator score": {
    metric: "labs/cardiovascular_risk",
  },
  "Self reported systolic blood pressure": { entry: "systolic_blood_pressure" },
  "Self reported diastolic blood pressure": {
    entry: "diastolic_blood_pressure",
  },
  "Systolic arterial pressure": { entry: "systolic_blood_pressure" },
  "Diastolic arterial pressure": { entry: "diastolic_blood_pressure" },
  "Pulse rate": { metric: "health/heart_rate" },
  "Heart rate": { metric: "health/heart_rate" },
  "Peripheral oxygen saturation": { metric: "health/spo2" },
  "Waist circumference": { entry: "waist_circumference" },
  "Red blood cell distribut width": { entry: "red_cell_distribution_width" },
  "Mean platelet volume": { entry: "mean_platelet_volume" },
  "Nucleated red blood cell count": { entry: "nucleated_red_cells" },
  "Percentage nucleated RBCs": {
    metric: "labs/nucleated_red_cells_per_100_wbc",
  },
  "Serum calcium": { entry: "calcium" },
  "Alcohol units consumed per day": { metric: "health/alcohol_units_per_day" },
  "Alcohol units consumed per week": {
    metric: "health/alcohol_units_per_week",
  },
  "Planned duration of foreign travel": {
    metric: "health/planned_travel_duration",
  },
  "AKI warning stage": { metric: "labs/aki_warning_stage" },
}

/** A unit one term prints otherwise than the shared table reads it: the
 * percentage of nucleated red cells prints `.` for a percent. */
export const TERM_UNITS: Readonly<
  Record<string, Readonly<Record<string, string>>>
> = {
  "Percentage nucleated RBCs": { ".": "%" },
}

const COMPARATORS: Readonly<Record<string, Comparator>> = {
  "<": "<",
  ">": ">",
  "<=": "<=",
  ">=": ">=",
  "≤": "<=",
  "≥": ">=",
}

/** The first number a displayed value holds (`152 g/L` is `152`), its sign
 * not read. */
const DIGITS = /\d+(?:\.\d+)?/

/** One value's text: its associated texts joined by a space, else what
 * the record displays, else none. */
function said(v: TestValue, display: string): string | null {
  const texts = (v.associatedTexts ?? [])
    .map((x) => (typeof x === "string" ? x : x?.text))
    .filter((t): t is string => !!t)
  return texts.join(" ") || display || null
}

function bound(x: string | number | null | undefined): string | null {
  if (x === null || x === undefined) return null
  return typeof x === "number" ? pythonFloat(x) : x
}

/** A test's values as rows, and the terms the table does not name. */
export function testRows(
  t: TestResult,
  left: LeftOut[]
): { rows: LabRow[]; unmapped: string[] } {
  const rows: LabRow[] = []
  const unmapped: string[] = []
  for (const v of t.testResultValues ?? []) {
    const analyte = NHS_TERMS[v.term]
    if (!analyte) {
      unmapped.push(v.term)
      continue
    }
    const display = v.displayValue ?? ""
    const digits =
      DIGITS.exec(display)?.[0] ??
      (v.numericValue === null || v.numericValue === undefined
        ? null
        : pythonFloat(v.numericValue))
    let comparator: Comparator | null = null
    if (v.numericOperator) {
      comparator = COMPARATORS[v.numericOperator.trim()] ?? null
      if (!comparator) {
        left.push({
          what: `${v.term}, ${t.term ?? t.id}`,
          why: `its comparator, ${v.numericOperator}, is not one the importer reads, so the value is written without it`,
        })
      }
    }
    const min = bound(v.range?.minimum)
    const max = bound(v.range?.maximum)
    const bands: Band[] =
      min !== null || max !== null ? [{ low: min, high: max, lab: "lab" }] : []
    const unit = v.units ?? null
    rows.push({
      key: `${t.id}/${v.term}`,
      analyte,
      coding: {
        system: TEST_SYSTEM,
        code: v.supplierTypeCode || v.term,
        display: v.term,
      },
      value: digits,
      comparator,
      text: digits ? null : said(v, display),
      unit: (unit !== null && TERM_UNITS[v.term]?.[unit]) || unit,
      bands,
      method: v.term.startsWith("Self reported") ? "self reported" : null,
      upstreamId: `${t.id}/${v.term}`,
    })
  }
  return { rows, unmapped }
}

/** Where the test results are written. */
export interface TestPlace {
  namespace: string
  source: RecordKey
  /** The GP practice's name, each report's `lab`. */
  lab: string
  revision: number
}

/** Every test as a report and its results, in the record's order, with the
 * test kept as its report's JSON file; the ids of the tests written, which
 * a consultation names. */
export function testRecords(
  tests: readonly { test: TestResult; raw: unknown }[],
  place: TestPlace,
  ctx: MetricContext
): {
  records: PlannedRecord[]
  documents: PlannedDocument[]
  left: LeftOut[]
  written: Set<string>
} {
  const records: PlannedRecord[] = []
  const documents: PlannedDocument[] = []
  const left: LeftOut[] = []
  const written = new Set<string>()
  const unmapped = new Map<string, number>()
  for (const { test, raw } of tests) {
    const { rows, unmapped: missing } = testRows(test, left)
    for (const term of missing)
      unmapped.set(term, (unmapped.get(term) ?? 0) + 1)
    if (!rows.length) continue
    const date = dateIn(test.date.value, "UTC") ?? test.date.value.slice(0, 10)
    const doc = jsonDocument(raw, {
      provider: "NHS",
      kind: "test result",
      date,
    })
    const out = labRecords(
      {
        namespace: place.namespace,
        key: test.id,
        source: place.source,
        lab: place.lab,
        accession: test.term ?? null,
        at: test.date.value,
        status: "final",
        file: doc.digest,
        revision: place.revision,
      },
      rows,
      ctx
    )
    left.push(...out.left)
    if (!out.records.length) continue
    records.push(...out.records)
    if (!documents.some((d) => d.digest === doc.digest)) documents.push(doc)
    written.add(test.id)
  }
  for (const [term, n] of [...unmapped].sort(([a], [b]) =>
    a < b ? -1 : a > b ? 1 : 0
  ))
    left.push({
      what: `the GP record's ${term}`,
      why: `no metric for it (${n} value${n === 1 ? "" : "s"})`,
    })
  return { records, documents, left, written }
}
