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
 * A VIEW attached to home (`attach: home`) is a card of its own between the
 * verdict row and the data counts, in card mode, each inside its boundary. */

import { Suspense, lazy, useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"

import { ViewBoundary } from "@/components/apps/view-boundary"
import { ActivityCard } from "@/components/home/activity-card"
import { ChangeRequestsCard } from "@/components/home/change-requests-card"
import { DataCountsZone } from "@/components/home/data-counts"
import { MergeRequestsCard } from "@/components/home/merge-requests-card"
import { ZoneError, ZoneHeader } from "@/components/home/zone"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { viewsQueryOptions } from "@/lib/api/apps"
import { buildKindNav, kindsQueryOptions } from "@/lib/api/kinds"
import type { KindInfo } from "@/lib/api/types"
import {
  homeViews,
  recordPageParams,
  type AttachedView,
} from "@/lib/apps/attach"
import type { ActionHost, ViewContext } from "@/lib/apps/spec"
import { kindByIdentity } from "@/lib/definition"

/** The apps runtime is one lazy chunk: an overview with no attached view
 * pays nothing for it. */
const ViewRenderer = lazy(() =>
  import("@/components/apps/view-renderer").then((m) => ({
    default: m.ViewRenderer,
  }))
)
const ActionButton = lazy(() =>
  import("@/components/apps/action-button").then((m) => ({
    default: m.ActionButton,
  }))
)

/** A home card is a card-mode mount with no app and no parent. */
const CARD_CTX: ViewContext = { inputs: {}, mode: "card" }

export function HomePage() {
  // One registry read feeds three zones: record links in the activity rows,
  // the MR pair peeks, and the data zone's authority → kind shape.
  const registry = useQuery(kindsQueryOptions)
  const kinds = registry.data ?? []
  const nav = buildKindNav(kinds)

  const views = useQuery(viewsQueryOptions())
  const attached = useMemo(
    () =>
      registry.data && views.data
        ? homeViews(views.data.records, registry.data)
        : [],
    [registry.data, views.data]
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
          <ZoneHeader title="Views" to="/apps" linkLabel="All views and apps" />
          <div className="grid gap-4 lg:grid-cols-2">
            {attached.map((v) => (
              <HomeViewCard key={v.spec.id} view={v} kinds={kinds} />
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

/** One view attached to home, as a card: its name is the door to the view's
 * own page (every tile a door), the view's primary and header actions are
 * the card's trailing button, the renderer draws in card mode, and a row tap
 * opens the row's own record page. */
function HomeViewCard({
  view,
  kinds,
}: {
  view: AttachedView
  kinds: KindInfo[]
}) {
  const navigate = useNavigate()
  const { spec } = view
  const kind = spec.kind ? kindByIdentity(kinds, spec.kind) : undefined
  const host: ActionHost = { spec, kind, kinds, ctx: CARD_CTX }
  const trailing = spec.actions.filter(
    (a) => a.placement === "primary" || a.placement === "header"
  )
  // A string, not a literal: the view route is the launcher's and the
  // router's registered set is not this page's to assert.
  const href: string = `/views/${encodeURIComponent(spec.id)}`
  return (
    <Card size="sm" className="min-w-0 gap-2">
      <CardHeader>
        <CardTitle>
          <Link to={href} className="underline-offset-4 hover:underline">
            {spec.name}
          </Link>
        </CardTitle>
        {spec.description && (
          <CardDescription>{spec.description}</CardDescription>
        )}
        {trailing.length > 0 && (
          <CardAction className="flex items-center gap-1">
            <Suspense fallback={null}>
              {trailing.map((action) => (
                <ActionButton
                  key={action.name}
                  host={host}
                  action={action}
                  compact
                  className="md:h-8"
                />
              ))}
            </Suspense>
          </CardAction>
        )}
      </CardHeader>
      <CardContent className="px-0">
        <Suspense fallback={<Skeleton className="mx-3 h-32" />}>
          <ViewBoundary label={spec.id}>
            <ViewRenderer
              spec={spec}
              kinds={kinds}
              ctx={CARD_CTX}
              onOpenRecord={(record) =>
                void navigate({
                  to: "/data/$authority/$pkg/$name/$id",
                  params: recordPageParams(record),
                })
              }
            />
          </ViewBoundary>
        </Suspense>
      </CardContent>
    </Card>
  )
}
