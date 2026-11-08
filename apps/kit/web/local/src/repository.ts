import type { SubstrateClient } from "@substrate/client"
import { RecordCache } from "./cache"
import type { Connectivity } from "./connectivity"
import { LinkedRecords, type LinkedRecordsOptions } from "./linked"
import { Outbox } from "./outbox"
import {
  databaseName,
  openRepositoryStorage,
  type RepositoryStorage,
} from "./storage"

/** One signed-in repository as an app keeps it in the browser: its
 * storage, the outbox its writes wait in, the cache its records open from,
 * and the linked records the screens read. */
export interface RepositoryLink<L extends LinkedRecords = LinkedRecords> {
  storage: RepositoryStorage
  outbox: Outbox
  cache: RecordCache
  linked: L
  /** Starts reading, following and sending. */
  start(): void
  /** Stops all of it, writes what the cache still holds and closes the
   * storage; what is kept stays. Resolves once the storage is closed, and
   * every call answers the same close. */
  close(): Promise<void>
  /** Closes, then deletes what this browser kept for the repository,
   * unsent writes included: a sign-out. Rejects with `NotForgotten`, whose
   * message a screen shows, when the browser did not delete it. */
  forget(): Promise<void>
}

export interface OpenRepositoryOptions<L extends LinkedRecords> {
  /** The app's id: two apps on one origin keep apart. */
  app: string
  repository: string
  client: SubstrateClient
  kinds: readonly string[]
  kept?: string
  connectivity?: Connectivity
  factory?: IDBFactory
  /** The linked records' class, for one that adds to it. */
  makeLinked?: (opts: LinkedRecordsOptions) => L
}

/** Opens what the browser keeps for a repository, and wires the outbox to
 * the linked records: a guarded change is sent on the version the records
 * last read, and a landed write is laid over them before it leaves the
 * queue. */
export async function openRepository<L extends LinkedRecords = LinkedRecords>(
  opts: OpenRepositoryOptions<L>
): Promise<RepositoryLink<L>> {
  const storage = await openRepositoryStorage(
    opts.app,
    opts.repository,
    opts.factory
  )
  // The outbox asks the records for versions, and the records follow the
  // outbox: one is made before the other, so the outbox reads through this.
  const holder: { linked?: L } = {}
  const outbox = new Outbox({
    client: opts.client,
    storage,
    connectivity: opts.connectivity,
    versionOf: (kind, id) => holder.linked?.version(kind, id),
    shareName: `${databaseName(opts.app, opts.repository)}.outbox`,
  })
  const cache = new RecordCache(storage)
  const linkedOptions: LinkedRecordsOptions = {
    client: opts.client,
    kinds: opts.kinds,
    outbox,
    cache,
    connectivity: opts.connectivity,
    kept: opts.kept,
  }
  const link: L = opts.makeLinked
    ? opts.makeLinked(linkedOptions)
    : (new LinkedRecords(linkedOptions) as L)
  holder.linked = link
  await outbox.ready
  let closing: Promise<void> | undefined
  const close = () =>
    (closing ??= (async () => {
      link.stop()
      try {
        await outbox.close()
        await cache.close()
      } finally {
        storage.close()
      }
    })())
  return {
    storage,
    outbox,
    cache,
    linked: link,
    start: () => link.start(),
    close,
    async forget() {
      await close()
      await storage.destroy()
    },
  }
}
