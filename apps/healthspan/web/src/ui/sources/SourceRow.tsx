import { ChevronRight } from "lucide-react"
import type { MetricRecord } from "../../catalogue/types"
import type { SourceSummary } from "../../data/sources"
import { ago } from "../../lib/format"
import { routeHash } from "../../route"
import { metricName, newest } from "../../sources/list"

/** This moment on the app's day: the clock's time of day on `today`, which
 * the demo pins. */
export function nowOn(today: Date): Date {
  const now = new Date()
  return new Date(
    today.getFullYear(),
    today.getMonth(),
    today.getDate(),
    now.getHours(),
    now.getMinutes()
  )
}

/** What a source reports, in words: a few metric names and how many more. */
export function reportsWords(
  s: SourceSummary,
  metrics: ReadonlyMap<string, MetricRecord>,
  max = 3
): string | undefined {
  const names = s.metrics.map((m) => metricName(m.metric, metrics))
  if (!names.length) return undefined
  if (names.length <= max) return names.join(", ")
  return `${names.slice(0, max).join(", ")} and ${names.length - max} more`
}

/** Its nights, workouts and reports, counted. */
export function heldWords(s: SourceSummary): string | undefined {
  const n = (x: number, one: string, many: string) =>
    `${x.toLocaleString("en-GB")} ${x === 1 ? one : many}`
  const parts = [
    s.nights ? n(s.nights, "night", "nights") : undefined,
    s.workouts ? n(s.workouts, "workout", "workouts") : undefined,
    s.reports ? n(s.reports, "lab report", "lab reports") : undefined,
  ].filter(Boolean)
  return parts.length ? parts.join(", ") : undefined
}

/** One source on the list: its name, what it reports, what it holds, and
 * when its newest value is from; it opens the source's page. */
export function SourceRow(props: {
  source: SourceSummary
  metrics: ReadonlyMap<string, MetricRecord>
  today: Date
}) {
  const { source: s } = props
  const reports = reportsWords(s, props.metrics)
  const held = heldWords(s)
  const last = newest(s)
  return (
    <a
      href={routeHash({ screen: "source", id: s.id })}
      className="flex min-h-11 w-full items-center gap-3 px-4 py-2.5 active:bg-selected"
    >
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="text-[17px] break-words">{s.name}</span>
        {reports && (
          <span className="text-[15px] break-words text-secondary">
            {reports}
          </span>
        )}
        {held && <span className="text-[15px] text-secondary">{held}</span>}
        <span className="text-[13px] text-secondary">
          {last
            ? `Newest value ${ago(last, nowOn(props.today))}`
            : "No values read yet"}
        </span>
      </span>
      <ChevronRight className="size-4 shrink-0 text-tertiary" aria-hidden />
    </a>
  )
}
