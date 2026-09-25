/** The collection grid: one page of records in a sheet that scrolls both ways,
 * the header row pinned to the top and the title column pinned to the left.
 * Columns are FIXED widths (a `colgroup` under `table-layout: fixed`): the
 * reader's drag override, else the column's own `meta.width`, else its flex
 * minimum; the table is their sum, stretched to the wrapper when the reader
 * wants tables full width. Rows never navigate on their own: the title cell
 * and the reference chips are the links, so a cell can be read, selected and
 * hovered without leaving the page. Instance state (order, visibility, drag
 * widths) is `useDataTable`'s, so the Columns menu drives this grid the way it
 * drives every other table. */

import { useEffect, useRef, useState, type ReactNode } from "react"
import { flexRender, type RowData } from "@tanstack/react-table"

import type { DataTableInstance } from "@/components/data-table/data-table"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "@/lib/utils"

/** A column that states neither a width nor a size still needs room. */
const DEFAULT_PX = 160
/** A drag can't crush a column below legibility. */
const RESIZE_MIN_PX = 60

export type GridDensity = "comfortable" | "compact"

function columnPx<TData extends RowData>(
  table: DataTableInstance<TData>,
  id: string
): number {
  const override = table.state.columnSizing[id]
  if (typeof override === "number" && override > 0) return override
  const meta = table.getColumn(id)?.columnDef.meta
  return meta?.width ?? meta?.size?.min ?? DEFAULT_PX
}

export function DataGrid<TData extends RowData>({
  table,
  density = "comfortable",
  fill = false,
  loading = false,
  empty,
  scrollKey,
  className,
}: {
  table: DataTableInstance<TData>
  density?: GridDensity
  /** Stretch the columns to the wrapper when their sum is narrower. */
  fill?: boolean
  /** Draw skeleton rows in place of the page. */
  loading?: boolean
  /** What an empty page says, under the header row. */
  empty?: ReactNode
  /** A change scrolls the grid back to its top-left (a new page). */
  scrollKey?: string | number
  className?: string
}) {
  const scrollRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    scrollRef.current?.scrollTo({ top: 0 })
  }, [scrollKey])

  const visible = table.getVisibleLeafColumns()
  const widths = visible.map((col) => columnPx(table, col.id))
  const total = widths.reduce((sum, px) => sum + px, 0)

  const [resizingId, setResizingId] = useState<string | null>(null)
  function startResize(e: React.PointerEvent, id: string, from: number) {
    e.preventDefault()
    e.stopPropagation()
    const startX = e.clientX
    setResizingId(id)
    const onMove = (ev: PointerEvent) => {
      const next = Math.max(
        RESIZE_MIN_PX,
        Math.round(from + (ev.clientX - startX))
      )
      table.setColumnSizing((old) => ({ ...old, [id]: next }))
    }
    const onUp = () => {
      window.removeEventListener("pointermove", onMove)
      setResizingId(null)
    }
    window.addEventListener("pointermove", onMove)
    window.addEventListener("pointerup", onUp, { once: true })
  }
  function clearResize(id: string) {
    table.setColumnSizing((old) => {
      if (!(id in old)) return old
      const next = { ...old }
      delete next[id]
      return next
    })
  }

  const rows = table.getRowModel().rows
  const rowH = density === "compact" ? "h-[30px]" : "h-[38px]"

  return (
    <div
      ref={scrollRef}
      data-slot="data-grid"
      className={cn("min-h-0 overflow-auto", className)}
    >
      <table
        className={cn(
          "table-fixed border-separate border-spacing-0",
          density === "compact" ? "text-[13px]" : "text-sm"
        )}
        style={{ width: fill ? `max(100%, ${total}px)` : total }}
      >
        <colgroup>
          {visible.map((col, i) => (
            <col key={col.id} style={{ width: widths[i] }} />
          ))}
        </colgroup>
        <thead>
          {table.getHeaderGroups().map((group) => (
            <tr key={group.id}>
              {group.headers.map((header, i) => {
                const meta = header.column.columnDef.meta
                return (
                  <th
                    key={header.id}
                    scope="col"
                    // The upward shadow is the header's cover: the scroller
                    // clips it while the row sits at the top, and wherever an
                    // engine rounds the pinned row a fraction of a pixel
                    // below the scroller's edge it paints over the strip a
                    // scrolled row would otherwise show through.
                    className={cn(
                      "group/th sticky top-0 z-[2] h-[34px] overflow-hidden border-b border-border bg-background bg-clip-border px-2.5 text-left text-[12.5px] font-medium whitespace-nowrap text-faint shadow-[0_-2px_0_var(--background)]",
                      i > 0 && "border-l",
                      i === 0 &&
                        "left-0 z-[3] shadow-[1px_0_0_var(--border),0_-2px_0_var(--background)]",
                      meta?.headerClassName
                    )}
                  >
                    {header.isPlaceholder
                      ? null
                      : flexRender(
                          header.column.columnDef.header,
                          header.getContext()
                        )}
                    <span
                      role="presentation"
                      className="group/resize absolute inset-y-0 right-0 z-10 flex w-2 cursor-col-resize touch-none justify-end select-none"
                      onPointerDown={(e) =>
                        startResize(e, header.column.id, widths[i])
                      }
                      onDoubleClick={(e) => {
                        e.stopPropagation()
                        clearResize(header.column.id)
                      }}
                      onClick={(e) => e.stopPropagation()}
                    >
                      <span
                        className={cn(
                          "h-full w-0.5 bg-primary opacity-0 transition-opacity group-hover/resize:opacity-60",
                          resizingId === header.column.id && "opacity-100"
                        )}
                      />
                    </span>
                  </th>
                )
              })}
            </tr>
          ))}
        </thead>
        <tbody>
          {loading ? (
            Array.from({ length: 12 }, (_, r) => (
              <tr key={`skeleton-${r}`}>
                {visible.map((col, i) => (
                  <td
                    key={col.id}
                    className={cn(
                      rowH,
                      "border-b border-border bg-background px-2.5",
                      i > 0 && "border-l",
                      i === 0 && "sticky left-0 z-[1]"
                    )}
                  >
                    <Skeleton className={cn("h-3.5", i ? "w-3/5" : "w-4/5")} />
                  </td>
                ))}
              </tr>
            ))
          ) : rows.length ? (
            rows.map((row) => (
              <tr
                key={row.id}
                data-slot="grid-row"
                className="group/row [&:hover>td]:bg-[color-mix(in_oklab,var(--background)_96%,var(--foreground))]"
              >
                {row.getVisibleCells().map((cell, i) => (
                  <td
                    key={cell.id}
                    className={cn(
                      rowH,
                      "overflow-hidden border-b border-border bg-background px-2.5 text-ellipsis whitespace-nowrap",
                      i > 0 && "border-l",
                      i === 0 &&
                        "sticky left-0 z-[1] font-medium shadow-[1px_0_0_var(--border)]",
                      cell.column.columnDef.meta?.cellClassName
                    )}
                  >
                    {flexRender(cell.column.columnDef.cell, cell.getContext())}
                  </td>
                ))}
              </tr>
            ))
          ) : (
            <tr>
              <td colSpan={visible.length || 1} className="p-0">
                {/* The empty state sits in the viewport, not across a table
                 * that may be wider than it. */}
                <div className="sticky left-0 w-[min(100%,640px)]">{empty}</div>
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  )
}
