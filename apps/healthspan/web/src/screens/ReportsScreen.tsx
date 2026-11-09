import { ChevronRight } from "lucide-react"
import { useMemo } from "react"
import { useBackend } from "../data"
import { reportCountText, reportSummaries } from "../labs/reports"
import { formatDate } from "../lib/format"
import { go, routeHash } from "../route"
import { Badge } from "../ui/Badge"
import { EmptyState } from "../ui/EmptyState"
import { Screen } from "../ui/Screen"
import { SkeletonRow } from "../ui/Skeleton"
import { ReadProblem } from "../ui/labs/ReadProblem"

/** Every lab report, newest first, with how many of its results the lab
 * flagged. Rows, not a table: each report is a place to go, its lab above
 * its facts. */
export function ReportsScreen() {
  const labs = useBackend().labs.useLabs()
  const index = labs.data?.index
  const rows = useMemo(() => (index ? reportSummaries(index) : []), [index])
  return (
    <Screen
      title="Lab reports"
      subtitle={
        index
          ? `${rows.length.toLocaleString("en-GB")} ${rows.length === 1 ? "report" : "reports"}`
          : undefined
      }
      back={{ label: "Labs", onClick: () => go({ screen: "labs", query: "" }) }}
    >
      {!index ? (
        labs.error ? (
          <ReadProblem error={labs.error} onRetry={labs.refresh} />
        ) : (
          <div
            role="status"
            aria-label="Loading the reports"
            className="overflow-hidden rounded-[10px] bg-card"
          >
            {Array.from({ length: 5 }, (_, i) => (
              <SkeletonRow key={i} hasAvatar={false} />
            ))}
          </div>
        )
      ) : rows.length === 0 ? (
        <EmptyState
          title="No lab reports yet"
          description="Import a lab report or connect a provider that sends them, on Sources."
          action={{
            label: "Open Sources",
            onClick: () => go({ screen: "sources" }),
          }}
        />
      ) : (
        <ul
          aria-label="Lab reports"
          className="overflow-hidden rounded-[10px] bg-card [&>li+li]:border-t [&>li+li]:border-separator/40"
        >
          {rows.map(({ report, title, results, flagged }) => (
            <li key={report.id}>
              <a
                href={routeHash({ screen: "report", id: report.id })}
                className="flex min-h-11 items-center gap-3 px-4 py-2.5 hover:bg-fill active:bg-selected"
              >
                <span className="flex min-w-0 flex-1 flex-col">
                  <span className="text-[17px] break-words">{title}</span>
                  <span className="text-[13px] text-secondary">
                    {reportCountText(results)}
                    {report.accession ? `, accession ${report.accession}` : ""}
                  </span>
                </span>
                <span className="flex shrink-0 flex-col items-end gap-1">
                  <time
                    dateTime={report.at}
                    className="text-[15px] text-secondary tabular-nums"
                  >
                    {formatDate(report.at)}
                  </time>
                  {flagged > 0 && (
                    <Badge label={`${flagged} flagged`} tone="danger" />
                  )}
                </span>
                <ChevronRight
                  className="size-4 shrink-0 text-tertiary"
                  aria-hidden
                />
              </a>
            </li>
          ))}
        </ul>
      )}
    </Screen>
  )
}
