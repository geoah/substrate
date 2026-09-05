/** The fetch envelope: every call to the substrate goes through `request`,
 * carries the session bearer and the actor-attribution header, and rejects
 * with `ApiError` shaped from the REST error envelope
 * (`{error: {code, message, problems}}`). A 401 on an authenticated call drops
 * the session (see session.ts).
 *
 * There is NO repository segment anywhere — the token implies the repository —
 * and no tenant. A collection path IS the kind reference split into segments:
 * `/{authority}/{package}/{kind}`, three segments, and the last of them is the
 * kind name. */

import { getToken, sessionExpired } from "./session"
import { ApiError, type ErrorCode, type ProblemDetail } from "./types"

export const API_BASE = "/api/v1"

/** The authority the substrate publishes its own vocabulary under. */
export const CORE_AUTHORITY = "substrate.reamde.dev"

/** The core package's own word — the middle segment of every core kind
 * reference. */
export const CORE_PACKAGE_NAME = "core"

/** The core package IDENTITY, `<authority>/<package>`: the package whose
 * meta-model collections (`kind`, `bundle`, `agent`, …) the console addresses
 * by name. Every other collection comes out of the registry. */
export const CORE_PACKAGE = `${CORE_AUTHORITY}/${CORE_PACKAGE_NAME}`

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
      | {
          error?: {
            code?: string
            message?: string
            problems?: string[]
            problemDetails?: ProblemDetail[]
          }
        }
      | undefined
  )?.error
  const code = (err?.code ?? fallbackCode(status)) as ErrorCode
  const message = err?.message ?? `request failed (${status})`
  return new ApiError(
    code,
    message,
    status,
    err?.problems ?? [],
    err?.problemDetails ?? [],
    retryAfter
  )
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

/** A kind reference split into its three parts. A stored kind is always
 * authority- and package-qualified (decisions 0042, 0047); the empty answer
 * only ever comes back for a bare shorthand name, which mirrors the server's
 * `SplitKindRef`. */
export function splitKind(ref: string): {
  authority: string
  pkg: string
  name: string
} {
  const first = ref.indexOf("/")
  if (first < 0) return { authority: "", pkg: "", name: ref }
  const second = ref.indexOf("/", first + 1)
  if (second < 0) return { authority: "", pkg: "", name: ref }
  return {
    authority: ref.slice(0, first),
    pkg: ref.slice(first + 1, second),
    name: ref.slice(second + 1),
  }
}

/** A kind reference from its parts — the inverse of `splitKind`. */
export function joinKind(authority: string, pkg: string, name: string): string {
  return authority ? `${authority}/${pkg}/${name}` : name
}

/** The package's own path prefix: the authority and the package, each its own
 * segment. */
function packagePath(authority: string, pkg: string): string {
  return `${API_BASE}/${seg(authority)}/${seg(pkg)}`
}

/** The collection path of a declared kind: the authority, the package and the
 * collection segment are three path segments, and every kind carries all three
 * (decisions 0042, 0047). The collection segment IS the kind's NAME (decision
 * 0033), so everything after `/api/v1/` is the kind reference and a record's
 * path is the value a `reference` property stores. */
export function collectionPath(
  authority: string,
  pkg: string,
  name: string
): string {
  return `${packagePath(authority, pkg)}/${seg(name)}`
}

/** The core package's own collections, addressed the same way (the segment
 * is the kind name). */
export function corePath(name: string, id?: string): string {
  const base = collectionPath(CORE_AUTHORITY, CORE_PACKAGE_NAME, name)
  return id === undefined ? base : `${base}/${seg(id)}`
}

/** A repository-wide endpoint that names no kind: `/api/v1/{segments}`. The
 * changefeed, the vocabulary apply, the blob store, GraphQL, the catalog, the
 * merge/split and the OAuth doors live at the version root, out of the kind
 * namespace, so nothing needs a separator to sit beside a collection (decision
 * 0033). */
export function rootPath(...segments: string[]): string {
  return `${API_BASE}/${segments.map(seg).join("/")}`
}

/** A sub-resource or action of one record: the record's path, then the
 * sub-resource segments, one level below the id where no id can collide with
 * them (`.../{id}/incoming`, `.../{name}/call`). */
export function recordSubPath(
  authority: string,
  pkg: string,
  name: string,
  id: string,
  ...sub: string[]
): string {
  return `${collectionPath(authority, pkg, name)}/${seg(id)}/${sub.map(seg).join("/")}`
}
