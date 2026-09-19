/** One Connection (`/connections/$authority/$pkg/$name/$id`): the account
 * record's properties, the core `sync` trait rendered whole, the provider's
 * record triggers on this kind with their cursor, lag, last fire, parked and
 * pending from `…/trigger/status`, the newest runs of those triggers off the
 * `triggerrun` ledger with status and error text, the parked deliveries with
 * a retry each, and the row counts of the bundle's mirror kinds. Cursors and
 * pending queues render as counts, never as raw JSON. */

import { useMemo } from "react"
import {
  useMutation,
  useQueries,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { ArrowUpRightIcon, RotateCcwIcon, ZapIcon } from "lucide-react"

import {
  HealthDot,
  SyncActions,
  SyncSummary,
} from "@/components/sync/sync-panel"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import { useLiveRecords } from "@/hooks/use-live-records"
import { useOAuthConnect } from "@/hooks/use-oauth-connect"
import { bundleStatusQueryOptions } from "@/lib/api/bundles"
import { kindsQueryOptions } from "@/lib/api/kinds"
import {
  formatCount,
  recordCountQueryOptions,
  recordQueryOptions,
} from "@/lib/api/records"
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
import { cellValue, relativeTime, tableDateTime } from "@/lib/format"
import { kindPackage } from "@/lib/definition"
import {
  accountViewOf,
  cursorText,
  requestTriggers,
  statusesOnKind,
  triggersOnKind,
  type TriggerSource,
} from "@/lib/sync"
import { connectionDetailRoute } from "@/router"

/** The trait's own properties, rendered by the sync panel rather than the
 * grid, and the host's secret, which never shows anyway. */
const SYNC_PROPS = new Set([
  "syncState",
  "syncMessage",
  "lastSyncedAt",
  "lastSyncStartedAt",
  "lastSyncDurationMs",
  "syncRequestedAt",
  "syncRequestedAck",
  "syncPaused",
  "syncProgress",
  "syncError",
  "syncErrorAt",
  "syncStreams",
  "tokenRef",
  "title",
])

/** A property whose name says it is a cursor or a queue renders as a count:
 * the value is connector-private and a reader wants its size. */
function isBulk(name: string, value: unknown): boolean {
  if (typeof value !== "object" || value === null) return false
  return /(cursor|pending|resume|queue|token)/i.test(name)
}

function PropertyGrid({ record }: { record: SubstrateRecord }) {
  const rows = Object.entries(record.properties).filter(
    ([key]) => !SYNC_PROPS.has(key)
  )
  if (!rows.length) {
    return (
      <p className="text-xs text-muted-foreground">
        This record has no other properties.
      </p>
    )
  }
  return (
    <dl className="grid grid-cols-[12rem_minmax(0,1fr)] gap-x-4 gap-y-1.5 text-xs">
      {rows.map(([key, value]) => (
        <div key={key} className="contents">
          <dt className="truncate data text-muted-foreground" title={key}>
            {key}
          </dt>
          <dd className="min-w-0 break-words">
            {isBulk(key, value) ? (
              <span className="text-muted-foreground">{cursorText(value)}</span>
            ) : (
              cellValue(value) || (
                <span className="text-muted-foreground">—</span>
              )
            )}
          </dd>
        </div>
      ))}
    </dl>
  )
}

function Section({
  title,
  children,
  aside,
}: {
  title: string
  children: React.ReactNode
  aside?: React.ReactNode
}) {
  return (
    <section className="flex flex-col gap-2 rounded-md border p-4">
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-sm font-medium">{title}</h2>
        {aside}
      </div>
      {children}
    </section>
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
  const queryClient = useQueryClient()
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ["triggers"] })
    void queryClient.invalidateQueries({ queryKey: ["sync"] })
    void queryClient.invalidateQueries({ queryKey: ["record"] })
  }
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
          className="data font-medium break-all hover:underline"
        >
          {source.id}
        </Link>
        <div className="text-xs text-muted-foreground">
          {source.ops.length ? source.ops.join(", ") : "every op"}
          {!source.enabled && " · disabled"}
        </div>
      </td>
      <td className="py-2 pr-3 data text-xs tabular-nums">
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
      <td
        className="py-2 pr-3 text-xs text-muted-foreground"
        title={status?.lastFire}
      >
        {status?.lastFire ? relativeTime(status.lastFire) : "—"}
      </td>
      <td className="py-2 pr-3 text-right data text-xs tabular-nums">
        {status ? (
          <span className={status.parked > 0 ? "text-destructive" : ""}>
            {status.parked}
          </span>
        ) : (
          "—"
        )}
      </td>
      <td className="py-2 pr-3 text-right data text-xs tabular-nums">
        {status?.pending ?? "—"}
      </td>
      <td className="py-2 text-right">
        <div className="flex justify-end gap-1">
          <Button
            variant="ghost"
            size="sm"
            className="h-7 gap-1 px-2 text-xs"
            disabled={wake.isPending || !source.enabled}
            onClick={() => wake.mutate()}
            title="Drain this trigger's backlog now"
          >
            {wake.isPending ? (
              <Spinner className="size-3" />
            ) : (
              <ZapIcon className="size-3" />
            )}
            Wake
          </Button>
          <Button
            variant="ghost"
            size="sm"
            className="h-7 gap-1 px-2 text-xs"
            disabled={run.isPending || !source.enabled}
            onClick={() => run.mutate()}
            title="Deliver this account's current state through the trigger, guard applied"
          >
            {run.isPending ? (
              <Spinner className="size-3" />
            ) : (
              <RotateCcwIcon className="size-3" />
            )}
            Run
          </Button>
        </div>
      </td>
    </tr>
  )
}

function ParkedList({ triggerIds }: { triggerIds: string[] }) {
  const queryClient = useQueryClient()
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
      void queryClient.invalidateQueries({ queryKey: ["triggers"] })
      void queryClient.invalidateQueries({ queryKey: ["sync"] })
      void queryClient.invalidateQueries({ queryKey: ["record"] })
    },
    onError: (e) =>
      toast.add({
        type: "error",
        title: "Retry failed",
        description: e.message,
      }),
  })
  const rows = parked.flatMap((q) => q.data ?? [])
  if (parked.some((q) => q.isPending))
    return <Skeleton className="h-10 w-full" />
  if (!rows.length) {
    return (
      <p className="text-xs text-muted-foreground">No parked deliveries.</p>
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
                <span className="data font-medium">{f.trigger}</span>
                <span className="text-muted-foreground" title={f.parkedAt}>
                  {relativeTime(f.parkedAt)} · {f.attempts}{" "}
                  {f.attempts === 1 ? "attempt" : "attempts"}
                  {f.seq
                    ? ` · seq ${f.seq}`
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
              size="sm"
              className="h-7 shrink-0 gap-1 px-2 text-xs"
              disabled={retry.isPending}
              onClick={() => retry.mutate({ trigger: f.trigger, id: f.id })}
            >
              <RotateCcwIcon className="size-3" />
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
      <p className="text-xs text-muted-foreground">No runs recorded yet.</p>
    )
  }
  return (
    <table className="w-full text-xs">
      <thead>
        <tr className="text-left text-muted-foreground">
          <th className="py-1 pr-3 font-medium">When</th>
          <th className="py-1 pr-3 font-medium">Trigger</th>
          <th className="py-1 pr-3 font-medium">Mode</th>
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
          return (
            <tr key={r.id} className="border-t align-top">
              <td
                className="py-1.5 pr-3 whitespace-nowrap text-muted-foreground"
                title={finished}
              >
                {finished ? tableDateTime(finished) : "—"}
              </td>
              <td className="py-1.5 pr-3 data break-all">{triggerId}</td>
              <td className="py-1.5 pr-3 data">
                {String(r.properties.mode ?? "")}
              </td>
              <td className="py-1.5 pr-3">
                <Badge
                  variant="outline"
                  className={`font-normal ${status === "parked" ? "text-destructive" : status === "skipped" ? "text-muted-foreground" : ""}`}
                >
                  <span className="data">{status}</span>
                </Badge>
              </td>
              <td className="py-1.5 break-words">
                {typeof r.properties.reason === "string" &&
                r.properties.reason ? (
                  <span
                    className={
                      status === "parked"
                        ? "text-destructive"
                        : "text-muted-foreground"
                    }
                  >
                    {r.properties.reason}
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
  )
}

/** The bundle's mirror kinds and their live row counts: every kind of the
 * owned package but the config and account kinds themselves. */
function MirrorCounts({ providerId }: { providerId: string }) {
  const registry = useQuery(kindsQueryOptions)
  const status = useQuery(bundleStatusQueryOptions(providerId))
  const mirrors = useMemo(
    () =>
      (registry.data ?? []).filter((k) => {
        if (kindPackage(k) !== providerId) return false
        const traits = (k.definition as { traits?: unknown }).traits
        return !(
          Array.isArray(traits) &&
          traits.some((t) => t === "accountconfig" || t === "oauth2")
        )
      }),
    [registry.data, providerId]
  )
  const counts = useQueries({
    queries: mirrors.map((k) =>
      recordCountQueryOptions(k.authority, k.package, k.name)
    ),
  })
  if (registry.isPending) return <Skeleton className="h-10 w-full" />
  if (!mirrors.length) {
    return (
      <p className="text-xs text-muted-foreground">
        This bundle declares no mirror kinds.
      </p>
    )
  }
  return (
    <div className="flex flex-col gap-2">
      {status.data && (
        <p className="text-xs text-muted-foreground">
          {status.data.liveRecords.toLocaleString()} live records across the
          package, per the bundle's status.
        </p>
      )}
      <ul className="grid gap-1 text-xs sm:grid-cols-2">
        {mirrors.map((k, i) => {
          const c = counts[i]
          return (
            <li
              key={k.identity}
              className="flex items-center justify-between gap-3 rounded border px-3 py-1.5"
            >
              <Link
                to="/data/$authority/$pkg/$name"
                params={{
                  authority: k.authority,
                  pkg: k.package,
                  name: k.name,
                }}
                className="truncate data hover:underline"
                title={k.identity}
              >
                {k.name}
              </Link>
              <span className="data text-muted-foreground tabular-nums">
                {c?.isPending ? (
                  <Skeleton className="h-3 w-8" />
                ) : c?.data ? (
                  formatCount(c.data)
                ) : (
                  "—"
                )}
              </span>
            </li>
          )
        })}
      </ul>
    </div>
  )
}

export function ConnectionDetailPage() {
  const { authority, pkg, name, id } = connectionDetailRoute.useParams()
  const record = useQuery(recordQueryOptions(authority, pkg, name, id))
  const registry = useQuery(kindsQueryOptions)
  const triggers = useQuery(triggerRecordsQueryOptions)
  const statuses = useQuery(triggerStatusesQueryOptions)
  const kind = `${authority}/${pkg}/${name}`
  useLiveRecords(
    [kind, "substrate.reamde.dev/core/triggerrun"],
    [["record", authority, pkg, name, id], ["triggers"], ["sync"]]
  )

  const view = useMemo(
    () =>
      record.data ? accountViewOf(record.data, registry.data ?? []) : undefined,
    [record.data, registry.data]
  )
  const sources = useMemo(
    () => triggersOnKind(triggers.data ?? [], kind),
    [triggers.data, kind]
  )
  const onKind = useMemo(
    () => statusesOnKind(statuses.data ?? [], sources),
    [statuses.data, sources]
  )
  const byID = useMemo(() => new Map(onKind.map((s) => [s.id, s])), [onKind])
  const connect = useOAuthConnect(id, view?.label ?? id)

  if (record.isPending) {
    return (
      <div className="flex flex-col gap-4 px-6 pt-5">
        <Skeleton className="h-6 w-56" />
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-40 w-full" />
      </div>
    )
  }
  if (record.isError || !view) {
    return (
      <div className="px-6 pt-5 text-sm">
        <p className="text-destructive">
          {record.error?.message ?? "This account was not found."}
        </p>
        <Button
          variant="outline"
          size="sm"
          className="mt-3"
          render={<Link to="/connections" />}
        >
          Back to Connections
        </Button>
      </div>
    )
  }

  const connected = view.tokenStatus === "connected"
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-auto px-6 pt-5 pb-8">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <HealthDot health={view.health} />
            <h1 className="text-2xl font-semibold tracking-tight break-words">
              {view.label}
            </h1>
          </div>
          <dl className="mt-1 grid grid-cols-[6rem_minmax(0,1fr)] gap-x-3 gap-y-0.5 text-xs">
            <dt className="text-muted-foreground">Provider</dt>
            <dd>
              <Link
                to="/registry/$id"
                params={{ id: view.provider }}
                className="data break-all hover:underline"
              >
                {view.provider}
              </Link>
            </dd>
            <dt className="text-muted-foreground">Kind</dt>
            <dd className="data break-all">{view.record.kind}</dd>
            <dt className="text-muted-foreground">Record</dt>
            <dd>
              <Link
                to="/data/$authority/$pkg/$name/$id"
                params={{ authority, pkg, name, id }}
                className="inline-flex items-center gap-0.5 data break-all hover:underline"
              >
                {view.record.id}
                <ArrowUpRightIcon className="size-3" />
              </Link>
            </dd>
            <dt className="text-muted-foreground">Token</dt>
            <dd
              className={
                view.tokenStatus === "erroring"
                  ? "data text-destructive"
                  : "data"
              }
            >
              {view.tokenStatus ?? "not connected"}
              {view.grantedScopes.length > 0 && (
                <span className="text-muted-foreground">
                  {" "}
                  · {view.grantedScopes.length}{" "}
                  {view.grantedScopes.length === 1 ? "scope" : "scopes"} granted
                </span>
              )}
            </dd>
          </dl>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={connect.isPending}
            onClick={() => connect.mutate()}
          >
            {connect.isPending && <Spinner className="size-3.5" />}
            {connected ? "Reconnect" : "Connect"}
          </Button>
          {view.syncable && (
            <SyncActions
              record={view.record}
              paused={view.sync.paused}
              requestTriggerIds={requestTriggers(sources).map((s) => s.id)}
            />
          )}
        </div>
      </div>

      <Section title="Synchronisation">
        {view.syncable ? (
          <SyncSummary
            fields={view.sync}
            legacyStatus={view.legacySyncStatus}
          />
        ) : (
          <p className="text-xs text-muted-foreground">
            This kind does not bind the core <span className="data">sync</span>{" "}
            trait, so there is no sync state to show
            {view.legacySyncStatus
              ? ` beyond the bundle's own status: ${view.legacySyncStatus}`
              : ""}
            .
          </p>
        )}
      </Section>

      <Section title="Properties">
        <PropertyGrid record={view.record} />
      </Section>

      <Section title="Triggers on this kind">
        {sources.length === 0 ? (
          <p className="text-xs text-muted-foreground">
            No record trigger names this kind.
          </p>
        ) : (
          <table className="w-full text-xs">
            <thead>
              <tr className="text-left text-muted-foreground">
                <th className="py-1 pr-3 font-medium">Trigger</th>
                <th className="py-1 pr-3 font-medium">Cursor / head</th>
                <th className="py-1 pr-3 font-medium">Last fire</th>
                <th className="py-1 pr-3 text-right font-medium">Parked</th>
                <th className="py-1 pr-3 text-right font-medium">Pending</th>
                <th className="py-1 font-medium">
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
                  record={view.record}
                />
              ))}
            </tbody>
          </table>
        )}
      </Section>

      <Section title="Recent runs">
        <RunsList
          triggerIds={sources.map((s) => s.id)}
          recordId={view.record.id}
        />
      </Section>

      <Section title="Parked deliveries">
        <ParkedList triggerIds={sources.map((s) => s.id)} />
      </Section>

      <Section title="Mirrored records">
        <MirrorCounts providerId={view.provider} />
      </Section>
    </div>
  )
}
