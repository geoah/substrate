/** The generic renderer for any record binding the core `sync` trait: a
 * state chip, the message, the last run as relative time, a progress bar
 * from `syncProgress`, one row per stream from `syncStreams`, the last
 * error, and the two owner actions the trait gives — Sync now (stamp
 * `syncRequestedAt`, wake the on-request triggers) and Pause/Resume
 * (`syncPaused`). It knows nothing about any provider: the Connections page,
 * the account detail and the record page all render it off the same fields
 * (lib/sync.ts syncFieldsOf). */

import { useMutation, useQueryClient } from "@tanstack/react-query"
import {
  LoaderCircleIcon,
  PauseIcon,
  PlayIcon,
  RefreshCwIcon,
} from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import { requestSync, setSyncPaused, wakeTriggers } from "@/lib/api/sync"
import type { SubstrateRecord, SyncState } from "@/lib/api/types"
import { relativeTime, tableDateTime } from "@/lib/format"
import {
  cursorText,
  durationText,
  requestServed,
  syncStateOf,
  type Health,
  type SyncFields,
} from "@/lib/sync"
import { cn } from "@/lib/utils"

/** Semantic tokens only: running and ok are primary (running pulses), an
 * error is destructive, throttled wants a look, never recedes. */
const STATE_DOT: Record<SyncState, string> = {
  never: "bg-muted-foreground/40",
  running: "bg-primary animate-pulse",
  ok: "bg-primary",
  erroring: "bg-destructive",
  throttled: "bg-warning",
}

const STATE_TEXT: Partial<Record<SyncState, string>> = {
  erroring: "text-destructive",
  throttled: "text-warning",
}

/** The trait's states as a person reads them; an unknown word a body wrote
 * shows as it is. */
const STATE_WORD: Record<SyncState, string> = {
  never: "Not synced yet",
  running: "Syncing…",
  ok: "Up to date",
  erroring: "Having trouble",
  throttled: "Slowed down",
}

export function SyncStateBadge({
  fields,
  className,
}: {
  fields: Pick<SyncFields, "state" | "rawState" | "paused">
  className?: string
}) {
  const word = fields.paused
    ? "Paused"
    : (fields.rawState ?? STATE_WORD[fields.state])
  return (
    <Badge
      variant="outline"
      className={cn(
        "gap-1.5 font-normal",
        fields.paused ? "text-warning" : STATE_TEXT[fields.state],
        className
      )}
    >
      <span
        className={cn(
          "size-1.5 shrink-0 rounded-full",
          fields.paused ? "bg-warning" : STATE_DOT[fields.state]
        )}
      />
      <span>{word}</span>
    </Badge>
  )
}

const HEALTH_DOT: Record<Health, string> = {
  healthy: "bg-primary",
  attention: "bg-warning",
  broken: "bg-destructive",
  idle: "bg-muted-foreground/40",
}

const HEALTH_LABEL: Record<Health, string> = {
  healthy: "healthy: connected and syncing",
  attention: "needs attention: not connected, paused or throttled",
  broken: "broken: the grant or the sync is erroring",
  idle: "idle: connected, never synced",
}

/** The one dot a row wears, with its meaning on hover and for a reader. */
export function HealthDot({ health }: { health: Health }) {
  return (
    <span
      role="img"
      aria-label={HEALTH_LABEL[health]}
      title={HEALTH_LABEL[health]}
      className={cn("inline-block size-2.5 rounded-full", HEALTH_DOT[health])}
    />
  )
}

export function SyncProgressBar({
  progress,
}: {
  progress: NonNullable<SyncFields["progress"]>
}) {
  const total = Math.max(progress.total, progress.done, 0)
  const pct =
    total > 0 ? Math.min(100, Math.round((progress.done / total) * 100)) : 0
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-baseline justify-between gap-3 text-xs">
        <span className="text-muted-foreground">
          {progress.phase ? `Phase ${progress.phase}` : "Progress"}
        </span>
        <span className="tabular-nums">
          {progress.done.toLocaleString()} / {total.toLocaleString()}
          {progress.pending > 0 && (
            <span className="text-muted-foreground">
              {" "}
              · {progress.pending.toLocaleString()} pending
            </span>
          )}
        </span>
      </div>
      <div
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={pct}
        className="h-1.5 w-full overflow-hidden rounded-full bg-muted"
      >
        <div className="h-full bg-primary" style={{ width: `${pct}%` }} />
      </div>
    </div>
  )
}

/** One row per stream: its own state, message, last drain and backlog. A
 * cursor is a count where it is a list, and otherwise "set", never raw
 * JSON. */
export function SyncStreams({ streams }: { streams: SyncFields["streams"] }) {
  const names = Object.keys(streams).sort()
  if (!names.length) return null
  return (
    <table className="w-full text-xs">
      <thead>
        <tr className="text-left text-muted-foreground">
          <th className="py-1 pr-3 font-medium">Stream</th>
          <th className="py-1 pr-3 font-medium">State</th>
          <th className="py-1 pr-3 font-medium">Last drained</th>
          <th className="py-1 pr-3 text-right font-medium">Pending</th>
          <th className="py-1 pr-3 font-medium">Cursor</th>
          <th className="py-1 font-medium">Message</th>
        </tr>
      </thead>
      <tbody>
        {names.map((name) => {
          const s = streams[name]
          const state = syncStateOf(s.state)
          return (
            <tr key={name} className="border-t">
              <td className="py-1.5 pr-3 font-medium">{name}</td>
              <td className="py-1.5 pr-3">
                {s.state ? (
                  <SyncStateBadge
                    fields={{
                      state,
                      rawState: s.state !== state ? s.state : undefined,
                      paused: false,
                    }}
                  />
                ) : (
                  <span className="text-muted-foreground">—</span>
                )}
              </td>
              <td
                className="py-1.5 pr-3 text-muted-foreground"
                title={s.lastAt}
              >
                {s.lastAt ? relativeTime(s.lastAt) : "never"}
              </td>
              <td className="py-1.5 pr-3 text-right tabular-nums">
                {s.pending.toLocaleString()}
              </td>
              <td className="py-1.5 pr-3 text-muted-foreground">
                {cursorText(s.cursor)}
              </td>
              <td className="py-1.5 break-words text-muted-foreground">
                {s.message ?? ""}
              </td>
            </tr>
          )
        })}
      </tbody>
    </table>
  )
}

/** The whole trait, read-only: the chip and the message on one line, the
 * timings on the next, then the progress bar, the streams and the error.
 * `compact` is the table cell's voice: chip, message and last-synced only. */
export function SyncSummary({
  fields,
  legacyStatus,
  compact = false,
}: {
  fields: SyncFields
  /** The free-text `syncStatus` a bundle still writes before it binds the
   * trait; shown in full when the trait carries no message of its own. */
  legacyStatus?: string
  compact?: boolean
}) {
  const message = fields.message ?? legacyStatus
  const served = requestServed(fields)
  if (compact) {
    return (
      <div className="flex min-w-0 flex-col gap-1">
        <div className="flex min-w-0 items-center gap-2">
          <SyncStateBadge fields={fields} />
          {!served && (
            <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
              <LoaderCircleIcon className="size-3 animate-spin" />
              requested
            </span>
          )}
        </div>
        {message && (
          <span className="text-xs break-words text-muted-foreground">
            {message}
          </span>
        )}
      </div>
    )
  }
  return (
    <div className="flex flex-col gap-3 text-sm">
      <div className="flex flex-wrap items-center gap-2">
        <SyncStateBadge fields={fields} />
        {message && <span className="break-words">{message}</span>}
      </div>
      <dl className="grid grid-cols-[9rem_minmax(0,1fr)] gap-x-4 gap-y-1 text-xs">
        <dt className="text-muted-foreground">Last synced</dt>
        <dd title={fields.lastSyncedAt}>
          {fields.lastSyncedAt ? (
            <>
              {relativeTime(fields.lastSyncedAt)}
              <span className="text-muted-foreground">
                {" "}
                · {tableDateTime(fields.lastSyncedAt)}
              </span>
            </>
          ) : (
            <span className="text-muted-foreground">never</span>
          )}
        </dd>
        <dt className="text-muted-foreground">Last run started</dt>
        <dd title={fields.lastSyncStartedAt}>
          {fields.lastSyncStartedAt ? (
            <>
              {relativeTime(fields.lastSyncStartedAt)}
              {fields.lastSyncDurationMs !== undefined && (
                <span className="text-muted-foreground">
                  {" "}
                  · took {durationText(fields.lastSyncDurationMs)}
                </span>
              )}
            </>
          ) : (
            <span className="text-muted-foreground">—</span>
          )}
        </dd>
        <dt className="text-muted-foreground">Requested</dt>
        <dd title={fields.requestedAt}>
          {fields.requestedAt ? (
            <>
              {relativeTime(fields.requestedAt)}
              <span className="text-muted-foreground">
                {" "}
                · {served ? "served" : "waiting for the sync to answer"}
              </span>
            </>
          ) : (
            <span className="text-muted-foreground">no request pending</span>
          )}
        </dd>
      </dl>
      {fields.progress && <SyncProgressBar progress={fields.progress} />}
      <SyncStreams streams={fields.streams} />
      {fields.error && (
        <div className="rounded-md border border-destructive/40 bg-destructive/5 px-3 py-2 text-xs">
          <div className="flex items-baseline justify-between gap-3">
            <span className="font-medium text-destructive">Last error</span>
            {fields.errorAt && (
              <span className="text-muted-foreground" title={fields.errorAt}>
                {relativeTime(fields.errorAt)}
              </span>
            )}
          </div>
          <p className="mt-1 break-words whitespace-pre-wrap">{fields.error}</p>
        </div>
      )}
    </div>
  )
}

function useRefreshSync() {
  const queryClient = useQueryClient()
  return () => {
    void queryClient.invalidateQueries({ queryKey: ["sync"] })
    void queryClient.invalidateQueries({ queryKey: ["trait", "records"] })
    void queryClient.invalidateQueries({ queryKey: ["triggers"] })
    void queryClient.invalidateQueries({ queryKey: ["record"] })
  }
}

/** Sync now: stamp the request and wake the on-request triggers the caller
 * resolved for the kind; with none to wake the stamp alone still fires them
 * on the dispatcher's next pass, and the toast says so. */
export function SyncNowButton({
  record,
  paused,
  requestTriggerIds,
  disabled,
  className,
}: {
  record: Pick<SubstrateRecord, "kind" | "id">
  paused: boolean
  requestTriggerIds: string[]
  disabled?: boolean
  className?: string
}) {
  const refresh = useRefreshSync()
  const syncNow = useMutation({
    mutationFn: async () => {
      await requestSync(record)
      const ran = requestTriggerIds.length
        ? await wakeTriggers(requestTriggerIds)
        : undefined
      return ran
    },
    onSuccess: (ran) => {
      toast.add({
        type: "success",
        title: "Sync requested",
        description:
          ran === undefined
            ? "The request is on the record; the dispatcher picks it up on its next pass."
            : ran > 0
              ? `${ran} ${ran === 1 ? "delivery" : "deliveries"} ran.`
              : "The triggers woke; nothing was due yet.",
      })
      refresh()
    },
    onError: (error) =>
      toast.add({
        type: "error",
        title: "Sync now failed",
        description: error.message,
      }),
  })
  return (
    <Button
      variant="outline"
      size="sm"
      className={cn("h-7 gap-1 px-2 text-xs", className)}
      disabled={disabled || paused || syncNow.isPending}
      onClick={() => syncNow.mutate()}
      title={paused ? "Resume before asking for a run" : "Ask for a run now"}
    >
      {syncNow.isPending ? (
        <Spinner className="size-3" />
      ) : (
        <RefreshCwIcon className="size-3" />
      )}
      Sync now
    </Button>
  )
}

/** Pause or Resume: the trait's `syncPaused`, the owner's other hand. */
export function PauseButton({
  record,
  paused,
  disabled,
  className,
}: {
  record: Pick<SubstrateRecord, "kind" | "id">
  paused: boolean
  disabled?: boolean
  className?: string
}) {
  const refresh = useRefreshSync()
  const pause = useMutation({
    mutationFn: () => setSyncPaused(record, !paused),
    onSuccess: () => {
      toast.add({
        type: "success",
        title: paused ? "Sync resumed" : "Sync paused",
      })
      refresh()
    },
    onError: (error) =>
      toast.add({
        type: "error",
        title: paused ? "Resume failed" : "Pause failed",
        description: error.message,
      }),
  })
  return (
    <Button
      variant="ghost"
      size="sm"
      className={cn("h-7 gap-1 px-2 text-xs", className)}
      disabled={disabled || pause.isPending}
      onClick={() => pause.mutate()}
    >
      {paused ? (
        <PlayIcon className="size-3" />
      ) : (
        <PauseIcon className="size-3" />
      )}
      {paused ? "Resume" : "Pause"}
    </Button>
  )
}

/** The trait's two owner hands side by side: the detail page's and the
 * record page's toolbar. */
export function SyncActions({
  record,
  paused,
  requestTriggerIds,
  disabled,
}: {
  record: Pick<SubstrateRecord, "kind" | "id">
  paused: boolean
  requestTriggerIds: string[]
  disabled?: boolean
}) {
  return (
    <div className="flex items-center gap-1.5">
      <SyncNowButton
        record={record}
        paused={paused}
        requestTriggerIds={requestTriggerIds}
        disabled={disabled}
        className="h-8 text-sm"
      />
      <PauseButton
        record={record}
        paused={paused}
        disabled={disabled}
        className="h-8 text-sm"
      />
    </div>
  )
}
