/** One extracted header piece (official DataTable pattern): the column title
 * in schema casing, the record-56 description as a Tooltip, and — rule 4 —
 * a sort indicator on every sortable column, the active sort distinct. */

import type { Column } from "@tanstack/react-table"
import { ArrowDownIcon, ArrowUpIcon, ChevronsUpDownIcon } from "lucide-react"

import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"

interface DataTableColumnHeaderProps<TData, TValue> {
  column: Column<TData, TValue>
  title: string
  description?: string
  align?: "left" | "right"
}

export function DataTableColumnHeader<TData, TValue>({
  column,
  title,
  description,
  align = "left",
}: DataTableColumnHeaderProps<TData, TValue>) {
  const sorted = column.getIsSorted()

  const label = description ? (
    <Tooltip>
      <TooltipTrigger render={<span className="cursor-help" />}>
        {title}
      </TooltipTrigger>
      <TooltipContent>{description}</TooltipContent>
    </Tooltip>
  ) : (
    <span>{title}</span>
  )

  if (!column.getCanSort()) {
    return (
      <div
        className={cn("flex items-center", align === "right" && "justify-end")}
      >
        {label}
      </div>
    )
  }

  return (
    <button
      type="button"
      className={cn(
        "flex w-full items-center gap-1 hover:text-foreground",
        align === "right" && "justify-end",
        sorted && "text-foreground"
      )}
      onClick={() => column.toggleSorting(sorted === "asc")}
    >
      {label}
      {sorted === "desc" ? (
        <ArrowDownIcon className="size-3.5" />
      ) : sorted === "asc" ? (
        <ArrowUpIcon className="size-3.5" />
      ) : (
        // opacity-40 sank below the contrast floor in dark mode (rule 12,
        // codex finding 2026-08-05); 65 keeps it quiet but legible.
        <ChevronsUpDownIcon className="size-3.5 opacity-65" />
      )}
    </button>
  )
}
