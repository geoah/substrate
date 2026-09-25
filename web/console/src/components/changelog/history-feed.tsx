/** History as sentences: "<Actor> changed <Record>", runs folded into
 * "<Actor> added 14 tasks", grouped under the day they happened. Shared by
 * the History page, the actor page and Home's recent changes. Technical mode
 * adds each entry's changelog sequence numbers and property keys; the actor's
 * raw id rides `ActorRef`. */

import { useMemo, type ReactNode } from "react"
import { Link } from "@tanstack/react-router"

import { ActorRef } from "@/components/identity/actor-ref"
import { KindPath } from "@/components/identity/kind-ref"
import { RecordRef } from "@/components/identity/record-ref"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import type { HistoryFeedState } from "@/hooks/use-history-feed"
import type { WatchStatus } from "@/lib/api/changes"
import { splitKind } from "@/lib/api/http"
import type { ChangeRow } from "@/lib/api/types"
import { relativeTime, shortTime } from "@/lib/format"
import {
  foldHistory,
  groupByDay,
  propertyLabel,
  type HistoryEntry,
} from "@/lib/history"
import { displayName, displayPlural } from "@/lib/kind-names"
import { cn } from "@/lib/utils"

function collectionLink(kind: string) {
  const { authority, pkg, name } = splitKind(kind)
  return { authority, pkg, name }
}

/** The object of the sentence: the one record, or "14 tasks" for a run. */
function EntryObject({
  entry,
  openEnded,
}: {
  entry: HistoryEntry
  openEnded?: boolean
}) {
  if (entry.records.length === 1 && !openEnded) {
    return <RecordRef kind={entry.kind} id={entry.records[0]} />
  }
  const count = entry.records.length
  const words =
    count === 1 ? displayName(entry.kind) : displayPlural(entry.kind)
  const { authority, pkg, name } = collectionLink(entry.kind)
  // A run cut by the page may go on in older rows: its count is a floor.
  const label = `${count}${openEnded ? "+" : ""} ${words.toLowerCase()}`
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
  openEnded,
}: {
  entry: HistoryEntry
  /** The feed has older rows, and this is its oldest entry. */
  openEnded?: boolean
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
        <EntryObject entry={entry} openEnded={openEnded} />
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
  more = false,
  className,
}: {
  rows: ChangeRow[]
  limit?: number
  /** Older rows exist beyond `rows`. */
  more?: boolean
  className?: string
}) {
  const [technical] = useTechnicalDetails()
  const { days, oldest } = useMemo(() => {
    // Collecting what a delete left behind is housekeeping, not a change a
    // person made; it reads only with technical details on.
    const entries = foldHistory(
      technical ? rows : rows.filter((r) => r.op !== "gc")
    )
    const oldest = more ? entries[entries.length - 1]?.key : undefined
    return {
      oldest,
      days: groupByDay(limit ? entries.slice(0, limit) : entries),
    }
  }, [rows, limit, technical, more])
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
              openEnded={entry.key === oldest}
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

/** The line that says what an everyday feed left out, so nothing is lost
 * silently: "12 system changes hidden · Show". With `shown`, the way back. */
export function SystemChangesNote({
  hidden,
  shown = false,
  onToggle,
  action,
  className,
}: {
  hidden: number
  shown?: boolean
  onToggle?: () => void
  /** In place of the toggle: somewhere else they can be seen. */
  action?: ReactNode
  className?: string
}) {
  if (!shown && hidden === 0) return null
  return (
    <p
      data-slot="system-changes"
      className={cn("text-[12.5px] text-faint", className)}
    >
      {shown
        ? "Showing system changes"
        : `${hidden} system ${hidden === 1 ? "change" : "changes"} hidden`}
      {" · "}
      {action ?? (
        <button
          type="button"
          onClick={onToggle}
          className="cursor-pointer text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
        >
          {shown ? "Hide" : "Show"}
        </button>
      )}
    </p>
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
  if (!feed.rows.length && !feed.hidden && !feed.hasOlder) {
    return <p className="py-8 text-muted-foreground">{empty}</p>
  }
  return (
    <>
      {feed.rows.length ? (
        <HistorySentences rows={feed.rows} more={feed.hasOlder} />
      ) : (
        <p className="py-8 text-muted-foreground">
          {feed.hasOlder
            ? "Only system changes among the latest ones."
            : "Only system changes so far."}
        </p>
      )}
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
