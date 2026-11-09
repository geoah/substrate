// What the runner's tests share: time that moves only when the runner
// waits, and the kit's fake substrate taught what the runner leans on and
// the fake leaves out (the tests' own wrapper, not the kit's): blobs by
// digest with the collector, which takes a blob no live record names a
// minute after its first store; a write naming a blob no longer stored
// refused as the server refuses it; tombstones, onto which a create is
// refused and a PUT held to their version restores; a delete held to
// `?ifVersion=`; and the list of tombstones by id. For tests only.

import { SubstrateClient, type SubstrateRecord } from "@substrate/client"
import { FakeSubstrate } from "@substrate/testing"
import { vi } from "vitest"
import { KIND } from "../kinds"
import type { StagingKeep } from "./keep"
import { memoryKeep } from "./keep"
import { blobDigestOf } from "./planfile"
import { refPath } from "./records"
import {
  discardImport,
  finishStaging,
  resumeImport,
  stageImport,
  undoImport,
  type Clock,
  type RunOptions,
  type RunProgress,
  type StageRequest,
} from "./runner"
import { clientStore } from "./store"

export const ORIGIN = "https://substrate.example.com"

/** Time that moves only when the runner waits, or a test moves it: the
 * lease's tests run at once. */
export class VirtualClock implements Clock {
  t = Date.parse("2026-11-01T10:00:00Z")
  private timers: { at: number; fire: () => void }[] = []
  now = () => this.t
  advance(ms: number) {
    this.t += ms
    for (const timer of this.timers.filter((x) => x.at <= this.t)) {
      this.timers.splice(this.timers.indexOf(timer), 1)
      timer.fire()
    }
  }
  sleep = (ms: number) => {
    this.advance(Math.max(0, ms))
    return Promise.resolve()
  }
  deadline = (at: number) => {
    const c = new AbortController()
    const timer = { at, fire: () => c.abort() }
    if (at <= this.t) c.abort()
    else this.timers.push(timer)
    return {
      signal: c.signal,
      clear: () => {
        const i = this.timers.indexOf(timer)
        if (i >= 0) this.timers.splice(i, 1)
      },
    }
  }
}

export const urlOf = (input: string | URL | Request) =>
  typeof input === "string"
    ? input
    : input instanceof URL
      ? input.href
      : input.url

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  })

const refused = (status: number, code: string, message: string) =>
  json(status, { error: { code, message } })

const DIGEST = /blob-sha256-[0-9a-f]{64}/g

/** Every blob digest a value names, anywhere in it. */
function digestsIn(v: unknown, out: Set<string> = new Set()): Set<string> {
  if (typeof v === "string") for (const m of v.match(DIGEST) ?? []) out.add(m)
  else if (Array.isArray(v)) for (const x of v) digestsIn(x, out)
  else if (v && typeof v === "object")
    for (const x of Object.values(v)) digestsIn(x, out)
  return out
}

/** The fake substrate with blobs, the collector and tombstones. */
export class Server {
  readonly fake = new FakeSubstrate(ORIGIN)
  /** Stored blobs, with when each was first stored. */
  readonly blobs = new Map<string, { bytes: Uint8Array; at: number }>()
  /** Every blob PUT, in order. */
  readonly blobPuts: string[] = []
  /** The blobs the collector took. */
  readonly collected: string[] = []
  /** Every request the server answered, the wrapper's own included. */
  readonly seen: {
    method: string
    path: string
    query: URLSearchParams
    body?: unknown
  }[] = []

  constructor(readonly clock: VirtualClock) {}

  /** The collector's pass: a blob no live record names, a minute or more
   * after its first store, is taken. */
  collect(): void {
    const named = new Set<string>()
    for (const r of this.fake.records.values())
      if (!r.deletedAt) digestsIn(r.properties, named)
    for (const [d, b] of this.blobs)
      if (!named.has(d) && this.clock.now() - b.at >= 60_000) {
        this.blobs.delete(d)
        this.collected.push(d)
      }
  }

  /** The record collector's pass: every live result whose `report` names
   * a tombstoned report is tombstoned with it (`onDelete: cascade`), and
   * every tombstone purged. */
  sweep(): void {
    const records = this.fake.records
    const gone = new Set(
      [...records.values()]
        .filter((r) => r.deletedAt && r.kind === KIND.labreport)
        .map((r) => `${r.kind}/${r.id}`)
    )
    for (const [key, r] of records)
      if (
        !r.deletedAt &&
        r.kind === KIND.labresult &&
        gone.has(refPath(r.properties.report) ?? "")
      )
        records.set(key, {
          ...r,
          version: r.version + 1,
          deletedAt: new Date(this.clock.now()).toISOString(),
        })
    for (const [key, r] of records) if (r.deletedAt) records.delete(key)
  }

  /** A live record, as the fake holds it. */
  live(kind: string, id: string): SubstrateRecord | undefined {
    return this.fake.live(kind, id)
  }

  /** The fetch a test installs. */
  fetch = async (
    input: string | URL | Request,
    init: RequestInit = {}
  ): Promise<Response> => {
    this.collect()
    const url = new URL(urlOf(input))
    const method = (init.method ?? "GET").toUpperCase()
    let parsed: unknown
    if (typeof init.body === "string") {
      try {
        parsed = JSON.parse(init.body)
      } catch {
        parsed = init.body
      }
    }
    this.seen.push({
      method,
      path: url.pathname,
      query: url.searchParams,
      body: parsed,
    })
    if (init.signal?.aborted) throw new DOMException("aborted", "AbortError")
    const blob = /^\/api\/v1\/blobs\/([^/]+)$/.exec(url.pathname)
    if (blob) {
      await Promise.resolve()
      const digest = decodeURIComponent(blob[1])
      if (method === "PUT") {
        const bytes = new Uint8Array(init.body as Uint8Array)
        if (blobDigestOf(bytes) !== digest)
          return refused(400, "bad_request", "the bytes hash to another digest")
        this.blobPuts.push(digest)
        if (!this.blobs.has(digest))
          this.blobs.set(digest, { bytes, at: this.clock.now() })
        return json(201, { digest, size: bytes.length })
      }
      const b = this.blobs.get(digest)
      return b
        ? new Response(b.bytes as Uint8Array<ArrayBuffer>, { status: 200 })
        : refused(404, "not_found", `blob ${digest} not found`)
    }
    if (url.pathname === "/api/v1/records" && method === "GET") {
      const filter = JSON.parse(url.searchParams.get("filter") ?? "{}") as {
        kinds?: string[]
        ids?: string[]
        deleted?: boolean
      }
      if (filter.deleted) {
        await Promise.resolve()
        const records = [...this.fake.records.values()].filter(
          (r) =>
            r.deletedAt &&
            (filter.kinds ?? []).includes(r.kind) &&
            (!filter.ids || filter.ids.includes(r.id))
        )
        return json(200, { records })
      }
    }
    const m = /^\/api\/v1\/([^/]+\/[^/]+\/[^/]+)\/([^/]+)$/.exec(url.pathname)
    if (m) {
      const kind = m[1]
      const id = decodeURIComponent(m[2])
      const here = this.fake.records.get(`${kind}/${id}`)
      if (method === "DELETE") {
        const at = url.searchParams.get("ifVersion")
        if (
          at !== null &&
          here &&
          !here.deletedAt &&
          Number(at) !== here.version
        )
          return refused(409, "conflict", `at version ${here.version}`)
      }
      if (
        (method === "PUT" || method === "PATCH") &&
        typeof init.body === "string"
      ) {
        const body = JSON.parse(init.body) as {
          properties?: Record<string, unknown>
          ifVersion?: number
        }
        for (const d of digestsIn(body.properties))
          if (!this.blobs.has(d))
            return refused(
              422,
              "validation",
              `validation: props: blob ${d} is unknown — store it before referencing it`
            )
        if (method === "PUT" && here?.deletedAt) {
          // A restore: the body merged onto what the tombstone kept, a
          // null deleting, as the server's put does.
          await Promise.resolve()
          if (body.ifVersion !== here.version)
            return refused(409, "conflict", `at version ${here.version}`)
          const props = { ...here.properties }
          for (const [k, v] of Object.entries(body.properties ?? {}))
            if (v === null) delete props[k]
            else props[k] = v
          return json(200, this.fake.seed(kind, id, props))
        }
      }
    }
    return this.fake.fetch(input, init)
  }

  /** Every live record of a kind. */
  all(kind: string): SubstrateRecord[] {
    return [...this.fake.records.values()].filter(
      (r) => r.kind === kind && !r.deletedAt
    )
  }

  /** The import record, as the fake holds it. */
  importRecord(id: string): SubstrateRecord | undefined {
    return this.fake.records.get(`${KIND.import}/${id}`)
  }
}

/** One browser: its own client over the server, its own device id and
 * keep. Each call installs its fetch before it runs. */
export function browser(
  server: Server,
  device: string,
  opts: { fetch?: typeof fetch; keep?: StagingKeep; labName?: string } = {}
) {
  const client = new SubstrateClient(ORIGIN, "tok")
  const store = clientStore(client)
  const progress: RunProgress[] = []
  const keep = opts.keep ?? memoryKeep()
  const base = (): RunOptions => ({
    store,
    device,
    clock: server.clock,
    keep,
    labName: opts.labName,
    onProgress: (p) => progress.push(p),
  })
  const install = (f?: typeof fetch) =>
    vi.stubGlobal("fetch", f ?? opts.fetch ?? server.fetch)
  return {
    client,
    store,
    keep,
    progress,
    install,
    stage: (req: StageRequest, o: Partial<RunOptions> = {}) => {
      install()
      return stageImport(req, { ...base(), ...o })
    },
    finish: (id: string, o: Partial<RunOptions> = {}) => {
      install()
      return finishStaging(id, { ...base(), ...o })
    },
    resume: (id: string, o: Partial<RunOptions> = {}) => {
      install()
      return resumeImport(id, { ...base(), ...o })
    },
    undo: (id: string, o: Partial<RunOptions> = {}) => {
      install()
      return undoImport(id, { ...base(), ...o })
    },
    discard: (id: string, o: Partial<RunOptions> = {}) => {
      install()
      return discardImport(id, { ...base(), ...o })
    },
  }
}
