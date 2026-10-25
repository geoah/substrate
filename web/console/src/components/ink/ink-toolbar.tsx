/** The ink editor's tools: what draws (pen, pencil, marker, eraser), in what
 * color and how wide, and undo. The paper and page controls belong to the
 * surface that has pages to offer, and arrive as `children`. */

import type { ReactNode } from "react"
import {
  EraserIcon,
  HighlighterIcon,
  PencilIcon,
  PenLineIcon,
  Redo2Icon,
  Undo2Icon,
  type LucideIcon,
} from "lucide-react"

import { Button } from "@/components/ui/button"
import { radioKeys, radioTabIndex } from "@/components/ui/segmented"
import { cn } from "@/lib/utils"

import {
  INK_COLORS,
  MARKER_COLORS,
  WIDTH_LABELS,
  type ToolState,
} from "./tools"
import type { InkInstrument } from "./use-ink-drawing"

const INSTRUMENTS: {
  value: InkInstrument
  label: string
  icon: LucideIcon
}[] = [
  { value: "pen", label: "Pen", icon: PenLineIcon },
  { value: "pencil", label: "Pencil", icon: PencilIcon },
  { value: "marker", label: "Highlighter", icon: HighlighterIcon },
  { value: "eraser", label: "Eraser", icon: EraserIcon },
]

function RadioRow<T extends string | number>({
  label,
  values,
  value,
  onChange,
  render,
  className,
}: {
  label: string
  values: readonly T[]
  value: T
  onChange: (value: T) => void
  render: (value: T, chosen: boolean) => ReactNode
  className?: string
}) {
  return (
    <div
      role="radiogroup"
      aria-label={label}
      className={cn("flex items-center gap-0.5", className)}
      onKeyDown={radioKeys(values, value, onChange)}
    >
      {values.map((v) => render(v, v === value))}
    </div>
  )
}

export function InkToolbar({
  tools,
  onTools,
  canUndo,
  canRedo,
  onUndo,
  onRedo,
  children,
  actions,
}: {
  tools: ToolState
  onTools: (tools: ToolState) => void
  canUndo: boolean
  canRedo: boolean
  onUndo: () => void
  onRedo: () => void
  /** The surface's own controls: the paper, the pages. */
  children?: ReactNode
  /** Save and cancel, at the row's end. */
  actions?: ReactNode
}) {
  const marker = tools.instrument === "marker"
  const colors = marker ? MARKER_COLORS : INK_COLORS
  const color = marker ? tools.markerColor : tools.inkColor
  const instruments = INSTRUMENTS.map((i) => i.value)
  return (
    <div
      data-slot="ink-toolbar"
      className="flex flex-wrap items-center gap-x-3 gap-y-2 rounded-lg border bg-background/95 px-2 py-1.5 shadow-xs backdrop-blur"
    >
      <RadioRow
        label="Tool"
        values={instruments}
        value={tools.instrument}
        onChange={(instrument) => onTools({ ...tools, instrument })}
        render={(value, chosen) => {
          const { label, icon: Icon } = INSTRUMENTS.find(
            (i) => i.value === value
          )!
          return (
            <Button
              key={value}
              type="button"
              role="radio"
              aria-checked={chosen}
              aria-label={label}
              title={label}
              tabIndex={radioTabIndex(instruments, tools.instrument, value)}
              variant={chosen ? "secondary" : "ghost"}
              size="icon-sm"
              className={cn(
                "pointer-coarse:size-11",
                chosen && "ring-1 ring-border-strong"
              )}
              onClick={() => onTools({ ...tools, instrument: value })}
            >
              <Icon />
            </Button>
          )
        }}
      />
      {tools.instrument !== "eraser" && (
        <>
          <RadioRow
            label={marker ? "Highlighter color" : "Ink color"}
            values={colors.map((c) => c.value as string)}
            value={color}
            onChange={(value) =>
              onTools(
                marker
                  ? { ...tools, markerColor: value }
                  : { ...tools, inkColor: value }
              )
            }
            render={(value, chosen) => (
              <button
                key={value}
                type="button"
                role="radio"
                aria-checked={chosen}
                aria-label={colors.find((c) => c.value === value)?.label}
                title={colors.find((c) => c.value === value)?.label}
                tabIndex={radioTabIndex(
                  colors.map((c) => c.value as string),
                  color,
                  value
                )}
                className={cn(
                  "grid size-7 place-items-center rounded-full outline-none focus-visible:ring-3 focus-visible:ring-ring/50 pointer-coarse:size-11",
                  chosen && "ring-2 ring-foreground/70"
                )}
                onClick={() =>
                  onTools(
                    marker
                      ? { ...tools, markerColor: value }
                      : { ...tools, inkColor: value }
                  )
                }
              >
                <span
                  className="size-4.5 rounded-full border border-black/10"
                  style={{ backgroundColor: value }}
                />
              </button>
            )}
          />
          <RadioRow
            label="Width"
            values={[0, 1, 2]}
            value={tools.width}
            onChange={(width) => onTools({ ...tools, width })}
            render={(value, chosen) => (
              <button
                key={value}
                type="button"
                role="radio"
                aria-checked={chosen}
                aria-label={WIDTH_LABELS[value]}
                title={WIDTH_LABELS[value]}
                tabIndex={radioTabIndex([0, 1, 2], tools.width, value)}
                className={cn(
                  "grid size-7 place-items-center rounded-md outline-none hover:bg-muted focus-visible:ring-3 focus-visible:ring-ring/50 pointer-coarse:size-11",
                  chosen && "bg-muted ring-1 ring-border-strong"
                )}
                onClick={() => onTools({ ...tools, width: value })}
              >
                <span
                  className="rounded-full bg-foreground"
                  style={{ width: 4 + value * 4, height: 4 + value * 4 }}
                />
              </button>
            )}
          />
        </>
      )}
      <div className="flex items-center gap-0.5">
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          className="pointer-coarse:size-11"
          aria-label="Undo"
          title="Undo"
          disabled={!canUndo}
          onClick={onUndo}
        >
          <Undo2Icon />
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          className="pointer-coarse:size-11"
          aria-label="Redo"
          title="Redo"
          disabled={!canRedo}
          onClick={onRedo}
        >
          <Redo2Icon />
        </Button>
      </div>
      {children}
      {actions && (
        <div className="ml-auto flex items-center gap-1.5">{actions}</div>
      )}
    </div>
  )
}
