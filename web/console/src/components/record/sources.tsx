/** "Where it comes from": the provider copies that fill this record in,
 * grouped by the mapping that links them ("Google contact fills in Name,
 * Emails"), each copy with when it last filled anything in. And "Merged":
 * the records combined into this one, each with the merge that joined it and
 * the request that proposed it. */

import { useState } from "react"
import { useQuery } from "@tanstack/react-query"

import { ago } from "@/components/property-sheet/dates"
import { IdText } from "@/components/identity/id-text"
import { KindPath, KindRef } from "@/components/identity/kind-ref"
import { ProviderBadge } from "@/components/identity/provider-badge"
import { RecordRef } from "@/components/identity/record-ref"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { providerOfKind } from "@/lib/actor-identity"
import { CORE_PACKAGE } from "@/lib/api/http"
import {
  mergeRequestsForQueryOptions,
  mergesIntoQueryOptions,
  recordPath,
} from "@/lib/api/records"
import {
  readReference,
  type KindInfo,
  type SubstrateRecord,
} from "@/lib/api/types"
import { displayName, lowerFirst } from "@/lib/kind-names"
import {
  groupSources,
  holderOf,
  syncedAt,
  type SourceGroup,
} from "@/lib/provenance"
import { humanizeName, propSpecsByName } from "@/lib/record-schema"
import { splitRecordPath } from "@/lib/record-path"

/** How many copies a group shows before "Show all". */
const FOLD_AT = 10

function Code({ children }: { children: string }) {
  return (
    <code className="rounded bg-hover px-1 font-mono text-[11.5px] text-foreground">
      {children}
    </code>
  )
}

function Group({
  group,
  record,
  kind,
}: {
  group: SourceGroup
  record: SubstrateRecord
  kind?: KindInfo
}) {
  const [technical] = useTechnicalDetails()
  const [all, setAll] = useState(false)
  const provider = providerOfKind(group.from)
  const labels = new Map(
    (kind ? propSpecsByName(kind) : []).map((s) => [s.name, s.label])
  )
  const shown = all ? group.members : group.members.slice(0, FOLD_AT)
  return (
    <div
      data-slot="source-group"
      className="overflow-hidden rounded-[10px] border"
    >
      <div className="flex flex-col gap-1.5 border-b bg-panel px-3 py-[9px] text-[12.5px] text-muted-foreground">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
          {provider && <ProviderBadge provider={provider} size="xs" />}
          <b className="font-medium text-foreground">
            {displayName(group.from)}
          </b>
          <span>
            {group.contributes.length ? "fills in" : "is linked here"}
          </span>
          {group.contributes.map((p, i) => (
            <span key={p} className="font-medium">
              {labels.get(p) ?? humanizeName(p)}
              {i < group.contributes.length - 1 ? "," : ""}
            </span>
          ))}
          <span className="ml-auto text-faint tabular-nums">
            {group.members.length}
          </span>
        </div>
        {technical && (
          <div className="flex flex-wrap items-center gap-1.5">
            <span>mapping</span>
            <IdText value={group.mapping} copy />
            <span>· from</span>
            <KindRef kind={group.from} mode="reference" />
            <span>through its</span>
            <Code>{group.property}</Code>
            <span>slot</span>
            {!group.declared && (
              <span className="text-destructive">
                · the mapping’s declaration is missing
              </span>
            )}
          </div>
        )}
      </div>
      {shown.map((m) => {
        const at = syncedAt(record, m.ref)
        return (
          <div
            key={m.ref}
            className="flex min-h-9 flex-wrap items-center gap-3 border-b px-3 last:border-b-0"
          >
            <RecordRef kind={m.kind} id={m.id} title={m.title} />
            <span className="ml-auto flex items-center gap-3 text-[12.5px] text-faint">
              {at && <span title={at}>synced {ago(at)}</span>}
              {technical && <IdText value={m.ref} copy />}
            </span>
          </div>
        )
      })}
      {group.members.length > FOLD_AT && (
        <button
          type="button"
          onClick={() => setAll((v) => !v)}
          className="w-full px-3 py-2 text-left text-[12.5px] text-muted-foreground hover:bg-hover"
        >
          {all ? "Show fewer" : `Show all ${group.members.length}`}
        </button>
      )}
    </div>
  )
}

export function SourcesSection({
  record,
  kind,
  mappings,
}: {
  record: SubstrateRecord
  kind?: KindInfo
  /** The recordmapping declarations the links name, read once per page. */
  mappings: SubstrateRecord[]
}) {
  const [technical] = useTechnicalDetails()
  const provider = providerOfKind(record.kind)
  const links = record.linkedFrom
  if (provider) {
    return (
      <p className="py-0.5 text-[13px] text-faint">
        This is {provider.name}’s own copy. {provider.name} keeps it up to date,
        and it fills in your own records where a mapping links them.
      </p>
    )
  }
  if (!links) {
    const others = [
      ...new Set(
        Object.values(record.propertyMeta ?? {})
          .map(holderOf)
          .filter((h) => h && h.mark !== "you")
          .map((h) => h!.label)
      ),
    ]
    return (
      <div className="flex flex-col gap-1 py-0.5 text-[13px] text-faint">
        <p>
          {others.length
            ? `${others.join(" and ")} set values here directly. No provider’s copy is linked to it.`
            : "Only you have added to this. No provider fills it in."}
        </p>
        {technical && (
          <p className="flex flex-wrap items-center gap-1.5">
            <span>No record mapping targets</span>
            <KindPath reference={record.kind} className="text-[12px]" />
          </p>
        )}
      </div>
    )
  }
  if (!links.length) {
    return (
      <p className="py-0.5 text-[13px] text-faint">
        Only you have added to this so far. A provider fills it in once one of
        its records matches.
      </p>
    )
  }
  const groups = groupSources(links, mappings)
  return (
    <div data-slot="sources" className="flex flex-col gap-3">
      {groups.map((g) => (
        <Group key={g.mapping} group={g} record={record} kind={kind} />
      ))}
    </div>
  )
}

/** The records merged into this one. */
export function MergedSection({ record }: { record: SubstrateRecord }) {
  const former = record.formerIds ?? []
  const path = recordPath(record.kind, record.id)
  const merges = useQuery(mergesIntoQueryOptions(path, former.length > 0))
  const requests = useQuery(
    mergeRequestsForQueryOptions(path, former.length > 0)
  )
  const rows = merges.data?.records ?? []
  const loserOf = (m: SubstrateRecord) =>
    readReference(m.properties.loser)?.path
  const requestFor = (loser: string | undefined) =>
    (requests.data?.records ?? []).find(
      (r) => readReference(r.properties.loser)?.path === loser
    )
  const unmatched = former.filter(
    (id) => !rows.some((m) => loserOf(m) === recordPath(record.kind, id))
  )
  const noun = lowerFirst(displayName(record.kind))
  return (
    <div data-slot="merged" className="flex flex-col gap-3">
      {rows.map((merge) => (
        <MergedRow
          key={merge.id}
          merge={merge}
          request={requestFor(loserOf(merge))}
          record={record}
        />
      ))}
      {unmatched.map((id) => (
        <div
          key={id}
          className="flex flex-wrap items-center gap-x-2 gap-y-1.5 rounded-[10px] border px-3.5 py-3 text-[13px]"
        >
          <RecordRef kind={record.kind} id={id} />
          <span className="text-muted-foreground">
            was combined into this {noun}.
          </span>
        </div>
      ))}
      {merges.isError && (
        <p role="alert" className="text-[12.5px] text-destructive">
          The merges didn’t load.{" "}
          <button className="underline" onClick={() => void merges.refetch()}>
            Try again
          </button>
        </p>
      )}
    </div>
  )
}

function MergedRow({
  merge,
  request,
  record,
}: {
  merge: SubstrateRecord
  request?: SubstrateRecord
  record: SubstrateRecord
}) {
  const [technical] = useTechnicalDetails()
  const loser = splitRecordPath(
    readReference(merge.properties.loser)?.path ?? ""
  )
  const mergeKind = `${CORE_PACKAGE}/recordmerge`
  const requestKind = `${CORE_PACKAGE}/recordmergerequest`
  const noun = lowerFirst(displayName(record.kind))
  return (
    <div
      data-slot="merge"
      className="flex flex-col gap-2 rounded-[10px] border px-3.5 py-3 text-[13px]"
    >
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1.5">
        <RecordRef kind={record.kind} id={loser?.id ?? merge.id} />
        <span className="text-muted-foreground">
          was combined into this {noun}{" "}
          <span title={merge.createdAt}>{ago(merge.createdAt)}</span>
          {request ? ", after you confirmed they were the same." : "."}
        </span>
      </div>
      {technical && (
        <div className="flex flex-wrap items-center gap-1.5 text-[12.5px] text-faint">
          <span>merge</span>
          <RecordRef kind={mergeKind} id={merge.id} />
          <IdText value={`${mergeKind}/${merge.id}`} copy />
          {request && (
            <>
              <span>· request</span>
              <RecordRef kind={requestKind} id={request.id} />
            </>
          )}
          {loser && (
            <>
              <span>· former id</span>
              <Code>{loser.id}</Code>
              <span>still resolves here</span>
            </>
          )}
        </div>
      )}
    </div>
  )
}
