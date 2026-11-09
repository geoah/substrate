import { ProblemBox, Row, Section, Spinner, cx } from "@substrate/react"
import { useMemo } from "react"
import { useBackend } from "../../data"
import type { ImportRecord } from "../../kinds"
import { formatDateTime } from "../../lib/format"
import { metricIndex } from "../../sources/list"
import { Badge } from "../Badge"
import { ImportActionsBar } from "../sources/ImportItem"
import { PlanPreview } from "./PlanPreview"
import {
  IMPORTER_WORDS,
  STATUS,
  countsWords,
  createdWords,
  runWords,
} from "./words"

/** One import: its state, the latest run's counts and what it created,
 * its problem or its run, its actions, then its plan: what it writes, its
 * documents and what was left out. */
export function ImportDetail(props: { imp: ImportRecord }) {
  const { imp } = props
  const sources = useBackend().sources
  const run = sources.useRunState(imp.id)
  const list = sources.useSourceList().data ?? []
  const metrics = metricIndex(sources.useMetrics().data ?? [])
  const created = sources.useCreated(imp).data
  const plan = sources.usePlan(imp)
  const status = STATUS[imp.status]
  const words = runWords(run)
  const source = list.find((s) => s.id === imp.source)?.name
  const skipped = useMemo(
    () =>
      (imp.skipped ?? "")
        .split("\n")
        .map((l) => l.replace(/^-\s*/, "").trim())
        .filter(Boolean),
    [imp.skipped]
  )
  const counts = countsWords(imp.counts)
  const left = created ?? 0

  return (
    <>
      <Section>
        <Row
          label="State"
          value={<Badge label={status.word} tone={status.tone} />}
        />
        {imp.importer && (
          <Row label="How" value={IMPORTER_WORDS[imp.importer]} />
        )}
        {source && <Row label={imp.lab ? "Lab" : "Source"} value={source} />}
        {imp.status === "staging" && (
          <Row
            label="Documents sent"
            value={imp.documents.length.toLocaleString("en-GB")}
          />
        )}
        {imp.importedAt && (
          <Row label="Imported" value={formatDateTime(imp.importedAt)} />
        )}
        {counts && imp.status !== "staging" && (
          <Row label="Latest run" value={counts} />
        )}
        {created !== undefined && imp.status !== "staging" && (
          <Row label="Records it created" value={createdWords(created)} />
        )}
        {imp.status === "stopped" && imp.problem && (
          <ProblemBox
            tone="caution"
            title="Why it stopped"
            message={imp.problem}
          />
        )}
        {imp.status !== "stopped" && imp.problem && (
          <ProblemBox
            tone="caution"
            title="Its last run"
            message={imp.problem}
          />
        )}
        {imp.status === "undone" && left > 0 && (
          <ProblemBox
            tone="caution"
            title="Written after it was undone"
            message={`${createdWords(left)} still point at this import: a write that was still in flight landed after the undo. Undo Again deletes ${left === 1 ? "it" : "them"}.`}
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

      {plan.data ? (
        <PlanPreview plan={{ ...plan.data, left: [] }} metrics={metrics} />
      ) : plan.loading ? (
        <div className="mb-7 flex items-center justify-center gap-2 py-6 text-[15px] text-secondary">
          <Spinner /> Reading its plan…
        </div>
      ) : imp.status === "staging" ? (
        <p className="mb-7 px-4 text-[15px] text-secondary">
          Its plan is sent once every document is.
        </p>
      ) : null}

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
