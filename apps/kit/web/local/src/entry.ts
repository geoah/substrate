import { uuidv7 } from "@substrate/client"

/** One write waiting for the server (the kit's OutboxEntry). */
export interface OutboxEntry {
  id: string
  /** `DELETE` is the plain, restorable delete: no purge. */
  method: "PUT" | "PATCH" | "DELETE"
  kind: string
  recordID: string
  properties?: Record<string, unknown>
  /** Sent with `ifVersion`: the version the device last saw the record at,
   * read from the server when it has none. On a conflict the record is read
   * again and the write sent once more on top. */
  guarded: boolean
  /** An edit the owner made on what they saw at this version, or a create
   * at 0: sent as `ifVersion`, moved along the app's own writes that landed
   * since. A conflict is never merged over: the entry is parked. */
  baseVersion?: number
  /** The writes that stand or fall together: a refusal holds the rest of the
   * batch back. Every write of one record is in its batch. */
  batch?: string
  attempts: number
  /** The server's words for the last refusal, kept while the entry waits. */
  lastError?: string
  /** Refused by the server for itself: not sent again until the owner
   * retries it, and the rest of its batch waits behind it. */
  parked: boolean
  /** Parked because the record changed on the server since `baseVersion`. */
  conflict: boolean
  /** Epoch ms. */
  queuedAt: number
}

/** The outbox as it is kept: its writes in order, and the app's own
 * guarded writes that landed, per record, as the version each asserted and
 * the version it made, so an edit made on a version the app itself then
 * moved is sent at the end of that chain. */
export interface OutboxFile {
  format: 1
  entries: OutboxEntry[]
  chains: Record<string, Record<string, number>>
}

export function emptyOutboxFile(): OutboxFile {
  return { format: 1, entries: [], chains: {} }
}

/** `kind/id`: how the outbox and the fold key a record. */
export function entryKey(e: { kind: string; recordID: string }): string {
  return `${e.kind}/${e.recordID}`
}

/** The version an edit made on `base` asserts: `base`, moved along the
 * app's own writes since. */
export function asserted(file: OutboxFile, key: string, base: number): number {
  let version = base
  const seen = new Set<number>()
  for (;;) {
    const next = file.chains[key]?.[String(version)]
    if (next === undefined || seen.has(version)) return version
    seen.add(version)
    version = next
  }
}

/** What one owner's action writes: its entries, and for a delete the batch
 * whose waiting writes it replaces in the same save. */
export interface LocalWrite {
  entries: OutboxEntry[]
  replacing?: string
}

function entry(
  method: OutboxEntry["method"],
  kind: string,
  recordID: string,
  extra: Partial<OutboxEntry> = {}
): OutboxEntry {
  return {
    id: uuidv7(),
    method,
    kind,
    recordID,
    guarded: false,
    batch: `${kind}/${recordID}`,
    attempts: 0,
    parked: false,
    conflict: false,
    queuedAt: Date.now(),
    ...extra,
  }
}

/** The outbox writes an owner's change makes, each in the batch of the
 * record it writes, so a refusal holds back that record's later writes and
 * never another's (the kit's RecordWrites). */
export const RecordWrites = {
  /** A create: a PUT with `ifVersion: 0` at an id the device minted. A
   * retry whose earlier try landed meets the record there and is sent again
   * as a PUT that merges, so it lands the same row. */
  create(
    kind: string,
    id: string,
    properties: Record<string, unknown>
  ): LocalWrite {
    return { entries: [entry("PUT", kind, id, { properties, baseVersion: 0 })] }
  },

  /** A change made in passing (a toggle, a move): a patch of what changed,
   * guarded by the version the device last saw, read again on a conflict
   * and sent once more on top. Nothing, where nothing changed. */
  change(
    kind: string,
    id: string,
    properties: Record<string, unknown>
  ): LocalWrite {
    if (!Object.keys(properties).length) return { entries: [] }
    return {
      entries: [entry("PATCH", kind, id, { properties, guarded: true })],
    }
  },

  /** An edit the owner filled in: a patch on the version they saw, never
   * merged over, so a change made elsewhere meanwhile parks it as a
   * conflict. Nothing, where nothing changed. */
  edit(
    kind: string,
    id: string,
    properties: Record<string, unknown>,
    seen: number
  ): LocalWrite {
    if (!Object.keys(properties).length) return { entries: [] }
    return {
      entries: [entry("PATCH", kind, id, { properties, baseVersion: seen })],
    }
  },

  /** A plain DELETE, which replaces the record's writes still waiting. One
   * already gone is done. */
  delete(kind: string, id: string): LocalWrite {
    return {
      entries: [entry("DELETE", kind, id)],
      replacing: `${kind}/${id}`,
    }
  },
}
