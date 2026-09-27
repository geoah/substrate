/** The collection grid: one page of records in a sheet that scrolls both ways,
 * the header row pinned to the top and the title column pinned to the left.
 * Columns are FIXED widths (a `colgroup` under `table-layout: fixed`): the
 * reader's drag override, else the column's own `meta.width`, else its flex
 * minimum; the table is their sum, stretched to the wrapper when the reader
 * wants tables full width. Rows never navigate on their own: the title cell
 * and the reference chips are the links, so a cell can be read, selected and
 * hovered without leaving the page. Instance state (order, visibility, drag
 * widths) is `useDataTable`'s, so the Columns menu drives this grid the way it
 * drives every other table.
 *
 * GROUPED (`groups`), the rows are cut wherever their group changes, and each
 * run is its own `tbody` under a head row that names the group, counts it and
 * folds it. The rows arrive in group order (the server sorts by the grouped
 * property first), so the grid only draws the cuts. */

import { useEffect, useRef, useState, type ReactNode } from "react"
import { flexRender, type Row, type RowData } from "@tanstack/react-table"
import { ChevronRightIcon } from "lucide-react"

import {
  ariaSortOf,
  type DataTableFeatures,
  type DataTableInstance,
} from "@/components/data-table/data-table"
import { Skeleton } from "@/components/ui/skeleton"
import { groupSegments } from "@/lib/grouping"
import { cn } from "@/lib/utils"

/** A column that states neither a width nor a size still needs room. */
const DEFAULT_PX = 160
/** The pinned header row's height; the scroll padding clears it. */
const HEADER_PX = 34
/** A drag can't crush a column below legibility. */
const RESIZE_MIN_PX = 60

export type GridDensity = "comfortable" | "compact"

/** How a grouped grid cuts and heads its rows. */
export interface GridGroups<TData> {
  /** The group a row is in; rows of one group arrive together. */
  keyOf: (row: TData) => string
  /** What the head shows: the group's value and its count. `at` says where
   * the run sits on the page and how many rows it has here, so a head can
   * say its group runs on from, or on to, another page. */
  head: (
    key: string,
    at: { first: boolean; last: boolean; rows: number }
  ) => ReactNode
  /** The head's name for the fold control ("Priority: High"). */
  label: (key: string) => string
  collapsed: ReadonlySet<string>
  onToggle: (key: string) => void
}

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
  marks,
  label,
  groups,
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
  /** Rows (by id) that just changed under the reader: tinted while `fresh`,
   * easing back while `fading`. */
  marks?: ReadonlyMap<string, "fresh" | "fading">
  /** The sheet's accessible name: the collection's display plural. */
  label: string
  /** Cut the rows into groups, each under a head. */
  groups?: GridGroups<TData>
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

  function drawRow(row: Row<DataTableFeatures, TData>) {
    const mark = marks?.get(row.id)
    return (
      <tr
        key={row.id}
        data-slot="grid-row"
        data-changed={mark}
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
              cell.column.columnDef.meta?.cellClassName,
              // Opaque, because the title column is pinned over the
              // cells that scroll under it.
              mark === "fresh" &&
                "bg-[color-mix(in_oklab,var(--background)_86%,var(--primary))]",
              mark === "fading" &&
                "transition-[background-color] duration-[1500ms] ease-out motion-reduce:transition-none"
            )}
          >
            {flexRender(cell.column.columnDef.cell, cell.getContext())}
          </td>
        ))}
      </tr>
    )
  }

  const segments =
    groups && !loading && rows.length
      ? groupSegments(rows, (row) => groups.keyOf(row.original))
      : undefined

  return (
    // Focusable, so a keyboard alone can scroll the sheet both ways; the
    // scroll padding keeps a focused cell clear of the pinned header row and
    // title column, which would otherwise cover it.
    <div
      ref={scrollRef}
      role="region"
      aria-label={label}
      tabIndex={0}
      data-slot="data-grid"
      className={cn(
        "min-h-0 overflow-auto outline-none focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:ring-inset",
        className
      )}
      style={{ scrollPaddingTop: HEADER_PX, scrollPaddingLeft: widths[0] ?? 0 }}
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
                    aria-sort={ariaSortOf(header.column.getIsSorted())}
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
        {segments ? (
          segments.map((segment, n) => {
            const folded = groups!.collapsed.has(segment.key)
            const name = groups!.label(segment.key)
            return (
              <tbody key={`${n}:${segment.key}`} data-slot="grid-group">
                <tr>
                  <th
                    scope="rowgroup"
                    colSpan={visible.length || 1}
                    className={cn(
                      rowH,
                      "border-b border-border bg-panel px-0 text-left text-[13px] font-medium"
                    )}
                  >
                    {/* Pinned to the left, so the head stays in view while
                        the sheet scrolls sideways. */}
                    <div className="sticky left-0 flex w-max max-w-[min(100vw,720px)] items-center gap-2 px-2.5">
                      <button
                        type="button"
                        aria-expanded={!folded}
                        aria-label={`${folded ? "Show" : "Hide"} ${name}`}
                        className="hit-area grid size-5 shrink-0 cursor-pointer place-items-center rounded-[4px] text-faint outline-none hover:bg-hover hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50"
                        onClick={() => groups!.onToggle(segment.key)}
                      >
                        <ChevronRightIcon
                          aria-hidden
                          className={cn(
                            "size-3.5 transition-transform motion-reduce:transition-none",
                            !folded && "rotate-90"
                          )}
                        />
                      </button>
                      {groups!.head(segment.key, {
                        first: n === 0,
                        last: n === segments.length - 1,
                        rows: segment.rows.length,
                      })}
                    </div>
                  </th>
                </tr>
                {!folded && segment.rows.map(drawRow)}
              </tbody>
            )
          })
        ) : (
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
                      <Skeleton
                        className={cn("h-3.5", i ? "w-3/5" : "w-4/5")}
                      />
                    </td>
                  ))}
                </tr>
              ))
            ) : rows.length ? (
              rows.map(drawRow)
            ) : (
              <tr>
                <td colSpan={visible.length || 1} className="p-0">
                  {/* The empty state sits in the viewport, not across a table
                   * that may be wider than it. */}
                  <div className="sticky left-0 w-[min(100%,640px)]">
                    {empty}
                  </div>
                </td>
              </tr>
            )}
          </tbody>
        )}
      </table>
    </div>
  )
}
