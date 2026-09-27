/** History as sentences: "<Actor> changed <Record>", a run the server
 * summarized as "<Actor> added 14 tasks", grouped under the day they
 * happened. Shared by
 * the History page, the actor page and Home's recent changes. The
 * substrate's own records read as what they are to a person ("Google updated
 * its package to version 35"). Technical mode adds a line after the sentence
 * with each entry's changelog sequence numbers, the raw actor id and the
 * property keys, the first two with copy buttons. A change to one record
 * says its values ("Priority: High → Urgent") where the server sends them,
 * and the property names where it does not; a run's rows are read for that
 * only when the sentence needs them. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import { ActorRef } from "@/components/identity/actor-ref"
import { ValueMoves } from "@/components/changelog/value-moves"
import { CopyButton } from "@/components/identity/copy-button"
import { KindPath, KindRef } from "@/components/identity/kind-ref"
import { RecordRef } from "@/components/identity/record-ref"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import type { HistoryFeedState } from "@/hooks/use-history-feed"
import { runRowsQueryOptions, type WatchStatus } from "@/lib/api/changes"
import { splitKind } from "@/lib/api/http"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { hostWritten, netMoves, valueSpecs } from "@/lib/change-values"
import { relativeTime, shortTime } from "@/lib/format"
import {
  groupByDay,
  namedProperties,
  propertyLabel,
  rowsComplete,
  runNeedsRows,
  systemPhrase,
  withRunRows,
  type HistoryEntry,
  type SystemPhrase,
} from "@/lib/history"
import { displayName, displayPlural, lowerFirst } from "@/lib/kind-names"
import { cn } from "@/lib/utils"

function collectionLink(kind: string) {
  const { authority, pkg, name } = splitKind(kind)
  return { authority, pkg, name }
}

/** The object of the sentence: the one record, or "14 tasks" for a run. A
 * run the loaded rows may cut short (only against a server that makes no
 * runs) is said without a count ("added tasks") rather than with one that
 * may be wrong. */
function EntryObject({ entry }: { entry: HistoryEntry }) {
  const openEnded = entry.openEnded === true
  if (entry.recordCount === 1 && entry.records[0] && !openEnded) {
    return <RecordRef kind={entry.kind} id={entry.records[0]} />
  }
  const count = entry.recordCount
  const words =
    count === 1 && !openEnded
      ? displayName(entry.kind)
      : displayPlural(entry.kind)
  const { authority, pkg, name } = collectionLink(entry.kind)
  const label = openEnded
    ? lowerFirst(words)
    : `${count.toLocaleString()} ${lowerFirst(words)}`
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

function Phrase({ phrase }: { phrase: SystemPhrase }) {
  return (
    <>
      <span>{phrase.words}</span>
      {phrase.collection && (
        <>
          {" "}
          <KindRef kind={phrase.collection} />
        </>
      )}
      {phrase.tail && <span> {phrase.tail}</span>}
    </>
  )
}

function seqRange(entry: HistoryEntry): string {
  const { newestSeq, oldestSeq } = entry
  return newestSeq === oldestSeq ? `${newestSeq}` : `${oldestSeq}–${newestSeq}`
}

export function HistoryEntryRow({
  entry: told,
  today,
}: {
  entry: HistoryEntry
  /** Today's entries read as "3m ago"; older ones by the time of day, under
   * their day's heading. */
  today: boolean
}) {
  const [technical] = useTechnicalDetails()
  const registry = useQuery(kindsQueryOptions)
  const runRows = useQuery({
    ...runRowsQueryOptions(told.run),
    enabled: runNeedsRows(told),
  })
  const entry = useMemo(
    () => withRunRows(told, runRows.data),
    [told, runRows.data]
  )
  const changed = entry.verb === "changed" && entry.properties.length > 0
  // One record's run says its net change in values, once every row of it is
  // in hand; a run over many records, or rows from a server that sends names
  // alone, says the names.
  const moves =
    entry.recordCount === 1 && rowsComplete(entry)
      ? netMoves(entry.rows, entry.records[0], entry.kind)
      : undefined
  const phrase = systemPhrase(entry, moves)
  const specs = useMemo(
    () => valueSpecs(registry.data?.find((k) => k.identity === entry.kind)),
    [registry.data, entry.kind]
  )
  const quiet = phrase?.complete && !technical
  const showMoves = changed && moves && moves.length > 0 && !quiet
  const names =
    changed && !moves && !quiet
      ? namedProperties(entry).filter(
          (p) => technical || !hostWritten(specs.get(p.name))
        )
      : []
  const label = (key: string) => specs.get(key)?.label ?? propertyLabel(key)
  const seq = seqRange(entry)
  return (
    <div
      data-slot="history-entry"
      className="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-2.5 border-b border-border py-[9px]"
    >
      <div className="min-w-0 leading-[1.6]">
        <ActorRef actor={entry.actor} inlineId={false} />{" "}
        {phrase ? (
          <Phrase phrase={phrase} />
        ) : (
          <>
            <span>{entry.verb}</span> <EntryObject entry={entry} />
          </>
        )}
        {showMoves && (
          <ValueMoves moves={moves} specs={specs} className="mt-1" />
        )}
        {(names.length > 0 || technical) && (
          <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[12.5px] text-faint">
            {names.length > 0 &&
              (technical ? (
                <span className="font-mono text-[11.5px]">
                  {names
                    .map((p) =>
                      p.renamedFrom ? `${p.renamedFrom} → ${p.name}` : p.name
                    )
                    .join(", ")}
                </span>
              ) : (
                <span>
                  {names
                    .map((p) =>
                      p.renamedFrom
                        ? `${label(p.renamedFrom)} renamed to ${label(p.name)}`
                        : label(p.name)
                    )
                    .join(" · ")}
                </span>
              ))}
            {technical && (
              <>
                <span className="inline-flex items-center gap-0.5 tabular-nums">
                  #{seq}
                  <CopyButton
                    value={seq.replace("–", "-")}
                    label={
                      entry.count > 1
                        ? "Copy the sequence numbers"
                        : "Copy the sequence number"
                    }
                  />
                </span>
                <span className="inline-flex min-w-0 items-center gap-0.5">
                  <span className="font-mono text-[11.5px] [overflow-wrap:anywhere]">
                    {entry.actor}
                  </span>
                  <CopyButton value={entry.actor} label="Copy the actor id" />
                </span>
                {entry.recordCount > 1 && (
                  <KindPath reference={entry.kind} className="text-[11.5px]" />
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
  entries,
  limit,
  className,
}: {
  entries: readonly HistoryEntry[]
  limit?: number
  className?: string
}) {
  const days = useMemo(
    () => groupByDay(limit ? entries.slice(0, limit) : entries),
    [entries, limit]
  )
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

/** The line that says an everyday feed leaves the system's own changes out,
 * so nothing is lost silently: "System changes hidden · Show". With `shown`,
 * the way back. */
export function SystemChangesNote({
  shown = false,
  onToggle,
  className,
}: {
  shown?: boolean
  onToggle: () => void
  className?: string
}) {
  return (
    <p
      data-slot="system-changes"
      className={cn("text-[12.5px] text-faint", className)}
    >
      {shown ? "Showing system changes" : "System changes hidden"}
      {" · "}
      <button
        type="button"
        onClick={onToggle}
        className="cursor-pointer text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
      >
        {shown ? "Hide" : "Show"}
      </button>
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
  if (!feed.entries.length && !feed.hasOlder) {
    return <p className="py-8 text-muted-foreground">{empty}</p>
  }
  return (
    <>
      <HistorySentences entries={feed.entries} />
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
