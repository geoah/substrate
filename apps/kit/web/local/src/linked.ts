import {
  SubstrateClient,
  SubstrateError,
  describe,
  follow,
  recordKey,
  type FollowOptions,
  type SubstrateRecord,
  type WatchCursor,
  type WatchEvent,
} from "@substrate/client"
import type { RecordCache } from "./cache"
import { browserConnectivity, type Connectivity } from "./connectivity"
import { entryKey, type OutboxEntry } from "./entry"
import { applyEntry } from "./fold"
import type { Outbox } from "./outbox"

export type LinkedStatus =
  "loading" | "live" | "reconnecting" | "error" | "signedOut" | "needsApproval"

export interface LinkedSnapshot {
  /** The records as the owner sees them, by `kind/id`: the server's, with
   * the writes still waiting laid over. */
  records: ReadonlyMap<string, SubstrateRecord>
  /** The records with a write waiting to be sent, by `kind/id`. */
  pending: ReadonlySet<string>
  /** The records with a write the server refused, by `kind/id`. */
  parked: ReadonlySet<string>
  status: LinkedStatus
  /** Words for a status line: why it is reconnecting, or what failed. */
  words?: string
  /** Shown from what this browser kept; nothing newer was read yet. */
  cached: boolean
  /** The read stopped at its limit with this many records read and more
   * left unread. */
  limitReached?: number
  revision: number
}

/** Where the linked records stand, apart from the records: a new object
 * only when one of these changes. */
export interface LinkedStatusSnapshot {
  status: LinkedStatus
  /** Words for a status line: why it is reconnecting, or what failed. */
  words?: string
  /** Shown from what this browser kept; nothing newer was read yet. */
  cached: boolean
  /** The read stopped at its limit with this many records read and more
   * left unread. */
  limitReached?: number
  /** How many records have a write waiting to be sent. */
  pending: number
  /** How many records have a write the server refused. */
  parked: number
}

/** A record's writes in the outbox: `parked` where one was refused, which
 * holds back the rest; `pending` where some wait to be sent. */
export type WriteState = "pending" | "parked"

export interface LinkedRecordsOptions {
  client: SubstrateClient
  kinds: readonly string[]
  outbox?: Outbox
  cache?: RecordCache
  connectivity?: Connectivity
  /** What the app calls its records in a sentence, plural: "tasks". */
  kept?: string
  /** The most records a read takes. */
  limit?: number
  follow?: FollowOptions
}

const signedOutWords = "Your access to this server ended. Sign in again."
const noRecords: ReadonlyMap<string, SubstrateRecord> = new Map()

/** `kind/id` split after the kind's three segments: an id may hold a
 * slash, a kind never more than two. */
function splitKey(key: string): [string, string] {
  let at = -1
  for (let i = 0; i < 3; i++) {
    at = key.indexOf("/", at + 1)
    if (at < 0) return [key, ""]
  }
  return [key.slice(0, at), key.slice(at + 1)]
}

/** Values by kind, then id, so a change to one kind touches no other's map,
 * keyed `kind/id` at its edge as the outbox and the fold key a record.
 * `touched` hears every kind that changed. */
class ByKind<V> {
  private readonly kinds = new Map<string, Map<string, V>>()
  private readonly touched: (kind: string) => void

  constructor(touched: (kind: string) => void) {
    this.touched = touched
  }

  of(kind: string): ReadonlyMap<string, V> {
    return this.kinds.get(kind) ?? (noRecords as ReadonlyMap<string, V>)
  }

  get(key: string): V | undefined {
    const [kind, id] = splitKey(key)
    return this.kinds.get(kind)?.get(id)
  }

  set(key: string, value: V): this {
    const [kind, id] = splitKey(key)
    let ids = this.kinds.get(kind)
    if (!ids) this.kinds.set(kind, (ids = new Map<string, V>()))
    if (ids.get(id) === value && ids.has(id)) return this
    ids.set(id, value)
    this.touched(kind)
    return this
  }

  delete(key: string): boolean {
    const [kind, id] = splitKey(key)
    if (!this.kinds.get(kind)?.delete(id)) return false
    this.touched(kind)
    return true
  }

  replace(kind: string, ids: Map<string, V>): void {
    this.kinds.set(kind, ids)
    this.touched(kind)
  }
}

/** One record's waiting writes as last laid over the server's copy: the
 * same answer while neither moves. */
interface Folded {
  base: SubstrateRecord | undefined
  writes: string
  record: SubstrateRecord | undefined
}

/** Whether a fold changed nothing a reader holds: the same records, each
 * the same object. */
function sameRecords(
  a: ReadonlyMap<string, SubstrateRecord>,
  b: ReadonlyMap<string, SubstrateRecord>
): boolean {
  if (a.size !== b.size) return false
  for (const [id, r] of b) if (a.get(id) !== r) return false
  return true
}

/** The records of the kinds an app shows, kept linked to the repository:
 * what this browser kept, shown at once; then read whole and followed on
 * the change feed, each changed record read again (decision 0061); with the
 * outbox's waiting writes laid over, so a change made offline shows at once
 * and stays shown until the outbox says it landed (the kit's
 * LinkedRecords).
 *
 * The read opens the watch first and lists after its bookmark, and the
 * watch is followed from that bookmark: an app token's lists carry no head
 * (decision 0195), so a list read first leaves nothing to follow from but
 * the live head, past whatever was written meanwhile. A change written
 * while the list was read is read again after it.
 *
 * A screen reads one kind, one record or the status, each through a
 * snapshot that stays the same object until what it holds changes: a
 * change to one record rebuilds its kind's map alone, and a status change
 * none. `subscribe` hears every change, once per turn. */
export class LinkedRecords {
  readonly client: SubstrateClient
  readonly kinds: string[]
  private readonly outbox?: Outbox
  private readonly cache?: RecordCache
  private readonly connectivity: Connectivity
  private readonly kept: string
  private readonly limit: number
  private readonly followOptions?: FollowOptions
  /** The records as the server last answered them. */
  private readonly server = new ByKind<SubstrateRecord>((k) => this.touch(k))
  /** The app's kinds' waiting writes, by kind, in the outbox's order. */
  private queue = new Map<string, OutboxEntry[]>()
  private queueEntries?: readonly OutboxEntry[]
  private queueSignature = new Map<string, string>()
  private writeStates = new Map<string, WriteState>()
  private pendingCount = 0
  private parkedCount = 0
  private views = new Map<string, ReadonlyMap<string, SubstrateRecord>>()
  private folds = new Map<string, Map<string, Folded>>()
  /** The kinds a change touched since their map was last folded. */
  private refold = new Set<string>()
  private listedKinds = new Set<string>()
  private status: LinkedStatus = "loading"
  private words?: string
  private cached = false
  private limitReached?: number
  private revision = 0
  private statusShot: LinkedStatusSnapshot
  private built?: LinkedSnapshot
  private listeners = new Set<() => void>()
  /** The session, start to stop. */
  private abort?: AbortController
  /** The follow running now and the reads it started: the session's one
   * child, replaced on every connect. */
  private watch?: AbortController
  private watchState: "connecting" | "open" | "paused" | "ended" | "stopped" =
    "stopped"
  private cursor?: WatchCursor
  /** The list in flight, by number; 0 for none. */
  private listing = 0
  private lists = 0
  /** The last list taken: one started before it answers into nothing. */
  private listTaken = 0
  private listRetry?: ReturnType<typeof setTimeout>
  private collected = new Set<string>()
  /** Each record's read in flight, by ticket: one removed or replaced is
   * answered into nothing. */
  private inflight = new Map<string, number>()
  private tickets = 0
  /** The last ticket handed out before a list was taken: a read from
   * before it that found nothing is older than the list. */
  private listedAt = 0
  /** The same, per record, for a write of it that landed. */
  private landedAt = new Map<string, number>()
  private unsubscribers: (() => void)[] = []
  private emitQueued = false

  constructor(opts: LinkedRecordsOptions) {
    this.client = opts.client
    this.kinds = [...opts.kinds]
    this.outbox = opts.outbox
    this.cache = opts.cache
    this.connectivity = opts.connectivity ?? browserConnectivity
    this.kept = opts.kept ?? "records"
    this.limit = opts.limit ?? 10_000
    this.followOptions = opts.follow
    this.takeQueue()
    this.statusShot = this.makeStatus()
  }

  subscribe = (fn: () => void): (() => void) => {
    this.listeners.add(fn)
    return () => this.listeners.delete(fn)
  }

  /** The records of one kind as the owner sees them, by id: the server's
   * with the waiting writes laid over, a refused one not. The same map
   * until a record of the kind changes. */
  kindSnapshot = (kind: string): ReadonlyMap<string, SubstrateRecord> => {
    if (this.refold.delete(kind)) {
      const next = this.fold(kind)
      const was = this.views.get(kind)
      if (!was || !sameRecords(was, next)) this.views.set(kind, next)
    }
    return this.views.get(kind) ?? noRecords
  }

  /** One record as the owner sees it: the same object until it changes. */
  recordSnapshot = (kind: string, id: string): SubstrateRecord | undefined =>
    this.kindSnapshot(kind).get(id)

  /** Whether a record has writes waiting, or one refused. */
  writeState = (kind: string, id: string): WriteState | undefined =>
    this.writeStates.get(recordKey(kind, id))

  /** Whether a kind's records are known whole, so a record not among them
   * is absent rather than not read yet: listed from the server, or kept
   * whole by this browser. */
  hasListed = (kind: string): boolean => this.listedKinds.has(kind)

  statusSnapshot = (): LinkedStatusSnapshot => {
    const s = this.statusShot
    if (
      s.status !== this.status ||
      s.words !== this.words ||
      s.cached !== this.cached ||
      s.limitReached !== this.limitReached ||
      s.pending !== this.pendingCount ||
      s.parked !== this.parkedCount
    )
      this.statusShot = this.makeStatus()
    return this.statusShot
  }

  /** Every kind's records and the status in one snapshot, built when asked
   * and kept until the next change. It copies every kind: a screen reads
   * the snapshot of what it shows. */
  getSnapshot = (): LinkedSnapshot => {
    if (this.built?.revision === this.revision) return this.built
    const records = new Map<string, SubstrateRecord>()
    for (const kind of this.kinds)
      for (const [id, r] of this.kindSnapshot(kind))
        records.set(recordKey(kind, id), r)
    const pending = new Set<string>()
    const parked = new Set<string>()
    for (const entries of this.queue.values())
      for (const e of entries) (e.parked ? parked : pending).add(entryKey(e))
    const s = this.statusSnapshot()
    this.built = {
      records,
      pending,
      parked,
      status: s.status,
      words: s.words,
      cached: s.cached,
      limitReached: s.limitReached,
      revision: this.revision,
    }
    return this.built
  }

  private makeStatus(): LinkedStatusSnapshot {
    return {
      status: this.status,
      words: this.words,
      cached: this.cached,
      limitReached: this.limitReached,
      pending: this.pendingCount,
      parked: this.parkedCount,
    }
  }

  /** One kind as the owner sees it. A record whose writes and server copy
   * did not move keeps the object it was laid over as. */
  private fold(kind: string): ReadonlyMap<string, SubstrateRecord> {
    const server = this.server.of(kind)
    const out = new Map(server)
    const byId = new Map<string, OutboxEntry[]>()
    for (const e of this.queue.get(kind) ?? []) {
      if (e.parked) continue
      const writes = byId.get(e.recordID)
      if (writes) writes.push(e)
      else byId.set(e.recordID, [e])
    }
    const was = this.folds.get(kind)
    const folds = new Map<string, Folded>()
    for (const [id, entries] of byId) {
      const base = server.get(id)
      const writes = entries.map((e) => e.id).join(",")
      let memo = was?.get(id)
      if (!memo || memo.base !== base || memo.writes !== writes)
        memo = {
          base,
          writes,
          record: entries.reduce<SubstrateRecord | undefined>(
            (r, e) => applyEntry(e, r),
            base
          ),
        }
      folds.set(id, memo)
      if (memo.record) out.set(id, memo.record)
      else out.delete(id)
    }
    this.folds.set(kind, folds)
    return out
  }

  /** The outbox's queue again: the kinds whose waiting writes moved are
   * folded again, and no other. */
  private takeQueue(): void {
    const entries = this.outbox?.getSnapshot().entries ?? []
    if (entries === this.queueEntries) return
    this.queueEntries = entries
    const byKind = new Map<string, OutboxEntry[]>()
    for (const e of entries) {
      if (!this.kinds.includes(e.kind)) continue
      const list = byKind.get(e.kind)
      if (list) list.push(e)
      else byKind.set(e.kind, [e])
    }
    let moved = false
    for (const kind of this.kinds) {
      const list = byKind.get(kind) ?? []
      const signature = list
        .map((e) => (e.parked ? `${e.id}!` : e.id))
        .join(",")
      if (signature === (this.queueSignature.get(kind) ?? "")) continue
      this.queueSignature.set(kind, signature)
      this.queue.set(kind, list)
      this.touch(kind)
      moved = true
    }
    if (!moved) return
    const states = new Map<string, WriteState>()
    for (const list of this.queue.values())
      for (const e of list) {
        const key = entryKey(e)
        if (e.parked) states.set(key, "parked")
        else if (!states.has(key)) states.set(key, "pending")
      }
    this.writeStates = states
    let pending = 0
    for (const state of states.values()) if (state === "pending") pending++
    this.pendingCount = pending
    this.parkedCount = states.size - pending
  }

  private touch(kind: string): void {
    this.refold.add(kind)
    this.emit()
  }

  /** Tells every listener once for every change made in the same turn. */
  private emit(): void {
    if (this.emitQueued) return
    this.emitQueued = true
    queueMicrotask(() => {
      this.emitQueued = false
      this.revision++
      for (const fn of this.listeners) fn()
    })
  }

  private setStatus(status: LinkedStatus, words?: string): void {
    if (this.status === status && this.words === words) return
    this.status = status
    this.words = words
    this.emit()
  }

  start(): void {
    this.stop()
    const abort = new AbortController()
    this.abort = abort
    abort.signal.addEventListener("abort", () => this.watch?.abort(), {
      once: true,
    })
    if (this.outbox) {
      this.takeQueue()
      this.unsubscribers.push(this.outbox.subscribe(() => this.takeQueue()))
      this.unsubscribers.push(
        this.outbox.onLanded((entry, record) => this.layLanded(entry, record))
      )
    }
    this.unsubscribers.push(
      this.connectivity.subscribe((event) => {
        if (event === "offline" || abort.signal.aborted) return
        // Back online or to the front: a follow waiting out its pause, one
        // that ended, or a list waiting to be tried again goes now. One
        // reading, open, or stopped for good is left alone.
        if (this.listing) return
        if (
          this.watchState === "paused" ||
          this.watchState === "ended" ||
          this.listRetry !== undefined
        )
          this.connect()
      })
    )
    void this.begin(abort.signal)
  }

  stop(): void {
    this.abort?.abort()
    this.abort = undefined
    this.watch = undefined
    this.watchState = "stopped"
    this.listing = 0
    clearTimeout(this.listRetry)
    this.listRetry = undefined
    for (const u of this.unsubscribers) u()
    this.unsubscribers = []
  }

  /** Reads every record again and follows the feed from the read's head. */
  async reload(): Promise<void> {
    this.cursor = undefined
    this.cache?.relist()
    if (this.abort) this.connect()
    else this.start()
    return Promise.resolve()
  }

  private async begin(signal: AbortSignal): Promise<void> {
    if (this.cache) {
      try {
        const kept = await this.cache.load(this.kinds)
        if (signal.aborted) return
        const byKind = new Map<string, Map<string, SubstrateRecord>>()
        for (const kind of kept.kinds ?? []) {
          byKind.set(kind, new Map())
          this.listedKinds.add(kind)
        }
        for (const r of kept.records) byKind.get(r.kind)?.set(r.id, r)
        for (const [kind, ids] of byKind) this.server.replace(kind, ids)
        this.cursor = kept.cursor
        this.cached = kept.records.length > 0 || !!kept.cursor
        this.emit()
      } catch {
        // nothing kept: read it all
      }
    }
    if (!signal.aborted) this.connect()
  }

  /** Follows the feed from where it was, or opens it to list after its
   * bookmark. Replaces the follow running, and the list it opened. */
  private connect(): void {
    if (!this.abort || this.abort.signal.aborted) return
    this.watch?.abort()
    clearTimeout(this.listRetry)
    this.listRetry = undefined
    const watch = new AbortController()
    this.watch = watch
    this.watchState = "connecting"
    let mustList = !this.cursor
    let listedEarly = false
    if (mustList && !this.listedKinds.size)
      this.setStatus("loading", `Reading your ${this.kept}…`)
    const onEvent = (e: WatchEvent) => {
      if (watch.signal.aborted) return
      switch (e.type) {
        case "opened":
          this.watchState = "open"
          if (mustList) {
            mustList = false
            void this.list(e.cursor, watch.signal)
          } else {
            this.cursor = e.cursor
            this.cache?.bookmark(e.cursor)
            this.cached = false
            this.setStatus("live")
            this.rereadStale(watch.signal)
          }
          break
        case "row": {
          const { row } = e
          if (this.cursor)
            this.cursor = {
              seq: Math.max(this.cursor.seq, row.seq),
              generation: this.cursor.generation,
            }
          if (!this.kinds.includes(row.kind)) break
          this.cache?.row(row)
          const key = recordKey(row.kind, row.recordId)
          if (this.listing) {
            this.collected.add(key)
          } else if (row.removes) {
            this.removed(key)
          } else {
            void this.refresh(row.kind, row.recordId, watch.signal)
          }
          if (!this.listing) this.setStatus("live")
          break
        }
        case "relist":
          this.cursor = undefined
          this.cache?.relist()
          this.connect()
          break
        case "interrupted":
          this.watchState = "paused"
          // A watch that cannot open holds back neither the records nor why
          // a list is refused, which says more than the watch's refusal.
          if (
            mustList &&
            !listedEarly &&
            this.kinds.some((k) => !this.listedKinds.has(k))
          ) {
            listedEarly = true
            void this.list(undefined, watch.signal)
          }
          this.setStatus("reconnecting", e.words)
          break
        case "signedOut":
          this.watchState = "stopped"
          this.setStatus("signedOut", signedOutWords)
          break
        case "needsApproval":
          this.watchState = "stopped"
          this.setStatus(
            "needsApproval",
            `Your approval does not reach your ${this.kept}.`
          )
          break
        case "ended":
          this.watchState = "ended"
          this.setStatus(
            "error",
            `Your ${this.kept} are not being updated: ${e.words}`
          )
          break
      }
    }
    follow(
      this.client,
      this.kinds,
      this.cursor ?? null,
      onEvent,
      watch.signal,
      this.followOptions
    )
  }

  /** The kinds read whole and kept, with the watch's bookmark as where the
   * watch follows them from; without one, shown and not followed from. */
  private async list(
    bookmark: WatchCursor | undefined,
    signal: AbortSignal
  ): Promise<void> {
    const run = ++this.lists
    this.listing = run
    clearTimeout(this.listRetry)
    this.listRetry = undefined
    try {
      const page = await this.client.listAll(
        { kinds: this.kinds, first: 500 },
        this.limit,
        signal
      )
      if (signal.aborted || run < this.listTaken) return
      this.listTaken = run
      this.listedAt = this.tickets
      this.landedAt.clear()
      const byKind = new Map<string, Map<string, SubstrateRecord>>(
        this.kinds.map((k) => [k, new Map()])
      )
      for (const r of page.records) {
        if (r.deletedAt) continue
        const here = this.server.get(recordKey(r.kind, r.id))
        byKind.get(r.kind)?.set(r.id, here && same(here, r) ? here : r)
      }
      for (const [kind, ids] of byKind) {
        this.server.replace(kind, ids)
        this.listedKinds.add(kind)
      }
      this.limitReached = page.cursor ? page.records.length : undefined
      this.cached = false
      this.cache?.listed(page.records, this.kinds, bookmark)
      if (bookmark) {
        this.cursor = bookmark
        this.status = "live"
        this.words = undefined
      }
      if (this.listing === run) this.listing = 0
      this.emit()
      const keys = [...this.collected]
      this.collected.clear()
      for (const key of keys) {
        const [kind, id] = splitKey(key)
        void this.refresh(kind, id, signal)
      }
    } catch (e) {
      if (signal.aborted) return
      if (e instanceof SubstrateError && e.status === 401)
        return this.setStatus("signedOut", signedOutWords)
      if (e instanceof SubstrateError && e.insufficientScope)
        return this.setStatus(
          "needsApproval",
          `Your approval does not reach your ${this.kept}.`
        )
      this.setStatus("reconnecting", describe(e))
      if (bookmark)
        this.listRetry = setTimeout(() => {
          this.listRetry = undefined
          if (!signal.aborted) void this.list(bookmark, signal)
        }, 5000)
    } finally {
      if (this.listing === run) this.listing = 0
    }
  }

  private rereadStale(signal: AbortSignal): void {
    for (const key of this.cache?.staleKeys ?? []) {
      const [kind, id] = splitKey(key)
      void this.refresh(kind, id, signal)
    }
  }

  /** A record the server says is gone: a read of it in flight answers
   * from before, or from a restore whose own row reads it again. */
  private removed(key: string): void {
    this.inflight.delete(key)
    this.landedAt.delete(key)
    this.server.delete(key)
  }

  /** Reads one record again, as the feed named it. An answer a removal,
   * a list or a landed write overtook while it was read is dropped, and a
   * version older than the one here never replaces it. */
  async refresh(kind: string, id: string, signal?: AbortSignal): Promise<void> {
    if (!this.kinds.includes(kind)) return
    const key = recordKey(kind, id)
    const ticket = ++this.tickets
    this.inflight.set(key, ticket)
    try {
      const record = await this.client.record(kind, id, signal)
      if (this.inflight.get(key) !== ticket || signal?.aborted) return
      this.inflight.delete(key)
      const before = Math.max(this.listedAt, this.landedAt.get(key) ?? 0)
      if (!record && ticket <= before) return
      const here = this.server.get(key)
      const next =
        record && here && (here.version > record.version || same(here, record))
          ? here
          : record
      if (next) this.server.set(key, next)
      else this.server.delete(key)
      this.cache?.refreshed(key, next)
    } catch (e) {
      if (this.inflight.get(key) === ticket) this.inflight.delete(key)
      if (e instanceof SubstrateError && e.status === 401)
        this.setStatus("signedOut", signedOutWords)
      // Otherwise it stays stale: read again when the watch opens again.
    }
  }

  /** A write the outbox says landed, laid over the server's copy before it
   * leaves the queue. */
  private layLanded(
    entry: OutboxEntry,
    record: SubstrateRecord | undefined
  ): void {
    if (!this.kinds.includes(entry.kind)) return
    const key = entryKey(entry)
    if (entry.method === "DELETE") {
      this.removed(key)
      this.cache?.refreshed(key, null)
      return
    }
    this.landedAt.set(key, this.tickets)
    if (!record) return
    const here = this.server.get(key)
    if (here && (here.version > record.version || same(here, record))) return
    this.server.set(key, record)
    this.cache?.refreshed(key, record)
  }

  /** The version the server last answered for a record: what a guarded
   * change is sent on. */
  version(kind: string, id: string): number | undefined {
    return this.server.get(recordKey(kind, id))?.version
  }

  /** A record as the owner sees it. */
  get(kind: string, id: string): SubstrateRecord | undefined {
    return this.recordSnapshot(kind, id)
  }

  /** Every record of a kind as the owner sees them. */
  all(kind: string): SubstrateRecord[] {
    return [...this.kindSnapshot(kind).values()]
  }
}

/** Two answers for one record at one version: the same record, so the
 * object a screen holds stays. */
function same(a: SubstrateRecord, b: SubstrateRecord): boolean {
  return a.version === b.version && a.title === b.title
}
