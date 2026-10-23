/** The blob store: `PUT /api/v1/blobs` stores bytes under their SHA-256 and
 * answers the manifest, `GET /api/v1/blobs/{digest}` reads them back. A record
 * names a blob through a `blobref` property, which stores the digest and reads
 * back as the manifest. Bytes are not JSON, so these two calls go around the
 * `request` envelope and carry its two headers themselves.
 *
 * A digest names one sequence of bytes forever, so a read never goes stale
 * and its query is cached for the session. */

import { queryOptions } from "@tanstack/react-query"

import { envelopeError, rootPath } from "./http"
import { getToken, sessionExpired } from "./session"
import { ApiError, type BlobInfo } from "./types"

/** The server's own cap on one blob (`maxBlobBody` in internal/api). */
export const MAX_BLOB_BYTES = 64 << 20

const BLOB_DIGEST = /^blob-sha256-[0-9a-f]{64}$/

export function isBlobDigest(value: unknown): value is string {
  return typeof value === "string" && BLOB_DIGEST.test(value)
}

/** The manifest a blobref property reads back as, or undefined for anything
 * else (a bare digest an editor holds, an absent value). */
export function readBlobInfo(value: unknown): BlobInfo | undefined {
  if (typeof value !== "object" || value === null || Array.isArray(value))
    return undefined
  const v = value as Record<string, unknown>
  if (!isBlobDigest(v.digest)) return undefined
  return {
    digest: v.digest,
    size: typeof v.size === "number" ? v.size : 0,
    name: typeof v.name === "string" ? v.name : undefined,
    mediaType: typeof v.mediaType === "string" ? v.mediaType : undefined,
    status:
      v.status === "pending" || v.status === "failed" ? v.status : "stored",
  }
}

function headers(): Record<string, string> {
  const out: Record<string, string> = { "X-Substrate-Actor": "console" }
  const token = getToken()
  if (token) out.Authorization = `Bearer ${token}`
  return out
}

async function refusal(res: Response): Promise<ApiError> {
  if (res.status === 401) sessionExpired()
  let body: unknown
  try {
    body = await res.json()
  } catch {
    body = undefined
  }
  return envelopeError(res.status, body)
}

/** Store `bytes` and answer the manifest. The same bytes always land on the
 * same digest, and a second upload keeps the name and media type the first
 * one gave. A record must name the blob within the server's upload grace (a
 * minute) or the sweep collects it, so call this right before the write that
 * references it. */
export async function putBlob(
  bytes: Blob | ArrayBuffer | Uint8Array | string,
  meta: { name?: string; mediaType?: string } = {}
): Promise<BlobInfo> {
  const body =
    bytes instanceof Blob
      ? bytes
      : new Blob([bytes as BlobPart], {
          type: meta.mediaType ?? "application/octet-stream",
        })
  if (body.size > MAX_BLOB_BYTES) {
    throw new ApiError(
      "bad_request",
      `The file is ${Math.ceil(body.size / (1 << 20))} MiB, and a substrate stores files up to 64 MiB.`,
      413
    )
  }
  const path = meta.name
    ? `${rootPath("blobs")}?name=${encodeURIComponent(meta.name)}`
    : rootPath("blobs")
  let res: Response
  try {
    res = await fetch(path, {
      method: "PUT",
      headers: {
        ...headers(),
        Accept: "application/json",
        "Content-Type":
          meta.mediaType ?? (body.type || "application/octet-stream"),
      },
      body,
    })
  } catch (cause) {
    throw new ApiError(
      "network",
      (cause as Error).message || "network error",
      0
    )
  }
  if (!res.ok) throw await refusal(res)
  return (await res.json()) as BlobInfo
}

/** The bytes a digest names. */
export async function fetchBlob(
  digest: string,
  signal?: AbortSignal
): Promise<ArrayBuffer> {
  let res: Response
  try {
    res = await fetch(rootPath("blobs", digest), {
      headers: headers(),
      signal,
    })
  } catch (cause) {
    throw new ApiError(
      "network",
      (cause as Error).message || "network error",
      0
    )
  }
  if (!res.ok) throw await refusal(res)
  return res.arrayBuffer()
}

export function blobQueryOptions(digest: string | undefined) {
  return queryOptions({
    queryKey: ["blob", digest],
    queryFn: ({ signal }) => fetchBlob(digest as string, signal),
    enabled: Boolean(digest),
    staleTime: Infinity,
    gcTime: 10 * 60_000,
  })
}

/** Hand bytes to the browser as a download named `name`. */
export function saveBytes(
  bytes: ArrayBuffer | Uint8Array | string,
  name: string,
  mediaType = "application/octet-stream"
) {
  const url = URL.createObjectURL(
    new Blob([bytes as BlobPart], { type: mediaType })
  )
  try {
    const link = document.createElement("a")
    link.href = url
    link.download = name
    document.body.appendChild(link)
    link.click()
    link.remove()
  } finally {
    setTimeout(() => URL.revokeObjectURL(url), 60_000)
  }
}
