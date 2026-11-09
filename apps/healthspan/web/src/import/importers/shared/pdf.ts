// A PDF's positioned text through pdf.js, loaded only when an importer reads
// a PDF (apps/healthspan/docs/importers/00-shared.md, PDFs). A parser reads
// the items by their positions; their order and spacing are pdf.js's, not
// another library's text layout.

import { Effect } from "effect"
import type { ImportError } from "../types"
import { unreadable } from "./schema"

/** One run of text pdf.js places on a page. Positions are PDF user space:
 * `x` from the left edge, `y` from the bottom edge to the text's baseline,
 * as the page's content puts them. */
export interface PdfItem {
  text: string
  x: number
  y: number
  width: number
  height: number
  fontSize: number
  /** pdf.js's name for the font, the same within one document. */
  fontName: string
}

export interface PdfPage {
  /** 1 for the first page. */
  number: number
  width: number
  height: number
  /** The page's text items in pdf.js's order, empty ones left out. */
  items: PdfItem[]
}

type PdfJs = typeof import("pdfjs-dist")

let loading: Promise<PdfJs> | undefined

/** pdf.js, imported on first use. In a browser its worker is the one Vite
 * bundles; where there is no Worker (Node, in tests) pdf.js runs in the
 * same thread. */
function loadPdfJs(): Promise<PdfJs> {
  loading ??= (async () => {
    const pdfjs = await import("pdfjs-dist")
    if (typeof Worker !== "undefined" && !pdfjs.GlobalWorkerOptions.workerSrc) {
      const worker = await import("pdfjs-dist/build/pdf.worker.min.mjs?url")
      pdfjs.GlobalWorkerOptions.workerSrc = worker.default
    }
    return pdfjs
  })()
  return loading
}

async function readPages(
  bytes: Uint8Array,
  signal: AbortSignal
): Promise<PdfPage[]> {
  const pdfjs = await loadPdfJs()
  // pdf.js hands its buffer to the worker, which detaches it: a copy keeps
  // the caller's bytes, which the plan may store as the document.
  const task = pdfjs.getDocument({ data: bytes.slice(), verbosity: 0 })
  const stop = () => void task.destroy()
  signal.addEventListener("abort", stop)
  try {
    const doc = await task.promise
    const pages: PdfPage[] = []
    for (let n = 1; n <= doc.numPages; n++) {
      const page = await doc.getPage(n)
      const view = page.getViewport({ scale: 1 })
      const content = await page.getTextContent()
      const items: PdfItem[] = []
      for (const item of content.items) {
        if (!("str" in item) || !item.str) continue
        const [, , c, d, e, f] = item.transform as number[]
        items.push({
          text: item.str,
          x: e,
          y: f,
          width: item.width,
          height: item.height,
          fontSize: Math.hypot(c, d),
          fontName: item.fontName,
        })
      }
      pages.push({ number: n, width: view.width, height: view.height, items })
      page.cleanup()
    }
    return pages
  } finally {
    signal.removeEventListener("abort", stop)
    await task.destroy()
  }
}

/** Every page of a PDF with its positioned text, or `Unreadable` naming
 * `path` where pdf.js cannot open it. Interrupting the effect stops pdf.js. */
export function pdfPages(
  bytes: Uint8Array,
  path: string
): Effect.Effect<PdfPage[], ImportError> {
  return Effect.tryPromise({
    try: (signal) => readPages(bytes, signal),
    catch: (e) =>
      unreadable(
        path,
        `the PDF could not be read${e instanceof Error && e.message ? `: ${e.message}` : ""}`
      ),
  })
}

/** A page's text as lines: items whose baselines lie within `tolerance` of
 * each other are one line, read left to right, top line first; items are
 * joined by a space where a gap separates them. */
export function pageLines(page: PdfPage, tolerance = 2): string[] {
  const sorted = [...page.items].sort((a, b) => b.y - a.y || a.x - b.x)
  const lines: PdfItem[][] = []
  for (const item of sorted) {
    const line = lines.find((l) => Math.abs(l[0].y - item.y) <= tolerance)
    if (line) line.push(item)
    else lines.push([item])
  }
  return lines.map((line) => {
    line.sort((a, b) => a.x - b.x)
    let out = ""
    let end: number | undefined
    for (const item of line) {
      const gap = end === undefined ? 0 : item.x - end
      if (
        out &&
        gap > item.fontSize * 0.1 &&
        !/\s$/.test(out) &&
        !/^\s/.test(item.text)
      )
        out += " "
      out += item.text
      end = item.x + item.width
    }
    return out.replace(/\s+/g, " ").trim()
  })
}

/** A document's text: each page's lines, non-empty pages joined by a blank
 * line; undefined where no page has any. */
export function pdfText(pages: readonly PdfPage[]): string | undefined {
  const text = pages
    .map((p) => pageLines(p).filter(Boolean).join("\n"))
    .filter(Boolean)
    .join("\n\n")
  return text || undefined
}
