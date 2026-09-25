/** "Connected to": the records that point here, grouped by the kind they are
 * and the property they point through ("Tasks with this as their
 * Assignee"), and the records this one points to. A group whose kind has a
 * done-like state carries how much of it is done. The mapping slots a
 * provider's copies point through are not here: those are "Where it comes
 * from". */

import { useMemo, useState } from "react"
import { useInfiniteQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import { friendlyDay } from "@/components/property-sheet/dates"
import { KindGlyph } from "@/components/identity/kind-glyph"
import { KindPath, KindRef } from "@/components/identity/kind-ref"
import { RecordRef } from "@/components/identity/record-ref"
import { StateBadge } from "@/components/identity/state-badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import {
  groupReferencing,
  recordPath,
  referencingInfiniteOptions,
  referencingRows,
  type ReferencingGroup,
} from "@/lib/api/records"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { kindByIdentity, temporalProperties } from "@/lib/definition"
import { recordTitle } from "@/lib/format"
import {
  displayName,
  displayPlural,
  lowerFirst,
  untitled,
} from "@/lib/kind-names"
import { humanizeName, propSpecsByName } from "@/lib/record-schema"
import {
  GROUP_FOLD,
  MERGE_REQUEST_KIND,
  REVIEW_ROUTES,
  connectedGroups,
  everydayGroups,
  sortConnected,
  doneState,
  outgoingOf,
} from "./record-model"

function propertyLabel(kind: KindInfo | undefined, property: string): string {
  const spec = kind
    ? propSpecsByName(kind).find((s) => s.name === property)
    : undefined
  return spec?.label ?? humanizeName(property)
}

function GroupHead({
  group,
  record,
  kinds,
}: {
  group: ReferencingGroup
  record: SubstrateRecord
  kinds: KindInfo[]
}) {
  const [technical] = useTechnicalDetails()
  const source = kindByIdentity(kinds, group.kind)
  if (technical) {
    return (
      <span className="flex min-w-0 flex-wrap items-center gap-1.5">
        <KindRef kind={group.kind} mode="reference" />
        <span>records point here through their</span>
        <code className="rounded bg-hover px-1 font-mono text-[11.5px] text-foreground">
          {group.property}
        </code>
        <span>property</span>
      </span>
    )
  }
  if (REVIEW_ROUTES[group.kind]) {
    const n = group.rows.length
    const merge = group.kind === MERGE_REQUEST_KIND
    return (
      <span className="flex min-w-0 flex-wrap items-center gap-1.5">
        <KindGlyph kind={group.kind} size="xs" />
        <b className="font-medium text-foreground">
          {merge ? "Possible duplicates" : "Suggested changes"}
        </b>
        <span>
          {merge
            ? `${n} ${n === 1 ? "record" : "records"} that may be the same as this one`
            : `${n} ${n === 1 ? "change" : "changes"} suggested for this one`}
        </span>
      </span>
    )
  }
  const plural = displayPlural(source ?? group.kind)
  if (group.kind === record.kind && group.property === "parent") {
    return (
      <span className="flex min-w-0 flex-wrap items-center gap-1.5">
        <KindGlyph kind={group.kind} size="xs" />
        <b className="font-medium text-foreground">Sub{lowerFirst(plural)}</b>
        <span>smaller {lowerFirst(plural)} that are part of this one</span>
      </span>
    )
  }
  return (
    <span className="flex min-w-0 flex-wrap items-center gap-1.5">
      <KindGlyph kind={group.kind} size="xs" />
      <b className="font-medium text-foreground">{plural}</b>
      <span>
        with this as their{" "}
        <b className="font-medium">{propertyLabel(source, group.property)}</b>
      </span>
    </span>
  )
}

function Group({
  group,
  record,
  kinds,
  partial,
}: {
  group: ReferencingGroup
  record: SubstrateRecord
  kinds: KindInfo[]
  partial: boolean
}) {
  const [technical] = useTechnicalDetails()
  const [all, setAll] = useState(false)
  const source = kindByIdentity(kinds, group.kind)
  const progress = doneState(source)
  const stateSpec = source
    ? propSpecsByName(source).find((s) => s.kind === "state")
    : undefined
  const due = source ? temporalProperties(source)[0] : undefined
  const ordered = sortConnected(group.rows, progress, due)
  const rows = all ? ordered : ordered.slice(0, GROUP_FOLD)
  const total = group.rows.length
  const done = progress
    ? group.rows.filter(
        (r) => r.record.properties[progress.property] === progress.done
      ).length
    : 0
  return (
    <div
      data-slot="connected-group"
      className="overflow-hidden rounded-[10px] border"
    >
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1.5 border-b bg-panel px-3 py-[9px] text-[12.5px] text-muted-foreground">
        <GroupHead group={group} record={record} kinds={kinds} />
        <span className="ml-auto text-faint tabular-nums">
          {progress ? (
            <span className="inline-flex items-center gap-2">
              <span
                aria-hidden
                className="relative block h-[5px] w-20 overflow-hidden rounded-[3px] bg-border"
              >
                <span
                  className="absolute inset-y-0 left-0 rounded-[3px] bg-ok"
                  style={{
                    width: `${total ? Math.round((done / total) * 100) : 0}%`,
                  }}
                />
              </span>
              {done} of {total}
              {partial ? "+" : ""} done
            </span>
          ) : (
            `${total}${partial ? "+" : ""}`
          )}
        </span>
      </div>
      {rows.map((row) => {
        const state = stateSpec
          ? row.record.properties[stateSpec.name]
          : undefined
        const when = due ? row.record.properties[due] : undefined
        return (
          <div
            key={`${row.record.kind}/${row.record.id}/${row.path ?? ""}`}
            className="flex min-h-9 flex-wrap items-center gap-3 border-b px-3 last:border-b-0"
          >
            {REVIEW_ROUTES[row.record.kind] ? (
              <Link
                to={REVIEW_ROUTES[row.record.kind]}
                params={{ id: row.record.id }}
                className="inline-flex min-w-0 items-center gap-[5px] text-foreground no-underline"
              >
                <KindGlyph kind={row.record.kind} size="xs" />
                <span className="truncate border-b border-border-strong leading-tight hover:border-muted-foreground">
                  {recordTitle(row.record.properties) ||
                    untitled(row.record.kind)}
                </span>
              </Link>
            ) : (
              <RecordRef
                kind={row.record.kind}
                id={row.record.id}
                title={recordTitle(row.record.properties) || undefined}
              />
            )}
            <span className="ml-auto flex items-center gap-3 text-[12.5px] text-faint">
              {typeof state === "string" && (
                <StateBadge value={state} initial={stateSpec?.initial} />
              )}
              {typeof when === "string" && (
                <span title={when}>{friendlyDay(when)}</span>
              )}
              {technical && (
                <span className="font-mono text-[11px] [overflow-wrap:anywhere]">
                  {recordPath(row.record.kind, row.record.id)}
                </span>
              )}
            </span>
          </div>
        )
      })}
      {total > GROUP_FOLD && (
        <button
          type="button"
          onClick={() => setAll((v) => !v)}
          className="w-full px-3 py-2 text-left text-[12.5px] text-muted-foreground hover:bg-hover"
        >
          {all ? "Show fewer" : `Show all ${total}`}
        </button>
      )}
    </div>
  )
}

export function ConnectedSection({
  record,
  kind,
  kinds,
}: {
  record: SubstrateRecord
  kind?: KindInfo
  kinds: KindInfo[]
}) {
  const [technical] = useTechnicalDetails()
  const referencing = useInfiniteQuery(
    referencingInfiniteOptions(recordPath(record.kind, record.id), 200)
  )
  const groups = useMemo(() => {
    const all = connectedGroups(
      groupReferencing(
        (referencing.data?.pages ?? []).flatMap(referencingRows)
      ),
      record
    )
    return technical
      ? all
      : everydayGroups(all, new Map(kinds.map((k) => [k.identity, k])))
  }, [referencing.data, record, technical, kinds])
  const outgoing = useMemo(() => outgoingOf(record, kind), [record, kind])

  return (
    <div data-slot="connected" className="flex flex-col gap-3">
      {referencing.isPending ? (
        <Skeleton className="h-9 w-full rounded-[10px]" />
      ) : referencing.isError ? (
        <p role="alert" className="text-[13px] text-destructive">
          What points here didn’t load: {referencing.error.message}{" "}
          <button
            className="underline"
            onClick={() => void referencing.refetch()}
          >
            Try again
          </button>
        </p>
      ) : groups.length ? (
        groups.map((group) => (
          <Group
            key={`${group.kind} ${group.property}`}
            group={group}
            record={record}
            kinds={kinds}
            partial={Boolean(referencing.hasNextPage)}
          />
        ))
      ) : (
        <p className="py-0.5 text-[13px] text-faint">
          Nothing else points to this yet.
        </p>
      )}
      {referencing.hasNextPage && (
        <div>
          <Button
            variant="outline"
            size="sm"
            disabled={referencing.isFetchingNextPage}
            onClick={() => void referencing.fetchNextPage()}
          >
            {referencing.isFetchingNextPage && <Spinner className="size-3.5" />}
            Load more
          </Button>
        </div>
      )}
      {outgoing.length > 0 && (
        <>
          <div className="mt-1.5 -mb-1 text-[11.5px] font-medium text-faint">
            This {lowerFirst(displayName(record.kind))} points to
          </div>
          <div className="overflow-hidden rounded-[10px] border">
            {outgoing.map((o) => (
              <div
                key={`${o.property} ${o.kind}/${o.id}`}
                className="flex min-h-9 flex-wrap items-center gap-3 border-b px-3 last:border-b-0"
              >
                {technical ? (
                  <code className="rounded bg-hover px-1 font-mono text-[11.5px]">
                    {o.property}
                  </code>
                ) : (
                  <span className="font-medium">{o.label}</span>
                )}
                <span aria-hidden className="text-faint">
                  →
                </span>
                <RecordRef kind={o.kind} id={o.id} />
                <span className="ml-auto text-[12.5px] text-faint">
                  {technical ? (
                    <KindPath reference={o.kind} className="text-[11.5px]" />
                  ) : (
                    displayName(o.kind)
                  )}
                </span>
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  )
}
