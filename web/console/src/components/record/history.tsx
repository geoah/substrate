/** A record's history as sentences: who did what, newest first, with the
 * properties each change touched. The changelog carries no values, only the
 * names of what moved (a state's new value rides beside them), so a row says
 * what changed and the record says what it is now.
 *
 * Two honesty rules hold (owner redline, 2026-08-06):
 * - Former ids are part of the record: the wire's `recordId` scope follows
 *   one id (plus the merge and split entries that name it), so a merged
 *   record's pre-merge history lives under its former ids. Those slices are
 *   read and stitched in after the live id's history is fully paged.
 * - Creation is always shown when derivable: when no `created` change row
 *   exists (history predates the retained changelog), the last row speaks
 *   the record's own `createdAt` and says plainly that the trail stops. */

import { useMemo } from "react"

import { ago } from "@/components/property-sheet/dates"
import { ActorRef } from "@/components/identity/actor-ref"
import { StateBadge } from "@/components/identity/state-badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import type { ChangeRow, KindInfo, SubstrateRecord } from "@/lib/api/types"
import { humanizeName, propSpecsByName } from "@/lib/record-schema"
import { changeSentence, plainChanges, stateMoves } from "./record-model"
import { useRecordChanges } from "./use-record-changes"

function Row({
  row,
  record,
  labels,
  formerIds,
}: {
  row: ChangeRow
  record: SubstrateRecord
  labels: Map<string, string>
  formerIds: string[]
}) {
  const [technical] = useTechnicalDetails()
  const created = row.op === "put" && row.payload?.created === true
  const moves = stateMoves(row)
  const changed = plainChanges(row)
  const label = (p: string) => labels.get(p) ?? humanizeName(p)
  const showChanged = changed.length > 0 && (!created || technical)
  return (
    <div
      data-slot="history-row"
      className="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-x-3.5 gap-y-1 border-b py-2.5"
    >
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-1.5 leading-normal">
          <ActorRef actor={row.actor} />
          <span>{changeSentence(row, record)}</span>
        </div>
        {(moves.length > 0 || showChanged) && (
          <div className="mt-1 flex flex-wrap items-center gap-x-2.5 gap-y-1 text-[12.5px] text-muted-foreground">
            {moves.map(([p, to]) => (
              <span key={p} className="inline-flex items-center gap-1.5">
                <span className="font-medium">{label(p)}</span>
                <span className="text-faint">→</span>
                <StateBadge value={to} />
              </span>
            ))}
            {showChanged &&
              changed.map((p) => (
                <span
                  key={p}
                  className="inline-flex items-center gap-1 font-medium"
                >
                  {created && <span className="text-ok">+</span>}
                  {label(p)}
                </span>
              ))}
          </div>
        )}
        {formerIds.includes(row.recordId) && row.recordId !== record.id && (
          <div className="mt-0.5 text-[12.5px] text-faint">
            before it was combined into this one
            {technical && (
              <span className="ml-1.5 font-mono text-[11px]">
                as {row.recordId}
              </span>
            )}
          </div>
        )}
      </div>
      <div className="flex flex-col items-end gap-0.5 text-[12.5px] whitespace-nowrap text-faint tabular-nums">
        <span title={row.ts}>{ago(row.ts)}</span>
        {technical && <span className="font-mono text-[11px]">#{row.seq}</span>}
      </div>
    </div>
  )
}

export function HistorySection({
  record,
  kind,
}: {
  record: SubstrateRecord
  kind?: KindInfo
}) {
  const { changes, rows } = useRecordChanges(record)
  const labels = useMemo(
    () =>
      new Map(
        (kind ? propSpecsByName(kind) : []).map((s) => [s.name, s.label])
      ),
    [kind]
  )
  const formerIds = record.formerIds ?? []

  if (changes.isPending) {
    return (
      <div className="flex flex-col gap-2 py-2">
        <Skeleton className="h-4 w-2/3" />
        <Skeleton className="h-4 w-1/2" />
      </div>
    )
  }
  if (changes.isError) {
    return (
      <p role="alert" className="text-[13px] text-destructive">
        The history didn’t load: {changes.error.message}{" "}
        <button className="underline" onClick={() => void changes.refetch()}>
          Try again
        </button>
      </p>
    )
  }
  const complete = !changes.hasNextPage
  const hasCreated = rows.some((r) => r.payload?.created === true)
  return (
    <div data-slot="history" className="flex flex-col">
      {rows.map((row) => (
        <Row
          key={row.seq}
          row={row}
          record={record}
          labels={labels}
          formerIds={formerIds}
        />
      ))}
      {complete && !hasCreated && (
        <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-x-3.5 border-b py-2.5">
          <div>
            <div>Added</div>
            <div className="mt-0.5 text-[12.5px] text-faint">
              {rows.length
                ? "Earlier changes are no longer kept."
                : "No changes are kept for this record."}
            </div>
          </div>
          <span
            className="text-[12.5px] text-faint tabular-nums"
            title={record.createdAt}
          >
            {ago(record.createdAt)}
          </span>
        </div>
      )}
      {changes.hasNextPage && (
        <div className="pt-3">
          <Button
            variant="outline"
            size="sm"
            disabled={changes.isFetchingNextPage}
            onClick={() => void changes.fetchNextPage()}
          >
            {changes.isFetchingNextPage && <Spinner className="size-3.5" />}
            Load more
          </Button>
        </div>
      )}
    </div>
  )
}
