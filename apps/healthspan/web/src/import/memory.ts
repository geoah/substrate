// Records in this page's memory, held as the server holds them: versions,
// `ifVersion` (a deleted record keeps its version, so a create onto it is
// refused and a PUT held to it restores it), deletes plain or guarded, and
// blobs by digest. The demo's imports run through it with the same runner
// the owner's do, and change nothing but the page.

import { SubstrateError, type SubstrateRecord } from "@substrate/client"
import { refPath } from "./records"
import type { RunnerStore } from "./runner"

export class MemoryStore implements RunnerStore {
  private readonly rows = new Map<string, SubstrateRecord>()
  private readonly blobs = new Map<string, Uint8Array>()
  private readonly listeners = new Set<() => void>()
  private revision = 0
  private clock: number

  constructor(start = Date.now()) {
    this.clock = start
  }

  /** A record as another writer left it, before the page opened. */
  seed(record: SubstrateRecord): void {
    this.rows.set(`${record.kind}/${record.id}`, record)
    this.changed()
  }

  /** Every live record of a kind. */
  all(kind: string): SubstrateRecord[] {
    return [...this.rows.values()].filter(
      (r) => r.kind === kind && !r.deletedAt
    )
  }

  /** Moves whenever a record changes: what a hook's snapshot keys on. */
  get version(): number {
    return this.revision
  }

  subscribe = (fn: () => void): (() => void) => {
    this.listeners.add(fn)
    return () => this.listeners.delete(fn)
  }

  private changed() {
    this.revision++
    for (const fn of this.listeners) fn()
  }

  private stamp(): string {
    this.clock = Math.max(this.clock + 1, Date.now())
    return new Date(this.clock).toISOString()
  }

  private refuse(status: number, code: string, message: string): never {
    throw new SubstrateError(code, message, status)
  }

  private async settle(signal?: AbortSignal) {
    await Promise.resolve()
    if (signal?.aborted) throw new DOMException("aborted", "AbortError")
  }

  async record(kind: string, id: string, signal?: AbortSignal) {
    await this.settle(signal)
    const r = this.rows.get(`${kind}/${id}`)
    return r && !r.deletedAt ? r : null
  }

  async tombstone(kind: string, id: string, signal?: AbortSignal) {
    await this.settle(signal)
    const r = this.rows.get(`${kind}/${id}`)
    return r?.deletedAt ? r : null
  }

  /** A blob stored as another writer left it, before the page opened. */
  seedBlob(digest: string, bytes: Uint8Array): void {
    this.blobs.set(digest, bytes)
    this.changed()
  }

  async putBlob(
    digest: string,
    bytes: Uint8Array,
    _mediaType: string,
    _name: string,
    signal?: AbortSignal
  ) {
    await this.settle(signal)
    this.blobs.set(digest, bytes)
  }

  async blob(digest: string, signal?: AbortSignal) {
    await this.settle(signal)
    const b = this.blobs.get(digest)
    if (!b) this.refuse(404, "not_found", `blob ${digest} not found`)
    return b
  }

  private write(
    kind: string,
    id: string,
    properties: Record<string, unknown>,
    ifVersion: number | undefined,
    patch: boolean
  ): SubstrateRecord {
    const key = `${kind}/${id}`
    const here = this.rows.get(key)
    const live = here && !here.deletedAt ? here : undefined
    if (patch && !live) this.refuse(404, "not_found", `${key} not found`)
    if (ifVersion !== undefined && ifVersion !== (here?.version ?? 0))
      this.refuse(409, "conflict", `${key} is at version ${here?.version ?? 0}`)
    const now = this.stamp()
    // A put onto a tombstone restores the row: what it kept, merged.
    const next: SubstrateRecord = {
      id,
      kind,
      properties: { ...(here?.properties ?? {}) },
      version: (here?.version ?? 0) + 1,
      createdAt: here?.createdAt ?? now,
      updatedAt: now,
    }
    for (const [k, v] of Object.entries(properties)) {
      if (v === null) delete next.properties[k]
      else next.properties[k] = v
    }
    this.rows.set(key, next)
    this.changed()
    return next
  }

  async put(
    kind: string,
    id: string,
    properties: Record<string, unknown>,
    ifVersion?: number,
    signal?: AbortSignal
  ) {
    await this.settle(signal)
    return this.write(kind, id, properties, ifVersion, false)
  }

  async patch(
    kind: string,
    id: string,
    properties: Record<string, unknown>,
    ifVersion?: number,
    signal?: AbortSignal
  ) {
    await this.settle(signal)
    return this.write(kind, id, properties, ifVersion, true)
  }

  async delete(
    kind: string,
    id: string,
    ifVersion?: number,
    signal?: AbortSignal
  ) {
    await this.settle(signal)
    const key = `${kind}/${id}`
    const r = this.rows.get(key)
    if (!r || r.deletedAt) return
    if (ifVersion !== undefined && ifVersion !== r.version)
      this.refuse(409, "conflict", `${key} is at version ${r.version}`)
    this.rows.set(key, {
      ...r,
      version: r.version + 1,
      deletedAt: this.stamp(),
    })
    this.changed()
  }

  async pointingAt(kinds: string[], path: string, signal?: AbortSignal) {
    await this.settle(signal)
    return [...this.rows.values()]
      .filter(
        (r) =>
          !r.deletedAt &&
          kinds.includes(r.kind) &&
          refPath(r.properties.origin) === path
      )
      .map((r) => ({ kind: r.kind, id: r.id }))
  }

  async naming(
    kind: string,
    property: string,
    path: string,
    signal?: AbortSignal
  ) {
    await this.settle(signal)
    return [...this.rows.values()].filter(
      (r) =>
        !r.deletedAt &&
        r.kind === kind &&
        refPath(r.properties[property]) === path
    )
  }
}
