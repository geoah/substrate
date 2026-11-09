import { describe } from "@substrate/client"
import { useBackend } from "../data"
import type { Read } from "../data/read"
import type { SourceSummary } from "../data/sources"
import { go } from "../route"
import { sortFindings, type Finding } from "../rules/types"
import { EmptyState } from "../ui/EmptyState"
import { Screen } from "../ui/Screen"
import { SkeletonRow } from "../ui/Skeleton"
import { FindingRow } from "../ui/labs/FindingRow"

interface Group {
  title: string
  read: Read<Finding[]>
  /** What the group says with nothing in it. */
  empty: string
}

/** Whether the repository holds nothing the app reads yet: no lab result,
 * and no source with a value, a night, a workout or a report. Unknown
 * while the sources are read. */
function holdsNothing(
  labResults: number | undefined,
  sources: SourceSummary[] | undefined
): boolean {
  return (
    labResults === 0 &&
    !!sources &&
    sources.every(
      (s) => !s.metrics.length && !s.nights && !s.workouts && !s.reports
    )
  )
}

/** Today: every finding, the most urgent first in each part of the app,
 * each opening what it is about. */
export function TodayScreen() {
  const backend = useBackend()
  const labs = backend.labs.useLabs()
  const labFindings = backend.labs.useLabFindings()
  const healthspan = backend.healthspan.useHealthspanFindings()
  const record = backend.record.useRecordFindings()
  const hasRecord = backend.record.useHasRecord()
  const sources = backend.sources.useSourceList()

  const groups: Group[] = [
    {
      title: "Labs",
      read: labFindings,
      empty: "Nothing out of range or overdue",
    },
    { title: "Healthspan", read: healthspan, empty: "Nothing to look at" },
    ...(hasRecord || record.data?.length
      ? [{ title: "Record", read: record, empty: "Nothing new or due" }]
      : []),
  ]
  const sorted = groups.map((g) =>
    g.read.data ? sortFindings(g.read.data) : undefined
  )
  const settled = groups.every((g) => g.read.data !== undefined)
  const total = sorted.reduce((n, fs) => n + (fs?.length ?? 0), 0)
  const empty = holdsNothing(labs.data?.index.results.length, sources.data)

  return (
    <Screen
      title="Today"
      subtitle={
        empty
          ? undefined
          : total > 0
            ? `${total.toLocaleString("en-GB")} ${total === 1 ? "finding" : "findings"}, the most urgent first`
            : settled
              ? "Nothing needs attention"
              : undefined
      }
    >
      {empty ? (
        <EmptyState
          title="Nothing here yet"
          description="Connect a provider, or import a lab report, on Sources: what needs attention shows here."
          action={{
            label: "Open Sources",
            onClick: () => go({ screen: "sources" }),
          }}
        />
      ) : (
        groups.map((g, i) => {
          const findings = sorted[i]
          return (
            <section key={g.title} className="mb-7" aria-label={g.title}>
              <h2 className="mb-1.5 flex px-4 text-[13px] tracking-wide text-secondary uppercase">
                <span className="flex-1">{g.title}</span>
                {findings && (
                  <span className="tabular-nums">{findings.length}</span>
                )}
              </h2>
              <div className="overflow-hidden rounded-[10px] bg-card">
                {findings ? (
                  findings.length ? (
                    <ul
                      aria-label={g.title}
                      className="[&>li+li]:border-t [&>li+li]:border-separator/40"
                    >
                      {findings.map((f) => (
                        <li key={`${f.section}:${f.subject}`}>
                          <FindingRow finding={f} />
                        </li>
                      ))}
                    </ul>
                  ) : (
                    <EmptyState
                      title={g.empty}
                      variant="compact"
                      className="px-4"
                    />
                  )
                ) : g.read.error ? (
                  <EmptyState
                    title={`Could not read: ${describe(g.read.error)}`}
                    variant="compact"
                    className="px-4"
                    action={{ label: "Try Again", onClick: g.read.refresh }}
                  />
                ) : (
                  <div role="status" aria-label={`Loading ${g.title}`}>
                    <SkeletonRow hasAvatar={false} />
                    <SkeletonRow hasAvatar={false} />
                  </div>
                )}
              </div>
            </section>
          )
        })
      )}
    </Screen>
  )
}
