// What this browser keeps of an import while it stages
// (docs/plans/healthspan.md, The package, import: Staging): the plan and the
// bytes of every document not yet named by the import record, so a page
// closed during staging finishes it on its next open, and a blob the
// collector took is uploaded again from here. A document's bytes go once the
// patch naming it is confirmed; the rest once the import is `planned`, or
// discarded.

import type { ImporterId } from "./importers/types"
import type { StoredPlan } from "./planfile"

/** A document's bytes, kept until the import names it. */
export interface KeptDocument {
  digest: string
  name: string
  mediaType: string
  bytes: Uint8Array
}

/** One import being staged from this browser. */
export interface KeptStaging {
  id: string
  name: string
  importer: ImporterId
  lab?: string
  /** A lab source already named when it was planned. */
  source?: string
  skipped?: string
  plan: StoredPlan
  /** The documents whose naming is not confirmed yet. */
  documents: KeptDocument[]
  /** When staging began, ms. */
  startedAt: number
}

export interface StagingKeep {
  put(entry: KeptStaging): Promise<void>
  get(id: string): Promise<KeptStaging | undefined>
  /** The imports this browser is staging, by id. */
  list(): Promise<string[]>
  /** Forgets the bytes of documents the import now names. */
  dropDocuments(id: string, digests: readonly string[]): Promise<void>
  /** Forgets the import: staged, or discarded. */
  drop(id: string): Promise<void>
}

/** A keep in this page's memory: the demo's, and the tests'. */
export function memoryKeep(): StagingKeep & {
  entries: Map<string, KeptStaging>
} {
  const entries = new Map<string, KeptStaging>()
  return {
    entries,
    put: (e) => {
      entries.set(e.id, { ...e, documents: [...e.documents] })
      return Promise.resolve()
    },
    get: (id) => Promise.resolve(entries.get(id)),
    list: () => Promise.resolve([...entries.keys()]),
    dropDocuments: (id, digests) => {
      const e = entries.get(id)
      if (e) {
        const gone = new Set(digests)
        e.documents = e.documents.filter((d) => !gone.has(d.digest))
      }
      return Promise.resolve()
    },
    drop: (id) => {
      entries.delete(id)
      return Promise.resolve()
    },
  }
}

const DB = "healthspan.staging"
const STORE = "imports"

function request<T>(r: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    r.onsuccess = () => resolve(r.result)
    r.onerror = () => reject(r.error ?? new Error("IndexedDB refused."))
  })
}

function open(): Promise<IDBDatabase> {
  const r = indexedDB.open(DB, 1)
  r.onupgradeneeded = () => {
    if (!r.result.objectStoreNames.contains(STORE))
      r.result.createObjectStore(STORE, { keyPath: "key" })
  }
  return request(r)
}

/** A keep in this browser's IndexedDB, one per repository (`scope`), so a
 * page of another repository never sees this one's staging. */
export function indexedDbKeep(scope: string): StagingKeep {
  let db: Promise<IDBDatabase> | undefined
  const store = async (mode: IDBTransactionMode) => {
    db ??= open().catch((e: unknown) => {
      db = undefined
      throw e
    })
    return (await db).transaction(STORE, mode).objectStore(STORE)
  }
  const key = (id: string) => `${scope}\u0000${id}`
  const read = async (id: string) => {
    const row = (await request((await store("readonly")).get(key(id)))) as
      (KeptStaging & { key: string }) | undefined
    if (!row) return undefined
    const { key: _key, ...entry } = row
    void _key
    return entry
  }
  const write = async (entry: KeptStaging) => {
    await request(
      (await store("readwrite")).put({ ...entry, key: key(entry.id) })
    )
  }
  return {
    put: write,
    get: read,
    list: async () => {
      const keys = (await request(
        (await store("readonly")).getAllKeys()
      )) as string[]
      const prefix = `${scope}\u0000`
      return keys
        .filter((k) => typeof k === "string" && k.startsWith(prefix))
        .map((k) => k.slice(prefix.length))
    },
    dropDocuments: async (id, digests) => {
      const e = await read(id)
      if (!e) return
      const gone = new Set(digests)
      await write({
        ...e,
        documents: e.documents.filter((d) => !gone.has(d.digest)),
      })
    },
    drop: async (id) => {
      await request((await store("readwrite")).delete(key(id)))
    },
  }
}
