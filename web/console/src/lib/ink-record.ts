/** Which of a record's properties hold ink or a PDF, and how the record page
 * shows them. Both are files: a `blobref` holds ink when its declaration
 * lists the `.inkd` media type, or, undeclared, when the blob it names is
 * one; the same for a PDF. A record's one PDF is shown as its pages, and an
 * ink file beside it is drawn over them, a marked-up PDF, rather than on
 * paper of its own. */

import { readBlobInfo } from "@/lib/api/blobs"
import type { KindInfo } from "@/lib/api/types"
import { recordTitle } from "@/lib/format"
import { INK_MEDIA_TYPE, readInk, type InkDocument } from "@/lib/ink"
import {
  PDF_MEDIA_TYPE,
  declaresMediaType,
  isInkBlob,
  isPdfBlob,
} from "@/lib/media-types"
import { titleProperty, type PropSpec } from "@/lib/record-schema"

export interface InkPlan {
  /** Ink drawn on its own paper, one section each. */
  ink: PropSpec[]
  /** A record's one PDF, and the ink layer drawn over it where the kind
   * has an ink file beside it. */
  markup?: { document: PropSpec; ink?: PropSpec }
}

function single(spec: PropSpec): boolean {
  return !spec.repeated && !spec.keyed
}

export function isInkSpec(spec: PropSpec, value?: unknown): boolean {
  if (!single(spec) || spec.kind !== "blobref") return false
  if (spec.mediaTypes?.length) return declaresMediaType(spec, INK_MEDIA_TYPE)
  const blob = readBlobInfo(value)
  return Boolean(blob && isInkBlob(blob))
}

export function isPdfSpec(spec: PropSpec, value?: unknown): boolean {
  if (!single(spec) || spec.kind !== "blobref") return false
  if (spec.mediaTypes?.length) return declaresMediaType(spec, PDF_MEDIA_TYPE)
  const blob = readBlobInfo(value)
  return Boolean(blob && isPdfBlob(blob))
}

export function inkPlan(
  specs: PropSpec[],
  properties: Record<string, unknown> = {}
): InkPlan {
  const ink = specs.filter((s) => isInkSpec(s, properties[s.name]))
  const pdfs = specs.filter((s) => isPdfSpec(s, properties[s.name]))
  if (pdfs.length !== 1) return { ink }
  const layer = ink.find((s) => s.kind === "blobref")
  return {
    ink: ink.filter((s) => s !== layer),
    markup: layer ? { document: pdfs[0], ink: layer } : { document: pdfs[0] },
  }
}

/** The property names the plan's sections show, which the sheet folds away. */
export function inkPlanNames(plan: InkPlan): string[] {
  const names = plan.ink.map((s) => s.name)
  if (plan.markup) names.push(plan.markup.document.name)
  if (plan.markup?.ink) names.push(plan.markup.ink.name)
  return names
}

/** The bytes a `.inkd` blob holds, read as a document. */
export function inkFromBytes(bytes: ArrayBuffer) {
  return readInk(new TextDecoder().decode(bytes))
}

/** A file name for a note: the record's title, made safe to save. */
export function inkFileName(title: string | undefined, fallback = "note") {
  const base = (title ?? "")
    .replace(/[\\/:*?"<>|]+/g, " ")
    .replace(/\s+/g, " ")
    .trim()
  return `${base || fallback}.inkd`
}

/** The file name a note held in `property` is saved under: the record's
 * title, else (a record not written yet has no derived title) the property
 * its kind's heading is typed into, else the property's own name. */
export function noteFileName(
  properties: Record<string, unknown>,
  kind: KindInfo | undefined,
  property: string
): string {
  const heading = titleProperty(kind)
  const typed = heading ? properties[heading] : undefined
  return inkFileName(
    recordTitle(properties) || (typeof typed === "string" ? typed : ""),
    property
  )
}

/** A layer with at least `count` pages, one per PDF page. Pages are added
 * blank at the end; nothing else changes. */
export function withPageCount(
  document: InkDocument,
  count: number,
  newId: () => string = () => crypto.randomUUID()
): InkDocument {
  if (document.pages.length >= count) return document
  const pages = document.pages.slice()
  while (pages.length < count)
    pages.push({ id: newId(), background: { type: "blank" }, strokes: [] })
  return { ...document, pages }
}

/** `12.4 KB`: a size a person reads. */
export function byteSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1 << 20) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1 << 20)).toFixed(1)} MB`
}

export function inkCounts(summary: { pages: number; strokes: number }) {
  const pages = `${summary.pages} ${summary.pages === 1 ? "page" : "pages"}`
  const strokes = `${summary.strokes} ${summary.strokes === 1 ? "stroke" : "strokes"}`
  return `${pages}, ${strokes}`
}
