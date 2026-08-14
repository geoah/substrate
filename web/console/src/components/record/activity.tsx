/** The Activity tab: this record's changelog slice on THE table system
 * (owner ruling 2026-08-06 — the timeline UI is rejected): a compact table
 * of time, actor, action, each row expandable to the shared change detail
 * (properties, states, managers, function stances). One type scale, one
 * muted tone, data in mono.
 *
 * Two honesty rules survive the migration (owner redline, 2026-08-06):
 * - Former ids are part of the record: the wire's `recordId` filter is an
 *   exact match, so a merged record's pre-merge history lives under its
 *   former ids — those slices are fetched and stitched in after the live
 *   id's history is fully paged.
 * - Creation is always shown when derivable: when no `created` change row
 *   exists (history predates the retained changelog), a terminal band speaks
 *   `metadata.createdAt` and says plainly that the trail is incomplete. */

import { useMemo, useState } from "react"
import { useInfiniteQuery, useQuery } from "@tanstack/react-query"
import type { ColumnDef } from "@tanstack/react-table"
import { HistoryIcon, PlusIcon } from "lucide-react"

import { changeActorColumn, timeColumn } from "@/components/data-table/columns"
import { DataTable, useDataTable } from "@/components/data-table/data-table"
import { DataTableColumnHeader } from "@/components/data-table/data-table-column-header"
import { DataTableCursorPagination } from "@/components/data-table/data-table-cursor-pagination"
import { DataTableViewOptions } from "@/components/data-table/data-table-view-options"
import { ChangeDetail, RowDetail } from "@/components/data-table/row-detail"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import {
  recordChangesInfiniteOptions,
  formerIdChangesQueryOptions,
} from "@/lib/api/records"
import type { ChangeRow, SubstrateRecord } from "@/lib/api/types"
import { relativeTime } from "@/lib/format"
import { changedProperties, verbOf } from "@/lib/changelog"

const RAIL_PAGE = 25

/** time, actor, action — the rail's compact vocabulary. The action cell
 * reads like the rest of the console: a plain verb, the touched-property
 * count, and the former id when the row predates a merge. */
function activityColumns(recordId: string): ColumnDef<ChangeRow, unknown>[] {
  return [
    timeColumn<ChangeRow>({
      id: "time",
      iso: (row) => row.ts,
      voice: "relative",
      width: 70,
    }),
    // No fixed width: an actor is identity-length, and 110px truncated every
    // chip while the action column sat half empty. The shared factory's
    // weighted share (min 140, max 240) already knows the answer.
    changeActorColumn(),
    {
      id: "action",
      accessorFn: (row) => verbOf(row),
      enableSorting: false,
      enableHiding: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="action" />
      ),
      cell: ({ row }) => {
        const changed = changedProperties(row.original).length
        const former =
          row.original.recordId !== recordId ? row.original.recordId : undefined
        const text = [
          verbOf(row.original),
          changed > 0
            ? `${changed} ${changed === 1 ? "property" : "properties"}`
            : "",
          former ? `as ${former}` : "",
        ]
          .filter(Boolean)
          .join(", ")
        return (
          <span
            className="block truncate data text-muted-foreground"
            title={text}
          >
            {text}
          </span>
        )
      },
      meta: { label: "action" },
    },
  ]
}

/** The trail's honest floor when no `created` row exists: metadata.createdAt
 * is server-owned truth, so creation is always shown — with the caveat that
 * the changelog no longer holds the row (predates retention, or the id's
 * early history is unrecorded). */
function CreationFallback({
  createdAt,
  hadRows,
}: {
  createdAt: string
  hadRows: boolean
}) {
  return (
    <div className="border-t px-4 py-2 text-xs">
      <div className="flex items-center gap-1.5">
        <PlusIcon className="size-3 shrink-0 text-muted-foreground" />
        <span className="data text-muted-foreground">created</span>
        <span
          className="ml-auto shrink-0 data text-muted-foreground"
          title={createdAt}
        >
          {relativeTime(createdAt)}
        </span>
      </div>
      <p className="mt-0.5 text-muted-foreground/70">
        From the record's own metadata — the change feed holds no record of
        {hadRows ? " the creation" : " this record"} (it predates the retained
        changelog).
      </p>
    </div>
  )
}

export function ActivityRail({ record }: { record: SubstrateRecord }) {
  const formerIds = useMemo(() => record.formerIds ?? [], [record.formerIds])
  const changes = useInfiniteQuery(
    recordChangesInfiniteOptions(record.id, record.kind, RAIL_PAGE)
  )
  const former = useQuery(formerIdChangesQueryOptions(formerIds, record.kind))
  const [page, setPage] = useState(1)

  // Former-id slices join only once current history is fully paged — their
  // rows are older than everything under the live id, so appending them
  // early would interleave wrongly with pages still to come.
  const wirePages = useMemo(() => changes.data?.pages ?? [], [changes.data])
  const stitched = useMemo(() => {
    if (changes.hasNextPage) return []
    const seen = new Set<number>()
    for (const p of wirePages) for (const r of p.changes) seen.add(r.seq)
    return (former.data ?? [])
      .filter((r) => !seen.has(r.seq) && (seen.add(r.seq), true))
      .sort((a, b) => b.seq - a.seq)
  }, [wirePages, changes.hasNextPage, former.data])

  // The stitched former-id rows page on after the wire pages end.
  const formerPageCount = Math.ceil(stitched.length / RAIL_PAGE)
  const pageCount = wirePages.length + formerPageCount
  const rows =
    page <= wirePages.length
      ? (wirePages[page - 1]?.changes ?? [])
      : stitched.slice(
          (page - wirePages.length - 1) * RAIL_PAGE,
          (page - wirePages.length) * RAIL_PAGE
        )

  const columns = useMemo(() => activityColumns(record.id), [record.id])
  const table = useDataTable({
    columns,
    data: rows,
    getRowId: (row) => String(row.seq),
    prefsKey: "record-activity",
  })

  async function next() {
    if (page < pageCount) {
      setPage(page + 1)
      return
    }
    if (!changes.hasNextPage || changes.isFetchingNextPage) return
    const res = await changes.fetchNextPage()
    const fetched = res.data?.pages
    if (fetched && fetched.length > page && fetched[page].changes.length > 0) {
      setPage(page + 1)
    }
  }

  if (changes.isError) {
    return (
      <Empty className="py-10">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <HistoryIcon />
          </EmptyMedia>
          <EmptyTitle>Activity didn't load</EmptyTitle>
          <EmptyDescription>{changes.error.message}</EmptyDescription>
        </EmptyHeader>
        <Button
          variant="outline"
          size="sm"
          onClick={() => void changes.refetch()}
        >
          Retry
        </Button>
      </Empty>
    )
  }

  const complete = !changes.hasNextPage
  const onLastPage = page >= pageCount
  const allRows = [...wirePages.flatMap((p) => p.changes), ...stitched]
  const hasCreatedRow = allRows.some((r) => r.payload?.created === true)
  const hasMore = page < pageCount || Boolean(changes.hasNextPage)

  return (
    <div className="flex flex-col">
      <div className="flex items-center justify-end px-2 pt-1">
        <DataTableViewOptions table={table} compact />
      </div>
      <DataTable
        table={table}
        density="compact"
        loading={changes.isPending}
        renderExpanded={(row) => (
          <RowDetail density="compact">
            <ChangeDetail row={row} />
          </RowDetail>
        )}
        empty={
          <p className="px-4 py-3 text-xs text-muted-foreground">
            No recorded changes — the feed holds nothing for this record.
          </p>
        }
      />
      {complete && onLastPage && !changes.isPending && !hasCreatedRow && (
        <CreationFallback
          createdAt={record.createdAt}
          hadRows={allRows.length > 0}
        />
      )}
      {(hasMore || page > 1) && (
        <DataTableCursorPagination
          density="compact"
          page={page}
          rows={rows.length}
          hasPrev={page > 1}
          hasNext={hasMore}
          onPrev={() => setPage(page - 1)}
          onNext={() => void next()}
          loading={changes.isFetchingNextPage}
        />
      )}
    </div>
  )
}
