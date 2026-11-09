import {
  SubstrateError,
  follow,
  type FollowOptions,
  type SubstrateClient,
  type WatchCursor,
  type WatchEvent,
} from "@substrate/client"
import type { Outbox } from "@substrate/local"

/** A read as a screen draws it. */
export interface ReadState<T> {
  /** The last answer, kept while a newer read runs and when one fails. */
  data: T | undefined
  /** Why the last read failed; cleared by the next that answers. */
  error: unknown
  /** Reading, with no answer to show yet. */
  loading: boolean
  /** Reading again, the last answer still shown. */
  refreshing: boolean
  /** Reads again now. */
  refresh: () => void
}

export interface ReadOptions {
  /** The kinds whose changes the answer depends on: it is read again when
   * one of them changes. */
  kinds?: readonly string[]
  /** How old an answer may be, in ms, before a read the feed does not keep
   * current is read again when the page comes back to the front. Five
   * minutes by default. */
  staleAfter?: number
}

/** A read through the session's client, aborted by its signal. */
export type Read<T> = (
  client: SubstrateClient,
  signal: AbortSignal
) => Promise<T>

export const defaultStaleAfter = 300_000

/** The kinds whose records arrive by the minute: never followed. A read of
 * them is read again by `staleAfter` and `refresh` alone. */
export const sampleKinds: ReadonlySet<string> = new Set([
  "substrate.reamde.dev/observations/observation",
  "substrate.reamde.dev/observations/trace",
])

export interface ReadCacheOptions {
  client: SubstrateClient
  /** A write of a read's kinds that it lands reads it again. */
  outbox?: Outbox
  follow?: FollowOptions
  /** How long the feed's changes are gathered before the reads they touch
   * are read again, in ms. */
  coalesce?: number
  /** How many answers no reader holds are kept for a reader that comes
   * back. */
  keep?: number
  now?: () => number
}

/** One mounted reader of a key. */
interface Reader {
  read: Read<unknown>
  kinds: readonly string[]
  staleAfter: number
  changed: () => void
}

interface Entry {
  key: string
  readers: Set<Reader>
  /** Its readers' kinds, kept after the last one leaves. */
  kinds: string[]
  staleAfter: number
  data: unknown
  hasData: boolean
  error: unknown
  /** The read running. */
  running?: AbortController
  /** A read is asked for and waits on its kinds' feed. */
  held: boolean
  wanted: boolean
  /** When its last read started; 0 before the first. */
  readAt: number
  /** The cache's tick when its last read started; 0 before the first. */
  readTick: number
  /** Read again when a reader next comes: one of its kinds changed while
   * no reader held it, or its read was cut short. */
  stale: boolean
  snapshot: ReadState<unknown>
  refresh: () => void
}

/** What the probe of a kind found: a list of it answered (`known`), was
 * refused for itself (`refused`, a 4xx), or could not be had now. */
type KindState = "probing" | "known" | "refused" | "unreachable"

const noop = () => {}

/** A key no reader has started: read once a reader holds it. */
export const startingRead: ReadState<never> = {
  data: undefined,
  error: undefined,
  loading: true,
  refreshing: false,
  refresh: noop,
}

/** The answers of the reads a session's screens make that are not linked
 * records (an aggregate, a list of a kind the app does not keep), by key,
 * and the one feed that keeps them current. It belongs to one session: its
 * close aborts every read and drops every answer.
 *
 * A key's readers share one read, aborted when its last reader leaves and
 * when a newer read of it starts. A read is read again when one of its
 * kinds changes: a write of it the outbox lands, `invalidate`, or a change
 * on the feed, gathered for `coalesce` ms first.
 *
 * The feed is one watch over the kinds the mounted reads name, except the
 * sample kinds. A kind joins it once a list of it answered this session,
 * so a kind the repository does not hold never ends the watch. An app's
 * token lists with no head, so the watch's own bookmark is where a read is
 * followed from: the first watch starts at the live head, and a read of a
 * kind it carries starts once it opened, so a change after the read began
 * is always heard. A watch that grows starts from the last change it saw.
 * A relist opens a fresh watch, and once that opened reads every followed
 * read again. A watch that ended leaves the reads to `staleAfter`: read
 * again when the page comes back to the front after it. */
export class ReadCache {
  readonly client: SubstrateClient
  private readonly outbox?: Outbox
  private readonly followOptions?: FollowOptions
  private readonly coalesce: number
  private readonly keep: number
  private readonly now: () => number
  private readonly entries = new Map<string, Entry>()
  /** The keys no reader holds, oldest first. */
  private readonly unheld = new Set<string>()
  private readonly session = new AbortController()
  private readonly states = new Map<string, KindState>()
  /** The kinds the watch running follows, and those it carries already,
   * each with the tick it has been carried since without a break. */
  private followed = new Set<string>()
  private live = new Map<string, number>()
  private ticks = 0
  private cursor: WatchCursor | null = null
  private watch?: AbortController
  private stopped = false
  /** The watch running opened since it started. */
  private opened = false
  /** It could not open yet, so the reads held for it went ahead. */
  private released = false
  /** It replaces one whose cursor the history no longer holds. */
  private relisting = false
  private changes = new Set<string>()
  private gathering?: ReturnType<typeof setTimeout>
  private landed = new Set<string>()
  private reconciling = false
  private unsubscribers: (() => void)[] = []
  private closed = false

  constructor(opts: ReadCacheOptions) {
    this.client = opts.client
    this.outbox = opts.outbox
    this.followOptions = opts.follow
    this.coalesce = opts.coalesce ?? 2_000
    this.keep = opts.keep ?? 50
    this.now = opts.now ?? Date.now
  }

  /** Listens for the outbox's landed writes and the page coming back to
   * the front. */
  start(): void {
    const outbox = this.outbox
    if (outbox) {
      this.unsubscribers.push(
        outbox.onLanded((entry) => {
          this.landed.add(entry.kind)
          this.afterSend()
        })
      )
      this.unsubscribers.push(outbox.subscribe(() => this.afterSend()))
    }
    if (typeof document !== "undefined") {
      const visible = () => {
        if (document.visibilityState === "visible") this.back()
      }
      document.addEventListener("visibilitychange", visible)
      this.unsubscribers.push(() =>
        document.removeEventListener("visibilitychange", visible)
      )
    }
  }

  /** Aborts every read and the watch, and drops every answer: the
   * session ended. */
  close(): void {
    this.closed = true
    this.session.abort()
    this.watch?.abort()
    this.watch = undefined
    clearTimeout(this.gathering)
    for (const u of this.unsubscribers) u()
    this.unsubscribers = []
    for (const entry of this.entries.values()) {
      entry.running?.abort()
      entry.running = undefined
    }
    this.entries.clear()
    this.unheld.clear()
  }

  /** The key's read as its readers draw it. */
  snapshot(key: string): ReadState<unknown> {
    return this.entries.get(key)?.snapshot ?? startingRead
  }

  /** A reader holds `key`: read it where nothing is read yet or it came
   * back, and follow its kinds. The answer lets it go. */
  attach(key: string, reader: Reader): () => void {
    if (this.closed) return noop
    let entry = this.entries.get(key)
    if (!entry) {
      entry = this.entry(key)
      this.entries.set(key, entry)
    }
    this.unheld.delete(key)
    const alone = entry.readers.size === 0
    entry.readers.add(reader)
    this.gather(entry)
    this.want(reader.kinds)
    if (alone && this.due(entry)) this.begin(entry)
    const held = entry
    return () => this.detach(held, reader)
  }

  /** Reads again every read naming one of `kinds`; one no reader holds,
   * when a reader comes back. */
  invalidate(kinds: readonly string[]): void {
    if (this.closed || !kinds.length) return
    const changed = new Set(kinds)
    for (const entry of this.entries.values())
      if (entry.kinds.some((k) => changed.has(k))) this.again(entry)
  }

  private entry(key: string): Entry {
    const entry: Entry = {
      key,
      readers: new Set(),
      kinds: [],
      staleAfter: defaultStaleAfter,
      data: undefined,
      hasData: false,
      error: undefined,
      held: false,
      wanted: false,
      readAt: 0,
      readTick: 0,
      stale: false,
      snapshot: startingRead,
      refresh: () => {
        if (this.entries.get(key) === entry) this.again(entry)
      },
    }
    return entry
  }

  /** The readers' kinds and the shortest `staleAfter` among them. */
  private gather(entry: Entry): void {
    if (!entry.readers.size) return
    const kinds = new Set<string>()
    let staleAfter = Infinity
    for (const r of entry.readers) {
      for (const k of r.kinds) kinds.add(k)
      staleAfter = Math.min(staleAfter, r.staleAfter)
    }
    entry.kinds = [...kinds]
    entry.staleAfter = staleAfter
  }

  /** The reader goes. A key its last reader left is let go a moment later,
   * so a reader that comes straight back (React's StrictMode) keeps its
   * read; then its read is aborted and its answer kept for a while. */
  private detach(entry: Entry, reader: Reader): void {
    if (!entry.readers.delete(reader)) return
    this.gather(entry)
    if (entry.readers.size) return
    queueMicrotask(() => {
      if (entry.readers.size || this.entries.get(entry.key) !== entry) return
      if (entry.running || entry.wanted) entry.stale = true
      entry.running?.abort()
      entry.running = undefined
      entry.held = false
      entry.wanted = false
      this.publish(entry)
      this.unheld.add(entry.key)
      for (const key of this.unheld) {
        if (this.unheld.size <= this.keep) break
        this.unheld.delete(key)
        this.entries.delete(key)
      }
    })
  }

  private again(entry: Entry): void {
    if (entry.readers.size) this.begin(entry)
    else entry.stale = true
  }

  /** Whether a key a reader takes up again is read again: never answered,
   * changed meanwhile, failed, or not kept current by the feed since its
   * read and either the feed will carry it or it is older than its
   * `staleAfter`. */
  private due(entry: Entry): boolean {
    if (entry.running || entry.held) return false
    if (!entry.hasData || entry.stale || entry.error !== undefined) return true
    if (this.fresh(entry)) return false
    const followable =
      !this.stopped &&
      entry.kinds.length > 0 &&
      entry.kinds.every(
        (k) => !sampleKinds.has(k) && this.states.get(k) !== "refused"
      )
    return followable || this.now() - entry.readAt >= entry.staleAfter
  }

  /** Whether the feed has carried every kind the read names since before
   * its last read started, so no change of them since went unheard. */
  private fresh(entry: Entry): boolean {
    return (
      entry.readTick > 0 &&
      entry.kinds.length > 0 &&
      entry.kinds.every((k) => (this.live.get(k) ?? Infinity) < entry.readTick)
    )
  }

  /** The kinds the feed carries from now: those it carried already keep
   * their tick, and the rest start one. */
  private carry(kinds: Iterable<string>): void {
    const next = new Map<string, number>()
    for (const k of kinds) next.set(k, this.live.get(k) ?? ++this.ticks)
    this.live = next
  }

  /** A read is asked for: now, or once its kinds are on the feed. */
  private begin(entry: Entry): void {
    entry.wanted = true
    this.settle(entry)
  }

  private settle(entry: Entry): void {
    if (!entry.readers.size || !entry.wanted) return
    if (this.pending(entry)) {
      if (!entry.held) {
        entry.held = true
        this.publish(entry)
      }
      return
    }
    entry.held = false
    entry.wanted = false
    this.run(entry)
  }

  /** Whether one of its kinds is still being probed, or is to be followed
   * and the watch carrying it has not opened. */
  private pending(entry: Entry): boolean {
    return entry.kinds.some((k) => {
      if (sampleKinds.has(k)) return false
      const state = this.states.get(k)
      if (state === "probing") return true
      if (state !== "known" || this.stopped || this.live.has(k)) return false
      return !(this.released && this.followed.has(k))
    })
  }

  /** Whether the feed carries every kind the read names. */
  private covers(entry: Entry): boolean {
    return entry.kinds.length > 0 && entry.kinds.every((k) => this.live.has(k))
  }

  private run(entry: Entry): void {
    entry.running?.abort()
    const abort = new AbortController()
    entry.running = abort
    entry.readAt = this.now()
    entry.readTick = ++this.ticks
    entry.stale = false
    this.publish(entry)
    const reader = [...entry.readers].at(-1)!
    // A read that throws before it returns fails as one that rejects.
    const reading = (async () => reader.read(this.client, abort.signal))()
    reading.then(
      (data) => {
        if (entry.running !== abort) return
        entry.running = undefined
        entry.data = data
        entry.hasData = true
        entry.error = undefined
        this.publish(entry)
      },
      (error: unknown) => {
        if (entry.running !== abort) return
        entry.running = undefined
        entry.error = error
        this.publish(entry)
      }
    )
  }

  private publish(entry: Entry): void {
    const busy = !!entry.running || entry.held
    entry.snapshot = {
      data: entry.data,
      error: entry.error,
      loading: busy && !entry.hasData,
      refreshing: busy && entry.hasData,
      refresh: entry.refresh,
    }
    for (const r of entry.readers) r.changed()
  }

  // ---- the feed ---------------------------------------------------------

  /** Probes the kinds not known yet, and follows the known ones. */
  private want(kinds: readonly string[]): void {
    for (const k of kinds) {
      if (sampleKinds.has(k)) continue
      const state = this.states.get(k)
      if (state === undefined || state === "unreachable") this.probe(k)
      else if (state === "known" && !this.followed.has(k)) this.reconcileSoon()
    }
  }

  /** A list of the kind, one record long: answered, the repository holds
   * it and the watch may name it; refused for itself, it is left out for
   * the session; not reached, it is asked again when a reader of it comes
   * or the page comes back. */
  private probe(kind: string): void {
    this.states.set(kind, "probing")
    this.client.list({ kinds: [kind], first: 1 }, this.session.signal).then(
      () => {
        if (this.closed) return
        this.states.set(kind, "known")
        this.reconcileSoon()
      },
      (e: unknown) => {
        if (this.closed) return
        const refused =
          e instanceof SubstrateError &&
          e.status >= 400 &&
          e.status < 500 &&
          e.status !== 429
        this.states.set(kind, refused ? "refused" : "unreachable")
        this.settleAll()
      }
    )
  }

  private reconcileSoon(): void {
    if (this.reconciling) return
    this.reconciling = true
    queueMicrotask(() => {
      this.reconciling = false
      this.reconcile()
    })
  }

  /** Follows the known kinds the mounted reads name, the watch started
   * again where they are more than it follows. */
  private reconcile(): void {
    if (this.closed) return
    if (!this.stopped) {
      const wanted = new Set<string>()
      for (const entry of this.entries.values()) {
        if (!entry.readers.size) continue
        for (const k of entry.kinds)
          if (this.states.get(k) === "known") wanted.add(k)
      }
      if ([...wanted].some((k) => !this.followed.has(k))) this.restart(wanted)
    }
    this.settleAll()
  }

  /** A watch over `kinds`, from the last change seen, else the live head.
   * The kinds the watch before carried and this one follows from its
   * cursor stay carried: every change after it is delivered again. */
  private restart(kinds: Set<string>): void {
    this.watch?.abort()
    const watch = new AbortController()
    this.watch = watch
    this.followed = kinds
    this.carry(
      this.cursor ? [...this.live.keys()].filter((k) => kinds.has(k)) : []
    )
    this.opened = false
    this.released = false
    follow(
      this.client,
      [...kinds].sort(),
      this.cursor,
      (e) => {
        if (this.watch === watch && !watch.signal.aborted) this.heard(e)
      },
      watch.signal,
      this.followOptions
    )
  }

  private heard(e: WatchEvent): void {
    switch (e.type) {
      case "opened": {
        this.cursor = e.cursor
        // A reconnect resumes from its cursor: nothing was missed.
        if (this.opened) return
        this.opened = true
        this.carry(this.followed)
        const relisted = this.relisting
        this.relisting = false
        for (const entry of this.entries.values()) {
          const followed = entry.kinds.some((k) => this.followed.has(k))
          if (!entry.readers.size) {
            if (relisted && followed) entry.stale = true
            continue
          }
          // A read made before the feed carried its kinds is made again,
          // and after a relist every followed read.
          const unheard =
            entry.readTick > 0 && this.covers(entry) && !this.fresh(entry)
          if ((relisted && followed) || unheard) this.begin(entry)
          else this.settle(entry)
        }
        return
      }
      case "row":
        if (this.cursor)
          this.cursor = {
            seq: Math.max(this.cursor.seq, e.row.seq),
            generation: this.cursor.generation,
          }
        this.changes.add(e.row.kind)
        this.gathering ??= setTimeout(() => {
          this.gathering = undefined
          const kinds = [...this.changes]
          this.changes.clear()
          this.invalidate(kinds)
        }, this.coalesce)
        return
      case "relist":
        // The history moved past the cursor: a fresh watch first, and the
        // reads once it opened, so no change falls between them.
        this.cursor = null
        this.relisting = true
        this.restart(new Set(this.followed))
        return
      case "interrupted":
        // A watch that cannot open holds no read back; each is read again
        // once it opens.
        if (!this.opened && !this.released) {
          this.released = true
          this.settleAll()
        }
        return
      case "signedOut":
      case "needsApproval":
      case "ended":
        this.stop()
        return
    }
  }

  /** No more following this session: the reads fall back to
   * `staleAfter`. */
  private stop(): void {
    this.watch?.abort()
    this.watch = undefined
    this.stopped = true
    this.followed = new Set()
    this.carry([])
    this.relisting = false
    this.settleAll()
  }

  private settleAll(): void {
    for (const entry of this.entries.values()) this.settle(entry)
  }

  /** A send ended: the reads of the kinds it landed are read again. */
  private afterSend(): void {
    if (!this.landed.size || this.outbox?.getSnapshot().flushing) return
    const kinds = [...this.landed]
    this.landed.clear()
    this.invalidate(kinds)
  }

  /** The page came back to the front: a read the feed does not keep
   * current is read again once older than its `staleAfter`, or where its
   * last read failed, and a kind not reached is asked again. */
  private back(): void {
    if (this.closed) return
    const now = this.now()
    for (const entry of this.entries.values()) {
      if (!entry.readers.size) continue
      for (const k of entry.kinds)
        if (this.states.get(k) === "unreachable") this.probe(k)
      if (entry.running || entry.held) continue
      const failed = entry.error !== undefined
      if (this.covers(entry) && !failed) continue
      if (failed || now - entry.readAt >= entry.staleAfter) this.begin(entry)
    }
  }
}
