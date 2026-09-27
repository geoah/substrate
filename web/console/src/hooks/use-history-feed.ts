/** One filter's History, newest first, as sentences with the live tail joined
 * on top: what History, the actor page, a provider's activity and Home read.
 * The server summarizes the runs (`runs=1`, decision 0126), so a sentence's
 * count is exact from one read; against a server without runs the rows are
 * read instead and folded here. */

import { useEffect, useMemo, useRef, useState } from "react"
import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"

import { useTechnicalDetails } from "@/hooks/use-console-preferences"

import {
  historyInfiniteOptions,
  watchChanges,
  type ChangeFeedFilter,
  type HistoryPage,
  type WatchStatus,
} from "@/lib/api/changes"
import { kindsQueryOptions } from "@/lib/api/kinds"
import type { ChangeRow } from "@/lib/api/types"
import { mergeFeed } from "@/lib/changelog"
import { kindPurpose } from "@/lib/definition"
import {
  foldHistory,
  isHousekeeping,
  joinLive,
  runEntry,
  type HistoryEntry,
} from "@/lib/history"

export interface HistoryFeedState {
  /** The sentences, newest first. */
  entries: HistoryEntry[]
  status: WatchStatus
  isPending: boolean
  error: Error | null
  hasOlder: boolean
  loadingOlder: boolean
  loadOlder: () => void
  retry: () => void
}

export interface HistoryFeedOptions {
  live?: boolean
  /** `false` reads nothing (a view whose actor set is empty). */
  enabled?: boolean
  /** Sentences a page reads. */
  first?: number
  /** Rows a page reads from a server that makes no runs. */
  rows?: number
  /** Say one record's changes in values (`values=1` on the reads of its
   * rows). Off by default: each read costs the server a walk back through
   * the record, which a glance at recent changes does not repay. */
  values?: boolean
}

/** The newest seq a page holds: the tail's rows at or below it are told. */
function pageTop(page: HistoryPage | undefined): number {
  if (!page) return 0
  return page.runs
    ? (page.runs[0]?.newestSeq ?? 0)
    : (page.changes[0]?.seq ?? 0)
}

/** The sentences a feed's pages and live rows say, housekeeping only with
 * technical details on, and nothing the filter leaves out. */
export function feedEntries({
  pages,
  live,
  filter,
  values,
  hasOlder,
  technical,
}: {
  pages: readonly HistoryPage[]
  live: readonly ChangeRow[]
  filter: ChangeFeedFilter
  values: boolean
  hasOlder: boolean
  technical: boolean
}): HistoryEntry[] {
  const top = pageTop(pages[0])
  const fresh = live.filter((r) => r.seq > top)
  let entries: HistoryEntry[]
  if (pages[0]?.runs) {
    const told = pages.flatMap((p) =>
      (p.runs ?? []).map((run) =>
        runEntry(run, { generation: p.generation, filter, values })
      )
    )
    // The server's runs are not split by time, so neither is the tail's.
    entries = joinLive(foldHistory(fresh, Infinity), told)
  } else {
    entries = foldHistory(
      mergeFeed(
        fresh,
        pages.flatMap((p) => p.changes ?? [])
      )
    )
    const oldest = entries.at(-1)
    if (oldest && hasOlder)
      entries[entries.length - 1] = { ...oldest, openEnded: true }
  }
  return entries.filter(
    (e) =>
      (technical || !isHousekeeping(e)) &&
      // A kind the registry no longer carries is left out by its reference.
      !(filter.excludeKinds && kindPurpose(e.kind) === "internal")
  )
}

/** The change feed for one filter, newest first, with the live tail joined on
 * top while `live` is on. */
export function useHistoryFeed(
  filter: ChangeFeedFilter,
  {
    live = true,
    enabled = true,
    first,
    rows,
    values = false,
  }: HistoryFeedOptions = {}
): HistoryFeedState {
  const queryClient = useQueryClient()
  const [technical] = useTechnicalDetails()
  const asked = useMemo(() => {
    const plain: ChangeFeedFilter = { ...filter }
    delete plain.values
    return plain
  }, [filter])
  const history = useInfiniteQuery({
    ...historyInfiniteOptions(asked, { first, rows }),
    enabled,
  })
  const [liveRows, setLiveRows] = useState<ChangeRow[]>([])
  const [status, setStatus] = useState<WatchStatus>("off")
  const [nonce, setNonce] = useState(0)
  const filterKey = JSON.stringify(asked)
  const [lastKey, setLastKey] = useState(filterKey)
  if (lastKey !== filterKey) {
    setLastKey(filterKey)
    setLiveRows([])
  }

  const pages = history.data?.pages
  // The tail resumes at the head the first page reported, under its
  // generation, so nothing between the read and the tail is lost; a row the
  // page already told is dropped by its seq.
  const head = pages?.[0]
  const resume = useRef<{ from?: number; generation?: string }>({})
  useEffect(() => {
    if (head) resume.current = { from: head.head, generation: head.generation }
  }, [head])
  const ready = Boolean(head)
  useEffect(() => {
    if (!live || !enabled || !ready) return undefined
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
        void queryClient.invalidateQueries({ queryKey: ["changes", "history"] })
        setNonce((n) => n + 1)
      },
    })
    return () => handle.stop()
  }, [live, enabled, ready, filterKey, nonce, queryClient])

  const hasOlder = Boolean(history.hasNextPage)
  const entries = useMemo(
    () =>
      feedEntries({
        pages: pages ?? [],
        live: liveRows,
        filter: asked,
        values,
        hasOlder,
        technical,
      }),
    [pages, liveRows, asked, values, hasOlder, technical]
  )

  return {
    entries,
    status: live && enabled && ready ? status : "off",
    isPending: enabled && history.isPending,
    error: history.error,
    hasOlder,
    loadingOlder: history.isFetchingNextPage,
    loadOlder: () => void history.fetchNextPage(),
    retry: () => void history.refetch(),
  }
}

/** What everyday History leaves out, server-side, so runs stay whole across
 * it and a page is full of what it shows: the changes to machinery rather
 * than to data a person keeps (every internal kind: trigger runs, tokens,
 * preferences) unless the reader asked to `show` them, and the housekeeping
 * that collects what a delete left behind. Nothing with technical details
 * on. Undefined while the registry that names the kinds loads, so no read
 * goes out only to be thrown away. */
export function useEverydayChanges(show = false): ChangeFeedFilter | undefined {
  const [technical] = useTechnicalDetails()
  const registry = useQuery(kindsQueryOptions)
  return useMemo(() => {
    if (technical) return {}
    if (show) return { excludeOps: ["gc"] }
    if (!registry.data) return undefined
    const excludeKinds = registry.data
      .filter((k) => kindPurpose(k) === "internal")
      .map((k) => k.identity)
      .sort()
    return { excludeKinds, excludeOps: ["gc"] }
  }, [technical, show, registry.data])
}
