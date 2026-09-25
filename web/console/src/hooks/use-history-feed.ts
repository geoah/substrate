/** One filter's change feed, newest first, with the live tail joined on top:
 * what History, the actor page and Home read their sentences from. */

import { useEffect, useMemo, useRef, useState } from "react"
import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"

import { useTechnicalDetails } from "@/hooks/use-console-preferences"

import {
  changesInfiniteOptions,
  watchChanges,
  type ChangeFeedFilter,
  type WatchStatus,
} from "@/lib/api/changes"
import { kindsQueryOptions } from "@/lib/api/kinds"
import type { ChangeRow } from "@/lib/api/types"
import { mergeFeed } from "@/lib/changelog"
import { isSystemChange, kindsByReference } from "@/lib/history"

/** Rows one history page reads: enough to fill a screen with sentences once
 * the runs fold. */
const HISTORY_PAGE = 100

/** How many further pages a filtered feed reads on its own to fill the
 * screen before it waits for "Show older changes": a repository whose last
 * thousand rows are all machinery must not page forever. */
const FILL_PAGES = 5

export interface HistoryFeedState {
  /** The rows `keep` admits, newest first. */
  rows: ChangeRow[]
  /** Loaded rows `keep` refused. */
  hidden: number
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
  first?: number
  /** A client-side filter over the loaded rows, for what the change feed
   * cannot filter server-side. Keep it referentially stable. */
  keep?: (row: ChangeRow) => boolean
  /** With `keep`: read further pages, up to FILL_PAGES of them, until this
   * many rows pass. */
  fill?: number
  /** Ask the history pages for before and after values (`values=1`). Off by
   * default: each page's values cost the server a walk back through every
   * record on it, which a glance at recent changes does not repay. */
  values?: boolean
}

/** What the feed asks of the server: its pages carry values only where the
 * reader asked, and the live tail never does, because every row it streams
 * would cost the server a walk of its own. */
export function historyFeedFilters(
  filter: ChangeFeedFilter,
  values = false
): { pages: ChangeFeedFilter; tail: ChangeFeedFilter } {
  const plain: ChangeFeedFilter = { ...filter }
  delete plain.values
  return { pages: values ? { ...plain, values: true } : plain, tail: plain }
}

/** The change feed for one filter, newest first, with the live tail joined on
 * top while `live` is on. */
export function useHistoryFeed(
  filter: ChangeFeedFilter,
  {
    live = true,
    enabled = true,
    first = HISTORY_PAGE,
    keep,
    fill = 0,
    values = false,
  }: HistoryFeedOptions = {}
): HistoryFeedState {
  const queryClient = useQueryClient()
  const { pages: asked, tail } = historyFeedFilters(filter, values)
  const history = useInfiniteQuery({
    ...changesInfiniteOptions(asked, { first }),
    enabled,
  })
  const [liveRows, setLiveRows] = useState<ChangeRow[]>([])
  const [status, setStatus] = useState<WatchStatus>("off")
  const [nonce, setNonce] = useState(0)
  const filterKey = JSON.stringify(asked)
  const tailKey = JSON.stringify(tail)
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
    if (!live || !enabled || !ready) return undefined
    const handle = watchChanges({
      from: resume.current.from,
      generation: resume.current.generation,
      filter: JSON.parse(tailKey) as ChangeFeedFilter,
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
  }, [live, enabled, ready, filterKey, tailKey, nonce, queryClient])

  const merged = useMemo(
    () => mergeFeed(liveRows, historyRows),
    [liveRows, historyRows]
  )
  const rows = useMemo(
    () => (keep ? merged.filter(keep) : merged),
    [merged, keep]
  )

  // A filtered page can come back nearly empty; read on until the screen
  // fills or the budget runs out. "Show older changes" raises the goal and
  // renews the budget.
  const goal = useRef(fill)
  const budget = useRef(FILL_PAGES)
  useEffect(() => {
    goal.current = fill
    budget.current = FILL_PAGES
  }, [filterKey, fill, keep])
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = history
  useEffect(() => {
    if (!keep || !enabled || !head) return
    if (rows.length >= goal.current || budget.current <= 0) return
    if (!hasNextPage || isFetchingNextPage) return
    budget.current -= 1
    void fetchNextPage()
  }, [
    keep,
    enabled,
    head,
    rows.length,
    hasNextPage,
    isFetchingNextPage,
    fetchNextPage,
  ])

  return {
    rows,
    hidden: merged.length - rows.length,
    status: live && enabled && ready ? status : "off",
    isPending: enabled && history.isPending,
    error: history.error,
    hasOlder: Boolean(history.hasNextPage),
    loadingOlder: history.isFetchingNextPage,
    loadOlder: () => {
      if (keep) {
        goal.current = rows.length + Math.max(fill, 1)
        budget.current = FILL_PAGES
      }
      void history.fetchNextPage()
    },
    retry: () => void history.refetch(),
  }
}

/** The `keep` everyday History reads with: only changes to data a person
 * keeps, machinery left out. Undefined — everything — with technical details
 * on, or once the reader asked to `show` the system changes. */
export function useEverydayChanges(
  show = false
): ((row: ChangeRow) => boolean) | undefined {
  const [technical] = useTechnicalDetails()
  const registry = useQuery(kindsQueryOptions)
  return useMemo(() => {
    if (technical || show) return undefined
    const kinds = kindsByReference(registry.data)
    return (row: ChangeRow) => !isSystemChange(row, kinds)
  }, [technical, show, registry.data])
}
