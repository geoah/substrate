// The files a plan stores: each by the SHA-256 of its bytes, under a
// neutral name that never repeats the provider's file name (which can carry
// the owner's name and birth date), and a JSON attachment as canonical JSON
// (apps/healthspan/docs/importers/00-shared.md, Documents).

import { sha256 } from "../../ids"
import type { PlannedDocument } from "../types"
import { hex } from "./keys"

/** A file's media type by its extension. */
export const MEDIA_TYPES: Readonly<Record<string, string>> = {
  pdf: "application/pdf",
  jpg: "image/jpeg",
  jpeg: "image/jpeg",
  png: "image/png",
  gif: "image/gif",
  tif: "image/tiff",
  tiff: "image/tiff",
  json: "application/json",
  html: "text/html",
  htm: "text/html",
  txt: "text/plain",
  csv: "text/csv",
  rtf: "application/rtf",
  xml: "application/xml",
  zip: "application/zip",
}

/** A path's extension, lower case and without its dot, "" where it has
 * none. */
export function extensionOf(path: string): string {
  const name = path.slice(path.lastIndexOf("/") + 1)
  const dot = name.lastIndexOf(".")
  return dot > 0 ? name.slice(dot + 1).toLowerCase() : ""
}

/** A path's media type by its extension, `fallback` for one not in the
 * table. */
export function mediaTypeOf(
  path: string,
  fallback = "application/octet-stream"
): string {
  return MEDIA_TYPES[extensionOf(path)] ?? fallback
}

/** The extension a media type is stored with (`jpg` for image/jpeg), ""
 * for one not in the table; any parameters after `;` are ignored. */
export function extensionFor(mediaType: string): string {
  const type = mediaType.split(";")[0].trim().toLowerCase()
  return Object.keys(MEDIA_TYPES).find((e) => MEDIA_TYPES[e] === type) ?? ""
}

/** The digest the repository stores bytes under: `blob-sha256-<hex>`. */
export function blobDigest(bytes: Uint8Array): string {
  return `blob-sha256-${hex(sha256(bytes))}`
}

/** A document's neutral name: `<provider> <kind> <date>.<ext>`
 * (`Randox health report 2026-01-19.pdf`), the extension left off where
 * there is none. */
export function documentName(
  provider: string,
  kind: string,
  date: string,
  ext: string
): string {
  const base = [provider, kind, date].filter(Boolean).join(" ")
  return ext ? `${base}.${ext}` : base
}

/** A document the plan stores, named neutrally. The media type is the
 * given one, else the extension's; the extension the given one, else the
 * media type's. */
export function plannedDocument(
  bytes: Uint8Array,
  meta: {
    provider: string
    kind: string
    /** `YYYY-MM-DD`. */
    date: string
    mediaType?: string
    ext?: string
  }
): PlannedDocument {
  const mediaType =
    meta.mediaType?.split(";")[0].trim() ||
    (meta.ext ? mediaTypeOf(`x.${meta.ext}`) : "application/octet-stream")
  const ext = (meta.ext ?? extensionFor(mediaType)).toLowerCase()
  return {
    digest: blobDigest(bytes),
    name: documentName(meta.provider, meta.kind, meta.date, ext),
    mediaType,
    bytes,
  }
}

/** Code-point order, which Python sorts strings in; JavaScript's default
 * sort compares UTF-16 units, which differs above U+FFFF. */
function byCodePoint(a: string, b: string): number {
  const x = [...a]
  const y = [...b]
  for (let i = 0; i < Math.min(x.length, y.length); i++) {
    const d = x[i].codePointAt(0)! - y[i].codePointAt(0)!
    if (d) return d
  }
  return x.length - y.length
}

/** A JSON value as canonical text: no whitespace, every object's keys in
 * code-point order, characters as they are (not `\u` escapes), numbers as
 * JavaScript prints them (`140` where Python prints `140.0`). An undefined
 * member is left out and an undefined array element is `null`, as
 * `JSON.stringify` does. */
export function canonicalJson(value: unknown): string {
  if (value === null || typeof value !== "object") {
    if (typeof value === "bigint") return value.toString()
    return JSON.stringify(value) ?? "null"
  }
  if (Array.isArray(value))
    return `[${value.map((v: unknown) => (v === undefined ? "null" : canonicalJson(v))).join(",")}]`
  const o = value as Record<string, unknown>
  const keys = Object.keys(o)
    .filter((k) => o[k] !== undefined && typeof o[k] !== "function")
    .sort(byCodePoint)
  return `{${keys.map((k) => `${JSON.stringify(k)}:${canonicalJson(o[k])}`).join(",")}}`
}

/** A value kept as a JSON document: its canonical JSON as UTF-8. */
export function jsonDocument(
  value: unknown,
  meta: { provider: string; kind: string; date: string }
): PlannedDocument {
  return plannedDocument(new TextEncoder().encode(canonicalJson(value)), {
    ...meta,
    mediaType: "application/json",
    ext: "json",
  })
}
