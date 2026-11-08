import {
  SubstrateClient,
  recordKey,
  type SubstrateRecord,
} from "@substrate/client"
import {
  LinkedRecords,
  type LinkedRecordsOptions,
  type LinkedSnapshot,
  type LinkedStatus,
} from "@substrate/local"

/** @deprecated Use `LinkedSnapshot` from `@substrate/local`. */
export type LiveSnapshot = LinkedSnapshot
/** @deprecated Use `LinkedStatus` from `@substrate/local`. */
export type LiveStatus = LinkedStatus

/** @deprecated The linked records with the first web kit's write hooks:
 * `apply` and `landed` lay a write over the records outside the outbox.
 * Write through `useWrites`, which keeps every write in the outbox. */
export class LiveCollection extends LinkedRecords {
  constructor(client: SubstrateClient, kinds: string[])
  constructor(opts: LinkedRecordsOptions)
  constructor(a: SubstrateClient | LinkedRecordsOptions, kinds?: string[]) {
    super(a instanceof SubstrateClient ? { client: a, kinds: kinds ?? [] } : a)
  }

  /** Shows a write before the server has it: the record as it will be, or
   * null for one deleted. Answers how to undo it. */
  apply(kind: string, id: string, next: SubstrateRecord | null): () => void {
    const key = recordKey(kind, id)
    const had = this.overlays.has(key)
    const before = this.overlays.get(key)
    this.overlays.set(key, next)
    this.emit()
    return () => {
      if (had) this.overlays.set(key, before ?? null)
      else this.overlays.delete(key)
      this.emit()
    }
  }

  /** What the server answered for a write, unless a later version is here. */
  landed(record: SubstrateRecord): void {
    const key = recordKey(record.kind, record.id)
    const here = this.server.get(key)
    this.overlays.delete(key)
    if (here && here.version > record.version) return this.emit()
    if (record.deletedAt) this.server.delete(key)
    else this.server.set(key, record)
    this.emit()
  }
}
