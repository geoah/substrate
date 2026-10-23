/** Drawing on a note's pages: the toolbar above, one page at a time below,
 * at the width of the column. Undo and redo are Ctrl/Cmd+Z and
 * Shift+Ctrl/Cmd+Z as well as buttons. Nothing is written until Save, which
 * hands the whole document to the caller. */

import { useCallback, useState } from "react"
import { ChevronLeftIcon, ChevronRightIcon, FilePlusIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Segmented } from "@/components/ui/segmented"
import {
  INK_BACKGROUNDS,
  backgroundType,
  type InkBackgroundType,
  type InkDocument,
  type InkStroke,
} from "@/lib/ink"
import { inkShape } from "@/lib/ink-render"
import { cn } from "@/lib/utils"

import { InkPageView, StrokeShape } from "./ink-page"
import { InkToolbar } from "./ink-toolbar"
import { DEFAULT_TOOLS, penSettings, type ToolState } from "./tools"
import { useInkDrawing } from "./use-ink-drawing"
import { useInkHistory, useUndoKeys } from "./use-ink-history"

const PAPER_LABELS: Record<InkBackgroundType, string> = {
  blank: "Blank",
  lined: "Lined",
  grid: "Grid",
  dots: "Dots",
}

export function InkEditor({
  initial,
  onSave,
  onCancel,
  saving = false,
  saveLabel = "Save",
  note,
}: {
  initial: InkDocument
  onSave: (document: InkDocument) => void
  onCancel?: () => void
  saving?: boolean
  saveLabel?: string
  /** A line under the page: what saving does, or why it failed. */
  note?: React.ReactNode
}) {
  const { document, canUndo, canRedo, dirty, dispatch } = useInkHistory(initial)
  const [tools, setTools] = useState<ToolState>(DEFAULT_TOOLS)
  const [pageIndex, setPageIndex] = useState(0)
  const page = Math.min(pageIndex, document.pages.length - 1)
  useUndoKeys(dispatch)

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
    onGestureStart,
    onStroke,
    onErase,
  })

  const paper = backgroundType(document.pages[page]?.background)

  return (
    <div data-slot="ink-editor" className="flex flex-col gap-3">
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
              {onCancel && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={onCancel}
                  disabled={saving}
                >
                  Cancel
                </Button>
              )}
              <Button
                type="button"
                size="sm"
                disabled={saving || !dirty}
                onClick={() => onSave(document)}
              >
                {saving ? "Saving…" : saveLabel}
              </Button>
            </>
          }
        >
          <Segmented
            label="Paper"
            value={paper}
            options={INK_BACKGROUNDS.map((b) => ({
              value: b,
              label: PAPER_LABELS[b],
            }))}
            onChange={(background) =>
              dispatch({ type: "background", page, background })
            }
          />
        </InkToolbar>
      </div>
      <div className="max-w-3xl overflow-hidden rounded-md border shadow-sm">
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
      <PageNav
        page={page}
        pages={document.pages.length}
        onPage={setPageIndex}
        onAdd={() => {
          dispatch({ type: "addPage", background: paper })
          setPageIndex(document.pages.length)
        }}
      />
      {note && <p className="text-xs text-muted-foreground">{note}</p>}
    </div>
  )
}

export function PageNav({
  page,
  pages,
  onPage,
  onAdd,
}: {
  page: number
  pages: number
  onPage: (page: number) => void
  onAdd?: () => void
}) {
  return (
    <div className="flex max-w-3xl items-center justify-center gap-2 text-sm">
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
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
        aria-label="Next page"
        disabled={page >= pages - 1}
        onClick={() => onPage(page + 1)}
      >
        <ChevronRightIcon />
      </Button>
      {onAdd && (
        <Button type="button" variant="outline" size="sm" onClick={onAdd}>
          <FilePlusIcon data-icon="inline-start" />
          Add page
        </Button>
      )}
    </div>
  )
}
