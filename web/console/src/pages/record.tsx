/** Record detail (`/data/:authority/:package/:kind/:id`): the views are TOP TABS
 * (owner ruling, 2026-08-08), no scrolling past one to reach another.
 * **Properties** leads and is the default (issue #38: a clicked row shows its
 * data field by field, not a YAML dump); **Manifest** is the document itself
 * (the YAML view, tinted, annotated and linkified: every key the kind
 * declares hovers with its DATATYPE and its one-liner, and references navigate
 * — reference paths, kinds, actors);
 * **Graph** sits beside Properties (both read the record as data, so they
 * neighbor); Activity and Provenance follow, none stacked underneath.
 * The active tab lives in the URL (`?tab=`) so it is linkable and back-button
 * friendly. An **Edit** action opens the YAML editor for this record. The layout
 * is generic — functions, kinds, triggers, agents and data records all render
 * through it.
 *
 * A VIEW attached to the record (`attach: record`, by the record's kind or
 * by a `via` pinned at it) is a card under the properties, scoped to this
 * record: on a phone the record reads top to bottom, its data first and
 * what hangs off it after. */

import { Suspense, lazy, useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import { FileQuestionIcon, PencilIcon } from "lucide-react"
import { parseAsStringLiteral, useQueryState } from "nuqs"

import { ViewBoundary } from "@/components/apps/view-boundary"
import { ActivityRail } from "@/components/record/activity"
import { GraphRail } from "@/components/record/graph"
import { PropertiesRail } from "@/components/record/properties"
import { ProvenanceRail } from "@/components/record/provenance"
import { YamlView } from "@/components/record/yaml-view"
import { StateBadge } from "@/components/state-badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardAction,
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
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { viewsQueryOptions } from "@/lib/api/apps"
import { recordQueryOptions } from "@/lib/api/records"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { ApiError, type KindInfo, type SubstrateRecord } from "@/lib/api/types"
import {
  recordPageParams,
  viewsForRecord,
  type AttachedView,
} from "@/lib/apps/attach"
import type { ActionHost, ViewContext } from "@/lib/apps/spec"
import { recordTitle } from "@/lib/format"
import { linkTargetsOf, manifestYAML } from "@/lib/manifest"
import {
  kindByCollection,
  kindByIdentity,
  stateProperties,
} from "@/lib/definition"
import { keyDocsOf } from "@/lib/yaml-annotations"
import { recordRoute } from "@/router"

/** The tab keys, in bar order; properties lead and are the default. A saved
 * `?tab=manifest` link still lands where it always did. */
const TABS = [
  "properties",
  "graph",
  "manifest",
  "activity",
  "provenance",
] as const
const tabParser = parseAsStringLiteral(TABS)
  .withDefault("properties")
  .withOptions({ history: "push" })

/** The apps runtime is one lazy chunk: a record with no attached view pays
 * nothing for it. */
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

export function RecordPage() {
  const { authority, pkg, name, id } = recordRoute.useParams()
  const [tab, setTab] = useQueryState("tab", tabParser)

  const registry = useQuery(kindsQueryOptions)
  const kindInfo = registry.data
    ? kindByCollection(registry.data, authority, pkg, name)
    : undefined
  const record = useQuery(recordQueryOptions(authority, pkg, name, id))

  const views = useQuery(viewsQueryOptions())
  const attached = useMemo(
    () =>
      record.data && registry.data && views.data
        ? viewsForRecord(views.data.records, registry.data, record.data)
        : [],
    [record.data, registry.data, views.data]
  )
  // One parent object per record, so every card's context holds still
  // across renders.
  const parent = useMemo(
    () =>
      record.data && kindInfo
        ? { record: record.data, kind: kindInfo }
        : undefined,
    [record.data, kindInfo]
  )

  // The hover vocabulary comes off the kinds query the page already holds —
  // one registry read backs every property tooltip on the manifest.
  const docs = useMemo(() => keyDocsOf(kindInfo), [kindInfo])
  const yaml = useMemo(
    () => (record.data ? manifestYAML(record.data) : ""),
    [record.data]
  )
  const targets = useMemo(
    () =>
      record.data && registry.data
        ? linkTargetsOf(record.data, registry.data)
        : undefined,
    [record.data, registry.data]
  )

  if (record.isPending || registry.isPending) {
    return <RecordSkeleton />
  }

  if (record.isError) {
    const notFound =
      record.error instanceof ApiError && record.error.code === "not_found"
    return (
      <div className="flex flex-1 p-6">
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <FileQuestionIcon />
            </EmptyMedia>
            <EmptyTitle>
              {notFound ? "No such record" : "The record didn't load"}
            </EmptyTitle>
            <EmptyDescription>
              <span className="data">
                {authority}/{name}/{id}
              </span>
              : {record.error.message}
            </EmptyDescription>
          </EmptyHeader>
          {!notFound && (
            <EmptyContent>
              <Button
                variant="outline"
                size="sm"
                onClick={() => void record.refetch()}
              >
                Retry
              </Button>
            </EmptyContent>
          )}
        </Empty>
      </div>
    )
  }

  const e = record.data
  const title = recordTitle(e.properties) || e.id
  const states = kindInfo
    ? stateProperties(kindInfo).filter(
        (p) => typeof e.properties[p.name] === "string"
      )
    : []

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-start justify-between gap-3 px-6 pt-5 pb-3">
        <div className="min-w-0">
          <h1 className="truncate text-lg font-semibold">{title}</h1>
          <p className="data text-xs text-muted-foreground">
            {authority}/{name}/{e.id}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-1.5 pt-0.5">
          {states.map((p) => (
            <StateBadge
              key={p.name}
              value={String(e.properties[p.name])}
              initial={p.initial}
            />
          ))}
          <Button
            variant="outline"
            size="sm"
            className="ml-1 gap-1.5"
            render={
              <Link
                to="/data/$authority/$pkg/$name/$id/edit"
                params={{
                  authority: authority,
                  pkg: pkg,
                  name,
                  id: e.id,
                }}
              />
            }
          >
            <PencilIcon className="size-3.5" />
            Edit
          </Button>
        </div>
      </div>

      <Tabs
        value={tab}
        onValueChange={(next) => void setTab(next as (typeof TABS)[number])}
        className="min-h-0 flex-1 gap-0"
      >
        <TabsList variant="line" className="mx-4 shrink-0 justify-start">
          <TabsTrigger value="properties">Properties</TabsTrigger>
          <TabsTrigger value="graph">Graph</TabsTrigger>
          <TabsTrigger value="manifest">Manifest</TabsTrigger>
          <TabsTrigger value="activity">Activity</TabsTrigger>
          <TabsTrigger value="provenance">Provenance</TabsTrigger>
        </TabsList>

        <TabsContent value="properties" className="min-h-0 border-t">
          <ScrollArea className="h-full">
            <PropertiesRail
              record={e}
              kind={kindInfo}
              kinds={registry.data ?? []}
            />
            {/* A card needs the record's declaration to scope through: no
                declaration, no cards. */}
            {parent && attached.length > 0 && (
              <div className="flex max-w-3xl flex-col gap-4 px-6 pb-6">
                {attached.map((v) => (
                  <RecordViewCard
                    key={v.spec.id}
                    view={v}
                    parent={parent}
                    kinds={registry.data ?? []}
                  />
                ))}
              </div>
            )}
          </ScrollArea>
        </TabsContent>
        <TabsContent value="graph" className="min-h-0 border-t">
          <ScrollArea className="h-full">
            <GraphRail
              authority={authority}
              pkg={pkg}
              name={name}
              record={e}
              kinds={registry.data ?? []}
            />
          </ScrollArea>
        </TabsContent>
        <TabsContent value="manifest" className="min-h-0 border-t">
          <ScrollArea className="h-full">
            <div className="px-2 pb-2">
              <YamlView source={yaml} docs={docs} targets={targets} />
            </div>
          </ScrollArea>
        </TabsContent>
        <TabsContent value="activity" className="min-h-0 border-t">
          <ScrollArea className="h-full">
            <ActivityRail record={e} />
          </ScrollArea>
        </TabsContent>
        <TabsContent value="provenance" className="min-h-0 border-t">
          <ScrollArea className="h-full">
            <ProvenanceRail propertyMeta={e.propertyMeta ?? {}} />
          </ScrollArea>
        </TabsContent>
      </Tabs>
    </div>
  )
}

/** One view attached to this record, as a card: the view's name heads it,
 * the renderer is scoped to the record through `via` (a project's card holds
 * that project's tasks alone, and its create prefills the project), the
 * view's primary and header actions are the card's trailing button, and a
 * row tap opens the row's own record page. */
function RecordViewCard({
  view,
  parent,
  kinds,
}: {
  view: AttachedView
  parent: { record: SubstrateRecord; kind: KindInfo }
  kinds: KindInfo[]
}) {
  const navigate = useNavigate()
  const ctx = useMemo<ViewContext>(
    () => ({ inputs: {}, parent, mode: "card" }),
    [parent]
  )
  const { spec } = view
  const kind = spec.kind ? kindByIdentity(kinds, spec.kind) : undefined
  const host: ActionHost = { spec, kind, kinds, ctx }
  const trailing = spec.actions.filter(
    (a) => a.placement === "primary" || a.placement === "header"
  )
  return (
    <Card size="sm" className="gap-2">
      <CardHeader>
        <CardTitle>{spec.name}</CardTitle>
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
        <Suspense fallback={<Skeleton className="mx-3 h-24" />}>
          <ViewBoundary label={spec.id}>
            <ViewRenderer
              spec={spec}
              kinds={kinds}
              ctx={ctx}
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

/** Mirrors the final layout: header block, tab bar, body. */
function RecordSkeleton() {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 px-6 pt-5 pb-3">
        <Skeleton className="h-6 w-48" />
        <Skeleton className="mt-1.5 h-3.5 w-64" />
      </div>
      <div className="flex shrink-0 gap-2 px-4 pb-3">
        <Skeleton className="h-7 w-20" />
        <Skeleton className="h-7 w-20" />
        <Skeleton className="h-7 w-20" />
        <Skeleton className="h-7 w-24" />
      </div>
      <div className="flex flex-col gap-2 border-t px-6 pt-4">
        {Array.from({ length: 12 }, (_, i) => (
          <Skeleton
            key={i}
            className="h-3.5"
            style={{
              width: `${[45, 60, 35, 70, 50, 40, 65, 30, 55, 45, 60, 38][i]}%`,
            }}
          />
        ))}
      </div>
    </div>
  )
}
