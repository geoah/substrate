/** The Columns dropdown (owner ruling, 2026-08-06): every table lets the
 * user show/hide AND reorder columns, per surface, persisted — the popover
 * edits the instance's state and `useDataTable` writes the delta to
 * localStorage. Reordering is explicit up/down (keyboard-honest, no drag
 * machinery); Reset returns the surface's own defaults and clears the store. */

import { ChevronDownIcon, ChevronUpIcon, Settings2Icon } from "lucide-react"
import type { Table as TanstackTable } from "@tanstack/react-table"

import { Button } from "@/components/ui/button"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { columnVisibilityOf } from "@/lib/table-prefs"
import { cn } from "@/lib/utils"

interface DataTableViewOptionsProps<TData> {
  table: TanstackTable<TData>
  /** Icon-only trigger for tight toolbars (the record rails). */
  compact?: boolean
}

export function DataTableViewOptions<TData>({
  table,
  compact,
}: DataTableViewOptionsProps<TData>) {
  const order = table.getState().columnOrder
  const columns = order
    .map((id) => table.getColumn(id))
    .filter((c): c is NonNullable<typeof c> => Boolean(c))

  const naturalIds = table.options.meta?.naturalIds ?? order
  const defaultHidden = table.options.meta?.defaultHidden ?? []
  const dirty =
    order.some((id, i) => id !== naturalIds[i]) ||
    columns.some((c) => c.getIsVisible() === defaultHidden.includes(c.id)) ||
    // a drag-resized column counts too — Reset clears sizing overrides.
    Object.keys(table.getState().columnSizing).length > 0

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
            variant={compact ? "ghost" : "outline"}
            size="sm"
            aria-label="Configure columns"
            className={cn(
              "h-8 gap-1.5 font-normal",
              compact
                ? "w-8 px-0 text-muted-foreground"
                : "text-muted-foreground"
            )}
          />
        }
      >
        <Settings2Icon className="size-3.5" />
        {!compact && "Columns"}
      </PopoverTrigger>
      <PopoverContent align="end" className="w-56 p-1">
        <div className="flex flex-col">
          <span className="px-2 pt-1 pb-1.5 text-xs text-muted-foreground">
            Columns — show, hide, reorder
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
                </label>
                <span className="flex shrink-0 items-center">
                  <button
                    type="button"
                    aria-label={`Move ${label} up`}
                    disabled={i === 0}
                    className="cursor-pointer rounded-sm p-0.5 text-muted-foreground hover:text-foreground disabled:pointer-events-none disabled:opacity-40"
                    onClick={() => move(column.id, -1)}
                  >
                    <ChevronUpIcon className="size-3.5" />
                  </button>
                  <button
                    type="button"
                    aria-label={`Move ${label} down`}
                    disabled={i === columns.length - 1}
                    className="cursor-pointer rounded-sm p-0.5 text-muted-foreground hover:text-foreground disabled:pointer-events-none disabled:opacity-40"
                    onClick={() => move(column.id, 1)}
                  >
                    <ChevronDownIcon className="size-3.5" />
                  </button>
                </span>
              </div>
            )
          })}
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
