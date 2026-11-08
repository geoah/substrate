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
 * cursor beside the records, the stale marks it moved past and the kinds
 * read whole under it, so a reload never resumes past a change whose record
 * it does not hold. A save that fails is kept and written with the next. */
export class RecordCache {
  private readonly storage: RepositoryStorage
  private readonly delay: number
  private cursor?: WatchCursor
  /** The kinds whose records are held whole: listed, then followed. */
  private covered: string[] = []
  private stale = new Set<string>()
  private replace = new Set<string>()
  private dirty = new Map<string, SubstrateRecord | null>()
  private metaDirty = false
  private timer?: ReturnType<typeof setTimeout>
  private saving: Promise<void> = Promise.resolve()
  /** Moved by `clear`: a save that failed before it is not put back. */
  private epoch = 0
  private closed = false
  private listeners = new Set<() => void>()
  /** Why the last save failed, until one succeeds. */
  saveProblem?: string

  constructor(storage: RepositoryStorage, delay = 150) {
    this.storage = storage
    this.delay = delay
  }

  /** Calls `fn` after each save lands, or fails: what a reader of the
   * storage's size reads again on. */
  subscribe(fn: () => void): () => void {
    this.listeners.add(fn)
    return () => this.listeners.delete(fn)
  }

  /** What the device kept of `kinds`: the records of those it held whole,
   * and the cursor only where it held all of them, since a watch from it
   * never carries what changed in another kind before it. A kind the app
   * shows that the device never listed is read again with every other. */
  async load(kinds: readonly string[]): Promise<StoredRecords> {
    const stored = await this.storage.readRecords()
    const whole = kinds.filter((k) => stored.kinds?.includes(k))
    const resumes = !!stored.cursor && whole.length === kinds.length
    const shown = (key: string) => kinds.some((k) => key.startsWith(`${k}/`))
    this.cursor = resumes ? stored.cursor : undefined
    this.covered = whole
    this.stale = new Set(resumes ? stored.stale.filter(shown) : [])
    return {
      records: stored.records.filter((r) => whole.includes(r.kind)),
      cursor: this.cursor,
      kinds: whole,
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
    if (this.closed) return
    for (const kind of kinds) {
      this.replace.add(kind)
      for (const key of [...this.dirty.keys()])
        if (key.startsWith(`${kind}/`)) this.dirty.delete(key)
      for (const key of [...this.stale])
        if (key.startsWith(`${kind}/`)) this.stale.delete(key)
    }
    for (const r of records)
      if (!r.deletedAt) this.dirty.set(recordKey(r.kind, r.id), r)
    if (bookmark) {
      this.cursor = bookmark
      this.covered = [...kinds]
    } else {
      for (const kind of kinds)
        if (!this.covered.includes(kind)) this.covered.push(kind)
    }
    this.metaDirty = true
    this.schedule()
  }

  /** The watch's opening bookmark: where it follows from when nothing was
   * listed under one, or the history changed. */
  bookmark(cursor: WatchCursor): void {
    if (this.closed || this.cursor?.generation === cursor.generation) return
    this.cursor = cursor
    this.metaDirty = true
    this.schedule()
  }

  /** One watch row: a removal drops the record, anything else marks it to be
   * read again. */
  row(row: WatchRow): void {
    if (this.closed) return
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
    if (this.closed) return
    this.dirty.set(key, record && !record.deletedAt ? record : null)
    if (this.stale.delete(key)) this.metaDirty = true
    this.schedule()
  }

  /** The cursor addresses no history the server holds: everything is read
   * again and the watch opens at the new head. The records stay shown
   * until the list replaces them. */
  relist(): void {
    if (this.closed) return
    this.cursor = undefined
    this.stale.clear()
    this.metaDirty = true
    this.schedule()
  }

  async clear(): Promise<void> {
    clearTimeout(this.timer)
    this.epoch++
    this.cursor = undefined
    this.covered = []
    this.stale.clear()
    this.replace.clear()
    this.dirty.clear()
    this.metaDirty = false
    const cleared = this.saving.then(() => this.storage.clearRecords())
    this.saving = cleared.catch(() => {})
    await cleared
    for (const fn of this.listeners) fn()
  }

  /** Writes what waits and takes no more: what a link closing awaits. */
  async close(): Promise<void> {
    await this.save()
    this.closed = true
    clearTimeout(this.timer)
  }

  private schedule(): void {
    clearTimeout(this.timer)
    if (!this.closed)
      this.timer = setTimeout(() => void this.save(), this.delay)
  }

  /** Writes what changed since the last save landed, after any save still
   * running. */
  save(): Promise<void> {
    clearTimeout(this.timer)
    this.saving = this.saving.then(() => this.write())
    return this.saving
  }

  /** The batch is taken when its turn comes, never earlier, so a save
   * queued behind one that fails carries what that one could not write. */
  private async write(): Promise<void> {
    const replaceKinds = [...this.replace]
    const dirty = this.dirty
    const meta = this.metaDirty
    if (!replaceKinds.length && !dirty.size && !meta) return
    this.replace = new Set()
    this.dirty = new Map()
    this.metaDirty = false
    const epoch = this.epoch
    const put: SubstrateRecord[] = []
    const remove: string[] = []
    for (const [key, r] of dirty) {
      if (r) put.push(r)
      else remove.push(key)
    }
    try {
      await this.storage.writeRecords({
        replaceKinds,
        put,
        remove,
        cursor: meta ? (this.cursor ?? null) : undefined,
        kinds: meta ? [...this.covered] : undefined,
        stale: meta ? [...this.stale] : undefined,
      })
      this.saveProblem = undefined
    } catch (e) {
      this.saveProblem = e instanceof Error ? e.message : String(e)
      if (epoch === this.epoch) this.putBack(replaceKinds, dirty)
    }
    for (const fn of this.listeners) fn()
  }

  /** A batch that failed, under what changed since: a kind listed since
   * replaces its records, a record read since is newer. The cursor goes
   * with the next save, which now holds every change it moved past. */
  private putBack(
    replaceKinds: string[],
    dirty: Map<string, SubstrateRecord | null>
  ): void {
    const relisted = [...this.replace]
    for (const kind of replaceKinds) this.replace.add(kind)
    for (const [key, r] of dirty) {
      if (this.dirty.has(key)) continue
      if (relisted.some((k) => key.startsWith(`${k}/`))) continue
      this.dirty.set(key, r)
    }
    this.metaDirty = true
  }
}
