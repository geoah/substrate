/** One filter's change feed, newest first, with the live tail joined on top:
 * what History, the actor page and Home read their sentences from. */

import { useEffect, useMemo, useRef, useState } from "react"
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query"

import {
  changesInfiniteOptions,
  watchChanges,
  type ChangeFeedFilter,
  type WatchStatus,
} from "@/lib/api/changes"
import type { ChangeRow } from "@/lib/api/types"
import { mergeFeed } from "@/lib/changelog"

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
    status: live && enabled && ready ? status : "off",
    isPending: enabled && history.isPending,
    error: history.error,
    hasOlder: Boolean(history.hasNextPage),
    loadingOlder: history.isFetchingNextPage,
    loadOlder: () => void history.fetchNextPage(),
    retry: () => void history.refetch(),
  }
}
