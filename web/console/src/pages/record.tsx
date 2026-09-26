/** One record, read like a document: its head, its properties as a sheet
 * that edits in place, its prose, what it is connected to, where it comes
 * from, what was merged into it and its history, top to bottom on one
 * left-aligned page. Technical mode adds the record's own facts and a
 * Source toggle that shows the YAML envelope. */

import { useMemo, useState, type ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"
import { FileQuestionIcon } from "lucide-react"

import { DocPage } from "@/components/identity/page-layout"
import { PropertySheet } from "@/components/property-sheet/property-sheet"
import { sheetRows } from "@/components/property-sheet/sheet-rows"
import { ConnectedSection } from "@/components/record/connected"
import { HistorySection } from "@/components/record/history"
import { RecordBody } from "@/components/record/record-body"
import { RecordDetails } from "@/components/record/record-details"
import { RecordHeader } from "@/components/record/record-header"
import { hasMerges } from "@/components/record/record-model"
import { MergedSection, SourcesSection } from "@/components/record/sources"
import { useRecordChanges } from "@/components/record/use-record-changes"
import { SourceView } from "@/components/record/source-view"
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
import { providerOfKind } from "@/lib/actor-identity"
import { kindsQueryOptions } from "@/lib/api/kinds"
import {
  recordMappingsQueryOptions,
  recordQueryOptions,
} from "@/lib/api/records"
import { ApiError, type KindInfo, type SubstrateRecord } from "@/lib/api/types"
import { kindByCollection } from "@/lib/definition"
import { displayName, lowerFirst } from "@/lib/kind-names"
import { SYNC_TRAIT_IDENTITY, kindHasTrait } from "@/lib/sync"
import { recordRoute } from "@/router"

const NO_KINDS: KindInfo[] = []

function SectionHead({ title, hint }: { title: string; hint?: ReactNode }) {
  return (
    <div className="mt-8 mb-2.5 flex flex-wrap items-baseline gap-x-2 gap-y-1">
      <h2 className="text-[15px] font-semibold tracking-[-0.01em]">{title}</h2>
      {hint && <span className="text-[12.5px] text-faint">{hint}</span>}
    </div>
  )
}

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
  const [technical] = useTechnicalDetails()
  const [source, setSource] = useState(false)
  const [holders, setHolders] = useState(false)
  const readOnly = Boolean(providerOfKind(record.kind))
  const { rows } = useRecordChanges(record)
  const mappingIds = useMemo(
    () => (record.linkedFrom ?? []).map((l) => l.mapping),
    [record.linkedFrom]
  )
  const mappings = useQuery(recordMappingsQueryOptions(mappingIds))
  const body = useMemo(
    () => sheetRows(record, kind, readOnly).body,
    [record, kind, readOnly]
  )
  const syncable = kindHasTrait(kind, SYNC_TRAIT_IDENTITY)
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
        <SourceView record={record} kind={kind} kinds={kinds} />
      ) : (
        <>
          <PropertySheet
            record={record}
            kind={kind}
            kinds={kinds}
            readOnly={readOnly}
            holders={holders}
            mappings={mappings.data?.records}
          />
          {body && (
            <RecordBody record={record} spec={body} readOnly={readOnly} />
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
