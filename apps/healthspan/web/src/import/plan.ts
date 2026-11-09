// The plan an import writes (docs/plans/healthspan.md, The package,
// import): the ticked rows grouped into reports (one per accession, else
// one per lab and day), one import per lab, at most 2,000 rows an import
// record (a request body is capped at 1 MiB), a larger file split between
// reports; and the rows left out, with why.

import type { ImportReport, ImportRow } from "../kinds"
import type { ResolvedRow } from "./resolve"

/** The most rows one import record holds. */
export const MAX_ROWS = 2000

/** One import the plan writes, before it has an id. */
export interface PlannedImport {
  name: string
  format: "csv" | "manual"
  /** The lab's name, as the file or the owner gave it. */
  lab: string
  rows: ImportRow[]
  reports: ImportReport[]
  /** The rows left out and why, as markdown. */
  skipped?: string
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

function skippedMarkdown(rows: ResolvedRow[]): string | undefined {
  if (!rows.length) return undefined
  return rows
    .map((r) => {
      const what = r.name ?? r.loinc ?? "a row"
      return `- Line ${r.line}, ${what}: ${leftOutReason(r)}`
    })
    .join("\n")
}

/** The imports the ticked rows make: one or more per lab, each its own
 * reports and rows keyed from 1, in the order of their dates. */
export function buildPlan(
  rows: readonly ResolvedRow[],
  ticked: ReadonlySet<number>,
  opts: { name: string; format: "csv" | "manual" }
): PlannedImport[] {
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
  const out: PlannedImport[] = []
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
      const imp: PlannedImport = {
        name: opts.name,
        format: opts.format,
        lab,
        rows: [],
        reports: [],
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
      imp.skipped = skippedMarkdown(mine)
  }
  const orphans = left.filter((r) => !out.some((o) => o.lab === r.lab))
  if (orphans.length && out[0])
    out[0].skipped = [out[0].skipped, skippedMarkdown(orphans)]
      .filter(Boolean)
      .join("\n")
  return out
}
