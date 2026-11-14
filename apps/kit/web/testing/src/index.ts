import type {
  AuthorizationCheckResult,
  SubstrateRecord,
} from "@substrate/client"

/** One request the fake answered, as a test reads it back. */
export interface SeenRequest {
  method: string
  path: string
  query: URLSearchParams
  body?: unknown
  headers: Headers
}

/** A refusal the fake answers instead of the next matching request. */
interface Failure {
  method?: string
  path?: RegExp
  status: number
  body?: unknown
  headers?: Record<string, string>
  times: number
}

interface Row {
  seq: number
  kind: string
  recordId: string
  op: string
}

const json = (
  status: number,
  body: unknown,
  headers: Record<string, string> = {}
) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", ...headers },
  })

const refused = (status: number, code: string, message: string) =>
  json(status, { error: { code, message } })

/** A substrate in memory behind `fetch`: records with versions, a write's
 * `ifVersion` held as the server holds it, a plain delete, the list read
 * (without a head, as an app token's is) a page at a time, the change
 * feed's ndjson watch, which stays open and carries each later write, the
 * sign-in's code exchange, and token revocation, after which the token is
 * refused, as a request with none is. `offline` makes every request fail as `fetch` does without a
 * network. */
export class FakeSubstrate {
  readonly origin: string
  readonly records = new Map<string, SubstrateRecord>()
  readonly requests: SeenRequest[] = []
  /** The tokens revoked through `/oauth/revoke`. */
  readonly revoked = new Set<string>()
  /** The tokens `/oauth/token` mints next, in order; then `tok-<n>`. */
  readonly mint: string[] = []
  /** The repository a minted token is for. */
  repository = "ada.example.org"
  private minted = 0
  offline = false
  generation = "g1"
  checkResult: AuthorizationCheckResult = { scopes: { missing: [] }, setup: [] }
  private rows: Row[] = []
  private seq = 0
  private failures: Failure[] = []
  private refusedCursors = 0
  private watchers = new Set<{
    kinds: string[]
    push: (line: string) => void
    end: () => void
  }>()
  private clock = Date.parse("2026-11-01T10:00:00Z")

  constructor(origin = "https://substrate.example.com") {
    this.origin = origin
  }

  /** Answers the next `times` matching requests with this refusal. */
  fail(
    match: { method?: string; path?: RegExp },
    status: number,
    body: unknown = {
      error: { code: "unavailable", message: "Try again soon." },
    },
    headers: Record<string, string> = {},
    times = 1
  ): void {
    this.failures.push({ ...match, status, body, headers, times })
  }

  /** Refuses the next `times` list continuations (a list with `after`)
   * with `410 compacted`, as a server does that can no longer resume a
   * cursor: a restore, or an upgrade that changed the order it was cut in.
   * Without a head, as an app token's refusal is. */
  refuseCursors(times = 1): void {
    this.refusedCursors += times
  }

  /** A record written by someone else, as the server would hold it. */
  seed(
    kind: string,
    id: string,
    properties: Record<string, unknown>
  ): SubstrateRecord {
    return this.write(kind, id, properties, "put", true)
  }

  /** A change made on another device: merged, its version moved, its row on
   * the feed. */
  change(
    kind: string,
    id: string,
    properties: Record<string, unknown>
  ): SubstrateRecord {
    return this.write(kind, id, properties, "patch", false)
  }

  get head(): number {
    return this.seq
  }

  /** The history moves on (a restore, a rebuild): every cursor is stale. */
  compact(): void {
    this.generation = `g${Number(this.generation.slice(1)) + 1}`
  }

  live(kind: string, id: string): SubstrateRecord | undefined {
    const r = this.records.get(`${kind}/${id}`)
    return r && !r.deletedAt ? r : undefined
  }

  private now(): string {
    this.clock += 1000
    return new Date(this.clock).toISOString()
  }

  private write(
    kind: string,
    id: string,
    properties: Record<string, unknown>,
    op: string,
    replace: boolean
  ): SubstrateRecord {
    const key = `${kind}/${id}`
    const here = this.records.get(key)
    const live = here && !here.deletedAt ? here : undefined
    const next: SubstrateRecord = {
      id,
      kind,
      properties: replace || !live ? {} : { ...live.properties },
      version: (here?.version ?? 0) + 1,
      createdAt: live?.createdAt ?? this.now(),
      updatedAt: this.now(),
    }
    for (const [k, v] of Object.entries(properties)) {
      if (v === null) delete next.properties[k]
      else next.properties[k] = v
    }
    this.records.set(key, next)
    this.log(kind, id, op)
    return next
  }

  private log(kind: string, recordId: string, op: string): void {
    const row = { seq: ++this.seq, kind, recordId, op }
    this.rows.push(row)
    for (const w of this.watchers)
      if (w.kinds.includes(kind)) w.push(this.rowLine(row))
  }

  private rowLine(row: Row): string {
    return (
      JSON.stringify({ ...row, ts: new Date(this.clock).toISOString() }) + "\n"
    )
  }

  /** The `fetch` a test installs. */
  fetch = async (
    input: string | URL | Request,
    init: RequestInit = {}
  ): Promise<Response> => {
    const url = new URL(
      typeof input === "string"
        ? input
        : input instanceof Request
          ? input.url
          : input.toString()
    )
    const method = (init.method ?? "GET").toUpperCase()
    const signal = init.signal ?? undefined
    if (signal?.aborted) throw new DOMException("aborted", "AbortError")
    if (this.offline) throw new TypeError("Failed to fetch")
    // A real request leaves the page before it is answered.
    await Promise.resolve()
    let body: unknown
    if (typeof init.body === "string") {
      try {
        body = JSON.parse(init.body)
      } catch {
        body = init.body
      }
    } else if (init.body instanceof URLSearchParams) {
      body = Object.fromEntries(init.body)
    }
    const path = url.pathname
    this.requests.push({
      method,
      path,
      query: url.searchParams,
      body,
      headers: new Headers(init.headers),
    })
    const failure = this.failures.find(
      (f) =>
        f.times > 0 &&
        (!f.method || f.method === method) &&
        (!f.path || f.path.test(path))
    )
    if (failure) {
      failure.times--
      return json(failure.status, failure.body, failure.headers)
    }
    if (path === "/oauth/token" && method === "POST") {
      const token = this.mint.shift() ?? `tok-${++this.minted}`
      return json(200, {
        access_token: token,
        token_type: "Bearer",
        repository: this.repository,
      })
    }
    if (path === "/oauth/revoke" && method === "POST") {
      const token = (body as { token?: string } | undefined)?.token
      if (token) this.revoked.add(token)
      return json(200, {})
    }
    const bearer = new Headers(init.headers)
      .get("Authorization")
      ?.replace(/^Bearer /, "")
    if (bearer && this.revoked.has(bearer))
      return refused(401, "auth", "the token was revoked")
    if (!bearer && path.startsWith("/api/"))
      return refused(401, "auth", "no token")
    if (path === "/api/v1/changes") return this.watch(url, signal)
    if (path === "/api/v1/records" && method === "GET") return this.list(url)
    if (path === "/api/v1/authorizations/check" && method === "POST")
      return json(200, this.checkResult)
    const m = /^\/api\/v1\/([^/]+\/[^/]+\/[^/]+)\/([^/]+)$/.exec(path)
    if (m) return this.record(method, m[1], decodeURIComponent(m[2]), body)
    return refused(404, "not_found", `no route ${method} ${path}`)
  }

  private list(url: URL): Response {
    if (url.searchParams.get("after") && this.refusedCursors > 0) {
      this.refusedCursors--
      return refused(
        410,
        "compacted",
        "the cursor was minted by another version of the server's paging; list again from the first page"
      )
    }
    const filter = JSON.parse(url.searchParams.get("filter") ?? "{}") as {
      kinds?: string[]
    }
    const all = [...this.records.values()].filter(
      (r) => !r.deletedAt && (filter.kinds ?? []).includes(r.kind)
    )
    const from = Number(url.searchParams.get("after") ?? 0)
    const first = Number(url.searchParams.get("first") ?? 200)
    const records = all.slice(from, from + first)
    const more = from + first < all.length
    return json(
      200,
      more ? { records, cursor: String(from + first) } : { records }
    )
  }

  private record(
    method: string,
    kind: string,
    id: string,
    body: unknown
  ): Response {
    const key = `${kind}/${id}`
    const here = this.records.get(key)
    const live = here && !here.deletedAt ? here : undefined
    const input = (body ?? {}) as {
      properties?: Record<string, unknown>
      ifVersion?: number
    }
    const held = (version: number | undefined) =>
      input.ifVersion !== undefined && input.ifVersion !== (version ?? 0)
    switch (method) {
      case "GET":
        return live
          ? json(200, live)
          : refused(404, "not_found", `${key} not found`)
      case "PUT":
        if (held(live?.version))
          return refused(
            409,
            "conflict",
            `${key} is at version ${live?.version ?? 0}`
          )
        return json(
          200,
          this.write(kind, id, input.properties ?? {}, "put", false)
        )
      case "PATCH":
        if (!live) return refused(404, "not_found", `${key} not found`)
        if (held(live.version))
          return refused(
            409,
            "conflict",
            `${key} is at version ${live.version}`
          )
        return json(
          200,
          this.write(kind, id, input.properties ?? {}, "patch", false)
        )
      case "DELETE": {
        if (!live) return refused(404, "not_found", `${key} not found`)
        this.records.set(key, {
          ...live,
          version: live.version + 1,
          deletedAt: this.now(),
        })
        this.log(kind, id, "delete")
        return new Response(null, { status: 204 })
      }
      default:
        return refused(405, "bad_request", method)
    }
  }

  private watch(url: URL, signal: AbortSignal | undefined): Response {
    const kinds = url.searchParams.getAll("kinds")
    const fromText = url.searchParams.get("from")
    const generation = url.searchParams.get("generation")
    if (fromText !== null && generation !== this.generation)
      return json(410, {
        error: {
          code: "compacted",
          message: "the cursor addresses another history",
        },
      })
    const from = fromText === null ? this.seq : Number(fromText)
    const encoder = new TextEncoder()
    let watcher:
      | { kinds: string[]; push: (line: string) => void; end: () => void }
      | undefined
    const stream = new ReadableStream<Uint8Array>({
      start: (controller) => {
        const push = (line: string) => {
          try {
            controller.enqueue(encoder.encode(line))
          } catch {
            // the reader went away
          }
        }
        push(
          JSON.stringify({ bookmark: from, generation: this.generation }) + "\n"
        )
        for (const row of this.rows)
          if (row.seq > from && kinds.includes(row.kind))
            push(this.rowLine(row))
        const end = () => {
          if (watcher) this.watchers.delete(watcher)
          try {
            controller.error(new TypeError("network connection lost"))
          } catch {
            // already closed
          }
        }
        watcher = { kinds, push, end }
        this.watchers.add(watcher)
        signal?.addEventListener("abort", end, { once: true })
      },
      cancel: () => {
        if (watcher) this.watchers.delete(watcher)
      },
    })
    return new Response(stream, {
      status: 200,
      headers: { "Content-Type": "application/x-ndjson" },
    })
  }

  /** Drops every open watch, as a network that goes away does. */
  dropWatches(): void {
    for (const w of [...this.watchers]) w.end()
  }

  /** How many watches are open now. */
  get openWatches(): number {
    return this.watchers.size
  }

  /** The writes the fake took, in order, as `METHOD kind/id`. */
  writes(): string[] {
    return this.requests
      .filter(
        (r) =>
          r.method !== "GET" &&
          r.path.startsWith("/api/v1/") &&
          r.path !== "/api/v1/authorizations/check"
      )
      .map((r) => `${r.method} ${r.path.slice("/api/v1/".length)}`)
  }
}

/** Waits until `check` holds, or fails after `timeout` ms. */
export async function until(
  check: () => boolean,
  timeout = 2000,
  what = "the condition"
): Promise<void> {
  const deadline = Date.now() + timeout
  while (!check()) {
    if (Date.now() > deadline) throw new Error(`timed out waiting for ${what}`)
    await new Promise((r) => setTimeout(r, 5))
  }
}

type LockCallback<T> = (lock: Lock | null) => T | PromiseLike<T>

/** Web Locks in memory, for an environment without them (jsdom): one
 * exclusive lock per name, granted in the order asked, `ifAvailable`
 * answered with no lock while it is held. `hold` takes a name and keeps it
 * until let go, so a test lines up two tabs' changes behind it and lets
 * them run in the order it asked them; `granted` lists the names in the
 * order each was granted; `install` puts it at `navigator.locks`. */
export class FakeLocks {
  /** The names granted, in order. */
  readonly granted: string[] = []
  private readonly held = new Set<string>()
  private readonly waiting = new Map<string, (() => void)[]>()

  request<T>(name: string, callback: LockCallback<T>): Promise<Awaited<T>>
  request<T>(
    name: string,
    options: LockOptions,
    callback: LockCallback<T>
  ): Promise<Awaited<T>>
  async request<T>(
    name: string,
    a: LockOptions | LockCallback<T>,
    b?: LockCallback<T>
  ): Promise<Awaited<T>> {
    const options = typeof a === "function" ? {} : a
    const callback = typeof a === "function" ? a : b!
    if (options.ifAvailable && this.held.has(name)) return await callback(null)
    const release = await this.take(name)
    try {
      return await callback({ name, mode: "exclusive" })
    } finally {
      release()
    }
  }

  /** Takes `name` as a request would, and keeps it until the answer is
   * called. */
  async hold(name: string): Promise<() => void> {
    return this.take(name)
  }

  /** How many requests wait for `name`. */
  queued(name: string): number {
    return this.waiting.get(name)?.length ?? 0
  }

  /** Puts these locks at `navigator.locks`; the answer takes them away. */
  install(): () => void {
    Object.defineProperty(navigator, "locks", {
      value: this,
      configurable: true,
    })
    return () => {
      delete (navigator as { locks?: LockManager }).locks
    }
  }

  private async take(name: string): Promise<() => void> {
    if (this.held.has(name)) {
      let list = this.waiting.get(name)
      if (!list) this.waiting.set(name, (list = []))
      await new Promise<void>((r) => list.push(r))
    } else {
      this.held.add(name)
    }
    this.granted.push(name)
    let released = false
    return () => {
      if (released) return
      released = true
      const next = this.waiting.get(name)?.shift()
      if (next) next()
      else this.held.delete(name)
    }
  }
}
