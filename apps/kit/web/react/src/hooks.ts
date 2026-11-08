import type { AppStatus } from "@substrate/auth"
import { recordKey, type Kind } from "@substrate/client"
import type {
  LinkedSnapshot,
  OutboxEntry,
  OutboxSummary,
} from "@substrate/local"
import { useMemo, useSyncExternalStore } from "react"
import { useSubstrate } from "./context"
import {
  deriveLinkStatus,
  statusSentence,
  statusTone,
  type LinkStatus,
} from "./status"

/** The records of the app's kinds as the owner sees them, current with the
 * change feed and the outbox. */
export function useLive(): LinkedSnapshot {
  const { linked } = useSubstrate()
  return useSyncExternalStore(linked.subscribe, linked.getSnapshot)
}

// A record object is never changed in place, so what a kind read of it
// stands for as long as the object does.
const reads = new WeakMap<object, Map<Kind<unknown, never>, unknown>>()

function readThrough<T>(kind: Kind<T, never>, record: object): T {
  let byKind = reads.get(record)
  if (!byKind) {
    byKind = new Map()
    reads.set(record, byKind)
  }
  const k = kind as Kind<unknown, never>
  if (byKind.has(k)) return byKind.get(k) as T
  const value = kind.read(record as Parameters<Kind<T, never>["read"]>[0])
  byKind.set(k, value)
  return value
}

/** Every record of a kind, read as the app's own value. The array changes
 * only when the records do. */
export function useRecords<T, D>(kind: Kind<T, D>): T[] {
  const live = useLive()
  const k = kind as unknown as Kind<T, never>
  return useMemo(() => {
    const out: T[] = []
    for (const r of live.records.values())
      if (r.kind === kind.kind) out.push(readThrough(k, r))
    return out
  }, [live.records, kind.kind, k])
}

/** One record of a kind, read as the app's own value, or undefined while
 * the repository holds none. */
export function useRecord<T, D>(
  kind: Kind<T, D>,
  id: string | undefined
): T | undefined {
  const live = useLive()
  const k = kind as unknown as Kind<T, never>
  const record = id ? live.records.get(recordKey(kind.kind, id)) : undefined
  return useMemo(
    () => (record ? readThrough(k, record) : undefined),
    [record, k]
  )
}

/** Whether a record has a write waiting to be sent: show it as not landed
 * yet. A write the server refused is not waiting; `useRefused` says it. */
export function usePending(
  kind: string | Kind<unknown, never>,
  id: string
): boolean {
  const live = useLive()
  return live.pending.has(
    recordKey(typeof kind === "string" ? kind : kind.kind, id)
  )
}

/** Whether a record has a write the server refused, waiting for the owner. */
export function useRefused(
  kind: string | Kind<unknown, never>,
  id: string
): boolean {
  const live = useLive()
  return live.parked.has(
    recordKey(typeof kind === "string" ? kind : kind.kind, id)
  )
}

/** Where the outbox stands: the writes waiting, the ones refused, and how
 * its last send ended. */
export function useOutbox(): OutboxSummary {
  const { outbox } = useSubstrate()
  return useSyncExternalStore(outbox.subscribe, outbox.getSnapshot)
}

/** The writes the server refused, waiting for the owner to retry or
 * discard them. */
export function useParkedWrites(): readonly OutboxEntry[] {
  return useOutbox().parked
}

/** The last check of the app's standing with its server, and how to run it
 * again. */
export function useAppStatus(): {
  status: AppStatus | null
  checking: boolean
  refresh: () => Promise<void>
} {
  const { appStatus, checking, checkStatus } = useSubstrate()
  return useMemo(
    () => ({
      status: appStatus,
      checking,
      refresh: () => checkStatus(true),
    }),
    [appStatus, checking, checkStatus]
  )
}

/** Where the app stands with its repository, in one sentence, as Settings'
 * Status says it. */
export function useLinkStatus(): {
  status: LinkStatus
  sentence: string
  tone: ReturnType<typeof statusTone>
} {
  const { manifest, appStatus, kept } = useSubstrate()
  const outbox = useOutbox()
  return useMemo(() => {
    const status = deriveLinkStatus({
      packages: appStatus?.packages ?? null,
      outbox: outbox.state,
      waiting: outbox.waiting,
      issues: appStatus?.issues ?? [],
      notKept: outbox.saveProblem,
    })
    return {
      status,
      sentence: statusSentence(status, {
        app: manifest.name,
        device: "this browser",
        kept,
      }),
      tone: statusTone(status),
    }
  }, [appStatus, outbox, manifest.name, kept])
}
