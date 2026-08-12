/** Incoming edges (record 57) on THE table system (owner ruling 2026-08-06):
 * what points here as a compact table — rel, record, kind — paged
 * prev/next on its own cursor resource. The server orders (rel, kind, id),
 * so the flat rows still read grouped as pages advance. */

import { useMemo, useState } from "react"
import { useInfiniteQuery } from "@tanstack/react-query"
import type { ColumnDef } from "@tanstack/react-table"
import { Link } from "@tanstack/react-router"
import { ArrowDownLeftIcon } from "lucide-react"

import { DataTable, useDataTable } from "@/components/data-table/data-table"
import { DataTableColumnHeader } from "@/components/data-table/data-table-column-header"
import { DataTableCursorPagination } from "@/components/data-table/data-table-cursor-pagination"
import { DataTableViewOptions } from "@/components/data-table/data-table-view-options"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { incomingInfiniteOptions } from "@/lib/api/records"
import type { IncomingEdge, KindInfo } from "@/lib/api/types"
import { splitKind, kindByIdentity } from "@/lib/definition"

function FromCell({ row, kinds }: { row: IncomingEdge; kinds: KindInfo[] }) {
  const label = row.from.title || row.from.id
  const fromKind = kindByIdentity(kinds, row.from.kind)
  const { authority } = splitKind(row.from.kind)
  if (!fromKind) {
    return (
      <span className="block truncate data" title={label}>
        {label}
      </span>
    )
  }
  return (
    <Link
      to="/data/$authority/$plural/$id"
      params={{ authority: authority, plural: fromKind.plural, id: row.from.id }}
      className="block truncate underline-offset-4 hover:underline"
      title={label}
    >
      {label}
    </Link>
  )
}

function incomingColumns(kinds: KindInfo[]): ColumnDef<IncomingEdge, unknown>[] {
  return [
    {
      id: "rel",
      accessorFn: (row) => row.rel,
      enableSorting: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="rel" />
      ),
      cell: ({ row }) => (
        <span
          className="block truncate data text-muted-foreground"
          title={row.original.rel}
        >
          {row.original.rel}
        </span>
      ),
      meta: { label: "rel", width: 80 },
    },
    {
      id: "record",
      accessorFn: (row) => row.from.title || row.from.id,
      enableSorting: false,
      enableHiding: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="record" />
      ),
      cell: ({ row }) => <FromCell row={row.original} kinds={kinds} />,
      meta: { label: "record" },
    },
    {
      id: "kind",
      accessorFn: (row) => splitKind(row.from.kind).name,
      enableSorting: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="kind" />
      ),
      cell: ({ row }) => {
        const { name } = splitKind(row.original.from.kind)
        return (
          <span
            className="block truncate data text-muted-foreground"
            title={row.original.from.kind}
          >
            {name}
          </span>
        )
      },
      meta: { label: "kind", width: 110 },
    },
  ]
}

export function IncomingRail({
  authority,
  plural,
  id,
  kinds,
}: {
  authority: string
  plural: string
  id: string
  kinds: KindInfo[]
}) {
  const incoming = useInfiniteQuery(
    incomingInfiniteOptions(authority, plural, id)
  )
  const [page, setPage] = useState(1)

  const pages = useMemo(
    () => (incoming.data?.pages ?? []).map((p) => p.incoming ?? []),
    [incoming.data]
  )
  const total = incoming.data?.pages[0]?.total ?? 0
  const rows = pages[page - 1] ?? []

  const columns = useMemo(() => incomingColumns(kinds), [kinds])
  const table = useDataTable({
    columns,
    data: rows,
    getRowId: (row) => `${row.rel}:${row.from.kind}:${row.from.id}`,
    prefsKey: "record-incoming",
  })

  async function next() {
    if (page < pages.length) {
      setPage(page + 1)
      return
    }
    if (!incoming.hasNextPage || incoming.isFetchingNextPage) return
    const res = await incoming.fetchNextPage()
    const fetched = res.data?.pages
    if (
      fetched &&
      fetched.length > page &&
      (fetched[page].incoming ?? []).length > 0
    ) {
      setPage(page + 1)
    }
  }

  if (incoming.isError) {
    return (
      <Empty className="py-10">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <ArrowDownLeftIcon />
          </EmptyMedia>
          <EmptyTitle>Incoming edges didn't load</EmptyTitle>
          <EmptyDescription>{incoming.error.message}</EmptyDescription>
        </EmptyHeader>
        <Button
          variant="outline"
          size="sm"
          onClick={() => void incoming.refetch()}
        >
          Retry
        </Button>
      </Empty>
    )
  }

  if (!incoming.isPending && total === 0) {
    return (
      <Empty className="py-10">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <ArrowDownLeftIcon />
          </EmptyMedia>
          <EmptyTitle>Nothing points here</EmptyTitle>
          <EmptyDescription>
            No live record holds an edge onto this one.
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  const hasMore = page < pages.length || Boolean(incoming.hasNextPage)

  return (
    <div className="flex flex-col">
      <div className="flex items-center justify-end px-2 pt-1">
        <DataTableViewOptions table={table} compact />
      </div>
      <DataTable table={table} density="compact" loading={incoming.isPending} />
      <DataTableCursorPagination
        density="compact"
        page={page}
        rows={rows.length}
        hasPrev={page > 1}
        hasNext={hasMore}
        onPrev={() => setPage(page - 1)}
        onNext={() => void next()}
        loading={incoming.isFetchingNextPage}
        summary={`${total.toLocaleString()} incoming`}
      />
    </div>
  )
}
