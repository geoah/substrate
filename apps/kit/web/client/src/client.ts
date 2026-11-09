import type {
  AggregateAnswer,
  AggregateOptions,
  AggregateRequest,
} from "./aggregate"
import type {
  AuthorizationCheck,
  AuthorizationCheckResult,
} from "./authorizations"
import { SubstrateError, failure } from "./errors"
import type { ListQuery, RecordPage, SubstrateRecord } from "./records"

export type Query = Record<string, string | string[] | undefined>

export interface RequestOptions {
  query?: Query
  body?: unknown
  accept?: number[]
  signal?: AbortSignal
}

/** One substrate, as one signed-in app sees it: its origin and the bearer
 * token the approval minted. Every call carries the token itself; the server
 * sets no cookie, which is what lets it answer CORS with a wildcard. */
/** `text` without its trailing slashes. A loop, not `/\/+$/`: that pattern
 * backtracks quadratically on a long run of slashes not at the end. */
function trimSlashes(text: string): string {
  let end = text.length
  while (end > 0 && text[end - 1] === "/") end--
  return text.slice(0, end)
}

export class SubstrateClient {
  readonly server: string
  readonly token: string | undefined
  /** Called on a 401: the token was revoked or expired. */
  onUnauthorized?: () => void

  constructor(server: string, token?: string) {
    this.server = trimSlashes(server)
    this.token = token
  }

  url(path: string, query: Query = {}): string {
    const u = new URL(this.server + "/" + path.replace(/^\/+/, ""))
    for (const [k, v] of Object.entries(query)) {
      if (v === undefined) continue
      for (const one of Array.isArray(v) ? v : [v])
        u.searchParams.append(k, one)
    }
    return u.toString()
  }

  headers(accept = "application/json"): Headers {
    const h = new Headers({ Accept: accept })
    if (this.token) h.set("Authorization", `Bearer ${this.token}`)
    return h
  }

  async send(
    method: string,
    path: string,
    opts: RequestOptions = {}
  ): Promise<Response> {
    const headers = this.headers()
    let body: string | undefined
    if (opts.body !== undefined) {
      headers.set("Content-Type", "application/json")
      body = JSON.stringify(opts.body)
    }
    const res = await fetch(this.url(path, opts.query), {
      method,
      headers,
      body,
      signal: opts.signal,
      redirect: "error",
    })
    if (res.ok || opts.accept?.includes(res.status)) return res
    if (res.status === 401) this.onUnauthorized?.()
    throw await failure(res)
  }

  async json<T>(
    method: string,
    path: string,
    opts: RequestOptions = {}
  ): Promise<T> {
    const res = await this.send(method, path, opts)
    return (await res.json()) as T
  }

  /** A record's REST path: its kind reference, then its id as one segment. */
  static recordPath(kind: string, id: string): string {
    return `api/v1/${kind}/${encodeURIComponent(id)}`
  }

  /** One live record, or null: a deleted record reads as absent. */
  async record(
    kind: string,
    id: string,
    signal?: AbortSignal
  ): Promise<SubstrateRecord | null> {
    const res = await this.send("GET", SubstrateClient.recordPath(kind, id), {
      accept: [404],
      signal,
    })
    if (res.status === 404) return null
    const record = (await res.json()) as SubstrateRecord
    return record.deletedAt ? null : record
  }

  list(q: ListQuery, signal?: AbortSignal): Promise<RecordPage> {
    const filter: Record<string, unknown> = { kinds: q.kinds }
    if (q.properties && Object.keys(q.properties).length)
      filter.properties = q.properties
    return this.json<RecordPage>("GET", "api/v1/records", {
      query: {
        first: String(q.first ?? 200),
        filter: JSON.stringify(filter),
        after: q.after,
        orderBy: q.orderBy,
      },
      signal,
    })
  }

  /** Every page of a read, up to `limit` rows, the first page's head kept:
   * a watch from that head sees every write after the read. */
  async listAll(
    q: ListQuery,
    limit = 5000,
    signal?: AbortSignal
  ): Promise<RecordPage> {
    const all = await this.list(q, signal)
    all.records ??= []
    let cursor = all.cursor
    while (cursor && all.records.length < limit) {
      const page = await this.list({ ...q, after: cursor }, signal)
      all.records.push(...(page.records ?? []))
      cursor = page.cursor
    }
    all.cursor = cursor
    return all
  }

  /** The aggregate read: `GET /api/v1/records` with `aggregate`, and the
   * `filter` and `downsample` beside it, each as JSON. A refusal throws
   * the envelope's `SubstrateError`, a 422 naming the limit, the form or
   * the unit. */
  aggregate(
    aggregate: AggregateRequest,
    opts: AggregateOptions = {}
  ): Promise<AggregateAnswer> {
    return this.json<AggregateAnswer>("GET", "api/v1/records", {
      query: {
        aggregate: JSON.stringify(aggregate),
        filter: opts.filter && JSON.stringify(opts.filter),
        downsample: opts.downsample && JSON.stringify(opts.downsample),
      },
      signal: opts.signal,
    })
  }

  /** A put merges per property and never prunes; `ifVersion: 0` creates only. */
  put(
    kind: string,
    id: string,
    properties: Record<string, unknown>,
    ifVersion?: number,
    signal?: AbortSignal
  ): Promise<SubstrateRecord> {
    return this.json("PUT", SubstrateClient.recordPath(kind, id), {
      body: { properties, ifVersion },
      signal,
    })
  }

  /** A patch merges per property; null clears one. A state moves only here. */
  patch(
    kind: string,
    id: string,
    properties: Record<string, unknown>,
    ifVersion?: number,
    signal?: AbortSignal
  ): Promise<SubstrateRecord> {
    return this.json("PATCH", SubstrateClient.recordPath(kind, id), {
      body: { properties, ifVersion },
      signal,
    })
  }

  /** Deletes a record, restorably; one already gone is not an error. */
  async delete(kind: string, id: string, signal?: AbortSignal): Promise<void> {
    await this.send("DELETE", SubstrateClient.recordPath(kind, id), {
      accept: [404],
      signal,
    })
  }

  /** `POST /api/v1/authorizations/check`: what the token lacks of the
   * request the app would send now. */
  checkAuthorization(
    check: AuthorizationCheck,
    signal?: AbortSignal
  ): Promise<AuthorizationCheckResult> {
    return this.json("POST", "api/v1/authorizations/check", {
      body: check,
      signal,
    })
  }
}

export { SubstrateError }
