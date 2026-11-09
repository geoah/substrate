import { ProblemBox, Row, Section, cx } from "@substrate/react"
import { useMemo } from "react"
import { useBackend } from "../../data"
import type { ImportRow, LabImport } from "../../kinds"
import { formatDate, formatDateTime } from "../../lib/format"
import { unitLabel } from "../../lib/units"
import { metricIndex, metricName } from "../../sources/list"
import { Badge } from "../Badge"
import { ImportActionsBar } from "../sources/ImportItem"
import { STATUS, countWords, runWords } from "./words"

/** A planned row's value as the lab wrote it: `<0.6 mg/L`, `Not detected`. */
export function rowValue(r: {
  value?: string
  comparator?: string
  unit?: string
  text?: string
}): string {
  if (r.text !== undefined) return r.text
  const u = unitLabel(r.unit)
  const v = `${r.comparator ?? ""}${r.value ?? ""}`
  return u ? (u === "%" ? `${v}%` : `${v} ${u}`) : v
}

/** A row's range, in words. */
export function rowRange(r: {
  low?: string
  high?: string
}): string | undefined {
  if (r.low !== undefined && r.high !== undefined)
    return `Range ${r.low} to ${r.high}`
  if (r.high !== undefined) return `Range up to ${r.high}`
  if (r.low !== undefined) return `Range ${r.low} or more`
  return undefined
}

function PlannedRow(props: { row: ImportRow; metric: string }) {
  const { row } = props
  const range = rowRange(row)
  return (
    <div className="flex min-h-11 flex-wrap items-start gap-x-3 gap-y-0.5 px-4 py-2.5">
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="text-[17px] break-words">
          {row.name ?? props.metric}
        </span>
        {row.name && row.name !== props.metric && (
          <span className="text-[13px] break-words text-secondary">
            As {props.metric}
          </span>
        )}
      </span>
      <span className="flex flex-col items-end">
        <span className="text-[17px] text-secondary tabular-nums">
          {rowValue(row)}
        </span>
        {(range || row.flag) && (
          <span className="text-[13px] text-secondary">
            {[range, row.flag && row.flag !== "N" ? `Flag ${row.flag}` : ""]
              .filter(Boolean)
              .join(", ")}
          </span>
        )}
      </span>
    </div>
  )
}

/** One import: its state and counts, its problem or its run, Resume and
 * Undo, then its plan by report and the rows it left out. */
export function ImportDetail(props: { imp: LabImport }) {
  const { imp } = props
  const sources = useBackend().sources
  const run = sources.useRunState(imp.id)
  const list = sources.useSourceList().data ?? []
  const metrics = metricIndex(sources.useMetrics().data ?? [])
  const still = sources.useStillPointing(imp.id, imp.status === "undone")
  const status = STATUS[imp.status]
  const words = runWords(run)
  const lab = list.find((s) => s.id === imp.source)?.name
  const skipped = useMemo(
    () =>
      (imp.skipped ?? "")
        .split("\n")
        .map((l) => l.replace(/^-\s*/, "").trim())
        .filter(Boolean),
    [imp.skipped]
  )
  const left = still.data ?? 0

  return (
    <>
      <Section>
        <Row
          label="State"
          value={<Badge label={status.word} tone={status.tone} />}
        />
        <Row
          label="Results"
          value={countWords(
            imp.results,
            imp.rows.length,
            imp.status !== "importing" &&
              imp.status !== "stopped" &&
              imp.status !== "undoing"
          )}
        />
        <Row
          label="Reports"
          value={imp.reports.length.toLocaleString("en-GB")}
        />
        {lab && <Row label="Lab" value={lab} />}
        <Row
          label="How"
          value={imp.format === "manual" ? "Entered by hand" : "From a file"}
        />
        {imp.importedAt && (
          <Row label="Imported" value={formatDateTime(imp.importedAt)} />
        )}
        {imp.status === "stopped" && imp.problem && (
          <ProblemBox
            tone="caution"
            title="Why it stopped"
            message={imp.problem}
          />
        )}
        {imp.status === "undone" && left > 0 && (
          <ProblemBox
            tone="caution"
            title="Written after it was undone"
            message={`${left.toLocaleString("en-GB")} ${left === 1 ? "record still points" : "records still point"} at this import: a write that was still in flight landed after the undo. Undo again deletes ${left === 1 ? "it" : "them"}.`}
          />
        )}
        {words && (
          <p
            role="status"
            className={cx(
              "px-4 py-2.5 text-[15px] break-words",
              run.running ? "text-info-ink" : "text-secondary"
            )}
          >
            {words}
          </p>
        )}
        <div className="px-4">
          <ImportActionsBar imp={imp} />
        </div>
      </Section>

      {imp.reports.map((rep) => {
        const rows = imp.rows.filter((r) => r.report === rep.key)
        return (
          <Section
            key={rep.key}
            header={[
              formatDate(rep.takenAt),
              rep.accession ? `accession ${rep.accession}` : undefined,
            ]
              .filter(Boolean)
              .join(", ")}
          >
            {rows.map((r) => (
              <PlannedRow
                key={r.key}
                row={r}
                metric={metricName(r.metric, metrics)}
              />
            ))}
          </Section>
        )
      })}

      {skipped.length > 0 && (
        <Section header="Left out">
          {skipped.map((line, i) => (
            <p key={i} className="px-4 py-2.5 text-[15px] break-words">
              {line}
            </p>
          ))}
        </Section>
      )}
    </>
  )
}
