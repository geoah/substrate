/** A property's declared `mediaTypes` and a blob's own media type, compared.
 * A declaration lists exact types (`application/pdf`) or a family
 * (`image/*`); a blob's type may carry parameters (`; charset=utf-8`), which
 * never decide a match. */

import { INK_EXTENSION, INK_MEDIA_TYPE } from "@/lib/ink"

export const PDF_MEDIA_TYPE = "application/pdf"

/** The bare `type/subtype`, lowercased, parameters dropped. */
export function essence(mediaType: string | undefined): string {
  return (mediaType ?? "").split(";")[0].trim().toLowerCase()
}

/** Whether `actual` is one of `declared`, a family wildcard included. */
export function mediaTypeMatches(declared: string, actual: string): boolean {
  const want = essence(declared)
  const got = essence(actual)
  if (!want || !got) return false
  if (want.endsWith("/*")) return got.startsWith(want.slice(0, -1))
  return want === got
}

/** Whether a declaration lists `mediaType` (or a family holding it). */
export function declaresMediaType(
  spec: { mediaTypes?: string[] },
  mediaType: string
): boolean {
  return (spec.mediaTypes ?? []).some((d) => mediaTypeMatches(d, mediaType))
}

/** Whether a stored blob is ink: by its media type, or, where the uploader
 * said none, by its `.inkd` name. */
export function isInkBlob(blob: { mediaType?: string; name?: string }) {
  if (blob.mediaType && essence(blob.mediaType) !== "application/octet-stream")
    return mediaTypeMatches(INK_MEDIA_TYPE, blob.mediaType)
  return (blob.name ?? "").toLowerCase().endsWith(INK_EXTENSION)
}

export function isPdfBlob(blob: { mediaType?: string; name?: string }) {
  if (blob.mediaType && essence(blob.mediaType) !== "application/octet-stream")
    return mediaTypeMatches(PDF_MEDIA_TYPE, blob.mediaType)
  return (blob.name ?? "").toLowerCase().endsWith(".pdf")
}

/** The `accept` attribute a file input offers for a declaration: its media
 * types, plus the extension a browser needs for a type it does not know. */
export function acceptFor(spec: { mediaTypes?: string[] }): string | undefined {
  const types = spec.mediaTypes ?? []
  if (!types.length) return undefined
  const out = new Set<string>()
  for (const t of types) {
    out.add(t)
    if (mediaTypeMatches(t, INK_MEDIA_TYPE)) out.add(INK_EXTENSION)
    if (mediaTypeMatches(t, PDF_MEDIA_TYPE)) out.add(".pdf")
  }
  return [...out].join(",")
}
