/** Overview (`/`): "is everything okay, what needs me?" — three zones, every
 * tile a door, nothing decorative (IA ticket 003; charts deferred). Zone 1
 * answers what just happened (the changelog's feed, 60s refetch, no watch), zone 2
 * what needs a verdict (the merge queue's evidence cards and the pending
 * changes beneath them), zone 3 what the substrate
 * holds (per-kind counts, one probe at a time, the repository's own
 * authorities only — the machinery is in the nav, not on the glance). Each
 * zone loads, empties and fails on its own — one slow surface never blanks
 * the glance.
 *
 * An APP attached to home (`attach: home`) is a card of its own between the
 * verdict row and the data counts, its guest mounted inline and sized by
 * what it reports, up to 60 vh. The apps runtime is one lazy chunk, so an
 * overview with no attached app pays nothing for it; the page reads only
 * the raw row's `attach`. */

import { Suspense, lazy, useMemo } from "react"
import { useQuery } from "@tanstack/react-query"

import { ActivityCard } from "@/components/home/activity-card"
import { ChangeRequestsCard } from "@/components/home/change-requests-card"
import { DataCountsZone } from "@/components/home/data-counts"
import { MergeRequestsCard } from "@/components/home/merge-requests-card"
import { ZoneError, ZoneHeader } from "@/components/home/zone"
import { Skeleton } from "@/components/ui/skeleton"
import { appAttaches, appsQueryOptions } from "@/lib/api/apps"
import { buildKindNav, kindsQueryOptions } from "@/lib/api/kinds"

const AppCard = lazy(() =>
  import("@/components/apps/runtime").then((m) => ({ default: m.AppCard }))
)

export function HomePage() {
  // One registry read feeds three zones: record links in the activity rows,
  // the MR pair peeks, and the data zone's authority → kind shape.
  const registry = useQuery(kindsQueryOptions)
  const kinds = registry.data ?? []
  const nav = buildKindNav(kinds)

  const apps = useQuery(appsQueryOptions())
  const attached = useMemo(
    () => (apps.data?.records ?? []).filter((a) => appAttaches(a, "home")),
    [apps.data]
  )

  return (
    <div className="flex flex-col gap-6 px-6 py-5">
      <div>
        <h1 className="text-lg font-semibold">Overview</h1>
        <p className="text-xs text-muted-foreground">
          Recent activity, what needs a decision, and what this repository
          holds.
        </p>
      </div>

      {/* The cards share the row's height — a shorter card stretches so
          the blank sits inside its border, deliberate, not a dead zone
          between zones (codex finding, 2026-08-06). The verdict zone is one
          column of two queues: both ask the same question of the reader (is
          this right?) and differ only in what they would write. */}
      <div className="grid gap-4 lg:grid-cols-3">
        <div className="min-w-0 lg:col-span-2">
          <ActivityCard kinds={kinds} />
        </div>
        <div className="flex min-w-0 flex-col gap-4">
          <MergeRequestsCard kinds={kinds} />
          <ChangeRequestsCard kinds={kinds} />
        </div>
      </div>

      {attached.length > 0 && (
        <section className="flex flex-col gap-2.5">
          <ZoneHeader title="Apps" to="/apps" linkLabel="All apps" />
          <div className="grid gap-4 lg:grid-cols-2">
            {attached.map((app) => (
              <Suspense
                key={app.id}
                fallback={<Skeleton className="h-40 rounded-xl" />}
              >
                <AppCard app={app} at="home" />
              </Suspense>
            ))}
          </div>
        </section>
      )}

      {registry.isPending ? (
        <section className="flex flex-col gap-2.5">
          <Skeleton className="h-4 w-16" />
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {Array.from({ length: 2 }, (_, i) => (
              <Skeleton key={i} className="h-32 rounded-xl" />
            ))}
          </div>
        </section>
      ) : registry.isError ? (
        <section className="flex flex-col gap-2.5">
          <h2 className="text-sm font-semibold">Data</h2>
          <ZoneError
            message={registry.error.message}
            onRetry={() => void registry.refetch()}
          />
        </section>
      ) : (
        <DataCountsZone authorities={nav.authorities} />
      )}
    </div>
  )
}
