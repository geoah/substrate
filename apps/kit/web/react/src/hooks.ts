import type { AppStatus } from "@substrate/auth"
import type { Kind, SubstrateRecord } from "@substrate/client"
import type {
  LinkedStatusSnapshot,
  Outbox,
  OutboxEntry,
  OutboxSummary,
} from "@substrate/local"
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useSyncExternalStore,
} from "react"
import { useConnection, useSubstrate } from "./context"
import {
  defaultStaleAfter,
  type Read,
  type ReadOptions,
  type ReadState,
} from "./reads"
import {
  deriveLinkStatus,
  statusSentence,
  statusTone,
  type LinkStatus,
} from "./status"

// A record object is never changed in place, so a kind's read of it stands
// as long as the object does: a record that did not change reads as the
// same value, and a memoized row keyed on it does not draw again.
const reads = new WeakMap<object, WeakMap<object, unknown>>()

/** A record read through its kind's codec, once per record object. */
export function readThrough<T>(
  kind: { read: (record: SubstrateRecord) => T },
  record: SubstrateRecord
): T {
  let byKind = reads.get(record)
  if (!byKind) {
    byKind = new WeakMap()
    reads.set(record, byKind)
  }
  if (byKind.has(kind)) return byKind.get(kind) as T
  const value = kind.read(record)
  byKind.set(kind, value)
  return value
}

/** Every record of a kind, read as the app's own value. The array is the
 * same until a record of that kind changes, and each value the same until
 * its record does, so a change to another kind, the connection or the
 * outbox draws nothing here. */
export function useRecords<T, D>(kind: Kind<T, D>): readonly T[] {
  const { linked } = useSubstrate()
  const records = useSyncExternalStore(linked.subscribe, () =>
    linked.kindSnapshot(kind.kind)
  )
  return useMemo(
    () => Array.from(records.values(), (r) => readThrough(kind, r)),
    [records, kind]
  )
}

/** One record as a screen draws it: still being read, not in the
 * repository, its read failed (why, and `retry` to read it now; it is also
 * read again by itself a few times), or its value. */
export type RecordRead<T> =
  | { state: "loading"; value?: undefined }
  | { state: "absent"; value?: undefined }
  | { state: "failed"; value?: undefined; words: string; retry: () => void }
  | { state: "present"; value: T }

const loading: RecordRead<never> = { state: "loading" }
const absentRead: RecordRead<never> = { state: "absent" }

/** One record of a kind, read as the app's own value. `loading` until it is
 * known absent: its kind read whole, or, where the kind's read stopped at
 * its limit, the record read by itself. A link to a record opened before
 * the read finishes waits rather than saying it is gone, and a read that
 * failed says so (`failed`) rather than waiting for ever. */
export function useRecord<T, D>(
  kind: Kind<T, D>,
  id: string | undefined
): RecordRead<T> {
  const { linked } = useSubstrate()
  const record = useSyncExternalStore(linked.subscribe, () =>
    id ? linked.recordSnapshot(kind.kind, id) : undefined
  )
  const absent = useSyncExternalStore(
    linked.subscribe,
    () => !id || linked.isAbsent(kind.kind, id)
  )
  const failed = useSyncExternalStore(linked.subscribe, () =>
    id ? linked.readError(kind.kind, id) : undefined
  )
  const unresolved = !!id && !record && !absent
  useEffect(() => {
    if (unresolved) linked.resolve(kind.kind, id)
  }, [linked, kind.kind, id, unresolved])
  return useMemo((): RecordRead<T> => {
    if (record) return { state: "present", value: readThrough(kind, record) }
    if (absent) return absentRead
    if (failed && id)
      return {
        state: "failed",
        words: failed,
        retry: () => linked.retryRead(kind.kind, id),
      }
    return loading
  }, [record, absent, failed, kind, id, linked])
}

/** Whether a kind's records were read whole from the server, or kept here
 * from such a read: what tells "none yet" from "none". A read that stopped
 * at its limit (`useLinkedStatus().limitReached`) is not whole. */
export function useListed(kind: string | Kind<unknown, never>): boolean {
  const { linked } = useSubstrate()
  const ref = typeof kind === "string" ? kind : kind.kind
  return useSyncExternalStore(linked.subscribe, () => linked.hasListed(ref))
}

/** Whether a record's last write is still waiting to be sent (`pending`)
 * or was refused by the server and waits for the owner (`parked`). */
export function useWriteState(
  kind: string | Kind<unknown, never>,
  id: string
): "pending" | "parked" | undefined {
  const { linked } = useSubstrate()
  const ref = typeof kind === "string" ? kind : kind.kind
  return useSyncExternalStore(linked.subscribe, () =>
    linked.writeState(ref, id)
  )
}

/** How the reading of the app's records stands: loading, live,
 * reconnecting and why, from what this browser kept, and how many records
 * wait or were refused. A new object only when one of these changes. */
export function useLinkedStatus(): LinkedStatusSnapshot {
  const { linked } = useSubstrate()
  return useSyncExternalStore(linked.subscribe, linked.statusSnapshot)
}

/** Where the outbox stands: the writes waiting, the ones refused, and how
 * its last send ended. Every change to the outbox draws it again. */
export function useOutbox(): OutboxSummary {
  const { outbox } = useSubstrate()
  return useSyncExternalStore(outbox.subscribe, outbox.getSnapshot)
}

const noEntries: readonly OutboxEntry[] = []
const parkedSelectors = new WeakMap<Outbox, () => readonly OutboxEntry[]>()

/** An outbox's parked writes, as the same array while the same writes are
 * parked with the same words. */
function parkedOf(outbox: Outbox): () => readonly OutboxEntry[] {
  let select = parkedSelectors.get(outbox)
  if (select) return select
  let last = noEntries
  select = () => {
    const parked = outbox.getSnapshot().parked
    const same =
      parked.length === last.length &&
      parked.every(
        (e, i) =>
          e.id === last[i].id &&
          e.lastError === last[i].lastError &&
          e.conflict === last[i].conflict
      )
    if (!same) last = parked.length ? parked : noEntries
    return last
  }
  parkedSelectors.set(outbox, select)
  return select
}

/** The writes the server refused, waiting for the owner to retry or
 * discard them: the same array while the same writes are parked. */
export function useParkedWrites(): readonly OutboxEntry[] {
  const { outbox } = useSubstrate()
  return useSyncExternalStore(outbox.subscribe, parkedOf(outbox))
}

/** The last check of the app's standing with its server, and how to run it
 * again. */
export function useAppStatus(): {
  status: AppStatus | null
  checking: boolean
  refresh: () => Promise<void>
} {
  const { appStatus, checking, checkStatus } = useConnection()
  return useMemo(
    () => ({
      status: appStatus,
      checking,
      refresh: () => checkStatus(true),
    }),
    [appStatus, checking, checkStatus]
  )
}

/** Where the app stands with its repository, in Settings' one sentence:
 * the status, the sentence and its tone. */
export function useStatusSentence(): {
  status: LinkStatus
  sentence: string
  tone: ReturnType<typeof statusTone>
} {
  const { manifest, kept } = useSubstrate()
  const { appStatus } = useConnection()
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

const noKinds: readonly string[] = []
const noRead = () => () => {}
const disabledRead: ReadState<never> = {
  data: undefined,
  error: undefined,
  loading: false,
  refreshing: false,
  refresh: () => {},
}

/** A read through the session's client that is not a linked record (an
 * aggregate, a list of a kind the app does not keep), kept by `key` for
 * the session: every component reading one key shares one read, and a
 * null key reads nothing. It is read again when one of `kinds` changes (a
 * write the outbox lands, `useInvalidate`, or the change feed), and a read
 * the feed does not keep current when the page comes back to the front
 * after `staleAfter`. The last answer stays while a newer read runs
 * (`refreshing`); `loading` is a read with no answer yet. `read` is called
 * with the session's client and a signal aborted when the session ends,
 * the last reader of the key goes, or a newer read of it starts. */
export function useRead<T>(
  key: string | null,
  read: Read<T>,
  options: ReadOptions = {}
): ReadState<T> {
  const { reads } = useSubstrate()
  const kindKey = (options.kinds ?? noKinds).join(" ")
  const staleAfter = options.staleAfter ?? defaultStaleAfter
  // The read as last drawn: a key's next read calls the newest.
  const latest = useRef(read)
  useEffect(() => {
    latest.current = read
  })
  const subscribe = useCallback(
    (changed: () => void) =>
      key === null
        ? noRead()
        : reads.attach(key, {
            read: (client, signal) => latest.current(client, signal),
            kinds: kindKey ? kindKey.split(" ") : noKinds,
            staleAfter,
            changed,
          }),
    [reads, key, kindKey, staleAfter]
  )
  const snapshot = useCallback(
    () => (key === null ? disabledRead : reads.snapshot(key)),
    [reads, key]
  )
  return useSyncExternalStore(subscribe, snapshot) as ReadState<T>
}

/** Reads again every `useRead` naming one of `kinds`: for a write the app
 * made through the client itself, which the outbox never saw. */
export function useInvalidate(): (kinds: readonly string[]) => void {
  const { reads } = useSubstrate()
  return useCallback((kinds) => reads.invalidate(kinds), [reads])
}
