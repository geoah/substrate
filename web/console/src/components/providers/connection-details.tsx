/** Connection details (technical mode): for each account, the core `sync`
 * trait rendered whole; the record triggers on its kind with their cursor,
 * lag, last fire, parked and pending counts and the wake and run verbs; its
 * newest runs off the `triggerrun` ledger; and its parked deliveries, each
 * with a retry. Cursors and queues render as counts, never as raw JSON. */

import {
  useMutation,
  useQueries,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { RotateCcwIcon, ZapIcon } from "lucide-react"

import { IdText } from "@/components/identity/id-text"
import { StateBadge } from "@/components/identity/state-badge"
import { SyncSummary } from "@/components/sync/sync-panel"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import {
  retryParked,
  runTrigger,
  triggerParkedQueryOptions,
  triggerRecordsQueryOptions,
  triggerRunsQueryOptions,
  triggerStatusesQueryOptions,
  wakeTrigger,
} from "@/lib/api/sync"
import type { SubstrateRecord, TriggerStatus } from "@/lib/api/types"
import { relativeTime, tableDateTime } from "@/lib/format"
import {
  statusesOnKind,
  triggersOnKind,
  type AccountView,
  type TriggerSource,
} from "@/lib/sync"

function useRefreshTriggers() {
  const queryClient = useQueryClient()
  return () => {
    void queryClient.invalidateQueries({ queryKey: ["triggers"] })
    void queryClient.invalidateQueries({ queryKey: ["sync"] })
    void queryClient.invalidateQueries({ queryKey: ["record"] })
  }
}

export function ConnectionDetails({ accounts }: { accounts: AccountView[] }) {
  const triggers = useQuery(triggerRecordsQueryOptions)
  const statuses = useQuery(triggerStatusesQueryOptions)
  return (
    <div className="flex flex-col gap-4">
      {accounts.map((account) => {
        const sources = triggersOnKind(triggers.data ?? [], account.record.kind)
        const byID = new Map(
          statusesOnKind(statuses.data ?? [], sources).map((s) => [s.id, s])
        )
        return (
          <div
            key={account.record.id}
            data-slot="connection-details"
            className="flex flex-col gap-4 rounded-[10px] border p-4"
          >
            <div className="flex flex-wrap items-baseline gap-x-2">
              <span className="font-medium">{account.label}</span>
              <IdText
                value={`${account.record.kind}/${account.record.id}`}
                copy
              />
            </div>
            {account.syncable ? (
              <SyncSummary
                fields={account.sync}
                legacyStatus={account.legacySyncStatus}
              />
            ) : (
              <p className="text-[12.5px] text-muted-foreground">
                This kind does not bind the core sync trait, so there is no sync
                state to show
                {account.legacySyncStatus
                  ? ` beyond its own status: ${account.legacySyncStatus}`
                  : ""}
                .
              </p>
            )}
            <Block title="Triggers on this kind">
              {sources.length === 0 ? (
                <p className="text-[12.5px] text-muted-foreground">
                  No record trigger names this kind.
                </p>
              ) : (
                <div className="overflow-x-auto">
                  <table className="w-full min-w-[560px] text-xs">
                    <thead>
                      <tr className="text-left text-faint">
                        <th className="py-1 pr-3 font-medium">Trigger</th>
                        <th className="py-1 pr-3 font-medium">Cursor / head</th>
                        <th className="py-1 pr-3 font-medium">Last fire</th>
                        <th className="py-1 pr-3 text-right font-medium">
                          Parked
                        </th>
                        <th className="py-1 pr-3 text-right font-medium">
                          Pending
                        </th>
                        <th className="py-1">
                          <span className="sr-only">Actions</span>
                        </th>
                      </tr>
                    </thead>
                    <tbody>
                      {sources.map((s) => (
                        <TriggerRow
                          key={s.id}
                          source={s}
                          status={byID.get(s.id)}
                          record={account.record}
                        />
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </Block>
            <Block title="Recent runs">
              <RunsList
                triggerIds={sources.map((s) => s.id)}
                recordId={account.record.id}
              />
            </Block>
            <Block title="Parked deliveries">
              <ParkedList
                triggerIds={sources.map((s) => s.id)}
                recordId={account.record.id}
              />
            </Block>
          </div>
        )
      })}
    </div>
  )
}

function Block({
  title,
  children,
}: {
  title: string
  children: React.ReactNode
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <h3 className="text-[12.5px] font-medium text-muted-foreground">
        {title}
      </h3>
      {children}
    </div>
  )
}

function TriggerRow({
  source,
  status,
  record,
}: {
  source: TriggerSource
  status: TriggerStatus | undefined
  record: SubstrateRecord
}) {
  const refresh = useRefreshTriggers()
  const wake = useMutation({
    mutationFn: () => wakeTrigger(source.id),
    onSuccess: (r) => {
      toast.add({
        type: "success",
        title: `${source.id} woke`,
        description: `${r.ran} ran.`,
      })
      refresh()
    },
    onError: (e) =>
      toast.add({
        type: "error",
        title: "Wake failed",
        description: e.message,
      }),
  })
  const run = useMutation({
    mutationFn: () => runTrigger(source.id, record.kind, record.id),
    onSuccess: (r) => {
      toast.add({
        type: "success",
        title: `${source.id} ran against this account`,
        description:
          r.ran > 0 ? `${r.ran} ran.` : "The guard said no; nothing ran.",
      })
      refresh()
    },
    onError: (e) =>
      toast.add({ type: "error", title: "Run failed", description: e.message }),
  })
  return (
    <tr className="border-t align-top">
      <td className="py-2 pr-3">
        <Link
          to="/data/$authority/$pkg/$name/$id"
          params={{
            authority: "substrate.reamde.dev",
            pkg: "core",
            name: "trigger",
            id: source.id,
          }}
          className="font-mono break-all hover:underline"
        >
          {source.id}
        </Link>
        <div className="text-muted-foreground">
          {source.ops.length ? source.ops.join(", ") : "every change"}
          {!source.enabled && " · off"}
        </div>
      </td>
      <td className="py-2 pr-3 tabular-nums">
        {status ? (
          <>
            {status.cursor ?? status.head} / {status.head}
            {(status.lag ?? 0) > 0 && (
              <span className="text-warning"> · {status.lag} behind</span>
            )}
          </>
        ) : (
          "—"
        )}
      </td>
      <td className="py-2 pr-3 text-muted-foreground" title={status?.lastFire}>
        {status?.lastFire ? relativeTime(status.lastFire) : "—"}
      </td>
      <td className="py-2 pr-3 text-right tabular-nums">
        {status ? (
          <span className={status.parked > 0 ? "text-destructive" : ""}>
            {status.parked}
          </span>
        ) : (
          "—"
        )}
      </td>
      <td className="py-2 pr-3 text-right tabular-nums">
        {status?.pending ?? "—"}
      </td>
      <td className="py-2 text-right">
        <div className="flex justify-end gap-1">
          <Button
            variant="ghost"
            size="xs"
            disabled={wake.isPending || !source.enabled}
            onClick={() => wake.mutate()}
            title="Drain this trigger's backlog now"
          >
            {wake.isPending ? <Spinner className="size-3" /> : <ZapIcon />}
            Wake
          </Button>
          <Button
            variant="ghost"
            size="xs"
            disabled={run.isPending || !source.enabled}
            onClick={() => run.mutate()}
            title="Deliver this account's current state through the trigger, guard applied"
          >
            {run.isPending ? <Spinner className="size-3" /> : <RotateCcwIcon />}
            Run
          </Button>
        </div>
      </td>
    </tr>
  )
}

function ParkedList({
  triggerIds,
  recordId,
}: {
  triggerIds: string[]
  recordId: string
}) {
  const refresh = useRefreshTriggers()
  const parked = useQueries({
    queries: triggerIds.map((id) => triggerParkedQueryOptions(id)),
  })
  const retry = useMutation({
    mutationFn: ({ trigger, id }: { trigger: string; id: number }) =>
      retryParked(trigger, id),
    onSuccess: (r) => {
      toast.add({
        type: "success",
        title: "Retried",
        description: r.ran > 0 ? `${r.ran} ran.` : "Nothing ran.",
      })
      refresh()
    },
    onError: (e) =>
      toast.add({
        type: "error",
        title: "Retry failed",
        description: e.message,
      }),
  })
  // This record's own failures, plus a fire's (a schedule occurrence names
  // no record and may have touched this one).
  const rows = parked
    .flatMap((q) => q.data ?? [])
    .filter((f) => !f.recordId || f.recordId === recordId)
  if (parked.some((q) => q.isPending))
    return <Skeleton className="h-10 w-full" />
  if (!rows.length) {
    return (
      <p className="text-[12.5px] text-muted-foreground">Nothing is parked.</p>
    )
  }
  return (
    <ul className="flex flex-col divide-y">
      {rows
        .sort((a, b) => b.parkedAt.localeCompare(a.parkedAt))
        .map((f) => (
          <li
            key={`${f.trigger}/${f.id}`}
            className="flex items-start justify-between gap-3 py-2"
          >
            <div className="min-w-0 text-xs">
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-mono font-medium">{f.trigger}</span>
                <span className="text-muted-foreground" title={f.parkedAt}>
                  {relativeTime(f.parkedAt)} · {f.attempts}{" "}
                  {f.attempts === 1 ? "attempt" : "attempts"}
                  {f.seq
                    ? ` · change ${f.seq}`
                    : f.fireId
                      ? ` · fire ${f.fireId}`
                      : ""}
                </span>
              </div>
              <p className="mt-1 break-words whitespace-pre-wrap text-destructive">
                {f.lastError}
              </p>
            </div>
            <Button
              variant="outline"
              size="xs"
              disabled={retry.isPending}
              onClick={() => retry.mutate({ trigger: f.trigger, id: f.id })}
            >
              <RotateCcwIcon />
              Retry
            </Button>
          </li>
        ))}
    </ul>
  )
}

const RUNS_PER_TRIGGER = 10

function RunsList({
  triggerIds,
  recordId,
}: {
  triggerIds: string[]
  recordId: string
}) {
  const runs = useQueries({
    queries: triggerIds.map((id) =>
      triggerRunsQueryOptions(id, RUNS_PER_TRIGGER)
    ),
  })
  if (runs.some((q) => q.isPending)) return <Skeleton className="h-10 w-full" />
  const rows = runs
    .flatMap((q) => q.data ?? [])
    // A record trigger's runs name the delivered record; keep this account's
    // and every run that names none (a schedule fire touches all accounts).
    .filter((r) => {
      const rec = r.properties.record
      return typeof rec !== "string" || rec === recordId
    })
    .sort((a, b) =>
      String(b.properties.finishedAt ?? "").localeCompare(
        String(a.properties.finishedAt ?? "")
      )
    )
  if (!rows.length) {
    return (
      <p className="text-[12.5px] text-muted-foreground">It hasn’t run yet.</p>
    )
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[560px] text-xs">
        <thead>
          <tr className="text-left text-faint">
            <th className="py-1 pr-3 font-medium">When</th>
            <th className="py-1 pr-3 font-medium">Trigger</th>
            <th className="py-1 pr-3 font-medium">Status</th>
            <th className="py-1 font-medium">Detail</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => {
            const status = String(r.properties.status ?? "")
            const finished =
              typeof r.properties.finishedAt === "string"
                ? r.properties.finishedAt
                : undefined
            const trigger = r.properties.trigger as
              { ref?: string } | string | undefined
            const triggerId =
              typeof trigger === "string"
                ? trigger.slice(trigger.lastIndexOf("/") + 1)
                : (trigger?.ref?.slice(trigger.ref.lastIndexOf("/") + 1) ?? "")
            const effects = r.properties.effects as
              Record<string, number> | undefined
            const reason =
              typeof r.properties.reason === "string" ? r.properties.reason : ""
            return (
              <tr key={r.id} className="border-t align-top">
                <td
                  className="py-1.5 pr-3 whitespace-nowrap text-muted-foreground"
                  title={finished}
                >
                  {finished ? tableDateTime(finished) : "—"}
                </td>
                <td className="py-1.5 pr-3 font-mono whitespace-nowrap">
                  {triggerId}
                </td>
                <td className="py-1.5 pr-3">
                  {status && <StateBadge value={status} />}
                </td>
                <td className="py-1.5 break-words">
                  {reason ? (
                    // The first line names the failure; the traceback behind
                    // it is the parked delivery's to show whole.
                    <span
                      className={
                        status === "parked"
                          ? "text-destructive"
                          : "text-muted-foreground"
                      }
                      title={reason}
                    >
                      {reason.split("\n")[0]}
                    </span>
                  ) : effects && Object.keys(effects).length ? (
                    <span className="text-muted-foreground">
                      {Object.entries(effects)
                        .map(([k, n]) => `${n} ${k}`)
                        .join(", ")}
                    </span>
                  ) : (
                    <span className="text-muted-foreground">—</span>
                  )}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
