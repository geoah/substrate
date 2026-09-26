/** The Columns dropdown (owner ruling, 2026-08-06): every table lets the
 * user show/hide AND reorder columns, per surface, persisted — the popover
 * edits the instance's state and `useDataTable` writes the delta to
 * localStorage. Reordering is explicit up/down (keyboard-honest, no drag
 * machinery); Reset returns the surface's own defaults and clears the store. */

import {
  ChevronDownIcon,
  ChevronUpIcon,
  Columns3Icon,
  Settings2Icon,
} from "lucide-react"
import type { RowData } from "@tanstack/react-table"

import type { DataTableInstance } from "@/components/data-table/data-table"
import { Button } from "@/components/ui/button"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { columnVisibilityOf } from "@/lib/table-prefs"
import { cn } from "@/lib/utils"

interface DataTableViewOptionsProps<TData extends RowData> {
  table: DataTableInstance<TData>
  /** Icon-only trigger for tight toolbars (the record rails). */
  compact?: boolean
  /** The collection toolbar's icon button. */
  toolbar?: boolean
}

export function DataTableViewOptions<TData extends RowData>({
  table,
  compact,
  toolbar,
}: DataTableViewOptionsProps<TData>) {
  const order = table.state.columnOrder
  const columns = order
    .map((id) => table.getColumn(id))
    .filter((c): c is NonNullable<typeof c> => Boolean(c))

  const naturalIds = table.options.meta?.naturalIds ?? order
  const defaultHidden = table.options.meta?.defaultHidden ?? []
  const emptyHidden = table.options.meta?.emptyHidden ?? []
  const dirty =
    order.some((id, i) => id !== naturalIds[i]) ||
    columns.some(
      (c) =>
        !emptyHidden.includes(c.id) &&
        c.getIsVisible() === defaultHidden.includes(c.id)
    ) ||
    // a drag-resized column counts too — Reset clears sizing overrides.
    Object.keys(table.state.columnSizing).length > 0

  function move(id: string, delta: -1 | 1) {
    const at = order.indexOf(id)
    const to = at + delta
    if (at < 0 || to < 0 || to >= order.length) return
    const next = [...order]
    next.splice(at, 1)
    next.splice(to, 0, id)
    table.setColumnOrder(next)
  }

  function reset() {
    // One atomic write: sequential set calls would each persist the others'
    // stale halves (caught live, 2026-08-06).
    if (table.options.meta?.resetColumnPrefs) {
      table.options.meta.resetColumnPrefs()
    } else {
      table.setColumnOrder([...naturalIds])
      table.setColumnVisibility(columnVisibilityOf(naturalIds, defaultHidden))
      table.setColumnSizing({})
    }
  }

  return (
    <Popover>
      <PopoverTrigger
        render={
          <Button
            variant={compact || toolbar ? "ghost" : "outline"}
            size="sm"
            aria-label="Configure columns"
            title="Columns"
            className={cn(
              "h-8 gap-1.5 font-normal",
              compact || toolbar
                ? "w-8 px-0 text-muted-foreground"
                : "text-muted-foreground"
            )}
          />
        }
      >
        {toolbar ? (
          <Columns3Icon className="size-4" />
        ) : (
          <Settings2Icon className="size-3.5" />
        )}
        {!compact && !toolbar && "Columns"}
      </PopoverTrigger>
      <PopoverContent align="end" className="w-56 p-1">
        <div className="flex flex-col">
          <span className="px-2 pt-1 pb-1.5 text-xs text-muted-foreground">
            Show, hide and reorder columns
          </span>
          {columns.map((column, i) => {
            const visible = column.getIsVisible()
            const label = column.columnDef.meta?.label ?? column.id
            return (
              <div
                key={column.id}
                className="flex h-7 items-center gap-2 rounded-sm px-2 text-sm hover:bg-muted/50"
              >
                <label className="flex min-w-0 flex-1 cursor-pointer items-center gap-2">
                  <input
                    type="checkbox"
                    className="accent-primary"
                    checked={visible}
                    disabled={!column.getCanHide()}
                    onChange={(e) => column.toggleVisibility(e.target.checked)}
                  />
                  <span className="truncate">{label}</span>
                  {emptyHidden.includes(column.id) && (
                    <span className="shrink-0 text-xs text-faint">empty</span>
                  )}
                </label>
                <span className="flex shrink-0 items-center">
                  <button
                    type="button"
                    aria-label={`Move ${label} up`}
                    disabled={i === 0}
                    className="cursor-pointer rounded-sm p-[5px] text-muted-foreground hover:text-foreground disabled:pointer-events-none disabled:opacity-40"
                    onClick={() => move(column.id, -1)}
                  >
                    <ChevronUpIcon className="size-3.5" />
                  </button>
                  <button
                    type="button"
                    aria-label={`Move ${label} down`}
                    disabled={i === columns.length - 1}
                    className="cursor-pointer rounded-sm p-[5px] text-muted-foreground hover:text-foreground disabled:pointer-events-none disabled:opacity-40"
                    onClick={() => move(column.id, 1)}
                  >
                    <ChevronDownIcon className="size-3.5" />
                  </button>
                </span>
              </div>
            )
          })}
          {emptyHidden.length > 0 && table.options.meta?.showEmptyColumns && (
            <>
              <div className="mx-1 my-1 border-b" />
              <button
                type="button"
                className="cursor-pointer rounded-sm px-2 py-1 text-left text-xs text-muted-foreground hover:bg-muted/50 hover:text-foreground"
                onClick={table.options.meta.showEmptyColumns}
              >
                Show {emptyHidden.length} empty{" "}
                {emptyHidden.length === 1 ? "column" : "columns"}
              </button>
            </>
          )}
          {dirty && (
            <>
              <div className="mx-1 my-1 border-b" />
              <button
                type="button"
                className="cursor-pointer rounded-sm px-2 py-1 text-left text-xs text-muted-foreground hover:bg-muted/50 hover:text-foreground"
                onClick={reset}
              >
                Reset to defaults
              </button>
            </>
          )}
        </div>
      </PopoverContent>
    </Popover>
  )
}
