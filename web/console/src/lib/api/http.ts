/** The fetch envelope: every call to the substrate goes through `request`,
 * carries the session bearer and the actor-attribution header, and rejects
 * with `ApiError` shaped from the REST error envelope
 * (`{error: {code, message, problems}}`). A 401 on an authenticated call drops
 * the session (see session.ts).
 *
 * There is NO repository segment anywhere — the token implies the repository —
 * and no tenant. A collection path IS the kind reference split into segments:
 * `/{authority}/{kind}` for a published kind, `/{kind}` for a
 * repository-local one. */

import { getToken, sessionExpired } from "./session"
import { ApiError, type ErrorCode } from "./types"

export const API_BASE = "/api/v1"

/** The substrate's own authority, whose meta-model collections
 * (`kinds`, `catalog`, `bundles`, `changes`, …) the console addresses by
 * name. Every other collection comes out of the registry. */
export const CORE_AUTHORITY = "core.substrate.reamde.dev"

/** The console names which door a write came through: `X-Substrate-Actor` is
 * ATTRIBUTION, not authorization. */
const ACTOR = "console"

type Method = "GET" | "POST" | "PUT" | "PATCH" | "DELETE"

export interface RequestOpts {
  /** Skip the stored bearer (the door: login, register, password, totp). A 401
   * on an anonymous call never drops the session — there was no session at
   * stake. */
  anonymous?: boolean
  /** Carry this token instead of the stored one (login verification). */
  token?: string
  signal?: AbortSignal
}

function fallbackCode(status: number): ErrorCode {
  switch (status) {
    case 400:
      return "bad_request"
    case 401:
      return "auth"
    case 403:
      return "forbidden"
    case 404:
      return "not_found"
    case 409:
      return "conflict"
    case 410:
      return "compacted"
    case 422:
      return "validation"
    case 429:
      return "rate_limited"
    case 500:
      return "internal"
    case 501:
      return "unsupported"
    case 503:
      return "unavailable"
    default:
      return "network"
  }
}

export function envelopeError(
  status: number,
  body: unknown,
  retryAfter?: number
): ApiError {
  const err = (
    body as
      | { error?: { code?: string; message?: string; problems?: string[] } }
      | undefined
  )?.error
  const code = (err?.code ?? fallbackCode(status)) as ErrorCode
  const message = err?.message ?? `request failed (${status})`
  return new ApiError(code, message, status, err?.problems ?? [], retryAfter)
}

export async function request<T>(
  method: Method,
  path: string,
  body?: unknown,
  opts: RequestOpts = {}
): Promise<T> {
  const headers: Record<string, string> = {
    Accept: "application/json",
    "X-Substrate-Actor": ACTOR,
  }
  if (body !== undefined) headers["Content-Type"] = "application/json"
  const token = opts.token ?? (opts.anonymous ? null : getToken())
  if (token) headers.Authorization = `Bearer ${token}`

  let res: Response
  try {
    res = await fetch(path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: opts.signal,
    })
  } catch (cause) {
    throw new ApiError(
      "network",
      (cause as Error).message || "network error",
      0
    )
  }

  const text = await res.text()
  let parsed: unknown
  if (text) {
    try {
      parsed = JSON.parse(text)
    } catch {
      parsed = undefined
    }
  }

  if (!res.ok) {
    const retryHeader = res.headers.get("Retry-After")
    const err = envelopeError(
      res.status,
      parsed,
      retryHeader ? Number(retryHeader) || undefined : undefined
    )
    if (res.status === 401 && !opts.anonymous && !opts.token) sessionExpired()
    throw err
  }
  return parsed as T
}

// ── the kind-reference grammar ──────────────────────────────────────────────

/** One path segment: `encodeURIComponent` turns the `/` inside a declaration
 * record's id into `%2F`, which the API decodes exactly once (`pathParam`). */
export const seg = encodeURIComponent

/** A kind reference split into its two parts. A bare name has no authority. */
export function splitKind(ref: string): { authority: string; name: string } {
  const slash = ref.indexOf("/")
  return slash < 0
    ? { authority: "", name: ref }
    : { authority: ref.slice(0, slash), name: ref.slice(slash + 1) }
}

/** A kind reference from its parts — the inverse of `splitKind`. */
export function joinKind(authority: string, name: string): string {
  return authority ? `${authority}/${name}` : name
}

/** The segment reserved for verbs at every depth. Nothing stored can be
 * spelled with it — an id begins with an alphanumeric, a kind name is one
 * lowercase word, an authority carries a dot — so a verb never takes an id out
 * of a collection (decision 0028). */
export const VERB = "-"

/** The collection path of a declared kind: the authority and the collection
 * segment are both segments, and a repository-local kind (empty authority) has
 * only the collection. The collection segment IS the kind's name, so
 * everything after `/api/v1/` is the kind reference and a record's path is the
 * value a `reference` property stores. */
export function collectionPath(authority: string, collection: string): string {
  return authority
    ? `${API_BASE}/${seg(authority)}/${seg(collection)}`
    : `${API_BASE}/${seg(collection)}`
}

/** The core meta-model's own collections, addressed the same way. */
export function corePath(collection: string, id?: string): string {
  const base = `${API_BASE}/${CORE_AUTHORITY}/${collection}`
  return id === undefined ? base : `${base}/${seg(id)}`
}

/** A repository verb: `/api/v1/-/{verb}`. The changefeed, the vocabulary
 * apply, the blob store, GraphQL, the catalog and the OAuth doors all live
 * here — none of them is a collection. */
export function verbPath(...segments: string[]): string {
  return `${API_BASE}/${VERB}/${segments.join("/")}`
}

/** A verb at one record: the record's path, the reserved segment, the verb. */
export function recordVerbPath(
  authority: string,
  collection: string,
  id: string,
  ...verb: string[]
): string {
  return `${collectionPath(authority, collection)}/${seg(id)}/${VERB}/${verb.join("/")}`
}
