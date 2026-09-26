/** One extracted header piece (official DataTable pattern): the column title
 * in schema casing, the record-56 description as a Tooltip, and — rule 4 —
 * a sort indicator on every sortable column, the active sort distinct. */

import { cloneElement, type ReactElement, type ReactNode } from "react"
import type { Column, RowData } from "@tanstack/react-table"
import { ArrowDownIcon, ArrowUpIcon, ChevronsUpDownIcon } from "lucide-react"

import type { DataTableFeatures } from "@/components/data-table/data-table"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"

interface DataTableColumnHeaderProps<TData extends RowData, TValue> {
  column: Column<DataTableFeatures, TData, TValue>
  title: string
  description?: string
  align?: "left" | "right"
}

export function DataTableColumnHeader<TData extends RowData, TValue>({
  column,
  title,
  description,
  align = "left",
}: DataTableColumnHeaderProps<TData, TValue>) {
  const sorted = column.getIsSorted()

  // The tooltip rides a focusable control, so a keyboard reader reaches the
  // description too: the sort button where there is one, else a button of
  // its own.
  const withTip = (trigger: ReactElement, body: ReactNode) =>
    description ? (
      <Tooltip>
        <TooltipTrigger render={trigger}>{body}</TooltipTrigger>
        <TooltipContent>{description}</TooltipContent>
      </Tooltip>
    ) : (
      cloneElement(trigger, undefined, body)
    )

  if (!column.getCanSort()) {
    return (
      <div
        className={cn("flex items-center", align === "right" && "justify-end")}
      >
        {description ? (
          withTip(
            <button
              type="button"
              className="cursor-help rounded-[2px] outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
            />,
            title
          )
        ) : (
          <span>{title}</span>
        )}
      </div>
    )
  }

  return withTip(
    <button
      type="button"
      className={cn(
        "flex w-full items-center gap-1 hover:text-foreground",
        align === "right" && "justify-end",
        sorted && "text-foreground"
      )}
      onClick={() => column.toggleSorting(sorted === "asc")}
    />,
    <>
      <span>{title}</span>
      {sorted === "desc" ? (
        <ArrowDownIcon className="size-3.5" />
      ) : sorted === "asc" ? (
        <ArrowUpIcon className="size-3.5" />
      ) : (
        // opacity-40 sank below the contrast floor in dark mode (rule 12,
        // codex finding 2026-08-05); 65 keeps it quiet but legible.
        <ChevronsUpDownIcon className="size-3.5 opacity-65" />
      )}
    </>
  )
}
