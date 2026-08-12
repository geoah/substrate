/** Overview (`/`): "is everything okay, what needs me?" — three zones, every
 * tile a door, nothing decorative (IA ticket 003; charts deferred). Zone 1
 * answers what just happened (the changelog's feed, 60s refetch, no watch), zone 2
 * what needs a verdict (the queue's evidence cards), zone 3 what the substrate
 * holds (per-kind counts behind a concurrency gate, the repository's own
 * authorities only — the machinery is in the nav, not on the glance). Each
 * zone loads, empties and fails on its own — one slow surface never blanks
 * the glance. */

import { useQuery } from "@tanstack/react-query"

import { ActivityCard } from "@/components/home/activity-card"
import { DataCountsZone } from "@/components/home/data-counts"
import { MergeRequestsCard } from "@/components/home/merge-requests-card"
import { ZoneError } from "@/components/home/zone"
import { Skeleton } from "@/components/ui/skeleton"
import { buildKindNav, kindsQueryOptions } from "@/lib/api/kinds"

export function HomePage() {
  // One registry read feeds three zones: record links in the activity rows,
  // the MR pair peeks, and the data zone's authority → kind shape.
  const registry = useQuery(kindsQueryOptions)
  const kinds = registry.data ?? []
  const nav = buildKindNav(kinds)

  return (
    <div className="flex flex-col gap-6 px-6 py-5">
      <div>
        <h1 className="text-lg font-semibold">Overview</h1>
        <p className="text-xs text-muted-foreground">
          The latest activity, pending merge requests and what the substrate
          holds — every tile opens its surface.
        </p>
      </div>

      {/* The two cards share the row's height — a shorter card stretches so
          the blank sits inside its border, deliberate, not a dead zone
          between zones (codex finding, 2026-08-06). */}
      <div className="grid gap-4 lg:grid-cols-3">
        <div className="min-w-0 lg:col-span-2">
          <ActivityCard kinds={kinds} />
        </div>
        <div className="min-w-0">
          <MergeRequestsCard kinds={kinds} />
        </div>
      </div>

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
