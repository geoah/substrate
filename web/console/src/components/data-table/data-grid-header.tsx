/** A grid column's header: the property's icon and label (its key in
 * technical mode), an ⓘ that shows on hover and carries the property's
 * description, and — on a sortable column — the click that sorts by it, with
 * an arrow while it does. */

import type { Column, RowData } from "@tanstack/react-table"
import { ArrowDownIcon, ArrowUpIcon, InfoIcon } from "lucide-react"
import type { LucideIcon } from "lucide-react"

import type { DataTableFeatures } from "@/components/data-table/data-table"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"

export function GridColumnHeader<TData extends RowData, TValue>({
  column,
  label,
  icon: Icon,
  description,
  mono,
  align = "left",
}: {
  column: Column<DataTableFeatures, TData, TValue>
  label: string
  icon?: LucideIcon
  description?: string
  /** The label is a property key (technical mode). */
  mono?: boolean
  align?: "left" | "right"
}) {
  const sorted = column.getIsSorted()
  const canSort = column.getCanSort()
  const body = (
    <>
      {Icon && <Icon aria-hidden className="size-3.5 shrink-0" />}
      <span className={cn("truncate", mono && "font-mono text-[12px]")}>
        {label}
      </span>
      {sorted === "desc" ? (
        <ArrowDownIcon aria-label="sorted descending" className="size-3" />
      ) : sorted === "asc" ? (
        <ArrowUpIcon aria-label="sorted ascending" className="size-3" />
      ) : null}
    </>
  )
  return (
    <div
      className={cn(
        "flex min-w-0 items-center gap-1",
        align === "right" && "justify-end"
      )}
    >
      {canSort ? (
        <button
          type="button"
          title={`Sort by ${label}`}
          className={cn(
            "flex min-w-0 cursor-pointer items-center gap-1.5 rounded-[4px] outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50",
            sorted && "text-foreground"
          )}
          onClick={() => column.toggleSorting(sorted === "asc")}
        >
          {body}
        </button>
      ) : (
        <span className="flex min-w-0 items-center gap-1.5">{body}</span>
      )}
      {description && (
        <Tooltip>
          <TooltipTrigger
            render={
              <button
                type="button"
                aria-label={description}
                className="inline-grid shrink-0 cursor-help place-items-center rounded-[4px] opacity-0 outline-none group-hover/th:opacity-100 focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-ring/50"
              />
            }
          >
            <InfoIcon className="size-3.5" />
          </TooltipTrigger>
          <TooltipContent className="max-w-72">{description}</TooltipContent>
        </Tooltip>
      )}
    </div>
  )
}
