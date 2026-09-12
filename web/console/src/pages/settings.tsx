/** Settings (`/settings`): every bundle that ships a `setting` or a `secret`
 * record, one ROW each, and `/settings/$id` is that bundle's form (decision
 * record 0076).
 *
 * A bundle ships its settings as records, empty where the person has to fill
 * them in, and the id prefix says which bundle owns which. So this page is a
 * read of two core collections and a grouping, not a second configuration
 * model: the same records are on the bundle's own page, in the same form.
 *
 * A list rather than every form stacked down one page: the set grows with each
 * bundle imported, and a page that renders all of them at once is a page that
 * gets longer forever. So it rides THE table system like every other list in
 * this console, and a row opens the one bundle's settings. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import { SearchXIcon, SlidersHorizontalIcon } from "lucide-react"

import { BundleSettingsForm } from "@/components/bundle-settings"
import { SetupBadge } from "@/components/bundle-state-badge"
import type { DataTableColumn } from "@/components/data-table/data-table"
import { DataTable, useDataTable } from "@/components/data-table/data-table"
import { DataTableColumnHeader } from "@/components/data-table/data-table-column-header"
import { DataTableViewOptions } from "@/components/data-table/data-table-view-options"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { bundleStatusesQueryOptions } from "@/lib/api/bundles"
import { settingRecordsQueryOptions } from "@/lib/api/settings"
import { relativeTime } from "@/lib/format"
import { groupSettings, unset, type SettingGroup } from "@/lib/settings"
import { bundleSettingsRoute } from "@/router"

/** The bundle's own name where its status is loaded, else the package word out
 * of its id: the row is named before the status read lands. */
function bundleName(id: string, names: Map<string, string>): string {
  return names.get(id) ?? (id.split("/").slice(1).join("/") || id)
}

/** One row of the list: a bundle's group with the three numbers the table
 * shows. They are folds of the same fields the form renders, so no surface
 * here asks the server a second question. */
interface SettingsRow {
  group: SettingGroup
  name: string
  /** Required settings still empty: what the sidebar's badge counts, per
   * bundle. */
  attention: number
  /** The latest write across the bundle's settings, as the row's `updated`. */
  updatedAt: string
}

function rowsOf(
  groups: SettingGroup[],
  names: Map<string, string>
): SettingsRow[] {
  return groups.map((group) => ({
    group,
    name: bundleName(group.bundle, names),
    attention: group.fields.filter(unset).length,
    updatedAt: group.fields.reduce(
      (latest, f) =>
        f.record.updatedAt > latest ? f.record.updatedAt : latest,
      ""
    ),
  }))
}

function settingsColumns(): DataTableColumn<SettingsRow>[] {
  return [
    {
      id: "bundle",
      accessorFn: (r) => r.name,
      enableSorting: false,
      enableHiding: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="bundle" />
      ),
      // The id under the name, not a description: a bundle's description lives
      // in the catalog, and a sample rehomed onto this repository's authority
      // has no catalog entry to read one from. The id is the one line that is
      // right for every bundle on this page.
      cell: ({ row }) => (
        <div className="min-w-0">
          <Link
            to="/settings/$id"
            params={{ id: row.original.group.bundle }}
            className="block truncate font-medium hover:underline"
          >
            {row.original.name}
          </Link>
          <div
            className="truncate data text-xs text-muted-foreground"
            title={row.original.group.bundle}
          >
            {row.original.group.bundle}
          </div>
        </div>
      ),
      meta: { label: "bundle", size: { min: 220, max: 460, weight: 1.5 } },
    },
    {
      id: "settings",
      accessorFn: (r) => r.group.fields.length,
      enableSorting: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="settings" />
      ),
      cell: ({ row }) => (
        <span className="data text-muted-foreground">
          {row.original.group.fields.length}
        </span>
      ),
      meta: { label: "settings", width: 100 },
    },
    {
      id: "attention",
      accessorFn: (r) => r.attention,
      enableSorting: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="needs attention" />
      ),
      // Nothing empty renders nothing: the chip is a signal, and a permanent
      // blank cell says the bundle is done.
      cell: ({ row }) => <SetupBadge count={row.original.attention} />,
      meta: { label: "needs attention", width: 150 },
    },
    {
      id: "updated",
      accessorFn: (r) => r.updatedAt,
      enableSorting: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="updated" align="right" />
      ),
      cell: ({ row }) =>
        row.original.updatedAt ? (
          <span
            className="block truncate data text-muted-foreground"
            title={row.original.updatedAt}
          >
            {relativeTime(row.original.updatedAt)}
          </span>
        ) : (
          <span className="text-muted-foreground">—</span>
        ),
      meta: {
        label: "updated",
        width: 120,
        headerClassName: "text-right",
        cellClassName: "text-right",
      },
    },
  ]
}

/** The read both surfaces make: the two core collections, grouped by the
 * bundle their ids name, with each bundle's own word where the status read has
 * landed. */
function useSettingGroups() {
  const records = useQuery(settingRecordsQueryOptions)
  const statuses = useQuery(bundleStatusesQueryOptions)
  const groups = useMemo(
    () => groupSettings(records.data ?? []),
    [records.data]
  )
  const names = useMemo(
    () => new Map((statuses.data ?? []).map((b) => [b.id, b.name])),
    [statuses.data]
  )
  return { records, groups, names }
}

function LoadFailed({
  message,
  onRetry,
}: {
  message: string
  onRetry: () => void
}) {
  return (
    <Empty className="py-10">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <SearchXIcon />
        </EmptyMedia>
        <EmptyTitle>The settings didn't load</EmptyTitle>
        <EmptyDescription>{message}</EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button variant="outline" size="sm" onClick={onRetry}>
          Retry
        </Button>
      </EmptyContent>
    </Empty>
  )
}

function NoSettings() {
  return (
    <Empty className="py-12">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <SlidersHorizontalIcon />
        </EmptyMedia>
        <EmptyTitle>No settings yet</EmptyTitle>
        <EmptyDescription>
          A bundle that needs settings ships them, so they appear here once you
          import it.
        </EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button variant="outline" size="sm" render={<Link to="/registry" />}>
          Open the registry
        </Button>
      </EmptyContent>
    </Empty>
  )
}

export function SettingsPage() {
  const navigate = useNavigate()
  const { records, groups, names } = useSettingGroups()
  const rows = useMemo(() => rowsOf(groups, names), [groups, names])
  const columns = useMemo(() => settingsColumns(), [])
  const table = useDataTable({
    columns,
    data: rows,
    getRowId: (row) => row.group.bundle,
    prefsKey: "settings",
  })

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-auto">
      <div className="shrink-0 px-6 pt-5 pb-2">
        <h1 className="text-lg font-semibold">Settings</h1>
        <p className="text-xs text-muted-foreground">
          What each bundle needs to run: its settings and its secrets. Open a
          bundle to fill them in.
        </p>
      </div>

      {records.isError ? (
        <div className="flex flex-1 px-6">
          <LoadFailed
            message={records.error.message}
            onRetry={() => void records.refetch()}
          />
        </div>
      ) : (
        <section className="flex flex-col">
          <div className="flex justify-end px-6">
            <DataTableViewOptions table={table} />
          </div>
          <DataTable
            table={table}
            loading={records.isPending}
            onRowClick={(row) =>
              void navigate({
                to: "/settings/$id",
                params: { id: row.group.bundle },
              })
            }
            empty={<NoSettings />}
          />
        </section>
      )}
    </div>
  )
}

/** One bundle's settings (`/settings/$id`): the same form the bundle's own
 * page renders, full width, under the bundle's name. The id in the path IS the
 * bundle id (`<authority>/<package>`), which is the prefix its setting records
 * carry, so nothing has to be looked up to know which records belong here. */
export function BundleSettingsPage() {
  const { id } = bundleSettingsRoute.useParams()
  const { records, groups, names } = useSettingGroups()
  const group = groups.find((g) => g.bundle === id)

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-auto">
      <div className="shrink-0 px-6 pt-5 pb-2">
        <h1 className="text-lg font-semibold">{bundleName(id, names)}</h1>
        <p className="text-xs text-muted-foreground">
          <Link
            to="/registry/$id"
            params={{ id }}
            className="data underline-offset-4 hover:underline"
          >
            {id}
          </Link>
        </p>
      </div>
      <div className="px-6 py-4">
        {records.isPending ? (
          <Skeleton className="h-40 w-full rounded-md" />
        ) : records.isError ? (
          <LoadFailed
            message={records.error.message}
            onRetry={() => void records.refetch()}
          />
        ) : group ? (
          <BundleSettingsForm fields={group.fields} />
        ) : (
          <Empty className="py-10">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <SlidersHorizontalIcon />
              </EmptyMedia>
              <EmptyTitle>No settings here</EmptyTitle>
              <EmptyDescription>
                This bundle ships no settings and no secrets.
              </EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <Button
                variant="outline"
                size="sm"
                render={<Link to="/settings" />}
              >
                Back to settings
              </Button>
            </EmptyContent>
          </Empty>
        )}
      </div>
    </div>
  )
}
