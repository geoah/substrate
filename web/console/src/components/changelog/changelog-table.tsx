/** The flat change feed (owner ruling, 2026-08-06 — the intent fold is
 * gone): every commit is one table row on THE table system — time, actor,
 * action, record, kind, authority, summary — expandable in place to the shared
 * change detail, paged prev/next by seq cursor (the changelog has no offset
 * cursor), columns user-configurable and persisted per surface. Follow rides
 * the ndjson watch: new rows land at the top of page 1; scrolled away or on
 * an older page they hold behind the "N new events" pill. Shared by
 * /changelog, the actor view and the connector timeline (one spine, three
 * views). */

import {
  useEffect,
  useMemo,
  useReducer,
  useRef,
  useState,
  type ReactNode,
} from "react"
import { useInfiniteQuery, useQuery } from "@tanstack/react-query"
import { ArrowUpIcon, InboxIcon, SearchXIcon } from "lucide-react"

import {
  changeActorColumn,
  changeAuthorityColumn,
  changeRecordColumn,
  changeKindColumn,
  changeOpColumn,
  changeSummaryColumn,
  changeTimeColumn,
} from "@/components/data-table/columns"
import { DataTable, useDataTable } from "@/components/data-table/data-table"
import { DataTableCursorPagination } from "@/components/data-table/data-table-cursor-pagination"
import { DataTableViewOptions } from "@/components/data-table/data-table-view-options"
import { ChangeDetail, RowDetail } from "@/components/data-table/row-detail"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import {
  changesInfiniteOptions,
  seekQueryOptions,
  watchChanges,
  type ChangeFeedFilter,
  type WatchStatus,
} from "@/lib/api/changes"
import type { ChangeRow, KindInfo } from "@/lib/api/types"
import {
  EMPTY_LIVE_FEED,
  flushLive,
  isVocabularyChange,
  mergeFeed,
  pushLive,
  type LiveFeed,
} from "@/lib/changelog"

// ── live buffer wiring ──────────────────────────────────────────────────────

type LiveAction =
  | { kind: "push"; row: ChangeRow; paused: boolean }
  | { kind: "flush" }
  | { kind: "reset" }

function liveReducer(feed: LiveFeed, action: LiveAction): LiveFeed {
  switch (action.kind) {
    case "push":
      return pushLive(feed, action.row, action.paused)
    case "flush":
      return flushLive(feed)
    case "reset":
      return EMPTY_LIVE_FEED
  }
}

/** Scrolled past this is "away from the top" — new rows hold. */
const TOP_THRESHOLD_PX = 24

/** Table pages, prev/next by seq cursor. */
export const CHANGELOG_TABLE_PAGE = 50

// ── the surface ─────────────────────────────────────────────────────────────

export interface ChangelogTableProps {
  /** Server-side facets, already folded (authority → kinds, actor fixed…). */
  filter: ChangeFeedFilter
  /** Client floor of the time range (the wire has no time parameter). */
  sinceMs?: number
  /** Ceiling — answered by the seq seek before history loads. */
  untilMs?: number
  follow: boolean
  kinds: KindInfo[]
  onStatus?: (status: WatchStatus, detail?: string) => void
  /** The per-surface column-prefs key (changelog / actor / connector views). */
  surface?: string
  /** Columns this view hides until asked (the actor view drops `actor`). */
  defaultHidden?: string[]
  /** The filter row, rendered into the shared toolbar's left seam. */
  toolbarLeft?: ReactNode
  /** Follow toggle etc., right seam — the Columns dropdown sits beside it. */
  toolbarRight?: ReactNode
}

export function ChangelogTable({
  filter,
  sinceMs,
  untilMs,
  follow,
  kinds,
  onStatus,
  surface = "changelog",
  defaultHidden,
  toolbarLeft,
  toolbarRight,
}: ChangelogTableProps) {
  // The seek answers "where does history ≤ until start" before paging begins.
  const seek = useQuery({
    ...seekQueryOptions(untilMs ?? 0),
    enabled: untilMs !== undefined,
  })
  const startBefore = untilMs === undefined ? 0 : seek.data
  const history = useInfiniteQuery({
    ...changesInfiniteOptions(filter, {
      first: CHANGELOG_TABLE_PAGE,
      startBefore: startBefore ?? 0,
      sinceMs,
    }),
    enabled: startBefore !== undefined,
  })

  const [live, dispatch] = useReducer(liveReducer, EMPTY_LIVE_FEED)
  const [page, setPage] = useState(1)
  const pausedRef = useRef(false)
  const pageRef = useRef(1)
  useEffect(() => {
    pageRef.current = page
  }, [page])
  const scrollRef = useRef<HTMLDivElement>(null)
  // Kept fresh by effect (before the watch effect below), so the tail
  // wiring never re-opens just because the parent re-rendered.
  const statusRef = useRef(onStatus)
  useEffect(() => {
    statusRef.current = onStatus
  }, [onStatus])
  // Compaction recovery: a `compacted` watch signal means the resume seq fell
  // below retention — the gap can't be tailed, so we re-list from a fresh head
  // and re-open the tail. The refetch handle stays in a ref (its identity
  // changes each render); a nonce re-runs the watch effect after the re-list.
  const refetchHistoryRef = useRef(history.refetch)
  useEffect(() => {
    refetchHistoryRef.current = history.refetch
  }, [history.refetch])
  const [resetNonce, setResetNonce] = useState(0)

  /** The time bounds, applied to whatever page shows (the seek bounds by
   * seq; these keep the range honest to the instant). */
  function inRange(row: ChangeRow): boolean {
    const t = Date.parse(row.ts)
    if (untilMs !== undefined && t > untilMs) return false
    if (sinceMs !== undefined && t < sinceMs) return false
    return true
  }

  const pages = useMemo(
    () => (history.data?.pages ?? []).map((p) => p.changes.filter(inRange)),
    // eslint-disable-next-line react-hooks/exhaustive-deps -- inRange is pure over these
    [history.data, sinceMs, untilMs]
  )
  const pageCount = pages.length
  const pageRows = pages[page - 1] ?? []

  // Live rows join the top of page 1 only; elsewhere they hold in the pill.
  const rows = useMemo(
    () =>
      page === 1 ? mergeFeed(live.rows, pageRows).filter(inRange) : pageRows,
    // eslint-disable-next-line react-hooks/exhaustive-deps -- inRange is pure over these
    [page, live.rows, pageRows, sinceMs, untilMs]
  )
  const headSeq = rows[0]?.seq

  // A new facet set or time range restarts at the head (render-adjustment
  // pattern — no effect, no cascading render).
  const filterKey = JSON.stringify(filter)
  const viewKey = `${filterKey}|${sinceMs ?? ""}|${untilMs ?? ""}`
  const [lastViewKey, setLastViewKey] = useState(viewKey)
  if (lastViewKey !== viewKey) {
    setLastViewKey(viewKey)
    setPage(1)
  }

  // The tail: resume above what is already showing, deliver into the buffer.
  const headRef = useRef<number | undefined>(undefined)
  useEffect(() => {
    headRef.current = headSeq
  }, [headSeq])
  useEffect(() => {
    dispatch({ kind: "reset" })
    if (!follow) {
      statusRef.current?.("off")
      return
    }
    const handle = watchChanges({
      from: headRef.current,
      filter: JSON.parse(filterKey) as ChangeFeedFilter,
      onRow: (row) =>
        dispatch({
          kind: "push",
          row,
          paused: pausedRef.current || pageRef.current !== 1,
        }),
      onStatus: (status, detail) => statusRef.current?.(status, detail),
      onCompacted: () => {
        // Re-list, then bump the nonce so this effect re-subscribes from the
        // fresh head instead of the stale (now-compacted) resume seq.
        void refetchHistoryRef.current?.()
        setResetNonce((n) => n + 1)
      },
    })
    return () => handle.stop()
  }, [follow, filterKey, resetNonce])

  function onScroll() {
    const el = scrollRef.current
    if (!el) return
    const paused = el.scrollTop > TOP_THRESHOLD_PX
    pausedRef.current = paused
    if (!paused && page === 1 && live.pending.length) {
      dispatch({ kind: "flush" })
    }
  }

  function flushToTop() {
    setPage(1)
    scrollRef.current?.scrollTo({ top: 0, behavior: "smooth" })
    pausedRef.current = false
    dispatch({ kind: "flush" })
  }

  async function next() {
    if (page < pageCount) {
      setPage(page + 1)
      scrollRef.current?.scrollTo({ top: 0 })
      return
    }
    if (!history.hasNextPage || history.isFetchingNextPage) return
    const res = await history.fetchNextPage()
    const fetched = res.data?.pages
    if (fetched && fetched.length > page && fetched[page].changes.length > 0) {
      setPage(page + 1)
      scrollRef.current?.scrollTo({ top: 0 })
    }
  }

  function prev() {
    if (page > 1) {
      setPage(page - 1)
      scrollRef.current?.scrollTo({ top: 0 })
    }
  }

  const columns = useMemo(
    () => [
      changeTimeColumn(),
      changeActorColumn(),
      changeOpColumn(),
      changeRecordColumn(kinds),
      changeKindColumn(),
      changeAuthorityColumn(),
      changeSummaryColumn(),
    ],
    [kinds]
  )

  const table = useDataTable({
    columns,
    data: rows,
    getRowId: (row) => String(row.seq),
    prefsKey: surface,
    defaultHidden,
  })

  const loading = history.isPending || (untilMs !== undefined && seek.isPending)
  const error = history.isError
    ? history.error
    : seek.isError
      ? seek.error
      : undefined

  const atFeedEnd = page >= pageCount && !history.hasNextPage

  return (
    <>
      <div className="flex shrink-0 flex-wrap items-center gap-2 pr-6">
        {toolbarLeft}
        <div className="ml-auto flex items-center gap-2 py-2.5 pl-2">
          <DataTableViewOptions table={table} />
          {toolbarRight}
        </div>
      </div>
      {error ? (
        <ChangelogEmpty
          icon={<SearchXIcon />}
          title="The change feed didn't load"
          description={error.message}
        >
          <Button
            variant="outline"
            size="sm"
            onClick={() => void (history.isError ? history : seek).refetch()}
          >
            Retry
          </Button>
        </ChangelogEmpty>
      ) : (
        <>
          <div
            ref={scrollRef}
            onScroll={onScroll}
            className="relative min-h-0 flex-1 overflow-auto"
          >
            {live.pending.length > 0 && (
              <div className="sticky top-2 z-10 flex h-0 justify-center">
                <Button
                  size="sm"
                  className="h-7 rounded-full shadow-md"
                  onClick={flushToTop}
                >
                  <ArrowUpIcon className="size-3.5" />
                  {live.pending.length} new{" "}
                  {live.pending.length === 1 ? "event" : "events"}
                </Button>
              </div>
            )}
            <DataTable
              table={table}
              loading={loading}
              renderExpanded={(row) => (
                <RowDetail>
                  <ChangeDetail row={row} />
                </RowDetail>
              )}
              rowClassName={(row) =>
                isVocabularyChange(row)
                  ? "bg-primary/5 dark:bg-primary/10"
                  : undefined
              }
              empty={
                <ChangelogEmpty
                  icon={<InboxIcon />}
                  title="No events"
                  description="Nothing in the changelog matches this view."
                />
              }
            />
          </div>
          <DataTableCursorPagination
            page={page}
            rows={rows.length}
            hasPrev={page > 1}
            hasNext={!atFeedEnd}
            onPrev={prev}
            onNext={() => void next()}
            loading={history.isFetchingNextPage}
            summary={
              atFeedEnd && !loading
                ? sinceMs !== undefined
                  ? "start of the range"
                  : "beginning of the feed"
                : undefined
            }
          />
        </>
      )}
    </>
  )
}

function ChangelogEmpty({
  icon,
  title,
  description,
  children,
}: {
  icon: ReactNode
  title: string
  description: string
  children?: ReactNode
}) {
  return (
    <div className="flex flex-1 p-6">
      <Empty>
        <EmptyHeader>
          <EmptyMedia variant="icon">{icon}</EmptyMedia>
          <EmptyTitle>{title}</EmptyTitle>
          <EmptyDescription>{description}</EmptyDescription>
        </EmptyHeader>
        {children && <EmptyContent>{children}</EmptyContent>}
      </Empty>
    </div>
  )
}
