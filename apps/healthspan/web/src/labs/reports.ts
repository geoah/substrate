// The lab reports as the Reports screens list them: each with its
// specimens, how many the lab flagged, and the name it goes by.

import { formatBytes } from "../lib/format"
import { outOfRange, resultFlag, seriesName } from "../rules/labs"
import { resultText } from "./result"
import type { LabIndex, LabReport, Specimen, StoredFile } from "./types"

export interface ReportSummary {
  report: LabReport
  /** The lab, by its source's name, else as the report names it. */
  title: string
  results: number
  flagged: number
}

/** A report's lab: its source's name, else its own `lab`. */
export function reportTitle(
  index: Pick<LabIndex, "sources">,
  r: LabReport
): string {
  return (
    (r.source ? index.sources.get(r.source)?.name : undefined) ??
    r.lab ??
    "Lab report"
  )
}

/** The specimens a report holds: those any of whose results names it. */
export function reportSpecimens(
  index: Pick<LabIndex, "specimens">,
  id: string
): Specimen[] {
  return index.specimens.filter((s) => s.members.some((m) => m.report === id))
}

/** Every report, newest first, with its results counted once per specimen
 * and those the lab flagged. */
export function reportSummaries(index: LabIndex): ReportSummary[] {
  const counts = new Map<string, { results: number; flagged: number }>()
  for (const s of index.specimens) {
    const ids = new Set(s.members.flatMap((m) => (m.report ? [m.report] : [])))
    for (const id of ids) {
      const c = counts.get(id) ?? { results: 0, flagged: 0 }
      c.results++
      if (outOfRange(s.shown)) c.flagged++
      counts.set(id, c)
    }
  }
  return [...index.reports.values()]
    .sort(
      (a, b) => Date.parse(b.at) - Date.parse(a.at) || a.id.localeCompare(b.id)
    )
    .map((report) => ({
      report,
      title: reportTitle(index, report),
      ...(counts.get(report.id) ?? { results: 0, flagged: 0 }),
    }))
}

/** "12 results". */
export function reportCountText(results: number): string {
  return `${results.toLocaleString("en-GB")} ${results === 1 ? "result" : "results"}`
}

const rankOf = (f: ReturnType<typeof resultFlag>) =>
  f?.tone === "danger" ? 0 : f?.tone === "warning" ? 1 : f ? 2 : 3

export interface ReportLine {
  specimen: Specimen
  name: string
  /** The value in the subject's unit. */
  value: string
  /** The value as the lab wrote it, where the line shows it converted. */
  asReported?: string
  /** The lab's own verdict only: a report is the lab's document. */
  flag: ReturnType<typeof resultFlag>
}

/** A report's results, flagged ones first, then by name. */
export function reportLines(index: LabIndex, id: string): ReportLine[] {
  return reportSpecimens(index, id)
    .map((s) => {
      const subject = index.subjects.get(s.subject)
      const original =
        s.members.find((m) => m.report === id && !m.derivedFrom) ??
        s.members.find((m) => m.report === id)
      const converted = s.via === "converted" || s.via === "twin"
      const om = original ? index.metrics.get(original.metric) : undefined
      return {
        specimen: s,
        name: seriesName(subject, { code: s.subject, channel: s.channel }),
        value: resultText(
          s.shown,
          s.via === "apart" ? undefined : subject?.precision,
          subject?.unit
        ),
        asReported:
          converted && original
            ? resultText(original, om?.precision, om?.unit)
            : undefined,
        flag: resultFlag(s.shown, undefined, false),
      }
    })
    .sort(
      (a, b) => rankOf(a.flag) - rankOf(b.flag) || a.name.localeCompare(b.name)
    )
}

/** What a report's original document is: "PDF, 84 kB". */
export function fileText(f: StoredFile): string {
  const type = f.mediaType?.split("/")[1]?.toUpperCase() ?? "File"
  return f.size !== undefined ? `${type}, ${formatBytes(f.size)}` : type
}
