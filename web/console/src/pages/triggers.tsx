/** Triggers (`/triggers`): the delivery bindings and their computed
 * bookkeeping — source kind, callable, enabled, the cursor/head lag, and the
 * parked-failure count surfaced as an actionable signal. On THE table system;
 * a row expands in place to its run ledger and its parked deliveries, with
 * the per-trigger verbs the API exposes (wake, replay, per-failure retry). */

import { useMemo, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import type { ColumnDef } from "@tanstack/react-table"
import { SearchXIcon, ZapIcon } from "lucide-react"

import { DataTable, useDataTable } from "@/components/data-table/data-table"
import { DataTableColumnHeader } from "@/components/data-table/data-table-column-header"
import { DataTableViewOptions } from "@/components/data-table/data-table-view-options"
import { RowDetail } from "@/components/data-table/row-detail"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import {
  replayTrigger,
  retryParked,
  triggerParkedQueryOptions,
  triggerRunsQueryOptions,
  triggerStatusesQueryOptions,
  wakeTrigger,
  type TriggerStatus,
} from "@/lib/api/triggers"
import type { SubstrateRecord } from "@/lib/api/types"
import { cellValue, relativeTime, shortDateTime } from "@/lib/format"
import { cn } from "@/lib/utils"

function EnabledBadge({ enabled }: { enabled: boolean }) {
  return (
    <Badge variant="outline" className="gap-1.5 font-normal">
      <span className={cn("size-1.5 rounded-full", enabled ? "bg-primary" : "bg-muted-foreground/50")} />
      <span className="data">{enabled ? "enabled" : "disabled"}</span>
    </Badge>
  )
}

function buildColumns(): ColumnDef<TriggerStatus, unknown>[] {
  return [
    {
      id: "trigger",
      accessorFn: (t) => t.id,
      enableSorting: false,
      enableHiding: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="trigger" />
      ),
      cell: ({ row }) => (
        <Link
          to="/data/$authority/$plural/$id"
          params={{ authority: "core.substrate.reamde.dev", plural: "triggers", id: row.original.id }}
          className="block truncate data underline-offset-4 hover:underline"
          title={row.original.id}
          onClick={(e) => e.stopPropagation()}
        >
          {row.original.id}
        </Link>
      ),
      meta: { label: "trigger", size: { min: 160, max: 340, weight: 1.25 } },
    },
    {
      id: "kind",
      accessorFn: (t) => t.kind,
      enableSorting: false,
      header: ({ column }) => <DataTableColumnHeader column={column} title="kind" />,
      cell: ({ row }) => (
        <span className="block truncate data text-muted-foreground">{row.original.kind}</span>
      ),
      meta: { label: "kind", width: 90 },
    },
    {
      id: "callable",
      accessorFn: (t) => t.callable,
      enableSorting: false,
      header: ({ column }) => <DataTableColumnHeader column={column} title="callable" />,
      cell: ({ row }) => (
        <span className="block truncate data text-muted-foreground" title={row.original.callable}>
          {row.original.callable}
        </span>
      ),
      meta: { label: "callable", size: { min: 140, max: 280, weight: 1 } },
    },
    {
      id: "enabled",
      accessorFn: (t) => t.enabled,
      enableSorting: false,
      header: ({ column }) => <DataTableColumnHeader column={column} title="enabled" />,
      cell: ({ row }) => <EnabledBadge enabled={row.original.enabled} />,
      meta: { label: "enabled", width: 110 },
    },
    {
      id: "lag",
      accessorFn: (t) => t.lag ?? 0,
      enableSorting: false,
      header: ({ column }) => <DataTableColumnHeader column={column} title="lag" align="right" />,
      cell: ({ row }) => {
        const t = row.original
        if (t.kind !== "record") return <span className="block text-right text-muted-foreground/50">—</span>
        const lag = t.lag ?? 0
        return (
          <span
            className={cn("block text-right data", lag > 0 ? "text-warning" : "text-muted-foreground")}
            title={`cursor ${t.cursor ?? 0}, head ${t.head}`}
          >
            {lag.toLocaleString()}
          </span>
        )
      },
      meta: { label: "lag", width: 80, headerClassName: "text-right", cellClassName: "text-right" },
    },
    {
      id: "parked",
      accessorFn: (t) => t.parked,
      enableSorting: false,
      header: ({ column }) => <DataTableColumnHeader column={column} title="parked" align="right" />,
      cell: ({ row }) => {
        const n = row.original.parked
        return (
          <span className={cn("block text-right data", n > 0 ? "text-destructive" : "text-muted-foreground")}>
            {n.toLocaleString()}
          </span>
        )
      },
      meta: { label: "parked", width: 80, headerClassName: "text-right", cellClassName: "text-right" },
    },
    {
      id: "error",
      accessorFn: (t) => t.error ?? "",
      enableSorting: false,
      header: ({ column }) => <DataTableColumnHeader column={column} title="error" />,
      cell: ({ row }) => {
        const err = row.original.error
        if (!err) return <span className="text-muted-foreground/50">—</span>
        return (
          <span className="block truncate data text-warning" title={err}>
            {err}
          </span>
        )
      },
      meta: { label: "error", size: { min: 140, weight: 1 } },
    },
  ]
}

// ── the expanded panel: verbs, runs, parked ─────────────────────────────────

function useTriggerMutation(run: () => Promise<{ ran?: number; from?: number }>, label: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: run,
    onSuccess: (res) => {
      const n = res.ran ?? 0
      toast.add({
        type: "success",
        title: `${label} — ${n} ${n === 1 ? "delivery" : "deliveries"}.`,
      })
      void queryClient.invalidateQueries()
    },
    onError: (error) => {
      toast.add({ type: "error", title: `${label} failed`, description: error.message })
      void queryClient.invalidateQueries()
    },
  })
}

/** A run/delivery outcome as a small dot+label chip. `ok` reads calm, `skipped`
 * recedes, anything else (error/parked) is destructive. */
function StatusChip({ status }: { status: string }) {
  const tone =
    status === "ok"
      ? "bg-primary"
      : status === "skipped"
        ? "bg-muted-foreground/40"
        : "bg-destructive"
  return (
    <Badge variant="outline" className="shrink-0 gap-1.5 font-normal">
      <span className={cn("size-1.5 rounded-full", tone)} />
      <span className="data">{status}</span>
    </Badge>
  )
}

/** A labeled runtime figure. Bounded numeric/temporal data, so a stat block
 * (label over value) is the right shape — never a comma-joined inline string. */
function Stat({
  label,
  value,
  tone,
  title,
}: {
  label: string
  value: React.ReactNode
  tone?: string
  title?: string
}) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-xs text-muted-foreground/70">{label}</span>
      <span className={cn("data text-sm", tone ?? "text-foreground")} title={title}>
        {value}
      </span>
    </div>
  )
}

/** One entry in the run ledger: the outcome chip and target flow on the top
 * line; a real error gets its own full-width, wrapped line so it's readable
 * rather than truncated into the row. */
function RunRow({ run }: { run: SubstrateRecord }) {
  const p = run.properties
  const status = typeof p.status === "string" ? p.status : "?"
  const target = cellValue(p.record ?? p.fireId ?? "") || "—"
  const error = typeof p.error === "string" ? p.error : ""
  const startedAt = typeof p.startedAt === "string" ? p.startedAt : ""
  return (
    <div className="flex flex-col gap-1 py-2">
      <div className="flex items-center gap-3">
        <StatusChip status={status} />
        <span className="min-w-0 flex-1 truncate data text-muted-foreground" title={target}>
          {target}
        </span>
        <span className="shrink-0 data text-xs text-muted-foreground" title={startedAt}>
          {startedAt ? relativeTime(startedAt) : "—"}
        </span>
      </div>
      {error && <p className="data pl-1 text-xs break-words text-destructive">{error}</p>}
    </div>
  )
}

/** A state-changing trigger verb the user must confirm before it runs — the
 * console's rule (GUIDE §8): destructive/state-changing acts pass a confirm
 * that states the posture. */
interface TriggerAction {
  title: string
  body: React.ReactNode
  confirmLabel: string
  destructive?: boolean
  run: () => void
}

function TriggerDetail({ trigger }: { trigger: TriggerStatus }) {
  const runs = useQuery(triggerRunsQueryOptions(trigger.id))
  const parked = useQuery({
    ...triggerParkedQueryOptions(trigger.id),
    enabled: trigger.parked > 0,
  })
  const queryClient = useQueryClient()
  const [confirming, setConfirming] = useState<TriggerAction | null>(null)

  const wake = useTriggerMutation(() => wakeTrigger(trigger.id), "Woke the trigger")
  const replay = useTriggerMutation(() => replayTrigger(trigger.id, 0), "Replayed from the start")

  const retry = useMutation({
    mutationFn: (fid: number) => retryParked(trigger.id, fid),
    onSuccess: (res) => {
      toast.add({ type: "success", title: `Retried — ${res.ran} applied.` })
      void queryClient.invalidateQueries()
    },
    onError: (error) => {
      toast.add({ type: "error", title: "Retry failed", description: error.message })
      void queryClient.invalidateQueries()
    },
  })

  const busy = wake.isPending || replay.isPending || retry.isPending
  const runRows = runs.data?.records ?? []

  return (
    <div className="flex flex-col gap-5 bg-muted/30 px-6 py-4">
      <div className="flex flex-wrap items-end justify-between gap-x-8 gap-y-4">
        <div className="flex flex-wrap items-end gap-x-8 gap-y-3">
          {trigger.kind === "record" ? (
            <>
              <Stat label="Cursor" value={(trigger.cursor ?? 0).toLocaleString()} />
              <Stat label="Head" value={trigger.head.toLocaleString()} />
              <Stat
                label="Lag"
                value={(trigger.lag ?? 0).toLocaleString()}
                tone={(trigger.lag ?? 0) > 0 ? "text-warning" : undefined}
              />
            </>
          ) : (
            <Stat label="Source" value={trigger.kind} />
          )}
          <Stat
            label="Last fire"
            value={trigger.lastFire ? relativeTime(trigger.lastFire) : "never"}
            title={trigger.lastFire ? shortDateTime(trigger.lastFire) : undefined}
          />
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            size="sm"
            className="h-8"
            disabled={busy}
            onClick={() =>
              setConfirming({
                title: `Wake ${trigger.id}?`,
                body: "Fires the trigger once now against current state — a real delivery through its callable, effects and all.",
                confirmLabel: "Wake now",
                run: () => wake.mutate(),
              })
            }
          >
            {wake.isPending && <Spinner className="size-3" />}
            Wake now
          </Button>
          {trigger.kind === "record" && (
            <Button
              variant="outline"
              size="sm"
              className="h-8"
              disabled={busy}
              onClick={() =>
                setConfirming({
                  title: `Replay ${trigger.id} from the start?`,
                  body: "Resets the cursor to seq 0 and re-delivers every matching change since. Idempotent effects and no-op suppression make replay safe, but it re-runs the whole history.",
                  confirmLabel: "Replay from start",
                  destructive: true,
                  run: () => replay.mutate(),
                })
              }
            >
              {replay.isPending && <Spinner className="size-3" />}
              Replay from start
            </Button>
          )}
        </div>
      </div>

      {confirming && (
        <Dialog open onOpenChange={(open) => !open && !busy && setConfirming(null)}>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>{confirming.title}</DialogTitle>
              <DialogDescription>{confirming.body}</DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button variant="outline" disabled={busy} onClick={() => setConfirming(null)}>
                Cancel
              </Button>
              <Button
                variant={confirming.destructive ? "destructive" : "default"}
                disabled={busy}
                onClick={() => {
                  confirming.run()
                  setConfirming(null)
                }}
              >
                {confirming.confirmLabel}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}

      <section className="flex flex-col gap-1">
        <h3 className="text-sm font-medium text-foreground">
          Recent runs
          {runRows.length > 0 && (
            <span className="ml-2 data text-xs font-normal text-muted-foreground">
              {runRows.length}
            </span>
          )}
        </h3>
        {runs.isPending ? (
          <Skeleton className="h-10 w-full" />
        ) : runRows.length ? (
          <div className="flex flex-col divide-y divide-border/60">
            {runRows.map((r) => (
              <RunRow key={r.id} run={r} />
            ))}
          </div>
        ) : (
          <p className="text-xs text-muted-foreground">No runs recorded yet.</p>
        )}
      </section>

      {trigger.parked > 0 && (
        <section className="flex flex-col gap-2">
          <h3 className="flex items-center gap-2 text-sm font-medium text-foreground">
            Parked deliveries
            <Badge variant="outline" className="gap-1.5 font-normal">
              <span className="size-1.5 rounded-full bg-destructive" />
              <span className="data">{trigger.parked}</span>
            </Badge>
          </h3>
          {parked.isPending ? (
            <Skeleton className="h-16 w-full" />
          ) : (
            <div className="flex flex-col gap-2">
              {(parked.data ?? []).map((f) => (
                <div key={f.id} className="rounded-md border bg-background/60 p-3">
                  <div className="flex items-start justify-between gap-3">
                    <div className="flex min-w-0 flex-col gap-0.5">
                      <span
                        className="truncate data text-foreground"
                        title={f.recordId || f.fireId || `seq ${f.seq}`}
                      >
                        {f.recordId || f.fireId || `seq ${f.seq}`}
                      </span>
                      <span className="text-xs text-muted-foreground">
                        {f.attempts} {f.attempts === 1 ? "attempt" : "attempts"}
                      </span>
                    </div>
                    <Button
                      variant="outline"
                      size="sm"
                      className="h-7 shrink-0"
                      disabled={busy}
                      onClick={() =>
                        setConfirming({
                          title: "Retry this parked delivery?",
                          body: "Re-runs the delivery against current state. On success the parked row clears; on failure it stays with the new error.",
                          confirmLabel: "Retry",
                          run: () => retry.mutate(f.id),
                        })
                      }
                    >
                      {retry.isPending && <Spinner className="size-3" />}
                      Retry
                    </Button>
                  </div>
                  <p className="data mt-2 text-xs break-words text-destructive">{f.lastError}</p>
                </div>
              ))}
            </div>
          )}
        </section>
      )}
    </div>
  )
}

export function TriggersPage() {
  const triggers = useQuery(triggerStatusesQueryOptions)
  const rows = useMemo(() => triggers.data ?? [], [triggers.data])
  const columns = useMemo(() => buildColumns(), [])
  const table = useDataTable({
    columns,
    data: rows,
    getRowId: (row) => row.id,
    prefsKey: "triggers",
  })

  if (triggers.isPending) return <TriggersSkeleton />

  if (triggers.isError) {
    return (
      <div className="flex flex-1 p-6">
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <SearchXIcon />
            </EmptyMedia>
            <EmptyTitle>The triggers didn't load</EmptyTitle>
            <EmptyDescription>{triggers.error.message}</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button variant="outline" size="sm" onClick={() => void triggers.refetch()}>
              Retry
            </Button>
          </EmptyContent>
        </Empty>
      </div>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-end justify-between gap-3 px-6 pt-5 pb-2">
        <div>
          <h1 className="text-lg font-semibold">Triggers</h1>
          <p className="text-xs text-muted-foreground">
            {rows.length.toLocaleString()} bindings, from{" "}
            <span className="data">core.substrate.reamde.dev/triggers</span>
          </p>
        </div>
        <DataTableViewOptions table={table} />
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        <DataTable
          table={table}
          renderExpanded={(row) => (
            <RowDetail className="border-0 bg-transparent p-0">
              <TriggerDetail trigger={row} />
            </RowDetail>
          )}
          empty={
            <Empty className="py-16">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <ZapIcon />
                </EmptyMedia>
                <EmptyTitle>No triggers</EmptyTitle>
                <EmptyDescription>
                  Nothing subscribes to the changelog on this substrate yet.
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          }
        />
      </div>
    </div>
  )
}

function TriggersSkeleton() {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 px-6 pt-5 pb-1">
        <Skeleton className="h-6 w-28" />
        <Skeleton className="mt-1.5 h-3.5 w-56" />
      </div>
      <div className="min-h-0 flex-1 overflow-hidden px-6 pt-4">
        {Array.from({ length: 5 }, (_, i) => (
          <div key={i} className="flex h-12 items-center gap-6 border-b last:border-0">
            <Skeleton className="h-4 w-40" />
            <Skeleton className="h-4 w-16" />
            <Skeleton className="ml-auto h-4 w-16" />
          </div>
        ))}
      </div>
    </div>
  )
}
