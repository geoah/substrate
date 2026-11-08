import {
  SubstrateClient,
  describe,
  follow,
  recordKey,
  type SubstrateRecord,
  type WatchCursor,
  type WatchEvent,
} from "@substrate/client"

export type LiveStatus =
  "loading" | "live" | "reconnecting" | "error" | "signedOut"

export interface LiveSnapshot {
  records: ReadonlyMap<string, SubstrateRecord>
  status: LiveStatus
  /** Words for a status line: why it is reconnecting, or what failed. */
  words?: string
  revision: number
}

/** The records of a set of kinds, kept current: read whole once, then
 * followed on the change feed, each changed row read again (decision 0061:
 * a change event names the record and the client fetches it). A write the
 * app makes shows at once (`apply`) and is replaced by what the server
 * answers. The kit's LinkedRecords, for a browser. */
export class LiveCollection {
  readonly client: SubstrateClient
  readonly kinds: string[]
  private records = new Map<string, SubstrateRecord>()
  private status: LiveStatus = "loading"
  private words?: string
  private revision = 0
  private snapshot: LiveSnapshot
  private listeners = new Set<() => void>()
  private abort?: AbortController
  private inflight = new Map<string, number>()

  constructor(client: SubstrateClient, kinds: string[]) {
    this.client = client
    this.kinds = kinds
    this.snapshot = this.makeSnapshot()
  }

  subscribe = (fn: () => void): (() => void) => {
    this.listeners.add(fn)
    return () => this.listeners.delete(fn)
  }

  getSnapshot = (): LiveSnapshot => this.snapshot

  private makeSnapshot(): LiveSnapshot {
    return {
      records: new Map(this.records),
      status: this.status,
      words: this.words,
      revision: this.revision,
    }
  }

  private emit(): void {
    this.revision++
    this.snapshot = this.makeSnapshot()
    for (const fn of this.listeners) fn()
  }

  private setStatus(status: LiveStatus, words?: string): void {
    if (this.status === status && this.words === words) return
    this.status = status
    this.words = words
    this.emit()
  }

  start(): void {
    this.stop()
    this.abort = new AbortController()
    void this.load(this.abort.signal)
  }

  stop(): void {
    this.abort?.abort()
    this.abort = undefined
  }

  /** Reads every record again and follows the feed from the read's head. */
  async reload(): Promise<void> {
    this.start()
  }

  private async load(signal: AbortSignal): Promise<void> {
    try {
      const page = await this.client.listAll(
        { kinds: this.kinds, first: 500 },
        10000,
        signal
      )
      if (signal.aborted) return
      this.records = new Map(
        page.records
          .filter((r) => !r.deletedAt)
          .map((r) => [recordKey(r.kind, r.id), r])
      )
      this.status = "live"
      this.words = undefined
      this.emit()
      const from: WatchCursor | null = page.head
        ? { seq: page.head, generation: page.generation ?? "" }
        : null
      follow(
        this.client,
        this.kinds,
        from,
        (e) => this.onEvent(e, signal),
        signal
      )
    } catch (e) {
      if (signal.aborted) return
      const status = (e as { status?: number }).status
      if (status === 401)
        return this.setStatus(
          "signedOut",
          "Your access to this server ended. Sign in again."
        )
      this.setStatus("error", describe(e))
      setTimeout(() => {
        if (!signal.aborted) void this.load(signal)
      }, 5000)
    }
  }

  private onEvent(e: WatchEvent, signal: AbortSignal): void {
    switch (e.type) {
      case "opened":
        this.setStatus("live")
        break
      case "row":
        void this.refresh(e.row.kind, e.row.recordId, signal)
        if (this.status !== "live") this.setStatus("live")
        break
      case "relist":
        void this.load(signal)
        break
      case "interrupted":
        this.setStatus("reconnecting", e.words)
        break
      case "signedOut":
        this.setStatus(
          "signedOut",
          "Your access to this server ended. Sign in again."
        )
        break
      case "ended":
        this.setStatus("error", e.words)
        break
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
      if (record) this.records.set(key, record)
      else this.records.delete(key)
      this.emit()
    } catch {
      // The feed reads it again on its next change, or a relist does.
    }
  }

  get(kind: string, id: string): SubstrateRecord | undefined {
    return this.records.get(recordKey(kind, id))
  }

  all(kind: string): SubstrateRecord[] {
    return [...this.records.values()].filter((r) => r.kind === kind)
  }

  /** Shows a write before the server has it: the record as it will be, or
   * null for one deleted. Answers how to undo it. */
  apply(kind: string, id: string, next: SubstrateRecord | null): () => void {
    const key = recordKey(kind, id)
    const before = this.records.get(key)
    if (next) this.records.set(key, next)
    else this.records.delete(key)
    this.emit()
    return () => {
      if (before) this.records.set(key, before)
      else this.records.delete(key)
      this.emit()
    }
  }

  /** What the server answered for a write, unless a later version is here. */
  landed(record: SubstrateRecord): void {
    const key = recordKey(record.kind, record.id)
    const here = this.records.get(key)
    if (here && here.version > record.version) return
    if (record.deletedAt) this.records.delete(key)
    else this.records.set(key, record)
    this.emit()
  }
}
