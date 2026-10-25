/*! The reader in this file is adapted from obsidian-inkidian
 * (https://github.com/toffalori/obsidian-inkidian), under its MIT License:
 *
 * Copyright (c) 2026 Thomas Toffalori
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to
 * deal in the Software without restriction, including without limitation the
 * rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
 * sell copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in
 * all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
 * FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS
 * IN THE SOFTWARE.
 */

/** Handwritten ink, in the Inkidian `.inkd` format version 1: a JSON document
 * of fixed-size pages, each a background pattern and a list of pressure
 * strokes. The spec is `format/SPEC.md` in
 * github.com/toffalori/obsidian-inkidian (MIT); section numbers below (§) are
 * that file's.
 *
 * Reading is lenient (§7): an unknown tool draws as a pen, an unknown
 * background as a blank page, and a missing optional field takes its default,
 * at the moment it is drawn. The document itself is never normalized, because
 * a writer must keep every field it does not understand on every object it
 * did not change (§2): a note written by a newer iPad app round-trips through
 * the console without losing anything. */

export const INK_FORMAT = "inkidian"
export const INK_FORMAT_VERSION = 1
/** The media type a `.inkd` blob is stored under. Inkidian registers none; a
 * vendor tree type is what RFC 6838 offers an unregistered format. */
export const INK_MEDIA_TYPE = "application/vnd.inkidian+json"
export const INK_EXTENSION = ".inkd"

export type InkTool = "pen" | "pencil" | "marker"
export type InkBackgroundType = "blank" | "lined" | "grid" | "dots"

/** `[x, y, pressure, t, azimuth, altitude]` (§6): page points, 0-1, seconds
 * since the stroke's first point, radians, radians. Readers ignore anything
 * after the sixth element. */
export type InkPoint = number[]

export interface InkStroke {
  id: string
  tool: string
  color: string
  opacity: number
  baseWidth: number
  points: InkPoint[]
  [unknown: string]: unknown
}

export interface InkBackground {
  type: string
  spacing?: number
  color?: string
  [unknown: string]: unknown
}

export interface InkPage {
  id: string
  background: InkBackground
  strokes: InkStroke[]
  [unknown: string]: unknown
}

export interface InkPageSize {
  width: number
  height: number
  unit: "pt"
  [unknown: string]: unknown
}

export interface InkDocument {
  format: typeof INK_FORMAT
  formatVersion: number
  id: string
  createdAt: string
  modifiedAt: string
  app?: { name?: string; version?: string; [unknown: string]: unknown }
  page: InkPageSize
  pages: InkPage[]
  [unknown: string]: unknown
}

export type InkReadResult =
  | { ok: true; document: InkDocument }
  | {
      ok: false
      reason: "invalid" | "not-ink" | "newer-version"
      message: string
    }

export const INK_TOOLS: readonly InkTool[] = ["pen", "pencil", "marker"]
export const INK_BACKGROUNDS: readonly InkBackgroundType[] = [
  "blank",
  "lined",
  "grid",
  "dots",
]
export const DEFAULT_SPACING = 32
export const DEFAULT_PATTERN_COLOR = "#D0D7E2"
export const PAPER_COLOR = "#FFFFFF"

/** The page sizes the iPad app creates (§3), at 1240 pt per 210 mm. */
export const INK_PAGE_SIZES = {
  a4: { width: 1240, height: 1754, label: "A4" },
  a5: { width: 874, height: 1240, label: "A5" },
  letter: { width: 1275, height: 1650, label: "US Letter" },
} as const

export type InkPaperName = keyof typeof INK_PAGE_SIZES
export type InkOrientation = "portrait" | "landscape"

/** A page's width and height in page units. */
export interface InkSize {
  width: number
  height: number
}

/** A paper size turned to `orientation`: portrait stands the long side up. */
export function oriented(size: InkSize, orientation: InkOrientation): InkSize {
  const long = Math.max(size.width, size.height)
  const short = Math.min(size.width, size.height)
  return orientation === "portrait"
    ? { width: short, height: long }
    : { width: long, height: short }
}

export function orientationOf(size: InkSize): InkOrientation {
  return size.width > size.height ? "landscape" : "portrait"
}

/** The named paper a size is, in either orientation, or undefined. */
export function paperOf(size: InkSize): InkPaperName | undefined {
  for (const name of Object.keys(INK_PAGE_SIZES) as InkPaperName[]) {
    const paper = INK_PAGE_SIZES[name]
    const { width, height } = oriented(paper, orientationOf(size))
    if (width === size.width && height === size.height) return name
  }
  return undefined
}

/** How a size reads: "A4 · Portrait", or "1032 × 1376 · Portrait". */
export function sizeLabel(size: InkSize): string {
  const paper = paperOf(size)
  const orientation =
    orientationOf(size) === "portrait" ? "Portrait" : "Landscape"
  const name = paper
    ? INK_PAGE_SIZES[paper].label
    : `${Math.round(size.width)} × ${Math.round(size.height)}`
  return `${name} · ${orientation}`
}

/** Whether any stroke reaches past a page of `size`: drawn, it would be
 * clipped there, though the file keeps it. */
export function inkOutside(document: InkDocument, size: InkSize): boolean {
  return document.pages.some((page) =>
    page.strokes.some((stroke) =>
      strokePoints(stroke).some(([x, y]) => x > size.width || y > size.height)
    )
  )
}

const HEX_COLOR = /^#[0-9a-f]{6}$/i

/** Reads a `.inkd` document from its text or its parsed JSON. It refuses what
 * §7 says cannot be rendered (not JSON, another format, a missing page size
 * or page list) and a version newer than this reader (§2); everything else
 * renders, leniently. */
export function readInk(input: unknown): InkReadResult {
  let json = input
  if (typeof input === "string") {
    try {
      json = JSON.parse(input)
    } catch {
      return invalid("it is not valid JSON")
    }
  }
  if (!isObject(json) || json.format !== INK_FORMAT) {
    return {
      ok: false,
      reason: "not-ink",
      message: "This is not an Inkidian note.",
    }
  }
  const version = json.formatVersion
  if (typeof version !== "number" || !Number.isInteger(version) || version < 1)
    return invalid("its format version is missing")
  if (version > INK_FORMAT_VERSION) {
    return {
      ok: false,
      reason: "newer-version",
      message: `This note was written in Inkidian format version ${version}, and the console reads version ${INK_FORMAT_VERSION}.`,
    }
  }
  const page = json.page
  if (!isObject(page) || !isPositive(page.width) || !isPositive(page.height))
    return invalid("its page size is missing")
  if (!Array.isArray(json.pages) || json.pages.length === 0)
    return invalid("it has no pages")
  const pages: unknown[] = json.pages
  if (!pages.every((p) => isObject(p) && Array.isArray(p.strokes)))
    return invalid("a page has no stroke list")
  // Every read of a stroke (the summary, the outline, the eraser) takes its
  // fields, so a stroke that is not an object cannot be drawn leniently.
  const strokes = pages.flatMap((p) => (p as { strokes: unknown[] }).strokes)
  if (!strokes.every(isObject))
    return invalid("a page holds a stroke that is not an object")
  return { ok: true, document: json as InkDocument }
}

function invalid(detail: string): InkReadResult {
  return {
    ok: false,
    reason: "invalid",
    message: `This note can't be displayed because ${detail}.`,
  }
}

/** A fresh one-page note. `now` and `newId` are parameters so a test can
 * pin them. */
export function newInkDocument(
  options: {
    size?: { width: number; height: number }
    background?: InkBackgroundType
    now?: Date
    newId?: () => string
  } = {}
): InkDocument {
  const size = options.size ?? INK_PAGE_SIZES.a4
  const newId = options.newId ?? randomId
  const stamp = inkTimestamp(options.now ?? new Date())
  return {
    format: INK_FORMAT,
    formatVersion: INK_FORMAT_VERSION,
    id: newId(),
    createdAt: stamp,
    modifiedAt: stamp,
    app: { name: "substrate console", version: "1" },
    page: { width: size.width, height: size.height, unit: "pt" },
    pages: [newInkPage(options.background ?? "blank", newId)],
  }
}

export function newInkPage(
  background: InkBackgroundType,
  newId: () => string = randomId
): InkPage {
  return { id: newId(), background: { type: background }, strokes: [] }
}

/** The document with `stroke` drawn last on page `pageIndex`. Every object
 * the change does not touch is carried over as the same reference, unknown
 * fields and all (§2). */
export function withStroke(
  document: InkDocument,
  pageIndex: number,
  stroke: InkStroke,
  now: Date = new Date()
): InkDocument {
  return withPage(document, pageIndex, now, (page) => ({
    ...page,
    strokes: [...page.strokes, stroke],
  }))
}

/** The document without `strokes` on page `pageIndex`, matched by object
 * (strokes are never mutated, so the object is the stroke). */
export function withoutStrokes(
  document: InkDocument,
  pageIndex: number,
  strokes: ReadonlySet<InkStroke>,
  now: Date = new Date()
): InkDocument {
  if (strokes.size === 0) return document
  return withPage(document, pageIndex, now, (page) => ({
    ...page,
    strokes: page.strokes.filter((stroke) => !strokes.has(stroke)),
  }))
}

/** The document with one more page after the last, on `background`. */
export function withNewPage(
  document: InkDocument,
  background: InkBackgroundType,
  now: Date = new Date(),
  newId: () => string = randomId
): InkDocument {
  return {
    ...document,
    modifiedAt: inkTimestamp(now),
    pages: [...document.pages, newInkPage(background, newId)],
  }
}

/** The document on pages of another size. One size holds for every page
 * (§3), and the strokes keep their coordinates: the page grows or shrinks
 * from its top-left corner, and ink past the new edge is clipped when drawn,
 * never moved or removed. */
export function withPageSize(
  document: InkDocument,
  size: InkSize,
  now: Date = new Date()
): InkDocument {
  if (
    document.page.width === size.width &&
    document.page.height === size.height
  )
    return document
  return {
    ...document,
    modifiedAt: inkTimestamp(now),
    page: { ...document.page, width: size.width, height: size.height },
  }
}

/** The document with page `pageIndex` on another background pattern. */
export function withBackground(
  document: InkDocument,
  pageIndex: number,
  background: InkBackgroundType,
  now: Date = new Date()
): InkDocument {
  return withPage(document, pageIndex, now, (page) => ({
    ...page,
    background: { ...page.background, type: background },
  }))
}

function withPage(
  document: InkDocument,
  pageIndex: number,
  now: Date,
  change: (page: InkPage) => InkPage
): InkDocument {
  const page = document.pages[pageIndex]
  if (!page) return document
  const pages = document.pages.slice()
  pages[pageIndex] = change(page)
  return { ...document, modifiedAt: inkTimestamp(now), pages }
}

/** The bytes a `.inkd` file holds. Writers may pretty-print (§1); the console
 * does not, because a note is mostly point arrays and indentation roughly
 * doubles them. */
export function inkText(document: InkDocument): string {
  return JSON.stringify(document)
}

/** RFC 3339 in UTC, as §3 requires; fractional seconds are dropped. */
export function inkTimestamp(date: Date): string {
  return date.toISOString().replace(/\.\d+Z$/, "Z")
}

/** The point the spec stores for one input sample, rounded to its precision
 * (§4, §6). An unknown pressure is 0.5, an unknown azimuth 0 and an unknown
 * altitude π/2, which is what a mouse reports. */
export function inkPoint(
  x: number,
  y: number,
  pressure = 0.5,
  t = 0,
  azimuth = 0,
  altitude = Math.PI / 2
): InkPoint {
  return [
    round(x, 2),
    round(y, 2),
    round(clamp01(pressure), 3),
    round(Math.max(0, t), 3),
    round(azimuth, 3),
    round(altitude, 3),
  ]
}

// ---- lenient reads, at draw time (§7)

export function strokeTool(stroke: InkStroke): InkTool {
  return (INK_TOOLS as readonly string[]).includes(stroke.tool)
    ? (stroke.tool as InkTool)
    : "pen"
}

export function strokeColor(stroke: InkStroke): string {
  return isColor(stroke.color) ? stroke.color : "#000000"
}

export function strokeOpacity(stroke: InkStroke): number {
  return isFiniteNumber(stroke.opacity) ? clamp01(stroke.opacity) : 1
}

export function strokeBaseWidth(stroke: InkStroke): number {
  return isPositive(stroke.baseWidth) ? stroke.baseWidth : 1
}

/** The stroke's usable points as `[x, y, pressure]`; a point without a finite
 * x and y is skipped, a missing pressure is 0.5. */
export function strokePoints(stroke: InkStroke): [number, number, number][] {
  if (!Array.isArray(stroke.points)) return []
  const out: [number, number, number][] = []
  for (const point of stroke.points) {
    if (!Array.isArray(point)) continue
    const [x, y, pressure] = point
    if (!isFiniteNumber(x) || !isFiniteNumber(y)) continue
    out.push([x, y, isFiniteNumber(pressure) ? clamp01(pressure) : 0.5])
  }
  return out
}

export function backgroundType(background: unknown): InkBackgroundType {
  const type = isObject(background) ? background.type : undefined
  return typeof type === "string" &&
    (INK_BACKGROUNDS as readonly string[]).includes(type)
    ? (type as InkBackgroundType)
    : "blank"
}

export function backgroundSpacing(background: unknown): number {
  const spacing = isObject(background) ? background.spacing : undefined
  return isPositive(spacing) ? spacing : DEFAULT_SPACING
}

export function backgroundColor(background: unknown): string {
  const color = isObject(background) ? background.color : undefined
  return isColor(color) ? color : DEFAULT_PATTERN_COLOR
}

/** How much ink the document holds, for a list cell or a summary line. */
export function inkSummary(document: InkDocument): {
  pages: number
  strokes: number
  points: number
} {
  let strokes = 0
  let points = 0
  for (const page of document.pages) {
    strokes += page.strokes.length
    for (const stroke of page.strokes)
      points += Array.isArray(stroke.points) ? stroke.points.length : 0
  }
  return { pages: document.pages.length, strokes, points }
}

export function randomId(): string {
  return crypto.randomUUID()
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

function isFiniteNumber(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value)
}

function isPositive(value: unknown): value is number {
  return isFiniteNumber(value) && value > 0
}

function isColor(value: unknown): value is string {
  return typeof value === "string" && HEX_COLOR.test(value)
}

function clamp01(value: number): number {
  return Math.min(Math.max(value, 0), 1)
}

function round(value: number, decimals: number): number {
  const factor = 10 ** decimals
  return Math.round(value * factor) / factor
}
