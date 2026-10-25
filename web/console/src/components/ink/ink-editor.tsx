/** Drawing mode: the note fills the window, with its name, save state and
 * Save at the top, the tools under them, the page in a scrolling area that
 * zooms, and the pages at the bottom. On a tablet the pen draws and a finger
 * scrolls, unless "Draw with finger" is on. Undo and redo are Ctrl/Cmd+Z and
 * Shift+Ctrl/Cmd+Z as well as buttons. Nothing is written until Save, which
 * hands the whole document to the caller; leaving with unsaved ink, by
 * Cancel, Escape, a link or the browser's back, asks first. */

import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from "react"
import {
  ChevronLeftIcon,
  ChevronRightIcon,
  FilePlusIcon,
  HandIcon,
  ZoomInIcon,
  ZoomOutIcon,
} from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  backgroundType,
  inkOutside,
  type InkDocument,
  type InkStroke,
} from "@/lib/ink"
import { inkShape } from "@/lib/ink-render"
import { readInkSetup, writeInkSetup } from "@/lib/ink-setup"
import { cn } from "@/lib/utils"

import { InkPageView, StrokeShape } from "./ink-page"
import { InkToolbar } from "./ink-toolbar"
import { useDiscardGuard, type DiscardWording } from "./discard-guard"
import { PageSetup } from "./page-setup"
import { DEFAULT_TOOLS, penSettings, type ToolState } from "./tools"
import { useInkDrawing } from "./use-ink-drawing"
import { useInkHistory, useUndoKeys } from "./use-ink-history"

/** Zoom steps around "fit": 1 is the page at the width of the window, up to
 * a reading width on a wide screen. */
const ZOOMS = [0.5, 0.75, 1, 1.25, 1.5, 2, 3]
const FIT_MAX = 960

export function InkEditor({
  initial,
  onSave,
  onCancel,
  saving = false,
  saveLabel = "Save",
  note,
  heading,
  allowPristineSave = false,
  changed = false,
  discard,
}: {
  initial: InkDocument
  onSave: (document: InkDocument) => void
  /** Called once leaving is settled: at once with nothing unsaved, else after
   * the person agrees to discard. */
  onCancel?: () => void
  saving?: boolean
  saveLabel?: string
  /** A line under the tools: what saving does, or why it failed. */
  note?: ReactNode
  /** The note's name at the top: an input for a new note, text otherwise. */
  heading?: ReactNode
  /** Save is offered before any change: a new note is worth saving blank. */
  allowPristineSave?: boolean
  /** Something besides the ink is unsaved, such as a new note's name. */
  changed?: boolean
  /** How the question before discarding unsaved ink is put. */
  discard: DiscardWording
}) {
  const { document, canUndo, canRedo, dirty, dispatch } = useInkHistory(
    initial,
    saving
  )
  const [tools, setTools] = useState<ToolState>(DEFAULT_TOOLS)
  const [fingerDraws, setFingerDraws] = useState(
    () => readInkSetup().fingerDraws
  )
  const [pageIndex, setPageIndex] = useState(0)
  const [zoom, setZoom] = useState(1)
  const page = Math.min(pageIndex, document.pages.length - 1)
  useUndoKeys(dispatch)

  // While a save runs nothing is asked: a new note's save ends by moving to
  // the record it created.
  const { confirm, guard } = useDiscardGuard(
    (dirty || changed) && !saving,
    discard
  )
  const cancel = () => {
    if (onCancel && !saving) confirm(onCancel)
  }

  // The window is the editor's: the page under it does not scroll, and
  // focus starts in the editor so Escape and Tab stay in it.
  const root = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const body = globalThis.document.body
    const overflow = body.style.overflow
    body.style.overflow = "hidden"
    root.current?.focus({ preventScroll: true })
    return () => {
      body.style.overflow = overflow
    }
  }, [])
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    // A menu or dialog opened from the editor sits outside it in the page
    // and answers its own keys.
    if (!event.currentTarget.contains(event.target as Node)) return
    if (event.key === "Escape") {
      // The question opens before this key reaches the page, where the
      // question's own Escape would close it again at once.
      event.preventDefault()
      event.stopPropagation()
      cancel()
    } else if (event.key === "Tab") {
      trapTab(event)
    }
  }

  const scroller = useRef<HTMLDivElement>(null)
  const [fit, setFit] = useState(FIT_MAX)
  useEffect(() => {
    const el = scroller.current
    if (!el || typeof ResizeObserver === "undefined") return
    const observer = new ResizeObserver(([entry]) =>
      setFit(Math.min(FIT_MAX, Math.max(240, entry.contentRect.width - 32)))
    )
    observer.observe(el)
    return () => observer.disconnect()
  }, [])

  const onGestureStart = useCallback(
    () => dispatch({ type: "gesture" }),
    [dispatch]
  )
  const onStroke = useCallback(
    (stroke: InkStroke) => dispatch({ type: "stroke", page, stroke }),
    [dispatch, page]
  )
  const onErase = useCallback(
    (x: number, y: number) => dispatch({ type: "erase", page, x, y }),
    [dispatch, page]
  )
  const { live, handlers } = useInkDrawing({
    settings: penSettings(tools),
    enabled: !saving,
    fingerDraws,
    onGestureStart,
    onStroke,
    onErase,
  })

  const paper = backgroundType(document.pages[page]?.background)
  const zoomAt = ZOOMS.indexOf(zoom)
  const status = saving ? "Saving…" : dirty ? "Unsaved changes" : undefined

  return (
    <div
      ref={root}
      data-slot="ink-editor"
      role="dialog"
      aria-modal="true"
      aria-label="Drawing"
      tabIndex={-1}
      onKeyDown={onKeyDown}
      className="fixed inset-0 z-50 flex flex-col bg-muted outline-none"
    >
      <header className="flex items-center gap-3 border-b bg-background px-3 pt-[max(env(safe-area-inset-top),0.5rem)] pb-2">
        <div className="min-w-0 flex-1">{heading}</div>
        {status && (
          <span
            className="shrink-0 text-xs text-muted-foreground"
            aria-live="polite"
          >
            {status}
          </span>
        )}
        {onCancel && (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={cancel}
            disabled={saving}
          >
            Cancel
          </Button>
        )}
        <Button
          type="button"
          size="sm"
          disabled={saving || (!dirty && !allowPristineSave)}
          onClick={() => onSave(document)}
        >
          {saving ? "Saving…" : saveLabel}
        </Button>
      </header>
      <div className="border-b bg-background px-3 py-1.5">
        <InkToolbar
          tools={tools}
          onTools={setTools}
          canUndo={canUndo}
          canRedo={canRedo}
          onUndo={() => dispatch({ type: "undo" })}
          onRedo={() => dispatch({ type: "redo" })}
        >
          <PageSetup
            size={document.page}
            onSize={(size) => dispatch({ type: "resize", size })}
            background={paper}
            onBackground={(background) =>
              dispatch({ type: "background", page, background })
            }
            pages={document.pages.length}
            inkOutside={inkOutside(document, document.page)}
            disabled={saving}
          />
          <Button
            type="button"
            variant={fingerDraws ? "secondary" : "ghost"}
            size="sm"
            aria-pressed={fingerDraws}
            title="A finger draws instead of scrolling the page"
            className={cn(fingerDraws && "ring-1 ring-border-strong")}
            onClick={() => {
              const next = !fingerDraws
              setFingerDraws(next)
              writeInkSetup({ fingerDraws: next })
            }}
          >
            <HandIcon data-icon="inline-start" />
            Draw with finger
          </Button>
          <div
            role="group"
            aria-label="Zoom"
            className="flex items-center gap-0.5"
          >
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label="Zoom out"
              disabled={zoomAt <= 0}
              onClick={() => setZoom(ZOOMS[zoomAt - 1])}
            >
              <ZoomOutIcon />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              aria-label="Fit the page to the window"
              onClick={() => setZoom(1)}
              className="min-w-14 tabular-nums"
            >
              {zoom === 1 ? "Fit" : `${Math.round(zoom * 100)}%`}
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label="Zoom in"
              disabled={zoomAt >= ZOOMS.length - 1}
              onClick={() => setZoom(ZOOMS[zoomAt + 1])}
            >
              <ZoomInIcon />
            </Button>
          </div>
        </InkToolbar>
        {note && <p className="mt-1.5 px-1 text-xs">{note}</p>}
      </div>
      <div ref={scroller} className="min-h-0 flex-1 overflow-auto">
        <div
          className="mx-auto my-4 overflow-hidden rounded-md border bg-background shadow-sm"
          style={{ width: Math.round(fit * zoom) }}
        >
          <InkPageView
            document={document}
            pageIndex={page}
            className={cn(
              "touch-none select-none",
              tools.instrument === "eraser" ? "cursor-cell" : "cursor-crosshair"
            )}
            surfaceProps={handlers}
            label={`Page ${page + 1} of ${document.pages.length}, drawing`}
          >
            {live && <StrokeShape shape={inkShape(live)} />}
          </InkPageView>
        </div>
      </div>
      <footer className="border-t bg-background px-3 pt-1.5 pb-[max(env(safe-area-inset-bottom),0.375rem)]">
        <PageNav
          page={page}
          pages={document.pages.length}
          onPage={setPageIndex}
          addDisabled={saving}
          onAdd={() => {
            dispatch({ type: "addPage", background: paper })
            setPageIndex(document.pages.length)
          }}
        />
      </footer>
      {guard}
    </div>
  )
}

const TABBABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

/** Tab past the last control comes back to the first, and Shift+Tab before
 * the first goes to the last: the page behind the editor is out of reach. */
function trapTab(event: KeyboardEvent<HTMLDivElement>) {
  const items = [...event.currentTarget.querySelectorAll<HTMLElement>(TABBABLE)]
  if (!items.length) return
  const first = items[0]
  const last = items[items.length - 1]
  const at = globalThis.document.activeElement
  if (event.shiftKey && (at === first || at === event.currentTarget)) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && at === last) {
    event.preventDefault()
    first.focus()
  }
}

export function PageNav({
  page,
  pages,
  onPage,
  onAdd,
  addDisabled = false,
}: {
  page: number
  pages: number
  onPage: (page: number) => void
  onAdd?: () => void
  addDisabled?: boolean
}) {
  return (
    <div className="flex items-center justify-center gap-2 text-sm">
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        className="pointer-coarse:size-11"
        aria-label="Previous page"
        disabled={page <= 0}
        onClick={() => onPage(page - 1)}
      >
        <ChevronLeftIcon />
      </Button>
      <span className="min-w-24 text-center text-muted-foreground tabular-nums">
        Page {page + 1} of {pages}
      </span>
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        className="pointer-coarse:size-11"
        aria-label="Next page"
        disabled={page >= pages - 1}
        onClick={() => onPage(page + 1)}
      >
        <ChevronRightIcon />
      </Button>
      {onAdd && (
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={addDisabled}
          onClick={onAdd}
        >
          <FilePlusIcon data-icon="inline-start" />
          Add page
        </Button>
      )}
    </div>
  )
}
