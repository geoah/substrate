/** Provenance & offers (§7.1, read-only) on THE table system (owner ruling
 * 2026-08-06): per managed property one compact row — property, manager,
 * since, offers — expanding beneath the line to every live offer that
 * disagrees ("Google says X"), through the same expansion slot every other
 * table uses. Client-side data (propertyMeta rides the single record read),
 * so no pagination — the row set is the schema's size, not a feed. */

import { useMemo } from "react"
import type { ColumnDef } from "@tanstack/react-table"
import { FileLockIcon } from "lucide-react"

import { ActorChip } from "@/components/actor-chip"
import {
  actorColumn,
  timeColumn,
} from "@/components/data-table/columns"
import { DataTable, useDataTable } from "@/components/data-table/data-table"
import { DataTableColumnHeader } from "@/components/data-table/data-table-column-header"
import { DataTableViewOptions } from "@/components/data-table/data-table-view-options"
import { RowDetail } from "@/components/data-table/row-detail"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import type { PropertyMeta } from "@/lib/api/types"
import { cellValue, relativeTime } from "@/lib/format"

interface ProvenanceRow {
  name: string
  meta: PropertyMeta
}

const COLUMNS: ColumnDef<ProvenanceRow, unknown>[] = [
  {
    id: "property",
    accessorFn: (row) => row.name,
    enableSorting: false,
    enableHiding: false,
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title="property" />
    ),
    cell: ({ row }) => (
      <span className="block truncate data" title={row.original.name}>
        {row.original.name}
      </span>
    ),
    meta: { label: "property" },
  },
  actorColumn<ProvenanceRow>({
    id: "manager",
    title: "manager",
    actor: (row) => row.meta.manager,
    width: 108,
  }),
  timeColumn<ProvenanceRow>({
    id: "since",
    title: "since",
    iso: (row) => row.meta.updatedAt,
    voice: "relative",
    width: 64,
    align: "right",
  }),
  {
    id: "offers",
    accessorFn: (row) => row.meta.alternatives?.length ?? 0,
    enableSorting: false,
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title="offers" align="right" />
    ),
    cell: ({ row }) => {
      const n = row.original.meta.alternatives?.length ?? 0
      return n > 0 ? (
        <span className="block text-right data">{n}</span>
      ) : (
        <span className="block text-right text-muted-foreground/50">—</span>
      )
    },
    meta: {
      label: "offers",
      width: 56,
      headerClassName: "text-right",
      cellClassName: "text-right",
    },
  },
]

/** The competing offers, opened beneath the grid line (rule 11) in the
 * shared detail band: who says what, since when. */
function OffersDetail({ row }: { row: ProvenanceRow }) {
  const offers = row.meta.alternatives ?? []
  return (
    <RowDetail density="compact">
      <div className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-x-2.5 gap-y-1">
        {offers.map((offer) => (
          <div key={offer.actor} className="contents">
            <ActorChip actor={offer.actor} />
            <span
              className="truncate text-right data"
              title={cellValue(offer.value)}
            >
              {cellValue(offer.value)}
            </span>
            <span
              className="text-right data text-muted-foreground"
              title={offer.updatedAt}
            >
              {relativeTime(offer.updatedAt)}
            </span>
          </div>
        ))}
      </div>
    </RowDetail>
  )
}

export function ProvenanceRail({
  propertyMeta,
}: {
  propertyMeta: Record<string, PropertyMeta>
}) {
  const rows = useMemo(
    () =>
      Object.keys(propertyMeta)
        .sort()
        .map((name) => ({ name, meta: propertyMeta[name] })),
    [propertyMeta]
  )

  const table = useDataTable({
    columns: COLUMNS,
    data: rows,
    getRowId: (row) => row.name,
    prefsKey: "record-provenance",
  })

  if (!rows.length) {
    return (
      <Empty className="py-10">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <FileLockIcon />
          </EmptyMedia>
          <EmptyTitle>No managed properties</EmptyTitle>
          <EmptyDescription>
            No actor holds a property on this record.
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className="flex flex-col">
      <div className="flex items-center justify-end px-2 pt-1">
        <DataTableViewOptions table={table} compact />
      </div>
      <DataTable
        table={table}
        density="compact"
        renderExpanded={(row) => <OffersDetail row={row} />}
        expandable={(row) => (row.meta.alternatives?.length ?? 0) > 0}
      />
    </div>
  )
}
