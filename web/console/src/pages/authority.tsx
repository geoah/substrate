/** Authority page (`/data/:authority`): the authority's kinds at a glance on
 * THE table system — name, description where the schema carries one, live
 * record count (a bounded keyset-walk count, capped collections read as N+),
 * each row a door into that kind's browse. Reached from the breadcrumb's
 * authority segment. Bounded registry data (a handful of rows), so no
 * pagination seam. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import type { ColumnDef } from "@tanstack/react-table"
import { FileCode2Icon } from "lucide-react"

import { DataTable, useDataTable } from "@/components/data-table/data-table"
import { DataTableColumnHeader } from "@/components/data-table/data-table-column-header"
import { DataTableViewOptions } from "@/components/data-table/data-table-view-options"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { recordCountQueryOptions, formatCount } from "@/lib/api/records"
import { kindsQueryOptions } from "@/lib/api/kinds"
import type { KindInfo } from "@/lib/api/types"
import { authorityRoute } from "@/router"

/** The one-liner a kind carries: its reconciled `definition.description` when
 * one exists. There is no `sourceYAML` on the wire (record 61) — the parsed
 * declaration IS the document — so a kind that declares no description simply
 * has an empty cell. */
function kindDescription(k: KindInfo): string | undefined {
  const declared = k.definition?.description
  return typeof declared === "string" && declared.trim()
    ? declared.trim()
    : undefined
}

function CountCell({ kind }: { kind: KindInfo }) {
  const count = useQuery(recordCountQueryOptions(kind.authority, kind.plural))
  if (count.isPending) {
    return <Skeleton className="ml-auto h-3.5 w-10" />
  }
  if (count.isError) {
    return <span className="block text-right text-muted-foreground">—</span>
  }
  return (
    <span className="block text-right data">{formatCount(count.data)}</span>
  )
}

function buildColumns(authority: string): ColumnDef<KindInfo, unknown>[] {
  return [
    {
      id: "kind",
      accessorFn: (k) => k.name,
      enableSorting: false,
      enableHiding: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="kind" />
      ),
      cell: ({ row }) => (
        <Link
          to="/data/$authority/$plural"
          params={{ authority: authority, plural: row.original.plural }}
          className="block truncate data underline-offset-4 hover:underline"
          onClick={(e) => e.stopPropagation()}
        >
          {row.original.name}
        </Link>
      ),
      meta: { label: "kind", width: 180 },
    },
    {
      id: "description",
      accessorFn: (k) => kindDescription(k),
      enableSorting: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="description" />
      ),
      cell: ({ row }) => {
        const text = kindDescription(row.original)
        if (!text) return null
        return (
          // the truncated remainder stays readable on hover (sweep finding,
          // 2026-08-06); truncation only at the column boundary
          <span className="block truncate text-muted-foreground" title={text}>
            {text}
          </span>
        )
      },
      meta: { label: "description", size: { min: 240, weight: 2 } },
    },
    {
      id: "records",
      enableSorting: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="records" align="right" />
      ),
      cell: ({ row }) => <CountCell kind={row.original} />,
      meta: {
        label: "records",
        width: 100,
        headerClassName: "text-right",
        cellClassName: "text-right",
      },
    },
  ]
}

export function AuthorityPage() {
  // The route path still spells `$authority`; a data root is an authority.
  const { authority } = authorityRoute.useParams()
  const navigate = useNavigate()
  const registry = useQuery(kindsQueryOptions)
  const kinds = useMemo(
    () =>
      (registry.data ?? [])
        .filter((k) => k.authority === authority)
        .sort((a, b) => a.name.localeCompare(b.name)),
    [registry.data, authority]
  )

  const columns = useMemo(() => buildColumns(authority), [authority])
  const table = useDataTable({
    columns,
    data: kinds,
    getRowId: (k) => k.identity,
    prefsKey: "authority-kinds",
  })

  if (registry.isPending) {
    return (
      <div className="flex flex-col gap-3 px-6 pt-5">
        <Skeleton className="h-6 w-56" />
        <Skeleton className="mt-1 h-3.5 w-40" />
        <div className="mt-3 flex flex-col gap-2">
          {Array.from({ length: 5 }, (_, i) => (
            <Skeleton key={i} className="h-8 w-full" />
          ))}
        </div>
      </div>
    )
  }

  if (registry.isError || !kinds.length) {
    return (
      <div className="flex flex-1 p-6">
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <FileCode2Icon />
            </EmptyMedia>
            <EmptyTitle>
              {registry.isError
                ? "The registry didn't load"
                : "No such authority"}
            </EmptyTitle>
            <EmptyDescription>
              {registry.isError ? (
                registry.error.message
              ) : (
                <>
                  <span className="data">{authority}</span> declares no kinds.
                </>
              )}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      </div>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-end justify-between gap-3 px-6 pt-5 pb-2">
        <div>
          <h1 className="data text-lg font-semibold">{authority}</h1>
          <p className="text-xs text-muted-foreground">
            {kinds.length} {kinds.length === 1 ? "kind" : "kinds"}
          </p>
        </div>
        <DataTableViewOptions table={table} />
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        <DataTable
          table={table}
          onRowClick={(k) =>
            void navigate({
              to: "/data/$authority/$plural",
              params: { authority: authority, plural: k.plural },
            })
          }
        />
      </div>
    </div>
  )
}
