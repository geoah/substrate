import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { FileQuestionIcon, PencilIcon } from "lucide-react"
import { parseAsStringLiteral, useQueryState } from "nuqs"

import { ActivityRail } from "@/components/record/activity"
import { GraphRail } from "@/components/record/graph"
import { PropertiesRail } from "@/components/record/properties"
import { ProvenanceRail } from "@/components/record/provenance"
import { SourcesFooter, SourcesSection } from "@/components/record/sources"
import { YamlView } from "@/components/record/yaml-view"
import { StateBadge } from "@/components/state-badge"
import { SyncRail } from "@/components/sync/sync-rail"
import { Button } from "@/components/ui/button"
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
import { useReferenceTitles } from "@/hooks/use-reference-titles"
import {
  recordMappingsQueryOptions,
  recordQueryOptions,
} from "@/lib/api/records"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { ApiError, type KindInfo } from "@/lib/api/types"
import { recordTitle } from "@/lib/format"
import { linkTargetsOf, manifestYAML } from "@/lib/manifest"
import { referencePathsOf } from "@/lib/reference-titles"
import { stateProperties, kindByCollection } from "@/lib/definition"
import { SYNC_TRAIT_IDENTITY, kindHasTrait } from "@/lib/sync"
import { keyDocsOf } from "@/lib/yaml-annotations"
import { recordRoute } from "@/router"

/** The tab keys, in bar order; properties lead and are the default. A saved
 * `?tab=manifest` link still lands where it always did. */
const TABS = [
  "properties",
  "sync",
  "manifest",
  "graph",
  "activity",
  "provenance",
] as const
const tabParser = parseAsStringLiteral(TABS)
  .withDefault("properties")
  .withOptions({ history: "push" })

const NO_KINDS: KindInfo[] = []

export function RecordPage() {
  const { authority, pkg, name, id } = recordRoute.useParams()
  const [tab, setTab] = useQueryState("tab", tabParser)

  const registry = useQuery(kindsQueryOptions)
  // One stable empty registry: a fresh `[]` every render would re-derive
  // every memo that keys on it.
  const kinds = registry.data ?? NO_KINDS
  const kindInfo = registry.data
    ? kindByCollection(registry.data, authority, pkg, name)
    : undefined
  const record = useQuery(recordQueryOptions(authority, pkg, name, id))
  // The mapping declarations the record's sources name, ONCE per page: the
  // Provenance tab's groups and the Manifest's footer both read them.
  const mappingIds = useMemo(
    () => (record.data?.linkedFrom ?? []).map((l) => l.mapping),
    [record.data]
  )
  const mappings = useQuery(recordMappingsQueryOptions(mappingIds))
  const mappingRecords = mappings.data?.records ?? []

  // A GET does not expand (docs/api.md), so the pointers this record holds
  // arrive as bare paths and every pill would read as a record id. One
  // batched list read over them is what makes `assignee` read as a person's
  // name; the pill falls back to the id for anything it does not answer.
  const referenced = useMemo(
    () => (record.data ? referencePathsOf(record.data) : []),
    [record.data]
  )
  const referenceTitles = useReferenceTitles(referenced, kinds)
  // A kind binding the core `sync` trait grows a Sync tab: the trait's
  // renderer over this record, beside the properties it reads from.
  const syncable = kindHasTrait(kindInfo, SYNC_TRAIT_IDENTITY)

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
                {authority}/{pkg}/{name}/{id}
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
          <h1 className="text-2xl font-semibold tracking-tight break-words">
            {title}
          </h1>
          <p className="data text-xs text-muted-foreground">
            {e.kind}/{e.id}
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
          {syncable && <TabsTrigger value="sync">Sync</TabsTrigger>}
          <TabsTrigger value="manifest">Manifest</TabsTrigger>
          <TabsTrigger value="graph">Graph</TabsTrigger>
          <TabsTrigger value="activity">Activity</TabsTrigger>
          <TabsTrigger value="provenance">Provenance</TabsTrigger>
        </TabsList>

        <TabsContent value="properties" className="min-h-0 border-t">
          <ScrollArea className="h-full">
            <PropertiesRail
              record={e}
              kind={kindInfo}
              kinds={registry.data ?? []}
              titles={referenceTitles}
            />
          </ScrollArea>
        </TabsContent>
        {syncable && (
          <TabsContent value="sync" className="min-h-0 border-t">
            <ScrollArea className="h-full">
              <SyncRail record={e} />
            </ScrollArea>
          </TabsContent>
        )}
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
            <SourcesFooter
              record={e}
              mappings={mappingRecords}
              onOpen={() => void setTab("provenance")}
            />
          </ScrollArea>
        </TabsContent>
        <TabsContent value="activity" className="min-h-0 border-t">
          <ScrollArea className="h-full">
            <ActivityRail record={e} />
          </ScrollArea>
        </TabsContent>
        <TabsContent value="provenance" className="min-h-0 border-t">
          <ScrollArea className="h-full">
            <div className="flex max-w-5xl min-w-0 flex-col gap-6 p-6">
              <SourcesSection
                record={e}
                kinds={registry.data ?? []}
                mappings={mappingRecords}
                mappingsPending={mappings.isPending && mappingIds.length > 0}
              />
              <ProvenanceRail
                record={e}
                kind={kindInfo}
                kinds={registry.data ?? []}
                referenceTitles={referenceTitles}
              />
            </div>
          </ScrollArea>
        </TabsContent>
      </Tabs>
    </div>
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
