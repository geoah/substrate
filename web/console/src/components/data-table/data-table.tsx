/* eslint-disable react-refresh/only-export-components -- the instance hook
 * and its renderer are one module by design; nothing here hot-reloads alone. */

/** THE table system (owner ruling, 2026-08-06): one modular DataTable every
 * big list in the console rides — type browse, changelog, actor log, merge
 * requests, record activity/incoming/provenance, connectors, sync runs.
 *
 * Split in two so surfaces differ by config, not code:
 * - `useDataTable` builds the TanStack v8 instance in fully manual mode (the
 *   server sorts/filters/pages) and wires per-surface column visibility AND
 *   order, persisted in localStorage (`lib/table-prefs.ts`) and edited
 *   through `DataTableViewOptions`.
 * - `DataTable` renders one page: fixed table layout with KIND-AWARE,
 *   PROPORTIONAL column widths — a column is fixed px (`meta.width`) or
 *   flexible (`meta.size = {min, max?, weight?}`), and `distributeWidths`
 *   (lib/column-widths.ts) shares the measured container among the flexible
 *   ones. A cell uses its column's width and truncates only at the column
 *   boundary — full value on hover is the cell renderer's contract. Headers
 *   carry drag-resize handles: a resized column becomes a fixed px override
 *   persisted with the other prefs; double-click clears it back to computed.
 *   Dynamic gutters on the first and last VISIBLE columns (rule 6 survives
 *   hiding/reordering), optional expandable rows (one shared expansion slot
 *   for payload/evidence detail), a compact density for rails, and
 *   consistent skeleton/empty states. */

import { Fragment, useLayoutEffect, useMemo, useRef, useState } from "react"
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
  type ColumnOrderState,
  type ColumnSizingState,
  type OnChangeFn,
  type SortingState,
  type Table as TanstackTable,
  type VisibilityState,
} from "@tanstack/react-table"
import { ChevronRightIcon } from "lucide-react"

import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  distributeWidths,
  type ColumnWidthSpec,
  type FlexSize,
} from "@/lib/column-widths"
import {
  columnVisibilityOf,
  loadTablePrefs,
  orderedColumns,
  prefsDelta,
  saveTablePrefs,
  type TablePrefs,
} from "@/lib/table-prefs"
import { cn } from "@/lib/utils"

/** Per-column presentation knobs carried on `meta` (the v8-sanctioned spot). */
declare module "@tanstack/react-table" {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  interface ColumnMeta<TData, TValue> {
    headerClassName?: string
    cellClassName?: string
    /** The Columns dropdown's name for this column (defaults to the id). */
    label?: string
    /** Fixed width in px — the escape hatch for true fixed columns (action
     * verbs, datetimes, `updated`); `size` wins nothing here, don't set both. */
    width?: number
    /** Kind-aware flexible sizing: the column shares the container's
     * leftover width proportionally to `weight`, clamped to `[min, max]`
     * (lib/column-widths.ts). Absent both knobs, the column flexes with the
     * density's default min and weight 1. */
    size?: FlexSize
  }
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  interface TableMeta<TData> {
    /** The surface's natural column order — the Reset target. */
    naturalIds?: string[]
    /** Hidden-by-default column ids — the Reset target's visibility. */
    defaultHidden?: string[]
    /** ONE atomic reset — order, visibility AND sizing together (sequential
     * state writes would persist each other's stale halves). */
    resetColumnPrefs?: () => void
  }
}

// ── the instance ────────────────────────────────────────────────────────────

export interface UseDataTableOptions<TData> {
  columns: ColumnDef<TData, unknown>[]
  data: TData[]
  sorting?: SortingState
  onSortingChange?: OnChangeFn<SortingState>
  /** Stable row identity — expansion state and React keys ride on it. */
  getRowId?: (row: TData, index: number) => string
  /** The per-surface localStorage key for column visibility + order; absent,
   * the column state still works but does not persist. */
  prefsKey?: string
  /** Columns the surface hides until the user asks for them. */
  defaultHidden?: string[]
}

const NOOP_SORT: OnChangeFn<SortingState> = () => {}

export function useDataTable<TData>(
  opts: UseDataTableOptions<TData>
): TanstackTable<TData> {
  const { prefsKey, defaultHidden } = opts
  const naturalIds = useMemo(
    () =>
      opts.columns
        .map((c) => c.id)
        .filter((id): id is string => typeof id === "string"),
    [opts.columns]
  )

  const [prefs, setPrefs] = useState<TablePrefs>(() =>
    prefsKey ? (loadTablePrefs(prefsKey) ?? {}) : {}
  )
  // A surface switch (browse navigating between types) re-reads its store.
  const keyRef = useRef(prefsKey)
  if (keyRef.current !== prefsKey) {
    keyRef.current = prefsKey
    setPrefs(prefsKey ? (loadTablePrefs(prefsKey) ?? {}) : {})
  }

  const columnOrder = useMemo(
    () => orderedColumns(naturalIds, prefs.order),
    [naturalIds, prefs.order]
  )
  const columnVisibility = useMemo(
    () => columnVisibilityOf(naturalIds, prefs.hidden ?? defaultHidden),
    [naturalIds, prefs.hidden, defaultHidden]
  )
  // The drag-resize overrides: column id → fixed px, empty = all computed.
  const columnSizing = useMemo<ColumnSizingState>(
    () => prefs.sizing ?? {},
    [prefs.sizing]
  )

  function persist(
    order: string[],
    visibility: Record<string, boolean>,
    sizing: Record<string, number>
  ) {
    const delta = prefsDelta(
      naturalIds,
      order,
      visibility,
      defaultHidden,
      sizing
    )
    setPrefs(delta)
    if (prefsKey) saveTablePrefs(prefsKey, delta)
  }

  const onColumnOrderChange: OnChangeFn<ColumnOrderState> = (updater) => {
    const next = typeof updater === "function" ? updater(columnOrder) : updater
    persist(next, columnVisibility, columnSizing)
  }
  const onColumnVisibilityChange: OnChangeFn<VisibilityState> = (updater) => {
    const next =
      typeof updater === "function" ? updater(columnVisibility) : updater
    persist(columnOrder, { ...columnVisibility, ...next }, columnSizing)
  }
  const onColumnSizingChange: OnChangeFn<ColumnSizingState> = (updater) => {
    const next = typeof updater === "function" ? updater(columnSizing) : updater
    persist(columnOrder, columnVisibility, next)
  }

  return useReactTable({
    data: opts.data,
    columns: opts.columns,
    getCoreRowModel: getCoreRowModel(),
    manualSorting: true,
    manualFiltering: true,
    manualPagination: true,
    enableSortingRemoval: false,
    columnResizeMode: "onChange",
    getRowId: opts.getRowId,
    state: {
      sorting: opts.sorting ?? [],
      columnOrder,
      columnVisibility,
      columnSizing,
    },
    onSortingChange: opts.onSortingChange ?? NOOP_SORT,
    onColumnOrderChange,
    onColumnVisibilityChange,
    onColumnSizingChange,
    meta: {
      naturalIds,
      defaultHidden,
      resetColumnPrefs: () =>
        persist(
          [...naturalIds],
          columnVisibilityOf(naturalIds, defaultHidden),
          {}
        ),
    },
  })
}

// ── the renderer ────────────────────────────────────────────────────────────

export type TableDensity = "default" | "compact"

interface DataTableProps<TData> {
  table: TanstackTable<TData>
  onRowClick?: (row: TData) => void
  /** Render skeleton rows in place of data (initial load). */
  loading?: boolean
  /** What an empty page should say — the first-party Empty composed by the
   * page, so every blankable surface speaks for itself. */
  empty?: React.ReactNode
  /** THE shared expansion slot: a row opens in place beneath its line. */
  renderExpanded?: (row: TData) => React.ReactNode
  /** Which rows can expand; default all, when `renderExpanded` is given. */
  expandable?: (row: TData) => boolean
  rowClassName?: (row: TData) => string | undefined
  /** `compact` is the rails' voice: xs type, tighter rows, narrower gutter. */
  density?: TableDensity
}

/** Flexible columns without a declared `size` still need room to speak. */
const FLEX_MIN_PX = { default: 140, compact: 96 }
const EXPANDER_PX = 28
/** A drag can't crush a column below legibility. */
const RESIZE_MIN_PX = 60

export function DataTable<TData>({
  table,
  onRowClick,
  loading,
  empty,
  renderExpanded,
  expandable,
  rowClassName,
  density = "default",
}: DataTableProps<TData>) {
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set())
  function toggle(id: string) {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const hasExpander = Boolean(renderExpanded)
  const visible = table.getVisibleLeafColumns()
  const span = visible.length + (hasExpander ? 1 : 0)
  const gutterL = density === "compact" ? "pl-4" : "pl-6"
  const gutterR = density === "compact" ? "pr-4" : "pr-6"

  // ── kind-aware widths ─────────────────────────────────────────────────────
  // The wrapper is measured (ResizeObserver keeps it fresh) and the visible
  // columns' specs — user override px > meta.width px > meta.size flex —
  // run through the pure distribution; the table gets the exact sum, so it
  // fills the container or overflows into the horizontal scroll, never both.
  const wrapRef = useRef<HTMLDivElement>(null)
  const [containerPx, setContainerPx] = useState(0)
  useLayoutEffect(() => {
    const el = wrapRef.current
    if (!el) return
    setContainerPx(el.clientWidth)
    if (typeof ResizeObserver === "undefined") return // jsdom
    const ro = new ResizeObserver(() => setContainerPx(el.clientWidth))
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const sizingOverrides = table.getState().columnSizing
  const specs: ColumnWidthSpec[] = visible.map((col) => {
    const override = sizingOverrides[col.id]
    if (typeof override === "number" && override > 0) {
      return { id: col.id, px: override }
    }
    const meta = col.columnDef.meta
    if (meta?.width !== undefined) return { id: col.id, px: meta.width }
    return {
      id: col.id,
      flex: {
        min: meta?.size?.min ?? FLEX_MIN_PX[density],
        max: meta?.size?.max,
        weight: meta?.size?.weight,
      },
    }
  })
  const widths = distributeWidths(
    containerPx - (hasExpander ? EXPANDER_PX : 0),
    specs
  )
  const tableWidth =
    Object.values(widths).reduce((sum, px) => sum + px, 0) +
    (hasExpander ? EXPANDER_PX : 0)

  // ── drag-resize ───────────────────────────────────────────────────────────
  // The handle writes a fixed px override through TanStack's columnSizing
  // (persisted by useDataTable with the other prefs); while dragging, the
  // OTHER flexible columns keep redistributing live. Double-click clears the
  // override back to the computed width.
  const [resizingId, setResizingId] = useState<string | null>(null)
  function startResize(e: React.PointerEvent, id: string) {
    e.preventDefault()
    e.stopPropagation()
    const startX = e.clientX
    const startW = widths[id] ?? FLEX_MIN_PX[density]
    setResizingId(id)
    const onMove = (ev: PointerEvent) => {
      const next = Math.max(
        RESIZE_MIN_PX,
        Math.round(startW + (ev.clientX - startX))
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

  return (
    <div ref={wrapRef} className="w-full">
      <Table
        className={cn(
          "table-fixed",
          density === "compact"
            ? "text-xs [&_td]:py-1.5 [&_th]:h-8"
            : "[&_td]:py-2.5"
        )}
        style={{ width: tableWidth, minWidth: tableWidth }}
      >
        <TableHeader>
          {table.getHeaderGroups().map((headerGroup) => (
            <TableRow key={headerGroup.id} className="hover:bg-transparent">
              {hasExpander && (
                <TableHead
                  className={cn(gutterL, "pr-0")}
                  style={{ width: EXPANDER_PX }}
                />
              )}
              {headerGroup.headers.map((header, i) => (
                <TableHead
                  key={header.id}
                  className={cn(
                    "relative",
                    header.column.columnDef.meta?.headerClassName,
                    !hasExpander && i === 0 && gutterL,
                    i === headerGroup.headers.length - 1 && gutterR
                  )}
                  style={{ width: widths[header.column.id] }}
                >
                  {header.isPlaceholder
                    ? null
                    : flexRender(
                        header.column.columnDef.header,
                        header.getContext()
                      )}
                  {/* the slim grab handle: drag fixes the column's px (persisted
                    with the prefs), double-click returns it to computed. */}
                  <span
                    role="presentation"
                    className="group/resize absolute inset-y-0 right-0 z-10 flex w-2 cursor-col-resize touch-none justify-end select-none"
                    onPointerDown={(e) => startResize(e, header.column.id)}
                    onDoubleClick={(e) => {
                      e.stopPropagation()
                      clearResize(header.column.id)
                    }}
                    onClick={(e) => e.stopPropagation()}
                  >
                    <span
                      className={cn(
                        "h-full w-px bg-border opacity-0 transition-opacity group-hover/resize:opacity-100",
                        resizingId === header.column.id &&
                          "w-0.5 bg-primary opacity-100"
                      )}
                    />
                  </span>
                </TableHead>
              ))}
            </TableRow>
          ))}
        </TableHeader>
        <TableBody>
          {loading ? (
            Array.from({ length: 10 }, (_, i) => (
              <TableRow key={`skeleton-${i}`} className="hover:bg-transparent">
                {hasExpander && <TableCell className={cn(gutterL, "pr-0")} />}
                {visible.map((col, j) => (
                  <TableCell
                    key={col.id}
                    className={cn(
                      col.columnDef.meta?.cellClassName,
                      !hasExpander && j === 0 && gutterL,
                      j === visible.length - 1 && gutterR
                    )}
                  >
                    <Skeleton className="h-4 w-3/5" />
                  </TableCell>
                ))}
              </TableRow>
            ))
          ) : rows.length ? (
            rows.map((row) => {
              const canExpand =
                hasExpander && (expandable?.(row.original) ?? true)
              const open = canExpand && expanded.has(row.id)
              const cells = row.getVisibleCells()
              return (
                <Fragment key={row.id}>
                  <TableRow
                    className={cn(
                      (onRowClick || canExpand) && "cursor-pointer",
                      open && "bg-muted/50",
                      rowClassName?.(row.original)
                    )}
                    data-expanded={open || undefined}
                    onClick={
                      onRowClick
                        ? () => onRowClick(row.original)
                        : canExpand
                          ? () => toggle(row.id)
                          : undefined
                    }
                  >
                    {hasExpander && (
                      <TableCell className={cn(gutterL, "pr-0")}>
                        {canExpand && (
                          <button
                            type="button"
                            aria-expanded={open}
                            aria-label="Toggle row details"
                            className="flex cursor-pointer items-center"
                            onClick={(e) => {
                              e.stopPropagation()
                              toggle(row.id)
                            }}
                          >
                            <ChevronRightIcon
                              className={cn(
                                "size-3 text-muted-foreground transition-transform",
                                open && "rotate-90"
                              )}
                            />
                          </button>
                        )}
                      </TableCell>
                    )}
                    {cells.map((cell, i) => (
                      <TableCell
                        key={cell.id}
                        className={cn(
                          "overflow-hidden",
                          cell.column.columnDef.meta?.cellClassName,
                          !hasExpander && i === 0 && gutterL,
                          i === cells.length - 1 && gutterR
                        )}
                      >
                        {flexRender(
                          cell.column.columnDef.cell,
                          cell.getContext()
                        )}
                      </TableCell>
                    ))}
                  </TableRow>
                  {open && (
                    <TableRow className="hover:bg-transparent">
                      <TableCell colSpan={span} className="p-0">
                        {renderExpanded!(row.original)}
                      </TableCell>
                    </TableRow>
                  )}
                </Fragment>
              )
            })
          ) : (
            <TableRow className="hover:bg-transparent">
              <TableCell colSpan={span} className="p-0">
                {empty}
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </div>
  )
}
