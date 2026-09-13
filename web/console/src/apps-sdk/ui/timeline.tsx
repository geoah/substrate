import { Fragment, type KeyboardEvent } from "react"

import type { SubstrateRecord } from "@/lib/api/types"
import { Badge } from "./badge"
import {
  dayKey,
  dayLabel,
  groupByDay,
  timeSpan,
  timelineRows,
  titleOf,
  type TimelineRow,
} from "./buckets"
import { Empty } from "./empty"
import { useNow } from "./now"
import { host, type Page } from "./sdk"
import { Spinner } from "./spinner"

export interface TimelineProps<T extends SubstrateRecord> {
  page: Page & { loadMore?(): void }
  /** A row tap; without it a tap opens the console's record page. */
  onTap?(record: T): void
  /** The word a row's kind badge shows; the kind's own name by default. */
  kindLabel?(record: T): string
}

function kindName(record: SubstrateRecord): string {
  return record.kind.split("/").pop() ?? record.kind
}

/** Records sectioned by the local day of their bound point (`at`, else
 * `dueAt`), empty days left out, today marked (with a line where it would
 * fall when it has nothing), a range shown as a time span, a kind badge per
 * row, and a note when the page was cut. */
export function Timeline<T extends SubstrateRecord>({
  page,
  onTap,
  kindLabel,
}: TimelineProps<T>) {
  const now = useNow()
  const rows = timelineRows((page.records as T[] | undefined) ?? [])
  const days = groupByDay(rows)
  const today = dayKey(now)

  if (!rows.length) {
    if (page.loading) {
      return (
        <div className="kit-loading">
          <Spinner />
        </div>
      )
    }
    if (page.error) {
      return <Empty title="Could not load" description={page.error.message} />
    }
    return <Empty title="Nothing scheduled" />
  }

  const open = (row: TimelineRow<T>) => {
    if (onTap) onTap(row.record)
    else {
      void host.navigate({
        record: { kind: row.record.kind, id: row.record.id },
      })
    }
  }
  const onKey = (e: KeyboardEvent, row: TimelineRow<T>) => {
    if (e.key !== "Enter" && e.key !== " ") return
    e.preventDefault()
    open(row)
  }

  const hasToday = days.some((d) => d.key === today)
  // Where a line for today goes when today has no rows: before the first
  // later day, or after the last one when everything is past.
  const nowBefore = hasToday ? undefined : days.find((d) => d.key > today)?.key
  const nowLast = !hasToday && nowBefore === undefined

  return (
    <div className="kit-timeline">
      {days.map((day) => {
        const isToday = day.key === today
        return (
          <Fragment key={day.key}>
            {nowBefore === day.key && <NowLine />}
            <section
              className="kit-day"
              aria-label={dayLabel(day.key, now)}
              aria-current={isToday ? "date" : undefined}
            >
              <div
                className={
                  isToday
                    ? "kit-list-header kit-list-header--today"
                    : "kit-list-header"
                }
              >
                <span>{dayLabel(day.key, now)}</span>
                <span className="kit-list-count">{day.rows.length}</span>
              </div>
              <div role="list">
                {day.rows.map((row) => (
                  <div
                    key={`${row.record.kind}/${row.record.id}`}
                    role="button"
                    tabIndex={0}
                    className="kit-timeline-row kit-timeline-row--tappable"
                    onClick={() => open(row)}
                    onKeyDown={(e) => onKey(e, row)}
                  >
                    <span className="kit-timeline-time">
                      {timeSpan(row.at, row.endsAt)}
                    </span>
                    <span className="kit-timeline-body">
                      <span className="kit-timeline-title">
                        {titleOf(row.record)}
                      </span>
                      <span className="kit-timeline-tags">
                        <Badge tone="muted">
                          {kindLabel?.(row.record) ?? kindName(row.record)}
                        </Badge>
                      </span>
                    </span>
                  </div>
                ))}
              </div>
            </section>
          </Fragment>
        )
      })}
      {nowLast && <NowLine />}
      {page.cursor && page.loadMore && (
        <button
          type="button"
          className="kit-btn kit-list-more"
          disabled={page.loading}
          onClick={() => page.loadMore?.()}
        >
          {page.loading ? "Loading…" : "Load more"}
        </button>
      )}
      {(page.incomplete || (page.cursor && !page.loadMore)) && (
        <p className="kit-list-foot">
          Showing the first {rows.length}; more were not loaded.
        </p>
      )}
      {page.error && (
        <p className="kit-list-foot">Could not refresh: {page.error.message}</p>
      )}
    </div>
  )
}

function NowLine() {
  return (
    <div className="kit-now" aria-label="Today" role="separator">
      Today
    </div>
  )
}
