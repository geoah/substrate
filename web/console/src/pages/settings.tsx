/** Settings (`/settings`): every `setting` and `secret` record in the
 * repository, one card per bundle that owns some (decision record 0076).
 *
 * A bundle ships its settings as records, empty where the person has to fill
 * them in, and the id prefix says which bundle owns which. So this page is a
 * read of two core collections and a grouping, not a second configuration
 * model: the same records are on the bundle's own page, in the same form. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { SearchXIcon, SlidersHorizontalIcon } from "lucide-react"

import { BundleSettingsForm } from "@/components/bundle-settings"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
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
import { groupSettings, type SettingGroup } from "@/lib/settings"

/** The bundle's own name where its status is loaded, else the package word out
 * of its id: the card is titled before the status read lands. */
function bundleName(id: string, names: Map<string, string>): string {
  return names.get(id) ?? (id.split("/").slice(1).join("/") || id)
}

function BundleCard({
  group,
  names,
}: {
  group: SettingGroup
  names: Map<string, string>
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <Link
            to="/registry/$id"
            params={{ id: group.bundle }}
            className="underline-offset-4 hover:underline"
          >
            {bundleName(group.bundle, names)}
          </Link>
        </CardTitle>
        <CardDescription>
          <span className="data">{group.bundle}</span>
        </CardDescription>
      </CardHeader>
      <CardContent>
        <BundleSettingsForm fields={group.fields} />
      </CardContent>
    </Card>
  )
}

export function SettingsPage() {
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

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 px-6 pt-5 pb-2">
        <h1 className="text-lg font-semibold">Settings</h1>
        <p className="text-xs text-muted-foreground">
          What each bundle needs to run: its settings and its secrets. A secret
          never reads back, so leave one blank to keep the stored value.
        </p>
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        <div className="flex max-w-3xl flex-col gap-4 px-6 py-4">
          {records.isPending ? (
            <>
              <Skeleton className="h-40 w-full rounded-md" />
              <Skeleton className="h-40 w-full rounded-md" />
            </>
          ) : records.isError ? (
            <Empty className="py-10">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <SearchXIcon />
                </EmptyMedia>
                <EmptyTitle>The settings didn't load</EmptyTitle>
                <EmptyDescription>{records.error.message}</EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => void records.refetch()}
                >
                  Retry
                </Button>
              </EmptyContent>
            </Empty>
          ) : groups.length === 0 ? (
            <Empty className="py-10">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <SlidersHorizontalIcon />
                </EmptyMedia>
                <EmptyTitle>No settings yet</EmptyTitle>
                <EmptyDescription>
                  A bundle that needs settings ships them, so they appear here
                  once you import it.
                </EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button
                  variant="outline"
                  size="sm"
                  render={<Link to="/registry" />}
                >
                  Open the registry
                </Button>
              </EmptyContent>
            </Empty>
          ) : (
            groups.map((group) => (
              <BundleCard key={group.bundle} group={group} names={names} />
            ))
          )}
        </div>
      </div>
    </div>
  )
}
