/** A view to a layout. Two ways in: a screen that already decoded the record
 * hands `spec`, `kinds`, `ctx` and its own `onOpenRecord`; a card on a record
 * page, a kind page or the overview hands the raw `view` record with a
 * `mode` and an optional `parent`, and the renderer reads the registry,
 * decodes the spec and, on a tap, pushes the view's own route with the row
 * (the `opens` view when it names one). Either way it joins the shared live
 * tail for what the view reads and switches on `layout` to one lazily
 * imported component per value, so a layout's own weight loads only when a
 * view of that layout is opened. A kind the registry lacks is the install
 * offer, a package below a floor the view declares is the shortfall in place
 * of rows, a blocking problem is the problem list, a warning sits in a strip
 * above the layout, and every layout is inside the boundary, so a throw takes
 * its own rectangle and nothing beside it. A view already open on the line
 * above this mount (`ctx.ancestors`: a detail that relates itself, two that
 * relate each other) is one line saying so and no layout, because an error
 * boundary catches a throw and not a tree that never stops growing. */

import { lazy, Suspense, useMemo, type ComponentType } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import { PackageIcon } from "lucide-react"

import { ProblemList, ProblemStrip } from "@/components/apps/problems"
import { ViewBoundary } from "@/components/apps/view-boundary"
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
import { useLiveRecords } from "@/hooks/use-live-records"
import { kindsQueryOptions } from "@/lib/api/kinds"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import {
  liveKinds,
  useFloorProblems,
  useImplementors,
} from "@/lib/apps/queries"
import { recordSegment } from "@/lib/apps/route"
import {
  blockingProblems,
  type Layout,
  type LayoutProps,
  type ViewContext,
  type ViewSpec,
} from "@/lib/apps/spec"
import { packageOf, viewSpec } from "@/lib/apps/view-spec"
import { kindByIdentity } from "@/lib/definition"

const LAYOUTS: Record<Layout, ComponentType<LayoutProps>> = {
  list: lazy(() =>
    import("@/components/apps/list-view").then((m) => ({
      default: m.ListView,
    }))
  ),
  board: lazy(() => import("@/components/apps/layouts/board")),
  timeline: lazy(() => import("@/components/apps/layouts/timeline")),
  contacts: lazy(() => import("@/components/apps/layouts/contacts")),
  detail: lazy(() => import("@/components/apps/layouts/detail")),
  form: lazy(() => import("@/components/apps/layouts/form")),
  custom: lazy(() =>
    import("@/components/apps/custom-view").then((m) => ({
      default: m.CustomView,
    }))
  ),
}

/** The install offer a view renders in place of its rows while the kind it
 * names is not in this repository. */
export function NeedsPackage({ identity }: { identity: string }) {
  return (
    <Empty className="py-16">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <PackageIcon />
        </EmptyMedia>
        <EmptyTitle>Needs {packageOf(identity)}</EmptyTitle>
        <EmptyDescription>
          This view reads <span className="data">{identity}</span>, which this
          repository does not have yet.
        </EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button variant="outline" render={<Link to="/registry" />}>
          Open the registry
        </Button>
      </EmptyContent>
    </Empty>
  )
}

function LayoutSkeleton() {
  return (
    <div className="flex flex-col gap-3 p-4">
      <Skeleton className="h-4 w-1/2" />
      <Skeleton className="h-4 w-2/3" />
      <Skeleton className="h-4 w-1/3" />
    </div>
  )
}

interface Decoded {
  spec: ViewSpec
  kinds: KindInfo[]
  ctx: ViewContext
  onOpenRecord: (record: SubstrateRecord) => void
}

interface Mounted {
  /** The view record; the renderer decodes it against the registry. */
  view: SubstrateRecord
  mode: ViewContext["mode"]
  /** The record a `via` view is scoped to, or a detail's subject. */
  parent?: ViewContext["parent"]
  /** Absent: a tap pushes the view's own route with the row. */
  onOpenRecord?: (record: SubstrateRecord) => void
}

export type ViewRendererProps = Decoded | Mounted

export function ViewRenderer(props: ViewRendererProps) {
  const registry = useQuery(kindsQueryOptions)
  const navigate = useNavigate()
  const ownSpec = "spec" in props ? props.spec : undefined
  const viewRecord = "view" in props ? props.view : undefined
  const spec = useMemo(
    () =>
      ownSpec ??
      (viewRecord && registry.data
        ? viewSpec(viewRecord, registry.data)
        : undefined),
    [ownSpec, viewRecord, registry.data]
  )
  const kinds = "kinds" in props ? props.kinds : (registry.data ?? [])
  const ownCtx = "ctx" in props ? props.ctx : undefined
  const mode = "mode" in props ? props.mode : "page"
  const parent = "parent" in props ? props.parent : undefined
  const ctx = useMemo<ViewContext>(
    () => ownCtx ?? { inputs: {}, parent, mode },
    [ownCtx, parent, mode]
  )
  const viewId = spec?.id
  const reentered = Boolean(viewId && ctx.ancestors?.includes(viewId))
  // The layout's context carries this view on the line, so a mount it makes
  // (a related section) can tell it is inside this one.
  const layoutCtx = useMemo<ViewContext>(
    () =>
      viewId ? { ...ctx, ancestors: [...(ctx.ancestors ?? []), viewId] } : ctx,
    [ctx, viewId]
  )
  const onOpenRecord =
    props.onOpenRecord ??
    ((record: SubstrateRecord) =>
      void navigate({
        to: "/views/$id/$record",
        params: {
          id: spec?.opens ?? spec?.id ?? "",
          record: recordSegment(record),
        },
      }))

  const kind = spec?.kind ? kindByIdentity(kinds, spec.kind) : undefined
  const implementors = useImplementors(
    spec ?? { trait: undefined, kind: undefined }
  )
  useLiveRecords(spec && !reentered ? liveKinds(spec, implementors) : undefined)
  const floors = useFloorProblems(spec)

  if (!spec) return <LayoutSkeleton />
  if (reentered) {
    return (
      <ProblemStrip
        problems={[
          {
            path: spec.id,
            message: `${spec.id} is already open above this`,
            severity: "warning",
          },
        ]}
      />
    )
  }
  const blocking = blockingProblems(spec.problems)
  const warnings = spec.problems.filter((p) => p.severity === "warning")
  if (spec.kind && !kind) return <NeedsPackage identity={spec.kind} />
  if (blocking.length) return <ProblemList problems={blocking} />
  // Below a floor the view declares, the shortfall stands in for the rows:
  // the cells and actions were written against properties the installed
  // package may not have yet.
  if (!floors) return <LayoutSkeleton />
  if (floors.length) return <ProblemList problems={floors} />

  const Layout = LAYOUTS[spec.layout]
  return (
    <ViewBoundary label={spec.id}>
      <ProblemStrip problems={warnings} />
      <Suspense fallback={<LayoutSkeleton />}>
        <Layout
          spec={spec}
          kind={kind}
          kinds={kinds}
          ctx={layoutCtx}
          onOpenRecord={onOpenRecord}
        />
      </Suspense>
    </ViewBoundary>
  )
}
