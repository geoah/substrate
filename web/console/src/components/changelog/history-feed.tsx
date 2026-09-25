/** History as sentences: "<Actor> changed <Record>", runs folded into
 * "<Actor> added 14 tasks", grouped under the day they happened. Shared by
 * the History page, the actor page and Home's recent changes. Technical mode
 * adds each entry's changelog sequence numbers and property keys; the actor's
 * raw id rides `ActorRef`. */

import { useEffect, useMemo, useRef, useState } from "react"
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import { ActorRef } from "@/components/identity/actor-ref"
import { KindPath } from "@/components/identity/kind-ref"
import { RecordRef } from "@/components/identity/record-ref"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import {
  changesInfiniteOptions,
  watchChanges,
  type ChangeFeedFilter,
  type WatchStatus,
} from "@/lib/api/changes"
import { splitKind } from "@/lib/api/http"
import type { ChangeRow } from "@/lib/api/types"
import { mergeFeed } from "@/lib/changelog"
import { relativeTime, shortTime } from "@/lib/format"
import {
  foldHistory,
  groupByDay,
  propertyLabel,
  type HistoryEntry,
} from "@/lib/history"
import { displayName, displayPlural } from "@/lib/kind-names"
import { cn } from "@/lib/utils"

/** Rows one history page reads: enough to fill a screen with sentences once
 * the runs fold. */
const HISTORY_PAGE = 100

export interface HistoryFeedState {
  rows: ChangeRow[]
  status: WatchStatus
  isPending: boolean
  error: Error | null
  hasOlder: boolean
  loadingOlder: boolean
  loadOlder: () => void
  retry: () => void
}

/** The change feed for one filter, newest first, with the live tail joined on
 * top while `live` is on. `enabled: false` reads nothing (a view whose actor
 * set is empty). */
export function useHistoryFeed(
  filter: ChangeFeedFilter,
  { live = true, enabled = true, first = HISTORY_PAGE } = {}
): HistoryFeedState {
  const queryClient = useQueryClient()
  const history = useInfiniteQuery({
    ...changesInfiniteOptions(filter, { first }),
    enabled,
  })
  const [liveRows, setLiveRows] = useState<ChangeRow[]>([])
  const [status, setStatus] = useState<WatchStatus>("off")
  const [nonce, setNonce] = useState(0)
  const filterKey = JSON.stringify(filter)
  const [lastKey, setLastKey] = useState(filterKey)
  if (lastKey !== filterKey) {
    setLastKey(filterKey)
    setLiveRows([])
  }

  const pages = history.data?.pages
  const historyRows = useMemo(
    () => (pages ?? []).flatMap((p) => p.changes),
    [pages]
  )
  // The tail resumes above the newest row the first page read, under that
  // page's generation, so nothing between the read and the tail is lost.
  const head = pages?.[0]
  const resume = useRef<{ from?: number; generation?: string }>({})
  useEffect(() => {
    if (head) {
      resume.current = {
        from: head.changes[0]?.seq ?? head.head,
        generation: head.generation,
      }
    }
  }, [head])
  const ready = Boolean(head)
  useEffect(() => {
    if (!live || !enabled || !ready) {
      setStatus("off")
      return undefined
    }
    const handle = watchChanges({
      from: resume.current.from,
      generation: resume.current.generation,
      filter: JSON.parse(filterKey) as ChangeFeedFilter,
      onRow: (row) =>
        setLiveRows((rows) =>
          rows.some((r) => r.seq === row.seq) ? rows : [row, ...rows]
        ),
      onStatus: (next) => setStatus(next),
      onCompacted: () => {
        // The cursor no longer addresses this history: re-list from the head
        // and re-open the tail there.
        resume.current = {}
        setLiveRows([])
        void queryClient.invalidateQueries({ queryKey: ["changes", "feed"] })
        setNonce((n) => n + 1)
      },
    })
    return () => handle.stop()
  }, [live, enabled, ready, filterKey, nonce, queryClient])

  const rows = useMemo(
    () => mergeFeed(liveRows, historyRows),
    [liveRows, historyRows]
  )
  return {
    rows,
    status,
    isPending: enabled && history.isPending,
    error: history.error,
    hasOlder: Boolean(history.hasNextPage),
    loadingOlder: history.isFetchingNextPage,
    loadOlder: () => void history.fetchNextPage(),
    retry: () => void history.refetch(),
  }
}

function collectionLink(kind: string) {
  const { authority, pkg, name } = splitKind(kind)
  return { authority, pkg, name }
}

/** The object of the sentence: the one record, or "14 tasks" for a run. */
function EntryObject({ entry }: { entry: HistoryEntry }) {
  if (entry.records.length === 1) {
    return <RecordRef kind={entry.kind} id={entry.records[0]} />
  }
  const count = entry.records.length
  const words =
    count === 1 ? displayName(entry.kind) : displayPlural(entry.kind)
  const { authority, pkg, name } = collectionLink(entry.kind)
  const label = `${count} ${words.toLowerCase()}`
  if (!authority || !pkg || !name) return <span>{label}</span>
  return (
    <Link
      to="/data/$authority/$pkg/$name"
      params={{ authority, pkg, name }}
      className="text-primary-text no-underline hover:underline"
    >
      {label}
    </Link>
  )
}

function seqLabel(entry: HistoryEntry): string {
  const newest = entry.rows[0].seq
  const oldest = entry.rows[entry.rows.length - 1].seq
  return newest === oldest ? `#${newest}` : `#${oldest}–${newest}`
}

export function HistoryEntryRow({
  entry,
  today,
}: {
  entry: HistoryEntry
  /** Today's entries read as "3m ago"; older ones by the time of day, under
   * their day's heading. */
  today: boolean
}) {
  const [technical] = useTechnicalDetails()
  const changed = entry.verb === "changed" && entry.properties.length > 0
  return (
    <div
      data-slot="history-entry"
      className="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-2.5 border-b border-border py-[9px]"
    >
      <div className="min-w-0 leading-[1.6]">
        <ActorRef actor={entry.actor} /> <span>{entry.verb}</span>{" "}
        <EntryObject entry={entry} />
        {(changed || technical) && (
          <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[12.5px] text-faint">
            {changed &&
              (technical ? (
                <span className="font-mono text-[11.5px]">
                  {entry.properties.join(", ")}
                </span>
              ) : (
                <span>{entry.properties.map(propertyLabel).join(" · ")}</span>
              ))}
            {technical && (
              <>
                <span className="tabular-nums">{seqLabel(entry)}</span>
                {entry.records.length > 1 && (
                  <KindPath reference={entry.kind} className="text-[11px]" />
                )}
              </>
            )}
          </div>
        )}
      </div>
      <span
        className="pt-px text-[12.5px] whitespace-nowrap text-faint tabular-nums"
        title={entry.ts}
      >
        {today ? relativeTime(entry.ts) : shortTime(entry.ts)}
      </span>
    </div>
  )
}

/** The sentences, grouped by day. `limit` caps the entries (Home shows a
 * few); the full page shows every loaded one. */
export function HistorySentences({
  rows,
  limit,
  className,
}: {
  rows: ChangeRow[]
  limit?: number
  className?: string
}) {
  const days = useMemo(() => {
    const entries = foldHistory(rows)
    return groupByDay(limit ? entries.slice(0, limit) : entries)
  }, [rows, limit])
  return (
    <div data-slot="history" className={cn("flex flex-col", className)}>
      {days.map((day) => (
        <section key={day.key} aria-label={day.label}>
          <h3 className="pt-[18px] pb-1 text-xs font-semibold text-faint first:pt-2">
            {day.label}
          </h3>
          {day.entries.map((entry) => (
            <HistoryEntryRow
              key={entry.key}
              entry={entry}
              today={day.label === "Today"}
            />
          ))}
        </section>
      ))}
    </div>
  )
}

export function HistorySkeleton({ rows = 6 }: { rows?: number }) {
  return (
    <div className="flex flex-col gap-3 pt-3" aria-busy>
      <Skeleton className="h-3 w-16" />
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton key={i} className="h-5 w-full" />
      ))}
    </div>
  )
}

/** A feed with its own paging: the sentences, then "Show older changes". */
export function HistoryFeed({
  feed,
  empty,
}: {
  feed: HistoryFeedState
  empty: string
}) {
  if (feed.isPending) return <HistorySkeleton />
  if (feed.error) {
    return (
      <div className="flex flex-col items-start gap-2 py-6 text-muted-foreground">
        <p>History didn’t load: {feed.error.message}</p>
        <Button variant="outline" size="sm" onClick={feed.retry}>
          Try again
        </Button>
      </div>
    )
  }
  if (!feed.rows.length) {
    return <p className="py-8 text-muted-foreground">{empty}</p>
  }
  return (
    <>
      <HistorySentences rows={feed.rows} />
      {feed.hasOlder && (
        <div className="pt-4">
          <Button
            variant="outline"
            size="sm"
            disabled={feed.loadingOlder}
            onClick={feed.loadOlder}
          >
            {feed.loadingOlder ? "Loading…" : "Show older changes"}
          </Button>
        </div>
      )}
    </>
  )
}

/** Where the live tail stands, in words. */
export function LiveStatus({ status }: { status: WatchStatus }) {
  const words: Record<WatchStatus, string> = {
    live: "Live",
    connecting: "Connecting…",
    retrying: "Reconnecting…",
    compacted: "Catching up…",
    stopped: "Stopped",
    off: "Paused",
  }
  return (
    <span className="inline-flex items-center gap-1.5 text-[12.5px] text-faint">
      <span
        aria-hidden
        className={cn(
          "size-1.5 rounded-full",
          status === "live" ? "bg-ok" : "bg-border-strong",
          (status === "connecting" || status === "retrying") && "animate-pulse"
        )}
      />
      {words[status]}
    </span>
  )
}
