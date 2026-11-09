import { NoteRow, Row, Section } from "@substrate/react"
import type { MetricRecord } from "../../catalogue/types"
import type { LeftOut, PlannedRecord } from "../../import/importers/types"
import { KIND } from "../../kinds"
import { formatDate } from "../../lib/format"
import { unitLabel } from "../../lib/units"
import { metricName } from "../../sources/list"
import { kindCount, kindHeading } from "./words"

/** What a plan holds, as a preview or an import's page reads it: an
 * importer's plan before it is written, or the plan an import keeps. */
export interface PlanView {
  records: readonly PlannedRecord[]
  documents: readonly { digest: string; name: string }[]
  left: readonly LeftOut[]
}

/** The most reports listed one by one; the rest are counted. */
const REPORTS_SHOWN = 30

/** A result's value as the lab wrote it: `<0.6 mg/L`, `Not detected`. */
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

/** A result's range, in words. */
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

function quantity(v: unknown): { value?: string; unit?: string } {
  if (!v || typeof v !== "object") return {}
  const o = v as Record<string, unknown>
  return {
    value: typeof o.value === "string" ? o.value : undefined,
    unit: typeof o.unit === "string" ? o.unit : undefined,
  }
}

/** A planned result's line: its name, the metric it is written under,
 * its value and range. */
function ResultLine(props: {
  rec: PlannedRecord
  metrics: ReadonlyMap<string, MetricRecord>
}) {
  const p = props.rec.properties
  const metricPath = typeof p.metric === "string" ? p.metric : ""
  const metricId = metricPath.startsWith(`${KIND.metric}/`)
    ? metricPath.slice(KIND.metric.length + 1)
    : metricPath
  const metric = metricId ? metricName(metricId, props.metrics) : ""
  const coding = p.coding as { display?: unknown } | undefined
  const name =
    typeof coding?.display === "string" ? coding.display : metric || "Result"
  const v = p.value as Record<string, unknown> | undefined
  const value = rowValue({
    ...quantity(v),
    comparator: typeof v?.comparator === "string" ? v.comparator : undefined,
    text: typeof p.text === "string" ? p.text : undefined,
  })
  const ranges = Array.isArray(p.referenceRanges)
    ? (p.referenceRanges as Record<string, unknown>[])
    : []
  const range = ranges[0]
    ? rowRange({
        low: quantity(ranges[0].low).value,
        high: quantity(ranges[0].high).value,
      })
    : undefined
  const flags = Array.isArray(p.interpretation)
    ? (p.interpretation as { code?: unknown }[])
        .map((i) => (typeof i.code === "string" ? i.code : ""))
        .filter((c) => c && c !== "N")
    : []
  return (
    <div className="flex min-h-11 flex-wrap items-start gap-x-3 gap-y-0.5 px-4 py-2.5">
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="text-[17px] break-words">{name}</span>
        {metric && name !== metric && (
          <span className="text-[13px] break-words text-secondary">
            As {metric}
          </span>
        )}
      </span>
      <span className="flex flex-col items-end">
        <span className="text-[17px] break-words text-secondary tabular-nums">
          {value}
        </span>
        {(range || flags.length > 0) && (
          <span className="text-[13px] text-secondary">
            {[range, ...flags.map((f) => `Flag ${f}`)]
              .filter(Boolean)
              .join(", ")}
          </span>
        )}
      </span>
    </div>
  )
}

function reportHeader(rec: PlannedRecord): string {
  const p = rec.properties
  return [
    typeof p.at === "string" ? formatDate(p.at) : undefined,
    typeof p.lab === "string" ? p.lab : undefined,
    typeof p.accession === "string" ? `accession ${p.accession}` : undefined,
  ]
    .filter(Boolean)
    .join(", ")
}

/** A plan's records by kind, its documents, its reports with their
 * results, and what was left out and why. Whether each record is created
 * or updated is decided as it is written, so the counts are by kind. */
export function PlanPreview(props: {
  plan: PlanView
  metrics: ReadonlyMap<string, MetricRecord>
  /** Lists each report with its results. */
  reports?: boolean
}) {
  const { plan } = props
  const kinds: string[] = []
  const counts = new Map<string, number>()
  for (const r of plan.records) {
    if (!counts.has(r.kind)) kinds.push(r.kind)
    counts.set(r.kind, (counts.get(r.kind) ?? 0) + 1)
  }
  const reports = plan.records.filter((r) => r.kind === KIND.labreport)
  const results = plan.records.filter((r) => r.kind === KIND.labresult)
  const resultsOf = (rep: PlannedRecord) =>
    results.filter((r) => {
      const ref = r.properties.report as
        { $ref?: { key?: unknown; namespace?: unknown } } | undefined
      return ref?.$ref?.key === rep.key && ref.$ref.namespace === rep.namespace
    })
  return (
    <>
      <Section
        header="What it writes"
        footer="Whether each record is created or updated is decided as it is written: one already there from an earlier import is compared, and an older export never overwrites a newer one."
      >
        {kinds.map((k) => (
          <Row
            key={k}
            label={kindHeading(k)}
            value={(counts.get(k) ?? 0).toLocaleString("en-GB")}
          />
        ))}
        {!kinds.length && <NoteRow tone="caution">Nothing to write.</NoteRow>}
        {plan.documents.length > 0 && (
          <Row
            label="Documents"
            value={plan.documents.length.toLocaleString("en-GB")}
          />
        )}
      </Section>

      {props.reports !== false &&
        reports.slice(0, REPORTS_SHOWN).map((rep) => {
          const mine = resultsOf(rep)
          return (
            <Section
              key={`${rep.namespace}/${rep.key}`}
              header={reportHeader(rep) || `Report ${rep.key}`}
            >
              {mine.map((r) => (
                <ResultLine
                  key={`${r.namespace}/${r.key}`}
                  rec={r}
                  metrics={props.metrics}
                />
              ))}
              {!mine.length && <NoteRow>No results of its own.</NoteRow>}
            </Section>
          )
        })}
      {props.reports !== false && reports.length > REPORTS_SHOWN && (
        <p className="mb-7 px-4 text-[15px] text-secondary">
          {`And ${kindCount(KIND.labreport, reports.length - REPORTS_SHOWN)} more.`}
        </p>
      )}

      {plan.documents.length > 0 && (
        <Section
          header="Documents"
          footer="Each is stored under a name of its own, never the file's, which can carry your name and birth date."
        >
          {plan.documents.slice(0, 20).map((d) => (
            <p key={d.digest} className="px-4 py-2.5 text-[15px] break-words">
              {d.name}
            </p>
          ))}
          {plan.documents.length > 20 && (
            <NoteRow>
              {`And ${(plan.documents.length - 20).toLocaleString("en-GB")} more.`}
            </NoteRow>
          )}
        </Section>
      )}

      {plan.left.length > 0 && (
        <Section header="Left out">
          {plan.left.map((l, i) => (
            <div key={i} className="flex flex-col gap-0.5 px-4 py-2.5">
              <span className="text-[17px] break-words">{l.what}</span>
              <span className="text-[13px] break-words text-secondary">
                {l.why}
              </span>
            </div>
          ))}
        </Section>
      )}
    </>
  )
}
