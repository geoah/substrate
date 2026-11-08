import {
  recordKey,
  type SubstrateRecord,
  type WatchCursor,
  type WatchRow,
} from "@substrate/client"
import type { RepositoryStorage, StoredRecords } from "./storage"

/** The records of the kinds an app shows, as last read, and where the watch
 * over them resumes, kept so the app opens offline with its last state (the
 * kit's RecordCache). A list read fills it; each watch row marks its record
 * stale (or removes it) and moves the cursor; the app reads the stale ones
 * again and stores what it read. All of it is the server's and can be read
 * again, so a save that fails costs a read, never a write.
 *
 * Saves are gathered for `delay` ms and written in one transaction, the
 * cursor beside the records and the stale marks it moved past, so a reload
 * never resumes past a change whose record it does not hold. */
export class RecordCache {
  private readonly storage: RepositoryStorage
  private readonly delay: number
  private cursor?: WatchCursor
  private stale = new Set<string>()
  private replace = new Set<string>()
  private dirty = new Map<string, SubstrateRecord | null>()
  private metaDirty = false
  private timer?: ReturnType<typeof setTimeout>
  private saving: Promise<void> = Promise.resolve()
  /** Why the last save failed, until one succeeds. */
  saveProblem?: string

  constructor(storage: RepositoryStorage, delay = 150) {
    this.storage = storage
    this.delay = delay
  }

  /** What the device kept of `kinds`. A record of a kind the app no longer
   * shows is left out. */
  async load(kinds: readonly string[]): Promise<StoredRecords> {
    const stored = await this.storage.readRecords()
    const shown = (key: string) => kinds.some((k) => key.startsWith(`${k}/`))
    this.cursor = stored.cursor
    this.stale = new Set(stored.stale.filter(shown))
    return {
      records: stored.records.filter((r) => kinds.includes(r.kind)),
      cursor: stored.cursor,
      stale: [...this.stale],
    }
  }

  get resumeFrom(): WatchCursor | undefined {
    return this.cursor
  }

  get staleKeys(): string[] {
    return [...this.stale]
  }

  /** A list read of `kinds`, whole: their records replaced by these. The
   * watch resumes from `bookmark`, the one it opened at before the list
   * was read. */
  listed(
    records: SubstrateRecord[],
    kinds: readonly string[],
    bookmark: WatchCursor | undefined
  ): void {
    for (const kind of kinds) {
      this.replace.add(kind)
      for (const key of [...this.dirty.keys()])
        if (key.startsWith(`${kind}/`)) this.dirty.delete(key)
      for (const key of [...this.stale])
        if (key.startsWith(`${kind}/`)) this.stale.delete(key)
    }
    for (const r of records)
      if (!r.deletedAt) this.dirty.set(recordKey(r.kind, r.id), r)
    if (bookmark) this.cursor = bookmark
    this.metaDirty = true
    this.schedule()
  }

  /** The watch's opening bookmark: where it follows from when nothing was
   * listed under one, or the history changed. */
  bookmark(cursor: WatchCursor): void {
    if (this.cursor?.generation === cursor.generation) return
    this.cursor = cursor
    this.metaDirty = true
    this.schedule()
  }

  /** One watch row: a removal drops the record, anything else marks it to be
   * read again. */
  row(row: WatchRow): void {
    const key = recordKey(row.kind, row.recordId)
    if (row.removes) {
      this.dirty.set(key, null)
      this.stale.delete(key)
    } else {
      this.stale.add(key)
    }
    if (this.cursor)
      this.cursor = {
        seq: Math.max(this.cursor.seq, row.seq),
        generation: this.cursor.generation,
      }
    this.metaDirty = true
    this.schedule()
  }

  /** A record read again: stored, or removed when the read found none. */
  refreshed(key: string, record: SubstrateRecord | null): void {
    this.dirty.set(key, record && !record.deletedAt ? record : null)
    if (this.stale.delete(key)) this.metaDirty = true
    this.schedule()
  }

  /** The cursor addresses no history the server holds: everything is read
   * again and the watch opens at the new head. */
  relist(): void {
    this.cursor = undefined
    this.stale.clear()
    this.metaDirty = true
    this.schedule()
  }

  async clear(): Promise<void> {
    clearTimeout(this.timer)
    this.cursor = undefined
    this.stale.clear()
    this.replace.clear()
    this.dirty.clear()
    this.metaDirty = false
    await this.saving
    await this.storage.clearRecords()
  }

  private schedule(): void {
    clearTimeout(this.timer)
    this.timer = setTimeout(() => void this.save(), this.delay)
  }

  /** Writes what changed since the last save. */
  save(): Promise<void> {
    clearTimeout(this.timer)
    const replaceKinds = [...this.replace]
    const put: SubstrateRecord[] = []
    const remove: string[] = []
    for (const [key, r] of this.dirty) {
      if (r) put.push(r)
      else remove.push(key)
    }
    const meta = this.metaDirty
    this.replace.clear()
    this.dirty.clear()
    this.metaDirty = false
    if (!replaceKinds.length && !put.length && !remove.length && !meta)
      return this.saving
    const cursor = this.cursor ?? null
    const stale = [...this.stale]
    this.saving = this.saving.then(async () => {
      try {
        await this.storage.writeRecords({
          replaceKinds,
          put,
          remove,
          cursor: meta ? cursor : undefined,
          stale: meta ? stale : undefined,
        })
        this.saveProblem = undefined
      } catch (e) {
        this.saveProblem = e instanceof Error ? e.message : String(e)
      }
    })
    return this.saving
  }
}
