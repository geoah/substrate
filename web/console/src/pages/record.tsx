/** One record, read like a document and kept live off the change feed: its
 * head, the record views its kind has (decision record 0166), its
 * properties as a sheet that edits in place (a value that changes under the
 * reader is marked briefly), its prose, what it is connected to, where it
 * comes from, what was merged into it and its history, top to bottom on one
 * left-aligned page. Technical mode adds the record's own facts and a
 * Source toggle that shows the YAML envelope or the JSON. */

import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { FileQuestionIcon } from "lucide-react"

import { DocPage } from "@/components/identity/page-layout"
import { SectionHead } from "@/components/identity/section-head"
import { PropertySheet } from "@/components/property-sheet/property-sheet"
import { sheetRows } from "@/components/property-sheet/sheet-rows"
import { ConnectedSection } from "@/components/record/connected"
import { HistorySection } from "@/components/record/history"
import { RecordBody } from "@/components/record/record-body"
import { RecordDetails } from "@/components/record/record-details"
import { RecordHeader } from "@/components/record/record-header"
import { hasMerges } from "@/components/record/record-model"
import { MetricRollups } from "@/components/record/metric-rollups"
import { MergedSection, SourcesSection } from "@/components/record/sources"
import { useLiveRecord } from "@/components/record/use-live-record"
import { useRecordChanges } from "@/components/record/use-record-changes"
import { SourceView } from "@/components/record/source-view"
import { ObservedSection } from "@/components/observations/observed-section"
import { RecordViews } from "@/components/record-view/record-views"
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
import { Skeleton } from "@/components/ui/skeleton"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { useRecordViews } from "@/hooks/use-record-views"
import { kindsQueryOptions } from "@/lib/api/kinds"
import {
  recordMappingsQueryOptions,
  recordQueryOptions,
} from "@/lib/api/records"
import { ApiError, type KindInfo, type SubstrateRecord } from "@/lib/api/types"
import { kindByCollection } from "@/lib/definition"
import { displayName, lowerFirst } from "@/lib/kind-names"
import { METRIC_TRAIT, SYNC_TRAIT } from "@/lib/core-kinds"
import { OBSERVED_SHOWN, observedVariant } from "@/lib/observations"
import { readOnlyReason } from "@/lib/provenance"
import { foldedBy } from "@/lib/record-view"
import { kindHasTrait } from "@/lib/sync"
import { recordRoute } from "@/router"
import { useUnits } from "@/hooks/use-units"

const NO_KINDS: KindInfo[] = []

export function RecordPage() {
  const { authority, pkg, name, id } = recordRoute.useParams()
  const registry = useQuery(kindsQueryOptions)
  const kinds = registry.data ?? NO_KINDS
  const kind = registry.data
    ? kindByCollection(registry.data, authority, pkg, name)
    : undefined
  const record = useQuery(recordQueryOptions(authority, pkg, name, id))

  if (record.isPending || registry.isPending) return <RecordSkeleton />

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
              {notFound ? "This record isn’t here" : "The record didn’t load"}
            </EmptyTitle>
            <EmptyDescription>
              {notFound
                ? "It may have been deleted, or the link is wrong."
                : record.error.message}
              <span className="mt-1 block font-mono text-xs">
                {authority}/{pkg}/{name}/{id}
              </span>
            </EmptyDescription>
          </EmptyHeader>
          {!notFound && (
            <EmptyContent>
              <Button
                variant="outline"
                size="sm"
                onClick={() => void record.refetch()}
              >
                Try again
              </Button>
            </EmptyContent>
          )}
        </Empty>
      </div>
    )
  }

  return <RecordDocument record={record.data} kind={kind} kinds={kinds} />
}

export function RecordDocument({
  record,
  kind,
  kinds,
}: {
  record: SubstrateRecord
  kind?: KindInfo
  kinds: KindInfo[]
}) {
  const units = useUnits()
  const [technical] = useTechnicalDetails()
  const [source, setSource] = useState(false)
  const [holders, setHolders] = useState(false)
  // A provider's copy and an adapter's output are read only: the owner
  // changes the first upstream and corrects the second with a record of
  // their own (the header says how).
  const readOnly = Boolean(readOnlyReason(record))
  const { rows } = useRecordChanges(record)
  const moved = useLiveRecord(record)
  const mappingIds = useMemo(
    () => (record.linkedFrom ?? []).map((l) => l.mapping),
    [record.linkedFrom]
  )
  const mappings = useQuery(recordMappingsQueryOptions(mappingIds))
  const body = useMemo(
    () => sheetRows(units, record, kind, readOnly).body,
    [units, record, kind, readOnly]
  )
  const syncable = kindHasTrait(kind, SYNC_TRAIT)
  // A kind binding observed opens on its measurement, and its sheet folds
  // away what that section already shows; a record view folds away what
  // its own sections show.
  const variant = observedVariant(kind)
  const views = useRecordViews(kind)
  const elsewhere = useMemo(() => {
    const folded = foldedBy(views)
    const shown = variant ? OBSERVED_SHOWN[variant] : undefined
    if (!folded.length) return shown
    return {
      names: [...(shown?.names ?? []), ...folded],
      where: shown?.where ?? views[0]?.name ?? "the view above",
    }
  }, [variant, views])
  const noun = lowerFirst(displayName(kind ?? record.kind))

  return (
    <DocPage>
      <RecordHeader
        record={record}
        kind={kind}
        rows={rows}
        source={technical && source}
        onSource={setSource}
        holders={holders}
        onHolders={setHolders}
      />
      {technical && source ? (
        <SourceView
          record={record}
          kind={kind}
          kinds={kinds}
          readOnly={readOnly}
        />
      ) : (
        <>
          {variant && <ObservedSection record={record} kind={kind} />}
          <RecordViews
            views={views}
            record={record}
            kind={kind}
            kinds={kinds}
          />
          {kindHasTrait(kind, METRIC_TRAIT) && (
            <MetricRollups record={record} readOnly={readOnly} />
          )}
          <PropertySheet
            record={record}
            kind={kind}
            kinds={kinds}
            readOnly={readOnly}
            holders={holders}
            mappings={mappings.data?.records}
            elsewhere={elsewhere}
            moved={moved}
          />
          {body && (
            <RecordBody
              record={record}
              spec={body}
              readOnly={readOnly}
              moved={moved.has(body.name)}
            />
          )}
          {syncable && (
            <>
              <SectionHead title="Sync" hint="how this account keeps up" />
              <SyncRail record={record} />
            </>
          )}
          <SectionHead
            title="Connected to"
            hint="what points here, and what this points to"
          />
          <ConnectedSection record={record} kind={kind} kinds={kinds} />
          <SectionHead
            title="Where it comes from"
            hint="providers that fill this in"
          />
          <SourcesSection
            record={record}
            kind={kind}
            mappings={mappings.data?.records ?? []}
          />
          {hasMerges(record) && (
            <>
              <SectionHead title="Merged" />
              <MergedSection record={record} />
            </>
          )}
          <SectionHead title="History" hint="every change, newest first" />
          <HistorySection record={record} kind={kind} />
          {technical && (
            <>
              <SectionHead title="Record" hint={`this ${noun}’s own facts`} />
              <RecordDetails record={record} kind={kind} rows={rows} />
            </>
          )}
        </>
      )}
    </DocPage>
  )
}

function RecordSkeleton() {
  return (
    <DocPage>
      <Skeleton className="size-10 rounded-[10px]" />
      <Skeleton className="mt-3 h-8 w-2/3" />
      <Skeleton className="mt-2 h-3.5 w-1/2" />
      <div className="mt-6 flex flex-col gap-3">
        {[40, 55, 35, 50, 45].map((w, i) => (
          <div key={i} className="flex gap-6">
            <Skeleton className="h-4 w-28" />
            <Skeleton className="h-4" style={{ width: `${w}%` }} />
          </div>
        ))}
      </div>
    </DocPage>
  )
}
