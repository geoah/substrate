import type { SubstrateRecord, WatchCursor } from "@substrate/client"
import { emptyOutboxFile, type OutboxFile } from "./entry"

/** What the device keeps for one repository: the outbox, and the records
 * with where the watch over them resumes. */
export interface RepositoryStorage {
  /** Whether what is kept survives the page closing. */
  readonly persistent: boolean
  /** Why it does not, in words for a screen. */
  readonly problem?: string
  readOutbox(): Promise<OutboxFile>
  /** Reads, edits and writes the outbox in one transaction, so two tabs
   * editing it never lose each other's writes. Answers what was written. */
  updateOutbox(edit: (file: OutboxFile) => void): Promise<OutboxFile>
  readRecords(): Promise<StoredRecords>
  writeRecords(change: RecordsChange): Promise<void>
  clearRecords(): Promise<void>
  /** The bytes each area holds, as a person reads a size: roughly the JSON
   * it is kept as. */
  usage(): Promise<{ outbox: number; records: number }>
  /** Forgets everything kept for the repository. */
  destroy(): Promise<void>
  close(): void
}

export interface StoredRecords {
  records: SubstrateRecord[]
  cursor?: WatchCursor
  /** Records a watch row named that were not read again since, as
   * `kind/id`. */
  stale: string[]
}

/** One save of the record cache: `replaceKinds` drops every record of those
 * kinds before `put` lands, and `cursor` and `stale` are written in the same
 * transaction as the records, so the cursor never runs ahead of them. */
export interface RecordsChange {
  replaceKinds?: string[]
  put: SubstrateRecord[]
  remove: string[]
  cursor?: WatchCursor | null
  stale?: string[]
}

const recordKey = (r: { kind: string; id: string }) => `${r.kind}/${r.id}`

/** The database one app keeps for one repository. Several apps served from
 * one origin share its IndexedDB, so the app is part of the name. */
export function databaseName(app: string, repository: string): string {
  return `substrate.${app}.${repository.toLowerCase()}`
}

/** The repository's storage in IndexedDB, or in memory with the reason
 * where IndexedDB cannot be opened (some private windows, a blocked
 * origin). */
export async function openRepositoryStorage(
  app: string,
  repository: string,
  factory: IDBFactory | undefined = globalThis.indexedDB
): Promise<RepositoryStorage> {
  if (!factory) {
    return new MemoryStorage(
      "This browser keeps nothing for this page, so changes not sent yet are lost when it closes."
    )
  }
  try {
    const db = await openDatabase(factory, databaseName(app, repository))
    return new IndexedDBStorage(db, factory)
  } catch (e) {
    return new MemoryStorage(
      `This browser would not keep anything for this page (${e instanceof Error ? e.message : String(e)}), so changes not sent yet are lost when it closes.`
    )
  }
}

function openDatabase(factory: IDBFactory, name: string): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = factory.open(name, 1)
    req.onupgradeneeded = () => {
      const db = req.result
      for (const store of ["outbox", "records", "meta"])
        if (!db.objectStoreNames.contains(store)) db.createObjectStore(store)
    }
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error ?? new Error("IndexedDB refused"))
    req.onblocked = () => reject(new Error("another tab holds an older copy"))
  })
}

function done(tx: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    tx.oncomplete = () => resolve()
    tx.onerror = () => reject(tx.error ?? new Error("IndexedDB refused"))
    tx.onabort = () => reject(tx.error ?? new Error("IndexedDB aborted"))
  })
}

function request<T>(req: IDBRequest): Promise<T> {
  return new Promise((resolve, reject) => {
    req.onsuccess = () => resolve(req.result as T)
    req.onerror = () => reject(req.error ?? new Error("IndexedDB refused"))
  })
}

function kindRange(kind: string): IDBKeyRange {
  return IDBKeyRange.bound(`${kind}/`, `${kind}/￿`)
}

function bytes(value: unknown): number {
  return new TextEncoder().encode(JSON.stringify(value) ?? "").length
}

class IndexedDBStorage implements RepositoryStorage {
  readonly persistent = true
  private readonly db: IDBDatabase
  private readonly factory: IDBFactory

  constructor(db: IDBDatabase, factory: IDBFactory) {
    this.db = db
    this.factory = factory
    // A newer copy of the app in another tab upgrades the database: this
    // one steps aside rather than hold it back.
    db.onversionchange = () => db.close()
  }

  async readOutbox(): Promise<OutboxFile> {
    const tx = this.db.transaction("outbox", "readonly")
    const file = await request<OutboxFile | undefined>(
      tx.objectStore("outbox").get("file")
    )
    return file ?? emptyOutboxFile()
  }

  async updateOutbox(edit: (file: OutboxFile) => void): Promise<OutboxFile> {
    const tx = this.db.transaction("outbox", "readwrite")
    const store = tx.objectStore("outbox")
    let written = emptyOutboxFile()
    const get = store.get("file")
    get.onsuccess = () => {
      const file = (get.result as OutboxFile | undefined) ?? emptyOutboxFile()
      edit(file)
      written = file
      store.put(file, "file")
    }
    await done(tx)
    return written
  }

  async readRecords(): Promise<StoredRecords> {
    const tx = this.db.transaction(["records", "meta"], "readonly")
    const [records, cursor, stale] = await Promise.all([
      request<SubstrateRecord[]>(tx.objectStore("records").getAll()),
      request<WatchCursor | undefined>(tx.objectStore("meta").get("cursor")),
      request<string[] | undefined>(tx.objectStore("meta").get("stale")),
    ])
    return { records, cursor, stale: stale ?? [] }
  }

  async writeRecords(change: RecordsChange): Promise<void> {
    const tx = this.db.transaction(["records", "meta"], "readwrite")
    const records = tx.objectStore("records")
    const meta = tx.objectStore("meta")
    for (const kind of change.replaceKinds ?? [])
      records.delete(kindRange(kind))
    for (const key of change.remove) records.delete(key)
    for (const r of change.put) records.put(r, recordKey(r))
    if (change.cursor === null) meta.delete("cursor")
    else if (change.cursor) meta.put(change.cursor, "cursor")
    if (change.stale) meta.put(change.stale, "stale")
    await done(tx)
  }

  async clearRecords(): Promise<void> {
    const tx = this.db.transaction(["records", "meta"], "readwrite")
    tx.objectStore("records").clear()
    tx.objectStore("meta").clear()
    await done(tx)
  }

  async usage(): Promise<{ outbox: number; records: number }> {
    const tx = this.db.transaction(["outbox", "records", "meta"], "readonly")
    const [outbox, records, meta] = await Promise.all([
      request(tx.objectStore("outbox").getAll()),
      request(tx.objectStore("records").getAll()),
      request(tx.objectStore("meta").getAll()),
    ])
    return { outbox: bytes(outbox), records: bytes(records) + bytes(meta) }
  }

  async destroy(): Promise<void> {
    const name = this.db.name
    this.db.close()
    await new Promise<void>((resolve) => {
      const req = this.factory.deleteDatabase(name)
      req.onsuccess = req.onerror = req.onblocked = () => resolve()
    })
  }

  close(): void {
    this.db.close()
  }
}

/** Everything in memory: what a page keeps when the browser keeps nothing,
 * and what tests run against. */
export class MemoryStorage implements RepositoryStorage {
  readonly persistent: boolean
  readonly problem?: string
  private outbox = emptyOutboxFile()
  private records = new Map<string, SubstrateRecord>()
  private cursor?: WatchCursor
  private stale: string[] = []

  constructor(problem?: string) {
    this.persistent = false
    this.problem = problem
  }

  readOutbox(): Promise<OutboxFile> {
    return Promise.resolve(structuredClone(this.outbox))
  }

  updateOutbox(edit: (file: OutboxFile) => void): Promise<OutboxFile> {
    const file = structuredClone(this.outbox)
    edit(file)
    this.outbox = file
    return Promise.resolve(structuredClone(file))
  }

  readRecords(): Promise<StoredRecords> {
    return Promise.resolve({
      records: [...this.records.values()].map((r) => structuredClone(r)),
      cursor: this.cursor,
      stale: [...this.stale],
    })
  }

  writeRecords(change: RecordsChange): Promise<void> {
    for (const kind of change.replaceKinds ?? [])
      for (const key of [...this.records.keys()])
        if (key.startsWith(`${kind}/`)) this.records.delete(key)
    for (const key of change.remove) this.records.delete(key)
    for (const r of change.put)
      this.records.set(recordKey(r), structuredClone(r))
    if (change.cursor === null) this.cursor = undefined
    else if (change.cursor) this.cursor = change.cursor
    if (change.stale) this.stale = [...change.stale]
    return Promise.resolve()
  }

  clearRecords(): Promise<void> {
    this.records.clear()
    this.cursor = undefined
    this.stale = []
    return Promise.resolve()
  }

  usage(): Promise<{ outbox: number; records: number }> {
    return Promise.resolve({
      outbox: bytes(this.outbox),
      records: bytes([...this.records.values()]),
    })
  }

  async destroy(): Promise<void> {
    this.outbox = emptyOutboxFile()
    await this.clearRecords()
  }

  close(): void {}
}
