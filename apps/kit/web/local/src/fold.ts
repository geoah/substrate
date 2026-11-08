import type { SubstrateRecord } from "@substrate/client"
import { entryKey, type OutboxEntry } from "./entry"

/** One write over a record: a put or a patch merges (null clears a
 * property), a delete removes. A put over nothing is a record not sent yet,
 * at version 0; a patch over nothing is of a record already gone. */
export function applyEntry(
  entry: OutboxEntry,
  record: SubstrateRecord | undefined
): SubstrateRecord | undefined {
  if (entry.method === "DELETE") return undefined
  if (entry.method === "PATCH" && !record) return undefined
  const now = new Date(entry.queuedAt).toISOString()
  const next: SubstrateRecord = record
    ? { ...record, properties: { ...record.properties } }
    : {
        id: entry.recordID,
        kind: entry.kind,
        properties: {},
        version: 0,
        createdAt: now,
        updatedAt: now,
      }
  for (const [name, value] of Object.entries(entry.properties ?? {})) {
    if (value === null) delete next.properties[name]
    else next.properties[name] = value
  }
  return next
}

/** The records as the owner sees them: the server's as last read, with the
 * writes still waiting laid over in their order, so a change made offline
 * shows at once and stays shown when the server's copy is read again
 * before the write lands. A parked write is not laid over: the record shows
 * as the server holds it while the owner chooses Retry or Discard (the
 * kit's RecordFold). */
export function foldRecords(
  server: ReadonlyMap<string, SubstrateRecord>,
  queue: readonly OutboxEntry[],
  kinds?: readonly string[]
): Map<string, SubstrateRecord> {
  const out = new Map(server)
  for (const entry of queue) {
    if (entry.parked) continue
    if (kinds && !kinds.includes(entry.kind)) continue
    const key = entryKey(entry)
    const next = applyEntry(entry, out.get(key))
    if (next) out.set(key, next)
    else out.delete(key)
  }
  return out
}
