/** Authority page (`/data/:authority`) and package page
 * (`/data/:authority/:package`): the kinds under one authority, or under one of
 * its packages, at a glance on THE table system — package, name, description
 * where the declaration carries one, live record count (a bounded keyset-walk
 * count, capped collections read as N+), each row a door into that kind's
 * browse. Reached from the breadcrumb's authority and package segments. Bounded
 * registry data (a handful of rows), so no pagination seam. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import type { DataTableColumn } from "@/components/data-table/data-table"
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
import { authorityRoute, packageRoute } from "@/router"

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
  const count = useQuery(
    recordCountQueryOptions(kind.authority, kind.package, kind.name)
  )
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

function buildColumns(): DataTableColumn<KindInfo>[] {
  return [
    {
      id: "package",
      accessorFn: (k) => k.package,
      enableSorting: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="package" />
      ),
      cell: ({ row }) => (
        <span className="block truncate data">{row.original.package}</span>
      ),
      meta: { label: "package", width: 140 },
    },
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
          to="/data/$authority/$pkg/$name"
          params={{
            authority: row.original.authority,
            pkg: row.original.package,
            name: row.original.name,
          }}
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

/** The kinds table both data-root pages render: one heading, one table, one
 * empty state. `scope` is what the reader asked for and what the empty state
 * names; `title` is the heading. */
function KindsTable({
  scope,
  kinds,
  prefsKey,
  pending,
  error,
}: {
  scope: string
  kinds: KindInfo[]
  prefsKey: string
  pending: boolean
  error?: Error | null
}) {
  const navigate = useNavigate()
  const columns = useMemo(() => buildColumns(), [])
  const table = useDataTable({
    columns,
    data: kinds,
    getRowId: (k) => k.identity,
    prefsKey,
  })

  if (pending) {
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

  if (error || !kinds.length) {
    return (
      <div className="flex flex-1 p-6">
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <FileCode2Icon />
            </EmptyMedia>
            <EmptyTitle>
              {error ? "The registry didn't load" : "Nothing declared here"}
            </EmptyTitle>
            <EmptyDescription>
              {error ? (
                error.message
              ) : (
                <>
                  <span className="data">{scope}</span> declares no kinds.
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
          <h1 className="data text-lg font-semibold">{scope}</h1>
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
              to: "/data/$authority/$pkg/$name",
              params: { authority: k.authority, pkg: k.package, name: k.name },
            })
          }
        />
      </div>
    </div>
  )
}

/** Every kind one authority publishes, across its packages. */
export function AuthorityPage() {
  const { authority } = authorityRoute.useParams()
  const registry = useQuery(kindsQueryOptions)
  const kinds = useMemo(
    () =>
      (registry.data ?? [])
        .filter((k) => k.authority === authority)
        .sort(
          (a, b) =>
            a.package.localeCompare(b.package) || a.name.localeCompare(b.name)
        ),
    [registry.data, authority]
  )
  return (
    <KindsTable
      scope={authority}
      kinds={kinds}
      prefsKey="authority-kinds"
      pending={registry.isPending}
      error={registry.isError ? registry.error : null}
    />
  )
}

/** The kinds of ONE package, the group a declaration is versioned and
 * quarantined in (decision 0047). */
export function PackagePage() {
  const { authority, pkg } = packageRoute.useParams()
  const registry = useQuery(kindsQueryOptions)
  const kinds = useMemo(
    () =>
      (registry.data ?? [])
        .filter((k) => k.authority === authority && k.package === pkg)
        .sort((a, b) => a.name.localeCompare(b.name)),
    [registry.data, authority, pkg]
  )
  return (
    <KindsTable
      scope={`${authority}/${pkg}`}
      kinds={kinds}
      prefsKey="package-kinds"
      pending={registry.isPending}
      error={registry.isError ? registry.error : null}
    />
  )
}
