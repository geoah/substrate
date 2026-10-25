/** A PDF with handwriting over it. PDF.js draws each page, the annotations the
 * PDF already carries included (ink another app wrote into it, Inkidian's or
 * Preview's), and the record's own `.inkd` layer is drawn on top: page n of
 * the layer over page n of the PDF. Annotating draws on the layer and never
 * touches the PDF's bytes.
 *
 * The layer's coordinates are Inkidian's for ink on a PDF page (§9.3): the
 * page's top-left corner as displayed, y down, at 1240 units per 210 mm, so
 * a stroke lands where it would in the app. */

import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { DownloadIcon, PenLineIcon, RefreshCwIcon } from "lucide-react"
import type { PDFDocumentProxy } from "pdfjs-dist"

import { SectionHead } from "@/components/identity/section-head"
import { useRecordPatch } from "@/components/property-sheet/use-record-patch"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import {
  blobQueryOptions,
  putBlob,
  readBlobInfo,
  saveBytes,
} from "@/lib/api/blobs"
import type { SubstrateRecord } from "@/lib/api/types"
import {
  INK_MEDIA_TYPE,
  inkSummary,
  inkText,
  newInkDocument,
  type InkDocument,
  type InkStroke,
} from "@/lib/ink"
import {
  byteSize,
  inkCounts,
  inkFileName,
  inkFromBytes,
  withPageCount,
} from "@/lib/ink-record"
import { inkShape } from "@/lib/ink-render"
import { recordTitle } from "@/lib/format"
import type { PropSpec } from "@/lib/record-schema"

import { InkPageView, StrokeShape } from "./ink-page"
import { inkSaveFailure, type InkSaveFailure } from "./save-failure"
import { SaveFailureNote } from "./save-failure-note"
import { InkToolbar } from "./ink-toolbar"
import { DEFAULT_TOOLS, penSettings, type ToolState } from "./tools"
import { useInkDrawing, type PenSettings } from "./use-ink-drawing"
import { useInkHistory, useUndoKeys, type InkAction } from "./use-ink-history"

/** Layer units per PDF point: 1240 units are 210 mm, which is 595.2756 pt. */
export const LAYER_PER_POINT = 1240 / 595.2756

/** PDF.js, loaded the first time a PDF is shown: it is most of a megabyte
 * and only this section needs it. */
async function loadPdf(bytes: ArrayBuffer) {
  const [pdfjs, worker] = await Promise.all([
    import("pdfjs-dist"),
    import("pdfjs-dist/build/pdf.worker.min.mjs?url"),
  ])
  pdfjs.GlobalWorkerOptions.workerSrc = worker.default
  // PDF.js transfers the buffer to its worker, so it gets a copy and the
  // cached bytes stay readable.
  return pdfjs.getDocument({ data: new Uint8Array(bytes.slice(0)) })
}

interface PdfPageSize {
  /** Points, as displayed: rotation applied. */
  width: number
  height: number
}

function usePdf(bytes: ArrayBuffer | undefined) {
  const [state, setState] = useState<
    | { pdf: PDFDocumentProxy; sizes: PdfPageSize[] }
    | { error: string }
    | undefined
  >()
  useEffect(() => {
    if (!bytes) return
    let cancelled = false
    let task: Awaited<ReturnType<typeof loadPdf>> | undefined
    loadPdf(bytes)
      .then((loading) => {
        task = loading
        return loading.promise
      })
      .then(async (pdf) => {
        const sizes: PdfPageSize[] = []
        for (let i = 1; i <= pdf.numPages; i++) {
          const viewport = (await pdf.getPage(i)).getViewport({ scale: 1 })
          sizes.push({ width: viewport.width, height: viewport.height })
        }
        if (!cancelled) setState({ pdf, sizes })
      })
      .catch((e: unknown) => {
        if (!cancelled)
          setState({ error: e instanceof Error ? e.message : String(e) })
      })
    return () => {
      cancelled = true
      void task?.destroy()
    }
  }, [bytes])
  return state
}

/** One PDF page drawn into a canvas at the width it is shown, sharp on a
 * high-density screen, with the annotations the PDF carries. */
function PdfCanvas({
  pdf,
  pageNumber,
  size,
}: {
  pdf: PDFDocumentProxy
  pageNumber: number
  size: PdfPageSize
}) {
  const canvas = useRef<HTMLCanvasElement>(null)
  const [width, setWidth] = useState(0)
  useEffect(() => {
    const el = canvas.current?.parentElement
    if (!el) return
    const observer = new ResizeObserver(([entry]) =>
      setWidth(Math.round(entry.contentRect.width))
    )
    observer.observe(el)
    return () => observer.disconnect()
  }, [])
  useEffect(() => {
    const el = canvas.current
    if (!el || !width) return
    let task: { cancel: () => void; promise: Promise<void> } | undefined
    let cancelled = false
    void (async () => {
      const [{ AnnotationMode }, page] = await Promise.all([
        import("pdfjs-dist"),
        pdf.getPage(pageNumber),
      ])
      if (cancelled) return
      const scale = (width / size.width) * (window.devicePixelRatio || 1)
      const viewport = page.getViewport({ scale })
      el.width = Math.floor(viewport.width)
      el.height = Math.floor(viewport.height)
      task = page.render({
        canvas: el,
        viewport,
        annotationMode: AnnotationMode.ENABLE,
      })
      await task.promise.catch(() => undefined)
    })()
    return () => {
      cancelled = true
      task?.cancel()
    }
  }, [pdf, pageNumber, size.width, width])
  return (
    <canvas
      ref={canvas}
      className="block h-auto w-full bg-white"
      style={{ aspectRatio: `${size.width} / ${size.height}` }}
      aria-label={`PDF page ${pageNumber}`}
      role="img"
    />
  )
}

/** One page of the layer over one PDF page, drawable while annotating. */
function LayerPage({
  layer,
  pageIndex,
  size,
  settings,
  annotating,
  dispatch,
}: {
  layer: InkDocument
  pageIndex: number
  size: PdfPageSize
  settings: PenSettings
  annotating: boolean
  dispatch: (action: InkAction) => void
}) {
  const onGestureStart = useCallback(
    () => dispatch({ type: "gesture" }),
    [dispatch]
  )
  const onStroke = useCallback(
    (stroke: InkStroke) =>
      dispatch({ type: "stroke", page: pageIndex, stroke }),
    [dispatch, pageIndex]
  )
  const onErase = useCallback(
    (x: number, y: number) =>
      dispatch({ type: "erase", page: pageIndex, x, y }),
    [dispatch, pageIndex]
  )
  const { live, handlers } = useInkDrawing({
    settings,
    enabled: annotating,
    onGestureStart,
    onStroke,
    onErase,
  })
  return (
    <InkPageView
      document={layer}
      pageIndex={pageIndex}
      transparent
      viewBox={{
        x: 0,
        y: 0,
        width: size.width * LAYER_PER_POINT,
        height: size.height * LAYER_PER_POINT,
      }}
      className={
        annotating
          ? settings.instrument === "eraser"
            ? "absolute inset-0 cursor-cell touch-none select-none"
            : "absolute inset-0 cursor-crosshair touch-none select-none"
          : "pointer-events-none absolute inset-0"
      }
      surfaceProps={annotating ? handlers : undefined}
      label={`Ink on page ${pageIndex + 1}`}
    >
      {live && <StrokeShape shape={inkShape(live)} />}
    </InkPageView>
  )
}

/** A fresh layer for a PDF: one blank page per PDF page, sized like the
 * first. */
function newLayer(sizes: PdfPageSize[]): InkDocument {
  const first = sizes[0] ?? { width: 595.2756, height: 841.8898 }
  return withPageCount(
    newInkDocument({
      size: {
        width: Math.round(first.width * LAYER_PER_POINT * 100) / 100,
        height: Math.round(first.height * LAYER_PER_POINT * 100) / 100,
      },
    }),
    sizes.length
  )
}

function MarkupPages({
  pdf,
  sizes,
  layer,
  annotating,
  tools,
  dispatch,
}: {
  pdf: PDFDocumentProxy
  sizes: PdfPageSize[]
  layer: InkDocument
  annotating: boolean
  tools: ToolState
  dispatch: (action: InkAction) => void
}) {
  const settings = penSettings(tools)
  return (
    <ol className="grid max-w-3xl gap-4" data-slot="pdf-pages">
      {sizes.map((size, i) => (
        <li key={i} className="flex flex-col gap-1">
          <div className="relative overflow-hidden rounded-md border shadow-sm">
            <PdfCanvas pdf={pdf} pageNumber={i + 1} size={size} />
            {layer.pages[i] && (
              <LayerPage
                layer={layer}
                pageIndex={i}
                size={size}
                settings={settings}
                annotating={annotating}
                dispatch={dispatch}
              />
            )}
          </div>
          <span className="text-center text-xs text-muted-foreground tabular-nums">
            Page {i + 1} of {sizes.length}
          </span>
        </li>
      ))}
    </ol>
  )
}

/** The editing half: the layer's history lives here, so cancelling throws it
 * away whole. */
function MarkupEditor({
  pdf,
  sizes,
  initial,
  saving,
  onSave,
  onCancel,
  note,
}: {
  pdf: PDFDocumentProxy
  sizes: PdfPageSize[]
  initial: InkDocument
  saving: boolean
  onSave: (layer: InkDocument) => void
  onCancel: () => void
  note?: React.ReactNode
}) {
  const { document, canUndo, canRedo, dirty, dispatch } = useInkHistory(initial)
  const [tools, setTools] = useState<ToolState>(DEFAULT_TOOLS)
  useUndoKeys(dispatch)
  return (
    <div className="flex flex-col gap-3">
      <div className="sticky top-2 z-10">
        <InkToolbar
          tools={tools}
          onTools={setTools}
          canUndo={canUndo}
          canRedo={canRedo}
          onUndo={() => dispatch({ type: "undo" })}
          onRedo={() => dispatch({ type: "redo" })}
          actions={
            <>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={onCancel}
                disabled={saving}
              >
                Cancel
              </Button>
              <Button
                type="button"
                size="sm"
                disabled={saving || !dirty}
                onClick={() => onSave(document)}
              >
                {saving ? "Saving…" : "Save"}
              </Button>
            </>
          }
        />
      </div>
      {note && <p className="text-xs text-muted-foreground">{note}</p>}
      <MarkupPages
        pdf={pdf}
        sizes={sizes}
        layer={document}
        annotating={!saving}
        tools={tools}
        dispatch={dispatch}
      />
    </div>
  )
}

const noDispatch = () => undefined

export function PdfMarkupSection({
  record,
  document: documentSpec,
  ink: inkSpec,
  readOnly,
}: {
  record: SubstrateRecord
  document: PropSpec
  /** The layer's property; without one the PDF is shown as it is. */
  ink?: PropSpec
  readOnly: boolean
}) {
  const pdfBlob = readBlobInfo(record.properties[documentSpec.name])
  const inkBlob = inkSpec
    ? readBlobInfo(record.properties[inkSpec.name])
    : undefined
  const pdfBytes = useQuery(blobQueryOptions(pdfBlob?.digest))
  const inkBytes = useQuery(blobQueryOptions(inkBlob?.digest))
  const loaded = usePdf(pdfBytes.data)
  // The layer and the version annotating began from, held while it is open:
  // the page re-reads a record that moves under the reader, and a save
  // asserting the newer version would silently replace the other save, while
  // re-deriving the layer would unmount the drawing.
  const [session, setSession] = useState<
    { base: number; layer: InkDocument } | undefined
  >()
  const annotating = session !== undefined
  const patch = useRecordPatch(record, session?.base)
  const [saving, setSaving] = useState(false)
  const [failure, setFailure] = useState<InkSaveFailure | undefined>()

  const read = useMemo(
    () => (inkBytes.data ? inkFromBytes(inkBytes.data) : undefined),
    [inkBytes.data]
  )
  const sizes = loaded && "sizes" in loaded ? loaded.sizes : undefined
  const named = Boolean(inkBlob)
  // A blank layer only where the record names none. A stored layer still
  // loading, or that failed to load or to read, gets no layer at all, so
  // nothing can be drawn and saved over it.
  const layer = useMemo(() => {
    if (!sizes) return undefined
    if (read?.ok) return withPageCount(read.document, sizes.length)
    return named ? undefined : newLayer(sizes)
  }, [read, sizes, named])
  const fileName = inkFileName(recordTitle(record.properties), "markup")

  const save = async (next: InkDocument) => {
    if (!inkSpec) return
    setSaving(true)
    setFailure(undefined)
    try {
      const info = await putBlob(inkText(next), {
        name: fileName,
        mediaType: INK_MEDIA_TYPE,
      })
      await patch.mutateAsync({ [inkSpec.name]: info.digest })
      setSession(undefined)
    } catch (e) {
      setFailure(inkSaveFailure(e, next))
    } finally {
      setSaving(false)
    }
  }

  const summary = read?.ok ? inkSummary(read.document) : undefined
  const hint = [
    pdfBlob && `${pdfBlob.name ?? "PDF"}, ${byteSize(pdfBlob.size)}`,
    sizes && `${sizes.length} ${sizes.length === 1 ? "page" : "pages"}`,
    inkSpec &&
      (summary
        ? `ink layer: ${inkCounts(summary).split(", ")[1]}`
        : !inkBlob && "no ink layer yet"),
  ]
    .filter(Boolean)
    .join(" · ")

  let body: React.ReactNode
  if (!pdfBlob) {
    body = <p className="text-sm text-muted-foreground">No PDF yet.</p>
  } else if (pdfBytes.isError || (loaded && "error" in loaded)) {
    body = (
      <p className="text-sm text-destructive">
        The PDF didn’t load:{" "}
        {pdfBytes.error?.message ??
          (loaded && "error" in loaded ? loaded.error : "")}
      </p>
    )
  } else if (session && loaded && "pdf" in loaded && sizes) {
    body = (
      <MarkupEditor
        pdf={loaded.pdf}
        sizes={sizes}
        initial={session.layer}
        saving={saving}
        onSave={(next) => void save(next)}
        onCancel={() => {
          setFailure(undefined)
          setSession(undefined)
        }}
        note={
          failure ? (
            <SaveFailureNote failure={failure} fileName={fileName} />
          ) : (
            "The PDF stays as it is. Saving stores the ink as a separate .inkd file over it."
          )
        }
      />
    )
  } else if (inkBlob && inkBytes.isError) {
    body = (
      <div className="flex flex-wrap items-center gap-2">
        <p className="text-sm text-destructive">
          The ink layer didn’t load, so annotating is off until it does:{" "}
          {inkBytes.error.message}
        </p>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => void inkBytes.refetch()}
        >
          <RefreshCwIcon data-icon="inline-start" />
          Retry
        </Button>
      </div>
    )
  } else if (read && !read.ok) {
    body = <p className="text-sm text-destructive">{read.message}</p>
  } else if (!loaded || !layer || !sizes || (inkBlob && inkBytes.isPending)) {
    body = <Skeleton className="aspect-[595/842] w-full max-w-3xl" />
  } else if ("pdf" in loaded) {
    body = (
      <>
        <MarkupPages
          pdf={loaded.pdf}
          sizes={sizes}
          layer={layer}
          annotating={false}
          tools={DEFAULT_TOOLS}
          dispatch={noDispatch}
        />
        {failure && (
          <p className="mt-2 text-xs text-destructive">{failure.message}</p>
        )}
      </>
    )
  }

  return (
    <section data-slot="pdf-markup" aria-label={documentSpec.label}>
      <SectionHead
        title={documentSpec.label}
        hint={hint}
        actions={
          annotating ? undefined : (
            <>
              {pdfBytes.data && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() =>
                    saveBytes(
                      pdfBytes.data,
                      pdfBlob?.name ??
                        `${recordTitle(record.properties) || "document"}.pdf`,
                      "application/pdf"
                    )
                  }
                >
                  <DownloadIcon data-icon="inline-start" />
                  Download PDF
                </Button>
              )}
              {inkBytes.data && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() =>
                    saveBytes(inkBytes.data, fileName, INK_MEDIA_TYPE)
                  }
                >
                  <DownloadIcon data-icon="inline-start" />
                  Download ink layer
                </Button>
              )}
              {!readOnly && inkSpec && layer && (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => setSession({ base: record.version, layer })}
                >
                  <PenLineIcon data-icon="inline-start" />
                  Annotate
                </Button>
              )}
            </>
          )
        }
      />
      {body}
    </section>
  )
}
