/** One filter's History, newest first, as sentences with the live tail joined
 * on top: what History, the actor page, a provider's activity and Home read.
 * The server summarizes the runs (`runs=1`, decision 0126), so a sentence's
 * count is exact from one read; against a server without runs the rows are
 * read instead and folded here. */

import { useEffect, useMemo, useState } from "react"
import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
  type QueryKey,
} from "@tanstack/react-query"

import { useTechnicalDetails } from "@/hooks/use-console-preferences"

import {
  historyInfiniteOptions,
  watchChanges,
  type ChangeFeedFilter,
  type HistoryPage,
  type WatchStatus,
} from "@/lib/api/changes"
import { emptyQuery, holdAtRefusal } from "@/lib/api/compacted"
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
  /** What the feed's runs come under RULE W (compacted.ts) by: a run's rows
   * refused `compacted` read the summary again at most once a minute. */
  runRecovery: RunRecovery
}

/** The feed a run's rows belong to, under RULE W: one allowance per feed,
 * and the summary that named the run, read again by a recovery. */
export interface RunRecovery {
  /** The allowance's key: the feed's filter, so the feed mounted again
   * within the minute meets the allowance it left. */
  key: string
  /** Reads the feed's summary again, from its first page. */
  reload: () => void
}

/** Where the tail opens: above the head a page named, under its
 * generation, so nothing between the read and the tail is lost; or, after a
 * recovery, at the server's head (no `from`), whatever page lands
 * meanwhile. Each recovery opens a new one. */
interface TailOpening {
  from?: number
  generation?: string
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
  const historyOptions = historyInfiniteOptions(asked, { first, rows })
  const history = useInfiniteQuery({ ...historyOptions, enabled })
  const historyKey = JSON.stringify(historyOptions.queryKey)
  const [liveRows, setLiveRows] = useState<ChangeRow[]>([])
  const [status, setStatus] = useState<WatchStatus>("off")
  const [tail, setTail] = useState<TailOpening>()
  const filterKey = JSON.stringify(asked)
  const [lastKey, setLastKey] = useState(filterKey)
  if (lastKey !== filterKey) {
    setLastKey(filterKey)
    setLiveRows([])
    setStatus("off")
    setTail(undefined)
  }

  const pages = history.data?.pages
  // A tail waiting for a page resumes at the head the first page reported,
  // under its generation; a row the page already told is dropped by its seq.
  const head = pages?.[0]
  if (!tail && head && lastKey === filterKey)
    setTail({ from: head.head, generation: head.generation })
  useEffect(() => {
    if (!live || !enabled || !tail) return undefined
    const queryKey = JSON.parse(historyKey) as QueryKey
    // The stopped tail's last word stands: closing it says nothing more.
    let open = true
    const handle = watchChanges({
      from: tail.from,
      generation: tail.generation,
      filter: JSON.parse(filterKey) as ChangeFeedFilter,
      reader: {
        key: `history tail ${filterKey}`,
        // The pages name the refused history's head and generation, which a
        // tail opened on a later mount would resume, and the rows the tail
        // delivered are of that history.
        drop: () => {
          setLiveRows([])
          emptyQuery(queryClient, queryKey)
        },
        // The feed says it didn't load, with Try again, and the tail
        // `stopped`; a page read again opens a tail at its head.
        fail: (refusal) => {
          setTail(undefined)
          holdAtRefusal(queryClient, queryKey, refusal)
        },
      },
      onRow: (row) =>
        setLiveRows((rows) =>
          rows.some((r) => r.seq === row.seq) ? rows : [row, ...rows]
        ),
      onStatus: (next) => {
        if (open) setStatus(next)
      },
      // Open the tail again at the server's head, then read the first page:
      // a row committed between the two arrives on both, told once by its
      // seq.
      onCompacted: () => {
        setTail({})
        void queryClient.resetQueries({ queryKey, exact: true })
      },
    })
    return () => {
      open = false
      handle.stop()
    }
  }, [live, enabled, tail, filterKey, historyKey, queryClient])
  const runRecovery = useMemo<RunRecovery>(
    () => ({
      key: `history runs ${filterKey}`,
      reload: () =>
        void queryClient.invalidateQueries({
          queryKey: JSON.parse(historyKey) as QueryKey,
          exact: true,
        }),
    }),
    [filterKey, historyKey, queryClient]
  )

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
    status: live && enabled && (tail || status === "stopped") ? status : "off",
    isPending: enabled && history.isPending,
    error: history.error,
    hasOlder,
    loadingOlder: history.isFetchingNextPage,
    loadOlder: () => void history.fetchNextPage(),
    retry: () => void history.refetch(),
    runRecovery,
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
