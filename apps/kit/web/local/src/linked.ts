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
import { foldRecords } from "./fold"
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
 * while the list was read is read again after it. */
export class LinkedRecords {
  readonly client: SubstrateClient
  readonly kinds: string[]
  protected readonly outbox?: Outbox
  protected readonly cache?: RecordCache
  private readonly connectivity: Connectivity
  private readonly kept: string
  private readonly limit: number
  private readonly followOptions?: FollowOptions
  protected server = new Map<string, SubstrateRecord>()
  /** Writes shown before the server has them outside the outbox, by key:
   * what the deprecated `apply` lays over. */
  protected overlays = new Map<string, SubstrateRecord | null>()
  private status: LinkedStatus = "loading"
  private words?: string
  private cached = false
  private limitReached?: number
  private revision = 0
  private snapshot: LinkedSnapshot
  private listeners = new Set<() => void>()
  private abort?: AbortController
  private watch?: AbortController
  private cursor?: WatchCursor
  private listing = false
  private collected = new Set<string>()
  private inflight = new Map<string, number>()
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
    this.snapshot = this.makeSnapshot()
  }

  subscribe = (fn: () => void): (() => void) => {
    this.listeners.add(fn)
    return () => this.listeners.delete(fn)
  }

  getSnapshot = (): LinkedSnapshot => this.snapshot

  private makeSnapshot(): LinkedSnapshot {
    const queue = (this.outbox?.getSnapshot().entries ?? []).filter((e) =>
      this.kinds.includes(e.kind)
    )
    const records = foldRecords(this.server, queue)
    for (const [key, r] of this.overlays) {
      if (r) records.set(key, r)
      else records.delete(key)
    }
    const pending = new Set<string>()
    const parked = new Set<string>()
    for (const e of queue) (e.parked ? parked : pending).add(entryKey(e))
    return {
      records,
      pending,
      parked,
      status: this.status,
      words: this.words,
      cached: this.cached,
      limitReached: this.limitReached,
      revision: this.revision,
    }
  }

  /** Draws once for every change made in the same turn. */
  protected emit(): void {
    if (this.emitQueued) return
    this.emitQueued = true
    queueMicrotask(() => {
      this.emitQueued = false
      this.revision++
      this.snapshot = this.makeSnapshot()
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
    if (this.outbox) {
      this.unsubscribers.push(this.outbox.subscribe(() => this.emit()))
      this.unsubscribers.push(
        this.outbox.onLanded((entry, record) => this.layLanded(entry, record))
      )
    }
    this.unsubscribers.push(
      this.connectivity.subscribe((event) => {
        if (event === "offline" || abort.signal.aborted) return
        // Back online or to the front: a watch waiting out its pause opens
        // now. One stopped for good stays stopped.
        if (
          this.status === "reconnecting" ||
          this.status === "error" ||
          this.status === "loading"
        )
          this.connect(abort.signal)
      })
    )
    void this.begin(abort.signal)
  }

  stop(): void {
    this.abort?.abort()
    this.abort = undefined
    this.watch = undefined
    for (const u of this.unsubscribers) u()
    this.unsubscribers = []
  }

  /** Reads every record again and follows the feed from the read's head. */
  async reload(): Promise<void> {
    this.cursor = undefined
    this.cache?.relist()
    if (this.abort) this.connect(this.abort.signal)
    else this.start()
    return Promise.resolve()
  }

  private async begin(signal: AbortSignal): Promise<void> {
    if (this.cache) {
      try {
        const kept = await this.cache.load(this.kinds)
        if (signal.aborted) return
        for (const r of kept.records)
          this.server.set(recordKey(r.kind, r.id), r)
        this.cursor = kept.cursor
        this.cached = kept.records.length > 0 || !!kept.cursor
        this.emit()
      } catch {
        // nothing kept: read it all
      }
    }
    if (!signal.aborted) this.connect(signal)
  }

  /** Follows the feed from where it was, or opens it to list after its
   * bookmark. Replaces any follow running. */
  private connect(signal: AbortSignal): void {
    this.watch?.abort()
    const watch = new AbortController()
    this.watch = watch
    signal.addEventListener("abort", () => watch.abort(), { once: true })
    let mustList = !this.cursor
    let listedEarly = false
    if (mustList && !this.server.size)
      this.setStatus("loading", `Reading your ${this.kept}…`)
    const onEvent = (e: WatchEvent) => {
      if (watch.signal.aborted) return
      switch (e.type) {
        case "opened":
          if (mustList) {
            mustList = false
            void this.list(e.cursor, watch.signal)
          } else {
            this.cursor = e.cursor
            this.cache?.bookmark(e.cursor)
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
            this.server.delete(key)
            this.emit()
          } else {
            void this.refresh(row.kind, row.recordId, watch.signal)
          }
          if (!this.listing) this.setStatus("live")
          break
        }
        case "relist":
          this.cursor = undefined
          this.cache?.relist()
          this.connect(signal)
          break
        case "interrupted":
          // A watch that cannot open holds back neither the records nor why
          // a list is refused, which says more than the watch's refusal.
          if (mustList && !listedEarly && !this.server.size) {
            listedEarly = true
            void this.list(undefined, watch.signal)
          }
          this.setStatus("reconnecting", e.words)
          break
        case "signedOut":
          this.setStatus("signedOut", signedOutWords)
          break
        case "needsApproval":
          this.setStatus(
            "needsApproval",
            `Your approval does not reach your ${this.kept}.`
          )
          break
        case "ended":
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
    this.listing = true
    try {
      const page = await this.client.listAll(
        { kinds: this.kinds, first: 500 },
        this.limit,
        signal
      )
      if (signal.aborted) return
      this.server = new Map(
        page.records
          .filter((r) => !r.deletedAt && this.kinds.includes(r.kind))
          .map((r) => [recordKey(r.kind, r.id), r])
      )
      this.limitReached = page.cursor ? page.records.length : undefined
      this.cached = false
      this.cache?.listed(page.records, this.kinds, bookmark)
      if (bookmark) {
        this.cursor = bookmark
        this.status = "live"
        this.words = undefined
      }
      this.listing = false
      this.emit()
      const keys = [...this.collected]
      this.collected.clear()
      for (const key of keys) {
        const at = key.lastIndexOf("/")
        void this.refresh(key.slice(0, at), key.slice(at + 1), signal)
      }
    } catch (e) {
      this.listing = false
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
        setTimeout(() => {
          if (!signal.aborted) void this.list(bookmark, signal)
        }, 5000)
    }
  }

  private rereadStale(signal: AbortSignal): void {
    for (const key of this.cache?.staleKeys ?? []) {
      const at = key.lastIndexOf("/")
      void this.refresh(key.slice(0, at), key.slice(at + 1), signal)
    }
  }

  /** Reads one record again, as the feed named it. */
  async refresh(kind: string, id: string, signal?: AbortSignal): Promise<void> {
    if (!this.kinds.includes(kind)) return
    const key = recordKey(kind, id)
    const ticket = (this.inflight.get(key) ?? 0) + 1
    this.inflight.set(key, ticket)
    try {
      const record = await this.client.record(kind, id, signal)
      if (this.inflight.get(key) !== ticket) return
      if (record) this.server.set(key, record)
      else this.server.delete(key)
      this.overlays.delete(key)
      this.cache?.refreshed(key, record)
      this.emit()
    } catch (e) {
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
      this.server.delete(key)
      this.cache?.refreshed(key, null)
      return
    }
    if (!record) return
    const here = this.server.get(key)
    if (here && here.version > record.version) return
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
    return this.snapshot.records.get(recordKey(kind, id))
  }

  /** Every record of a kind as the owner sees them. */
  all(kind: string): SubstrateRecord[] {
    return [...this.snapshot.records.values()].filter((r) => r.kind === kind)
  }
}
