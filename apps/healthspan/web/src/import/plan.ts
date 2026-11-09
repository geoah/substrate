// The plans a file of lab results or a hand entry makes
// (docs/plans/healthspan.md, Import): the ticked rows grouped into reports
// (one per accession, else one per lab and day), one import per lab, at most
// 2,000 rows an import, a larger file split between reports; and the rows
// left out, with why. Each is a `Plan` the one runner writes, its records
// keyed in the import's own namespace.

import { KIND } from "../kinds"
import { interpretation } from "./importers/shared/ranges"
import type {
  ImporterId,
  LeftOut,
  Plan,
  PlannedRecord,
  PlannedRef,
  RecordKey,
} from "./importers/types"
import { LAB_SOURCE, reportKey, resultKey } from "./records"
import type { ResolvedRow } from "./resolve"
import { offsetOf } from "./values"

const ref = (key: RecordKey): PlannedRef => ({ $ref: key })

/** One result a CSV or a hand entry plans. */
export interface ImportRow {
  key: string
  /** The key of its report in the plan. */
  report: string
  name?: string
  /** The metric's id, such as `labs/ldl_cholesterol`. */
  metric: string
  takenAt: string
  value?: string
  comparator?: string
  unit?: string
  text?: string
  low?: string
  high?: string
  flag?: string
}

/** One report a CSV or a hand entry plans. */
export interface ImportReport {
  key: string
  takenAt: string
  accession?: string
}

/** One report's record, exactly as `labs/labreport` declares it: when, the
 * lab's name and source, its accession. */
export function reportRecord(lab: string, report: ImportReport): PlannedRecord {
  const properties: Record<string, unknown> = {
    at: report.takenAt,
    lab,
    source: ref(LAB_SOURCE),
  }
  if (report.accession) properties.accession = report.accession
  return { ...reportKey(report.key), mode: "maintain", properties }
}

/** One result's record, exactly as `labs/labresult` declares it: a gauge
 * held for no time, its value a quantity with its comparator, or its
 * words; the lab's range as two quantities in the result's unit; the flag
 * as the lab's interpretation. */
export function resultRecord(row: ImportRow): PlannedRecord {
  const out: Record<string, unknown> = {
    metric: `${KIND.metric}/${row.metric}`,
    source: ref(LAB_SOURCE),
    at: row.takenAt,
    utcOffset: offsetOf(row.takenAt),
    form: "gauge",
    hold: "PT0S",
    report: ref(reportKey(row.report)),
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
  const flag = interpretation(row.flag)
  if (flag) out.interpretation = flag
  return { ...resultKey(row.key), mode: "maintain", properties: out }
}

/** The most rows one import holds. */
export const MAX_ROWS = 2000

/** One import a file or a hand entry makes, before it has an id: its plan,
 * and the lab's name its source is found or created by. */
export interface LabPlan {
  /** The lab's name, as the file or the owner gave it. */
  lab: string
  plan: Plan
  /** The rows it writes, for the screens. */
  rows: ImportRow[]
  reports: ImportReport[]
}

/** The plan of one lab's reports and rows. */
export function labPlan(opts: {
  name: string
  importer: Extract<ImporterId, "csv" | "manual">
  lab: string
  reports: readonly ImportReport[]
  rows: readonly ImportRow[]
  left?: LeftOut[]
}): Plan {
  return {
    importer: opts.importer,
    name: opts.name,
    source: LAB_SOURCE,
    records: opts.reports.flatMap((rep) => [
      reportRecord(opts.lab, rep),
      ...opts.rows.filter((r) => r.report === rep.key).map(resultRecord),
    ]),
    documents: [],
    left: opts.left ?? [],
  }
}

interface Report {
  lab: string
  accession?: string
  takenAt: string
  rows: ResolvedRow[]
}

function toRow(r: ResolvedRow, key: string, report: string): ImportRow {
  const row: ImportRow = {
    key,
    report,
    metric: r.metric!.id,
    takenAt: r.at!,
  }
  if (r.name) row.name = r.name
  if (r.text !== undefined) row.text = r.text
  else {
    row.value = r.value
    if (r.comparator) row.comparator = r.comparator
    if (r.unit) row.unit = r.unit
  }
  if (r.range.low !== undefined) row.low = r.range.low
  if (r.range.high !== undefined) row.high = r.range.high
  if (r.flag) row.flag = r.flag
  return row
}

/** Why a row was left out, in the skipped list's words. */
export function leftOutReason(r: ResolvedRow): string {
  if (r.problem) return r.problem
  if (r.held) return "It may already be here, so it was left out."
  return "Left out."
}

function leftOf(rows: ResolvedRow[]): LeftOut[] {
  return rows.map((r) => ({
    what: `Line ${r.line}, ${r.name ?? r.loinc ?? "a row"}`,
    why: leftOutReason(r),
  }))
}

/** The imports the ticked rows make: one or more per lab, each its own
 * reports and rows keyed from 1, in the order of their dates. */
export function buildPlan(
  rows: readonly ResolvedRow[],
  ticked: ReadonlySet<number>,
  opts: { name: string; importer: Extract<ImporterId, "csv" | "manual"> }
): LabPlan[] {
  const going = rows.filter(
    (r) => ticked.has(r.line) && !r.problem && r.metric && r.at && r.lab
  )
  const left = rows.filter((r) => !going.includes(r))
  const reports = new Map<string, Report>()
  for (const r of going) {
    const key = r.accession
      ? `${r.lab}\u0000a\u0000${r.accession}`
      : `${r.lab}\u0000d\u0000${r.day}`
    let rep = reports.get(key)
    if (!rep) {
      rep = { lab: r.lab!, accession: r.accession, takenAt: r.at!, rows: [] }
      reports.set(key, rep)
    }
    if (Date.parse(r.at!) < Date.parse(rep.takenAt)) rep.takenAt = r.at!
    rep.rows.push(r)
  }
  const byLab = new Map<string, Report[]>()
  for (const rep of reports.values()) {
    const list = byLab.get(rep.lab) ?? []
    list.push(rep)
    byLab.set(rep.lab, list)
  }
  const out: (Omit<LabPlan, "plan"> & { name: string; left: LeftOut[] })[] = []
  const labs = [...byLab.keys()].sort((a, b) => a.localeCompare(b))
  for (const lab of labs) {
    const reps = byLab
      .get(lab)!
      .sort(
        (a, b) =>
          Date.parse(a.takenAt) - Date.parse(b.takenAt) ||
          (a.accession ?? "").localeCompare(b.accession ?? "")
      )
    // A report longer than an import is split across imports itself.
    const pieces: Report[] = reps.flatMap((rep) => {
      if (rep.rows.length <= MAX_ROWS) return [rep]
      const parts: Report[] = []
      for (let i = 0; i < rep.rows.length; i += MAX_ROWS)
        parts.push({ ...rep, rows: rep.rows.slice(i, i + MAX_ROWS) })
      return parts
    })
    let current: Report[] = []
    let count = 0
    const flush = () => {
      if (!current.length) return
      const imp: (typeof out)[number] = {
        name: opts.name,
        lab,
        rows: [],
        reports: [],
        left: [],
      }
      current.forEach((rep, i) => {
        const key = String(i + 1)
        imp.reports.push(
          rep.accession
            ? { key, takenAt: rep.takenAt, accession: rep.accession }
            : { key, takenAt: rep.takenAt }
        )
        for (const r of rep.rows)
          imp.rows.push(toRow(r, String(imp.rows.length + 1), key))
      })
      out.push(imp)
      current = []
      count = 0
    }
    for (const rep of pieces) {
      if (count + rep.rows.length > MAX_ROWS) flush()
      current.push(rep)
      count += rep.rows.length
    }
    flush()
  }
  if (out.length > 1)
    out.forEach((imp, i) => {
      imp.name = `${opts.name} (${i + 1} of ${out.length})`
    })
  // The rows left out are listed on the first import of their lab, else
  // on the first import.
  for (const imp of out) {
    const mine = left.filter((r) => r.lab === imp.lab)
    if (mine.length && out.find((o) => o.lab === imp.lab) === imp)
      imp.left = leftOf(mine)
  }
  const orphans = left.filter((r) => !out.some((o) => o.lab === r.lab))
  if (orphans.length && out[0])
    out[0].left = [...out[0].left, ...leftOf(orphans)]
  return out.map((imp) => ({
    lab: imp.lab,
    rows: imp.rows,
    reports: imp.reports,
    plan: labPlan({
      name: imp.name,
      importer: opts.importer,
      lab: imp.lab,
      reports: imp.reports,
      rows: imp.rows,
      left: imp.left,
    }),
  }))
}
