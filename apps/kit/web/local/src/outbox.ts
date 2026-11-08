import {
  SubstrateClient,
  SubstrateError,
  describe,
  isNetworkError,
  type SubstrateRecord,
} from "@substrate/client"
import { browserConnectivity, type Connectivity } from "./connectivity"
import {
  asserted,
  emptyOutboxFile,
  entryKey,
  type LocalWrite,
  type OutboxEntry,
  type OutboxFile,
} from "./entry"
import type { RepositoryStorage } from "./storage"

/** How the outbox's last send ended. */
export type OutboxState =
  | { type: "idle" }
  /** Every write that could go landed. */
  | { type: "sent" }
  /** The server could not be reached, or was busy; nothing is lost.
   * `retryAt` is when the next try starts, where one is scheduled. */
  | { type: "offline"; words: string; retryAt?: number }
  /** The token was refused: sign in again. */
  | { type: "signedOut" }
  /** The token lacks a scope a write needs. */
  | { type: "needsApproval" }
  /** The server refused one or more writes, which wait, parked, with its
   * words until the owner retries or discards them. */
  | { type: "refused"; words: string }

/** Where the outbox stands: what a screen shows, and the queue the records
 * are folded with. */
export interface OutboxSummary {
  state: OutboxState
  entries: readonly OutboxEntry[]
  /** Every write still queued, parked ones included. */
  waiting: number
  /** The writes the server refused, waiting for the owner. */
  parked: readonly OutboxEntry[]
  /** The writes a send would try now: not parked, and not behind a parked
   * write of their batch. */
  sendable: number
  /** A send is running in this tab. */
  flushing: boolean
  /** Moved by every change the app makes to the queue, never by a send. */
  revision: number
  /** Why the last save failed, until one succeeds. */
  saveProblem?: string
  /** Whether the queue survives the page closing. */
  persistent: boolean
  /** Why it does not. */
  storageProblem?: string
}

/** The outbox could not keep a change: nothing was queued. */
export class OutboxNotSaved extends Error {
  constructor(reason: string) {
    super(`The change could not be kept in this browser: ${reason}`)
    this.name = "OutboxNotSaved"
  }
}

/** A guarded edit met a newer version on the server: another device or
 * writer changed the record since the owner began editing. */
export class EditConflict extends Error {
  readonly server: string
  constructor(server: string) {
    super(
      "It changed on your substrate since you began editing it (another device or app), so your edit was not sent."
    )
    this.name = "EditConflict"
    this.server = server
  }
}

/** A write the record has moved past: a newer delivery of it landed
 * first, and this one is stale. */
export class Overtaken extends Error {
  constructor() {
    super("A newer write of the record landed first.")
    this.name = "Overtaken"
  }
}

export type Verdict =
  /** A newer write of the record landed first: this one is stale. */
  | { type: "drop" }
  /** The run stops; nothing is lost. */
  | { type: "stop"; state: OutboxState; retryAfter?: number }
  /** Left queued, its batch held for the rest of the run. */
  | { type: "hold"; words: string }
  /** Refused for itself: parked for the owner. */
  | { type: "park"; words: string; conflict: boolean }

/** What a failed write means for the run. */
export function classify(error: unknown): Verdict {
  if (error instanceof Overtaken) return { type: "drop" }
  if (error instanceof EditConflict)
    return { type: "park", words: error.message, conflict: true }
  if (error instanceof SubstrateError) {
    if (error.status === 401)
      return { type: "stop", state: { type: "signedOut" } }
    if (error.insufficientScope)
      return { type: "stop", state: { type: "needsApproval" } }
    if (error.transient)
      return {
        type: "stop",
        state: { type: "offline", words: error.words },
        retryAfter: error.retryAfter,
      }
    if (error.conflict) return { type: "hold", words: error.words }
    return { type: "park", words: error.words, conflict: false }
  }
  if (error instanceof DOMException && error.name === "AbortError")
    return { type: "stop", state: { type: "idle" } }
  if (isNetworkError(error))
    return { type: "stop", state: { type: "offline", words: describe(error) } }
  return { type: "park", words: describe(error), conflict: false }
}

export interface OutboxOptions {
  client: SubstrateClient
  storage: RepositoryStorage
  connectivity?: Connectivity
  /** The version the device last saw a record at, which a guarded change is
   * sent on; absent, the record is read first. */
  versionOf?: (kind: string, id: string) => number | undefined
  /** The waits between sends that met no server, the last one repeated. */
  backoff?: number[]
  random?: () => number
  now?: () => number
  /** Shared by the tabs of one app and repository: one sends at a time
   * (Web Locks), and each hears the others' changes (BroadcastChannel). */
  shareName?: string
}

const defaultBackoff = [1000, 2000, 5000, 10_000, 30_000, 60_000]
/** How many of a record's landed edits the chain keeps. */
const chainLimit = 32
/** The version every record is created at. */
const createdVersion = 1

/** What a write that landed leaves: the record the server holds after it,
 * where it answered one, and the version the write itself made, which the
 * chain moves a later edit's base along. */
interface Landed {
  record?: SubstrateRecord
  made?: number
}

/** A record's `revision`, the number an observed kind rewrites under
 * (decision 0162). */
function revisionOf(properties: Record<string, unknown> | undefined): number {
  const v = properties?.revision
  if (typeof v === "number") return v
  if (typeof v === "string" && v.trim() !== "") return Number(v)
  return NaN
}

function sendable(entries: readonly OutboxEntry[]): number {
  const held = new Set<string>()
  let count = 0
  for (const e of entries) {
    if (e.batch && held.has(e.batch)) continue
    if (e.parked) {
      if (e.batch) held.add(e.batch)
      continue
    }
    count++
  }
  return count
}

/** The writes waiting for the server, kept in IndexedDB per repository so a
 * write made offline survives a reload, and sent in order when the server
 * answers (the kit's Outbox). A write is found again by its id after every
 * call, never by its position: the queue may change while one is in
 * flight, from this tab or another. */
export class Outbox {
  private readonly client: SubstrateClient
  private readonly storage: RepositoryStorage
  private readonly connectivity: Connectivity
  private readonly versionOf?: OutboxOptions["versionOf"]
  private readonly backoff: number[]
  private readonly random: () => number
  private readonly now: () => number
  private readonly shareName?: string
  private file: OutboxFile = emptyOutboxFile()
  private state: OutboxState = { type: "idle" }
  private flushing = false
  private revision = 0
  private saveProblem?: string
  /** Moved by `reset`: a send that finds it moved after a call stops. */
  private generation = 0
  private failures = 0
  private timer?: ReturnType<typeof setTimeout>
  private closed = false
  private abort = new AbortController()
  private channel?: BroadcastChannel
  /** Every read and write of the kept queue, one after another in the
   * order asked: a read answered after a later write would put back what
   * that write took out, a landed write with its old base among it. */
  private io: Promise<void> = Promise.resolve()
  private reloadQueued = false
  /** The send running in this tab, which `close` waits out. */
  private running: Promise<void> = Promise.resolve()
  private unsubscribe?: () => void
  private listeners = new Set<() => void>()
  private landedListeners = new Set<
    (entry: OutboxEntry, record: SubstrateRecord | undefined) => void
  >()
  private snapshot: OutboxSummary
  readonly ready: Promise<void>

  constructor(opts: OutboxOptions) {
    this.client = opts.client
    this.storage = opts.storage
    this.connectivity = opts.connectivity ?? browserConnectivity
    this.versionOf = opts.versionOf
    this.backoff = opts.backoff ?? defaultBackoff
    this.random = opts.random ?? Math.random
    this.now = opts.now ?? Date.now
    this.shareName = opts.shareName
    this.snapshot = this.summarize()
    this.ready = this.open()
  }

  private async open(): Promise<void> {
    await this.serial(async () => {
      try {
        this.file = await this.storage.readOutbox()
      } catch (e) {
        this.saveProblem = describe(e)
      }
    })
    if (this.closed) return
    this.unsubscribe = this.connectivity.subscribe((event) => {
      if (event === "offline") {
        if (sendable(this.file.entries))
          this.setState({ type: "offline", words: "You're offline." })
        return
      }
      if (event === "online") this.failures = 0
      if (sendable(this.file.entries)) void this.flush()
    })
    if (this.shareName && typeof BroadcastChannel !== "undefined") {
      this.channel = new BroadcastChannel(this.shareName)
      this.channel.onmessage = () => this.reload()
    }
    this.emit()
    if (sendable(this.file.entries)) this.soon()
  }

  /** Stops sending and listening, and resolves once the send in progress
   * and the queue's last read or write are done, so the storage can close
   * under it. What waits stays kept. */
  async close(): Promise<void> {
    this.closed = true
    this.abort.abort()
    clearTimeout(this.timer)
    this.unsubscribe?.()
    this.channel?.close()
    await this.ready
    await this.running
    await this.io
  }

  private serial<T>(fn: () => Promise<T>): Promise<T> {
    const next = this.io.then(fn)
    this.io = next.then(
      () => undefined,
      () => undefined
    )
    return next
  }

  // ---- following --------------------------------------------------------

  subscribe = (fn: () => void): (() => void) => {
    this.listeners.add(fn)
    return () => this.listeners.delete(fn)
  }

  getSnapshot = (): OutboxSummary => this.snapshot

  /** Called with each write the server took, and the record it answered
   * (none for a delete), before the write leaves the queue: a reader lays
   * the answer over its copy first, so the record never shows undone in
   * between. */
  onLanded(
    fn: (entry: OutboxEntry, record: SubstrateRecord | undefined) => void
  ): () => void {
    this.landedListeners.add(fn)
    return () => this.landedListeners.delete(fn)
  }

  private summarize(): OutboxSummary {
    const entries = this.file.entries
    return {
      state: this.state,
      entries,
      waiting: entries.length,
      parked: entries.filter((e) => e.parked),
      sendable: sendable(entries),
      flushing: this.flushing,
      revision: this.revision,
      saveProblem: this.saveProblem,
      persistent: this.storage.persistent,
      storageProblem: this.storage.problem,
    }
  }

  private emit(): void {
    this.snapshot = this.summarize()
    for (const fn of this.listeners) fn()
  }

  private setState(state: OutboxState): void {
    this.state = state
    this.emit()
  }

  /** Reads the queue again: another tab changed it. */
  private reload(): void {
    if (this.reloadQueued) return
    this.reloadQueued = true
    void this.serial(async () => {
      this.reloadQueued = false
      if (this.closed) return
      try {
        this.file = await this.storage.readOutbox()
        this.emit()
      } catch {
        // the next change reads it again
      }
    })
  }

  // ---- queueing ---------------------------------------------------------

  /** Queues what one owner's action writes, kept before it returns: on a
   * failed save nothing is queued and `OutboxNotSaved` says why. */
  async write(write: LocalWrite): Promise<void> {
    if (!write.entries.length) return
    await this.ready
    await this.change((file) => {
      if (write.replacing)
        file.entries = file.entries.filter((e) => e.batch !== write.replacing)
      file.entries.push(...write.entries)
    })
    this.soon()
  }

  /** Sends a parked write again. */
  async retry(id: string): Promise<void> {
    await this.change((file) => {
      const e = file.entries.find((x) => x.id === id)
      if (e) e.parked = false
    })
    this.soon()
  }

  /** Gives up a parked write and every write of its batch still waiting. */
  async discard(id: string): Promise<void> {
    await this.change((file) => {
      const e = file.entries.find((x) => x.id === id)
      if (!e) return
      file.entries = file.entries.filter((x) =>
        e.batch ? x.batch !== e.batch : x.id !== id
      )
    })
    this.soon()
  }

  /** The owner's answer to a conflicting edit: `keepMine` sends it again
   * without a version, so the properties it changed replace the server's
   * and the rest stay as the other writer left them; otherwise it is
   * dropped, with its batch, and the server's record stands. */
  async resolve(id: string, keepMine: boolean): Promise<void> {
    if (!keepMine) return this.discard(id)
    await this.change((file) => {
      const e = file.entries.find((x) => x.id === id)
      if (!e) return
      e.baseVersion = undefined
      e.guarded = false
      e.conflict = false
      e.parked = false
      e.lastError = undefined
    })
    this.soon()
  }

  /** Forgets every write: the session ended and its repository's data goes
   * with it. A send in progress stops at its next step without writing. */
  async reset(): Promise<void> {
    this.generation++
    this.abort.abort()
    this.abort = new AbortController()
    clearTimeout(this.timer)
    this.state = { type: "idle" }
    await this.change((file) => {
      file.entries = []
      file.chains = {}
    })
  }

  /** A change the app asked for: saved before it returns. */
  private async change(edit: (file: OutboxFile) => void): Promise<void> {
    await this.serial(async () => {
      try {
        this.file = await this.storage.updateOutbox(edit)
        this.saveProblem = undefined
      } catch (e) {
        this.saveProblem = describe(e)
        this.emit()
        throw new OutboxNotSaved(this.saveProblem)
      }
      this.revision++
      this.emit()
    })
    this.channel?.postMessage("changed")
  }

  /** A change a send makes after the server answered: kept in memory
   * whatever storage says, since the server holds it now. At worst a write
   * is sent again after a reload, which every write here survives. */
  private async commit(edit: (file: OutboxFile) => void): Promise<void> {
    await this.serial(async () => {
      try {
        this.file = await this.storage.updateOutbox(edit)
        this.saveProblem = undefined
      } catch (e) {
        const file = structuredClone(this.file)
        edit(file)
        this.file = file
        this.saveProblem = describe(e)
      }
      this.emit()
    })
    this.channel?.postMessage("changed")
  }

  // ---- sending ----------------------------------------------------------

  /** Sends at once, the backoff forgotten: the owner's Send Now. */
  sendNow(): Promise<OutboxState> {
    this.failures = 0
    return this.flush()
  }

  private soon(): void {
    if (this.closed) return
    clearTimeout(this.timer)
    this.timer = setTimeout(() => void this.flush(), 0)
  }

  /** The next try after a send that met no server, or a busy one: the
   * backoff's next step with a fifth of jitter, or longer where the server
   * asked for it. */
  private later(retryAfter?: number): number {
    clearTimeout(this.timer)
    const step = this.backoff[Math.min(this.failures, this.backoff.length - 1)]
    this.failures++
    const jittered = step * (1 + 0.2 * (this.random() * 2 - 1))
    const wait = Math.max((retryAfter ?? 0) * 1000, jittered)
    if (!this.closed) this.timer = setTimeout(() => void this.flush(), wait)
    return this.now() + wait
  }

  /** Sends what waits, in order. A write the server refuses for itself is
   * parked with its words and holds back the rest of its batch; the
   * network, a busy server or the token stop the run, and nothing is lost.
   * Writes queued while it runs are sent after it. */
  async flush(): Promise<OutboxState> {
    await this.ready
    if (this.flushing || this.closed) return this.state
    clearTimeout(this.timer)
    if (!sendable(this.file.entries)) {
      if (this.state.type === "offline") this.setState({ type: "sent" })
      return this.state
    }
    if (!this.connectivity.online()) {
      this.setState({ type: "offline", words: "You're offline." })
      return this.state
    }
    const revision = this.revision
    this.flushing = true
    this.emit()
    let settle!: () => void
    this.running = new Promise((resolve) => (settle = resolve))
    try {
      const locks =
        typeof navigator !== "undefined" ? navigator.locks : undefined
      if (this.shareName && locks) {
        const ran = await locks.request(
          this.shareName,
          { ifAvailable: true },
          async (lock) => {
            if (!lock) return false
            await this.run()
            return true
          }
        )
        // Another tab is sending this queue; look again in a moment.
        if (!ran) this.timer = setTimeout(() => void this.flush(), 3000)
      } else {
        await this.run()
      }
    } finally {
      this.flushing = false
      settle()
      this.emit()
    }
    // Writes queued during the run go now, unless the run stopped for the
    // network or the token, whose own trigger sends them.
    const ran = this.state.type === "sent" || this.state.type === "refused"
    if (this.revision !== revision && ran) this.soon()
    return this.state
  }

  private async run(): Promise<void> {
    // The queue as kept: another tab may have changed it.
    await this.serial(async () => {
      try {
        this.file = await this.storage.readOutbox()
      } catch {
        // the copy in memory stands
      }
    })
    const run = this.generation
    const tried = new Set<string>()
    let refusal: string | undefined
    let held = false
    for (let entry = this.next(tried); entry; entry = this.next(tried)) {
      tried.add(entry.id)
      const signal = this.abort.signal
      let landed: Landed = {}
      let failure: unknown
      let failed = false
      try {
        landed = await this.send(entry, signal)
      } catch (e) {
        failed = true
        failure = await this.overtaken(entry, e, signal).then(
          (stale) => (stale ? new Overtaken() : e),
          (readFailed: unknown) => readFailed
        )
      }
      if (run !== this.generation || this.closed) return
      const id = entry.id
      const key = entryKey(entry)
      if (!failed) {
        this.failures = 0
        const { record, made } = landed
        for (const fn of this.landedListeners) fn(entry, record)
        await this.commit((file) => {
          if (entry.method === "DELETE") {
            delete file.chains[key]
          } else if (made !== undefined && entry.baseVersion !== undefined) {
            // A create asserted its 0 itself; an edit, the end of the chain.
            const from =
              entry.method === "PUT"
                ? entry.baseVersion
                : asserted(file, key, entry.baseVersion)
            const chain = (file.chains[key] ??= {})
            chain[String(from)] = made
            const keys = Object.keys(chain)
            for (const k of keys.slice(
              0,
              Math.max(0, keys.length - chainLimit)
            ))
              delete chain[k]
          }
          file.entries = file.entries.filter((e) => e.id !== id)
        })
        continue
      }
      const verdict = classify(failure)
      switch (verdict.type) {
        case "drop":
          await this.commit((file) => {
            file.entries = file.entries.filter((e) => e.id !== id)
          })
          break
        case "stop": {
          const state = verdict.state
          if (state.type === "offline") {
            this.setState({ ...state, retryAt: this.later(verdict.retryAfter) })
          } else {
            this.setState(state)
          }
          return
        }
        case "hold":
          held = true
          await this.commit((file) => {
            const e = file.entries.find((x) => x.id === id)
            if (!e) return
            e.attempts++
            e.lastError = verdict.words
          })
          break
        case "park":
          refusal = verdict.words
          await this.commit((file) => {
            const e = file.entries.find((x) => x.id === id)
            if (!e) return
            e.attempts++
            e.lastError = verdict.words
            e.parked = true
            e.conflict = verdict.conflict
          })
          break
      }
    }
    refusal ??= this.file.entries.find((e) => e.parked)?.lastError
    if (refusal === undefined && this.file.entries.some((e) => e.parked))
      refusal = ""
    this.setState(
      refusal !== undefined
        ? { type: "refused", words: refusal }
        : { type: "sent" }
    )
    // A write moved again since its read waits for the next run.
    if (held) this.later()
  }

  /** The first entry not tried in this run whose batch nothing parked or
   * earlier holds back. */
  private next(tried: Set<string>): OutboxEntry | undefined {
    const held = new Set<string>()
    for (const e of this.file.entries) {
      if (e.batch && held.has(e.batch)) continue
      if (e.parked || tried.has(e.id)) {
        if (e.batch) held.add(e.batch)
        continue
      }
      return e
    }
    return undefined
  }

  /** One write, answering what it left on the server. */
  private async send(entry: OutboxEntry, signal: AbortSignal): Promise<Landed> {
    const { kind, recordID: id } = entry
    const props = entry.properties ?? {}
    const conflict = (e: unknown) => e instanceof SubstrateError && e.conflict
    const made = (record: SubstrateRecord): Landed => ({
      record,
      made: record.version,
    })
    switch (entry.method) {
      case "PUT":
        if (entry.baseVersion === undefined)
          return made(await this.client.put(kind, id, props, undefined, signal))
        try {
          return made(
            await this.client.put(kind, id, props, entry.baseVersion, signal)
          )
        } catch (e) {
          if (!conflict(e)) throw e
          // The id was minted here, so the row there is this create's
          // earlier try, its answer lost: the create landed. Sent again,
          // its properties would land over whatever another device wrote
          // since, so the row is read instead. The create made the version
          // every record is created at, so an edit queued on it is sent on
          // that one and parks if another device moved the record since.
          const record = await this.client.record(kind, id, signal)
          return { record: record ?? undefined, made: createdVersion }
        }
      case "PATCH":
        if (entry.baseVersion !== undefined) {
          const version = asserted(
            this.file,
            entryKey(entry),
            entry.baseVersion
          )
          try {
            return made(
              await this.client.patch(kind, id, props, version, signal)
            )
          } catch (e) {
            if (conflict(e)) throw new EditConflict((e as SubstrateError).words)
            throw e
          }
        }
        if (!entry.guarded)
          return made(
            await this.client.patch(kind, id, props, undefined, signal)
          )
        try {
          const version =
            this.versionOf?.(kind, id) ??
            (await this.readVersion(entry, signal))
          return made(await this.client.patch(kind, id, props, version, signal))
        } catch (e) {
          if (!conflict(e)) throw e
          // Somebody wrote it since: read it, and send the change again on
          // top, once.
          const version = await this.readVersion(entry, signal)
          return made(await this.client.patch(kind, id, props, version, signal))
        }
      case "DELETE":
        await this.client.delete(kind, id, signal)
        return {}
    }
  }

  /** Whether a refusal is the record holding a later revision than the
   * write carries: a newer delivery of an observation landed first (decision
   * 0162). The envelope says only `validation`, so the stored revision is
   * read and compared, never the server's words. */
  private async overtaken(
    entry: OutboxEntry,
    error: unknown,
    signal: AbortSignal
  ): Promise<boolean> {
    if (!(error instanceof SubstrateError) || error.code !== "validation")
      return false
    if (!error.problemDetails.some((d) => d.path === "props.revision"))
      return false
    const mine = revisionOf(entry.properties)
    if (!Number.isFinite(mine)) return false
    const stored = await this.client.record(entry.kind, entry.recordID, signal)
    return revisionOf(stored?.properties) > mine
  }

  private async readVersion(
    entry: OutboxEntry,
    signal: AbortSignal
  ): Promise<number> {
    const record = await this.client.record(entry.kind, entry.recordID, signal)
    if (!record) {
      // A change of nothing is refused, and parked for the owner.
      throw new SubstrateError(
        "not_found",
        `${entry.kind}/${entry.recordID} is not on your substrate any more.`,
        404
      )
    }
    return record.version
  }
}
