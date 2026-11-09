import { Row, Section } from "@substrate/react"
import { useMemo } from "react"
import { useBackend } from "../data"
import { fileText, reportLines, reportTitle } from "../labs/reports"
import { capitalize, formatDate, rangeText } from "../lib/format"
import { go, routeHash } from "../route"
import { normalBands, seriesHref } from "../rules/labs"
import { EmptyState } from "../ui/EmptyState"
import { Screen } from "../ui/Screen"
import { SkeletonRow } from "../ui/Skeleton"
import { ReadProblem } from "../ui/labs/ReadProblem"
import { ResultTable } from "../ui/labs/ResultTable"

const back = {
  label: "Lab reports",
  onClick: () => go({ screen: "reports" }),
}

/** One report: who issued it and when, then its results, the ones the lab
 * flagged first. */
export function ReportScreen(props: { id: string }) {
  const labs = useBackend().labs.useLabs()
  const index = labs.data?.index
  const lines = useMemo(
    () => (index ? reportLines(index, props.id) : []),
    [index, props.id]
  )
  if (!index)
    return (
      <Screen title="Lab report" back={back}>
        {labs.error ? (
          <ReadProblem error={labs.error} onRetry={labs.refresh} />
        ) : (
          <div
            role="status"
            aria-label="Loading the report"
            className="overflow-hidden rounded-[10px] bg-card"
          >
            {Array.from({ length: 4 }, (_, i) => (
              <SkeletonRow key={i} hasAvatar={false} />
            ))}
          </div>
        )}
      </Screen>
    )
  const rep = index.reports.get(props.id)
  if (!rep)
    return (
      <Screen title="Lab report" back={back}>
        <EmptyState
          title="This report is not here"
          description="It may have been deleted or undone with its import."
          action={{
            label: "All reports",
            onClick: () => go({ screen: "reports" }),
          }}
        />
      </Screen>
    )
  const facts: [string, string][] = [
    ["Lab", reportTitle(index, rep)],
    ["Collected", formatDate(rep.at)],
    ...(rep.issuedAt
      ? [["Issued", formatDate(rep.issuedAt)] as [string, string]]
      : []),
    ...(rep.accession
      ? [["Accession", rep.accession] as [string, string]]
      : []),
    ...(rep.status
      ? [["Status", capitalize(rep.status)] as [string, string]]
      : []),
    ...(rep.review
      ? [["Review", capitalize(rep.review)] as [string, string]]
      : []),
    ...(rep.file ? [["Original", fileText(rep.file)] as [string, string]] : []),
  ]
  return (
    <Screen
      title={reportTitle(index, rep)}
      subtitle={formatDate(rep.at)}
      back={back}
    >
      <Section
        footer={
          rep.file
            ? "The original document opens in your Substrate's console: an app is never given its files."
            : undefined
        }
      >
        {facts.map(([label, value]) => (
          <Row key={label} label={label} value={value} />
        ))}
      </Section>
      <section aria-label="Results" className="mb-7">
        <h2 className="mb-1.5 flex px-4 text-[13px] tracking-wide text-secondary uppercase">
          <span className="flex-1">Results</span>
          <span className="tabular-nums">{lines.length}</span>
        </h2>
        <ResultTable
          label="Results in this report"
          leadHeader="Test"
          rows={lines.map((l) => {
            const nb = normalBands(l.specimen.shown)[0]
            const precision =
              l.specimen.via === "apart"
                ? undefined
                : index.subjects.get(l.specimen.subject)?.precision
            return {
              key: l.specimen.key,
              lead: (
                <a
                  href={seriesHref({
                    code: l.specimen.subject,
                    channel: l.specimen.channel,
                  })}
                  className="text-accent-text underline-offset-2 hover:underline"
                >
                  {l.name}
                </a>
              ),
              value: l.value,
              twin: l.asReported ? `Reported as ${l.asReported}` : undefined,
              range: nb ? rangeText(nb.low, nb.high, precision) : undefined,
              flag: l.flag,
            }
          })}
          empty={
            <EmptyState
              title="No results in this report"
              variant="compact"
              className="px-4"
            />
          }
        />
      </section>
      <p className="px-4 text-[13px] text-secondary">
        Flags here are the lab's own.{" "}
        <a
          href={routeHash({ screen: "labs", query: "" })}
          className="text-accent-text underline-offset-2 hover:underline"
        >
          Labs
        </a>{" "}
        also reads every result against its targets.
      </p>
    </Screen>
  )
}
